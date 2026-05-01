// Package linear exposes Foglight's Linear connector — the set of MCP
// tools that read from Linear on behalf of an AI agent.
package linear

import (
	"errors"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const tokenEnv = "LINEAR_API_KEY"

// Register adds every Linear tool to server. It returns an error if the
// connector cannot be configured (e.g. missing credentials), so the
// process can fail loudly at startup rather than on the first tool call.
func Register(server *mcp.Server) error {
	apiKey := os.Getenv(tokenEnv)
	if apiKey == "" {
		return errors.New("linear connector: " + tokenEnv + " is not set")
	}

	client := newClient(apiKey)

	addIssueTools(server, client)
	addProjectTools(server, client)
	addOrgTools(server, client)
	addCommentTools(server, client)
	addTaxonomyTools(server, client)
	addCycleTools(server, client)
	addDocumentTools(server, client)

	return nil
}
