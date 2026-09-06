package api

import "testing"

func TestValidateModel(t *testing.T) {
	for _, ok := range []string{"", "scripted", "openai:gpt-5", "anthropic:claude-sonnet-4-5"} {
		if err := validateModel(ok); err != nil {
			t.Errorf("validateModel(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"gpt-5", "openai:", "google:gemini", "scripted:x"} {
		if err := validateModel(bad); err == nil {
			t.Errorf("validateModel(%q) = nil, want error", bad)
		}
	}
}
