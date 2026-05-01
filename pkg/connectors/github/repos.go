package github

import (
	"context"
	"fmt"
	"strings"
	"time"

	gh "github.com/google/go-github/v82/github"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	maxFileChars = 32000
	maxBodyChars = 16000
)

func addReposTools(server *mcp.Server, client *gh.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "github_get_file_contents",
		Description: "Get a file or directory at path in a repo. Files return decoded content (truncated for large files); directories return entry listings.",
	}, getFileContents(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "github_search_code",
		Description: "Search code across GitHub using GitHub's code search syntax.",
	}, searchCode(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "github_search_repositories",
		Description: "Search repositories using GitHub's search syntax.",
	}, searchRepositories(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "github_list_commits",
		Description: "List commits on a repo branch/path with optional author/since/until filters.",
	}, listCommits(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "github_get_commit",
		Description: "Get a single commit by SHA. With include_diff=true, includes per-file patches (truncated).",
	}, getCommit(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "github_list_branches",
		Description: "List branches in a repo. Optional protected filter.",
	}, listBranches(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "github_list_tags",
		Description: "List tags in a repo (lightweight, with referenced commit SHA and tarball URLs).",
	}, listTags(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "github_get_tag",
		Description: "Get details for a tag by name. Returns the ref's target commit SHA; for annotated tags also returns the tag message and tagger.",
	}, getTag(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "github_list_releases",
		Description: "List releases in a repo, most recent first.",
	}, listReleases(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "github_get_latest_release",
		Description: "Get the latest published (non-draft, non-prerelease) release for a repo.",
	}, getLatestRelease(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "github_get_release_by_tag",
		Description: "Get a release by its tag name.",
	}, getReleaseByTag(client))
}

// ---------- github_get_file_contents ----------

type GetFileContentsInput struct {
	Owner string `json:"owner" jsonschema:"repo owner (user or org)"`
	Repo  string `json:"repo" jsonschema:"repo name"`
	Path  string `json:"path,omitempty" jsonschema:"file or dir path; empty for repo root"`
	Ref   string `json:"ref,omitempty" jsonschema:"branch, tag, or commit SHA (default: repo default branch)"`
}

type DirEntry struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Size    int    `json:"size,omitempty"`
	SHA     string `json:"sha,omitempty"`
	HTMLURL string `json:"html_url,omitempty"`
}

type FileContents struct {
	Type      string     `json:"type"`
	Path      string     `json:"path"`
	Encoding  string     `json:"encoding,omitempty"`
	Size      int        `json:"size,omitempty"`
	Content   string     `json:"content,omitempty"`
	SHA       string     `json:"sha,omitempty"`
	HTMLURL   string     `json:"html_url,omitempty"`
	Entries   []DirEntry `json:"entries,omitempty"`
	Truncated bool       `json:"truncated,omitempty"`
}

func getFileContents(client *gh.Client) mcp.ToolHandlerFor[GetFileContentsInput, FileContents] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in GetFileContentsInput) (*mcp.CallToolResult, FileContents, error) {
		opts := &gh.RepositoryContentGetOptions{Ref: in.Ref}
		fileContent, dirContent, _, err := client.Repositories.GetContents(ctx, in.Owner, in.Repo, in.Path, opts)
		if err != nil {
			return nil, FileContents{}, fmt.Errorf("github: get contents %s/%s/%s: %w", in.Owner, in.Repo, in.Path, err)
		}

		if dirContent != nil {
			entries := make([]DirEntry, 0, len(dirContent))
			for _, e := range dirContent {
				entries = append(entries, DirEntry{
					Type:    e.GetType(),
					Name:    e.GetName(),
					Path:    e.GetPath(),
					Size:    e.GetSize(),
					SHA:     e.GetSHA(),
					HTMLURL: e.GetHTMLURL(),
				})
			}
			return nil, FileContents{
				Type:    "dir",
				Path:    in.Path,
				Entries: entries,
			}, nil
		}

		if fileContent == nil {
			return nil, FileContents{}, fmt.Errorf("github: get contents %s/%s/%s: empty response", in.Owner, in.Repo, in.Path)
		}

		decoded, err := fileContent.GetContent()
		if err != nil {
			return nil, FileContents{}, fmt.Errorf("github: decode contents %s/%s/%s: %w", in.Owner, in.Repo, in.Path, err)
		}
		body, truncated := truncateString(decoded, maxFileChars)

		return nil, FileContents{
			Type:      "file",
			Path:      fileContent.GetPath(),
			Encoding:  fileContent.GetEncoding(),
			Size:      fileContent.GetSize(),
			Content:   body,
			SHA:       fileContent.GetSHA(),
			HTMLURL:   fileContent.GetHTMLURL(),
			Truncated: truncated,
		}, nil
	}
}

