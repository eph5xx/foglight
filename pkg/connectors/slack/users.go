package slack

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	slacksdk "github.com/slack-go/slack"
)

const (
	defaultUserSearchLimit = 10
	maxUserSearchLimit     = 50
	// usersListPageSize controls the slack-go pagination cursor when we
	// scan users.list locally. Slack's max for users.list is 1000.
	usersListPageSize = 200
	// maxUsersScanned is a safety cap on local-filter searches so we
	// don't iterate the entire workspace on a poorly-targeted query.
	maxUsersScanned = 5000
)

func addUserTools(server *mcp.Server, client *slacksdk.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "slack_search_users",
		Description: "Find users by email (exact, fast path via users.lookupByEmail) or by name/handle substring (case-insensitive across realName, displayName, and username; backed by users.list). Returns user IDs you can pass to other tools.",
	}, searchUsers(client))
}

type SearchUsersInput struct {
	Query string `json:"query" jsonschema:"email (exact match) or substring of real name / display name / username"`
	Limit int    `json:"limit,omitempty" jsonschema:"max results, 1-50 (default 10)"`
}

type UserSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"` // username
	RealName    string `json:"realName,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	Email       string `json:"email,omitempty"`
	Title       string `json:"title,omitempty"`
	IsBot       bool   `json:"isBot,omitempty"`
	Deleted     bool   `json:"deleted,omitempty"`
}

type SearchUsersOutput struct {
	Users        []UserSummary `json:"users"`
	UsersScanned int           `json:"usersScanned,omitempty"`
	Truncated    bool          `json:"truncated,omitempty"` // we hit the scan cap before exhausting users.list
}

func searchUsers(client *slacksdk.Client) mcp.ToolHandlerFor[SearchUsersInput, SearchUsersOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in SearchUsersInput) (*mcp.CallToolResult, SearchUsersOutput, error) {
		query := strings.TrimSpace(in.Query)
		if query == "" {
			return nil, SearchUsersOutput{}, fmt.Errorf("slack: query is required")
		}
		limit := clampLimit(in.Limit, defaultUserSearchLimit, maxUserSearchLimit)

		// Email path: use lookupByEmail. Only treat as email if it contains
		// '@' AND has at least one '.' after it — narrow enough to avoid
		// misfiring on Slack handles that contain '@'.
		if isEmail(query) {
			user, err := client.GetUserByEmailContext(ctx, query)
			if err != nil {
				// Slack returns "users_not_found" — return an empty list,
				// not an error. Other errors propagate.
				if isUsersNotFound(err) {
					return nil, SearchUsersOutput{Users: []UserSummary{}}, nil
				}
				return nil, SearchUsersOutput{}, fmt.Errorf("slack: lookup user by email %q: %w", query, err)
			}
			return nil, SearchUsersOutput{Users: []UserSummary{userToSummary(user)}}, nil
		}

		// Substring path: paginate users.list and filter locally. Slack
		// has no server-side name search.
		out := SearchUsersOutput{Users: make([]UserSummary, 0, limit)}
		needle := strings.ToLower(query)

		page := client.GetUsersPaginated(slacksdk.GetUsersOptionLimit(usersListPageSize))
		for {
			var err error
			page, err = page.Next(ctx)
			if err != nil {
				if page.Done(err) {
					break
				}
				return nil, SearchUsersOutput{}, fmt.Errorf("slack: list users: %w", err)
			}
			for i := range page.Users {
				u := &page.Users[i]
				if userMatches(u, needle) {
					out.Users = append(out.Users, userToSummary(u))
					if len(out.Users) >= limit {
						return nil, out, nil
					}
				}
			}
			out.UsersScanned += len(page.Users)
			if out.UsersScanned >= maxUsersScanned {
				out.Truncated = true
				break
			}
		}
		return nil, out, nil
	}
}

func isEmail(s string) bool {
	at := strings.IndexByte(s, '@')
	if at <= 0 || at == len(s)-1 {
		return false
	}
	return strings.IndexByte(s[at+1:], '.') > 0
}

func isUsersNotFound(err error) bool {
	if err == nil {
		return false
	}
	// slack-go wraps API errors as slack.SlackErrorResponse with a code.
	var se slacksdk.SlackErrorResponse
	if errors.As(err, &se) {
		return se.Err == "users_not_found"
	}
	return strings.Contains(err.Error(), "users_not_found")
}

func userMatches(u *slacksdk.User, needleLower string) bool {
	if u.Deleted {
		return false
	}
	candidates := []string{
		u.Name,
		u.RealName,
		u.Profile.DisplayName,
		u.Profile.RealName,
		u.Profile.Email,
	}
	for _, c := range candidates {
		if c != "" && strings.Contains(strings.ToLower(c), needleLower) {
			return true
		}
	}
	return false
}

func userToSummary(u *slacksdk.User) UserSummary {
	return UserSummary{
		ID:          u.ID,
		Name:        u.Name,
		RealName:    firstNonEmpty(u.RealName, u.Profile.RealName),
		DisplayName: u.Profile.DisplayName,
		Email:       u.Profile.Email,
		Title:       u.Profile.Title,
		IsBot:       u.IsBot,
		Deleted:     u.Deleted,
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
