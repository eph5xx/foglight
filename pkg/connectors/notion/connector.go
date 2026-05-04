// Package notion exposes Foglight's Notion connector — read-only MCP tools
// over Notion's public REST API (Notion-Version 2026-03-11).
//
// The surface targets parity with the hosted Notion MCP's read tools that
// are reachable on an integration token, and exceeds it where the hosted
// MCP has known weaknesses:
//
//   - notion_fetch — polymorphic page/database/data_source/block fetch,
//     returning enhanced markdown plus a structured refs[] sidecar.
//   - notion_query_data_source — filtered/paginated row query, with
//     single-source database URLs auto-resolving and multi-source ones
//     surfacing each data source as a collection:// reference.
//   - notion_search — title-substring search across pages and data sources.
//   - notion_get_comments — un-resolved comments anchored at a page or block.
//   - notion_get_block_children — single-level subtree fetch with raw
//     type-specific payloads.
//   - notion_get_page_property — full value of a single property; required
//     for relations/rollups with more than 25 entries.
//   - notion_list_users / notion_get_user / notion_get_self.
//
// Out of scope (intentionally; document in README):
//
//   - All writes (create/update/move/duplicate/delete pages, databases,
//     data sources, comments, views).
//   - Connected-sources search (Slack, Drive, Jira) — Notion AI only.
//   - Multi-source AI database queries — Enterprise + Notion AI.
//   - View-based queries — internal view API.
//   - Resolved comments — REST returns un-resolved only.
//   - Teamspace listing — no public REST endpoint.
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
	addFetchTools(server, name, c.client)
	addQueryTools(server, name, c.client)
	addSearchTools(server, name, c.client)
	addCommentTools(server, name, c.client)
	addBlockTools(server, name, c.client)
	addPropertyTools(server, name, c.client)
	addUserTools(server, name, c.client)
	return nil
}
