package contract

import (
	"encoding/json"
	"time"
)

const (
	EventMessageSent       = "aether/message.sent"
	EventScheduleFired     = "aether/schedule.fired"
	EventApprovalAnswered  = "aether/approval.answered"
	EventMessageReplied    = "daemon/message.replied"
	EventApprovalRequested = "daemon/approval.requested"
	EventConjureRequested  = "daemon/conjure.requested"
	EventDeployRequested   = "daemon/deploy.requested"
	EventDismissRequested  = "daemon/dismiss.requested"
	EventChangeRequested   = "daemon/change.requested"
)

const (
	TurnConcurrencyKey   = "event.data.thread"
	TurnConcurrencyLimit = 1
)

type Call struct {
	ID      string          `json:"id"`
	Tool    string          `json:"tool"`
	Args    json.RawMessage `json:"args"`
	Context json.RawMessage `json:"context"`
}

type MessageSentPayload struct {
	Daemon  string `json:"daemon"`
	Thread  string `json:"thread"`
	Message string `json:"message"`
	Text    string `json:"text"`
}

type ScheduleFiredPayload struct {
	Daemon     string    `json:"daemon"`
	Thread     string    `json:"thread"`
	Schedule   string    `json:"schedule"`
	DueAt      time.Time `json:"due_at"`
	DeadlineAt time.Time `json:"deadline_at"`
}

type ApprovalAnsweredPayload struct {
	Daemon   string `json:"daemon"`
	Thread   string `json:"thread"`
	Call     string `json:"call"`
	Approval string `json:"approval"`
	Decision string `json:"decision"`
}

type MessageRepliedPayload struct {
	Daemon string `json:"daemon"`
	Thread string `json:"thread"`
	Reply  string `json:"reply"`
	Text   string `json:"text"`
}

type ApprovalRequestedPayload struct {
	Daemon string `json:"daemon"`
	Thread string `json:"thread"`
	Calls  []Call `json:"calls"`
}

type ConjureRequestedPayload struct {
	Daemon   string `json:"daemon"`
	Host     string `json:"host"`
	Language string `json:"language"`
}

type DeployRequestedPayload struct {
	Daemon string `json:"daemon"`
	Host   string `json:"host"`
}

type DismissRequestedPayload struct {
	Daemon string `json:"daemon"`
	Host   string `json:"host"`
}

type ChangeRequestedPayload struct {
	Daemon string   `json:"daemon"`
	Patch  string   `json:"patch"`
	Tests  []string `json:"tests"`
}

var Payloads = map[string]any{
	EventMessageSent:       MessageSentPayload{},
	EventScheduleFired:     ScheduleFiredPayload{},
	EventApprovalAnswered:  ApprovalAnsweredPayload{},
	EventMessageReplied:    MessageRepliedPayload{},
	EventApprovalRequested: ApprovalRequestedPayload{},
	EventConjureRequested:  ConjureRequestedPayload{},
	EventDeployRequested:   DeployRequestedPayload{},
	EventDismissRequested:  DismissRequestedPayload{},
	EventChangeRequested:   ChangeRequestedPayload{},
}
