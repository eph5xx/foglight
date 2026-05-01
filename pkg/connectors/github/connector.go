// Package github exposes Foglight's GitHub connector — the set of MCP
// tools that read from GitHub on behalf of an AI agent.
package github

import (
	"errors"
	"os"

	"github.com/google/go-github/v82/github"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const tokenEnv = "GITHUB_TOKEN"

// Register adds every GitHub tool to server. It returns an error if the
// connector cannot be configured (e.g. missing credentials), so the
// process can fail loudly at startup rather than on the first tool call.
func Register(server *mcp.Server) error {
	token := os.Getenv(tokenEnv)
	if token == "" {
		return errors.New("github connector: " + tokenEnv + " is not set")
	}

	client := github.NewClient(nil).WithAuthToken(token)

	addActionsTools(server, client)
	addContextTools(server, client)
	addPullRequestTools(server, client)
	addReposTools(server, client)

	return nil
}
