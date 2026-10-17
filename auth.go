package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	api "github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

type loginSession struct {
	expires time.Time
	auth    *api.AuthData
}

var sessions = struct {
	sync.Mutex
	items map[string]*loginSession
}{items: map[string]*loginSession{}}

func authData(name string, m map[string]any) api.AuthData {
	raw, _ := json.Marshal(m)
	label := str(m, "label")
	if label == "" {
		label = str(m, "email")
	}
	if label == "" {
		label = str(m, "phone")
	}
	if label == "" {
		label = "反诈 AI"
	}
	return api.AuthData{Provider: "fanzha", ID: name, FileName: name, Label: label, ProxyURL: str(m, "proxy_url"), Disabled: m["disabled"] == true, StorageJSON: raw, Metadata: m}
}
func parseAuth(b []byte) ([]byte, error) {
	var r api.AuthParseRequest
	if err := decode(b, &r); err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(r.RawJSON, &m); err != nil {
		return success(api.AuthParseResponse{})
	}
	if str(m, "type") != "fanzha" {
		return success(api.AuthParseResponse{})
	}
	if _, err := credential(r.RawJSON, nil); err != nil {
		return nil, err
	}
	name := r.FileName
	if name == "" {
		name = filepath.Base(r.Path)
	}
	return success(api.AuthParseResponse{Handled: true, Auth: authData(name, m)})
}
func startLogin(b []byte) ([]byte, error) {
	var r api.AuthLoginStartRequest
	if err := decode(b, &r); err != nil {
		return nil, err
	}
	base := cfg().PublicBase
	if base == "" {
		base = r.BaseURL
	}
	base, err := validPublicBase(base)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, 24)
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	state := hex.EncodeToString(nonce)
	expires := time.Now().Add(15 * time.Minute)
	sessions.Lock()
	defer sessions.Unlock()
	for k, v := range sessions.items {
		if time.Now().After(v.expires) {
			delete(sessions.items, k)
		}
	}
	if len(sessions.items) >= 64 {
		return nil, &statusError{429, "登录流程过多，请稍后重试"}
	}
	sessions.items[state] = &loginSession{expires: expires}
	return success(api.AuthLoginStartResponse{Provider: "fanzha", URL: base + "/v0/resource/plugins/fanzha/login?state=" + state, State: state, ExpiresAt: expires})
}
func pollLogin(b []byte) ([]byte, error) {
	var r api.AuthLoginPollRequest
	if err := decode(b, &r); err != nil {
		return nil, err
	}
	sessions.Lock()
	defer sessions.Unlock()
	s := sessions.items[r.State]
	if s == nil || time.Now().After(s.expires) {
		return success(api.AuthLoginPollResponse{Status: "error", Message: "登录流程已过期，请重新发起"})
	}
	if s.auth == nil {
		return success(api.AuthLoginPollResponse{Status: "pending", Message: "请在插件登录页完成登录"})
	}
	return success(api.AuthLoginPollResponse{Status: "success", Auth: *s.auth})
}
func refreshAuth(b []byte) ([]byte, error) {
	var r api.AuthRefreshRequest
	if err := decode(b, &r); err != nil {
		return nil, err
	}
	m, err := credential(r.StorageJSON, r.Metadata)
	if err != nil {
		return nil, err
	}
	expires := asTime(str(m, "expired"))
	// Do not rotate valid long-lived tokens every 5 minutes; retain existing tokens
	// until expiry is near. This avoids background token churn and auth races.
	if !expires.IsZero() && time.Until(expires) > 10*time.Minute {
		return success(api.AuthRefreshResponse{Auth: authData(r.AuthID, m), NextRefreshAfter: time.Now().Add(5 * time.Minute)})
	}
	token := str(m, "refresh_token")
	if token == "" {
		return nil, &statusError{401, "缺少刷新令牌，请重新登录"}
	}
	ctx, cancel := context.WithTimeout(rootCtx, 25*time.Second)
	defer cancel()
	resp, err := doUpstream(ctx, "POST", "/api/v1/user/token/refresh", token, nil)
	if err != nil {
		return nil, err
	}
	data, err := readAPI(resp)
	if err != nil {
		return nil, err
	}
	var update map[string]any
	if err = json.Unmarshal(data, &update); err != nil || str(update, "access_token") == "" {
		return nil, &statusError{502, "刷新响应缺少访问令牌"}
	}
	m["access_token"] = str(update, "access_token")
	if v := str(update, "refresh_token"); v != "" {
		m["refresh_token"] = v
	}
	m["expired"] = tokenExpiry(str(update, "access_token")).Format(time.RFC3339)
	m["last_refresh"] = time.Now().UTC().Format(time.RFC3339)
	return success(api.AuthRefreshResponse{Auth: authData(r.AuthID, m), NextRefreshAfter: time.Now().Add(5 * time.Minute)})
}
func tokenExpiry(token string) time.Time { return jwtExpiry(token) }

