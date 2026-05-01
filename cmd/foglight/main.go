package main

import (
	"context"
	"log"

	"github.com/eph5xx/foglight/pkg/connectors/github"
	"github.com/eph5xx/foglight/pkg/connectors/linear"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	serverName    = "foglight"
	serverVersion = "v0.1.0"
)

func main() {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    serverName,
		Version: serverVersion,
	}, nil)

	if err := github.Register(server); err != nil {
		log.Fatalf("foglight: %v", err)
	}

	if err := linear.Register(server); err != nil {
		log.Fatalf("foglight: %v", err)
	}

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("foglight: %v", err)
	}
}
