package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
)

const DefaultAPIURL = "https://api.telegram.org"

type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

type Config struct {
	APIURL        string
	Token         string
	WebhookSecret string
}

func ConfigFromEnv() Config {
	c := Config{
		APIURL:        os.Getenv("TELEGRAM_API_URL"),
		Token:         os.Getenv("TELEGRAM_BOT_TOKEN"),
		WebhookSecret: os.Getenv("TELEGRAM_WEBHOOK_SECRET"),
	}
	if c.APIURL == "" {
		c.APIURL = DefaultAPIURL
	}
	return c
}

func (c Config) Enabled() bool { return c.Token != "" }

func NewClient(cfg Config) *Client {
	return &Client{BaseURL: cfg.APIURL, Token: cfg.Token}
}

type Button struct {
	Text string `json:"text"`
	Data string `json:"callback_data"`
}

type Keyboard struct {
	Rows [][]Button `json:"inline_keyboard"`
}

type Message struct {
	ChatID int64  `json:"chat_id"`
	Text   string `json:"text"`
}

type sendMessage struct {
	Message
	ReplyMarkup *Keyboard `json:"reply_markup,omitempty"`
}

type Sent struct {
	MessageID int64 `json:"message_id"`
}

func (c *Client) SendMessage(ctx context.Context, m Message, kb *Keyboard) (Sent, error) {
	var sent Sent
	err := c.call(ctx, "sendMessage", sendMessage{Message: m, ReplyMarkup: kb}, &sent)
	return sent, err
}

func (c *Client) AnswerCallback(ctx context.Context, id, text string) error {
	return c.call(ctx, "answerCallbackQuery", map[string]string{"callback_query_id": id, "text": text}, nil)
}

func (c *Client) EditText(ctx context.Context, chatID, messageID int64, text string) error {
	body := map[string]any{"chat_id": chatID, "message_id": messageID, "text": text}
	return c.call(ctx, "editMessageText", body, nil)
}

func (c *Client) call(ctx context.Context, method string, in, out any) error {
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(in); err != nil {
		return fmt.Errorf("telegram: encode %s: %w", method, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/bot"+c.Token+"/"+method, &body)
	if err != nil {
		return fmt.Errorf("telegram: build %s: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("telegram: %s: %w", method, err)
	}
	defer resp.Body.Close()
	return decodeResponse(method, resp.Body, out)
}

func decodeResponse(method string, r io.Reader, out any) error {
	var env struct {
		OK          bool            `json:"ok"`
		Description string          `json:"description"`
		Result      json.RawMessage `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(r, 1<<20)).Decode(&env); err != nil {
		return fmt.Errorf("telegram: decode %s: %w", method, err)
	}
	if !env.OK {
		return fmt.Errorf("telegram: %s: %w", method, errors.New(env.Description))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(env.Result, out)
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}
