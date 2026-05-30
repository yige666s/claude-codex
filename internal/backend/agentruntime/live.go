package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"claude-codex/internal/harness/state"

	"github.com/gorilla/websocket"
)

const (
	defaultLiveModel                     = "gemini-live-2.5-flash-preview-native-audio-09-2025"
	defaultLiveVertexLocation            = "us-central1"
	defaultLiveVertexAPIVersion          = "v1beta1"
	defaultLiveInputAudioMIMEType        = "audio/pcm;rate=16000"
	defaultLiveSessionTimeout            = 10 * time.Minute
	defaultLiveVADPrefixPadding          = 150 * time.Millisecond
	defaultLiveVADSilenceDuration        = 350 * time.Millisecond
	defaultLiveInitialHistoryMaxMessages = 32
	defaultLiveInitialHistoryMaxTokens   = 16000
)

type VertexLiveService struct {
	config           LiveConfig
	recorder         LiveTurnRecorder
	dialer           *websocket.Dialer
	logger           *log.Logger
	tokenProvider    *vertexLiveAccessTokenProvider
	setupPromptCache LiveSetupPromptCache
}

type LiveTurnRecorder interface {
	LiveSystemInstruction(ctx context.Context, userID, sessionID string) string
	RecordLiveTurn(ctx context.Context, userID, sessionID, userText, assistantText, model string) error
}

type LiveInitialHistoryProvider interface {
	LiveInitialHistory(ctx context.Context, userID, sessionID string) ([]state.Message, error)
}

type LiveSkillHandler interface {
	DetectLiveSkillCommand(ctx context.Context, userID, sessionID, text string) bool
	ExecuteLiveSkillCommand(ctx context.Context, userID, sessionID, text string, sink EventSink) (bool, error)
}

type LiveSkillFunctionHandler interface {
	ExecuteLiveSkillFunctionCall(ctx context.Context, userID, sessionID, skillName, args, displayText string, sink EventSink) (bool, string, error)
}

func NewVertexLiveService(config LiveConfig, recorder LiveTurnRecorder, logger *log.Logger) *VertexLiveService {
	config = normalizeLiveConfig(config)
	return &VertexLiveService{
		config:        config,
		recorder:      recorder,
		dialer:        websocket.DefaultDialer,
		logger:        logger,
		tokenProvider: newVertexLiveAccessTokenProvider(&http.Client{Timeout: 30 * time.Second}),
	}
}

func (s *VertexLiveService) SetSetupPromptCache(cache LiveSetupPromptCache) {
	if s == nil {
		return
	}
	s.setupPromptCache = cache
}

func normalizeLiveConfig(config LiveConfig) LiveConfig {
	config.Provider = strings.ToLower(strings.TrimSpace(config.Provider))
	if config.Provider == "" {
		config.Provider = "vertex"
	}
	config.Model = strings.TrimSpace(config.Model)
	if config.Model == "" {
		config.Model = defaultLiveModel
	}
	config.VertexProjectID = strings.TrimSpace(config.VertexProjectID)
	config.VertexLocation = strings.TrimSpace(config.VertexLocation)
	if config.VertexLocation == "" {
		config.VertexLocation = defaultLiveVertexLocation
	}
	config.VertexAPIVersion = strings.TrimSpace(config.VertexAPIVersion)
	if config.VertexAPIVersion == "" {
		config.VertexAPIVersion = defaultLiveVertexAPIVersion
	}
	config.InputAudioMIMEType = strings.TrimSpace(config.InputAudioMIMEType)
	if config.InputAudioMIMEType == "" {
		config.InputAudioMIMEType = defaultLiveInputAudioMIMEType
	}
	config.OutputAudioMIMEType = strings.TrimSpace(config.OutputAudioMIMEType)
	config.LiveVADStartSensitivity = liveNormalizeEnum(config.LiveVADStartSensitivity, "START_SENSITIVITY_HIGH")
	config.LiveVADEndSensitivity = liveNormalizeEnum(config.LiveVADEndSensitivity, "END_SENSITIVITY_HIGH")
	if config.LiveVADPrefixPadding <= 0 {
		config.LiveVADPrefixPadding = defaultLiveVADPrefixPadding
	}
	if config.LiveVADSilenceDuration <= 0 {
		config.LiveVADSilenceDuration = defaultLiveVADSilenceDuration
	}
	if config.SessionTimeout <= 0 {
		config.SessionTimeout = defaultLiveSessionTimeout
	}
	return config
}

