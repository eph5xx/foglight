package github

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	gh "github.com/google/go-github/v82/github"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxLogChars = 32000

func addActionsTools(server *mcp.Server, name string, client *gh.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        name + "_get_action",
		Description: "Get a single Actions resource. method selects which: workflow, run, job, artifact, or logs (alias for fetching a single job's logs).",
	}, getAction(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        name + "_list_actions",
		Description: "List Actions resources. method selects which: workflows, runs, jobs (requires run_id), or artifacts (requires run_id).",
	}, listActions(client))

	mcp.AddTool(server, &mcp.Tool{
		Name:        name + "_get_job_logs",
		Description: "Download workflow job logs. Provide job_id for a single job, or run_id (optionally with failed_only=true) for all/failed jobs in a run. Logs are truncated.",
	}, getJobLogs(client))
}

// ---------- github_get_action ----------

type GetActionInput struct {
	Owner  string `json:"owner" jsonschema:"repo owner"`
	Repo   string `json:"repo" jsonschema:"repo name"`
	Method string `json:"method" jsonschema:"workflow|run|job|artifact|logs"`
	ID     int64  `json:"id,omitempty" jsonschema:"workflow/run/job/artifact id (required for non-workflow-by-name)"`
	Name   string `json:"name,omitempty" jsonschema:"workflow file name (e.g. 'ci.yml'); if set with method=workflow, used instead of id"`
}

