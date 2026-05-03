// Package notion exposes Foglight's Notion connector — a read-only set of
// MCP tools for finding and reading pages from a Notion workspace.
//
// Scope is intentionally narrow: search + page read + user lookup. Writes,
// database structured queries, file uploads, view CRUD, custom emojis, and
// webhook subscription management are out of scope. Notion's docs/specs are
// the artifact this connector surfaces; everything else can land later.
package notion

import (
	"context"
	"errors"
	"net/http"

	"github.com/eph5xx/foglight/pkg/connectors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const tokenEnv = "NOTION_API_KEY"

// Connector implements connectors.Connector for Notion. State is populated
// by Configure and consumed by HealthCheck and Register.
type Connector struct {
	client *Client
}

var _ connectors.Connector = (*Connector)(nil)

// New returns an unconfigured Notion connector. Call Configure before
// HealthCheck or Register.
func New() *Connector { return &Connector{} }

func (c *Connector) Name() string { return "notion" }

func (c *Connector) Configure(env connectors.Environ) error {
	apiKey := env(tokenEnv)
	if apiKey == "" {
		return connectors.ErrDisabled
	}
	c.client = newClient(apiKey)
	return nil
}

// HealthCheck issues GET /v1/users/me — the cheapest auth-touching call.
// Notion rejects bad tokens with 401, which surfaces here as an error.
func (c *Connector) HealthCheck(ctx context.Context) error {
	if c.client == nil {
		return errors.New("notion connector: not configured")
	}
	var out struct {
		Object string `json:"object"`
		ID     string `json:"id"`
	}
	return c.client.do(ctx, http.MethodGet, "/users/me", nil, &out)
}

func (c *Connector) Register(server *mcp.Server) error {
	if c.client == nil {
		return errors.New("notion connector: not configured")
	}
	name := c.Name()
	addSearchTools(server, name, c.client)
	addPageTools(server, name, c.client)
	addUserTools(server, name, c.client)
	return nil
}
