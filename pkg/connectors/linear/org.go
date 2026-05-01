package linear

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxTeamDescChars = 4000

func addOrgTools(server *mcp.Server, client *Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "linear_list_teams",
		Description: "List all teams in the workspace.",
	}, listTeams(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "linear_get_team",
		Description: "Get a single team by UUID or team key (e.g. 'ENG'). Includes description, timezone, issue count, and cycles enabled/duration.",
	}, getTeam(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "linear_list_users",
		Description: "List all active users in the workspace.",
	}, listUsers(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "linear_get_user",
		Description: "Get a single user by UUID.",
	}, getUser(client))
}

// ---------- linear_list_teams ----------

type teamSummary struct {
	ID          string `json:"id"`
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Private     bool   `json:"private,omitempty"`
	CreatedAt   string `json:"createdAt,omitempty"`
}

type ListTeamsInput struct {
	Limit  int    `json:"limit,omitempty" jsonschema:"1-100 (default 30)"`
	Cursor string `json:"cursor,omitempty" jsonschema:"opaque cursor"`
}

type ListTeamsOutput struct {
	Teams      []teamSummary `json:"teams"`
	NextCursor string        `json:"nextCursor,omitempty"`
	HasMore    bool          `json:"hasMore"`
}

const listTeamsQuery = `
query ListTeams($first: Int!, $after: String) {
	teams(first: $first, after: $after) {
		nodes { id key name description private createdAt }
		pageInfo { endCursor hasNextPage }
	}
}`

func listTeams(client *Client) mcp.ToolHandlerFor[ListTeamsInput, ListTeamsOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ListTeamsInput) (*mcp.CallToolResult, ListTeamsOutput, error) {
		var resp struct {
			Teams struct {
				Nodes    []teamSummary `json:"nodes"`
				PageInfo PageInfo      `json:"pageInfo"`
			} `json:"teams"`
		}
		if err := client.do(ctx, listTeamsQuery, pageVars(in.Limit, in.Cursor, nil), &resp); err != nil {
			return nil, ListTeamsOutput{}, fmt.Errorf("linear: list teams: %w", err)
		}
		out := ListTeamsOutput{
			Teams:      resp.Teams.Nodes,
			NextCursor: resp.Teams.PageInfo.EndCursor,
			HasMore:    resp.Teams.PageInfo.HasNextPage,
		}
		if out.Teams == nil {
			out.Teams = []teamSummary{}
		}
		return nil, out, nil
	}
}

// ---------- linear_get_team ----------

type GetTeamInput struct {
	ID string `json:"id" jsonschema:"team UUID or team key (e.g. 'ENG')"`
}

type GetTeamOutput struct {
	ID                   string `json:"id"`
	Key                  string `json:"key"`
	Name                 string `json:"name"`
	Description          string `json:"description,omitempty"`
	DescriptionTruncated bool   `json:"descriptionTruncated,omitempty"`
	Private              bool   `json:"private,omitempty"`
	Timezone             string `json:"timezone,omitempty"`
	IssueCount           int    `json:"issueCount,omitempty"`
	CyclesEnabled        bool   `json:"cyclesEnabled,omitempty"`
	CycleDuration        int    `json:"cycleDuration,omitempty"`
	CreatedAt            string `json:"createdAt,omitempty"`
	UpdatedAt            string `json:"updatedAt,omitempty"`
}

const getTeamQuery = `
query GetTeam($id: String!) {
	team(id: $id) {
		id
		key
		name
		description
		private
		timezone
		issueCount
		cyclesEnabled
		cycleDuration
		createdAt
		updatedAt
	}
}`

