package linear

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func addTaxonomyTools(server *mcp.Server, name string, client *Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        name + "_list_issue_statuses",
		Description: "List workflow states (issue statuses) with optional team_id and type filters. Type is one of: triage|backlog|unstarted|started|completed|canceled.",
	}, listIssueStatuses(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        name + "_get_issue_status",
		Description: "Get a single workflow state (issue status) by UUID.",
	}, getIssueStatus(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        name + "_list_issue_labels",
		Description: "List issue labels. Optional team_id scopes to a single team; absent returns all labels visible to the caller (including workspace-level labels).",
	}, listIssueLabels(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        name + "_list_project_labels",
		Description: "List project labels for the workspace.",
	}, listProjectLabels(client))
}

// ---------- linear_list_issue_statuses ----------

type workflowStateSummary struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Color       string   `json:"color,omitempty"`
	Position    float64  `json:"position,omitempty"`
	Description string   `json:"description,omitempty"`
	Team        *teamRef `json:"team,omitempty"`
}

type ListIssueStatusesInput struct {
	TeamID string `json:"team_id,omitempty" jsonschema:"team UUID; if absent, returns states across all teams"`
	Type   string `json:"type,omitempty" jsonschema:"triage|backlog|unstarted|started|completed|canceled"`
	Limit  int    `json:"limit,omitempty" jsonschema:"1-100 (default 30)"`
	Cursor string `json:"cursor,omitempty" jsonschema:"opaque cursor"`
}

type ListIssueStatusesOutput struct {
	Statuses   []workflowStateSummary `json:"statuses"`
	NextCursor string                 `json:"nextCursor,omitempty"`
	HasMore    bool                   `json:"hasMore"`
}

const listIssueStatusesQuery = `
query ListWorkflowStates($first: Int!, $after: String, $filter: WorkflowStateFilter) {
	workflowStates(first: $first, after: $after, filter: $filter) {
		nodes {
			id
			name
			type
			color
			position
			description
			team { id key name }
		}
		pageInfo { endCursor hasNextPage }
	}
}`

func listIssueStatuses(client *Client) mcp.ToolHandlerFor[ListIssueStatusesInput, ListIssueStatusesOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ListIssueStatusesInput) (*mcp.CallToolResult, ListIssueStatusesOutput, error) {
		filter := map[string]any{}
		if in.TeamID != "" {
			filter["team"] = map[string]any{"id": eqFilter(in.TeamID)}
		}
		if in.Type != "" {
			filter["type"] = eqFilter(in.Type)
		}
		extra := map[string]any{}
		if len(filter) > 0 {
			extra["filter"] = filter
		}

		var resp struct {
			WorkflowStates struct {
				Nodes    []workflowStateSummary `json:"nodes"`
				PageInfo PageInfo               `json:"pageInfo"`
			} `json:"workflowStates"`
		}
		if err := client.do(ctx, listIssueStatusesQuery, pageVars(in.Limit, in.Cursor, extra), &resp); err != nil {
			return nil, ListIssueStatusesOutput{}, fmt.Errorf("linear: list issue statuses: %w", err)
		}
		out := ListIssueStatusesOutput{
			Statuses:   resp.WorkflowStates.Nodes,
			NextCursor: resp.WorkflowStates.PageInfo.EndCursor,
			HasMore:    resp.WorkflowStates.PageInfo.HasNextPage,
		}
		if out.Statuses == nil {
			out.Statuses = []workflowStateSummary{}
		}
		return nil, out, nil
	}
}

// ---------- linear_get_issue_status ----------

type GetIssueStatusInput struct {
	ID string `json:"id" jsonschema:"workflow state UUID"`
}

const getIssueStatusQuery = `
query GetWorkflowState($id: String!) {
	workflowState(id: $id) {
		id
		name
		type
		color
		position
		description
		team { id key name }
	}
}`

func getIssueStatus(client *Client) mcp.ToolHandlerFor[GetIssueStatusInput, workflowStateSummary] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in GetIssueStatusInput) (*mcp.CallToolResult, workflowStateSummary, error) {
		id := strings.TrimSpace(in.ID)
		if id == "" {
			return nil, workflowStateSummary{}, fmt.Errorf("linear: id is required")
		}
		var resp struct {
			WorkflowState *workflowStateSummary `json:"workflowState"`
		}
		if err := client.do(ctx, getIssueStatusQuery, map[string]any{"id": id}, &resp); err != nil {
			return nil, workflowStateSummary{}, fmt.Errorf("linear: get issue status %q: %w", id, err)
		}
		if resp.WorkflowState == nil {
			return nil, workflowStateSummary{}, fmt.Errorf("linear: issue status %q not found", id)
		}
		return nil, *resp.WorkflowState, nil
	}
}

