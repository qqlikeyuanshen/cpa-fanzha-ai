package main

import (
	"encoding/json"
	api "github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
	"testing"
)

func TestRouteMustBindProviderAuth(t *testing.T) {
	b, _ := json.Marshal(api.ModelRouteRequest{RequestedModel: "fanzha-ai", AvailableProviders: []string{"fanzha"}})
	raw, e := dispatch("model.route", b)
	if e != nil {
		t.Fatal(e)
	}
	var r api.ModelRouteResponse
	json.Unmarshal(resultOf(t, raw), &r)
	if !r.Handled || r.TargetKind != "provider" || r.Target != "fanzha" {
		t.Fatal("route bypasses credential selection")
	}
	reg := registration().(map[string]any)["capabilities"].(map[string]any)
	if reg["executor_model_scope"] != "oauth" {
		t.Fatal("auth-dependent executor permits static calls")
	}
}
