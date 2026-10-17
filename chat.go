package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	api "github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

// The current public H5 build is K8SZW: it uses /api/ai/chat?type=0,
// not the production-only /api/chat/stream route or an OpenAI reverse proxy.
// Protocol overview: docs/PROTOCOL.md. No upstream JavaScript is redistributed.
const chatTimeout = 180 * time.Second
const maxSSEFrame = 1 << 20
const maxChatOutput = 8 << 20

type executorRPCRequest struct {
	api.ExecutorRequest
	StreamID       string `json:"stream_id"`
	HostCallbackID string `json:"host_callback_id"`
}
type chatInput struct {
	Model               string             `json:"model"`
	Messages            []chatInputMessage `json:"messages"`
	MaxTokens           *int               `json:"max_tokens"`
	MaxCompletionTokens *int               `json:"max_completion_tokens"`
	Temperature         *float64           `json:"temperature"`
	N                   *int               `json:"n"`
	Tools               json.RawMessage    `json:"tools"`
	ToolChoice          json.RawMessage    `json:"tool_choice"`
	Functions           json.RawMessage    `json:"functions"`
	FunctionCall        json.RawMessage    `json:"function_call"`
	ResponseFormat      json.RawMessage    `json:"response_format"`
}
type chatInputMessage struct {
	Role         string          `json:"role"`
	Content      json.RawMessage `json:"content"`
	ToolCalls    json.RawMessage `json:"tool_calls"`
	FunctionCall json.RawMessage `json:"function_call"`
}
type chatDelta struct{ Content, Reasoning string }

type streamEmitRequest struct {
	StreamID       string `json:"stream_id"`
	HostCallbackID string `json:"host_callback_id,omitempty"`
	Payload        []byte `json:"payload"`
}
type streamCloseRequest struct {
	StreamID       string `json:"stream_id"`
	HostCallbackID string `json:"host_callback_id,omitempty"`
	Error          string `json:"error,omitempty"`
}

