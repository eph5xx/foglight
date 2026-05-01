// Package slack exposes Foglight's Slack connector — the set of MCP
// tools that read from Slack on behalf of an AI agent.
//
// Auth is browser-session (xoxc token + xoxd cookie). xoxp/xoxb are not
// supported in this version; if either is needed later, add a fallback
// in newClient and a token-prefix check here.
package slack

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	xoxcEnv = "SLACK_XOXC_TOKEN"
	xoxdEnv = "SLACK_XOXD_COOKIE"
	// authProbeTimeout caps the startup auth.test call so a stuck network
	// can't deadlock the binary launching.
	authProbeTimeout = 10 * time.Second
)

// Register adds every Slack tool to server. Returns an error if the
// connector cannot be configured (missing creds, dead session) so the
// process can fail loudly at startup rather than on the first tool call.
//
// We probe with auth.test because xoxc/xoxd is browser-session auth and
// the failure modes are silent: a stale cookie or a decoded xoxd value
// will pass our local validation and then 401 every tool call. Catching
// it here turns "first call returns invalid_auth" into "process exits
// with a clear error before MCP even hands out tool listings."
func Register(server *mcp.Server) error {
	xoxc := os.Getenv(xoxcEnv)
	if xoxc == "" {
		return errors.New("slack connector: " + xoxcEnv + " is not set")
	}
	xoxd := os.Getenv(xoxdEnv)
	if xoxd == "" {
		return errors.New("slack connector: " + xoxdEnv + " is not set")
	}

	client := newClient(xoxc, xoxd)

	probeCtx, cancel := context.WithTimeout(context.Background(), authProbeTimeout)
	defer cancel()
	if _, err := client.AuthTestContext(probeCtx); err != nil {
		return fmt.Errorf("slack connector: auth probe failed (check %s and %s): %w", xoxcEnv, xoxdEnv, err)
	}

	addChannelTools(server, client)
	addConversationTools(server, client)
	addSearchTools(server, client)
	addUserTools(server, client)
	addUsergroupTools(server, client)

	return nil
}