func getTeam(client *Client) mcp.ToolHandlerFor[GetTeamInput, GetTeamOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in GetTeamInput) (*mcp.CallToolResult, GetTeamOutput, error) {
		id := strings.TrimSpace(in.ID)
		if id == "" {
			return nil, GetTeamOutput{}, fmt.Errorf("linear: id is required")
		}

		// Linear's team(id:) accepts either UUID or team key.
		var resp struct {
			Team *struct {
				ID            string `json:"id"`
				Key           string `json:"key"`
				Name          string `json:"name"`
				Description   string `json:"description"`
				Private       bool   `json:"private"`
				Timezone      string `json:"timezone"`
				IssueCount    int    `json:"issueCount"`
				CyclesEnabled bool   `json:"cyclesEnabled"`
				CycleDuration int    `json:"cycleDuration"`
				CreatedAt     string `json:"createdAt"`
				UpdatedAt     string `json:"updatedAt"`
			} `json:"team"`
		}
		if err := client.do(ctx, getTeamQuery, map[string]any{"id": id}, &resp); err != nil {
			// Fall back to filter-by-key for inputs that look like keys
			// in case team(id:) rejected them.
			if isLikelyTeamKey(id) {
				return getTeamByKey(ctx, client, id)
			}
			return nil, GetTeamOutput{}, fmt.Errorf("linear: get team %q: %w", id, err)
		}
		if resp.Team == nil {
			if isLikelyTeamKey(id) {
				return getTeamByKey(ctx, client, id)
			}
			return nil, GetTeamOutput{}, fmt.Errorf("linear: team %q not found", id)
		}

		desc, descTrunc := truncateString(resp.Team.Description, maxTeamDescChars)
		return nil, GetTeamOutput{
			ID:                   resp.Team.ID,
			Key:                  resp.Team.Key,
			Name:                 resp.Team.Name,
			Description:          desc,
			DescriptionTruncated: descTrunc,
			Private:              resp.Team.Private,
			Timezone:             resp.Team.Timezone,
			IssueCount:           resp.Team.IssueCount,
			CyclesEnabled:        resp.Team.CyclesEnabled,
			CycleDuration:        resp.Team.CycleDuration,
			CreatedAt:            resp.Team.CreatedAt,
			UpdatedAt:            resp.Team.UpdatedAt,
		}, nil
	}
}

const getTeamByKeyQuery = `
query GetTeamByKey($filter: TeamFilter) {
	teams(first: 1, filter: $filter) {
		nodes {
			id
			key
			name
			description
			private
			timezone
			issueCount
			cyclesEnabled
			cycleDuration
			createdAt
			updatedAt
		}
	}
}`

func getTeamByKey(ctx context.Context, client *Client, key string) (*mcp.CallToolResult, GetTeamOutput, error) {
	var resp struct {
		Teams struct {
			Nodes []struct {
				ID            string `json:"id"`
				Key           string `json:"key"`
				Name          string `json:"name"`
				Description   string `json:"description"`
				Private       bool   `json:"private"`
				Timezone      string `json:"timezone"`
				IssueCount    int    `json:"issueCount"`
				CyclesEnabled bool   `json:"cyclesEnabled"`
				CycleDuration int    `json:"cycleDuration"`
				CreatedAt     string `json:"createdAt"`
				UpdatedAt     string `json:"updatedAt"`
			} `json:"nodes"`
		} `json:"teams"`
	}
	vars := map[string]any{"filter": map[string]any{"key": eqFilter(key)}}
	if err := client.do(ctx, getTeamByKeyQuery, vars, &resp); err != nil {
		return nil, GetTeamOutput{}, fmt.Errorf("linear: get team %q: %w", key, err)
	}
	if len(resp.Teams.Nodes) == 0 {
		return nil, GetTeamOutput{}, fmt.Errorf("linear: team %q not found", key)
	}
	t := resp.Teams.Nodes[0]
	desc, descTrunc := truncateString(t.Description, maxTeamDescChars)
	return nil, GetTeamOutput{
		ID:                   t.ID,
		Key:                  t.Key,
		Name:                 t.Name,
		Description:          desc,
		DescriptionTruncated: descTrunc,
		Private:              t.Private,
		Timezone:             t.Timezone,
		IssueCount:           t.IssueCount,
		CyclesEnabled:        t.CyclesEnabled,
		CycleDuration:        t.CycleDuration,
		CreatedAt:            t.CreatedAt,
		UpdatedAt:            t.UpdatedAt,
	}, nil
}

