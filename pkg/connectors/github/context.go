package github

import (
	"context"
	"fmt"
	"strings"

	gh "github.com/google/go-github/v82/github"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func addContextTools(server *mcp.Server, name string, client *gh.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        name + "_get_me",
		Description: "Get the authenticated user's profile (login, name, email, bio, counts, timestamps).",
	}, getMe(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        name + "_get_teams",
		Description: "List teams. With no input, returns the authenticated user's teams. With org, returns all teams in that org.",
	}, getTeams(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        name + "_get_team_members",
		Description: "List members of a team in an org. Required: org, team_slug.",
	}, getTeamMembers(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        name + "_search_users",
		Description: "Search GitHub users using GitHub's search syntax.",
	}, searchUsers(client))
}

// ---------- github_get_me ----------

type GetMeInput struct{}

type UserProfile struct {
	Login       string `json:"login"`
	ID          int64  `json:"id"`
	Name        string `json:"name,omitempty"`
	Email       string `json:"email,omitempty"`
	Bio         string `json:"bio,omitempty"`
	Company     string `json:"company,omitempty"`
	Location    string `json:"location,omitempty"`
	Blog        string `json:"blog,omitempty"`
	Type        string `json:"type,omitempty"`
	URL         string `json:"url,omitempty"`
	PublicRepos int    `json:"public_repos,omitempty"`
	Followers   int    `json:"followers,omitempty"`
	Following   int    `json:"following,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

func getMe(client *gh.Client) mcp.ToolHandlerFor[GetMeInput, UserProfile] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ GetMeInput) (*mcp.CallToolResult, UserProfile, error) {
		u, _, err := client.Users.Get(ctx, "")
		if err != nil {
			return nil, UserProfile{}, fmt.Errorf("github: get authenticated user: %w", err)
		}
		return nil, userProfile(u), nil
	}
}

// ---------- github_get_teams ----------

type GetTeamsInput struct {
	Org     string `json:"org,omitempty" jsonschema:"if set, list all teams in this org instead of the authenticated user's teams"`
	PerPage int    `json:"per_page,omitempty" jsonschema:"1-100 (default 30)"`
	Page    int    `json:"page,omitempty" jsonschema:"1-based page (default 1)"`
}

type TeamSummary struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Slug         string `json:"slug"`
	Description  string `json:"description,omitempty"`
	Privacy      string `json:"privacy,omitempty"`
	Permission   string `json:"permission,omitempty"`
	URL          string `json:"url,omitempty"`
	HTMLURL      string `json:"html_url,omitempty"`
	Organization string `json:"organization,omitempty"`
	Parent       string `json:"parent,omitempty"`
}

type GetTeamsOutput struct {
	Teams []TeamSummary `json:"teams"`
}

func getTeams(client *gh.Client) mcp.ToolHandlerFor[GetTeamsInput, GetTeamsOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in GetTeamsInput) (*mcp.CallToolResult, GetTeamsOutput, error) {
		opts := &gh.ListOptions{
			PerPage: clampPerPage(in.PerPage),
			Page:    defaultInt(in.Page, 1),
		}

		var teams []*gh.Team
		var err error
		if in.Org != "" {
			teams, _, err = client.Teams.ListTeams(ctx, in.Org, opts)
		} else {
			teams, _, err = client.Teams.ListUserTeams(ctx, opts)
		}
		if err != nil {
			return nil, GetTeamsOutput{}, fmt.Errorf("github: list teams: %w", err)
		}

		out := GetTeamsOutput{Teams: make([]TeamSummary, 0, len(teams))}
		for _, t := range teams {
			out.Teams = append(out.Teams, teamSummary(t))
		}
		return nil, out, nil
	}
}

// ---------- github_get_team_members ----------

type GetTeamMembersInput struct {
	Org      string `json:"org" jsonschema:"organization login"`
	TeamSlug string `json:"team_slug" jsonschema:"team slug"`
	Role     string `json:"role,omitempty" jsonschema:"all|member|maintainer (default all)"`
	PerPage  int    `json:"per_page,omitempty" jsonschema:"1-100 (default 30)"`
	Page     int    `json:"page,omitempty" jsonschema:"1-based page (default 1)"`
}

type UserSummary struct {
	Login   string `json:"login"`
	ID      int64  `json:"id"`
	Type    string `json:"type,omitempty"`
	HTMLURL string `json:"html_url,omitempty"`
}

type GetTeamMembersOutput struct {
	Members []UserSummary `json:"members"`
}

func getTeamMembers(client *gh.Client) mcp.ToolHandlerFor[GetTeamMembersInput, GetTeamMembersOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in GetTeamMembersInput) (*mcp.CallToolResult, GetTeamMembersOutput, error) {
		if strings.TrimSpace(in.Org) == "" || strings.TrimSpace(in.TeamSlug) == "" {
			return nil, GetTeamMembersOutput{}, fmt.Errorf("github: org and team_slug are required")
		}

		opts := &gh.TeamListTeamMembersOptions{
			Role: in.Role,
			ListOptions: gh.ListOptions{
				PerPage: clampPerPage(in.PerPage),
				Page:    defaultInt(in.Page, 1),
			},
		}

		members, _, err := client.Teams.ListTeamMembersBySlug(ctx, in.Org, in.TeamSlug, opts)
		if err != nil {
			return nil, GetTeamMembersOutput{}, fmt.Errorf("github: list team members %s/%s: %w", in.Org, in.TeamSlug, err)
		}

		out := GetTeamMembersOutput{Members: make([]UserSummary, 0, len(members))}
		for _, u := range members {
			out.Members = append(out.Members, userSummary(u))
		}
		return nil, out, nil
	}
}

// ---------- github_search_users ----------

type SearchUsersInput struct {
	Query   string `json:"query" jsonschema:"GitHub user search query (e.g. 'language:go location:london')"`
	Sort    string `json:"sort,omitempty" jsonschema:"followers|repositories|joined"`
	Order   string `json:"order,omitempty" jsonschema:"asc|desc (default desc)"`
	PerPage int    `json:"per_page,omitempty" jsonschema:"1-100 (default 30)"`
	Page    int    `json:"page,omitempty" jsonschema:"1-based page (default 1)"`
}

type SearchUsersOutput struct {
	TotalCount        int           `json:"total_count"`
	IncompleteResults bool          `json:"incomplete_results"`
	Users             []UserSummary `json:"users"`
}

func searchUsers(client *gh.Client) mcp.ToolHandlerFor[SearchUsersInput, SearchUsersOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in SearchUsersInput) (*mcp.CallToolResult, SearchUsersOutput, error) {
		query := strings.TrimSpace(in.Query)
		if query == "" {
			return nil, SearchUsersOutput{}, fmt.Errorf("github: search query is required")
		}

		opts := &gh.SearchOptions{
			Sort:  in.Sort,
			Order: defaultStr(in.Order, "desc"),
			ListOptions: gh.ListOptions{
				PerPage: clampPerPage(in.PerPage),
				Page:    defaultInt(in.Page, 1),
			},
		}

		results, _, err := client.Search.Users(ctx, query, opts)
		if err != nil {
			return nil, SearchUsersOutput{}, fmt.Errorf("github: search users %q: %w", query, err)
		}

		out := SearchUsersOutput{
			TotalCount:        results.GetTotal(),
			IncompleteResults: results.GetIncompleteResults(),
			Users:             make([]UserSummary, 0, len(results.Users)),
		}
		for _, u := range results.Users {
			out.Users = append(out.Users, userSummary(u))
		}
		return nil, out, nil
	}
}

// ---------- helpers ----------

func userProfile(u *gh.User) UserProfile {
	if u == nil {
		return UserProfile{}
	}
	return UserProfile{
		Login:       u.GetLogin(),
		ID:          u.GetID(),
		Name:        u.GetName(),
		Email:       u.GetEmail(),
		Bio:         u.GetBio(),
		Company:     u.GetCompany(),
		Location:    u.GetLocation(),
		Blog:        u.GetBlog(),
		Type:        u.GetType(),
		URL:         u.GetHTMLURL(),
		PublicRepos: u.GetPublicRepos(),
		Followers:   u.GetFollowers(),
		Following:   u.GetFollowing(),
		CreatedAt:   formatTime(u.GetCreatedAt()),
		UpdatedAt:   formatTime(u.GetUpdatedAt()),
	}
}

func userSummary(u *gh.User) UserSummary {
	if u == nil {
		return UserSummary{}
	}
	return UserSummary{
		Login:   u.GetLogin(),
		ID:      u.GetID(),
		Type:    u.GetType(),
		HTMLURL: u.GetHTMLURL(),
	}
}

func teamSummary(t *gh.Team) TeamSummary {
	if t == nil {
		return TeamSummary{}
	}
	out := TeamSummary{
		ID:          t.GetID(),
		Name:        t.GetName(),
		Slug:        t.GetSlug(),
		Description: t.GetDescription(),
		Privacy:     t.GetPrivacy(),
		Permission:  t.GetPermission(),
		URL:         t.GetURL(),
		HTMLURL:     t.GetHTMLURL(),
	}
	if t.Organization != nil {
		out.Organization = t.Organization.GetLogin()
	}
	if t.Parent != nil {
		out.Parent = t.Parent.GetSlug()
	}
	return out
}
