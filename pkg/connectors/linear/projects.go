package linear

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	maxProjectDescChars    = 16000
	maxProjectContentChars = 32000
	maxProjectMilestones   = 50
)

func addProjectTools(server *mcp.Server, client *Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "linear_list_projects",
		Description: "List Linear projects. Filters: team_key (any accessible team), status (backlog|planned|started|paused|completed|canceled), lead_id.",
	}, listProjects(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "linear_get_project",
		Description: "Get a single Linear project by UUID. Returns description, long-form content, status, lead, members, and milestones. Does not inline issues — use linear_list_issues with project_id.",
	}, getProject(client))
}

// ---------- shared types ----------

type projectStatus struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type projectSummary struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	URL         string         `json:"url,omitempty"`
	Status      *projectStatus `json:"status,omitempty"`
	Lead        *userRef       `json:"lead,omitempty"`
	StartDate   string         `json:"startDate,omitempty"`
	TargetDate  string         `json:"targetDate,omitempty"`
	CompletedAt string         `json:"completedAt,omitempty"`
	CreatedAt   string         `json:"createdAt,omitempty"`
	UpdatedAt   string         `json:"updatedAt,omitempty"`
}

const projectSummaryFields = `
	id
	name
	url
	status { id name type }
	lead { id name displayName email }
	startDate
	targetDate
	completedAt
	createdAt
	updatedAt
`

// ---------- linear_list_projects ----------

type ListProjectsInput struct {
	TeamKey string `json:"team_key,omitempty" jsonschema:"team key like 'ENG' (matches any accessible team)"`
	Status  string `json:"status,omitempty" jsonschema:"backlog|planned|started|paused|completed|canceled"`
	LeadID  string `json:"lead_id,omitempty" jsonschema:"user UUID of project lead"`
	Limit   int    `json:"limit,omitempty" jsonschema:"1-100 (default 30)"`
	Cursor  string `json:"cursor,omitempty" jsonschema:"opaque cursor"`
}

type ListProjectsOutput struct {
	Projects   []projectSummary `json:"projects"`
	NextCursor string           `json:"nextCursor,omitempty"`
	HasMore    bool             `json:"hasMore"`
}

const listProjectsQuery = `
query ListProjects($first: Int!, $after: String, $filter: ProjectFilter) {
	projects(first: $first, after: $after, filter: $filter) {
		nodes {` + projectSummaryFields + `}
		pageInfo { endCursor hasNextPage }
	}
}`

func listProjects(client *Client) mcp.ToolHandlerFor[ListProjectsInput, ListProjectsOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ListProjectsInput) (*mcp.CallToolResult, ListProjectsOutput, error) {
		filter := map[string]any{}
		if in.TeamKey != "" {
			filter["accessibleTeams"] = map[string]any{
				"some": map[string]any{"key": eqFilter(in.TeamKey)},
			}
		}
		if in.Status != "" {
			filter["status"] = map[string]any{"type": eqFilter(in.Status)}
		}
		if in.LeadID != "" {
			filter["lead"] = map[string]any{"id": eqFilter(in.LeadID)}
		}

		extra := map[string]any{}
		if len(filter) > 0 {
			extra["filter"] = filter
		}

		var resp struct {
			Projects struct {
				Nodes    []projectSummary `json:"nodes"`
				PageInfo PageInfo         `json:"pageInfo"`
			} `json:"projects"`
		}
		if err := client.do(ctx, listProjectsQuery, pageVars(in.Limit, in.Cursor, extra), &resp); err != nil {
			return nil, ListProjectsOutput{}, fmt.Errorf("linear: list projects: %w", err)
		}

		out := ListProjectsOutput{
			Projects:   resp.Projects.Nodes,
			NextCursor: resp.Projects.PageInfo.EndCursor,
			HasMore:    resp.Projects.PageInfo.HasNextPage,
		}
		if out.Projects == nil {
			out.Projects = []projectSummary{}
		}
		return nil, out, nil
	}
}

// ---------- linear_get_project ----------

type GetProjectInput struct {
	ID string `json:"id" jsonschema:"project UUID"`
}

type ProjectMilestone struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	TargetDate string  `json:"targetDate,omitempty"`
	SortOrder  float64 `json:"sortOrder,omitempty"`
}