func liveNormalizeEnum(value, fallback string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return fallback
	}
	return value
}

func validateLiveConfig(config LiveConfig) error {
	config = normalizeLiveConfig(config)
	if config.Provider != "vertex" {
		return fmt.Errorf("live provider %q is not supported", config.Provider)
	}
	if !strings.Contains(strings.TrimSpace(config.Model), "/") && liveVertexProjectID(config) == "" {
		return fmt.Errorf("live vertex project ID is required; set AGENT_API_LIVE_VERTEX_PROJECT_ID, VERTEX_PROJECT_ID, or GOOGLE_CLOUD_PROJECT")
	}
	return nil
}

func (s *VertexLiveService) Run(ctx context.Context, req LiveRequest, input LiveClientStream, sink EventSink) error {
	if s == nil || !s.config.Enabled {
		return fmt.Errorf("live mode is not enabled")
	}
	if s.config.Provider != "vertex" {
		return fmt.Errorf("live provider %q is not supported", s.config.Provider)
	}
	if strings.TrimSpace(req.UserID) == "" || strings.TrimSpace(req.SessionID) == "" {
		return fmt.Errorf("live request requires user and session")
	}
	if input == nil || sink == nil {
		return fmt.Errorf("live request requires input and sink")
	}
	if s.recorder == nil {
		return fmt.Errorf("live recorder is not configured")
	}
	if err := validateLiveConfig(s.config); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, s.config.SessionTimeout)
	defer cancel()

	conn, err := s.connect(ctx, req)
	if err != nil {
		_ = sink.Send(ctx, liveErrorEvent(req.SessionID, err))
		return err
	}
	defer conn.Close()
	if err := sink.Send(ctx, Event{Type: "live_ready", SessionID: req.SessionID, Data: liveJSON(liveReadyPayload{
		Model:              s.config.Model,
		InputAudioMIMEType: s.config.InputAudioMIMEType,
	})}); err != nil {
		return err
	}

	errCh := make(chan error, 2)
	var writeMu sync.Mutex
	go func() {
		errCh <- s.receiveLoop(ctx, req, conn, &writeMu, sink)
	}()
	go func() {
		errCh <- s.sendLoop(ctx, req, input, conn, &writeMu, sink)
	}()
	err = <-errCh
	cancel()
	if err != nil && !isExpectedWebSocketClose(err) {
		_ = sink.Send(context.Background(), liveErrorEvent(req.SessionID, err))
		return err
	}
	_ = sink.Send(context.Background(), Event{Type: "done", SessionID: req.SessionID})
	return nil
}

func (s *VertexLiveService) connect(ctx context.Context, req LiveRequest) (*websocket.Conn, error) {
	tokenProvider := s.tokenProvider
	if tokenProvider == nil {
		tokenProvider = newVertexLiveAccessTokenProvider(&http.Client{Timeout: 30 * time.Second})
		s.tokenProvider = tokenProvider
	}
	token, err := tokenProvider.AccessToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("live vertex access token is required: %w", err)
	}
	u, err := liveVertexWebSocketURL(s.config)
	if err != nil {
		return nil, err
	}
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+token)
	conn, _, err := s.dialer.DialContext(ctx, u, headers)
	if err != nil {
		return nil, fmt.Errorf("connect live vertex websocket: %w", err)
	}
	if err := conn.WriteJSON(s.setupMessage(ctx, req)); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("write live setup: %w", err)
	}
	return conn, nil
}

