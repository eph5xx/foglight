package notion

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func addUserTools(server *mcp.Server, name string, client *Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        name + "_list_users",
		Description: "List Notion workspace users (members and guests). Cursor-paginated.",
	}, listUsers(client))
}

// ---------- notion_list_users ----------

type ListUsersInput struct {
	Limit  int    `json:"limit,omitempty" jsonschema:"1-100 (default 25)"`
	Cursor string `json:"cursor,omitempty" jsonschema:"opaque start_cursor from a prior call's next_cursor"`
}

type userSummary struct {
	ID        string `json:"id"`
	Name      string `json:"name,omitempty"`
	Type      string `json:"type"`
	Email     string `json:"email,omitempty"`
	IsBot     bool   `json:"isBot,omitempty"`
	AvatarURL string `json:"avatarUrl,omitempty"`
}

type ListUsersOutput struct {
	Users      []userSummary `json:"users"`
	NextCursor string        `json:"nextCursor,omitempty"`
	HasMore    bool          `json:"hasMore"`
}

// rawUser captures both person and bot user shapes from /v1/users.
type rawUser struct {
	Object    string `json:"object"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	AvatarURL string `json:"avatar_url"`
	Person    *struct {
		Email string `json:"email"`
	} `json:"person,omitempty"`
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
			Users:      make([]userSummary, 0, len(resp.Results)),
			NextCursor: resp.NextCursor,
			HasMore:    resp.HasMore,
		}
		for _, u := range resp.Results {
			summary := userSummary{
				ID:        u.ID,
				Name:      u.Name,
				Type:      u.Type,
				AvatarURL: u.AvatarURL,
				IsBot:     u.Type == "bot",
			}
			if u.Person != nil {
				summary.Email = u.Person.Email
			}
			out.Users = append(out.Users, summary)
		}
		return nil, out, nil
	}
}
