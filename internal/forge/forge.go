// Package forge talks to source-control forges (GitHub first) for the
// workspace-to-PR lifecycle: branch publishing stays in internal/workspace
// (forge-independent git), while PR create/detect/sync and CI check ingestion
// live here behind the Forge interface.
package forge

import (
	"context"
	"fmt"
	"strings"
)

// Check is one CI/status check attached to a commit or PR.
type Check struct {
	Name       string
	State      string // pending, passing, failing, canceled, skipped
	Conclusion string
	URL        string
}

// PR is the forge pull-request view Temper tracks per task branch.
type PR struct {
	Number int
	URL    string
	Title  string
	State  string // open, closed, merged, draft
	Head   string
	Base   string
	Draft  bool
	Checks []Check
}

// FailedChecks returns checks in a failing state.
func (p PR) FailedChecks() []Check {
	var out []Check
	for _, c := range p.Checks {
		if c.State == "failing" {
			out = append(out, c)
		}
	}
	return out
}

// PendingChecks returns checks still running.
func (p PR) PendingChecks() []Check {
	var out []Check
	for _, c := range p.Checks {
		if c.State == "pending" {
			out = append(out, c)
		}
	}
	return out
}

// Verdict summarizes PR state for operators: "clean" (open, no failing or
// pending checks), "pending", "failing", or the raw state otherwise.
func (p PR) Verdict() string {
	if !strings.EqualFold(p.State, "open") {
		return strings.ToLower(strings.TrimSpace(p.State))
	}
	if len(p.FailedChecks()) > 0 {
		return "failing"
	}
	if len(p.PendingChecks()) > 0 {
		return "pending"
	}
	return "clean"
}

// CreateOptions describes a new PR.
type CreateOptions struct {
	Title string
	Body  string
	Base  string
	Head  string
	Draft bool
}

// Forge creates, detects, and syncs pull requests plus CI checks.
type Forge interface {
	// Create opens a PR from the repo at dir.
	Create(ctx context.Context, dir string, opt CreateOptions) (PR, error)
	// Find detects the open PR for branch (usually the task branch).
	Find(ctx context.Context, dir, branch string) (PR, bool, error)
	// Sync refreshes PR state and checks by number.
	Sync(ctx context.Context, dir string, number int) (PR, error)
	// Checks ingests CI checks for ref (commit SHA or branch).
	Checks(ctx context.Context, dir, ref string) ([]Check, error)
}

// normalizeCheckState maps forge-specific conclusions to the Check vocabulary.
func normalizeCheckState(status, conclusion string) string {
	conclusion = strings.ToLower(strings.TrimSpace(conclusion))
	switch conclusion {
	case "success", "succeeded", "passed", "pass":
		return "passing"
	case "failure", "failed", "fail", "error", "timed_out", "timedout", "cancelled":
		if conclusion == "cancelled" {
			return "canceled"
		}
		return "failing"
	case "skipped", "neutral":
		return "skipped"
	case "startup_failure", "stale":
		return "failing"
	}
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed":
		return "passing"
	case "in_progress", "queued", "requested", "waiting", "pending":
		return "pending"
	case "":
		return "pending"
	}
	return "pending"
}

var errNoNumber = fmt.Errorf("no pull request number")