// ---------- github_search_code ----------

type SearchCodeInput struct {
	Query   string `json:"query" jsonschema:"GitHub code search query (e.g. 'addClass repo:jquery/jquery')"`
	Sort    string `json:"sort,omitempty" jsonschema:"indexed (default best-match)"`
	Order   string `json:"order,omitempty" jsonschema:"asc|desc (default desc)"`
	PerPage int    `json:"per_page,omitempty" jsonschema:"1-100 (default 30)"`
	Page    int    `json:"page,omitempty" jsonschema:"1-based page (default 1)"`
}

type CodeHit struct {
	Name         string `json:"name"`
	Path         string `json:"path"`
	SHA          string `json:"sha,omitempty"`
	HTMLURL      string `json:"html_url,omitempty"`
	RepoFullName string `json:"repo_full_name,omitempty"`
}

type SearchCodeOutput struct {
	TotalCount        int       `json:"total_count"`
	IncompleteResults bool      `json:"incomplete_results"`
	Items             []CodeHit `json:"items"`
}

func searchCode(client *gh.Client) mcp.ToolHandlerFor[SearchCodeInput, SearchCodeOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in SearchCodeInput) (*mcp.CallToolResult, SearchCodeOutput, error) {
		query := strings.TrimSpace(in.Query)
		if query == "" {
			return nil, SearchCodeOutput{}, fmt.Errorf("github: search query is required")
		}

		opts := &gh.SearchOptions{
			Sort:  in.Sort,
			Order: defaultStr(in.Order, "desc"),
			ListOptions: gh.ListOptions{
				PerPage: clampPerPage(in.PerPage),
				Page:    defaultInt(in.Page, 1),
			},
		}

		results, _, err := client.Search.Code(ctx, query, opts)
		if err != nil {
			return nil, SearchCodeOutput{}, fmt.Errorf("github: search code %q: %w", query, err)
		}

		out := SearchCodeOutput{
			TotalCount:        results.GetTotal(),
			IncompleteResults: results.GetIncompleteResults(),
			Items:             make([]CodeHit, 0, len(results.CodeResults)),
		}
		for _, r := range results.CodeResults {
			hit := CodeHit{
				Name:    r.GetName(),
				Path:    r.GetPath(),
				SHA:     r.GetSHA(),
				HTMLURL: r.GetHTMLURL(),
			}
			if r.Repository != nil {
				hit.RepoFullName = r.Repository.GetFullName()
			}
			out.Items = append(out.Items, hit)
		}
		return nil, out, nil
	}
}

// ---------- github_search_repositories ----------

type SearchRepositoriesInput struct {
	Query   string `json:"query" jsonschema:"GitHub repo search query (e.g. 'topic:database language:go stars:>1000')"`
	Sort    string `json:"sort,omitempty" jsonschema:"stars|forks|help-wanted-issues|updated"`
	Order   string `json:"order,omitempty" jsonschema:"asc|desc (default desc)"`
	PerPage int    `json:"per_page,omitempty" jsonschema:"1-100 (default 30)"`
	Page    int    `json:"page,omitempty" jsonschema:"1-based page (default 1)"`
}

