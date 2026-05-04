package notion

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func addUserTools(server *mcp.Server, name string, client *Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        name + "_list_users",
		Description: "List Notion workspace users (members, guests, and bots) visible to the integration. Cursor-paginated.",
	}, listUsers(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        name + "_get_user",
		Description: "Fetch a single Notion user by UUID. Notion does not expose lookup by email — list users with notion_list_users and filter client-side.",
	}, getUser(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        name + "_get_self",
		Description: "Return the bot user backing the integration token, including the workspace it belongs to. Useful for confirming auth and identifying who 'created_by' refers to in pages owned by the integration.",
	}, getSelf(client))
}

// ---------- shared types ----------

type UserSummary struct {
	ID        string `json:"id"`
	Name      string `json:"name,omitempty"`
	Type      string `json:"type"`
	Email     string `json:"email,omitempty"`
	IsBot     bool   `json:"is_bot,omitempty"`
	AvatarURL string `json:"avatar_url,omitempty"`
}

type rawUser struct {
	Object    string `json:"object"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	AvatarURL string `json:"avatar_url"`
	Person    *struct {
		Email string `json:"email"`
	} `json:"person,omitempty"`
	Bot *struct {
		Owner         json.RawMessage `json:"owner,omitempty"`
		WorkspaceName string          `json:"workspace_name,omitempty"`
	} `json:"bot,omitempty"`
}

func userSummaryFromRaw(u rawUser) UserSummary {
	s := UserSummary{
		ID:        u.ID,
		Name:      u.Name,
		Type:      u.Type,
		AvatarURL: u.AvatarURL,
		IsBot:     u.Type == "bot",
	}
	if u.Person != nil {
		s.Email = u.Person.Email
	}
	return s
}

// ---------- notion_list_users ----------

type ListUsersInput struct {
	Limit  int    `json:"limit,omitempty" jsonschema:"1-100 (default 25)"`
	Cursor string `json:"cursor,omitempty" jsonschema:"opaque start_cursor from a prior call's next_cursor"`
}

type ListUsersOutput struct {
	Users      []UserSummary `json:"users"`
	NextCursor string        `json:"next_cursor,omitempty"`
	HasMore    bool          `json:"has_more"`
}

func listUsers(client *Client) mcp.ToolHandlerFor[ListUsersInput, ListUsersOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ListUsersInput) (*mcp.CallToolResult, ListUsersOutput, error) {
		path := fmt.Sprintf("/users?page_size=%d", clampLimit(in.Limit))
		if in.Cursor != "" {
			path += "&start_cursor=" + url.QueryEscape(in.Cursor)
		}
		var resp struct {
			Results    []rawUser `json:"results"`
			NextCursor string    `json:"next_cursor"`
			HasMore    bool      `json:"has_more"`
		}
		if err := client.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
			return nil, ListUsersOutput{}, fmt.Errorf("notion: list users: %w", err)
		}
		out := ListUsersOutput{
			Users:      make([]UserSummary, 0, len(resp.Results)),
			NextCursor: resp.NextCursor,
			HasMore:    resp.HasMore,
		}
		for _, u := range resp.Results {
			out.Users = append(out.Users, userSummaryFromRaw(u))
		}
		return nil, out, nil
	}
}

// ---------- notion_get_user ----------

type GetUserInput struct {
	UserID string `json:"user_id" jsonschema:"Notion user UUID"`
}

func getUser(client *Client) mcp.ToolHandlerFor[GetUserInput, UserSummary] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in GetUserInput) (*mcp.CallToolResult, UserSummary, error) {
		id := normalizeID(in.UserID)
		if id == "" {
			return nil, UserSummary{}, fmt.Errorf("notion: user_id is required")
		}
		var u rawUser
		if err := client.do(ctx, http.MethodGet, "/users/"+id, nil, &u); err != nil {
			return nil, UserSummary{}, fmt.Errorf("notion: get user %q: %w", id, err)
		}
		return nil, userSummaryFromRaw(u), nil
	}
}

// ---------- notion_get_self ----------

type GetSelfInput struct{}

type GetSelfOutput struct {
	UserSummary
	WorkspaceName string `json:"workspace_name,omitempty"`
}

func getSelf(client *Client) mcp.ToolHandlerFor[GetSelfInput, GetSelfOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ GetSelfInput) (*mcp.CallToolResult, GetSelfOutput, error) {
		var u rawUser
		if err := client.do(ctx, http.MethodGet, "/users/me", nil, &u); err != nil {
			return nil, GetSelfOutput{}, fmt.Errorf("notion: get self: %w", err)
		}
		out := GetSelfOutput{UserSummary: userSummaryFromRaw(u)}
		if u.Bot != nil {
			out.WorkspaceName = u.Bot.WorkspaceName
		}
		return nil, out, nil
	}
}
