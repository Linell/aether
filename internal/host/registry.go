package host

import (
	"context"
	"net/http"

	"github.com/linell/aether/internal/rest"
)

type Daemon struct {
	Name string `json:"name"`
}

type Registry interface {
	Daemons(ctx context.Context) ([]Daemon, error)
}

type Client struct {
	rest.Client
	Host string
}

var _ Registry = (*Client)(nil)

func (c *Client) Register(ctx context.Context) error {
	return c.Do(ctx, http.MethodPost, "/v1/hosts", map[string]string{"name": c.Host}, nil)
}

func (c *Client) Daemons(ctx context.Context) ([]Daemon, error) {
	var daemons []Daemon
	err := c.Do(ctx, http.MethodGet, "/v1/hosts/"+c.Host+"/daemons", nil, &daemons)
	return daemons, err
}