type RepoSummary struct {
	FullName      string `json:"full_name"`
	Description   string `json:"description,omitempty"`
	HTMLURL       string `json:"html_url,omitempty"`
	Language      string `json:"language,omitempty"`
	Stars         int    `json:"stars,omitempty"`
	Forks         int    `json:"forks,omitempty"`
	OpenIssues    int    `json:"open_issues,omitempty"`
	Archived      bool   `json:"archived,omitempty"`
	Fork          bool   `json:"fork,omitempty"`
	DefaultBranch string `json:"default_branch,omitempty"`
	UpdatedAt     string `json:"updated_at,omitempty"`
	PushedAt      string `json:"pushed_at,omitempty"`
}

type SearchRepositoriesOutput struct {
	TotalCount        int           `json:"total_count"`
	IncompleteResults bool          `json:"incomplete_results"`
	Items             []RepoSummary `json:"items"`
}

func searchRepositories(client *gh.Client) mcp.ToolHandlerFor[SearchRepositoriesInput, SearchRepositoriesOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in SearchRepositoriesInput) (*mcp.CallToolResult, SearchRepositoriesOutput, error) {
		query := strings.TrimSpace(in.Query)
		if query == "" {
			return nil, SearchRepositoriesOutput{}, fmt.Errorf("github: search query is required")
		}

		opts := &gh.SearchOptions{
			Sort:  in.Sort,
			Order: defaultStr(in.Order, "desc"),
			ListOptions: gh.ListOptions{
				PerPage: clampPerPage(in.PerPage),
				Page:    defaultInt(in.Page, 1),
			},
		}

		results, _, err := client.Search.Repositories(ctx, query, opts)
		if err != nil {
			return nil, SearchRepositoriesOutput{}, fmt.Errorf("github: search repos %q: %w", query, err)
		}

		out := SearchRepositoriesOutput{
			TotalCount:        results.GetTotal(),
			IncompleteResults: results.GetIncompleteResults(),
			Items:             make([]RepoSummary, 0, len(results.Repositories)),
		}
		for _, r := range results.Repositories {
			out.Items = append(out.Items, repoSummary(r))
		}
		return nil, out, nil
	}
}

// ---------- github_list_commits ----------

type ListCommitsInput struct {
	Owner   string `json:"owner" jsonschema:"repo owner"`
	Repo    string `json:"repo" jsonschema:"repo name"`
	SHA     string `json:"sha,omitempty" jsonschema:"branch name or commit SHA to start from"`
	Path    string `json:"path,omitempty" jsonschema:"only commits touching this path"`
	Author  string `json:"author,omitempty" jsonschema:"GitHub login or email of author"`
	Since   string `json:"since,omitempty" jsonschema:"RFC3339 lower bound (inclusive)"`
	Until   string `json:"until,omitempty" jsonschema:"RFC3339 upper bound (inclusive)"`
	PerPage int    `json:"per_page,omitempty" jsonschema:"1-100 (default 30)"`
	Page    int    `json:"page,omitempty" jsonschema:"1-based page (default 1)"`
}

type CommitSummary struct {
	SHA         string `json:"sha"`
	Author      string `json:"author,omitempty"`
	AuthorEmail string `json:"author_email,omitempty"`
	Message     string `json:"message"`
	Date        string `json:"date,omitempty"`
	HTMLURL     string `json:"html_url,omitempty"`
}

type ListCommitsOutput struct {
	Commits []CommitSummary `json:"commits"`
}

