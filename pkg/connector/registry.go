package connector

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
)

type Registry interface {
	AddTool(t Tool)
}

type Tool struct {
	Name         string
	Description  string
	InputSchema  *jsonschema.Schema
	OutputSchema *jsonschema.Schema
	Handler      func(ctx context.Context, input json.RawMessage) (json.RawMessage, error)
}

func RegisterTool[In, Out any](
	r Registry, name, desc string,
	fn func(context.Context, In) (Out, error),
) error {
	in, err := jsonschema.For[In](nil)
	if err != nil {
		return fmt.Errorf("input schema for %s: %w", name, err)
	}
	out, err := jsonschema.For[Out](nil)
	if err != nil {
		return fmt.Errorf("output schema for %s: %w", name, err)
	}

	handler := func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
		var v In
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
		result, err := fn(ctx, v)
		if err != nil {
			return nil, err
		}
		return json.Marshal(result)
	}
	r.AddTool(Tool{
		Name:         name,
		Description:  desc,
		InputSchema:  in,
		OutputSchema: out,
		Handler:      handler,
	})
	return nil
}