func answerMode(model string) (string, error) {
	switch model {
	case "fanzha-ai":
		return "normal", nil
	case "fanzha-ai-deep":
		return "detailed", nil
	case "fanzha-ai-en":
		return "english", nil
	case "fanzha-ai-en-deep":
		return "english_detailed", nil
	default:
		return "", &statusError{400, "不支持的反诈模型"}
	}
}
func hasJSONValue(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return s != "" && s != "null" && s != "[]" && s != "{}" && s != "\"none\""
}
func messageText(raw json.RawMessage) (string, error) {
	var plain string
	if json.Unmarshal(raw, &plain) == nil {
		return plain, nil
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil || len(parts) == 0 {
		return "", &statusError{400, "反诈模型只接受纯文本消息"}
	}
	texts := make([]string, 0, len(parts))
	for _, p := range parts {
		if p.Type != "text" {
			return "", &statusError{400, "反诈模型不支持图片、音频或其他非文本内容"}
		}
		texts = append(texts, p.Text)
	}
	return strings.Join(texts, "\n"), nil
}
func prepareChat(req executorRPCRequest) (string, map[string]any, error) {
	if req.Format != "" && req.Format != "chat-completions" && req.Format != "openai" {
		return "", nil, &statusError{400, "反诈模型仅支持 chat-completions 格式"}
	}
	raw := req.Payload
	if len(raw) == 0 {
		raw = req.OriginalRequest
	}
	if len(raw) == 0 {
		return "", nil, &statusError{400, "聊天请求不能为空"}
	}
	var in chatInput
	if err := decode(raw, &in); err != nil {
		return "", nil, err
	}
	model := req.Model
	if model == "" {
		model = in.Model
	}
	mode, err := answerMode(model)
	if err != nil {
		return "", nil, err
	}
	if hasJSONValue(in.Tools) || hasJSONValue(in.ToolChoice) || hasJSONValue(in.Functions) || hasJSONValue(in.FunctionCall) {
		return "", nil, &statusError{400, "反诈模型不支持工具或函数调用"}
	}
	if in.N != nil && *in.N != 1 {
		return "", nil, &statusError{400, "反诈模型每次只支持一个回答"}
	}
	if hasJSONValue(in.ResponseFormat) {
		var f struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(in.ResponseFormat, &f) != nil || f.Type != "text" {
			return "", nil, &statusError{400, "反诈模型不支持结构化输出格式"}
		}
	}
	if len(in.Messages) == 0 {
		return "", nil, &statusError{400, "聊天请求缺少消息"}
	}
	texts := make([]string, 0, len(in.Messages))
	for _, m := range in.Messages {
		if hasJSONValue(m.ToolCalls) || hasJSONValue(m.FunctionCall) {
			return "", nil, &statusError{400, "反诈模型不支持工具历史"}
		}
		switch m.Role {
		case "system", "developer", "user", "assistant":
		default:
			return "", nil, &statusError{400, "反诈模型不支持该消息角色"}
		}
		text, err := messageText(m.Content)
		if err != nil {
			return "", nil, err
		}
		if strings.TrimSpace(text) == "" {
			return "", nil, &statusError{400, "聊天消息文本不能为空"}
		}
		if len(in.Messages) == 1 && m.Role == "user" {
			texts = append(texts, text)
		} else {
			texts = append(texts, "["+m.Role+"]\n"+text)
		}
	}
	if in.Messages[len(in.Messages)-1].Role != "user" {
		return "", nil, &statusError{400, "聊天请求最后一条必须是用户消息"}
	}
	// The upstream has only a text field, not native role-separated messages.
	// Explicitly preserve the supplied transcript rather than pretending it has
	// upstream system-role or automatic conversation-history support.
	text := strings.Join(texts, "\n\n")
	if len(text) > 1<<20 {
		return "", nil, &statusError{400, "聊天消息超过大小限制"}
	}
	maxTokens := 2048
	if in.MaxTokens != nil {
		maxTokens = *in.MaxTokens
	}
	if in.MaxCompletionTokens != nil {
		maxTokens = *in.MaxCompletionTokens
	}
	if maxTokens < 1 || maxTokens > 2048 {
		return "", nil, &statusError{400, "反诈模型 max_tokens 必须在 1 到 2048 之间"}
	}
	temperature := "0.1"
	if in.Temperature != nil {
		if *in.Temperature < 0 || *in.Temperature > 2 {
			return "", nil, &statusError{400, "temperature 必须在 0 到 2 之间"}
		}
		temperature = strconv.FormatFloat(*in.Temperature, 'f', -1, 64)
	}
	return model, map[string]any{"text": text, "version": 2, "stream": true, "max_tokens": maxTokens, "using_context": false, "temperature": temperature, "files": []any{}, "model_name": "", "answer_mode": mode}, nil
}

func checkChatHTTP(resp *http.Response) error {
	if resp == nil || resp.Body == nil {
		return &statusError{502, "反诈上游响应为空"}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		status := resp.StatusCode
		if status < 400 {
			status = 502
		}
		return &statusError{status, "反诈上游返回 HTTP " + strconv.Itoa(resp.StatusCode)}
	}
	return nil
}
func createChatSession(ctx context.Context, token string) (string, error) {
	resp, err := doUpstream(ctx, http.MethodPost, "/api/ai/create_session", token, map[string]any{})
	if err != nil {
		return "", err
	}
	if err = checkChatHTTP(resp); err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxSSEFrame+1))
	if err != nil || len(raw) > maxSSEFrame {
		return "", &statusError{502, "读取反诈会话失败"}
	}
	var outer struct {
		Code json.RawMessage `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &outer) != nil {
		return "", &statusError{502, "反诈会话响应不是有效 JSON"}
	}
	if len(outer.Code) > 0 && string(outer.Code) != "200" {
		return "", upstreamBusinessError(outer.Code, "反诈会话创建被拒绝")
	}
	var inner struct {
		Code string `json:"code"`
		Data string `json:"data"`
	}
	if json.Unmarshal(outer.Data, &inner) != nil || inner.Code != "10000" || strings.TrimSpace(inner.Data) == "" {
		return "", &statusError{502, "反诈会话响应缺少有效会话 ID"}
	}
	if len(inner.Data) > 512 {
		return "", &statusError{502, "反诈会话 ID 无效"}
	}
	return inner.Data, nil
}
func upstreamBusinessError(code json.RawMessage, message string) error {
	status := 502
	var n int
	if json.Unmarshal(code, &n) == nil && (n == 401 || n == 403 || n == 429) {
		status = n
	}
	return &statusError{status, message}
}

// Consume complete SSE events, joining multiple data: lines as the SSE standard
// requires. A clean TCP EOF is not a successful answer without an explicit
// upstream completion marker. exec_elapsed_secs is the current H5 end field.
func scanAntiFraudSSE(ctx context.Context, r io.Reader, onDelta func(chatDelta) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), maxSSEFrame)
	var event []string
	eventBytes, total, answerBytes := 0, 0, 0
	complete := false
	process := func() error {
		if len(event) == 0 {
			return nil
		}
		raw := strings.TrimSpace(strings.Join(event, "\n"))
		event = nil
		eventBytes = 0
		if raw == "[DONE]" {
			complete = true
			return nil
		}
		if raw == "" {
			return nil
		}
		var frame struct {
			Code json.RawMessage `json:"code"`
			Data json.RawMessage `json:"data"`
		}
		if json.Unmarshal([]byte(raw), &frame) != nil {
			return &statusError{502, "反诈 SSE 包含无效 JSON"}
		}
		if string(frame.Code) != "200" {
			return upstreamBusinessError(frame.Code, "反诈上游拒绝聊天请求")
		}
		var data struct {
			Type             string          `json:"type"`
			Answer           string          `json:"answer"`
			Thinking         string          `json:"thinking"`
			Reasoning        string          `json:"reasoning"`
			ReasoningContent string          `json:"reasoning_content"`
			ExecElapsed      json.RawMessage `json:"exec_elapsed_secs"`
		}
		if len(frame.Data) == 0 || string(frame.Data) == "null" || json.Unmarshal(frame.Data, &data) != nil {
			return &statusError{502, "反诈 SSE 数据字段无效"}
		}
		delta := chatDelta{}
		if data.Type == "answer" {
			delta.Content = data.Answer
		}
		// Current public H5 exposes answer only; these explicitly named optional
		// reasoning fields are compatible extensions, not fabricated thoughts.
		delta.Reasoning = data.ReasoningContent
		if delta.Reasoning == "" {
			delta.Reasoning = data.Thinking
		}
		if delta.Reasoning == "" {
			delta.Reasoning = data.Reasoning
		}
		if (data.Type == "thinking" || data.Type == "reasoning") && delta.Reasoning == "" {
			delta.Reasoning = data.Answer
		}
		total += len(delta.Content) + len(delta.Reasoning)
		answerBytes += len(delta.Content)
		if total > maxChatOutput {
			return &statusError{502, "反诈回答超过大小限制"}
		}
		if delta.Content != "" || delta.Reasoning != "" {
			if err := onDelta(delta); err != nil {
				return err
			}
		}
		if len(data.ExecElapsed) > 0 && string(data.ExecElapsed) != "null" {
			// The actual upstream also sends exec_elapsed_secs as a string.
			// The official H5 checks truthiness rather than requiring a number.
			var elapsed float64
			if json.Unmarshal(data.ExecElapsed, &elapsed) == nil {
				if elapsed < 0 {
					return &statusError{502, "反诈 SSE 结束字段无效"}
				}
				if elapsed > 0 {
					complete = true
				}
			} else {
				var elapsedText string
				if json.Unmarshal(data.ExecElapsed, &elapsedText) != nil || len(elapsedText) > 128 {
					return &statusError{502, "反诈 SSE 结束字段无效"}
				}
				if strings.TrimSpace(elapsedText) != "" {
					complete = true
				}
			}
		}
		return nil
	}
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return &statusError{502, "反诈聊天已取消或超时"}
		default:
		}
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			if err := process(); err != nil {
				return err
			}
			if complete {
				break
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, ok := strings.Cut(line, ":")
		if !ok {
			value = ""
		}
		if field == "data" {
			value = strings.TrimPrefix(value, " ")
			eventBytes += len(value) + 1
			if eventBytes > maxSSEFrame {
				return &statusError{502, "反诈 SSE 事件超过大小限制"}
			}
			event = append(event, value)
		}
	}
	if err := scanner.Err(); err != nil {
		return &statusError{502, "读取反诈 SSE 流失败"}
	}
	if !complete && len(event) > 0 {
		if err := process(); err != nil {
			return err
		}
	}
	if !complete {
		return &statusError{502, "反诈 SSE 提前结束，未收到完成标记"}
	}
	if answerBytes == 0 {
		return &statusError{502, "反诈上游没有返回回答文本"}
	}
	return nil
}

func completionID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err == nil {
		return "chatcmpl-fanzha-" + hex.EncodeToString(b[:])
	}
	return fmt.Sprintf("chatcmpl-fanzha-%d", time.Now().UnixNano())
}
func streamChunk(id, model string, created int64, delta map[string]any, finish any) []byte {
	raw, _ := json.Marshal(map[string]any{"id": id, "object": "chat.completion.chunk", "created": created, "model": model, "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}})
	// CPA's OpenAI HTTP handler wraps each executor payload in data: ... .
	// Emit bare JSON here to avoid nested SSE framing on the public endpoint.
	return raw
}
func execute(b []byte, stream bool) ([]byte, error) {
	var req executorRPCRequest
	if err := decode(b, &req); err != nil {
		return nil, err
	}
	model, body, err := prepareChat(req)
	if err != nil {
		return nil, err
	}
	if stream && strings.TrimSpace(req.StreamID) == "" {
		return nil, &statusError{400, "CPA 流请求缺少 stream_id"}
	}
	cred, err := credential(req.StorageJSON, req.AuthMetadata)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(rootCtx, chatTimeout)
	token := str(cred, "access_token")
	sessionID, err := createChatSession(ctx, token)
	if err != nil {
		cancel()
		return nil, err
	}
	body["conversation_id"] = sessionID
	resp, err := doUpstream(ctx, http.MethodPost, "/api/ai/chat?type=0", token, body)
	if err != nil {
		cancel()
		return nil, err
	}
	if err = checkChatHTTP(resp); err != nil {
		cancel()
		return nil, err
	}
	id, created := completionID(), time.Now().Unix()
	if !stream {
		defer cancel()
		defer resp.Body.Close()
		var content, reasoning strings.Builder
		err = scanAntiFraudSSE(ctx, resp.Body, func(d chatDelta) error {
			content.WriteString(d.Content)
			reasoning.WriteString(d.Reasoning)
			return nil
		})
		if err != nil {
			return nil, err
		}
		message := map[string]any{"role": "assistant", "content": content.String()}
		if reasoning.Len() > 0 {
			message["reasoning_content"] = reasoning.String()
		}
		raw, err := json.Marshal(map[string]any{"id": id, "object": "chat.completion", "created": created, "model": model, "choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": "stop"}}})
		if err != nil {
			return nil, err
		}
		return success(api.ExecutorResponse{Payload: raw, Headers: http.Header{"Content-Type": []string{"application/json"}}})
	}
	// The RPC acknowledges only after upstream HTTP success. Actual payload is
	// delivered incrementally through CPA's native stream bridge.
	response, err := success(map[string]any{"headers": http.Header{"Content-Type": []string{"text/event-stream"}, "Cache-Control": []string{"no-cache"}}})
	if err != nil {
		cancel()
		resp.Body.Close()
		return nil, err
	}
	jobs.Add(1)
	go func() {
		defer jobs.Done()
		defer cancel()
		defer resp.Body.Close()
		emit := func(payload []byte) error {
			if _, err := hostRPC("host.stream.emit", streamEmitRequest{req.StreamID, req.HostCallbackID, payload}); err != nil {
				cancel()
				resp.Body.Close()
				return &statusError{502, "CPA 流已关闭或消费端已断开"}
			}
			return nil
		}
		var streamErr error
		defer func() {
			message := ""
			if streamErr != nil {
				message = streamErr.Error()
			}
			_, _ = hostRPC("host.stream.close", streamCloseRequest{req.StreamID, req.HostCallbackID, message})
		}()
		if streamErr = emit(streamChunk(id, model, created, map[string]any{"role": "assistant"}, nil)); streamErr != nil {
			return
		}
		streamErr = scanAntiFraudSSE(ctx, resp.Body, func(d chatDelta) error {
			delta := map[string]any{}
			if d.Content != "" {
				delta["content"] = d.Content
			}
			if d.Reasoning != "" {
				delta["reasoning_content"] = d.Reasoning
			}
			return emit(streamChunk(id, model, created, delta, nil))
		})
		if streamErr != nil {
			return
		}
		if streamErr = emit(streamChunk(id, model, created, map[string]any{}, "stop")); streamErr != nil {
			return
		}
		// CPA adds [DONE] when its executor stream closes after finish_reason.
	}()
	return response, nil
}
