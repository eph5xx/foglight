// Package linear exposes Foglight's Linear connector — the set of MCP
// tools that read from Linear on behalf of an AI agent.
package linear

import (
	"context"
	"errors"

	"github.com/eph5xx/foglight/pkg/connectors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const tokenEnv = "LINEAR_API_KEY"

// Connector implements connectors.Connector for Linear. State is
// populated by Configure and consumed by HealthCheck and Register.
type Connector struct {
	client *Client
}

var _ connectors.Connector = (*Connector)(nil)

// New returns an unconfigured Linear connector. Call Configure before
// HealthCheck or Register.
func New() *Connector { return &Connector{} }

func (c *Connector) Name() string { return "linear" }

func (c *Connector) Configure(env connectors.Environ) error {
	apiKey := env(tokenEnv)
	if apiKey == "" {
		return connectors.ErrDisabled
	}
	c.client = newClient(apiKey)
	return nil
}

// HealthCheck issues a tiny `viewer { id }` query — the cheapest probe
// that exercises auth. Linear rejects bad keys with HTTP 400/401, which
// surfaces here as an error.
func (c *Connector) HealthCheck(ctx context.Context) error {
	if c.client == nil {
		return errors.New("linear connector: not configured")
	}
	var out struct {
		Viewer struct {
			ID string `json:"id"`
		} `json:"viewer"`
	}
	return c.client.do(ctx, "query { viewer { id } }", nil, &out)
}

func (c *Connector) Register(server *mcp.Server) error {
	if c.client == nil {
		return errors.New("linear connector: not configured")
	}
	addIssueTools(server, c.client)
	addProjectTools(server, c.client)
	addOrgTools(server, c.client)
	addCommentTools(server, c.client)
	addTaxonomyTools(server, c.client)
	addCycleTools(server, c.client)
	addDocumentTools(server, c.client)
	return nil
}