func (s *VertexLiveService) setupMessage(ctx context.Context, req LiveRequest) map[string]any {
	generationConfig := map[string]any{
		"responseModalities": []string{"AUDIO"},
	}
	if thinkingConfig := liveThinkingConfig(s.config.Model); len(thinkingConfig) > 0 {
		generationConfig["thinkingConfig"] = thinkingConfig
	}
	setup := map[string]any{
		"model":            liveVertexModelResource(s.config),
		"generationConfig": generationConfig,
		"realtimeInputConfig": map[string]any{
			"automaticActivityDetection": map[string]any{
				"disabled": true,
			},
			"turnCoverage": "TURN_INCLUDES_ONLY_ACTIVITY",
		},
		"sessionResumption": map[string]any{},
		"historyConfig": map[string]any{
			"initialHistoryInClientContent": true,
		},
		"contextWindowCompression": map[string]any{
			"slidingWindow": map[string]any{
				"targetTokens": defaultLiveInitialHistoryMaxTokens,
			},
		},
	}
	if handle := strings.TrimSpace(req.ResumeHandle); handle != "" {
		setup["sessionResumption"] = map[string]any{"handle": handle}
	}
	if s.config.InputTranscriptionEnabled {
		setup["inputAudioTranscription"] = map[string]any{}
	}
	if s.config.OutputTranscriptionEnabled {
		setup["outputAudioTranscription"] = map[string]any{}
	}
	if s.recorder != nil {
		if instruction := strings.TrimSpace(s.liveSystemInstruction(ctx, req)); instruction != "" {
			setup["systemInstruction"] = map[string]any{
				"parts": []map[string]any{{"text": instruction}},
			}
		}
		setup["tools"] = []map[string]any{{"functionDeclarations": []map[string]any{liveRunSkillFunctionDeclaration()}}}
	}
	return map[string]any{"setup": setup}
}

func liveRunSkillFunctionDeclaration() map[string]any {
	return map[string]any{
		"name":        "run_skill",
		"description": "Run one published backend skill for the current user session. Use this whenever the user asks to create, generate, transform, fetch, analyze, or process something that matches an available skill, especially images or other artifacts. Do not claim the skill has run before calling this function.",
		"parameters": map[string]any{
			"type": "OBJECT",
			"properties": map[string]any{
				"skill": map[string]any{
					"type":        "STRING",
					"description": "The published skill name from the system instruction skill list, without a leading slash.",
				},
				"args": map[string]any{
					"type":        "STRING",
					"description": "The user's concrete request to pass to the skill. Preserve important visual, file, style, and content details.",
				},
				"reason": map[string]any{
					"type":        "STRING",
					"description": "A short reason why this skill is the right one.",
				},
			},
			"required": []string{"skill", "args"},
		},
	}
}

func (s *VertexLiveService) liveSystemInstruction(ctx context.Context, req LiveRequest) string {
	if s == nil || s.recorder == nil {
		return ""
	}
	key := s.liveSetupPromptCacheKey(req)
	if s.setupPromptCache != nil && key != "" {
		if instruction, ok, err := s.setupPromptCache.GetLiveSetupPrompt(ctx, key); err == nil && ok {
			return instruction
		}
	}
	instruction := strings.TrimSpace(s.recorder.LiveSystemInstruction(ctx, req.UserID, req.SessionID))
	if instruction != "" && s.setupPromptCache != nil && key != "" {
		_ = s.setupPromptCache.SetLiveSetupPrompt(ctx, key, instruction)
	}
	return instruction
}

func (s *VertexLiveService) liveSetupPromptCacheKey(req LiveRequest) string {
	if s == nil {
		return ""
	}
	userID := strings.TrimSpace(req.UserID)
	sessionID := strings.TrimSpace(req.SessionID)
	if userID == "" || sessionID == "" {
		return ""
	}
	model := strings.NewReplacer("/", "_", ":", "_", " ", "_").Replace(strings.TrimSpace(s.config.Model))
	if model == "" {
		model = "default"
	}
	return fmt.Sprintf("%s:%s:%s", model, userPathID(userID), sessionID)
}

func liveThinkingConfig(model string) map[string]any {
	normalized := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.Contains(normalized, "2.5"):
		return map[string]any{"thinkingBudget": 0}
	case strings.Contains(normalized, "3.1"):
		return map[string]any{"thinkingLevel": "MINIMAL"}
	default:
		return nil
	}
}

