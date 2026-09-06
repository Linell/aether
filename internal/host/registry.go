package host

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type Daemon struct {
	Name string `json:"name"`
}

type Registry interface {
	Daemons(ctx context.Context) ([]Daemon, error)
}

type Client struct {
	BaseURL string
	Token   string
	Host    string
	HTTP    *http.Client
}

var _ Registry = (*Client)(nil)

func (c *Client) Register(ctx context.Context) error {
	return c.call(ctx, http.MethodPost, "/v1/hosts", map[string]string{"name": c.Host}, nil)
}

func (c *Client) Daemons(ctx context.Context) ([]Daemon, error) {
	var daemons []Daemon
	err := c.call(ctx, http.MethodGet, "/v1/hosts/"+c.Host+"/daemons", nil, &daemons)
	return daemons, err
}

func (c *Client) call(ctx context.Context, method, path string, in, out any) error {
	req, err := c.newRequest(ctx, method, path, in)
	if err != nil {
		return err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("host: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("host: %s %s: unexpected status %d", method, path, resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("host: %s %s: decode: %w", method, path, err)
	}
	return nil
}

func (c *Client) newRequest(ctx context.Context, method, path string, in any) (*http.Request, error) {
	var body bytes.Buffer
	if in != nil {
		if err := json.NewEncoder(&body).Encode(in); err != nil {
			return nil, fmt.Errorf("host: encode %s %s: %w", method, path, err)
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, &body)
	if err != nil {
		return nil, fmt.Errorf("host: build %s %s: %w", method, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}
