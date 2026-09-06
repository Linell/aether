package host

import (
	"slices"
	"testing"
)

func TestDaemonEnvPassesProviderKeys(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk")
	t.Setenv("AETHER_MODEL", "scripted")
	if env := BaseEnv(); slices.Contains(env, "OPENAI_API_KEY=sk") {
		t.Error("BaseEnv leaked OPENAI_API_KEY")
	}
	env := DaemonEnv("http://a", "tok")
	for _, want := range []string{"OPENAI_API_KEY=sk", "AETHER_MODEL=scripted", "AETHER_URL=http://a", "AETHER_TOKEN=tok"} {
		if !slices.Contains(env, want) {
			t.Errorf("DaemonEnv missing %s", want)
		}
	}
}
