package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	api "github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func mockResponse(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func setUpstream(t *testing.T, f func(context.Context, string, string, string, any) (*http.Response, error)) {
	t.Helper()
	old := doUpstream
	doUpstream = f
	t.Cleanup(func() { doUpstream = old })
}
func resultOf(t *testing.T, b []byte) json.RawMessage {
	t.Helper()
	var e envelope
	if json.Unmarshal(b, &e) != nil || !e.OK {
		t.Fatalf("RPC failure %s", b)
	}
	return e.Result
}

func TestLegacyCredentialParsePreservesAllFields(t *testing.T) {
	raw := []byte(`{"type":"fanzha","access_token":"mock-access","refresh_token":"mock-refresh","user_id":"old-id","email":"old-label","proxy_url":"direct","disabled":false,"expired":"2027-01-01T00:00:00Z","custom":true}`)
	b, _ := json.Marshal(api.AuthParseRequest{FileName: "fanzha-old.json", RawJSON: raw})
	got, err := parseAuth(b)
	if err != nil {
		t.Fatal(err)
	}
	var parsed api.AuthParseResponse
	if err = json.Unmarshal(resultOf(t, got), &parsed); err != nil {
		t.Fatal(err)
	}
	if !parsed.Handled || parsed.Auth.ID != "fanzha-old.json" || parsed.Auth.ProxyURL != "direct" || parsed.Auth.Provider != "fanzha" || parsed.Auth.Metadata["custom"] != true {
		t.Fatalf("incompatible parsed auth %+v", parsed.Auth)
	}
	if parsed.Auth.Metadata["access_token"] != "mock-access" {
		t.Fatal("token changed")
	}
}
func TestOtherProvidersIgnored(t *testing.T) {
	b, _ := json.Marshal(api.AuthParseRequest{RawJSON: []byte(`{"type":"workbuddy"}`)})
	got, e := parseAuth(b)
	if e != nil {
		t.Fatal(e)
	}
	var r api.AuthParseResponse
	json.Unmarshal(resultOf(t, got), &r)
	if r.Handled {
		t.Fatal("parsed another provider")
	}
}
func TestPasswordMatchesPublicHelper(t *testing.T) {
	got, e := encodePassword("abc")
	if e != nil || got != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatal("incorrect SHA256")
	}
}
func TestQuotaRealMetricsNoInventedReset(t *testing.T) {
	setUpstream(t, func(_ context.Context, method, path, token string, _ any) (*http.Response, error) {
		if method != "GET" || path != "/api/v1/conversations/usage" || token != "mock" {
			t.Fatal("incorrect quota request")
		}
		return mockResponse(`{"code":200,"data":{"used":3,"remaining":17,"limit":20,"banned":false}}`), nil
	})
	q, e := fetchQuota(map[string]any{"access_token": "mock"})
	if e != nil {
		t.Fatal(e)
	}
	if len(q.Summary) != 3 || q.Summary[1].Value != 17 || q.Groups[0].Buckets[0].RemainingFraction != .85 || q.Groups[0].Buckets[0].ResetTime != "" {
		t.Fatalf("incorrect normalized quota %+v", q)
	}
}
func TestQuotaMissingMetricsFails(t *testing.T) {
	setUpstream(t, func(context.Context, string, string, string, any) (*http.Response, error) {
		return mockResponse(`{"code":200,"data":{"used":0}}`), nil
	})
	_, e := fetchQuota(map[string]any{"access_token": "mock"})
	if e == nil {
		t.Fatal("fabricated metrics")
	}
}
func TestRefreshBearerAndPreserveMetadata(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"exp":1893456000}`))
	token := "e30." + payload + ".signature"
	setUpstream(t, func(_ context.Context, method, path, bearer string, body any) (*http.Response, error) {
		if method != "POST" || path != "/api/v1/user/token/refresh" || bearer != "mock-refresh" || body != nil {
			t.Fatal("incorrect refresh protocol")
		}
		return mockResponse(`{"code":200,"data":{"access_token":"` + token + `","refresh_token":"new-refresh"}}`), nil
	})
	r, _ := json.Marshal(api.AuthRefreshRequest{AuthID: "old.json", StorageJSON: []byte(`{"type":"fanzha","access_token":"expired","refresh_token":"mock-refresh","custom":"keep"}`)})
	got, e := refreshAuth(r)
	if e != nil {
		t.Fatal(e)
	}
	var response api.AuthRefreshResponse
	json.Unmarshal(resultOf(t, got), &response)
	if response.Auth.Metadata["custom"] != "keep" || response.Auth.Metadata["refresh_token"] != "new-refresh" || response.Auth.ID != "old.json" {
		t.Fatal("refresh lost metadata")
	}
}
func TestValidTokenNotRotated(t *testing.T) {
	called := false
	setUpstream(t, func(context.Context, string, string, string, any) (*http.Response, error) {
		called = true
		return nil, nil
	})
	raw, _ := json.Marshal(map[string]any{"type": "fanzha", "access_token": "valid", "expired": time.Now().Add(time.Hour).Format(time.RFC3339)})
	r, _ := json.Marshal(api.AuthRefreshRequest{AuthID: "old.json", StorageJSON: raw})
	_, e := refreshAuth(r)
	if e != nil || called {
		t.Fatal("unnecessary token rotation")
	}
}
func TestAccountDataDoesNotLeakSecrets(t *testing.T) {
	old := hostRPC
	hostRPC = func(method string, _ any) (json.RawMessage, error) {
		if method != "host.auth.list" {
			t.Fatal("wrong host method")
		}
		return json.RawMessage(`{"files":[{"auth_index":"1234567890123456","provider":"fanzha","name":"fz.json","label":"a"},{"provider":"workbuddy","name":"wb.json"}]}`), nil
	}
	defer func() { hostRPC = old }()
	v, e := accounts()
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(v)
	if strings.Contains(string(b), "wb.json") || strings.Contains(string(b), "token") || strings.Contains(string(b), "path") {
		t.Fatal("unexpected account output")
	}
}
func TestPublicResourcesNeverCallHost(t *testing.T) {
	old := hostRPC
	hostRPC = func(string, any) (json.RawMessage, error) {
		t.Fatal("public resource accessed credentials")
		return nil, nil
	}
	defer func() { hostRPC = old }()
	for _, page := range []string{"login", "quota"} {
		b, _ := json.Marshal(api.ManagementRequest{Method: "GET", Path: "/v0/resource/plugins/fanzha/" + page})
		got, e := management(b)
		if e != nil {
			t.Fatal(e)
		}
		var r api.ManagementResponse
		json.Unmarshal(resultOf(t, got), &r)
		if r.StatusCode != 200 || !strings.Contains(string(r.Body), "<!doctype html>") {
			t.Fatal("resource not served")
		}
	}
}
func TestLoginMockDoesNotPersistPassword(t *testing.T) {
	setUpstream(t, func(_ context.Context, method, path, token string, body any) (*http.Response, error) {
		m := body.(map[string]any)
		if method != "POST" || path != "/api/v1/user/email/login" || token != "" || m["password"] == "abcdef" || m["email"] != "a@example.invalid" {
			t.Fatal("incorrect login request")
		}
		return mockResponse(`{"code":200,"data":{"access_token":"mock-access","refresh_token":"mock-refresh","user_id":"mock-user"}}`), nil
	})
	old := hostRPC
	hostRPC = func(method string, value any) (json.RawMessage, error) {
		if method != "host.auth.save" {
			t.Fatal("unexpected host call")
		}
		raw := value.(api.HostAuthSaveRequest).JSON
		if strings.Contains(string(raw), "password") || strings.Contains(string(raw), "abcdef") {
			t.Fatal("password persisted")
		}
		return json.RawMessage(`{"name":"saved"}`), nil
	}
	defer func() { hostRPC = old }()
	_, e := managementLogin([]byte(`{"account":"a@example.invalid","password":"abcdef","kind":"email"}`))
	if e != nil {
		t.Fatal(e)
	}
}
