package linear

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	maxIssueDescChars   = 16000
	maxCommentBodyChars = 8000
	maxCommentsPerIssue = 100
	maxLabelsPerIssue   = 50
	maxChildrenPerIssue = 50
)

func addIssueTools(server *mcp.Server, client *Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "linear_list_issues",
		Description: "List Linear issues with filters (team_key, state_type, assignee_id, project_id, label, updated_since). Cursor-paginated; returns summaries without descriptions.",
	}, listIssues(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "linear_get_issue",
		Description: "Get a single Linear issue by UUID or human identifier (e.g. 'ENG-123'). Includes description, labels, sub-issues, and inline comment thread.",
	}, getIssue(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "linear_list_my_issues",
		Description: "List issues assigned to the authenticated user. Optional state_type filter.",
	}, listMyIssues(client))
}

// ---------- shared types ----------

type stateRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type teamRef struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
}

type userRef struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	Email       string `json:"email,omitempty"`
}

type projectRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type cycleRef struct {
	ID     string `json:"id"`
	Name   string `json:"name,omitempty"`
	Number int    `json:"number,omitempty"`
}

type labelRef struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}

type issueSummary struct {
	ID         string      `json:"id"`
	Identifier string      `json:"identifier"`
	Title      string      `json:"title"`
	Priority   int         `json:"priority"`
	URL        string      `json:"url,omitempty"`
	CreatedAt  string      `json:"createdAt,omitempty"`
	UpdatedAt  string      `json:"updatedAt,omitempty"`
	State      *stateRef   `json:"state,omitempty"`
	Team       *teamRef    `json:"team,omitempty"`
	Assignee   *userRef    `json:"assignee,omitempty"`
	Project    *projectRef `json:"project,omitempty"`
}

const issueSummaryFields = `
	id
	identifier
	title
	priority
	url
	createdAt
	updatedAt
	state { id name type }
	team { id key name }
	assignee { id name displayName email }
	project { id name }
`

// buildIssueFilter assembles the IssueFilter object from the flat tool
// input fields. Returns nil if no filters are set so the caller can omit
// the filter variable entirely.
func buildIssueFilter(teamKey, stateType, assigneeID, projectID, label, updatedSince string) map[string]any {
	filter := map[string]any{}
	if teamKey != "" {
		filter["team"] = map[string]any{"key": eqFilter(teamKey)}
	}
	if stateType != "" {
		filter["state"] = map[string]any{"type": eqFilter(stateType)}
	}
	if assigneeID != "" {
		filter["assignee"] = map[string]any{"id": eqFilter(assigneeID)}
	}
	if projectID != "" {
		filter["project"] = map[string]any{"id": eqFilter(projectID)}
	}
	if label != "" {
		filter["labels"] = map[string]any{"name": eqFilter(label)}
	}
	if updatedSince != "" {
		filter["updatedAt"] = map[string]any{"gte": updatedSince}
	}
	if len(filter) == 0 {
		return nil
	}
	return filter
}

// ---------- linear_list_issues ----------

type ListIssuesInput struct {
	TeamKey      string `json:"team_key,omitempty" jsonschema:"team key like 'ENG'"`
	StateType    string `json:"state_type,omitempty" jsonschema:"backlog|unstarted|started|completed|canceled"`
	AssigneeID   string `json:"assignee_id,omitempty" jsonschema:"user UUID"`
	ProjectID    string `json:"project_id,omitempty" jsonschema:"project UUID"`
	Label        string `json:"label,omitempty" jsonschema:"label name (exact match)"`
	UpdatedSince string `json:"updated_since,omitempty" jsonschema:"RFC3339 lower bound on updatedAt"`
	Limit        int    `json:"limit,omitempty" jsonschema:"1-100 (default 30)"`
	Cursor       string `json:"cursor,omitempty" jsonschema:"opaque cursor from a prior call's next_cursor"`
}

type ListIssuesOutput struct {
	Issues     []issueSummary `json:"issues"`
	NextCursor string         `json:"nextCursor,omitempty"`
	HasMore    bool           `json:"hasMore"`
}

const listIssuesQuery = `
query ListIssues($first: Int!, $after: String, $filter: IssueFilter) {
	issues(first: $first, after: $after, filter: $filter) {
		nodes {` + issueSummaryFields + `}
		pageInfo { endCursor hasNextPage }
	}
}`

func listIssues(client *Client) mcp.ToolHandlerFor[ListIssuesInput, ListIssuesOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ListIssuesInput) (*mcp.CallToolResult, ListIssuesOutput, error) {
		updatedSince, err := parseUpdatedSince(in.UpdatedSince)
		if err != nil {
			return nil, ListIssuesOutput{}, fmt.Errorf("linear: %w", err)
		}

		extra := map[string]any{}
		if filter := buildIssueFilter(in.TeamKey, in.StateType, in.AssigneeID, in.ProjectID, in.Label, updatedSince); filter != nil {
			extra["filter"] = filter
		}

		var resp struct {
			Issues struct {
				Nodes    []issueSummary `json:"nodes"`
				PageInfo PageInfo       `json:"pageInfo"`
			} `json:"issues"`
		}
		if err := client.do(ctx, listIssuesQuery, pageVars(in.Limit, in.Cursor, extra), &resp); err != nil {
			return nil, ListIssuesOutput{}, fmt.Errorf("linear: list issues: %w", err)
		}

		out := ListIssuesOutput{
			Issues:     resp.Issues.Nodes,
			NextCursor: resp.Issues.PageInfo.EndCursor,
			HasMore:    resp.Issues.PageInfo.HasNextPage,
		}
		if out.Issues == nil {
			out.Issues = []issueSummary{}
		}
		return nil, out, nil
	}
}

