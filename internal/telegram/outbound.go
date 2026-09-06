package telegram

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/linell/aether/internal/contract"
	"github.com/linell/aether/internal/store"
)

const Kind = "telegram"

type Outbound struct {
	Store  *store.Store
	Client *Client
}

func (o *Outbound) SendApproval(ctx context.Context, eventID string, p contract.ApprovalRequestedPayload) (int, error) {
	pending, err := o.pendingCalls(ctx, p)
	if err != nil {
		return 0, err
	}
	return o.fanOut(ctx, p.Thread, func(chat int64) error {
		return o.sendPending(ctx, eventID, chat, pending)
	})
}

func (o *Outbound) SendReply(ctx context.Context, eventID string, p contract.MessageRepliedPayload) (int, error) {
	return o.fanOut(ctx, p.Thread, func(chat int64) error {
		return o.sendOnce(ctx, eventID+":"+strconv.FormatInt(chat, 10), Message{ChatID: chat, Text: RenderReply(p.Text)}, nil)
	})
}

func (o *Outbound) fanOut(ctx context.Context, thread string, send func(chat int64) error) (int, error) {
	chats, err := o.Chats(ctx, thread)
	if err != nil {
		return 0, err
	}
	var errs []error
	for _, chat := range chats {
		errs = append(errs, send(chat))
	}
	return len(chats), errors.Join(errs...)
}

func (o *Outbound) Chats(ctx context.Context, thread string) ([]int64, error) {
	return store.Query(ctx, o.Store.DB(), scanChat,
		`SELECT external_id FROM channels WHERE kind = ? AND thread_id = ?`, Kind, thread)
}

func scanChat(rows *sql.Rows) (int64, error) {
	var raw string
	if err := rows.Scan(&raw); err != nil {
		return 0, err
	}
	return strconv.ParseInt(raw, 10, 64)
}

func (o *Outbound) sendPending(ctx context.Context, eventID string, chat int64, pending []PendingCall) error {
	var errs []error
	for _, p := range pending {
		text, kb := RenderApproval(p)
		key := fmt.Sprintf("%s:%d:%s", eventID, chat, p.Call.ID)
		errs = append(errs, o.sendOnce(ctx, key, Message{ChatID: chat, Text: text}, kb))
	}
	return errors.Join(errs...)
}

func (o *Outbound) sendOnce(ctx context.Context, key string, m Message, kb *Keyboard) error {
	claimed, err := o.claim(ctx, key)
	if err != nil || !claimed {
		return err
	}
	if _, err := o.Client.SendMessage(ctx, m, kb); err != nil {
		return errors.Join(err, o.release(ctx, key))
	}
	return nil
}

func (o *Outbound) claim(ctx context.Context, key string) (bool, error) {
	claimed := false
	err := o.Store.Tx(ctx, func(tx *store.Tx) error {
		out, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO operations (id, kind) VALUES (?, 'telegram.send')`, opID(key))
		if err != nil {
			return err
		}
		n, err := out.RowsAffected()
		claimed = n == 1
		return err
	})
	return claimed, err
}

func (o *Outbound) release(ctx context.Context, key string) error {
	_, err := o.Store.DB().ExecContext(ctx, `DELETE FROM operations WHERE id = ?`, opID(key))
	return err
}

func opID(key string) string { return "telegram:send:" + key }

func (o *Outbound) pendingCalls(ctx context.Context, p contract.ApprovalRequestedPayload) ([]PendingCall, error) {
	out := make([]PendingCall, 0, len(p.Calls))
	for _, call := range p.Calls {
		pc := PendingCall{Daemon: p.Daemon, Call: call}
		err := o.Store.DB().QueryRowContext(ctx,
			`SELECT id, status FROM approvals WHERE call_id = ?`, call.ID).Scan(&pc.Approval, &pc.Status)
		if err != nil {
			return nil, fmt.Errorf("telegram: approval for call %s: %w", call.ID, err)
		}
		out = append(out, pc)
	}
	return out, nil
}

func (o *Outbound) Pending(ctx context.Context, approval string) (PendingCall, error) {
	var pc PendingCall
	var args, callCtx string
	err := o.Store.DB().QueryRowContext(ctx,
		`SELECT a.id, a.status, d.name, a.call_id, a.tool, a.args, a.context
		 FROM approvals a JOIN threads t ON t.id = a.thread_id JOIN daemons d ON d.id = t.daemon_id WHERE a.id = ?`,
		approval).Scan(&pc.Approval, &pc.Status, &pc.Daemon, &pc.Call.ID, &pc.Call.Tool, &args, &callCtx)
	pc.Call.Args, pc.Call.Context = json.RawMessage(args), json.RawMessage(callCtx)
	return pc, err
}
