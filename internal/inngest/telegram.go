package inngest

import (
	"context"
	"fmt"
	"log"

	"github.com/inngest/inngestgo"
	"github.com/inngest/inngestgo/step"
	"github.com/linell/aether/internal/contract"
	"github.com/linell/aether/internal/store"
	"github.com/linell/aether/internal/telegram"
)

const channelRetries = 3

func RegisterTelegram(c *Client, st *store.Store, out *telegram.Outbound) error {
	if err := registerChannel(c, "telegram-approval", contract.EventApprovalRequested,
		func(ctx context.Context, in inngestgo.Input[contract.ApprovalRequestedPayload]) (int, error) {
			return out.SendApproval(ctx, eventID(in.Event.ID, in.InputCtx.RunID), in.Event.Data)
		}, st); err != nil {
		return err
	}
	return registerChannel(c, "telegram-reply", contract.EventMessageReplied,
		func(ctx context.Context, in inngestgo.Input[contract.MessageRepliedPayload]) (int, error) {
			return out.SendReply(ctx, eventID(in.Event.ID, in.InputCtx.RunID), in.Event.Data)
		}, st)
}

type channelPayload interface {
	contract.ApprovalRequestedPayload | contract.MessageRepliedPayload
}

func registerChannel[P channelPayload](c *Client, id, event string, send func(context.Context, inngestgo.Input[P]) (int, error), st *store.Store) error {
	_, err := inngestgo.CreateFunction(c.inner,
		inngestgo.FunctionOpts{ID: id, Name: id, Retries: inngestgo.IntPtr(channelRetries)},
		inngestgo.EventTrigger(event, nil),
		func(ctx context.Context, in inngestgo.Input[P]) (any, error) {
			return step.Run(ctx, "send", func(ctx context.Context) (int, error) {
				n, err := send(ctx, in)
				if err != nil && in.InputCtx.Attempt >= channelRetries {
					markChannelFailed(ctx, st, id, in, err)
				}
				return n, err
			})
		})
	if err != nil {
		return fmt.Errorf("inngest: register %s: %w", id, err)
	}
	return nil
}

func eventID(id *string, fallback string) string {
	if id != nil && *id != "" {
		return *id
	}
	return fallback
}

func markChannelFailed[P channelPayload](ctx context.Context, st *store.Store, fn string, in inngestgo.Input[P], cause error) {
	daemon, thread := payloadTarget(in.Event.Data)
	id := eventID(in.Event.ID, in.InputCtx.RunID)
	detail := fmt.Sprintf(`{"function":%q,"error":%q}`, fn, cause.Error())
	err := st.Tx(ctx, func(tx *store.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO markers (id, kind, daemon_id, thread_id, ref_id, detail)
			 SELECT ?, 'channel.failed', id, ?, ?, ? FROM daemons WHERE name = ?`,
			id+":channel.failed", thread, id, detail, daemon)
		return err
	})
	if err != nil {
		log.Printf("inngest: mark channel.failed for %s: %v", id, err)
	}
}

func payloadTarget[P channelPayload](p P) (string, string) {
	switch v := any(p).(type) {
	case contract.ApprovalRequestedPayload:
		return v.Daemon, v.Thread
	case contract.MessageRepliedPayload:
		return v.Daemon, v.Thread
	}
	return "", ""
}
