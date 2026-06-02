# Live API 技术方案

## 目标

Live API 应被设计成一个持续语音会话，而不是一次请求。

核心目标：

- 降低环境音误触发。
- 保持连续对话体验。
- 保留 session 上下文、长期记忆、个性化和 browser memory。
- 避免模型在用户没有新请求时主动处理旧内容。
- 连接异常后可恢复。
- 出问题时能判断是前端 VAD、麦克风、WebSocket、Gemini Live 还是状态机问题。

## 总体架构

```text
Browser Mic
-> Frontend VAD / Audio Gate
-> /v1/sessions/{session_id}/live/ws
-> Backend Live Session Orchestrator
-> Vertex Gemini Live API
-> Backend Event Normalizer
-> Frontend Playback + Transcript UI
```

职责划分：

- 前端负责判断什么时候应该发送麦克风音频。
- 后端负责连接 Gemini Live、注入上下文、转发事件、落库、恢复会话和过滤 transcript 噪声。
- Gemini Live 负责实时理解音频并生成语音/文本回复。

## 当前链路基线

关键文件：

- `apps/web/src/features/workspace/hooks/useLiveVoice.ts`
- `internal/backend/agentruntime/live.go`
- `internal/backend/agentruntime/runtime.go`
- `internal/backend/agentruntime/live_noise.go`
- `scripts/live_transcript_noise.json`

当前主要由前端 VAD 决定是否送音频。后端不做主要音量判断，只转发前端的活动边界和音频帧。

```text
前端检测到持续说话
-> activity_start
-> audio frames
-> activity_end
-> Gemini Live 生成响应
-> live_audio / live_transcript / message
-> 前端播放语音并恢复监听
```

## 前端设计

### 状态机

Live 前端状态应按持续会话处理：

```text
idle
-> connecting
-> listening
-> speaking
-> thinking
-> responding
-> listening
```

普通一轮响应结束不能进入 `idle`。只有用户主动关闭 Live、会话不可恢复错误或权限错误，才应退出 Live。

### VAD 门控

前端是第一道 VAD 门控。

推荐策略：

- 使用 RMS + peak 双阈值判断候选语音。
- 要求语音持续一小段时间后才确认开始。
- 保留少量 pre-speech buffer，避免切掉开头。
- 静音持续一段时间后再发送 `activity_end`。
- 模型播放期间暂停输入，避免模型听到自己的输出。

推荐默认参数：

```ts
const livePreSpeechFrameLimit = 6;
const liveSpeechStartMinMs = 260;
const liveEndOfSpeechMs = 700;

const speechThreshold = Math.max(0.018, Math.min(0.09, noiseFloor * 4.8 + 0.007));
const peakThreshold = Math.max(0.12, speechThreshold * 4.5);
```

### 音频事件

前端只在确认语音后发送音频：

```text
activity_start
audio
audio
...
activity_end
```

静音、背景音和短促噪声不应进入上游 Live session。

### 响应结束处理

`done` 不能代表 Live 会话停止。它只能表示一轮事件完成。

正确处理方式：

- 收到 `live_response_end` 后等待语音播放队列结束。
- 收到普通 `done` 时也走恢复监听逻辑。
- 播放完成后设置 `liveInputPaused=false`。
- 如果麦克风还在，状态回到 `listening`。
- 顶部状态同步显示 `Listening`。

## 后端设计

### Live Session Orchestrator

后端 `VertexLiveService` 负责：

- 建立到 Vertex Gemini Live 的 WebSocket。
- 发送 setup message。
- 等待上游 `setupComplete`。
- 发送官方 `clientContent.turns` 初始历史。
- 转发前端音频和活动边界。
- 归一化上游事件。
- turn complete 后记录 user/assistant message。
- 处理 `goAway`、resumption token 和错误。

### Setup 配置

推荐测试环境配置：

```env
AGENT_API_LIVE_ENABLED=true
AGENT_API_LIVE_PROVIDER=vertex
AGENT_API_LIVE_MODEL=gemini-live-2.5-flash-native-audio
AGENT_API_LIVE_LANGUAGE_CODE=zh-CN
AGENT_API_LIVE_VOICE_NAME=Leda
AGENT_API_LIVE_INPUT_TRANSCRIPTION_ENABLED=true
AGENT_API_LIVE_OUTPUT_TRANSCRIPTION_ENABLED=true
AGENT_API_LIVE_SESSION_TIMEOUT=10m
```

### 上下文注入