func listCommits(client *gh.Client) mcp.ToolHandlerFor[ListCommitsInput, ListCommitsOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ListCommitsInput) (*mcp.CallToolResult, ListCommitsOutput, error) {
		opts := &gh.CommitsListOptions{
			SHA:    in.SHA,
			Path:   in.Path,
			Author: in.Author,
			ListOptions: gh.ListOptions{
				PerPage: clampPerPage(in.PerPage),
				Page:    defaultInt(in.Page, 1),
			},
		}
		if in.Since != "" {
			t, err := time.Parse(time.RFC3339, in.Since)
			if err != nil {
				return nil, ListCommitsOutput{}, fmt.Errorf("github: parse since %q: %w", in.Since, err)
			}
			opts.Since = t
		}
		if in.Until != "" {
			t, err := time.Parse(time.RFC3339, in.Until)
			if err != nil {
				return nil, ListCommitsOutput{}, fmt.Errorf("github: parse until %q: %w", in.Until, err)
			}
			opts.Until = t
		}

		commits, _, err := client.Repositories.ListCommits(ctx, in.Owner, in.Repo, opts)
		if err != nil {
			return nil, ListCommitsOutput{}, fmt.Errorf("github: list commits %s/%s: %w", in.Owner, in.Repo, err)
		}

		out := ListCommitsOutput{Commits: make([]CommitSummary, 0, len(commits))}
		for _, c := range commits {
			out.Commits = append(out.Commits, commitSummary(c))
		}
		return nil, out, nil
	}
}

// ---------- github_get_commit ----------

type GetCommitInput struct {
	Owner       string `json:"owner" jsonschema:"repo owner"`
	Repo        string `json:"repo" jsonschema:"repo name"`
	SHA         string `json:"sha" jsonschema:"commit SHA (full or short)"`
	IncludeDiff bool   `json:"include_diff,omitempty" jsonschema:"if true, include per-file patches (truncated)"`
}

type CommitFileChange struct {
	Filename       string `json:"filename"`
	Status         string `json:"status"`
	Additions      int    `json:"additions"`
	Deletions      int    `json:"deletions"`
	Patch          string `json:"patch,omitempty"`
	PatchTruncated bool   `json:"patch_truncated,omitempty"`
}

type CommitDetail struct {
	SHA         string             `json:"sha"`
	Author      string             `json:"author,omitempty"`
	AuthorEmail string             `json:"author_email,omitempty"`
	Committer   string             `json:"committer,omitempty"`
	Message     string             `json:"message"`
	Date        string             `json:"date,omitempty"`
	HTMLURL     string             `json:"html_url,omitempty"`
	Additions   int                `json:"additions,omitempty"`
	Deletions   int                `json:"deletions,omitempty"`
	Total       int                `json:"total,omitempty"`
	Files       []CommitFileChange `json:"files,omitempty"`
}

func getCommit(client *gh.Client) mcp.ToolHandlerFor[GetCommitInput, CommitDetail] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in GetCommitInput) (*mcp.CallToolResult, CommitDetail, error) {
		c, _, err := client.Repositories.GetCommit(ctx, in.Owner, in.Repo, in.SHA, &gh.ListOptions{PerPage: listPerPageCap})
		if err != nil {
			return nil, CommitDetail{}, fmt.Errorf("github: get commit %s/%s@%s: %w", in.Owner, in.Repo, in.SHA, err)
		}

		out := CommitDetail{
			SHA:     c.GetSHA(),
			Message: c.GetCommit().GetMessage(),
			HTMLURL: c.GetHTMLURL(),
		}
		if a := c.GetCommit().GetAuthor(); a != nil {
			out.Author = a.GetName()
			out.AuthorEmail = a.GetEmail()
			out.Date = formatTime(a.GetDate())
		}
		if a := c.Author; a != nil && out.Author == "" {
			out.Author = a.GetLogin()
		}
		if cm := c.GetCommit().GetCommitter(); cm != nil {
			out.Committer = cm.GetName()
		}
		if s := c.Stats; s != nil {
			out.Additions = s.GetAdditions()
			out.Deletions = s.GetDeletions()
			out.Total = s.GetTotal()
		}
		if in.IncludeDiff {
			out.Files = make([]CommitFileChange, 0, len(c.Files))
			for _, f := range c.Files {
				patch, truncated := truncateString(f.GetPatch(), maxPatchChars)
				out.Files = append(out.Files, CommitFileChange{
					Filename:       f.GetFilename(),
					Status:         f.GetStatus(),
					Additions:      f.GetAdditions(),
					Deletions:      f.GetDeletions(),
					Patch:          patch,
					PatchTruncated: truncated,
				})
			}
		}
		return nil, out, nil
	}
}