func (s *VertexLiveService) sendLoop(ctx context.Context, req LiveRequest, input LiveClientStream, conn *websocket.Conn, writeMu *sync.Mutex, sink EventSink) error {
	for {
		event, err := input.ReceiveLiveClientEvent(ctx)
		if err != nil {
			return err
		}
		payload, err := liveClientEventToVertexPayload(event, s.config.InputAudioMIMEType)
		if err != nil {
			_ = sink.Send(ctx, liveErrorEvent(req.SessionID, err))
			continue
		}
		if payload == nil {
			if strings.EqualFold(strings.TrimSpace(event.Type), "close") {
				return nil
			}
			continue
		}
		writeMu.Lock()
		err = conn.WriteJSON(payload)
		writeMu.Unlock()
		if err != nil {
			return fmt.Errorf("send live input: %w", err)
		}
	}
}

func liveClientEventToVertexPayload(event LiveClientEvent, defaultMIME string) (map[string]any, error) {
	switch strings.ToLower(strings.TrimSpace(event.Type)) {
	case "audio":
		data := strings.TrimSpace(event.Data)
		if data == "" {
			return nil, fmt.Errorf("live audio event requires base64 data")
		}
		mimeType := strings.TrimSpace(event.MIMEType)
		if mimeType == "" {
			mimeType = defaultMIME
		}
		return map[string]any{
			"realtimeInput": map[string]any{
				"audio": map[string]any{"mimeType": mimeType, "data": data},
			},
		}, nil
	case "audio_end", "audio_end_and_close", "done":
		return map[string]any{"realtimeInput": map[string]any{"audioStreamEnd": true}}, nil
	case "activity_start":
		return map[string]any{"realtimeInput": map[string]any{"activityStart": map[string]any{}}}, nil
	case "activity_end":
		return map[string]any{"realtimeInput": map[string]any{"activityEnd": map[string]any{}}}, nil
	case "text":
		text := strings.TrimSpace(event.Content)
		if text == "" {
			return nil, fmt.Errorf("live text event requires content")
		}
		return map[string]any{"realtimeInput": map[string]any{"text": text}}, nil
	case "client_trace":
		return nil, nil
	case "close":
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown live client event type %q", event.Type)
	}
}

func (s *VertexLiveService) receiveLoop(ctx context.Context, req LiveRequest, conn *websocket.Conn, writeMu *sync.Mutex, sink EventSink) error {
	var turn liveTurnAccumulator
	skillHandler, _ := s.recorder.(LiveSkillHandler)
	functionHandler, _ := s.recorder.(LiveSkillFunctionHandler)
	skillTurn := false
	initialHistorySent := false
	for {
		var message map[string]any
		if err := conn.ReadJSON(&message); err != nil {
			return err
		}
		if calls := liveToolFunctionCalls(message); len(calls) > 0 {
			result, err := s.handleToolFunctionCalls(ctx, req, calls, functionHandler, conn, writeMu, sink, turn.inputText())
			if err != nil {
				return err
			}
			if result.handledSkill {
				turn.clearInput()
			}
			continue
		}
		events, complete, err := turn.consume(message, s.config.OutputAudioMIMEType)
		if err != nil {
			return err
		}
		if skillHandler != nil && !skillTurn && skillHandler.DetectLiveSkillCommand(ctx, req.UserID, req.SessionID, turn.inputText()) {
			skillTurn = true
			turn.suppressOutput()
		}
		if skillTurn {
			events = liveSkillVisibleEvents(events)
		}
		for _, event := range events {
			if event.Type == "live_setup_complete" && !initialHistorySent {
				if err := s.sendInitialHistory(ctx, req, conn, writeMu); err != nil {
					return err
				}
				initialHistorySent = true
			}
			event.SessionID = req.SessionID
			if err := sink.Send(ctx, event); err != nil {
				return err
			}
		}
		if complete {
			userText, assistantText := turn.flush()
			if skillTurn {
				skillTurn = false
				if strings.TrimSpace(userText) != "" && skillHandler != nil {
					handled, err := skillHandler.ExecuteLiveSkillCommand(ctx, req.UserID, req.SessionID, userText, sink)
					if err != nil {
						return err
					}
					if handled {
						continue
					}
				}
			}
			if strings.TrimSpace(userText) != "" || strings.TrimSpace(assistantText) != "" {
				if err := s.recorder.RecordLiveTurn(ctx, req.UserID, req.SessionID, userText, assistantText, s.config.Model); err != nil {
					return err
				}
				if strings.TrimSpace(userText) != "" {
					_ = sink.Send(ctx, Event{Type: "message", SessionID: req.SessionID, Role: "user", Content: userText})
				}
				if strings.TrimSpace(assistantText) != "" {
					_ = sink.Send(ctx, Event{Type: "message", SessionID: req.SessionID, Role: "assistant", Content: assistantText})
				}
			}
			_ = sink.Send(ctx, Event{Type: "live_response_end", SessionID: req.SessionID})
		}
	}
}

