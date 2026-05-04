package notion

import (
	"context"
	"fmt"
	"net/http"
)

// probeOrder is the sequence resolveKind tries on a hint=auto call. Pages
// come first because they cover both standalone pages and database rows
// (which are pages); blocks come last because every Notion ID is reachable
// as a block, so probing it before pages would always succeed and tell the
// caller nothing useful.
var probeOrder = []kindHint{kindPage, kindDatabase, kindDataSource, kindBlock}

// resolveKind returns the entity kind for an ID, probing endpoints when the
// hint is kindAuto. When the hint is anything else, resolveKind trusts it
// and returns immediately — letting the actual fetch surface a 404 if the
// caller picked wrong.
func resolveKind(ctx context.Context, c *Client, id string, hint kindHint) (kindHint, error) {
	if id == "" {
		return "", fmt.Errorf("notion: id is required")
	}
	if hint != kindAuto && hint != "" {
		return hint, nil
	}
	for _, k := range probeOrder {
		if err := probeOne(ctx, c, id, k); err == nil {
			return k, nil
		} else if !isNotFound(err) {
			// Surface anything that isn't a clean 404 — auth failures,
			// 500s, decode errors. Probing past those would only mask the
			// real problem.
			return "", fmt.Errorf("notion: probe %s: %w", k, err)
		}
	}
	return "", fmt.Errorf(
		"notion: %s not found as page, database, data_source, or block. "+
			"On integration tokens, each top-level page or database must be "+
			"explicitly shared with the integration in the Notion UI. See "+
			"https://www.notion.so/help/add-and-manage-connections-with-the-api",
		id,
	)
}

// probeOne issues a HEAD-equivalent GET against the kind's endpoint. We
// pass out=nil so the body is read but not decoded — cheaper than parsing
// the full payload just to learn whether it exists.
func probeOne(ctx context.Context, c *Client, id string, kind kindHint) error {
	path := pathForKind(id, kind)
	if path == "" {
		return fmt.Errorf("notion: unknown kind %q", kind)
	}
	return c.do(ctx, http.MethodGet, path, nil, nil)
}

func pathForKind(id string, kind kindHint) string {
	switch kind {
	case kindPage:
		return "/pages/" + id
	case kindDatabase:
		return "/databases/" + id
	case kindDataSource:
		return "/data_sources/" + id
	case kindBlock:
		return "/blocks/" + id
	}
	return ""
}