// ---------- linear_list_issue_labels ----------

type issueLabelSummary struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Color       string   `json:"color,omitempty"`
	Description string   `json:"description,omitempty"`
	Team        *teamRef `json:"team,omitempty"`
	IsGroup     bool     `json:"isGroup,omitempty"`
}

type ListIssueLabelsInput struct {
	TeamID string `json:"team_id,omitempty" jsonschema:"team UUID; if absent, returns workspace + all team labels"`
	Limit  int    `json:"limit,omitempty" jsonschema:"1-100 (default 30)"`
	Cursor string `json:"cursor,omitempty" jsonschema:"opaque cursor"`
}

type ListIssueLabelsOutput struct {
	Labels     []issueLabelSummary `json:"labels"`
	NextCursor string              `json:"nextCursor,omitempty"`
	HasMore    bool                `json:"hasMore"`
}

const listIssueLabelsQuery = `
query ListIssueLabels($first: Int!, $after: String, $filter: IssueLabelFilter) {
	issueLabels(first: $first, after: $after, filter: $filter) {
		nodes {
			id
			name
			color
			description
			isGroup
			team { id key name }
		}
		pageInfo { endCursor hasNextPage }
	}
}`

func listIssueLabels(client *Client) mcp.ToolHandlerFor[ListIssueLabelsInput, ListIssueLabelsOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ListIssueLabelsInput) (*mcp.CallToolResult, ListIssueLabelsOutput, error) {
		extra := map[string]any{}
		if in.TeamID != "" {
			extra["filter"] = map[string]any{"team": map[string]any{"id": eqFilter(in.TeamID)}}
		}

		var resp struct {
			IssueLabels struct {
				Nodes    []issueLabelSummary `json:"nodes"`
				PageInfo PageInfo            `json:"pageInfo"`
			} `json:"issueLabels"`
		}
		if err := client.do(ctx, listIssueLabelsQuery, pageVars(in.Limit, in.Cursor, extra), &resp); err != nil {
			return nil, ListIssueLabelsOutput{}, fmt.Errorf("linear: list issue labels: %w", err)
		}
		out := ListIssueLabelsOutput{
			Labels:     resp.IssueLabels.Nodes,
			NextCursor: resp.IssueLabels.PageInfo.EndCursor,
			HasMore:    resp.IssueLabels.PageInfo.HasNextPage,
		}
		if out.Labels == nil {
			out.Labels = []issueLabelSummary{}
		}
		return nil, out, nil
	}
}

// ---------- linear_list_project_labels ----------

type projectLabelSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Color       string `json:"color,omitempty"`
	Description string `json:"description,omitempty"`
}

type ListProjectLabelsInput struct {
	Limit  int    `json:"limit,omitempty" jsonschema:"1-100 (default 30)"`
	Cursor string `json:"cursor,omitempty" jsonschema:"opaque cursor"`
}

type ListProjectLabelsOutput struct {
	Labels     []projectLabelSummary `json:"labels"`
	NextCursor string                `json:"nextCursor,omitempty"`
	HasMore    bool                  `json:"hasMore"`
}

const listProjectLabelsQuery = `
query ListProjectLabels($first: Int!, $after: String) {
	projectLabels(first: $first, after: $after) {
		nodes { id name color description }
		pageInfo { endCursor hasNextPage }
	}
}`

func listProjectLabels(client *Client) mcp.ToolHandlerFor[ListProjectLabelsInput, ListProjectLabelsOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ListProjectLabelsInput) (*mcp.CallToolResult, ListProjectLabelsOutput, error) {
		var resp struct {
			ProjectLabels struct {
				Nodes    []projectLabelSummary `json:"nodes"`
				PageInfo PageInfo              `json:"pageInfo"`
			} `json:"projectLabels"`
		}
		if err := client.do(ctx, listProjectLabelsQuery, pageVars(in.Limit, in.Cursor, nil), &resp); err != nil {
			return nil, ListProjectLabelsOutput{}, fmt.Errorf("linear: list project labels: %w", err)
		}
		out := ListProjectLabelsOutput{
			Labels:     resp.ProjectLabels.Nodes,
			NextCursor: resp.ProjectLabels.PageInfo.EndCursor,
			HasMore:    resp.ProjectLabels.PageInfo.HasNextPage,
		}
		if out.Labels == nil {
			out.Labels = []projectLabelSummary{}
		}
		return nil, out, nil
	}
}