// isLikelyTeamKey returns true if s looks like a Linear team key (short,
// all uppercase letters/digits, no hyphens). UUIDs contain hyphens.
//
// Linear allows team keys up to 12 chars, configurable per workspace.
func isLikelyTeamKey(s string) bool {
	if s == "" || len(s) > 12 {
		return false
	}
	for _, r := range s {
		if !((r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

// ---------- linear_list_users ----------

type userSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName,omitempty"`
	Email       string `json:"email,omitempty"`
	Active      bool   `json:"active"`
	Admin       bool   `json:"admin,omitempty"`
	URL         string `json:"url,omitempty"`
	CreatedAt   string `json:"createdAt,omitempty"`
}

type ListUsersInput struct {
	Limit  int    `json:"limit,omitempty" jsonschema:"1-100 (default 30)"`
	Cursor string `json:"cursor,omitempty" jsonschema:"opaque cursor"`
}

type ListUsersOutput struct {
	Users      []userSummary `json:"users"`
	NextCursor string        `json:"nextCursor,omitempty"`
	HasMore    bool          `json:"hasMore"`
}

const listUsersQuery = `
query ListUsers($first: Int!, $after: String) {
	users(first: $first, after: $after) {
		nodes { id name displayName email active admin url createdAt }
		pageInfo { endCursor hasNextPage }
	}
}`

func listUsers(client *Client) mcp.ToolHandlerFor[ListUsersInput, ListUsersOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ListUsersInput) (*mcp.CallToolResult, ListUsersOutput, error) {
		var resp struct {
			Users struct {
				Nodes    []userSummary `json:"nodes"`
				PageInfo PageInfo      `json:"pageInfo"`
			} `json:"users"`
		}
		if err := client.do(ctx, listUsersQuery, pageVars(in.Limit, in.Cursor, nil), &resp); err != nil {
			return nil, ListUsersOutput{}, fmt.Errorf("linear: list users: %w", err)
		}
		out := ListUsersOutput{
			Users:      resp.Users.Nodes,
			NextCursor: resp.Users.PageInfo.EndCursor,
			HasMore:    resp.Users.PageInfo.HasNextPage,
		}
		if out.Users == nil {
			out.Users = []userSummary{}
		}
		return nil, out, nil
	}
}

// ---------- linear_get_user ----------

type GetUserInput struct {
	ID string `json:"id" jsonschema:"user UUID"`
}

type GetUserOutput struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName,omitempty"`
	Email       string `json:"email,omitempty"`
	Active      bool   `json:"active"`
	Admin       bool   `json:"admin,omitempty"`
	Guest       bool   `json:"guest,omitempty"`
	URL         string `json:"url,omitempty"`
	Description string `json:"description,omitempty"`
	StatusEmoji string `json:"statusEmoji,omitempty"`
	StatusLabel string `json:"statusLabel,omitempty"`
	Timezone    string `json:"timezone,omitempty"`
	CreatedAt   string `json:"createdAt,omitempty"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
}

const getUserQuery = `
query GetUser($id: String!) {
	user(id: $id) {
		id
		name
		displayName
		email
		active
		admin
		guest
		url
		description
		statusEmoji
		statusLabel
		timezone
		createdAt
		updatedAt
	}
}`

func getUser(client *Client) mcp.ToolHandlerFor[GetUserInput, GetUserOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in GetUserInput) (*mcp.CallToolResult, GetUserOutput, error) {
		id := strings.TrimSpace(in.ID)
		if id == "" {
			return nil, GetUserOutput{}, fmt.Errorf("linear: id is required")
		}
		var resp struct {
			User *struct {
				ID          string `json:"id"`
				Name        string `json:"name"`
				DisplayName string `json:"displayName"`
				Email       string `json:"email"`
				Active      bool   `json:"active"`
				Admin       bool   `json:"admin"`
				Guest       bool   `json:"guest"`
				URL         string `json:"url"`
				Description string `json:"description"`
				StatusEmoji string `json:"statusEmoji"`
				StatusLabel string `json:"statusLabel"`
				Timezone    string `json:"timezone"`
				CreatedAt   string `json:"createdAt"`
				UpdatedAt   string `json:"updatedAt"`
			} `json:"user"`
		}
		if err := client.do(ctx, getUserQuery, map[string]any{"id": id}, &resp); err != nil {
			return nil, GetUserOutput{}, fmt.Errorf("linear: get user %q: %w", id, err)
		}
		if resp.User == nil {
			return nil, GetUserOutput{}, fmt.Errorf("linear: user %q not found", id)
		}
		u := resp.User
		return nil, GetUserOutput{
			ID:          u.ID,
			Name:        u.Name,
			DisplayName: u.DisplayName,
			Email:       u.Email,
			Active:      u.Active,
			Admin:       u.Admin,
			Guest:       u.Guest,
			URL:         u.URL,
			Description: u.Description,
			StatusEmoji: u.StatusEmoji,
			StatusLabel: u.StatusLabel,
			Timezone:    u.Timezone,
			CreatedAt:   u.CreatedAt,
			UpdatedAt:   u.UpdatedAt,
		}, nil
	}
}