func managementLogin(raw []byte) (any, error) {
	var r struct {
		Account  string `json:"account"`
		Password string `json:"password"`
		Kind     string `json:"kind"`
		State    string `json:"state"`
	}
	if err := decode(raw, &r); err != nil {
		return nil, err
	}
	r.Account = strings.TrimSpace(r.Account)
	if r.Account == "" || len(r.Account) > 254 || len(r.Password) < 6 || len(r.Password) > 128 {
		return nil, &statusError{400, "请填写有效账号及密码"}
	}
	if r.Kind != "email" && r.Kind != "phone" {
		return nil, &statusError{400, "请选择邮箱或手机号登录"}
	}
	if r.State != "" {
		sessions.Lock()
		s := sessions.items[r.State]
		valid := s != nil && time.Now().Before(s.expires)
		sessions.Unlock()
		if !valid {
			return nil, &statusError{400, "登录流程已过期"}
		}
	}
	encoded, err := encodePassword(r.Password)
	if err != nil {
		return nil, err
	}
	path := "/api/v1/user/email/login"
	body := map[string]any{"email": r.Account, "password": encoded, "remember_me": false}
	if r.Kind == "phone" {
		path = "/api/v1/user/login"
		delete(body, "email")
		body["phone"] = r.Account
	}
	ctx, cancel := context.WithTimeout(rootCtx, 30*time.Second)
	defer cancel()
	resp, err := doUpstream(ctx, "POST", path, "", body)
	if err != nil {
		return nil, err
	}
	data, err := readAPI(resp)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err = json.Unmarshal(data, &m); err != nil || str(m, "access_token") == "" {
		return nil, &statusError{502, "登录响应缺少访问令牌"}
	}
	// Only persist provider-owned auth fields; neither password nor CPA key is saved.
	id := str(m, "user_id")
	if id == "" {
		id = str(m, "id")
	}
	if id == "" {
		id = r.Account
	}
	stable := sha256.Sum256([]byte(id))
	name := "fanzha-" + hex.EncodeToString(stable[:8]) + ".json"
	auth := map[string]any{"type": "fanzha", "access_token": str(m, "access_token"), "refresh_token": str(m, "refresh_token"), "user_id": id, "label": r.Account, "email": r.Account, "proxy_url": "direct", "disabled": false, "expired": tokenExpiry(str(m, "access_token")).Format(time.RFC3339), "last_refresh": time.Now().UTC().Format(time.RFC3339), "refresh_interval_seconds": 300}
	if r.Kind == "phone" {
		auth["phone"] = r.Account
		if len(r.Account) >= 7 {
			auth["label"] = r.Account[:3] + "******" + r.Account[len(r.Account)-2:]
			auth["email"] = auth["label"]
		}
	}
	if r.State != "" {
		a := authData(name, auth)
		sessions.Lock()
		s := sessions.items[r.State]
		if s != nil {
			s.auth = &a
		}
		sessions.Unlock()
	} else {
		saved, _ := json.Marshal(auth)
		if _, err = hostRPC("host.auth.save", api.HostAuthSaveRequest{Name: name, JSON: saved}); err != nil {
			return nil, fmt.Errorf("登录成功，但 CPA 保存账号失败：%w", err)
		}
	}
	return map[string]any{"ok": true, "name": name, "label": auth["label"], "message": "登录成功，账号已交给 CPA 保存"}, nil
}
