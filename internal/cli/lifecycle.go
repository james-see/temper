package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/james-see/temper/internal/config"
	"github.com/james-see/temper/internal/event"
	"github.com/james-see/temper/internal/forge"
	"github.com/james-see/temper/internal/run"
	"github.com/james-see/temper/internal/workspace"
)

// Forge and task-lifecycle commands: pr create/status, archive/restore/delete,
// and workspace clean. All state-changing commands append to the run's event
// stream so the lifecycle stays observable via inspect.

func prCmd(cfgPath *string) *cobra.Command {
	cmd := &cobra.Command{Use: "pr", Short: "Pull-request lifecycle for a run"}
	var runID, title, body, base, head, remote string
	var draft bool
	create := &cobra.Command{
		Use:   "create",
		Short: "Publish the run branch and open a GitHub PR",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(runID) == "" || strings.TrimSpace(title) == "" {
				return fmt.Errorf("--run and --title are required")
			}
			return prCreate(cmd.Context(), *cfgPath, runID, title, body, base, head, remote, draft)
		},
	}
	create.Flags().StringVar(&runID, "run", "", "run id")
	create.Flags().StringVar(&title, "title", "", "PR title")
	create.Flags().StringVar(&body, "body", "", "PR body")
	create.Flags().StringVar(&base, "base", "", "PR base branch (default: configured or repo default)")
	create.Flags().StringVar(&head, "head", "", "PR head branch (default: run branch)")
	create.Flags().StringVar(&remote, "remote", "origin", "git remote to publish to")
	create.Flags().BoolVar(&draft, "draft", false, "open as draft")
	var statusRun, statusRef string
	status := &cobra.Command{
		Use:   "status",
		Short: "Detect and sync the run PR plus CI checks",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(statusRun) == "" {
				return fmt.Errorf("--run is required")
			}
			return prStatus(cmd.Context(), *cfgPath, statusRun, statusRef)
		},
	}
	status.Flags().StringVar(&statusRun, "run", "", "run id")
	status.Flags().StringVar(&statusRef, "ref", "", "PR number, branch, or commit (default: run branch)")
	cmd.AddCommand(create, status)
	return cmd
}

func archiveCmd(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "archive <run-id>",
		Short: "Archive a run (restorable)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return setArchived(cmd, *cfgPath, args[0], true)
		},
	}
}

func restoreCmd(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "restore <run-id>",
		Short: "Restore an archived run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return setArchived(cmd, *cfgPath, args[0], false)
		},
	}
}

func deleteCmd(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <run-id>",
		Short: "Delete a run and its events (irreversible)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, mgr, err := openLifecycle(*cfgPath)
			if err != nil {
				return err
			}
			defer mgr.Close()
			if _, _, err := mgr.Load(cmd.Context(), args[0]); err != nil {
				return err
			}
			if err := mgr.Store.DeleteRun(cmd.Context(), args[0]); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "deleted %s\n", args[0])
			return nil
		},
	}
}

func workspaceCmd(cfgPath *string) *cobra.Command {
	cmd := &cobra.Command{Use: "workspace", Short: "Worktree inspection and cleanup"}
	var runID string
	var all, dry bool
	clean := &cobra.Command{
		Use:   "clean",
		Short: "Remove worktrees and report disk usage",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return workspaceClean(cmd, *cfgPath, runID, all, dry)
		},
	}
	clean.Flags().StringVar(&runID, "run", "", "remove one run's worktree")
	clean.Flags().BoolVar(&all, "all", false, "prune all orphaned worktree dirs")
	clean.Flags().BoolVar(&dry, "dry-run", false, "report only, delete nothing")
	cmd.AddCommand(clean)
	return cmd
}

func openLifecycle(cfgPath string) (*config.Loaded, string, *run.Manager, error) {
	loaded, err := config.Load(config.Flags{Config: cfgPath})
	if err != nil {
		return nil, "", nil, err
	}
	root, err := loaded.Config.WorkspaceRoot()
	if err != nil {
		return nil, "", nil, err
	}
	mgr, err := run.Open(loaded.Config, root)
	if err != nil {
		return nil, "", nil, err
	}
	return loaded, root, mgr, nil
}