func (s *VertexLiveService) sendInitialHistory(ctx context.Context, req LiveRequest, conn *websocket.Conn, writeMu *sync.Mutex) error {
	payload := liveInitialHistoryPayload(nil)
	if provider, ok := s.recorder.(LiveInitialHistoryProvider); ok && provider != nil {
		messages, err := provider.LiveInitialHistory(ctx, req.UserID, req.SessionID)
		if err != nil {
			return fmt.Errorf("load live initial history: %w", err)
		}
		payload = liveInitialHistoryPayload(messages)
	}
	writeMu.Lock()
	err := conn.WriteJSON(payload)
	writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("write live initial history: %w", err)
	}
	return nil
}

func liveInitialHistoryPayload(messages []state.Message) map[string]any {
	turns := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		role, ok := liveInitialHistoryRole(message)
		if !ok {
			continue
		}
		content := strings.TrimSpace(message.Content)
		if content == "" {
			continue
		}
		if isSystemContextMessage(message) {
			content = "Conversation summary from earlier turns:\n" + content
		}
		turns = append(turns, map[string]any{
			"role":  role,
			"parts": []map[string]any{{"text": content}},
		})
	}
	return map[string]any{
		"clientContent": map[string]any{
			"turns":        turns,
			"turnComplete": true,
		},
	}
}

func liveInitialHistoryRole(message state.Message) (string, bool) {
	if message.Hidden {
		return "", false
	}
	if message.Status != 0 && message.Status != state.MessageStatusNormal {
		return "", false
	}
	switch message.Role {
	case state.MessageRoleUser:
		return "user", true
	case state.MessageRoleAssistant:
		return "model", true
	case state.MessageRoleSystem:
		if isSystemContextMessage(message) {
			return "user", true
		}
	}
	return "", false
}

func liveSkillVisibleEvents(events []Event) []Event {
	out := make([]Event, 0, len(events))
	for _, event := range events {
		if event.Type == "live_audio" {
			continue
		}
		if event.Type == "live_transcript" && event.Role == state.MessageRoleAssistant {
			continue
		}
		out = append(out, event)
	}
	return out
}

type liveFunctionCall struct {
	ID   string
	Name string
	Args map[string]any
}

type liveToolFunctionResult struct {
	handledSkill bool
}

func (s *VertexLiveService) handleToolFunctionCalls(ctx context.Context, req LiveRequest, calls []liveFunctionCall, handler LiveSkillFunctionHandler, conn *websocket.Conn, writeMu *sync.Mutex, sink EventSink, displayText string) (liveToolFunctionResult, error) {
	responses := make([]map[string]any, 0, len(calls))
	var result liveToolFunctionResult
	for _, call := range calls {
		response := map[string]any{}
		if !strings.EqualFold(strings.TrimSpace(call.Name), "run_skill") {
			response["error"] = fmt.Sprintf("unsupported live function %q", call.Name)
			responses = append(responses, liveFunctionResponse(call, response))
			continue
		}
		if handler == nil {
			response["error"] = "live skill function handler is not configured"
			responses = append(responses, liveFunctionResponse(call, response))
			continue
		}
		skillName := firstLiveString(call.Args["skill"], call.Args["skill_name"], call.Args["skillName"], call.Args["name"])
		args := firstLiveString(call.Args["args"], call.Args["arguments"], call.Args["prompt"], call.Args["request"])
		if skillName == "" {
			response["error"] = "run_skill requires a skill name"
			responses = append(responses, liveFunctionResponse(call, response))
			continue
		}
		handled, output, err := handler.ExecuteLiveSkillFunctionCall(ctx, req.UserID, req.SessionID, skillName, args, displayText, sink)
		if err != nil {
			response["error"] = liveToolResponseText(err.Error())
		} else if !handled {
			response["error"] = "skill was not found or is not user-invocable"
		} else {
			result.handledSkill = true
			response["result"] = liveToolResponseText(output)
		}
		responses = append(responses, liveFunctionResponse(call, response))
	}
	if len(responses) == 0 {
		return result, nil
	}
	payload := map[string]any{"toolResponse": map[string]any{"functionResponses": responses}}
	if writeMu != nil {
		writeMu.Lock()
		defer writeMu.Unlock()
	}
	if err := conn.WriteJSON(payload); err != nil {
		return result, fmt.Errorf("send live tool response: %w", err)
	}
	return result, nil
}

