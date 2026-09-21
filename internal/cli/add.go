package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kaito0079/git-parallel-worktrees/internal/gitcmd"
	"github.com/kaito0079/git-parallel-worktrees/internal/links"
	"github.com/kaito0079/git-parallel-worktrees/internal/repo"
)

const addUsage = "usage: pwt add [-b <new-branch>] [-B <new-branch>] [--detach] <path> [<commit-ish>]"

// addPassthroughFlags は値を取らず、そのまま git worktree add に渡すフラグ。
var addPassthroughFlags = map[string]bool{
	"-d": true, "--detach": true, "--no-detach": true,
	"-f": true, "--force": true, "--no-force": true,
	"--checkout": true, "--no-checkout": true,
	"--lock": true, "--no-lock": true,
	"--orphan": true, "--no-orphan": true,
	"--track": true, "--no-track": true,
	"--guess-remote": true, "--no-guess-remote": true,
	"--relative-paths": true, "--no-relative-paths": true,
	"-q": true, "--quiet": true,
}

// addArgs は pwt add の解析結果。
type addArgs struct {
	newBranch   string // -b
	resetBranch string // -B
	passthrough []string
	positional  []string
}

func (a addArgs) hasDetach() bool {
	for _, f := range a.passthrough {
		if f == "-d" || f == "--detach" {
			return true
		}
	}
	return false
}

// parseAddArgs は git worktree add 互換の引数を解析する。
//
// cobra (pflag) には解析させない。git worktree add の --no-* 系や
// 位置引数の並びをそのまま再現する必要があるため。
func parseAddArgs(args []string) (addArgs, error) {
	var a addArgs
	endOfOptions := false

	for i := 0; i < len(args); i++ {
		arg := args[i]

		if endOfOptions {
			a.positional = append(a.positional, arg)
			continue
		}

		switch {
		case arg == "--":
			endOfOptions = true

		case arg == "-b", arg == "-B":
			i++
			if i >= len(args) || args[i] == "" {
				return a, usagef("%s requires a branch name", arg)
			}
			if arg == "-b" {
				a.newBranch = args[i]
			} else {
				a.resetBranch = args[i]
			}

		case arg == "--reason":
			i++
			if i >= len(args) || args[i] == "" {
				return a, usagef("--reason requires a value")
			}
			a.passthrough = append(a.passthrough, "--reason", args[i])

		case arg == "-h", arg == "--help":
			return a, errHelp

		case addPassthroughFlags[arg]:
			a.passthrough = append(a.passthrough, arg)

		case strings.HasPrefix(arg, "-"):
			return a, usagef("unknown option: %s\n%s", arg, addUsage)

		default:
			a.positional = append(a.positional, arg)
		}
	}

	return a, nil
}

func newAddCommand(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "add [-b <new-branch>] [-B <new-branch>] [--detach] <path> [<commit-ish>]",
		Short: "Create a worktree (same argument syntax as git worktree add)",
		Long: "Create a worktree. The argument syntax matches git worktree add.\n\n" +
			"<path> handling:\n" +
			"  bare name (no slash)   placed under the shared work base\n" +
			"  contains a slash       passed to git worktree add as-is\n\n" +
			"Auto branch mode: when <path> is neither absolute nor an explicit\n" +
			"relative path (./ ../), and none of -b/-B/--detach/<commit-ish> is\n" +
			"given, <path> is also used as the branch name.",
		// git worktree add 互換の解析を自前で行う
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			wtPath, err := runAdd(e, args)
			if errors.Is(err, errHelp) {
				return cmd.Help()
			}
			if err != nil {
				return err
			}
			// 移動の案内は add のときだけ。switch -c は自分で移動する
			fmt.Fprintf(e.stdout, "  move:    cd %q\n", wtPath)
			return nil
		},
	}
}

