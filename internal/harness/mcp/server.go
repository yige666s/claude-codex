package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	toolkit "github.com/ding/claude-code/claude-go/internal/harness/tools"
)

type Server struct {
	Name     string
	Version  string
	Executor interface {
		Descriptors() []toolkit.Descriptor
		Execute(context.Context, string, json.RawMessage) (toolkit.Result, error)
	}
	registry *toolkit.Registry
	mu       sync.Mutex
	clients  map[chan []byte]struct{}
}

func NewServer(registry *toolkit.Registry) *Server {
	return &Server{
		registry: registry,
		clients:  map[chan []byte]struct{}{},
	}
}

func (s *Server) ServeStdio(ctx context.Context, in io.Reader, out io.Writer) error {
	decoder := json.NewDecoder(in)
	encoder := json.NewEncoder(out)
	for {
		var request Request
		if err := decoder.Decode(&request); err != nil {
			return err
		}
		response := s.handleRequest(ctx, request)
		if err := encoder.Encode(response); err != nil {
			return err
		}
	}
}

func (s *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	switch {
	case request.Method == http.MethodGet && request.URL.Path == "/tools":
		s.handleToolsHTTP(writer)
	case request.Method == http.MethodPost && request.URL.Path == "/call":
		s.handleCallHTTP(request.Context(), writer, request)
	case request.Method == http.MethodGet && request.URL.Path == "/sse":
		s.handleSSE(writer, request)
	default:
		http.NotFound(writer, request)
	}
}

func (s *Server) HTTPHandler() http.Handler {
	return http.HandlerFunc(s.ServeHTTP)
}

func (s *Server) handleRequest(ctx context.Context, request Request) Response {
	response := Response{JSONRPC: "2.0", ID: request.ID}
	switch request.Method {
	case "list_tools":
		data, _ := json.Marshal(ListToolsResult{Tools: s.describeTools()})
		response.Result = data
	case "call_tool":
		var params CallToolParams
		if err := json.Unmarshal(request.Params, &params); err != nil {
			response.Error = &RPCError{Code: -32602, Message: err.Error()}
			return response
		}
		result, err := s.execute(ctx, params.Name, params.Input)
		if err != nil {
			response.Error = &RPCError{Code: -32000, Message: err.Error()}
			return response
		}
		s.broadcastEvent("tool_call", map[string]any{"name": params.Name, "output": result.Output})
		data, _ := json.Marshal(CallToolResult{Output: result.Output})
		response.Result = data
	default:
		response.Error = &RPCError{Code: -32601, Message: "unknown method"}
	}
	return response
}

func (s *Server) handleToolsHTTP(writer http.ResponseWriter) {
	_ = json.NewEncoder(writer).Encode(ListToolsResult{Tools: s.describeTools()})
}

func (s *Server) handleCallHTTP(ctx context.Context, writer http.ResponseWriter, request *http.Request) {
	var params CallToolParams
	if err := json.NewDecoder(request.Body).Decode(&params); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	response := s.handleRequest(ctx, Request{Method: "call_tool", Params: mustJSON(params)})
	if response.Error != nil {
		http.Error(writer, response.Error.Message, http.StatusBadRequest)
		return
	}
	_, _ = writer.Write(response.Result)
}

func (s *Server) handleSSE(writer http.ResponseWriter, request *http.Request) {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		http.Error(writer, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	writer.Header().Set("content-type", "text/event-stream")
	writer.Header().Set("cache-control", "no-cache")
	client := make(chan []byte, 8)
	s.mu.Lock()
	s.clients[client] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.clients, client)
		s.mu.Unlock()
	}()

	fmt.Fprintf(writer, "event: ready\ndata: {\"status\":\"ok\"}\n\n")
	flusher.Flush()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-request.Context().Done():
			return
		case data := <-client:
			fmt.Fprintf(writer, "event: message\ndata: %s\n\n", data)
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprintf(writer, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

func (s *Server) describeTools() []ToolDefinition {
	var descriptors []toolkit.Descriptor
	switch {
	case s.registry != nil:
		descriptors = s.registry.Descriptors()
	case s.Executor != nil:
		descriptors = s.Executor.Descriptors()
	}
	tools := make([]ToolDefinition, 0, len(descriptors))
	for _, descriptor := range descriptors {
		tools = append(tools, ToolDefinition{
			Name:        descriptor.Name,
			Description: descriptor.Description,
			InputSchema: descriptor.InputSchema,
		})
	}
	return tools
}

func (s *Server) execute(ctx context.Context, name string, input json.RawMessage) (toolkit.Result, error) {
	switch {
	case s.registry != nil:
		tool, err := s.registry.Get(name)
		if err != nil {
			return toolkit.Result{}, err
		}
		return tool.Execute(ctx, input)
	case s.Executor != nil:
		return s.Executor.Execute(ctx, name, input)
	default:
		return toolkit.Result{}, fmt.Errorf("mcp server has no executor or registry")
	}
}

func (s *Server) broadcastEvent(event string, payload any) {
	data, _ := json.Marshal(map[string]any{"event": event, "payload": payload})
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.clients {
		select {
		case ch <- data:
		default:
		}
	}
}

func mustJSON(value any) json.RawMessage {
	data, _ := json.Marshal(value)
	return data
}