func liveFunctionResponse(call liveFunctionCall, response map[string]any) map[string]any {
	out := map[string]any{
		"name":     call.Name,
		"response": response,
	}
	if strings.TrimSpace(call.ID) != "" {
		out["id"] = call.ID
	}
	return out
}

func liveToolResponseText(text string) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	const limit = 2000
	if len([]rune(text)) <= limit {
		return text
	}
	runes := []rune(text)
	return string(runes[:limit]) + "..."
}

func liveToolFunctionCalls(message map[string]any) []liveFunctionCall {
	toolCall, _ := firstLiveMap(message["toolCall"], message["tool_call"])
	if len(toolCall) == 0 {
		return nil
	}
	rawCalls, _ := firstLiveSlice(toolCall["functionCalls"], toolCall["function_calls"])
	out := make([]liveFunctionCall, 0, len(rawCalls))
	for _, raw := range rawCalls {
		callMap, _ := firstLiveMap(raw)
		if len(callMap) == 0 {
			continue
		}
		args, _ := firstLiveMap(callMap["args"], callMap["arguments"])
		out = append(out, liveFunctionCall{
			ID:   firstLiveString(callMap["id"], callMap["functionCallId"], callMap["function_call_id"]),
			Name: firstLiveString(callMap["name"], callMap["functionName"], callMap["function_name"]),
			Args: args,
		})
	}
	return out
}

type liveTurnAccumulator struct {
	input            strings.Builder
	output           strings.Builder
	outputSuppressed bool
	outputActive     bool
}

func (a *liveTurnAccumulator) consume(message map[string]any, outputMIME string) ([]Event, bool, error) {
	if errValue := message["error"]; errValue != nil {
		data, _ := json.Marshal(errValue)
		return nil, false, fmt.Errorf("live server error: %s", data)
	}
	if _, ok := message["setupComplete"]; ok {
		return []Event{{Type: "live_setup_complete"}}, false, nil
	}
	if update, _ := firstLiveMap(message["sessionResumptionUpdate"], message["session_resumption_update"]); len(update) > 0 {
		if handle := firstLiveString(update["newHandle"], update["new_handle"], update["handle"]); handle != "" {
			return []Event{{Type: "live_resumption_token", Data: liveJSON(map[string]any{"handle": handle})}}, false, nil
		}
		return nil, false, nil
	}
	if goAway, _ := firstLiveMap(message["goAway"], message["go_away"]); len(goAway) > 0 {
		timeLeft := firstLiveString(goAway["timeLeft"], goAway["time_left"])
		return []Event{{Type: "live_go_away", Data: liveJSON(map[string]any{"time_left": timeLeft})}}, false, nil
	}
	content, _ := message["serverContent"].(map[string]any)
	if len(content) == 0 {
		return nil, false, nil
	}
	var events []Event
	if interrupted, _ := content["interrupted"].(bool); interrupted {
		a.output.Reset()
		a.outputSuppressed = true
		a.outputActive = false
		events = append(events, Event{Type: "live_interrupted"})
	}
	if input := liveTranscriptionText(content, "inputTranscription"); input != "" && !liveIsNoisyInputTranscript(input) {
		a.input.WriteString(input)
		events = append(events, Event{Type: "live_transcript", Role: state.MessageRoleUser, Content: input, Data: liveJSON(map[string]any{"source": "input", "final": false})})
	}
	if !a.outputSuppressed {
		outputEventStart := len(events)
		outputStarted := false
		if output := liveTranscriptionText(content, "outputTranscription"); output != "" {
			a.output.WriteString(output)
			outputStarted = true
			events = append(events, Event{Type: "live_transcript", Role: state.MessageRoleAssistant, Content: output, Data: liveJSON(map[string]any{"source": "output", "final": false})})
		}
		audioParts := liveOutputAudioParts(content, outputMIME)
		if len(audioParts) > 0 {
			outputStarted = true
		}
		if outputStarted && !a.outputActive {
			a.outputActive = true
			events = append(events[:outputEventStart], append([]Event{{Type: "live_response_start"}}, events[outputEventStart:]...)...)
		}
		for _, audio := range audioParts {
			events = append(events, Event{Type: "live_audio", Role: state.MessageRoleAssistant, Data: liveJSON(audio)})
		}
	}
	complete, _ := content["turnComplete"].(bool)
	return events, complete, nil
}