// runAdd は worktree を作成し、そのパスを返す。
// switch -c はこの戻り値を移動先に使うため、<path> を再解析しなくてよい。
func runAdd(e *env, args []string) (string, error) {
	a, err := parseAddArgs(args)
	if err != nil {
		return "", err
	}

	ctx, err := e.context()
	if err != nil {
		return "", err
	}

	if len(a.positional) == 0 {
		return "", usagef("%s", addUsage)
	}
	if len(a.positional) > 2 {
		return "", usagef("too many arguments: %s", a.positional[2])
	}

	pathArg := a.positional[0]
	commitish := ""
	if len(a.positional) == 2 {
		commitish = a.positional[1]
	}

	if pathArg == "" {
		return "", usagef("<path> is empty")
	}
	if strings.HasPrefix(pathArg, "-") {
		return "", usagef("invalid <path> (starts with %q): %s", "-", pathArg)
	}
	if strings.HasPrefix(commitish, "-") {
		return "", usagef("invalid <commit-ish> (starts with %q): %s", "-", commitish)
	}

	autoBranch := false
	if isAutoBranchCandidate(pathArg) &&
		commitish == "" && a.newBranch == "" && a.resetBranch == "" && !a.hasDetach() {
		a, commitish = resolveAutoBranch(e, ctx, pathArg, a)
		autoBranch = true
	}

	if a.newBranch != "" && a.resetBranch != "" {
		return "", usagef("-b and -B cannot be used together")
	}
	for _, b := range []string{a.newBranch, a.resetBranch} {
		if b == "" {
			continue
		}
		if err := validateBranch(e, ctx.ProjectRoot, b); err != nil {
			return "", err
		}
	}

	var wtPath string
	if autoBranch {
		// auto branch mode では / を保持して work_base 配下に置く（ブランチ名と一致させる）
		wtPath = repo.WorktreePath(ctx.WorkBase, ctx.ProjectName, ctx.UsePrefix, pathArg)
	} else {
		wtPath, err = repo.ResolveAddPath(pathArg, ctx.WorkBase, ctx.ProjectName, ctx.UsePrefix, e.cwd)
		if err != nil {
			return "", err
		}
	}
	wtPath = strings.TrimSuffix(wtPath, "/")

	if _, err := os.Lstat(wtPath); err == nil {
		return "", fmt.Errorf("%s already exists", wtPath)
	}
	// work_base やその下の中間ディレクトリを用意する
	if err := os.MkdirAll(filepath.Dir(wtPath), 0o755); err != nil {
		return "", fmt.Errorf("failed to create directory %s: %w", filepath.Dir(wtPath), err)
	}

	fmt.Fprintf(e.stdout, "==> Creating worktree: %s\n", wtPath)

	gitArgs := []string{"worktree", "add"}
	gitArgs = append(gitArgs, a.passthrough...)
	if a.newBranch != "" {
		gitArgs = append(gitArgs, "-b", a.newBranch)
	}
	if a.resetBranch != "" {
		gitArgs = append(gitArgs, "-B", a.resetBranch)
	}
	gitArgs = append(gitArgs, "--", wtPath)
	if commitish != "" {
		gitArgs = append(gitArgs, commitish)
	}

	if err := e.git.Run(ctx.ProjectRoot, gitArgs...); err != nil {
		return "", fmt.Errorf("failed to create worktree: %w", err)
	}

	if ensureConfig(e, ctx, wtPath) {
		fmt.Fprintln(e.stdout)
		if err := syncLinks(e, ctx.ProjectRoot, wtPath, false); err != nil {
			return wtPath, err
		}
	}

	fmt.Fprintf(e.stdout, "\n  created: %s\n", wtPath)

	return wtPath, nil
}

