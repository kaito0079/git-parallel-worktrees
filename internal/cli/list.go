package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newListCommand(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List worktrees with their index and branch",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return runList(e) },
	}
}

func runList(e *env) error {
	ctx, err := e.context()
	if err != nil {
		return err
	}
	wts, err := e.worktrees(ctx)
	if err != nil {
		return err
	}
	current := e.currentRoot()

	// パス幅は最長のエントリに合わせる。固定幅にすると長いパスで桁が崩れる。
	width := 0
	for _, w := range wts {
		if len(w.Path) > width {
			width = len(w.Path)
		}
	}

	fmt.Fprintf(e.stdout, "=== %s ===\n", ctx.ProjectName)
	for i, w := range wts {
		marker := " "
		if w.Path == current {
			marker = ">"
		}
		fmt.Fprintf(e.stdout, " %s%2d  %-*s  (%s)\n", marker, i, width, w.Path, w.Branch)
	}

	return nil
}

func newPathCommand(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "path <index|branch|name>",
		Short: "Print the absolute path of a worktree",
		Long: "Print the absolute path of a worktree and nothing else.\n\n" +
			"Use it to move without shell integration:\n" +
			"  cd \"$(pwt path 2)\"",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := lookupWorktree(e, args[0])
			if err != nil {
				return err
			}
			fmt.Fprintln(e.stdout, path)
			return nil
		},
	}
}

// lookupWorktree は target に対応する worktree のパスを返す。
func lookupWorktree(e *env, target string) (string, error) {
	ctx, err := e.context()
	if err != nil {
		return "", err
	}
	wts, err := e.worktrees(ctx)
	if err != nil {
		return "", err
	}
	m, err := resolveTarget(wts, target, findOptions{allowIndex: true})
	if err != nil {
		return "", err
	}
	return m.wt.Path, nil
}
