package codex

import (
	"strings"
	"testing"
)

func TestProfileTOMLArgvEncoding(t *testing.T) {
	v, e := tomlValue(map[string]any{"relay": map[string]any{"name": "quote \" / 中文", "env_key": "OPENAI_API_KEY", "base_url": "https://example.com/v1", "requires_openai_auth": false}})
	if e != nil || !strings.Contains(v, `"env_key"="OPENAI_API_KEY"`) || !strings.Contains(v, `"requires_openai_auth"=false`) {
		t.Fatalf("bad TOML encoding: %s %v", v, e)
	}
	if _, e = tomlValue(nil); e == nil {
		t.Fatal("accepted null")
	}
}