func (a *liveTurnAccumulator) flush() (string, string) {
	userText := strings.TrimSpace(a.input.String())
	assistantText := strings.TrimSpace(a.output.String())
	a.input.Reset()
	a.output.Reset()
	a.outputSuppressed = false
	a.outputActive = false
	return userText, assistantText
}

func (a *liveTurnAccumulator) inputText() string {
	if a == nil {
		return ""
	}
	return strings.TrimSpace(a.input.String())
}

func (a *liveTurnAccumulator) clearInput() {
	if a == nil {
		return
	}
	a.input.Reset()
}

func (a *liveTurnAccumulator) suppressOutput() {
	if a == nil {
		return
	}
	a.output.Reset()
	a.outputSuppressed = true
}

func liveTranscriptionText(content map[string]any, key string) string {
	transcription, _ := content[key].(map[string]any)
	text, _ := transcription["text"].(string)
	return text
}

func liveIsNoisyInputTranscript(text string) bool {
	compact := liveCompactTranscriptNoiseText(text)
	runes := []rune(compact)
	if liveTranscriptNoiseContains(liveTranscriptNoise.MeaningfulShortUtterances, compact) {
		return false
	}
	if compact == "" || len(runes) < liveTranscriptNoise.MinMeaningfulRunes {
		return true
	}
	if liveTranscriptNoiseContains(liveTranscriptNoise.StandaloneFillers, compact) {
		return true
	}
	for _, filler := range liveTranscriptNoise.RepeatableFillers {
		if liveTranscriptNoiseIsExtendedFiller(compact, filler) {
			return true
		}
	}
	if len(runes) >= liveTranscriptNoise.RepeatedSingleRuneMinRunes {
		same := true
		for _, r := range runes[1:] {
			if r != runes[0] {
				same = false
				break
			}
		}
		if same {
			return true
		}
	}
	for _, item := range liveTranscriptNoise.ShortContains {
		if len(runes) <= item.MaxRunes && strings.Contains(compact, item.Value) {
			return true
		}
	}
	return false
}

func liveOutputAudioParts(content map[string]any, fallbackMIME string) []map[string]any {
	modelTurn, _ := content["modelTurn"].(map[string]any)
	parts, _ := modelTurn["parts"].([]any)
	out := make([]map[string]any, 0, len(parts))
	for _, item := range parts {
		part, _ := item.(map[string]any)
		inlineData, _ := firstLiveMap(part["inlineData"], part["inline_data"])
		if len(inlineData) == 0 {
			continue
		}
		mimeType := firstLiveString(inlineData["mimeType"], inlineData["mime_type"])
		data, _ := inlineData["data"].(string)
		if data == "" || (!strings.HasPrefix(mimeType, "audio/") && fallbackMIME == "") {
			continue
		}
		if mimeType == "" {
			mimeType = fallbackMIME
		}
		out = append(out, map[string]any{"mime_type": mimeType, "data": data})
	}
	return out
}

func firstLiveMap(values ...any) (map[string]any, bool) {
	for _, value := range values {
		if mapped, ok := value.(map[string]any); ok && len(mapped) > 0 {
			return mapped, true
		}
	}
	return nil, false
}

func firstLiveSlice(values ...any) ([]any, bool) {
	for _, value := range values {
		if items, ok := value.([]any); ok && len(items) > 0 {
			return items, true
		}
	}
	return nil, false
}

