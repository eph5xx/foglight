package main

import (
	"context"
	"errors"
	"log"
	"os"
	"time"

	"github.com/eph5xx/foglight/pkg/connectors"
	"github.com/eph5xx/foglight/pkg/connectors/github"
	"github.com/eph5xx/foglight/pkg/connectors/linear"
	"github.com/eph5xx/foglight/pkg/connectors/slack"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	serverName    = "foglight"
	serverVersion = "v0.1.0"

	// healthCheckTimeout caps each connector's startup auth probe so a
	// stuck network can't deadlock the binary launching.
	healthCheckTimeout = 10 * time.Second
)

func main() {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    serverName,
		Version: serverVersion,
	}, nil)

	list := []connectors.Connector{
		github.New(),
		linear.New(),
		slack.New(),
	}

	env := connectors.Environ(os.Getenv)
	ctx := context.Background()

	for _, c := range list {
		if err := c.Configure(env); err != nil {
			if errors.Is(err, connectors.ErrDisabled) {
				log.Printf("foglight: %s: skipped (not configured)", c.Name())
				continue
			}
			log.Fatalf("foglight: %s: configure: %v", c.Name(), err)
		}

		probeCtx, cancel := context.WithTimeout(ctx, healthCheckTimeout)
		err := c.HealthCheck(probeCtx)
		cancel()
		if err != nil {
			log.Fatalf("foglight: %s: health check: %v", c.Name(), err)
		}

		if err := c.Register(server); err != nil {
			log.Fatalf("foglight: %s: register: %v", c.Name(), err)
		}
		log.Printf("foglight: %s: registered", c.Name())
	}

	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
		log.Fatalf("foglight: %v", err)
	}
}
