package inngest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/inngest/inngestgo"
	"github.com/linell/aether/internal/store"
)

const AppID = "aether"

var ErrMissingCredentials = errors.New("inngest: INNGEST_EVENT_KEY and INNGEST_SIGNING_KEY are required")

type Client struct {
	inner inngestgo.Client
}

type Options struct {
	AppID       string
	EventKey    string
	SigningKey  string
	EventAPIURL string
}

func OptionsFromEnv(appID string) Options {
	return Options{
		AppID:      appID,
		EventKey:   os.Getenv("INNGEST_EVENT_KEY"),
		SigningKey: os.Getenv("INNGEST_SIGNING_KEY"),
	}
}

func New(o Options) (*Client, error) {
	if o.EventKey == "" || o.SigningKey == "" {
		return nil, ErrMissingCredentials
	}
	inner, err := inngestgo.NewClient(clientOpts(o))
	if err != nil {
		return nil, fmt.Errorf("inngest: new client: %w", err)
	}
	return &Client{inner: inner}, nil
}

func clientOpts(o Options) inngestgo.ClientOpts {
	opts := inngestgo.ClientOpts{
		AppID:      o.AppID,
		EventKey:   &o.EventKey,
		SigningKey: &o.SigningKey,
	}
	if o.EventAPIURL != "" {
		opts.EventAPIBaseURL = &o.EventAPIURL
	}
	return opts
}

func (c *Client) Publish(ctx context.Context, id, name string, payload json.RawMessage) error {
	var data map[string]any
	if err := json.Unmarshal(payload, &data); err != nil {
		return fmt.Errorf("inngest: decode payload for %s: %w", id, err)
	}
	_, err := c.inner.Send(ctx, inngestgo.Event{ID: &id, Name: name, Data: data})
	if err != nil {
		return fmt.Errorf("inngest: send %s: %w", id, err)
	}
	return nil
}

var _ store.Publisher = (*Client)(nil)
