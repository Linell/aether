package api

import (
	"fmt"
	"strings"
)

func validateModel(spec string) error {
	if spec == "" || spec == "scripted" {
		return nil
	}
	provider, model, ok := strings.Cut(spec, ":")
	if ok && model != "" && (provider == "openai" || provider == "anthropic") {
		return nil
	}
	return fmt.Errorf("invalid model: %q", spec)
}
