package slack

import (
	"context"

	"github.com/eph5xx/foglight/pkg/connector"
	"gopkg.in/yaml.v3"
)

type Config struct{}

func (Config) Validate() error { return nil }

type Connector struct {
	cfg Config
}

func NewFromYAML(node yaml.Node) (*Connector, error) {
	cfg, err := connector.DecodeConfig[Config](node)
	if err != nil {
		return nil, err
	}
	return &Connector{cfg: cfg}, nil
}

func (*Connector) Name() string                          { return "slack" }
func (*Connector) Auth(ctx context.Context) error        { return nil }
func (*Connector) HealthCheck(ctx context.Context) error { return nil }

type PingInput struct{}

type PingOutput struct {
	Status  string `json:"status" jsonschema:"liveness status, always \"ok\""`
	Service string `json:"service" jsonschema:"name of the service"`
}

func (c *Connector) Register(r connector.Registry) error {
	return connector.RegisterTool(r, "slack_ping", "Ping the Slack stub connector", c.ping)
}

func (c *Connector) ping(_ context.Context, _ PingInput) (PingOutput, error) {
	return PingOutput{Status: "ok", Service: c.Name()}, nil
}