type GetProjectOutput struct {
	ID                   string             `json:"id"`
	Name                 string             `json:"name"`
	Description          string             `json:"description,omitempty"`
	DescriptionTruncated bool               `json:"descriptionTruncated,omitempty"`
	Content              string             `json:"content,omitempty"`
	ContentTruncated     bool               `json:"contentTruncated,omitempty"`
	URL                  string             `json:"url,omitempty"`
	Status               *projectStatus     `json:"status,omitempty"`
	Lead                 *userRef           `json:"lead,omitempty"`
	StartDate            string             `json:"startDate,omitempty"`
	TargetDate           string             `json:"targetDate,omitempty"`
	CompletedAt          string             `json:"completedAt,omitempty"`
	CanceledAt           string             `json:"canceledAt,omitempty"`
	CreatedAt            string             `json:"createdAt,omitempty"`
	UpdatedAt            string             `json:"updatedAt,omitempty"`
	Teams                []teamRef          `json:"teams"`
	Members              []userRef          `json:"members"`
	Milestones           []ProjectMilestone `json:"milestones"`
}

const getProjectQuery = `
query GetProject($id: String!, $teamsFirst: Int!, $membersFirst: Int!, $milestonesFirst: Int!) {
	project(id: $id) {
		id
		name
		description
		content
		url
		status { id name type }
		lead { id name displayName email }
		startDate
		targetDate
		completedAt
		canceledAt
		createdAt
		updatedAt
		teams(first: $teamsFirst) { nodes { id key name } }
		members(first: $membersFirst) { nodes { id name displayName email } }
		projectMilestones(first: $milestonesFirst) {
			nodes { id name targetDate sortOrder }
		}
	}
}`

func getProject(client *Client) mcp.ToolHandlerFor[GetProjectInput, GetProjectOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in GetProjectInput) (*mcp.CallToolResult, GetProjectOutput, error) {
		id := strings.TrimSpace(in.ID)
		if id == "" {
			return nil, GetProjectOutput{}, fmt.Errorf("linear: id is required")
		}

		var resp struct {
			Project *struct {
				ID          string         `json:"id"`
				Name        string         `json:"name"`
				Description string         `json:"description"`
				Content     string         `json:"content"`
				URL         string         `json:"url"`
				Status      *projectStatus `json:"status"`
				Lead        *userRef       `json:"lead"`
				StartDate   string         `json:"startDate"`
				TargetDate  string         `json:"targetDate"`
				CompletedAt string         `json:"completedAt"`
				CanceledAt  string         `json:"canceledAt"`
				CreatedAt   string         `json:"createdAt"`
				UpdatedAt   string         `json:"updatedAt"`
				Teams       struct {
					Nodes []teamRef `json:"nodes"`
				} `json:"teams"`
				Members struct {
					Nodes []userRef `json:"nodes"`
				} `json:"members"`
				ProjectMilestones struct {
					Nodes []ProjectMilestone `json:"nodes"`
				} `json:"projectMilestones"`
			} `json:"project"`
		}

		vars := map[string]any{
			"id":              id,
			"teamsFirst":      maxLimit,
			"membersFirst":    maxLimit,
			"milestonesFirst": maxProjectMilestones,
		}
		if err := client.do(ctx, getProjectQuery, vars, &resp); err != nil {
			return nil, GetProjectOutput{}, fmt.Errorf("linear: get project %q: %w", id, err)
		}
		if resp.Project == nil {
			return nil, GetProjectOutput{}, fmt.Errorf("linear: project %q not found", id)
		}

		desc, descTrunc := truncateString(resp.Project.Description, maxProjectDescChars)
		content, contentTrunc := truncateString(resp.Project.Content, maxProjectContentChars)

		out := GetProjectOutput{
			ID:                   resp.Project.ID,
			Name:                 resp.Project.Name,
			Description:          desc,
			DescriptionTruncated: descTrunc,
			Content:              content,
			ContentTruncated:     contentTrunc,
			URL:                  resp.Project.URL,
			Status:               resp.Project.Status,
			Lead:                 resp.Project.Lead,
			StartDate:            resp.Project.StartDate,
			TargetDate:           resp.Project.TargetDate,
			CompletedAt:          resp.Project.CompletedAt,
			CanceledAt:           resp.Project.CanceledAt,
			CreatedAt:            resp.Project.CreatedAt,
			UpdatedAt:            resp.Project.UpdatedAt,
			Teams:                resp.Project.Teams.Nodes,
			Members:              resp.Project.Members.Nodes,
			Milestones:           resp.Project.ProjectMilestones.Nodes,
		}
		if out.Milestones == nil {
			out.Milestones = []ProjectMilestone{}
		}
		if out.Teams == nil {
			out.Teams = []teamRef{}
		}
		if out.Members == nil {
			out.Members = []userRef{}
		}
		return nil, out, nil
	}
}
