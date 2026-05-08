package dummy

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

func (*Connector) Name() string                          { return "dummy" }
func (*Connector) Auth(ctx context.Context) error        { return nil }
func (*Connector) HealthCheck(ctx context.Context) error { return nil }

type AddInput struct {
	A float64 `json:"a" jsonschema:"first addend"`
	B float64 `json:"b" jsonschema:"second addend"`
}

type AddOutput struct {
	Sum float64 `json:"sum" jsonschema:"sum of a and b"`
}

func (c *Connector) Register(r connector.Registry) error {
	return connector.RegisterTool(r, "add", "Add two numbers", c.add)
}

func (*Connector) add(_ context.Context, in AddInput) (AddOutput, error) {
	return AddOutput{Sum: in.A + in.B}, nil
}