Live 上下文分两层：

- 连接内短期上下文：由 Gemini Live session 维护。
- 连接外恢复上下文：由数据库 messages、summary、memory、personalization、browser memory 在下次 setup 时注入。

初始历史应走官方 `clientContent`，不要把最近对话塞进 `systemInstruction`。

```text
setup.systemInstruction
-> system policy / tools / personalization / memory policy

setupComplete
-> clientContent.turns
-> recent messages / summary / startup greeting instruction
```

## Prompt 策略

Live prompt 必须保留记忆，但禁止模型主动续做旧任务。

建议固定 Live 专用规则：

```text
Live conversation starts in a passive listening mode.
You may greet the user briefly after setup.
Do not answer, summarize, continue, or take action on previous messages, memory, browser context, or session history unless the user's new spoken request explicitly asks for it.
Use prior context only to disambiguate or personalize responses after the user asks something.
If the input appears to be background noise, accidental speech, or unclear transcription, do not answer substantively. Ask the user to repeat briefly.
```

这类规则应放在 Live system instruction 中，和普通 Chat 的系统消息区分。

## Transcript 噪声过滤

噪声过滤分三层：

1. 前端 VAD 减少环境音进入后端。
2. 后端 transcript filter 丢弃明显误识别文本。
3. Prompt 要求模型对含糊背景音不要实质回答。

后端和前端 transcript filter 需要使用同一套配置源：

- `scripts/live_transcript_noise.json`
- `apps/web/src/features/workspace/liveTranscriptNoiseConfig.ts`
- `internal/backend/agentruntime/live_noise_config_gen.go`

应过滤的典型内容：

- 重复字符。
- 常见 filler。
- 短外语误转写。
- 短韩文/日文且没有中文字符。
- 已知误识别短句。

不要过滤正常短中文问题，例如“今天几号”“现在几点”“你是谁”。

## 连接恢复

Live 连接要按长会话处理。

推荐策略：

- 保存 `sessionResumptionUpdate.newHandle` 到 `sessionStorage`。
- 新连接带上 `resume_handle`。
- 收到 `goAway` 时主动刷新上游 Live session。
- 非用户主动关闭时自动重连，限制最大次数。
- 重连中保持 UI 为 `reconnecting`，不要直接变成 `idle`。
- 用户主动关闭时才发送 close 并释放麦克风。

## 音频播放

前端播放应使用队列串行化：

```text
live_audio
-> queue playback
-> pause live input
-> play all queued audio
-> wait small guard delay
-> resume live input
-> listening
```

播放期间必须暂停麦克风输入，否则 Gemini Live 可能听到自己的语音输出。

## 可观测性

Live 需要记录结构化 trace，但不要记录原始音频。

推荐指标：

- setup complete 延迟。
- first input transcript 延迟。
- first output transcript 延迟。
- first voice 延迟。
- audio chunks sent。
- audio bytes sent。
- VAD start/end 次数。
- reconnect count。
- goAway count。
- filtered transcript count。
- last live status。
- websocket close reason。
- microphone track ended/muted 次数。

这些指标可以展示在 Admin Live health 或前端 debug trace 中。

## 验证清单

每次修改 Live 相关代码后至少验证：

```bash
go test ./internal/backend/agentruntime
npm --prefix apps/web run build
git diff --check
```

手工验证场景：

- 静音状态不产生用户消息。
- 背景噪声不触发回复。
- 正常中文短句能被识别。
- 一轮回复后顶部状态恢复 `Listening`。
- 一轮回复后可以继续说下一句。
- 语音播放期间不会把模型声音录回去。
- 断线后进入 reconnecting，并可恢复。
- 用户主动关闭后释放麦克风。

## 落地顺序

推荐按以下顺序推进：

1. 修正前端状态机，保证 `done` 不关闭 Live，会话响应结束后恢复监听。
2. 固化中文 Live 默认配置，保持 `zh-CN`、当前 Live model 和 voice。
3. 保留前端 VAD 作为主控，后端只转发活动边界。
4. 前后端共用 transcript noise 配置。
5. 增加 Live trace 和 Admin health 可观测信息。
6. 用真实环境做连续对话、背景音、打断、重连验证。

## 设计原则

Live API 的边界原则：

- 前端决定何时听。
- 后端保证上下文、恢复和落库。
- 模型只在明确用户输入后回答。
- 普通 turn 完成不等于 Live 会话结束。
- 记忆可以注入，但不能触发模型主动处理旧任务。
