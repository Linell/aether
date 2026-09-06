package host

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *Client) newRequest(ctx context.Context, method, path string, body []byte) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return nil, fmt.Errorf("host: build %s %s: %w", method, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("host: %s %s: %w", req.Method, req.URL, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		return nil, fmt.Errorf("host: %s %s: unexpected status %d", req.Method, req.URL, resp.StatusCode)
	}
	return resp, nil
}

func (c *Client) Register(ctx context.Context) error {
	body, err := json.Marshal(map[string]string{"name": c.Host})
	if err != nil {
		return fmt.Errorf("host: marshal register body: %w", err)
	}
	req, err := c.newRequest(ctx, http.MethodPost, "/v1/hosts", body)
	if err != nil {
		return err
	}
	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (c *Client) Daemons(ctx context.Context) ([]Daemon, error) {
	path := fmt.Sprintf("/v1/hosts/%s/daemons", c.Host)
	req, err := c.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var daemons []Daemon
	if err := json.NewDecoder(resp.Body).Decode(&daemons); err != nil {
		return nil, fmt.Errorf("host: decode daemons: %w", err)
	}
	return daemons, nil
}
