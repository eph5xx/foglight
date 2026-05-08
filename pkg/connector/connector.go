package connector

import "context"

type Connector interface {
	Name() string
	Auth(ctx context.Context) error
	HealthCheck(ctx context.Context) error
	Register(r Registry) error
}
