package github

import (
	"context"
	"fmt"
	"strings"

	gh "github.com/google/go-github/v82/github"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxPatchChars = 8000

func addPullRequestTools(server *mcp.Server, client *gh.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "github_list_pull_requests",
		Description: "List pull requests for a repo. Returns summaries (number, title, state, author, base/head, draft, URL, timestamps).",
	}, listPRs(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "github_read_pull_request",
		Description: "Read a single pull request in full: metadata, body, changed files with patches, reviews, review comments, and issue comments.",
	}, readPR(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "github_search_pull_requests",
		Description: "Search pull requests using GitHub's search syntax. The query is automatically scoped to PRs (is:pr is prepended if absent).",
	}, searchPRs(client))
}

// ---------- github_pr_list ----------

type ListInput struct {
	Owner     string `json:"owner" jsonschema:"repo owner (user or org)"`
	Repo      string `json:"repo" jsonschema:"repo name"`
	State     string `json:"state,omitempty" jsonschema:"open|closed|all (default open)"`
	Sort      string `json:"sort,omitempty" jsonschema:"created|updated|popularity|long-running (default updated)"`
	Direction string `json:"direction,omitempty" jsonschema:"asc|desc (default desc)"`
	PerPage   int    `json:"per_page,omitempty" jsonschema:"1-100 (default 30)"`
	Page      int    `json:"page,omitempty" jsonschema:"1-based page (default 1)"`
}

type PullRequestSummary struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	State     string `json:"state"`
	Author    string `json:"author"`
	Base      string `json:"base"`
	Head      string `json:"head"`
	Draft     bool   `json:"draft"`
	URL       string `json:"url"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type ListOutput struct {
	PullRequests []PullRequestSummary `json:"pull_requests"`
}

func listPRs(client *gh.Client) mcp.ToolHandlerFor[ListInput, ListOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ListInput) (*mcp.CallToolResult, ListOutput, error) {
		opts := &gh.PullRequestListOptions{
			State:     defaultStr(in.State, "open"),
			Sort:      defaultStr(in.Sort, "updated"),
			Direction: defaultStr(in.Direction, "desc"),
			ListOptions: gh.ListOptions{
				PerPage: clampPerPage(in.PerPage),
				Page:    defaultInt(in.Page, 1),
			},
		}

		prs, _, err := client.PullRequests.List(ctx, in.Owner, in.Repo, opts)
		if err != nil {
			return nil, ListOutput{}, fmt.Errorf("github: list PRs %s/%s: %w", in.Owner, in.Repo, err)
		}

		out := ListOutput{PullRequests: make([]PullRequestSummary, 0, len(prs))}
		for _, pr := range prs {
			out.PullRequests = append(out.PullRequests, PullRequestSummary{
				Number:    pr.GetNumber(),
				Title:     pr.GetTitle(),
				State:     pr.GetState(),
				Author:    pr.GetUser().GetLogin(),
				Base:      pr.GetBase().GetRef(),
				Head:      pr.GetHead().GetRef(),
				Draft:     pr.GetDraft(),
				URL:       pr.GetHTMLURL(),
				CreatedAt: formatTime(pr.GetCreatedAt()),
				UpdatedAt: formatTime(pr.GetUpdatedAt()),
			})
		}
		return nil, out, nil
	}
}

// ---------- github_pr_read ----------

type ReadInput struct {
	Owner  string `json:"owner" jsonschema:"repo owner (user or org)"`
	Repo   string `json:"repo" jsonschema:"repo name"`
	Number int    `json:"number" jsonschema:"PR number"`
}

type ChangedFile struct {
	Filename       string `json:"filename"`
	Status         string `json:"status"`
	Additions      int    `json:"additions"`
	Deletions      int    `json:"deletions"`
	Patch          string `json:"patch,omitempty"`
	PatchTruncated bool   `json:"patch_truncated,omitempty"`
}

type Review struct {
	User        string `json:"user"`
	State       string `json:"state"`
	Body        string `json:"body,omitempty"`
	SubmittedAt string `json:"submitted_at"`
}

type ReviewComment struct {
	User string `json:"user"`
	Body string `json:"body"`
	Path string `json:"path"`
	Line int    `json:"line,omitempty"`
}

type IssueComment struct {
	User      string `json:"user"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
}