func prCreate(ctx context.Context, cfgPath, runID, title, body, base, head, remote string, draft bool) error {
	loaded, root, mgr, err := openLifecycle(cfgPath)
	if err != nil {
		return err
	}
	defer mgr.Close()
	r, _, err := mgr.Load(ctx, runID)
	if err != nil {
		return err
	}
	dir := strings.TrimSpace(r.Workspace)
	if dir == "" {
		dir = root
	}
	ws := &workspace.Manager{RepoRoot: root, DataDir: config.DataDir(root), Worktree: dir}
	branch := strings.TrimSpace(head)
	if branch == "" {
		branch, err = ws.CurrentBranch()
		if strings.TrimSpace(branch) == "" {
			if err != nil {
				return fmt.Errorf("resolve run branch: %w (pass --head)", err)
			}
			return fmt.Errorf("resolve run branch: detached HEAD (pass --head)")
		}
	}
	gh := forge.NewGitHub()
	pr, found, ferr := gh.Find(ctx, dir, branch)
	if ferr != nil {
		return ferr
	}
	if found {
		_ = mgr.RecordEvent(ctx, runID, event.PRSynced, "operator", prEventData(pr))
		fmt.Fprintf(os.Stdout, "exists #%d %s\n", pr.Number, pr.URL)
		return nil
	}
	if err := ws.Publish(remote); err != nil {
		return err
	}
	_ = mgr.RecordEvent(ctx, runID, event.BranchPublished, "operator", map[string]any{
		"branch": branch, "remote": remote,
	})
	if strings.TrimSpace(base) == "" {
		base = workspace.ResolveBase(root, loaded.Config.Workspace.BaseBranch)
		base = strings.TrimPrefix(base, "origin/")
		if base == "HEAD" {
			base = ""
		}
	}
	pr, err = gh.Create(ctx, dir, forge.CreateOptions{
		Title: title, Body: body, Base: base, Head: branch, Draft: draft,
	})
	if err != nil {
		return err
	}
	_ = mgr.RecordEvent(ctx, runID, event.PRCreated, "operator", prEventData(pr))
	fmt.Fprintf(os.Stdout, "created #%d %s\n", pr.Number, pr.URL)
	return nil
}

func prStatus(ctx context.Context, cfgPath, runID, ref string) error {
	_, root, mgr, err := openLifecycle(cfgPath)
	if err != nil {
		return err
	}
	defer mgr.Close()
	r, _, err := mgr.Load(ctx, runID)
	if err != nil {
		return err
	}
	dir := strings.TrimSpace(r.Workspace)
	if dir == "" {
		dir = root
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		ws := &workspace.Manager{RepoRoot: root, DataDir: config.DataDir(root), Worktree: dir}
		ref, err = ws.CurrentBranch()
		if strings.TrimSpace(ref) == "" {
			if err != nil {
				return fmt.Errorf("resolve run branch: %w (pass --ref)", err)
			}
			return fmt.Errorf("resolve run branch: detached HEAD (pass --ref)")
		}
	}
	gh := forge.NewGitHub()
	pr, found, err := gh.Find(ctx, dir, ref)
	if err != nil {
		return err
	}
	if !found {
		checks, cerr := gh.Checks(ctx, dir, ref)
		if cerr != nil {
			fmt.Fprintf(os.Stdout, "no open PR for %s\n", ref)
			return nil
		}
		printChecks(checks)
		return nil
	}
	pr, err = gh.Sync(ctx, dir, pr.Number)
	if err != nil {
		return err
	}
	_ = mgr.RecordEvent(ctx, runID, event.PRSynced, "operator", prEventData(pr))
	_ = mgr.RecordEvent(ctx, runID, event.ChecksReported, "operator", map[string]any{
		"number": pr.Number, "verdict": pr.Verdict(), "checks": pr.Checks,
	})
	fmt.Fprintf(os.Stdout, "#%d %s [%s] %s\n", pr.Number, pr.Title, pr.Verdict(), pr.URL)
	printChecks(pr.Checks)
	return nil
}

