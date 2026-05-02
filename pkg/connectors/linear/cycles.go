package linear

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func addCycleTools(server *mcp.Server, name string, client *Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        name + "_list_cycles",
		Description: "List cycles (sprints). team_id scopes to a single team; type filters to current|previous|next.",
	}, listCycles(client))
}

// ---------- linear_list_cycles ----------

type cycleSummary struct {
	ID          string   `json:"id"`
	Name        string   `json:"name,omitempty"`
	Number      int      `json:"number"`
	Description string   `json:"description,omitempty"`
	StartsAt    string   `json:"startsAt,omitempty"`
	EndsAt      string   `json:"endsAt,omitempty"`
	CompletedAt string   `json:"completedAt,omitempty"`
	Progress    float64  `json:"progress,omitempty"`
	Team        *teamRef `json:"team,omitempty"`
}

type ListCyclesInput struct {
	TeamID string `json:"team_id,omitempty" jsonschema:"team UUID; if absent, returns cycles across all teams"`
	Type   string `json:"type,omitempty" jsonschema:"current|previous|next — filters to a single cycle relative to today"`
	Limit  int    `json:"limit,omitempty" jsonschema:"1-100 (default 30)"`
	Cursor string `json:"cursor,omitempty" jsonschema:"opaque cursor"`
}

type ListCyclesOutput struct {
	Cycles     []cycleSummary `json:"cycles"`
	NextCursor string         `json:"nextCursor,omitempty"`
	HasMore    bool           `json:"hasMore"`
}

const listCyclesQuery = `
query ListCycles($first: Int!, $after: String, $filter: CycleFilter) {
	cycles(first: $first, after: $after, filter: $filter) {
		nodes {
			id
			name
			number
			description
			startsAt
			endsAt
			completedAt
			progress
			team { id key name }
		}
		pageInfo { endCursor hasNextPage }
	}
}`

func listCycles(client *Client) mcp.ToolHandlerFor[ListCyclesInput, ListCyclesOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ListCyclesInput) (*mcp.CallToolResult, ListCyclesOutput, error) {
		filter := map[string]any{}
		if in.TeamID != "" {
			filter["team"] = map[string]any{"id": eqFilter(in.TeamID)}
		}
		switch in.Type {
		case "current":
			filter["isActive"] = eqFilter(true)
		case "previous":
			filter["isPrevious"] = eqFilter(true)
		case "next":
			filter["isNext"] = eqFilter(true)
		case "":
			// no relative-time filter
		default:
			return nil, ListCyclesOutput{}, fmt.Errorf("linear: invalid cycle type %q (want current|previous|next)", in.Type)
		}

		extra := map[string]any{}
		if len(filter) > 0 {
			extra["filter"] = filter
		}

		var resp struct {
			Cycles struct {
				Nodes    []cycleSummary `json:"nodes"`
				PageInfo PageInfo       `json:"pageInfo"`
			} `json:"cycles"`
		}
		if err := client.do(ctx, listCyclesQuery, pageVars(in.Limit, in.Cursor, extra), &resp); err != nil {
			return nil, ListCyclesOutput{}, fmt.Errorf("linear: list cycles: %w", err)
		}
		out := ListCyclesOutput{
			Cycles:     resp.Cycles.Nodes,
			NextCursor: resp.Cycles.PageInfo.EndCursor,
			HasMore:    resp.Cycles.PageInfo.HasNextPage,
		}
		if out.Cycles == nil {
			out.Cycles = []cycleSummary{}
		}
		return nil, out, nil
	}
}
