package telegram

import "strings"

const SecretHeader = "X-Telegram-Bot-Api-Secret-Token"

type Update struct {
	ID       int64          `json:"update_id"`
	Message  *IncomingText  `json:"message"`
	Callback *CallbackQuery `json:"callback_query"`
}

type Chat struct {
	ID int64 `json:"id"`
}

type IncomingText struct {
	MessageID int64  `json:"message_id"`
	Chat      Chat   `json:"chat"`
	Text      string `json:"text"`
}

type CallbackQuery struct {
	ID      string        `json:"id"`
	Data    string        `json:"data"`
	Message *IncomingText `json:"message"`
}

type Decision struct {
	Approval string
	Decision string
}

func ParseCallback(data string) (Decision, bool) {
	verb, approval, ok := strings.Cut(data, ":")
	if !ok || approval == "" {
		return Decision{}, false
	}
	switch verb {
	case "approve":
		return Decision{Approval: approval, Decision: "approved"}, true
	case "deny":
		return Decision{Approval: approval, Decision: "denied"}, true
	}
	return Decision{}, false
}

func StartCommand(text string) (string, bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(text), "/start")
	if !ok || (rest != "" && !strings.HasPrefix(rest, " ")) {
		return "", false
	}
	return strings.TrimSpace(rest), true
}
