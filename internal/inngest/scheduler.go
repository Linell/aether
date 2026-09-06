package inngest

import (
	"context"
	"fmt"
	"time"

	"github.com/inngest/inngestgo"
	"github.com/inngest/inngestgo/connect"
	"github.com/inngest/inngestgo/step"
	"github.com/linell/aether/internal/scheduler"
	"github.com/linell/aether/internal/store"
)

const TickCron = "* * * * *"

func RegisterScheduler(c *Client, st *store.Store) error {
	_, err := inngestgo.CreateFunction(c.inner,
		inngestgo.FunctionOpts{ID: "scheduler-tick", Name: "scheduler.tick", Retries: inngestgo.IntPtr(3)},
		inngestgo.CronTrigger(TickCron),
		func(ctx context.Context, _ inngestgo.Input[any]) (any, error) {
			return step.Run(ctx, "tick", func(ctx context.Context) (scheduler.Result, error) {
				return scheduler.Tick(ctx, st, time.Now())
			})
		})
	if err != nil {
		return fmt.Errorf("inngest: register scheduler.tick: %w", err)
	}
	return nil
}

func Connect(ctx context.Context, c *Client, instanceID string) (connect.WorkerConnection, error) {
	conn, err := inngestgo.Connect(ctx, inngestgo.ConnectOpts{
		Apps:       []inngestgo.Client{c.inner},
		InstanceID: &instanceID,
	})
	if err != nil {
		return nil, fmt.Errorf("inngest: connect: %w", err)
	}
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()
	return conn, nil
}