func prEventData(pr forge.PR) map[string]any {
	return map[string]any{
		"number": pr.Number, "url": pr.URL, "title": pr.Title,
		"state": pr.State, "head": pr.Head, "base": pr.Base,
		"draft": pr.Draft, "verdict": pr.Verdict(),
	}
}

func printChecks(checks []forge.Check) {
	if len(checks) == 0 {
		fmt.Fprintln(os.Stdout, "no checks reported")
		return
	}
	for _, c := range checks {
		fmt.Fprintf(os.Stdout, "  %-8s %s\n", c.State, c.Name)
	}
}

func setArchived(cmd *cobra.Command, cfgPath, runID string, archived bool) error {
	_, _, mgr, err := openLifecycle(cfgPath)
	if err != nil {
		return err
	}
	defer mgr.Close()
	if _, _, err := mgr.Load(cmd.Context(), runID); err != nil {
		return err
	}
	if err := mgr.Store.SetArchived(cmd.Context(), runID, archived); err != nil {
		return err
	}
	typ := event.TaskArchived
	verb := "archived"
	if !archived {
		typ = event.TaskRestored
		verb = "restored"
	}
	_ = mgr.RecordEvent(cmd.Context(), runID, typ, "operator", map[string]any{"run": runID})
	fmt.Fprintf(os.Stdout, "%s %s\n", verb, runID)
	return nil
}

func workspaceClean(cmd *cobra.Command, cfgPath, runID string, all, dry bool) error {
	_, root, mgr, err := openLifecycle(cfgPath)
	if err != nil {
		return err
	}
	defer mgr.Close()
	ws, err := workspace.New(root, config.DataDir(root))
	if err != nil {
		return err
	}
	usage, _ := ws.DiskUsage()
	if strings.TrimSpace(runID) != "" && all {
		return fmt.Errorf("pass --run or --all, not both")
	}
	if strings.TrimSpace(runID) != "" {
		if _, _, err := mgr.Load(cmd.Context(), runID); err != nil {
			return err
		}
		before, _ := ws.DiskUsage()
		if dry {
			fmt.Fprintf(os.Stdout, "would remove worktree for %s (worktrees: %s)\n", runID, humanBytes(before))
			return nil
		}
		if err := ws.RemoveWorktree(runID); err != nil {
			return err
		}
		after, _ := ws.DiskUsage()
		_ = mgr.RecordEvent(cmd.Context(), runID, event.WorktreeClean, "operator", map[string]any{
			"run": runID, "freed": before - after,
		})
		fmt.Fprintf(os.Stdout, "removed worktree for %s (freed %s, worktrees: %s)\n", runID, humanBytes(before-after), humanBytes(after))
		return nil
	}
	orphans, orphanBytes, err := ws.Orphans()
	if err != nil {
		return err
	}
	if !all || dry {
		fmt.Fprintf(os.Stdout, "worktrees: %s in %d orphaned dirs (%s reclaimable)\n", humanBytes(usage), len(orphans), humanBytes(orphanBytes))
		for _, o := range orphans {
			fmt.Fprintf(os.Stdout, "  %s (%s)\n", o.Path, humanBytes(o.Bytes))
		}
		if !all {
			fmt.Fprintln(os.Stdout, "pass --all to prune orphans, --run <id> to remove one worktree")
		}
		return nil
	}
	removed, freed, err := ws.Prune()
	if err != nil {
		return err
	}
	after, _ := ws.DiskUsage()
	fmt.Fprintf(os.Stdout, "pruned %d dirs (freed %s, worktrees: %s)\n", len(removed), humanBytes(freed), humanBytes(after))
	return nil
}

func humanBytes(n int64) string {
	if n < 0 {
		n = 0
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit && exp < 3; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "KMGT"[exp])
}