func firstLiveString(values ...any) string {
	for _, value := range values {
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			return text
		}
	}
	return ""
}

type liveReadyPayload struct {
	Model              string `json:"model"`
	InputAudioMIMEType string `json:"input_audio_mime_type"`
}

func liveVertexWebSocketURL(config LiveConfig) (string, error) {
	config = normalizeLiveConfig(config)
	base := strings.TrimSpace(config.VertexBaseURL)
	if base == "" {
		base = liveVertexBaseURL(config.VertexLocation)
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	scheme := parsed.Scheme
	if scheme != "ws" && scheme != "wss" {
		scheme = "wss"
	}
	path := strings.TrimRight(parsed.Path, "/")
	if !strings.Contains(path, "/ws/") {
		path += "/ws/google.cloud.aiplatform." + config.VertexAPIVersion + ".LlmBidiService/BidiGenerateContent"
	}
	return (&url.URL{Scheme: scheme, Host: parsed.Host, Path: path}).String(), nil
}

func liveVertexBaseURL(location string) string {
	location = strings.ToLower(strings.TrimSpace(location))
	switch location {
	case "", "us-central1":
		return "https://us-central1-aiplatform.googleapis.com"
	case "global":
		return "https://aiplatform.googleapis.com"
	case "us":
		return "https://aiplatform.us.rep.googleapis.com"
	case "eu":
		return "https://aiplatform.eu.rep.googleapis.com"
	default:
		return fmt.Sprintf("https://%s-aiplatform.googleapis.com", location)
	}
}

func liveVertexModelResource(config LiveConfig) string {
	model := strings.TrimSpace(config.Model)
	if strings.Contains(model, "/") {
		return strings.TrimLeft(model, "/")
	}
	return fmt.Sprintf("projects/%s/locations/%s/publishers/google/models/%s", liveVertexProjectID(config), config.VertexLocation, model)
}

func liveVertexProjectID(config LiveConfig) string {
	projectID := strings.TrimSpace(config.VertexProjectID)
	if projectID != "" {
		return projectID
	}
	return firstNonEmpty(envString("VERTEX_PROJECT_ID"), envString("GOOGLE_CLOUD_PROJECT"), envString("GCLOUD_PROJECT"))
}

func isExpectedWebSocketClose(err error) bool {
	if err == nil {
		return true
	}
	return websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) ||
		strings.Contains(err.Error(), "use of closed network connection") ||
		strings.Contains(err.Error(), context.Canceled.Error())
}

func liveErrorEvent(sessionID string, err error) Event {
	if err == nil {
		err = fmt.Errorf("live voice failed")
	}
	return Event{
		Type:      "error",
		SessionID: sessionID,
		Error:     err.Error(),
		Data:      liveJSON(map[string]any{"code": liveErrorCode(err), "message": livePublicErrorMessage(err)}),
	}
}

func liveErrorCode(err error) string {
	if err == nil {
		return "live_unknown"
	}
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "access token") || strings.Contains(text, "google_application_credentials") || strings.Contains(text, "vertex-service-account"):
		return "live_credentials_missing"
	case strings.Contains(text, "project id"):
		return "live_project_missing"
	case strings.Contains(text, "websocket") || strings.Contains(text, "connection refused") || strings.Contains(text, "i/o timeout"):
		return "live_provider_connection"
	case strings.Contains(text, context.DeadlineExceeded.Error()) || strings.Contains(text, "timeout"):
		return "live_timeout"
	case strings.Contains(text, "unknown live client event"):
		return "live_client_protocol"
	case strings.Contains(text, "audio event"):
		return "live_audio_invalid"
	default:
		return "live_provider_error"
	}
}

func livePublicErrorMessage(err error) string {
	switch liveErrorCode(err) {
	case "live_credentials_missing", "live_project_missing":
		return "Live mode is not configured for this environment."
	case "live_provider_connection":
		return "Live voice could not connect to the provider."
	case "live_timeout":
		return "Live voice timed out."
	case "live_client_protocol", "live_audio_invalid":
		return "Live voice received invalid audio data."
	default:
		return "Live voice failed."
	}
}

func liveJSON(value any) json.RawMessage {
	data, _ := json.Marshal(value)
	return data
}

func envString(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}