// ---------- github_list_branches ----------

type ListBranchesInput struct {
	Owner     string `json:"owner" jsonschema:"repo owner"`
	Repo      string `json:"repo" jsonschema:"repo name"`
	Protected *bool  `json:"protected,omitempty" jsonschema:"if set, filter to protected (true) or unprotected (false) only"`
	PerPage   int    `json:"per_page,omitempty" jsonschema:"1-100 (default 30)"`
	Page      int    `json:"page,omitempty" jsonschema:"1-based page (default 1)"`
}

type BranchSummary struct {
	Name      string `json:"name"`
	SHA       string `json:"sha,omitempty"`
	Protected bool   `json:"protected,omitempty"`
}

type ListBranchesOutput struct {
	Branches []BranchSummary `json:"branches"`
}

func listBranches(client *gh.Client) mcp.ToolHandlerFor[ListBranchesInput, ListBranchesOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ListBranchesInput) (*mcp.CallToolResult, ListBranchesOutput, error) {
		opts := &gh.BranchListOptions{
			Protected: in.Protected,
			ListOptions: gh.ListOptions{
				PerPage: clampPerPage(in.PerPage),
				Page:    defaultInt(in.Page, 1),
			},
		}

		branches, _, err := client.Repositories.ListBranches(ctx, in.Owner, in.Repo, opts)
		if err != nil {
			return nil, ListBranchesOutput{}, fmt.Errorf("github: list branches %s/%s: %w", in.Owner, in.Repo, err)
		}

		out := ListBranchesOutput{Branches: make([]BranchSummary, 0, len(branches))}
		for _, b := range branches {
			summary := BranchSummary{
				Name:      b.GetName(),
				Protected: b.GetProtected(),
			}
			if b.Commit != nil {
				summary.SHA = b.Commit.GetSHA()
			}
			out.Branches = append(out.Branches, summary)
		}
		return nil, out, nil
	}
}

// ---------- github_list_tags ----------

type ListTagsInput struct {
	Owner   string `json:"owner" jsonschema:"repo owner"`
	Repo    string `json:"repo" jsonschema:"repo name"`
	PerPage int    `json:"per_page,omitempty" jsonschema:"1-100 (default 30)"`
	Page    int    `json:"page,omitempty" jsonschema:"1-based page (default 1)"`
}

type TagSummary struct {
	Name       string `json:"name"`
	SHA        string `json:"sha,omitempty"`
	TarballURL string `json:"tarball_url,omitempty"`
	ZipballURL string `json:"zipball_url,omitempty"`
}

type ListTagsOutput struct {
	Tags []TagSummary `json:"tags"`
}

func listTags(client *gh.Client) mcp.ToolHandlerFor[ListTagsInput, ListTagsOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ListTagsInput) (*mcp.CallToolResult, ListTagsOutput, error) {
		opts := &gh.ListOptions{
			PerPage: clampPerPage(in.PerPage),
			Page:    defaultInt(in.Page, 1),
		}

		tags, _, err := client.Repositories.ListTags(ctx, in.Owner, in.Repo, opts)
		if err != nil {
			return nil, ListTagsOutput{}, fmt.Errorf("github: list tags %s/%s: %w", in.Owner, in.Repo, err)
		}

		out := ListTagsOutput{Tags: make([]TagSummary, 0, len(tags))}
		for _, t := range tags {
			summary := TagSummary{
				Name:       t.GetName(),
				TarballURL: t.GetTarballURL(),
				ZipballURL: t.GetZipballURL(),
			}
			if t.Commit != nil {
				summary.SHA = t.Commit.GetSHA()
			}
			out.Tags = append(out.Tags, summary)
		}
		return nil, out, nil
	}
}

// ---------- github_get_tag ----------

type GetTagInput struct {
	Owner string `json:"owner" jsonschema:"repo owner"`
	Repo  string `json:"repo" jsonschema:"repo name"`
	Tag   string `json:"tag" jsonschema:"tag name (e.g. 'v1.0.0')"`
}