type WorkflowSummary struct {
	ID        int64  `json:"id"`
	Name      string `json:"name,omitempty"`
	Path      string `json:"path,omitempty"`
	State     string `json:"state,omitempty"`
	HTMLURL   string `json:"html_url,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type RunSummary struct {
	ID           int64  `json:"id"`
	Name         string `json:"name,omitempty"`
	HeadBranch   string `json:"head_branch,omitempty"`
	HeadSHA      string `json:"head_sha,omitempty"`
	Event        string `json:"event,omitempty"`
	Status       string `json:"status,omitempty"`
	Conclusion   string `json:"conclusion,omitempty"`
	WorkflowID   int64  `json:"workflow_id,omitempty"`
	RunNumber    int    `json:"run_number,omitempty"`
	RunAttempt   int    `json:"run_attempt,omitempty"`
	Actor        string `json:"actor,omitempty"`
	HTMLURL      string `json:"html_url,omitempty"`
	CreatedAt    string `json:"created_at,omitempty"`
	UpdatedAt    string `json:"updated_at,omitempty"`
	RunStartedAt string `json:"run_started_at,omitempty"`
}

type JobStep struct {
	Name        string `json:"name"`
	Status      string `json:"status,omitempty"`
	Conclusion  string `json:"conclusion,omitempty"`
	Number      int64  `json:"number,omitempty"`
	StartedAt   string `json:"started_at,omitempty"`
	CompletedAt string `json:"completed_at,omitempty"`
}

type JobSummary struct {
	ID           int64     `json:"id"`
	RunID        int64     `json:"run_id,omitempty"`
	Name         string    `json:"name,omitempty"`
	WorkflowName string    `json:"workflow_name,omitempty"`
	Status       string    `json:"status,omitempty"`
	Conclusion   string    `json:"conclusion,omitempty"`
	HeadSHA      string    `json:"head_sha,omitempty"`
	HTMLURL      string    `json:"html_url,omitempty"`
	StartedAt    string    `json:"started_at,omitempty"`
	CompletedAt  string    `json:"completed_at,omitempty"`
	Steps        []JobStep `json:"steps,omitempty"`
}

type ArtifactSummary struct {
	ID                 int64  `json:"id"`
	Name               string `json:"name,omitempty"`
	SizeInBytes        int64  `json:"size_in_bytes,omitempty"`
	Expired            bool   `json:"expired,omitempty"`
	ArchiveDownloadURL string `json:"archive_download_url,omitempty"`
	CreatedAt          string `json:"created_at,omitempty"`
	UpdatedAt          string `json:"updated_at,omitempty"`
	ExpiresAt          string `json:"expires_at,omitempty"`
}

type GetActionOutput struct {
	Method        string           `json:"method"`
	Workflow      *WorkflowSummary `json:"workflow,omitempty"`
	Run           *RunSummary      `json:"run,omitempty"`
	Job           *JobSummary      `json:"job,omitempty"`
	Artifact      *ArtifactSummary `json:"artifact,omitempty"`
	Logs          string           `json:"logs,omitempty"`
	LogsTruncated bool             `json:"logs_truncated,omitempty"`
}

func getAction(client *gh.Client) mcp.ToolHandlerFor[GetActionInput, GetActionOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in GetActionInput) (*mcp.CallToolResult, GetActionOutput, error) {
		method := strings.ToLower(strings.TrimSpace(in.Method))
		out := GetActionOutput{Method: method}

		switch method {
		case "workflow":
			if in.Name != "" {
				w, _, err := client.Actions.GetWorkflowByFileName(ctx, in.Owner, in.Repo, in.Name)
				if err != nil {
					return nil, out, fmt.Errorf("github: get workflow %s/%s/%s: %w", in.Owner, in.Repo, in.Name, err)
				}
				s := workflowSummary(w)
				out.Workflow = &s
				return nil, out, nil
			}
			if in.ID == 0 {
				return nil, out, fmt.Errorf("github: get workflow requires id or name")
			}
			w, _, err := client.Actions.GetWorkflowByID(ctx, in.Owner, in.Repo, in.ID)
			if err != nil {
				return nil, out, fmt.Errorf("github: get workflow %s/%s#%d: %w", in.Owner, in.Repo, in.ID, err)
			}
			s := workflowSummary(w)
			out.Workflow = &s

		case "run":
			if in.ID == 0 {
				return nil, out, fmt.Errorf("github: get run requires id")
			}
			r, _, err := client.Actions.GetWorkflowRunByID(ctx, in.Owner, in.Repo, in.ID)
			if err != nil {
				return nil, out, fmt.Errorf("github: get run %s/%s#%d: %w", in.Owner, in.Repo, in.ID, err)
			}
			s := runSummary(r)
			out.Run = &s

		case "job":
			if in.ID == 0 {
				return nil, out, fmt.Errorf("github: get job requires id")
			}
			j, _, err := client.Actions.GetWorkflowJobByID(ctx, in.Owner, in.Repo, in.ID)
			if err != nil {
				return nil, out, fmt.Errorf("github: get job %s/%s#%d: %w", in.Owner, in.Repo, in.ID, err)
			}
			s := jobSummary(j)
			out.Job = &s

		case "artifact":
			if in.ID == 0 {
				return nil, out, fmt.Errorf("github: get artifact requires id")
			}
			a, _, err := client.Actions.GetArtifact(ctx, in.Owner, in.Repo, in.ID)
			if err != nil {
				return nil, out, fmt.Errorf("github: get artifact %s/%s#%d: %w", in.Owner, in.Repo, in.ID, err)
			}
			s := artifactSummary(a)
			out.Artifact = &s

		case "logs":
			if in.ID == 0 {
				return nil, out, fmt.Errorf("github: get logs requires job id")
			}
			logs, truncated, err := fetchJobLogs(ctx, client, in.Owner, in.Repo, in.ID)
			if err != nil {
				return nil, out, err
			}
			out.Logs = logs
			out.LogsTruncated = truncated

		default:
			return nil, out, fmt.Errorf("github: unknown method %q (want workflow|run|job|artifact|logs)", in.Method)
		}
		return nil, out, nil
	}
}

// ---------- github_list_actions ----------

type ListActionsInput struct {
	Owner   string `json:"owner" jsonschema:"repo owner"`
	Repo    string `json:"repo" jsonschema:"repo name"`
	Method  string `json:"method" jsonschema:"workflows|runs|jobs|artifacts"`
	RunID   int64  `json:"run_id,omitempty" jsonschema:"required for jobs and artifacts methods"`
	Branch  string `json:"branch,omitempty" jsonschema:"filter runs by branch (runs method only)"`
	Status  string `json:"status,omitempty" jsonschema:"filter runs by status (runs method only)"`
	Event   string `json:"event,omitempty" jsonschema:"filter runs by event (runs method only)"`
	PerPage int    `json:"per_page,omitempty" jsonschema:"1-100 (default 30)"`
	Page    int    `json:"page,omitempty" jsonschema:"1-based page (default 1)"`
}

type ListActionsOutput struct {
	Method     string            `json:"method"`
	TotalCount int               `json:"total_count"`
	Workflows  []WorkflowSummary `json:"workflows,omitempty"`
	Runs       []RunSummary      `json:"runs,omitempty"`
	Jobs       []JobSummary      `json:"jobs,omitempty"`
	Artifacts  []ArtifactSummary `json:"artifacts,omitempty"`
}

func listActions(client *gh.Client) mcp.ToolHandlerFor[ListActionsInput, ListActionsOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ListActionsInput) (*mcp.CallToolResult, ListActionsOutput, error) {
		method := strings.ToLower(strings.TrimSpace(in.Method))
		out := ListActionsOutput{Method: method}
		listOpts := gh.ListOptions{
			PerPage: clampPerPage(in.PerPage),
			Page:    defaultInt(in.Page, 1),
		}

		switch method {
		case "workflows":
			ws, _, err := client.Actions.ListWorkflows(ctx, in.Owner, in.Repo, &listOpts)
			if err != nil {
				return nil, out, fmt.Errorf("github: list workflows %s/%s: %w", in.Owner, in.Repo, err)
			}
			out.TotalCount = ws.GetTotalCount()
			out.Workflows = make([]WorkflowSummary, 0, len(ws.Workflows))
			for _, w := range ws.Workflows {
				out.Workflows = append(out.Workflows, workflowSummary(w))
			}

		case "runs":
			runOpts := &gh.ListWorkflowRunsOptions{
				Branch:      in.Branch,
				Status:      in.Status,
				Event:       in.Event,
				ListOptions: listOpts,
			}
			rs, _, err := client.Actions.ListRepositoryWorkflowRuns(ctx, in.Owner, in.Repo, runOpts)
			if err != nil {
				return nil, out, fmt.Errorf("github: list runs %s/%s: %w", in.Owner, in.Repo, err)
			}
			out.TotalCount = rs.GetTotalCount()
			out.Runs = make([]RunSummary, 0, len(rs.WorkflowRuns))
			for _, r := range rs.WorkflowRuns {
				out.Runs = append(out.Runs, runSummary(r))
			}

		case "jobs":
			if in.RunID == 0 {
				return nil, out, fmt.Errorf("github: list jobs requires run_id")
			}
			jobOpts := &gh.ListWorkflowJobsOptions{ListOptions: listOpts}
			js, _, err := client.Actions.ListWorkflowJobs(ctx, in.Owner, in.Repo, in.RunID, jobOpts)
			if err != nil {
				return nil, out, fmt.Errorf("github: list jobs %s/%s run=%d: %w", in.Owner, in.Repo, in.RunID, err)
			}
			out.TotalCount = js.GetTotalCount()
			out.Jobs = make([]JobSummary, 0, len(js.Jobs))
			for _, j := range js.Jobs {
				out.Jobs = append(out.Jobs, jobSummary(j))
			}

		case "artifacts":
			if in.RunID == 0 {
				return nil, out, fmt.Errorf("github: list artifacts requires run_id")
			}
			as, _, err := client.Actions.ListWorkflowRunArtifacts(ctx, in.Owner, in.Repo, in.RunID, &listOpts)
			if err != nil {
				return nil, out, fmt.Errorf("github: list artifacts %s/%s run=%d: %w", in.Owner, in.Repo, in.RunID, err)
			}
			out.TotalCount = int(as.GetTotalCount())
			out.Artifacts = make([]ArtifactSummary, 0, len(as.Artifacts))
			for _, a := range as.Artifacts {
				out.Artifacts = append(out.Artifacts, artifactSummary(a))
			}

		default:
			return nil, out, fmt.Errorf("github: unknown method %q (want workflows|runs|jobs|artifacts)", in.Method)
		}
		return nil, out, nil
	}
}

// ---------- github_get_job_logs ----------

type GetJobLogsInput struct {
	Owner      string `json:"owner" jsonschema:"repo owner"`
	Repo       string `json:"repo" jsonschema:"repo name"`
	JobID      int64  `json:"job_id,omitempty" jsonschema:"specific job (mutually exclusive with run_id)"`
	RunID      int64  `json:"run_id,omitempty" jsonschema:"all jobs in run (mutually exclusive with job_id)"`
	FailedOnly bool   `json:"failed_only,omitempty" jsonschema:"with run_id, only include jobs whose conclusion is 'failure'"`
}

type JobLog struct {
	JobID      int64  `json:"job_id"`
	JobName    string `json:"job_name,omitempty"`
	Conclusion string `json:"conclusion,omitempty"`
	Logs       string `json:"logs"`
	Truncated  bool   `json:"truncated,omitempty"`
}

type GetJobLogsOutput struct {
	Jobs []JobLog `json:"jobs"`
}

func getJobLogs(client *gh.Client) mcp.ToolHandlerFor[GetJobLogsInput, GetJobLogsOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in GetJobLogsInput) (*mcp.CallToolResult, GetJobLogsOutput, error) {
		if in.JobID == 0 && in.RunID == 0 {
			return nil, GetJobLogsOutput{}, fmt.Errorf("github: job_id or run_id is required")
		}
		if in.JobID != 0 && in.RunID != 0 {
			return nil, GetJobLogsOutput{}, fmt.Errorf("github: job_id and run_id are mutually exclusive")
		}

		out := GetJobLogsOutput{Jobs: []JobLog{}}

		if in.JobID != 0 {
			j, _, err := client.Actions.GetWorkflowJobByID(ctx, in.Owner, in.Repo, in.JobID)
			if err != nil {
				return nil, out, fmt.Errorf("github: get job %s/%s#%d: %w", in.Owner, in.Repo, in.JobID, err)
			}
			logs, truncated, err := fetchJobLogs(ctx, client, in.Owner, in.Repo, in.JobID)
			if err != nil {
				return nil, out, err
			}
			out.Jobs = append(out.Jobs, JobLog{
				JobID:      in.JobID,
				JobName:    j.GetName(),
				Conclusion: j.GetConclusion(),
				Logs:       logs,
				Truncated:  truncated,
			})
			return nil, out, nil
		}

		jobOpts := &gh.ListWorkflowJobsOptions{ListOptions: gh.ListOptions{PerPage: listPerPageCap}}
		js, _, err := client.Actions.ListWorkflowJobs(ctx, in.Owner, in.Repo, in.RunID, jobOpts)
		if err != nil {
			return nil, out, fmt.Errorf("github: list jobs %s/%s run=%d: %w", in.Owner, in.Repo, in.RunID, err)
		}

		for _, j := range js.Jobs {
			if in.FailedOnly && j.GetConclusion() != "failure" {
				continue
			}
			logs, truncated, err := fetchJobLogs(ctx, client, in.Owner, in.Repo, j.GetID())
			if err != nil {
				return nil, out, err
			}
			out.Jobs = append(out.Jobs, JobLog{
				JobID:      j.GetID(),
				JobName:    j.GetName(),
				Conclusion: j.GetConclusion(),
				Logs:       logs,
				Truncated:  truncated,
			})
		}
		return nil, out, nil
	}
}

// ---------- helpers ----------

func fetchJobLogs(ctx context.Context, client *gh.Client, owner, repo string, jobID int64) (string, bool, error) {
	logURL, _, err := client.Actions.GetWorkflowJobLogs(ctx, owner, repo, jobID, 1)
	if err != nil {
		return "", false, fmt.Errorf("github: resolve job logs %s/%s#%d: %w", owner, repo, jobID, err)
	}
	if logURL == nil {
		return "", false, nil
	}
	return downloadAndTruncate(ctx, logURL)
}

func downloadAndTruncate(ctx context.Context, u *url.URL) (string, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", false, fmt.Errorf("github: build log request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", false, fmt.Errorf("github: download logs: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", false, fmt.Errorf("github: download logs: unexpected status %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxLogChars)+1))
	if err != nil {
		return "", false, fmt.Errorf("github: read logs: %w", err)
	}
	text, truncated := truncateString(string(body), maxLogChars)
	return text, truncated, nil
}

func workflowSummary(w *gh.Workflow) WorkflowSummary {
	if w == nil {
		return WorkflowSummary{}
	}
	return WorkflowSummary{
		ID:        w.GetID(),
		Name:      w.GetName(),
		Path:      w.GetPath(),
		State:     w.GetState(),
		HTMLURL:   w.GetHTMLURL(),
		CreatedAt: formatTime(w.GetCreatedAt()),
		UpdatedAt: formatTime(w.GetUpdatedAt()),
	}
}

func runSummary(r *gh.WorkflowRun) RunSummary {
	if r == nil {
		return RunSummary{}
	}
	out := RunSummary{
		ID:           r.GetID(),
		Name:         r.GetName(),
		HeadBranch:   r.GetHeadBranch(),
		HeadSHA:      r.GetHeadSHA(),
		Event:        r.GetEvent(),
		Status:       r.GetStatus(),
		Conclusion:   r.GetConclusion(),
		WorkflowID:   r.GetWorkflowID(),
		RunNumber:    r.GetRunNumber(),
		RunAttempt:   r.GetRunAttempt(),
		HTMLURL:      r.GetHTMLURL(),
		CreatedAt:    formatTime(r.GetCreatedAt()),
		UpdatedAt:    formatTime(r.GetUpdatedAt()),
		RunStartedAt: formatTime(r.GetRunStartedAt()),
	}
	if r.Actor != nil {
		out.Actor = r.Actor.GetLogin()
	}
	return out
}

func jobSummary(j *gh.WorkflowJob) JobSummary {
	if j == nil {
		return JobSummary{}
	}
	out := JobSummary{
		ID:           j.GetID(),
		RunID:        j.GetRunID(),
		Name:         j.GetName(),
		WorkflowName: j.GetWorkflowName(),
		Status:       j.GetStatus(),
		Conclusion:   j.GetConclusion(),
		HeadSHA:      j.GetHeadSHA(),
		HTMLURL:      j.GetHTMLURL(),
		StartedAt:    formatTime(j.GetStartedAt()),
		CompletedAt:  formatTime(j.GetCompletedAt()),
	}
	if len(j.Steps) > 0 {
		out.Steps = make([]JobStep, 0, len(j.Steps))
		for _, s := range j.Steps {
			out.Steps = append(out.Steps, JobStep{
				Name:        s.GetName(),
				Status:      s.GetStatus(),
				Conclusion:  s.GetConclusion(),
				Number:      s.GetNumber(),
				StartedAt:   formatTime(s.GetStartedAt()),
				CompletedAt: formatTime(s.GetCompletedAt()),
			})
		}
	}
	return out
}

func artifactSummary(a *gh.Artifact) ArtifactSummary {
	if a == nil {
		return ArtifactSummary{}
	}
	return ArtifactSummary{
		ID:                 a.GetID(),
		Name:               a.GetName(),
		SizeInBytes:        a.GetSizeInBytes(),
		Expired:            a.GetExpired(),
		ArchiveDownloadURL: a.GetArchiveDownloadURL(),
		CreatedAt:          formatTime(a.GetCreatedAt()),
		UpdatedAt:          formatTime(a.GetUpdatedAt()),
		ExpiresAt:          formatTime(a.GetExpiresAt()),
	}
}
