package host

import (
	"os"

	"github.com/linell/aether/internal/policy"
)

var passthrough = []string{"INNGEST_EVENT_KEY", "INNGEST_SIGNING_KEY", "INNGEST_ENV", "INNGEST_DEV"}

func DaemonEnv(aetherURL, token string) []string {
	allow := append(append([]string{}, policy.DefaultEnvAllow...), passthrough...)
	env := policy.ScrubEnv(os.Environ(), allow)
	return append(env, "AETHER_URL="+aetherURL, "AETHER_TOKEN="+token)
}
