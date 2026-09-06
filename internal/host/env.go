package host

import (
	"os"

	"github.com/linell/aether/internal/policy"
)

var passthrough = []string{"INNGEST_EVENT_KEY", "INNGEST_SIGNING_KEY", "INNGEST_ENV", "INNGEST_DEV"}

func BaseEnv() []string {
	return scrubbed(policy.DefaultEnvAllow)
}

func DaemonEnv(aetherURL, token string) []string {
	env := scrubbed(append(append([]string{}, policy.DefaultEnvAllow...), passthrough...))
	return append(env, "AETHER_URL="+aetherURL, "AETHER_TOKEN="+token)
}

func scrubbed(allow []string) []string {
	return policy.ScrubEnv(os.Environ(), allow)
}
