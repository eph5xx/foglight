package slack

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	slacksdk "github.com/slack-go/slack"
)

func addUsergroupTools(server *mcp.Server, client *slacksdk.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "slack_list_usergroups",
		Description: "List all subteams (usergroups) in the workspace. Use this to find the right group ID by name/handle. Optional flags surface members and counts.",
	}, listUsergroups(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "slack_my_usergroups",
		Description: "List subteams the calling user belongs to. Read-only; does not join or leave groups.",
	}, myUsergroups(client))
}

type UsergroupSummary struct {
	ID          string   `json:"id"`
	Name        string   `json:"name,omitempty"`
	Handle      string   `json:"handle,omitempty"`
	Description string   `json:"description,omitempty"`
	UserCount   int      `json:"userCount,omitempty"`
	IsExternal  bool     `json:"isExternal,omitempty"`
	IsDisabled  bool     `json:"isDisabled,omitempty"`
	Users       []string `json:"users,omitempty"`
}

// ---------- slack_list_usergroups ----------

type ListUsergroupsInput struct {
	IncludeUsers    bool `json:"include_users,omitempty" jsonschema:"include each group's user IDs"`
	IncludeCount    bool `json:"include_count,omitempty" jsonschema:"include user_count per group"`
	IncludeDisabled bool `json:"include_disabled,omitempty" jsonschema:"include disabled groups"`
}

type ListUsergroupsOutput struct {
	Usergroups []UsergroupSummary `json:"usergroups"`
}

func listUsergroups(client *slacksdk.Client) mcp.ToolHandlerFor[ListUsergroupsInput, ListUsergroupsOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ListUsergroupsInput) (*mcp.CallToolResult, ListUsergroupsOutput, error) {
		groups, err := fetchUsergroups(ctx, client, in.IncludeUsers, in.IncludeCount, in.IncludeDisabled)
		if err != nil {
			return nil, ListUsergroupsOutput{}, err
		}
		out := ListUsergroupsOutput{Usergroups: make([]UsergroupSummary, 0, len(groups))}
		for i := range groups {
			out.Usergroups = append(out.Usergroups, usergroupToSummary(&groups[i], in.IncludeUsers))
		}
		return nil, out, nil
	}
}

// ---------- slack_my_usergroups ----------

type MyUsergroupsInput struct {
	IncludeDisabled bool `json:"include_disabled,omitempty" jsonschema:"include disabled groups"`
}

type MyUsergroupsOutput struct {
	UserID     string             `json:"userId"`
	Usergroups []UsergroupSummary `json:"usergroups"`
}

func myUsergroups(client *slacksdk.Client) mcp.ToolHandlerFor[MyUsergroupsInput, MyUsergroupsOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in MyUsergroupsInput) (*mcp.CallToolResult, MyUsergroupsOutput, error) {
		auth, err := client.AuthTestContext(ctx)
		if err != nil {
			return nil, MyUsergroupsOutput{}, fmt.Errorf("slack: auth test: %w", err)
		}

		// Slack has no usergroups.me; we list all groups with users
		// inlined, then filter for ones that contain our user_id.
		groups, err := fetchUsergroups(ctx, client, true, true, in.IncludeDisabled)
		if err != nil {
			return nil, MyUsergroupsOutput{}, err
		}

		out := MyUsergroupsOutput{
			UserID:     auth.UserID,
			Usergroups: make([]UsergroupSummary, 0),
		}
		for i := range groups {
			g := &groups[i]
			for _, uid := range g.Users {
				if uid == auth.UserID {
					out.Usergroups = append(out.Usergroups, usergroupToSummary(g, true))
					break
				}
			}
		}
		return nil, out, nil
	}
}

// ---------- shared ----------

func fetchUsergroups(ctx context.Context, client *slacksdk.Client, includeUsers, includeCount, includeDisabled bool) ([]slacksdk.UserGroup, error) {
	opts := []slacksdk.GetUserGroupsOption{
		slacksdk.GetUserGroupsOptionIncludeUsers(includeUsers),
		slacksdk.GetUserGroupsOptionIncludeCount(includeCount),
		slacksdk.GetUserGroupsOptionIncludeDisabled(includeDisabled),
	}
	groups, err := client.GetUserGroupsContext(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("slack: list usergroups: %w", err)
	}
	return groups, nil
}

func usergroupToSummary(g *slacksdk.UserGroup, includeUsers bool) UsergroupSummary {
	out := UsergroupSummary{
		ID:          g.ID,
		Name:        g.Name,
		Handle:      g.Handle,
		Description: g.Description,
		UserCount:   g.UserCount,
		IsExternal:  g.IsExternal,
		IsDisabled:  g.DateDelete != 0,
	}
	if includeUsers && len(g.Users) > 0 {
		out.Users = append(out.Users, g.Users...)
	}
	return out
}
