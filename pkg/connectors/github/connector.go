// Package github exposes Foglight's GitHub connector — the set of MCP
// tools that read from GitHub on behalf of an AI agent.
package github

import (
	"context"
	"errors"

	"github.com/eph5xx/foglight/pkg/connectors"
	gh "github.com/google/go-github/v82/github"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const tokenEnv = "GITHUB_TOKEN"

// Connector implements connectors.Connector for GitHub. State is
// populated by Configure and consumed by HealthCheck and Register.
type Connector struct {
	client *gh.Client
}

var _ connectors.Connector = (*Connector)(nil)

// New returns an unconfigured GitHub connector. Call Configure before
// HealthCheck or Register.
func New() *Connector { return &Connector{} }

func (c *Connector) Name() string { return "github" }

func (c *Connector) Configure(env connectors.Environ) error {
	token := env(tokenEnv)
	if token == "" {
		return connectors.ErrDisabled
	}
	c.client = gh.NewClient(nil).WithAuthToken(token)
	return nil
}

// HealthCheck calls Users.Get with an empty login, which the GitHub API
// resolves to the authenticated user — a cheap probe that fails fast on
// a bad token.
func (c *Connector) HealthCheck(ctx context.Context) error {
	if c.client == nil {
		return errors.New("github connector: not configured")
	}
	_, _, err := c.client.Users.Get(ctx, "")
	return err
}

func (c *Connector) Register(server *mcp.Server) error {
	if c.client == nil {
		return errors.New("github connector: not configured")
	}
	name := c.Name()
	addActionsTools(server, name, c.client)
	addContextTools(server, name, c.client)
	addPullRequestTools(server, name, c.client)
	addReposTools(server, name, c.client)
	return nil
}