// isAutoBranchCandidate は <path> を auto branch mode の対象にしてよいかを返す。
// 絶対パスと相対パス明示 (./ ../) は「ファイルシステムパス」の意図が明らかなので除く。
func isAutoBranchCandidate(p string) bool {
	switch {
	case strings.HasPrefix(p, "/"),
		strings.HasPrefix(p, "./"),
		strings.HasPrefix(p, "../"),
		p == ".", p == "..":
		return false
	}
	return true
}

// resolveAutoBranch は <path> をブランチ名として解釈し、必要な引数を補う。
// どのケースで解決したかを 1 行出力する。
func resolveAutoBranch(e *env, ctx repo.Context, pathArg string, a addArgs) (addArgs, string) {
	switch {
	case gitcmd.Succeeds(e.git, ctx.ProjectRoot, "show-ref", "--verify", "--quiet", "refs/heads/"+pathArg):
		fmt.Fprintf(e.stdout, "==> Checking out existing branch %s\n", pathArg)
		return a, pathArg

	case gitcmd.Succeeds(e.git, ctx.ProjectRoot, "show-ref", "--verify", "--quiet", "refs/remotes/origin/"+pathArg):
		fmt.Fprintf(e.stdout, "==> Creating branch %s tracking origin/%s\n", pathArg, pathArg)
		a.newBranch = pathArg
		return a, "origin/" + pathArg

	default:
		fmt.Fprintf(e.stdout, "==> Creating new branch %s from HEAD\n", pathArg)
		a.newBranch = pathArg
		return a, ""
	}
}

// validateBranch は構文チェックに加えて git check-ref-format でも検証する。
func validateBranch(e *env, dir, branch string) error {
	if err := repo.ValidateBranchName(branch); err != nil {
		return err
	}
	if !gitcmd.Succeeds(e.git, dir, "check-ref-format", "--branch", branch) {
		return fmt.Errorf("invalid branch name: %s", branch)
	}
	return nil
}

// ensureConfig は worktree に .worktreelinks を用意する。
// 無ければカレント worktree → メインリポジトリの順にコピー元を探す。
// 戻り値は用意できたかどうか。
func ensureConfig(e *env, ctx repo.Context, dest string) bool {
	destConfig := filepath.Join(dest, links.ConfigName)
	if _, err := os.Stat(destConfig); err == nil {
		return true
	}

	var src string
	for _, candidate := range []string{e.currentRoot(), ctx.ProjectRoot} {
		if candidate == "" {
			continue
		}
		p := filepath.Join(candidate, links.ConfigName)
		if _, err := os.Stat(p); err == nil {
			src = p
			break
		}
	}
	if src == "" {
		fmt.Fprintf(e.stdout, "  [!] %s not found (run `pwt init` to create one)\n", links.ConfigName)
		return false
	}

	data, err := os.ReadFile(src)
	if err != nil {
		fmt.Fprintf(e.stdout, "  [!] failed to copy %s: %v\n", links.ConfigName, err)
		return false
	}
	if err := os.WriteFile(destConfig, data, 0o644); err != nil {
		fmt.Fprintf(e.stdout, "  [!] failed to copy %s: %v\n", links.ConfigName, err)
		return false
	}
	fmt.Fprintf(e.stdout, "  [+] copied %s\n", links.ConfigName)

	return true
}

// syncLinks は .worktreelinks に従って同期し、結果を出力する。
func syncLinks(e *env, srcRoot, destRoot string, force bool) error {
	res, err := links.Sync(e.git, srcRoot, destRoot, links.Options{Force: force, Out: e.stdout})
	if err != nil {
		return err
	}

	switch {
	case res.Linked == 0 && res.Copied == 0:
		fmt.Fprintln(e.stdout, "  nothing to link or copy")
	default:
		if res.Linked > 0 {
			fmt.Fprintf(e.stdout, "  created %d symlink(s)\n", res.Linked)
		}
		if res.Copied > 0 {
			fmt.Fprintf(e.stdout, "  copied %d entr%s\n", res.Copied, plural(res.Copied, "y", "ies"))
		}
	}

	return nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
