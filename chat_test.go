package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	api "github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func chatRequest(streamID string) []byte {
	raw, _ := json.Marshal(executorRPCRequest{ExecutorRequest: api.ExecutorRequest{Model: "fanzha-ai", Format: "openai", Payload: []byte(`{"model":"fanzha-ai","messages":[{"role":"user","content":"如何防诈骗？"}]}`), StorageJSON: []byte(`{"type":"fanzha","access_token":"mock-token"}`)}, StreamID: streamID})
	return raw
}
func TestFourAnswerModes(t *testing.T) {
	for model, want := range map[string]string{"fanzha-ai": "normal", "fanzha-ai-deep": "detailed", "fanzha-ai-en": "english", "fanzha-ai-en-deep": "english_detailed"} {
		got, e := answerMode(model)
		if e != nil || got != want {
			t.Fatal(model)
		}
	}
}
func TestChatRejectsUnsupportedContentBeforeUpstream(t *testing.T) {
	for _, payload := range []string{`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.invalid"}}]}]}`, `{"messages":[{"role":"user","content":"test"}],"tools":[{"type":"function"}]}`} {
		_, _, err := prepareChat(executorRPCRequest{ExecutorRequest: api.ExecutorRequest{Model: "fanzha-ai", Payload: []byte(payload)}})
		if err == nil || statusOf(err) != 400 {
			t.Fatal("unsupported input accepted")
		}
	}
}
func TestSSEMultiLineCRLFAndReasoning(t *testing.T) {
	s := ": ping\r\ndata: {\r\ndata: \"code\":200,\"data\":{\"type\":\"thinking\",\"answer\":\"思考\"}}\r\n\r\ndata: {\"code\":200,\"data\":{\"type\":\"answer\",\"answer\":\"警惕\"}}\r\n\r\ndata: {\"code\":200,\"data\":{\"exec_elapsed_secs\":1.2}}\r\n\r\n"
	var content, reasoning strings.Builder
	err := scanAntiFraudSSE(context.Background(), strings.NewReader(s), func(d chatDelta) error {
		content.WriteString(d.Content)
		reasoning.WriteString(d.Reasoning)
		return nil
	})
	if err != nil || content.String() != "警惕" || reasoning.String() != "思考" {
		t.Fatalf("invalid SSE parse %v", err)
	}
}
func TestSSEStringElapsedIsValidCompletion(t *testing.T) {
	err := scanAntiFraudSSE(context.Background(), strings.NewReader("data: {\"code\":200,\"data\":{\"type\":\"answer\",\"answer\":\"answer\",\"exec_elapsed_secs\":\"1.25\"}}\n\n"), func(chatDelta) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
}
func TestSSETruncatedIsNotSuccess(t *testing.T) {
	err := scanAntiFraudSSE(context.Background(), strings.NewReader("data: {\"code\":200,\"data\":{\"type\":\"answer\",\"answer\":\"partial\"}}\n\n"), func(chatDelta) error { return nil })
	if err == nil {
		t.Fatal("truncated answer accepted")
	}
}
func TestSSEBusinessCodePreserved(t *testing.T) {
	err := scanAntiFraudSSE(context.Background(), strings.NewReader("data: {\"code\":429,\"data\":{}}\n\n"), func(chatDelta) error { return nil })
	if err == nil || statusOf(err) != 429 {
		t.Fatal("quota denial not propagated")
	}
}
func mockChatUpstream(t *testing.T) {
	setUpstream(t, func(_ context.Context, method, path, token string, body any) (*http.Response, error) {
		if method != "POST" || token != "mock-token" {
			t.Fatal("invalid chat auth")
		}
		if path == "/api/ai/create_session" {
			return mockResponse(`{"code":200,"data":{"code":"10000","data":"mock-conversation"}}`), nil
		}
		if path != "/api/ai/chat?type=0" {
			t.Fatal("wrong chat URL")
		}
		m := body.(map[string]any)
		if m["conversation_id"] != "mock-conversation" || m["answer_mode"] != "normal" || m["using_context"] != false {
			t.Fatal("incorrect upstream body")
		}
		return mockResponse("data: {\"code\":200,\"data\":{\"type\":\"answer\",\"answer\":\"先核实\"}}\n\ndata: {\"code\":200,\"data\":{\"type\":\"answer\",\"answer\":\"身份\",\"exec_elapsed_secs\":1}}\n\n"), nil
	})
}
func TestChatNonstreamOpenAICompatibility(t *testing.T) {
	mockChatUpstream(t)
	raw, err := execute(chatRequest(""), false)
	if err != nil {
		t.Fatal(err)
	}
	var response api.ExecutorResponse
	json.Unmarshal(resultOf(t, raw), &response)
	var payload map[string]any
	if json.Unmarshal(response.Payload, &payload) != nil {
		t.Fatal("invalid JSON response")
	}
	if payload["object"] != "chat.completion" || payload["usage"] != nil || !strings.Contains(string(response.Payload), "先核实身份") {
		t.Fatal("invalid response or fabricated usage")
	}
}
func TestChatNativeStreamBridge(t *testing.T) {
	mockChatUpstream(t)
	old := hostRPC
	frames := []string{}
	closed := make(chan string, 1)
	hostRPC = func(method string, v any) (json.RawMessage, error) {
		switch method {
		case "host.stream.emit":
			r := v.(streamEmitRequest)
			if r.StreamID != "mock-stream" {
				t.Error("wrong stream id")
			}
			frames = append(frames, string(r.Payload))
		case "host.stream.close":
			closed <- v.(streamCloseRequest).Error
		default:
			t.Error("unknown callback")
		}
		return json.RawMessage(`{}`), nil
	}
	defer func() { hostRPC = old }()
	_, err := execute(chatRequest("mock-stream"), true)
	if err != nil {
		t.Fatal(err)
	}
	jobs.Wait()
	if message := <-closed; message != "" {
		t.Fatal(message)
	}
	all := strings.Join(frames, "")
	if len(frames) < 4 || !strings.Contains(all, "先核实") || !strings.Contains(all, "finish_reason\":\"stop") {
		t.Fatal("stream not genuinely incremental")
	}
	for _, f := range frames {
		if !json.Valid([]byte(f)) || strings.HasPrefix(f, "data:") {
			t.Fatal("executor must emit bare JSON, CPA supplies SSE framing")
		}
	}
}
