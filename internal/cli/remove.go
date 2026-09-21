package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kaito0079/git-parallel-worktrees/internal/repo"
)

// errCancelled はユーザーが確認プロンプトで中止したことを表す。
var errCancelled = errors.New("cancelled")

// statusPreviewLines は変更一覧を何行まで表示するか。
const statusPreviewLines = 20

func newRemoveCommand(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <branch|name|.>",
		Short: "Remove a worktree",
		Long: "Remove a worktree. `.` removes the one you are currently in.\n\n" +
			"A target resolved by partial match always asks for confirmation,\n" +
			"as does a worktree with uncommitted changes.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return runRemove(e, args[0]) },
	}
}

func runRemove(e *env, target string) error {
	ctx, err := e.context()
	if err != nil {
		return err
	}
	current := e.currentRoot()

	wtPath, err := removeTarget(e, ctx, current, target)
	if err != nil {
		return err
	}

	if fi, err := os.Stat(wtPath); err != nil || !fi.IsDir() {
		return fmt.Errorf("worktree directory does not exist: %s", wtPath)
	}

	removingCurrent := current == wtPath
	if removingCurrent {
		if err := confirmRemovingCurrent(e); err != nil {
			return err
		}
	}

	force, err := confirmDirty(e, wtPath)
	if err != nil {
		return err
	}

	gitArgs := []string{"worktree", "remove"}
	if force {
		gitArgs = append(gitArgs, "--force")
	}
	gitArgs = append(gitArgs, "--", wtPath)

	if err := e.git.Run(ctx.ProjectRoot, gitArgs...); err != nil {
		return fmt.Errorf("failed to remove worktree %s: %w\n  remove it manually: rm -rf %q", wtPath, err, wtPath)
	}

	// 退避の依頼は削除に成功してから。中止・失敗のときに worktree は
	// 残ったままシェルだけ動いてしまうのを防ぐ。
	// 受け渡しに失敗しても削除は済んでいるので、警告に留めて成功として扱う。
	if removingCurrent && e.shellIntegration() {
		if err := e.requestCD(ctx.ProjectRoot); err != nil {
			fmt.Fprintf(e.stdout, "  [!] could not tell the shell where to go: %v\n", err)
			fmt.Fprintf(e.stdout, "      move with: cd %q\n", ctx.ProjectRoot)
		} else {
			fmt.Fprintln(e.stdout, "  the current worktree was removed; moved to the main repository")
		}
	}

	fmt.Fprintf(e.stdout, "removed: %s\n", wtPath)
	return nil
}

// removeTarget は削除対象の worktree パスを決める。
func removeTarget(e *env, ctx repo.Context, current, target string) (string, error) {
	if target == "." {
		if current == "" {
			return "", repo.ErrNotGitRepository
		}
		if current == ctx.ProjectRoot {
			return "", errors.New("cannot remove the main repository")
		}
		return current, nil
	}

	wts, err := e.worktrees(ctx)
	if err != nil {
		return "", err
	}
	m, err := resolveTarget(wts, target, findOptions{exclude: ctx.ProjectRoot})
	if err != nil {
		return "", err
	}

	// 部分一致で決まった場合は、変更の有無にかかわらず確認する
	if !m.exact {
		prompt := fmt.Sprintf("%q matched %s (%s). Remove it?", target, m.wt.Path, m.wt.Branch)
		if !e.confirm(prompt) {
			return "", errCancelled
		}
	}

	return m.wt.Path, nil
}

// confirmRemovingCurrent はカレント worktree を消してよいかを確認する。
// シェル統合があれば削除後にメインリポジトリへ退避できるので確認は要らない。
func confirmRemovingCurrent(e *env) error {
	if e.shellIntegration() {
		return nil
	}

	fmt.Fprintln(e.stdout, "  warning: you are inside the worktree being removed")
	fmt.Fprintln(e.stdout, "  your shell will be left in a deleted directory; run `cd` afterwards")
	if !e.confirm("Continue?") {
		return errCancelled
	}
	return nil
}

// confirmDirty は未コミットの変更があれば一覧を見せて確認を取る。
// 戻り値は --force を付けるかどうか。
func confirmDirty(e *env, wtPath string) (bool, error) {
	status, err := e.git.Output(wtPath, "status", "--short")
	if err != nil || strings.TrimSpace(status) == "" {
		return false, nil
	}

	lines := strings.Split(strings.TrimRight(status, "\n"), "\n")
	fmt.Fprintf(e.stdout, "  [!] %d uncommitted change(s):\n", len(lines))
	for i, l := range lines {
		if i == statusPreviewLines {
			fmt.Fprintf(e.stdout, "      ... (%d of %d shown)\n", statusPreviewLines, len(lines))
			break
		}
		fmt.Fprintf(e.stdout, "      %s\n", l)
	}

	if !e.confirm("Remove anyway?") {
		return false, errCancelled
	}
	return true, nil
}
