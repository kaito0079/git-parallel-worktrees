package cli

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/kaito0079/git-parallel-worktrees/internal/links"
	"github.com/kaito0079/git-parallel-worktrees/internal/repo"
)

func newSyncCommand(e *env) *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Re-create symlinks and copies from .worktreelinks",
		Long: "Re-create the symlinks and copies described by .worktreelinks.\n\n" +
			"Existing symlinks pointing into the main repository are removed first.\n" +
			"[copy] entries are skipped when a real file or directory is already\n" +
			"there; -f removes it and copies again.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return runSync(e, force) },
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false,
		"overwrite real files/directories for [copy] entries")

	return cmd
}

func runSync(e *env, force bool) error {
	ctx, current, err := worktreeContext(e)
	if err != nil {
		return err
	}

	if !ensureConfig(e, ctx, current) {
		return fmt.Errorf("%s not found (run `pwt init` to create one)", links.ConfigName)
	}

	fmt.Fprintf(e.stdout, "==> Syncing %s\n", filepath.Base(current))
	if force {
		fmt.Fprintln(e.stdout, "  (force: real files/directories for [copy] entries will be overwritten)")
	}

	return syncLinks(e, ctx.ProjectRoot, current, force)
}

func newUnsyncCommand(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "unsync",
		Short: "Remove all symlinks pointing into the main repository",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return runUnsync(e) },
	}
}

func runUnsync(e *env) error {
	ctx, current, err := worktreeContext(e)
	if err != nil {
		return err
	}

	fmt.Fprintf(e.stdout, "==> Removing symlinks in %s\n", filepath.Base(current))
	n, err := links.CleanSymlinks(ctx.ProjectRoot, current)
	if err != nil {
		return err
	}
	fmt.Fprintf(e.stdout, "  removed %d symlink(s)\n", n)

	fmt.Fprintln(e.stdout, "\n  re-sync:              pwt sync")
	fmt.Fprintln(e.stdout, "  drop specific entries: edit .worktreelinks, then pwt sync")

	return nil
}

// worktreeContext は「worktree 内で実行されていること」を確かめて
// Context とカレント worktree のルートを返す。
func worktreeContext(e *env) (repo.Context, string, error) {
	ctx, err := e.context()
	if err != nil {
		return repo.Context{}, "", err
	}
	current := e.currentRoot()
	if current == "" || current == ctx.ProjectRoot {
		return repo.Context{}, "", fmt.Errorf("run this inside a worktree (not the main repository)")
	}
	return ctx, current, nil
}
