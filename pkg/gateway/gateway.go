package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime/debug"

	"github.com/eph5xx/foglight/pkg/connector"
	"github.com/eph5xx/foglight/pkg/services/dummy"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const serverName = "foglight"

type Gateway struct {
	server *mcp.Server
}

func New(name, version string) *Gateway {
	return &Gateway{
		server: mcp.NewServer(&mcp.Implementation{Name: name, Version: version}, nil),
	}
}

func (g *Gateway) AddTool(t connector.Tool) {
	handler := func(ctx context.Context, _ *mcp.CallToolRequest, in json.RawMessage) (*mcp.CallToolResult, json.RawMessage, error) {
		out, err := t.Handler(ctx, in)
		if err != nil {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
			}, nil, nil
		}
		return nil, out, nil
	}
	mcp.AddTool(g.server, &mcp.Tool{
		Name:         t.Name,
		Description:  t.Description,
		InputSchema:  t.InputSchema,
		OutputSchema: t.OutputSchema,
	}, handler)
}

func serverVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

func Run(ctx context.Context, cfg *Config) error {
	g := New(serverName, serverVersion())

	for _, e := range cfg.Connectors {
		c, err := newConnector(e)
		if err != nil {
			return fmt.Errorf("init connector %s: %w", e.Name, err)
		}
		if err := c.Register(g); err != nil {
			return fmt.Errorf("register %s: %w", e.Name, err)
		}
	}

	return g.server.Run(ctx, &mcp.StdioTransport{})
}

func newConnector(e ConnectorEntry) (connector.Connector, error) {
	switch e.Name {
	case "dummy":
		return dummy.NewFromYAML(e.Config)
	default:
		return nil, fmt.Errorf("unknown connector %q", e.Name)
	}
}
