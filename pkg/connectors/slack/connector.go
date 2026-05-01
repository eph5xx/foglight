// Package slack exposes Foglight's Slack connector — the set of MCP
// tools that read from Slack on behalf of an AI agent.
//
// Auth is browser-session (xoxc token + xoxd cookie). xoxp/xoxb are not
// supported in this version; if either is needed later, add a fallback
// in newClient and a token-prefix check in Configure.
package slack

import (
	"context"
	"errors"
	"fmt"

	"github.com/eph5xx/foglight/pkg/connectors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	slacksdk "github.com/slack-go/slack"
)

const (
	xoxcEnv = "SLACK_XOXC_TOKEN"
	xoxdEnv = "SLACK_XOXD_COOKIE"
)

// Connector implements connectors.Connector for Slack. State is
// populated by Configure and consumed by HealthCheck and Register.
type Connector struct {
	client *slacksdk.Client
}

var _ connectors.Connector = (*Connector)(nil)

// New returns an unconfigured Slack connector. Call Configure before
// HealthCheck or Register.
func New() *Connector { return &Connector{} }

func (c *Connector) Name() string { return "slack" }

func (c *Connector) Configure(env connectors.Environ) error {
	xoxc := env(xoxcEnv)
	xoxd := env(xoxdEnv)
	if xoxc == "" && xoxd == "" {
		return connectors.ErrDisabled
	}
	// Partial config: one var set, the other missing. This is almost
	// always "user forgot the second var," so flag it loudly instead of
	// silently skipping the connector.
	if xoxc == "" {
		return fmt.Errorf("%s is set but %s is missing", xoxdEnv, xoxcEnv)
	}
	if xoxd == "" {
		return fmt.Errorf("%s is set but %s is missing", xoxcEnv, xoxdEnv)
	}
	c.client = newClient(xoxc, xoxd)
	return nil
}

// HealthCheck probes auth.test because xoxc/xoxd is browser-session auth
// and the failure modes are silent: a stale cookie or a decoded xoxd
// value will pass local validation and then 401 every tool call. Probing
// at startup turns "first call returns invalid_auth" into "process exits
// with a clear error before MCP even hands out tool listings."
func (c *Connector) HealthCheck(ctx context.Context) error {
	if c.client == nil {
		return errors.New("slack connector: not configured")
	}
	if _, err := c.client.AuthTestContext(ctx); err != nil {
		return fmt.Errorf("auth probe failed (check %s and %s): %w", xoxcEnv, xoxdEnv, err)
	}
	return nil
}

func (c *Connector) Register(server *mcp.Server) error {
	if c.client == nil {
		return errors.New("slack connector: not configured")
	}
	addChannelTools(server, c.client)
	addConversationTools(server, c.client)
	addSearchTools(server, c.client)
	addUserTools(server, c.client)
	addUsergroupTools(server, c.client)
	return nil
}