type ReadOutput struct {
	Number         int             `json:"number"`
	Title          string          `json:"title"`
	State          string          `json:"state"`
	Author         string          `json:"author"`
	Body           string          `json:"body"`
	Base           string          `json:"base"`
	Head           string          `json:"head"`
	Draft          bool            `json:"draft"`
	Merged         bool            `json:"merged"`
	URL            string          `json:"url"`
	CreatedAt      string          `json:"created_at"`
	UpdatedAt      string          `json:"updated_at"`
	Additions      int             `json:"additions"`
	Deletions      int             `json:"deletions"`
	ChangedFiles   []ChangedFile   `json:"changed_files"`
	Reviews        []Review        `json:"reviews"`
	ReviewComments []ReviewComment `json:"review_comments"`
	IssueComments  []IssueComment  `json:"issue_comments"`
}

func readPR(client *gh.Client) mcp.ToolHandlerFor[ReadInput, ReadOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ReadInput) (*mcp.CallToolResult, ReadOutput, error) {
		pr, _, err := client.PullRequests.Get(ctx, in.Owner, in.Repo, in.Number)
		if err != nil {
			return nil, ReadOutput{}, fmt.Errorf("github: get PR %s/%s#%d: %w", in.Owner, in.Repo, in.Number, err)
		}

		fileOpts := &gh.ListOptions{PerPage: listPerPageCap}
		files, _, err := client.PullRequests.ListFiles(ctx, in.Owner, in.Repo, in.Number, fileOpts)
		if err != nil {
			return nil, ReadOutput{}, fmt.Errorf("github: list files %s/%s#%d: %w", in.Owner, in.Repo, in.Number, err)
		}

		reviews, _, err := client.PullRequests.ListReviews(ctx, in.Owner, in.Repo, in.Number, fileOpts)
		if err != nil {
			return nil, ReadOutput{}, fmt.Errorf("github: list reviews %s/%s#%d: %w", in.Owner, in.Repo, in.Number, err)
		}

		reviewComments, _, err := client.PullRequests.ListComments(ctx, in.Owner, in.Repo, in.Number, &gh.PullRequestListCommentsOptions{
			ListOptions: gh.ListOptions{PerPage: listPerPageCap},
		})
		if err != nil {
			return nil, ReadOutput{}, fmt.Errorf("github: list review comments %s/%s#%d: %w", in.Owner, in.Repo, in.Number, err)
		}

		issueComments, _, err := client.Issues.ListComments(ctx, in.Owner, in.Repo, in.Number, &gh.IssueListCommentsOptions{
			ListOptions: gh.ListOptions{PerPage: listPerPageCap},
		})
		if err != nil {
			return nil, ReadOutput{}, fmt.Errorf("github: list issue comments %s/%s#%d: %w", in.Owner, in.Repo, in.Number, err)
		}

		out := ReadOutput{
			Number:         pr.GetNumber(),
			Title:          pr.GetTitle(),
			State:          pr.GetState(),
			Author:         pr.GetUser().GetLogin(),
			Body:           pr.GetBody(),
			Base:           pr.GetBase().GetRef(),
			Head:           pr.GetHead().GetRef(),
			Draft:          pr.GetDraft(),
			Merged:         pr.GetMerged(),
			URL:            pr.GetHTMLURL(),
			CreatedAt:      formatTime(pr.GetCreatedAt()),
			UpdatedAt:      formatTime(pr.GetUpdatedAt()),
			Additions:      pr.GetAdditions(),
			Deletions:      pr.GetDeletions(),
			ChangedFiles:   make([]ChangedFile, 0, len(files)),
			Reviews:        make([]Review, 0, len(reviews)),
			ReviewComments: make([]ReviewComment, 0, len(reviewComments)),
			IssueComments:  make([]IssueComment, 0, len(issueComments)),
		}

		for _, f := range files {
			patch, truncated := truncateString(f.GetPatch(), maxPatchChars)
			out.ChangedFiles = append(out.ChangedFiles, ChangedFile{
				Filename:       f.GetFilename(),
				Status:         f.GetStatus(),
				Additions:      f.GetAdditions(),
				Deletions:      f.GetDeletions(),
				Patch:          patch,
				PatchTruncated: truncated,
			})
		}

		for _, r := range reviews {
			out.Reviews = append(out.Reviews, Review{
				User:        r.GetUser().GetLogin(),
				State:       r.GetState(),
				Body:        r.GetBody(),
				SubmittedAt: formatTime(r.GetSubmittedAt()),
			})
		}

		for _, c := range reviewComments {
			out.ReviewComments = append(out.ReviewComments, ReviewComment{
				User: c.GetUser().GetLogin(),
				Body: c.GetBody(),
				Path: c.GetPath(),
				Line: c.GetLine(),
			})
		}

		for _, c := range issueComments {
			out.IssueComments = append(out.IssueComments, IssueComment{
				User:      c.GetUser().GetLogin(),
				Body:      c.GetBody(),
				CreatedAt: formatTime(c.GetCreatedAt()),
			})
		}

		return nil, out, nil
	}
}