// ---------- linear_get_issue ----------

type GetIssueInput struct {
	ID string `json:"id" jsonschema:"issue UUID or human identifier (e.g. 'ENG-123')"`
}

type IssueComment struct {
	ID            string   `json:"id"`
	Body          string   `json:"body"`
	BodyTruncated bool     `json:"bodyTruncated,omitempty"`
	CreatedAt     string   `json:"createdAt,omitempty"`
	UpdatedAt     string   `json:"updatedAt,omitempty"`
	User          *userRef `json:"user,omitempty"`
}

type IssueChild struct {
	ID         string    `json:"id"`
	Identifier string    `json:"identifier"`
	Title      string    `json:"title"`
	State      *stateRef `json:"state,omitempty"`
}

type IssueParent struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
}

type GetIssueOutput struct {
	ID                   string         `json:"id"`
	Identifier           string         `json:"identifier"`
	Title                string         `json:"title"`
	Description          string         `json:"description,omitempty"`
	DescriptionTruncated bool           `json:"descriptionTruncated,omitempty"`
	Priority             int            `json:"priority"`
	Estimate             float64        `json:"estimate,omitempty"`
	URL                  string         `json:"url,omitempty"`
	BranchName           string         `json:"branchName,omitempty"`
	CreatedAt            string         `json:"createdAt,omitempty"`
	UpdatedAt            string         `json:"updatedAt,omitempty"`
	CompletedAt          string         `json:"completedAt,omitempty"`
	CanceledAt           string         `json:"canceledAt,omitempty"`
	State                *stateRef      `json:"state,omitempty"`
	Team                 *teamRef       `json:"team,omitempty"`
	Assignee             *userRef       `json:"assignee,omitempty"`
	Creator              *userRef       `json:"creator,omitempty"`
	Project              *projectRef    `json:"project,omitempty"`
	Cycle                *cycleRef      `json:"cycle,omitempty"`
	Parent               *IssueParent   `json:"parent,omitempty"`
	Labels               []labelRef     `json:"labels"`
	Children             []IssueChild   `json:"children"`
	Comments             []IssueComment `json:"comments"`
	CommentsHasMore      bool           `json:"commentsHasMore,omitempty"`
}

const getIssueQuery = `
query GetIssue($id: String!, $commentsFirst: Int!, $labelsFirst: Int!, $childrenFirst: Int!) {
	issue(id: $id) {
		id
		identifier
		title
		description
		priority
		estimate
		url
		branchName
		createdAt
		updatedAt
		completedAt
		canceledAt
		state { id name type }
		team { id key name }
		assignee { id name displayName email }
		creator { id name displayName email }
		project { id name }
		cycle { id name number }
		parent { id identifier title }
		labels(first: $labelsFirst) { nodes { id name color } }
		children(first: $childrenFirst) {
			nodes { id identifier title state { id name type } }
		}
		comments(first: $commentsFirst) {
			nodes {
				id
				body
				createdAt
				updatedAt
				user { id name displayName email }
			}
			pageInfo { endCursor hasNextPage }
		}
	}
}`

