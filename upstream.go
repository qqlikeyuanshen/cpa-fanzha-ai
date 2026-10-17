package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	api "github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

var directTransport = &http.Transport{Proxy: nil, MaxIdleConns: 16, MaxIdleConnsPerHost: 4, IdleConnTimeout: 60 * time.Second, ResponseHeaderTimeout: 40 * time.Second}
var directClient = &http.Client{Transport: directTransport, Timeout: 180 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
var doUpstream = func(ctx context.Context, method, path, token string, body any) (*http.Response, error) {
	var raw []byte
	var err error
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}
	r, err := http.NewRequestWithContext(ctx, method, cfg().Upstream+path, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	r.Header.Set("Origin", cfg().Upstream)
	r.Header.Set("Referer", cfg().Upstream+"/")
	client := directClient
	if !cfg().Direct {
		// Honor only an explicit configured environment proxy when direct_http is disabled.
		tr := directTransport.Clone()
		tr.Proxy = http.ProxyFromEnvironment
		client = &http.Client{Transport: tr, Timeout: 180 * time.Second, CheckRedirect: directClient.CheckRedirect}
		defer tr.CloseIdleConnections()
	}
	resp, err := client.Do(r)
	if err != nil {
		return nil, &statusError{502, "反诈上游连接失败"}
	}
	if resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, &statusError{resp.StatusCode, "反诈上游返回 HTTP " + fmt.Sprint(resp.StatusCode)}
	}
	return resp, nil
}

func readAPI(resp *http.Response) (json.RawMessage, error) {
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, &statusError{502, "读取反诈上游失败"}
	}
	var d struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err = json.Unmarshal(b, &d); err != nil {
		return nil, &statusError{502, "反诈上游未返回有效 JSON"}
	}
	if d.Code != 0 && d.Code != 200 {
		status := 502
		if d.Code == 401 || d.Code == 403 || d.Code == 429 {
			status = d.Code
		}
		return nil, &statusError{status, "反诈上游业务请求被拒绝"}
	}
	if len(d.Data) == 0 || string(d.Data) == "null" {
		return nil, &statusError{502, "反诈上游响应缺少数据"}
	}
	return d.Data, nil
}
func fetchQuota(m map[string]any) (api.QuotaFetchResponse, error) {
	ctx, cancel := context.WithTimeout(rootCtx, 25*time.Second)
	defer cancel()
	resp, err := upstream(ctx, "GET", "/api/v1/conversations/usage", m, nil)
	if err != nil {
		return api.QuotaFetchResponse{}, err
	}
	raw, err := readAPI(resp)
	if err != nil {
		return api.QuotaFetchResponse{}, err
	}
	var usage struct {
		Used      *int   `json:"used"`
		Remaining *int   `json:"remaining"`
		Limit     *int   `json:"limit"`
		Banned    bool   `json:"banned"`
		Exceeded  bool   `json:"exceeded"`
		Reset     string `json:"reset_time"`
	}
	if err = json.Unmarshal(raw, &usage); err != nil || usage.Used == nil || usage.Remaining == nil || usage.Limit == nil || *usage.Limit <= 0 || *usage.Used < 0 || *usage.Remaining < 0 {
		return api.QuotaFetchResponse{}, &statusError{502, "上游额度字段无效，未伪造或填充额度"}
	}
	fraction := float64(*usage.Remaining) / float64(*usage.Limit)
	if fraction > 1 {
		fraction = 1
	}
	desc := fmt.Sprintf("已用 %d / %d 条，剩余 %d 条", *usage.Used, *usage.Limit, *usage.Remaining)
	if usage.Banned {
		desc += " · 账号被上游限制"
	}
	if usage.Exceeded {
		desc += " · 已达限额"
	}
	reset := ""
	if !asTime(usage.Reset).IsZero() {
		reset = usage.Reset
	}
	return api.QuotaFetchResponse{Subscription: &api.QuotaSubscription{Plan: fmt.Sprintf("24 小时 · %d 条", *usage.Limit)}, Summary: []api.QuotaMetric{
		{Key: "used", Label: "已使用", Value: float64(*usage.Used), Unit: "条"},
		{Key: "remaining", Label: "剩余", Value: float64(*usage.Remaining), Unit: "条"},
		{Key: "limit", Label: "总额度", Value: float64(*usage.Limit), Unit: "条"},
	}, Groups: []api.QuotaGroup{{DisplayName: "24 小时额度", Buckets: []api.QuotaBucket{{Window: "24h", RemainingFraction: fraction, ResetTime: reset, Description: desc}}}}}, nil
}

func validPublicBase(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return "", &statusError{400, "请配置 public_base_url 为 CPA 的 HTTPS 地址"}
	}
	u.RawQuery = ""
	u.Fragment = ""
	u.Path = ""
	return u.String(), nil
}