// ---------- github_pr_search ----------

type SearchInput struct {
	Query   string `json:"query" jsonschema:"GitHub search query (is:pr is auto-prepended if missing)"`
	Sort    string `json:"sort,omitempty" jsonschema:"comments|reactions|created|updated"`
	Order   string `json:"order,omitempty" jsonschema:"asc|desc (default desc)"`
	PerPage int    `json:"per_page,omitempty" jsonschema:"1-100 (default 30)"`
}

type SearchResultItem struct {
	Number       int    `json:"number"`
	Title        string `json:"title"`
	State        string `json:"state"`
	Author       string `json:"author"`
	RepoFullName string `json:"repo_full_name"`
	URL          string `json:"url"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

type SearchOutput struct {
	TotalCount        int                `json:"total_count"`
	IncompleteResults bool               `json:"incomplete_results"`
	Items             []SearchResultItem `json:"items"`
}

func searchPRs(client *gh.Client) mcp.ToolHandlerFor[SearchInput, SearchOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in SearchInput) (*mcp.CallToolResult, SearchOutput, error) {
		query := strings.TrimSpace(in.Query)
		if query == "" {
			return nil, SearchOutput{}, fmt.Errorf("github: search query is required")
		}
		if !hasPRScope(query) {
			query = "is:pr " + query
		}

		opts := &gh.SearchOptions{
			Sort:  in.Sort,
			Order: defaultStr(in.Order, "desc"),
			ListOptions: gh.ListOptions{
				PerPage: clampPerPage(in.PerPage),
			},
		}

		results, _, err := client.Search.Issues(ctx, query, opts)
		if err != nil {
			return nil, SearchOutput{}, fmt.Errorf("github: search PRs %q: %w", query, err)
		}

		out := SearchOutput{
			TotalCount:        results.GetTotal(),
			IncompleteResults: results.GetIncompleteResults(),
			Items:             make([]SearchResultItem, 0, len(results.Issues)),
		}
		for _, issue := range results.Issues {
			out.Items = append(out.Items, SearchResultItem{
				Number:       issue.GetNumber(),
				Title:        issue.GetTitle(),
				State:        issue.GetState(),
				Author:       issue.GetUser().GetLogin(),
				RepoFullName: repoFullNameFromURL(issue.GetRepositoryURL()),
				URL:          issue.GetHTMLURL(),
				CreatedAt:    formatTime(issue.GetCreatedAt()),
				UpdatedAt:    formatTime(issue.GetUpdatedAt()),
			})
		}
		return nil, out, nil
	}
}

// ---------- helpers ----------

func hasPRScope(query string) bool {
	for _, tok := range strings.Fields(strings.ToLower(query)) {
		if tok == "is:pr" {
			return true
		}
	}
	return false
}

func repoFullNameFromURL(url string) string {
	const prefix = "https://api.github.com/repos/"
	return strings.TrimPrefix(url, prefix)
}