func getIssue(client *Client) mcp.ToolHandlerFor[GetIssueInput, GetIssueOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in GetIssueInput) (*mcp.CallToolResult, GetIssueOutput, error) {
		id := strings.TrimSpace(in.ID)
		if id == "" {
			return nil, GetIssueOutput{}, fmt.Errorf("linear: id is required")
		}

		var resp struct {
			Issue *struct {
				ID          string      `json:"id"`
				Identifier  string      `json:"identifier"`
				Title       string      `json:"title"`
				Description string      `json:"description"`
				Priority    int         `json:"priority"`
				Estimate    float64     `json:"estimate"`
				URL         string      `json:"url"`
				BranchName  string      `json:"branchName"`
				CreatedAt   string      `json:"createdAt"`
				UpdatedAt   string      `json:"updatedAt"`
				CompletedAt string      `json:"completedAt"`
				CanceledAt  string      `json:"canceledAt"`
				State       *stateRef   `json:"state"`
				Team        *teamRef    `json:"team"`
				Assignee    *userRef    `json:"assignee"`
				Creator     *userRef    `json:"creator"`
				Project     *projectRef `json:"project"`
				Cycle       *cycleRef   `json:"cycle"`
				Parent      *IssueParent `json:"parent"`
				Labels      struct {
					Nodes []labelRef `json:"nodes"`
				} `json:"labels"`
				Children struct {
					Nodes []IssueChild `json:"nodes"`
				} `json:"children"`
				Comments struct {
					Nodes []struct {
						ID        string   `json:"id"`
						Body      string   `json:"body"`
						CreatedAt string   `json:"createdAt"`
						UpdatedAt string   `json:"updatedAt"`
						User      *userRef `json:"user"`
					} `json:"nodes"`
					PageInfo PageInfo `json:"pageInfo"`
				} `json:"comments"`
			} `json:"issue"`
		}

		vars := map[string]any{
			"id":            id,
			"commentsFirst": maxCommentsPerIssue,
			"labelsFirst":   maxLabelsPerIssue,
			"childrenFirst": maxChildrenPerIssue,
		}
		if err := client.do(ctx, getIssueQuery, vars, &resp); err != nil {
			return nil, GetIssueOutput{}, fmt.Errorf("linear: get issue %q: %w", id, err)
		}
		if resp.Issue == nil {
			return nil, GetIssueOutput{}, fmt.Errorf("linear: issue %q not found", id)
		}

		desc, descTrunc := truncateString(resp.Issue.Description, maxIssueDescChars)
		out := GetIssueOutput{
			ID:                   resp.Issue.ID,
			Identifier:           resp.Issue.Identifier,
			Title:                resp.Issue.Title,
			Description:          desc,
			DescriptionTruncated: descTrunc,
			Priority:             resp.Issue.Priority,
			Estimate:             resp.Issue.Estimate,
			URL:                  resp.Issue.URL,
			BranchName:           resp.Issue.BranchName,
			CreatedAt:            resp.Issue.CreatedAt,
			UpdatedAt:            resp.Issue.UpdatedAt,
			CompletedAt:          resp.Issue.CompletedAt,
			CanceledAt:           resp.Issue.CanceledAt,
			State:                resp.Issue.State,
			Team:                 resp.Issue.Team,
			Assignee:             resp.Issue.Assignee,
			Creator:              resp.Issue.Creator,
			Project:              resp.Issue.Project,
			Cycle:                resp.Issue.Cycle,
			Parent:               resp.Issue.Parent,
			Labels:               resp.Issue.Labels.Nodes,
			Children:             resp.Issue.Children.Nodes,
			Comments:             make([]IssueComment, 0, len(resp.Issue.Comments.Nodes)),
			CommentsHasMore:      resp.Issue.Comments.PageInfo.HasNextPage,
		}
		if out.Labels == nil {
			out.Labels = []labelRef{}
		}
		if out.Children == nil {
			out.Children = []IssueChild{}
		}
		for _, c := range resp.Issue.Comments.Nodes {
			body, trunc := truncateString(c.Body, maxCommentBodyChars)
			out.Comments = append(out.Comments, IssueComment{
				ID:            c.ID,
				Body:          body,
				BodyTruncated: trunc,
				CreatedAt:     c.CreatedAt,
				UpdatedAt:     c.UpdatedAt,
				User:          c.User,
			})
		}
		return nil, out, nil
	}
}

// ---------- linear_list_my_issues ----------

type ListMyIssuesInput struct {
	StateType string `json:"state_type,omitempty" jsonschema:"backlog|unstarted|started|completed|canceled"`
	Limit     int    `json:"limit,omitempty" jsonschema:"1-100 (default 30)"`
	Cursor    string `json:"cursor,omitempty" jsonschema:"opaque cursor"`
}

const listMyIssuesQuery = `
query ListMyIssues($first: Int!, $after: String, $filter: IssueFilter) {
	viewer {
		assignedIssues(first: $first, after: $after, filter: $filter) {
			nodes {` + issueSummaryFields + `}
			pageInfo { endCursor hasNextPage }
		}
	}
}`

func listMyIssues(client *Client) mcp.ToolHandlerFor[ListMyIssuesInput, ListIssuesOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ListMyIssuesInput) (*mcp.CallToolResult, ListIssuesOutput, error) {
		extra := map[string]any{}
		if in.StateType != "" {
			extra["filter"] = map[string]any{"state": map[string]any{"type": eqFilter(in.StateType)}}
		}

		var resp struct {
			Viewer struct {
				AssignedIssues struct {
					Nodes    []issueSummary `json:"nodes"`
					PageInfo PageInfo       `json:"pageInfo"`
				} `json:"assignedIssues"`
			} `json:"viewer"`
		}
		if err := client.do(ctx, listMyIssuesQuery, pageVars(in.Limit, in.Cursor, extra), &resp); err != nil {
			return nil, ListIssuesOutput{}, fmt.Errorf("linear: list my issues: %w", err)
		}

		out := ListIssuesOutput{
			Issues:     resp.Viewer.AssignedIssues.Nodes,
			NextCursor: resp.Viewer.AssignedIssues.PageInfo.EndCursor,
			HasMore:    resp.Viewer.AssignedIssues.PageInfo.HasNextPage,
		}
		if out.Issues == nil {
			out.Issues = []issueSummary{}
		}
		return nil, out, nil
	}
}
