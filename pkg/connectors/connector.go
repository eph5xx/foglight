// Package connectors defines the shared lifecycle contract for
// Foglight's MCP connectors.
package connectors

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Environ resolves environment-style configuration values. os.Getenv
// satisfies it directly; tests can substitute an in-memory map.
type Environ func(key string) string

// ErrDisabled signals from Configure that the connector has chosen to
// skip itself (typically because its credentials are unset). The host
// should log and move on rather than fail the whole binary.
var ErrDisabled = errors.New("connector disabled: required configuration not set")

// Connector is the lifecycle contract every Foglight connector implements.
//
// The host calls these in order: Configure, HealthCheck, Register. Any
// step may return an error to abort startup; Configure may additionally
// return ErrDisabled to opt out without failing.
type Connector interface {
	// Name returns a stable identifier used for logs and metrics.
	Name() string

	// Configure loads credentials and constructs the underlying client.
	// Return ErrDisabled (not a wrapped error) when required env vars
	// are absent. Return any other error for malformed or unusable
	// configuration.
	Configure(env Environ) error

	// HealthCheck probes the upstream to confirm credentials work. The
	// host applies its own timeout via ctx; implementations should not
	// add their own.
	HealthCheck(ctx context.Context) error

	// Register adds the connector's MCP tools to server. Called only
	// after Configure and HealthCheck have both succeeded.
	Register(server *mcp.Server) error
}
