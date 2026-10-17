package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	api "github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
	"gopkg.in/yaml.v3"
)

const version = "0.2.5-rewrite"

type envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}
type rpcError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	HTTPStatus int    `json:"http_status"`
}
type statusError struct {
	status  int
	message string
}

func (e *statusError) Error() string { return e.message }
func statusOf(err error) int {
	var e *statusError
	if errors.As(err, &e) && e.status >= 400 {
		return e.status
	}
	return 502
}
func failure(msg string, status int) []byte {
	b, _ := json.Marshal(envelope{Error: &rpcError{"fanzha_error", msg, status}})
	return b
}
func success(v any) ([]byte, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return nil, e
	}
	return json.Marshal(envelope{OK: true, Result: b})
}

var hostRPC = func(string, any) (json.RawMessage, error) { return nil, fmt.Errorf("未连接 CPA 主机") }

type settings struct {
	Upstream   string `yaml:"upstream_url"`
	PublicBase string `yaml:"public_base_url"`
	Direct     bool   `yaml:"direct_http"`
	Refresh    int    `yaml:"refresh_interval_seconds"`
}

var config = struct {
	sync.RWMutex
	value settings
}{value: settings{Upstream: "https://xzfzznt.gaj.sh.gov.cn", Direct: true, Refresh: 300}}

func cfg() settings { config.RLock(); defer config.RUnlock(); return config.value }

var rootCtx, rootCancel = context.WithCancel(context.Background())
var jobs sync.WaitGroup

func shutdown() { rootCancel(); jobs.Wait() }
func decode(b []byte, v any) error {
	if len(b) == 0 {
		return nil
	}
	if err := json.Unmarshal(b, v); err != nil {
		return &statusError{400, "无效 JSON 请求"}
	}
	return nil
}

func registration() any {
	return map[string]any{
		"schema_version": 6,
		"metadata": api.Metadata{Name: "反诈 AI", Version: version, Author: "成实外初一杨lingling", GitHubRepository: "https://github.com/qqlikeyuanshen/cpa-fanzha-ai", Logo: "/v0/resource/plugins/fanzha/logo.png", ConfigFields: []api.ConfigField{
			{Name: "upstream_url", Type: "string", Description: "反诈 AI 上游地址"},
			{Name: "public_base_url", Type: "string", Description: "CPA 公网 HTTPS 地址"},
			{Name: "direct_http", Type: "boolean", Description: "直连上游，默认开启，保持原账号网络策略"},
		}},
		"capabilities": map[string]any{"auth_provider": true, "model_registrar": true, "model_provider": true, "model_router": true, "executor": true, "executor_model_scope": "oauth", "executor_input_formats": []string{"chat-completions"}, "executor_output_formats": []string{"chat-completions"}, "quota_provider": true, "management_api": true},
	}
}

func dispatch(method string, b []byte) ([]byte, error) {
	switch method {
	case "plugin.register", "plugin.reconfigure":
		var r struct {
			Config []byte `json:"config_yaml"`
		}
		if err := decode(b, &r); err != nil {
			return nil, err
		}
		if len(r.Config) > 0 {
			c := cfg()
			if err := yaml.Unmarshal(r.Config, &c); err != nil {
				return nil, fmt.Errorf("插件配置 YAML 无效")
			}
			u, err := url.Parse(c.Upstream)
			if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
				return nil, fmt.Errorf("upstream_url 必须是有效 HTTPS 地址")
			}
			c.Upstream = strings.TrimRight(c.Upstream, "/")
			if c.Refresh < 60 {
				c.Refresh = 300
			}
			config.Lock()
			config.value = c
			config.Unlock()
		}
		return success(registration())
	case "plugin.quiesce":
		return success(map[string]any{})
	case "plugin.shutdown":
		shutdown()
		return success(map[string]any{})
	case "auth.identifier", "executor.identifier", "quota.identifier":
		return success(map[string]string{"identifier": "fanzha"})
	case "auth.parse":
		return parseAuth(b)
	case "auth.login.start":
		return startLogin(b)
	case "auth.login.poll":
		return pollLogin(b)
	case "auth.refresh":
		return refreshAuth(b)
	case "model.register", "model.static", "model.for_auth":
		return success(api.ModelResponse{Provider: "fanzha", Models: models()})
	case "model.route":
		var r api.ModelRouteRequest
		if err := decode(b, &r); err != nil {
			return nil, err
		}
		for _, m := range models() {
			if r.RequestedModel == m.ID {
				return success(api.ModelRouteResponse{Handled: true, TargetKind: "provider", Target: "fanzha", TargetModel: m.ID, Reason: "绑定反诈账号池执行"})
			}
		}
		return success(api.ModelRouteResponse{})
	case "executor.execute", "executor.execute_stream":
		return execute(b, method == "executor.execute_stream")
	case "executor.count_tokens":
		return nil, &statusError{501, "上游未提供准确 token 计数"}
	case "executor.http_request":
		return nil, &statusError{501, "不提供任意 HTTP 转发"}
	case "quota.describe":
		return success(api.QuotaDescribeResponse{SupportedProviders: []string{"fanzha"}, DisplayName: "反诈 AI · 24 小时额度"})
	case "quota.fetch":
		var r api.QuotaFetchRequest
		if err := decode(b, &r); err != nil {
			return nil, err
		}
		m, err := credential(r.StorageJSON, r.Metadata)
		if err != nil {
			return nil, err
		}
		q, err := fetchQuota(m)
		if err != nil {
			return nil, err
		}
		return success(q)
	case "quota.reset":
		return nil, &statusError{501, "上游不支持手动重置额度"}
	case "management.register":
		return success(managementRegistration())
	case "management.handle":
		return management(b)
	default:
		return nil, &statusError{501, "不支持的插件 RPC 方法"}
	}
}

func models() []api.ModelInfo {
	out := []api.ModelInfo{}
	for _, v := range []struct{ id, label string }{{"fanzha-ai", "反诈 AI"}, {"fanzha-ai-deep", "反诈 AI · 深度思考"}, {"fanzha-ai-en", "反诈 AI · 英文"}, {"fanzha-ai-en-deep", "反诈 AI · 英文深度思考"}} {
		out = append(out, api.ModelInfo{ID: v.id, Object: "model", OwnedBy: "fanzha", Type: "text", Name: v.id, DisplayName: v.label, MaxCompletionTokens: 2048, SupportedGenerationMethods: []string{"chat"}, UserDefined: true, SupportedInputModalities: []string{"text"}, SupportedOutputModalities: []string{"text"}})
	}
	return out
}

func credential(raw []byte, metadata map[string]any) (map[string]any, error) {
	m := map[string]any{}
	for k, v := range metadata {
		m[k] = v
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, &statusError{400, "凭证 JSON 无效"}
		}
	}
	if t := str(m, "type"); t != "" && t != "fanzha" {
		return nil, &statusError{400, "不是反诈账号"}
	}
	if str(m, "access_token") == "" {
		return nil, &statusError{401, "反诈账号缺少访问令牌，请重新登录"}
	}
	return m, nil
}
func str(m map[string]any, k string) string { v, _ := m[k].(string); return v }
func upstream(ctx context.Context, method, path string, m map[string]any, body any) (*http.Response, error) {
	return doUpstream(ctx, method, path, str(m, "access_token"), body)
}
func asTime(v string) time.Time { t, _ := time.Parse(time.RFC3339, v); return t }
