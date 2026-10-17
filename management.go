package main

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"strings"

	api "github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

//go:embed quota.html
var quotaPage []byte

//go:embed login.html
var loginPage []byte

//go:embed logo.png
var logoPNG []byte

func managementRegistration() any {
	return map[string]any{
		"resources": []api.ResourceRoute{
			{Path: "/quota", Menu: "反诈 AI · 额度", Description: "独立额度页：每账号24小时额度及剩余进度"},
			{Path: "/login", Menu: "反诈 AI · 登录", Description: "邮箱或手机密码登录现有反诈账号"},
			{Path: "/logo.png", Description: "反诈原版图标（内置同源资源）"},
		},
		"routes": []api.ManagementRoute{
			{Method: "GET", Path: "/fanzha/accounts"},
			{Method: "POST", Path: "/fanzha/quota"},
			{Method: "POST", Path: "/fanzha/login"},
		},
	}
}
func management(b []byte) ([]byte, error) {
	var r api.ManagementRequest
	if err := decode(b, &r); err != nil {
		return nil, err
	}
	// Resource routes return only static assets, never account tokens or user data.
	if r.Method == "GET" && r.Path == "/v0/resource/plugins/fanzha/logo.png" {
		return success(api.ManagementResponse{StatusCode: 200, Headers: http.Header{"Content-Type": []string{"image/png"}, "Cache-Control": []string{"public, max-age=86400"}, "X-Content-Type-Options": []string{"nosniff"}}, Body: logoPNG})
	}
	if r.Method == "GET" && r.Path == "/v0/resource/plugins/fanzha/quota" {
		return success(pageResponse(quotaPage))
	}
	if r.Method == "GET" && r.Path == "/v0/resource/plugins/fanzha/login" {
		return success(pageResponse(loginPage))
	}
	var value any
	var err error
	switch r.Method + " " + r.Path {
	case "GET /v0/management/fanzha/accounts":
		value, err = accounts()
	case "POST /v0/management/fanzha/quota":
		var q struct {
			Index string `json:"auth_index"`
		}
		if err = decode(r.Body, &q); err == nil {
			var m map[string]any
			m, err = getAccount(q.Index)
			if err == nil {
				value, err = fetchQuota(m)
			}
		}
	case "POST /v0/management/fanzha/login":
		value, err = managementLogin(r.Body)
	default:
		err = &statusError{404, "插件接口不存在"}
	}
	if err != nil {
		return success(jsonResponse(statusOf(err), map[string]string{"error": err.Error()}))
	}
	return success(jsonResponse(200, value))
}
func jsonResponse(status int, v any) api.ManagementResponse {
	b, _ := json.Marshal(v)
	return api.ManagementResponse{StatusCode: status, Headers: http.Header{"Content-Type": []string{"application/json; charset=utf-8"}, "Cache-Control": []string{"no-store"}}, Body: b}
}
func pageResponse(b []byte) api.ManagementResponse {
	return api.ManagementResponse{StatusCode: 200, Headers: http.Header{
		"Content-Type": []string{"text/html; charset=utf-8"}, "Cache-Control": []string{"no-store"}, "X-Content-Type-Options": []string{"nosniff"}, "Referrer-Policy": []string{"no-referrer"},
	}, Body: b}
}

func accounts() (any, error) {
	raw, err := hostRPC("host.auth.list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var r struct {
		Files []api.HostAuthFileEntry `json:"files"`
	}
	if err = json.Unmarshal(raw, &r); err != nil {
		return nil, &statusError{502, "无法解析 CPA 账号列表"}
	}
	out := []map[string]any{}
	for _, f := range r.Files {
		if f.Provider == "fanzha" || f.Type == "fanzha" {
			out = append(out, map[string]any{"auth_index": f.AuthIndex, "name": f.Name, "label": f.Label, "disabled": f.Disabled})
		}
	}
	return map[string]any{"accounts": out}, nil
}
func getAccount(index string) (map[string]any, error) {
	index = strings.TrimSpace(index)
	if len(index) != 16 || strings.ContainsAny(index, "/\\.") {
		return nil, &statusError{400, "无效 auth_index"}
	}
	raw, err := hostRPC("host.auth.get", api.HostAuthGetRequest{AuthIndex: index})
	if err != nil {
		return nil, err
	}
	var a api.HostAuthGetResponse
	if err = json.Unmarshal(raw, &a); err != nil {
		return nil, &statusError{502, "无法读取 CPA 账号"}
	}
	var m map[string]any
	if err = json.Unmarshal(a.JSON, &m); err != nil {
		return nil, &statusError{502, "账号格式无效"}
	}
	if str(m, "type") != "fanzha" {
		return nil, &statusError{400, "只能查询反诈账号"}
	}
	return credential(a.JSON, nil)
}