type GetTagOutput struct {
	Tag         string `json:"tag"`
	Type        string `json:"type"`
	SHA         string `json:"sha"`
	Message     string `json:"message,omitempty"`
	TaggerName  string `json:"tagger_name,omitempty"`
	TaggerEmail string `json:"tagger_email,omitempty"`
	TaggedAt    string `json:"tagged_at,omitempty"`
}

func getTag(client *gh.Client) mcp.ToolHandlerFor[GetTagInput, GetTagOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in GetTagInput) (*mcp.CallToolResult, GetTagOutput, error) {
		ref, _, err := client.Git.GetRef(ctx, in.Owner, in.Repo, "tags/"+in.Tag)
		if err != nil {
			return nil, GetTagOutput{}, fmt.Errorf("github: get tag ref %s/%s@%s: %w", in.Owner, in.Repo, in.Tag, err)
		}

		out := GetTagOutput{Tag: in.Tag}
		if obj := ref.Object; obj != nil {
			out.Type = obj.GetType()
			out.SHA = obj.GetSHA()
		}

		if out.Type == "tag" && out.SHA != "" {
			t, _, err := client.Git.GetTag(ctx, in.Owner, in.Repo, out.SHA)
			if err != nil {
				return nil, GetTagOutput{}, fmt.Errorf("github: get annotated tag %s/%s@%s: %w", in.Owner, in.Repo, in.Tag, err)
			}
			out.Message = t.GetMessage()
			if obj := t.GetObject(); obj != nil {
				out.SHA = obj.GetSHA()
			}
			if tg := t.GetTagger(); tg != nil {
				out.TaggerName = tg.GetName()
				out.TaggerEmail = tg.GetEmail()
				out.TaggedAt = formatTime(tg.GetDate())
			}
		}
		return nil, out, nil
	}
}

// ---------- github_list_releases ----------

type ListReleasesInput struct {
	Owner   string `json:"owner" jsonschema:"repo owner"`
	Repo    string `json:"repo" jsonschema:"repo name"`
	PerPage int    `json:"per_page,omitempty" jsonschema:"1-100 (default 30)"`
	Page    int    `json:"page,omitempty" jsonschema:"1-based page (default 1)"`
}

type ReleaseSummary struct {
	ID            int64  `json:"id"`
	TagName       string `json:"tag_name"`
	Name          string `json:"name,omitempty"`
	Body          string `json:"body,omitempty"`
	BodyTruncated bool   `json:"body_truncated,omitempty"`
	Draft         bool   `json:"draft,omitempty"`
	Prerelease    bool   `json:"prerelease,omitempty"`
	Author        string `json:"author,omitempty"`
	HTMLURL       string `json:"html_url,omitempty"`
	TarballURL    string `json:"tarball_url,omitempty"`
	ZipballURL    string `json:"zipball_url,omitempty"`
	CreatedAt     string `json:"created_at,omitempty"`
	PublishedAt   string `json:"published_at,omitempty"`
}

type ListReleasesOutput struct {
	Releases []ReleaseSummary `json:"releases"`
}

func listReleases(client *gh.Client) mcp.ToolHandlerFor[ListReleasesInput, ListReleasesOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ListReleasesInput) (*mcp.CallToolResult, ListReleasesOutput, error) {
		opts := &gh.ListOptions{
			PerPage: clampPerPage(in.PerPage),
			Page:    defaultInt(in.Page, 1),
		}

		releases, _, err := client.Repositories.ListReleases(ctx, in.Owner, in.Repo, opts)
		if err != nil {
			return nil, ListReleasesOutput{}, fmt.Errorf("github: list releases %s/%s: %w", in.Owner, in.Repo, err)
		}

		out := ListReleasesOutput{Releases: make([]ReleaseSummary, 0, len(releases))}
		for _, r := range releases {
			out.Releases = append(out.Releases, releaseSummary(r))
		}
		return nil, out, nil
	}
}

