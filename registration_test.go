package main

import (
	"encoding/json"
	"testing"

	api "github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func TestOpenSourceMetadata(t *testing.T) {
	meta := registration().(map[string]any)["metadata"].(api.Metadata)
	if meta.GitHubRepository != "https://github.com/qqlikeyuanshen/cpa-fanzha-ai" || meta.Author != "成实外初一杨lingling" {
		t.Fatal("incorrect public repository attribution")
	}
	if meta.Version != "0.2.5-rewrite" || meta.Name != "反诈 AI" {
		t.Fatal("metadata differs from the original version")
	}
	if meta.Logo != "/v0/resource/plugins/fanzha/logo.png" {
		t.Fatal("logo must match the original same-origin asset")
	}
}

func TestConfigRejectsUnsafeUpstream(t *testing.T) {
	before := cfg()
	t.Cleanup(func() {
		config.Lock()
		config.value = before
		config.Unlock()
	})
	for _, upstream := range []string{
		"http://example.invalid",
		"https://user:pass@example.invalid",
		"https://example.invalid?token=mock",
		"https://example.invalid#fragment",
		"//example.invalid",
	} {
		raw, err := json.Marshal(map[string]any{"config_yaml": []byte("upstream_url: " + upstream + "\n")})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = dispatch("plugin.reconfigure", raw); err == nil {
			t.Fatalf("unsafe upstream configuration accepted: %s", upstream)
		}
	}
	if cfg() != before {
		t.Fatal("invalid configuration changed active settings")
	}
}
