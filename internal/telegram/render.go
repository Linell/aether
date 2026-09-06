package telegram

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/linell/aether/internal/contract"
)

const TextLimit = 4096

type PendingCall struct {
	Approval string
	Status   string
	Daemon   string
	Call     contract.Call
}

func RenderApproval(p PendingCall) (string, *Keyboard) {
	text := fmt.Sprintf("%s asks to run:\n  %s\nin %s\napproval %s", p.Daemon, describe(p.Call), cwdOf(p.Call), p.Approval)
	if p.Status != "pending" {
		return Truncate(text + " (" + p.Status + ")"), nil
	}
	return Truncate(text), ApprovalKeyboard(p.Approval)
}

func ApprovalKeyboard(approval string) *Keyboard {
	return &Keyboard{Rows: [][]Button{{
		{Text: "Approve", Data: "approve:" + approval},
		{Text: "Deny", Data: "deny:" + approval},
	}}}
}

func RenderReply(text string) string { return Truncate(text) }

func describe(call contract.Call) string {
	var args struct {
		Argv []string `json:"argv"`
	}
	if json.Unmarshal(call.Args, &args) == nil && len(args.Argv) > 0 {
		return call.Tool + " " + strings.Join(args.Argv, " ")
	}
	return call.Tool + " " + string(call.Args)
}

func cwdOf(call contract.Call) string {
	var ctx struct {
		Cwd string `json:"cwd"`
	}
	_ = json.Unmarshal(call.Context, &ctx)
	return ctx.Cwd
}

func Truncate(text string) string {
	if len(text) <= TextLimit {
		return text
	}
	const suffix = "\n[truncated]"
	return strings.ToValidUTF8(text[:TextLimit-len(suffix)], "") + suffix
}