// ---------- github_get_latest_release ----------

type GetLatestReleaseInput struct {
	Owner string `json:"owner" jsonschema:"repo owner"`
	Repo  string `json:"repo" jsonschema:"repo name"`
}

func getLatestRelease(client *gh.Client) mcp.ToolHandlerFor[GetLatestReleaseInput, ReleaseSummary] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in GetLatestReleaseInput) (*mcp.CallToolResult, ReleaseSummary, error) {
		r, _, err := client.Repositories.GetLatestRelease(ctx, in.Owner, in.Repo)
		if err != nil {
			return nil, ReleaseSummary{}, fmt.Errorf("github: get latest release %s/%s: %w", in.Owner, in.Repo, err)
		}
		return nil, releaseSummary(r), nil
	}
}

// ---------- github_get_release_by_tag ----------

type GetReleaseByTagInput struct {
	Owner string `json:"owner" jsonschema:"repo owner"`
	Repo  string `json:"repo" jsonschema:"repo name"`
	Tag   string `json:"tag" jsonschema:"tag name"`
}

func getReleaseByTag(client *gh.Client) mcp.ToolHandlerFor[GetReleaseByTagInput, ReleaseSummary] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in GetReleaseByTagInput) (*mcp.CallToolResult, ReleaseSummary, error) {
		r, _, err := client.Repositories.GetReleaseByTag(ctx, in.Owner, in.Repo, in.Tag)
		if err != nil {
			return nil, ReleaseSummary{}, fmt.Errorf("github: get release by tag %s/%s@%s: %w", in.Owner, in.Repo, in.Tag, err)
		}
		return nil, releaseSummary(r), nil
	}
}

// ---------- helpers ----------

func repoSummary(r *gh.Repository) RepoSummary {
	if r == nil {
		return RepoSummary{}
	}
	return RepoSummary{
		FullName:      r.GetFullName(),
		Description:   r.GetDescription(),
		HTMLURL:       r.GetHTMLURL(),
		Language:      r.GetLanguage(),
		Stars:         r.GetStargazersCount(),
		Forks:         r.GetForksCount(),
		OpenIssues:    r.GetOpenIssuesCount(),
		Archived:      r.GetArchived(),
		Fork:          r.GetFork(),
		DefaultBranch: r.GetDefaultBranch(),
		UpdatedAt:     formatTime(r.GetUpdatedAt()),
		PushedAt:      formatTime(r.GetPushedAt()),
	}
}

func commitSummary(c *gh.RepositoryCommit) CommitSummary {
	if c == nil {
		return CommitSummary{}
	}
	out := CommitSummary{
		SHA:     c.GetSHA(),
		HTMLURL: c.GetHTMLURL(),
		Message: c.GetCommit().GetMessage(),
	}
	if a := c.GetCommit().GetAuthor(); a != nil {
		out.Author = a.GetName()
		out.AuthorEmail = a.GetEmail()
		out.Date = formatTime(a.GetDate())
	}
	if a := c.Author; a != nil && out.Author == "" {
		out.Author = a.GetLogin()
	}
	return out
}

func releaseSummary(r *gh.RepositoryRelease) ReleaseSummary {
	if r == nil {
		return ReleaseSummary{}
	}
	body, truncated := truncateString(r.GetBody(), maxBodyChars)
	out := ReleaseSummary{
		ID:            r.GetID(),
		TagName:       r.GetTagName(),
		Name:          r.GetName(),
		Body:          body,
		BodyTruncated: truncated,
		Draft:         r.GetDraft(),
		Prerelease:    r.GetPrerelease(),
		HTMLURL:       r.GetHTMLURL(),
		TarballURL:    r.GetTarballURL(),
		ZipballURL:    r.GetZipballURL(),
		CreatedAt:     formatTime(r.GetCreatedAt()),
		PublishedAt:   formatTime(r.GetPublishedAt()),
	}
	if r.Author != nil {
		out.Author = r.Author.GetLogin()
	}
	return out
}
