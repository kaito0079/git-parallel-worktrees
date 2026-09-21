// Package cli は pwt のサブコマンドを組み立てる。
package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kaito0079/git-parallel-worktrees/internal/gitcmd"
	"github.com/kaito0079/git-parallel-worktrees/internal/repo"
)

// EnvCDFile は shim が渡す「移動先を書き込むファイル」のパス。
// この環境変数の有無が、シェル統合が有効かどうかの判定になる。
const EnvCDFile = "PWT_CD_FILE"

// 終了コード。
const (
	ExitOK    = 0
	ExitError = 1
	ExitUsage = 2
)

// errHelp は引数の中にヘルプ要求があったことを表す。DisableFlagParsing の
// コマンドでは cobra がヘルプフラグを解釈しないため、引数を読む側が
// これを返し、コマンド側で cmd.Help() に振り分ける。
var errHelp = errors.New("help requested")

// usageError は引数の使い方の誤りを表す。終了コードを 2 に分けるために使う。
type usageError struct{ err error }

func (u usageError) Error() string { return u.err.Error() }
func (u usageError) Unwrap() error { return u.err }

func usagef(format string, a ...any) error {
	return usageError{fmt.Errorf(format, a...)}
}

// env は 1 回の実行で共有する依存。テストから差し替える。
type env struct {
	version string
	git     gitcmd.Runner
	stdout  io.Writer
	stderr  io.Writer
	// stdin はバッファ済みで持つ。1 回の実行で複数回確認することがあり、
	// confirm のたびに包み直すと前回バッファに読み込んだ残りを捨ててしまう。
	stdin *bufio.Reader
	cwd   string
	// cdFile が空ならシェル統合なし（cd を要求できない）。
	cdFile string
}

// Main は os の状態から env を組み立ててコマンドを実行し、終了コードを返す。
func Main(version string, args []string) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return ExitError
	}
	return Run(&env{
		version: version,
		git:     gitcmd.Exec{},
		stdout:  os.Stdout,
		stderr:  os.Stderr,
		stdin:   bufio.NewReader(os.Stdin),
		cwd:     cwd,
		cdFile:  os.Getenv(EnvCDFile),
	}, args)
}

// Run はコマンドを実行して終了コードを返す。
func Run(e *env, args []string) int {
	root := newRootCommand(e)
	root.SetArgs(args)
	root.SetOut(e.stdout)
	root.SetErr(e.stderr)

	if err := root.Execute(); err != nil {
		// ユーザーが確認プロンプトで中止した場合は異常ではないので
		// error: を付けずに知らせる
		if errors.Is(err, errCancelled) {
			fmt.Fprintln(e.stdout, "cancelled")
			return ExitError
		}
		fmt.Fprintf(e.stderr, "error: %v\n", err)
		var ue usageError
		if errors.As(err, &ue) {
			return ExitUsage
		}
		return ExitError
	}
	return ExitOK
}

func newRootCommand(e *env) *cobra.Command {
	root := &cobra.Command{
		Use:     "pwt",
		Version: e.version,
		Short:   "Manage git worktrees in parallel",
		Long: "pwt is a thin wrapper around git worktree.\n\n" +
			"It places worktrees in a shared location, syncs shared assets\n" +
			"(node_modules, .env, ...) via .worktreelinks, and can move between\n" +
			"worktrees when shell integration is enabled.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		// 引数なしは list と同じ
		RunE: func(cmd *cobra.Command, args []string) error { return runList(e) },
	}

	root.AddCommand(
		newListCommand(e),
		newPathCommand(e),
		newSwitchCommand(e),
		newAddCommand(e),
		newRemoveCommand(e),
		newSyncCommand(e),
		newUnsyncCommand(e),
		newInitCommand(e),
	)

	return root
}

// ---------------------------------------------------------------------------
// 共有ヘルパー

// context はカレントディレクトリから Context を解決する。
func (e *env) context() (repo.Context, error) {
	return repo.ResolveContext(e.git, e.cwd)
}

// worktrees はメインリポジトリ基準で worktree 一覧を返す。
func (e *env) worktrees(ctx repo.Context) ([]repo.Worktree, error) {
	out, err := e.git.Output(ctx.ProjectRoot, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, repo.ErrNotGitRepository
	}
	return repo.ParseWorktrees(out), nil
}

// currentRoot はカレントディレクトリが属する worktree のルートを返す。
func (e *env) currentRoot() string {
	out, err := e.git.Output(e.cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// shellIntegration はシェル統合（shim）が有効かを返す。
func (e *env) shellIntegration() bool { return e.cdFile != "" }

// requestCD は shim に移動先を伝える。シェル統合が無い場合は何もしない。
func (e *env) requestCD(path string) error {
	if !e.shellIntegration() {
		return nil
	}
	return os.WriteFile(e.cdFile, []byte(path), 0o600)
}

// confirm は y/N の確認を取る。空入力・EOF は「いいえ」とみなす。
func (e *env) confirm(prompt string) bool {
	fmt.Fprintf(e.stdout, "%s [y/N] ", prompt)
	line, err := e.stdin.ReadString('\n')
	if err != nil && line == "" {
		fmt.Fprintln(e.stdout)
		return false
	}
	ans := strings.TrimSpace(line)
	return ans == "y" || ans == "Y"
}
