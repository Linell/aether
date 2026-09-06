package inngest

import (
	"context"
	"fmt"

	"github.com/inngest/inngestgo"
	"github.com/inngest/inngestgo/step"
	"github.com/linell/aether/internal/contract"
)

func HostAppID(host string) string { return "host-" + host }

func RegisterConjure(c *Client, host string, conjure func(ctx context.Context, daemon string) error) error {
	filter := fmt.Sprintf("event.data.host == %q", host)
	_, err := inngestgo.CreateFunction(c.inner,
		inngestgo.FunctionOpts{ID: "conjure", Name: "conjure", Retries: inngestgo.IntPtr(3)},
		inngestgo.EventTrigger(contract.EventConjureRequested, &filter),
		func(ctx context.Context, input inngestgo.Input[contract.ConjureRequestedPayload]) (any, error) {
			return step.Run(ctx, "scaffold", func(ctx context.Context) (string, error) {
				return input.Event.Data.Daemon, conjure(ctx, input.Event.Data.Daemon)
			})
		})
	if err != nil {
		return fmt.Errorf("inngest: register conjure: %w", err)
	}
	return nil
}
