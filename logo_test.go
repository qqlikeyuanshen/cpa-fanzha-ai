package main

import (
	"bytes"
	"encoding/json"
	api "github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
	"testing"
)

func TestOriginalLogoAvailableWithoutAuth(t *testing.T) {
	old := hostRPC
	hostRPC = func(string, any) (json.RawMessage, error) { t.Fatal("logo must not read credentials"); return nil, nil }
	defer func() { hostRPC = old }()
	b, _ := json.Marshal(api.ManagementRequest{Method: "GET", Path: "/v0/resource/plugins/fanzha/logo.png"})
	got, e := management(b)
	if e != nil {
		t.Fatal(e)
	}
	var r api.ManagementResponse
	json.Unmarshal(resultOf(t, got), &r)
	if r.StatusCode != 200 || r.Headers.Get("Content-Type") != "image/png" || !bytes.HasPrefix(r.Body, []byte{137, 80, 78, 71, 13, 10, 26, 10}) {
		t.Fatal("missing PNG")
	}
	meta := registration().(map[string]any)["metadata"].(api.Metadata)
	if meta.Logo != "/v0/resource/plugins/fanzha/logo.png" {
		t.Fatal("metadata missing same-origin logo")
	}
}
