package notion

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	defaultBaseURL   = "https://api.notion.com/v1"
	defaultUserAgent = "foglight/v0.1.0"
	defaultTimeout   = 30 * time.Second
	maxErrSnippet    = 512

	// notionVersion pins the API version this client targets. Notion uses
	// date-string versioning; 2026-03-11 is the version that introduced
	// /v1/pages/{id}/markdown, which the page path of notion_fetch depends
	// on. See https://developers.notion.com/reference/versioning.
	notionVersion = "2026-03-11"

	// retryAfterCap bounds how long do() will wait on a 429 before giving
	// up. MCP request budgets are tight; if Notion asks for longer, surface
	// the error and let the agent decide.
	retryAfterCap = 5 * time.Second

	// retryAfterDefault is used when a 429 carries no parseable Retry-After.
	retryAfterDefault = time.Second
)

// Client is a thin wrapper around Notion's REST API. Auth is bearer with an
// internal-integration secret.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	userAgent  string
}

func newClient(apiKey string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: defaultTimeout},
		baseURL:    defaultBaseURL,
		apiKey:     apiKey,
		userAgent:  defaultUserAgent,
	}
}

// notionError is the canonical error envelope Notion returns on non-2xx.
type notionError struct {
	Object         string         `json:"object"`
	Status         int            `json:"status"`
	Code           string         `json:"code"`
	Message        string         `json:"message"`
	AdditionalData map[string]any `json:"additional_data,omitempty"`
}

// httpError is the typed error returned on non-2xx responses. Status is
// preserved so probe.go can distinguish 404 from other failures.
type httpError struct {
	Status  int
	Code    string
	Message string
	Body    string
}

func (e *httpError) Error() string {
	if e.Code != "" && e.Message != "" {
		return fmt.Sprintf("notion: http %d: %s: %s", e.Status, e.Code, e.Message)
	}
	if e.Body != "" {
		return fmt.Sprintf("notion: http %d: %s", e.Status, e.Body)
	}
	return fmt.Sprintf("notion: http %d", e.Status)
}

// retryableError signals from doOnce that the request should be retried
// after wait. Only emitted on 429 within the retryAfterCap budget.
type retryableError struct {
	wait time.Duration
}

func (r *retryableError) Error() string {
	return fmt.Sprintf("notion: rate limited (retry after %s)", r.wait)
}

// isNotFound reports whether err wraps a 404 response from Notion.
func isNotFound(err error) bool {
	var he *httpError
	if errors.As(err, &he) {
		return he.Status == http.StatusNotFound
	}
	return false
}

// do executes a request and decodes the response body into out (if non-nil).
// On 429 with a Retry-After under retryAfterCap, do() waits and retries once.
func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	err := c.doOnce(ctx, method, path, body, out)
	if err == nil {
		return nil
	}
	var rerr *retryableError
	if !errors.As(err, &rerr) {
		return err
	}
	select {
	case <-time.After(rerr.wait):
	case <-ctx.Done():
		return ctx.Err()
	}
	err = c.doOnce(ctx, method, path, body, out)
	if errors.As(err, &rerr) {
		// Second 429 in a row — give up with a structured error rather
		// than retrying further.
		return &httpError{Status: http.StatusTooManyRequests, Body: "rate limited (retried once)"}
	}
	return err
}

func (c *Client) doOnce(ctx context.Context, method, path string, body any, out any) error {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("notion: marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("notion: build request: %w", err)
	}
	c.setHeaders(req, body != nil)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("notion: http: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("notion: read response: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		wait := parseRetryAfter(resp.Header.Get("Retry-After"))
		if wait > retryAfterCap {
			return &httpError{Status: resp.StatusCode, Body: fmt.Sprintf("rate limited; retry-after %s exceeds cap %s", wait, retryAfterCap)}
		}
		return &retryableError{wait: wait}
	}

	if resp.StatusCode/100 != 2 {
		var ne notionError
		if json.Unmarshal(respBody, &ne) == nil && ne.Object == "error" && ne.Message != "" {
			return &httpError{Status: resp.StatusCode, Code: ne.Code, Message: ne.Message}
		}
		snippet, _ := truncateString(strings.TrimSpace(string(respBody)), maxErrSnippet)
		return &httpError{Status: resp.StatusCode, Body: snippet}
	}

	if out == nil {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		snippet, _ := truncateString(strings.TrimSpace(string(respBody)), maxErrSnippet)
		return fmt.Errorf("notion: decode response: %w (body: %s)", err, snippet)
	}
	return nil
}

// doRaw executes a GET and returns the raw response body. Used by the
// markdown endpoint, which returns markdown text wrapped in a JSON envelope
// the caller decodes itself.
func (c *Client) doRaw(ctx context.Context, method, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("notion: build request: %w", err)
	}
	c.setHeaders(req, false)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("notion: http: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("notion: read response: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		var ne notionError
		if json.Unmarshal(respBody, &ne) == nil && ne.Object == "error" && ne.Message != "" {
			return nil, &httpError{Status: resp.StatusCode, Code: ne.Code, Message: ne.Message}
		}
		snippet, _ := truncateString(strings.TrimSpace(string(respBody)), maxErrSnippet)
		return nil, &httpError{Status: resp.StatusCode, Body: snippet}
	}
	return respBody, nil
}

func (c *Client) setHeaders(req *http.Request, hasBody bool) {
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Notion-Version", notionVersion)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if hasBody {
		req.Header.Set("Content-Type", "application/json")
	}
}

// parseRetryAfter handles Notion's decimal-seconds Retry-After header.
// Falls back to retryAfterDefault on missing or unparseable values.
func parseRetryAfter(h string) time.Duration {
	h = strings.TrimSpace(h)
	if h == "" {
		return retryAfterDefault
	}
	if secs, err := strconv.ParseFloat(h, 64); err == nil && secs >= 0 {
		return time.Duration(secs * float64(time.Second))
	}
	if t, err := http.ParseTime(h); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return retryAfterDefault
}
