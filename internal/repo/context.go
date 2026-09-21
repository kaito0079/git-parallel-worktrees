package repo

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kaito0079/git-parallel-worktrees/internal/gitcmd"
)

// EnvWorkBase は work_base を上書きする環境変数名。
const EnvWorkBase = "GIT_PARALLEL_WORKTREES_BASE"

// ErrNotGitRepository は git リポジトリ外で実行されたときに返る。
var ErrNotGitRepository = errors.New("not a git repository")

// Context はコマンド実行時に解決されるリポジトリの情報。
type Context struct {
	// ProjectRoot はメインリポジトリのルート。
	ProjectRoot string
	// ProjectName は ProjectRoot のディレクトリ名。
	ProjectName string
	// WorkBase は worktree を配置するベースディレクトリ。
	// この時点では存在しないことがある。作成は EnsureWorkBase が行う。
	WorkBase string
	// UsePrefix は worktree のディレクトリ名に <repo>-- を付けるか。
	UsePrefix bool
}

// FindProjectRoot はメインリポジトリのルートを返す。
// git worktree list --porcelain の最初のエントリがメインリポジトリを指す（git の仕様）。
func FindProjectRoot(r gitcmd.Runner, dir string) (string, error) {
	out, err := r.Output(dir, "worktree", "list", "--porcelain")
	if err != nil {
		return "", ErrNotGitRepository
	}
	wts := ParseWorktrees(out)
	if len(wts) == 0 || wts[0].Path == "" {
		return "", ErrNotGitRepository
	}
	return wts[0].Path, nil
}

// ResolveContext は環境変数と git config から Context を組み立てる。
//
// 設定の優先順位:
//   - work_base: GIT_PARALLEL_WORKTREES_BASE > リポジトリの親
//   - pwt.worktreeDir が "./" 始まりなら main repo 自身を base とする
//   - pwt.worktreePrefix (repo|none|auto) で prefix の有無を明示できる
func ResolveContext(r gitcmd.Runner, dir string) (Context, error) {
	root, err := FindProjectRoot(r, dir)
	if err != nil {
		return Context{}, err
	}

	base := filepath.Dir(root)
	if env := os.Getenv(EnvWorkBase); env != "" {
		if !filepath.IsAbs(env) {
			return Context{}, fmt.Errorf("%s must be an absolute path: %s", EnvWorkBase, env)
		}
		if fi, err := os.Stat(env); err != nil || !fi.IsDir() {
			return Context{}, fmt.Errorf("%s does not exist: %s", EnvWorkBase, env)
		}
		base = env
	}

	// pwt.worktreeDir: worktree を専用サブディレクトリに格納する
	//   ".worktrees"   → {parent}/.worktrees/<repo>--<slug>
	//   "./.worktrees" → {main_repo}/.worktrees/<slug>
	wtDir := gitcmd.ConfigGet(r, root, "pwt.worktreeDir")
	useMainBase := false
	if strings.HasPrefix(wtDir, "./") {
		useMainBase = true
		wtDir = strings.TrimPrefix(wtDir, "./")
		if wtDir == "" {
			return Context{}, errors.New(`pwt.worktreeDir "./" points at the main repository itself`)
		}
	}
	if wtDir != "" {
		if filepath.IsAbs(wtDir) || strings.Contains(wtDir, "..") {
			return Context{}, fmt.Errorf("pwt.worktreeDir must be a relative subdirectory name: %s", wtDir)
		}
		if useMainBase {
			base = root
		}
		base = filepath.Join(base, wtDir)
	}

	// auto: main repo 内配置なら prefix を付けない、それ以外は付ける
	usePrefix := !useMainBase
	switch prefix := gitcmd.ConfigGet(r, root, "pwt.worktreePrefix"); prefix {
	case "repo":
		usePrefix = true
	case "none":
		usePrefix = false
	case "", "auto":
	default:
		return Context{}, fmt.Errorf("pwt.worktreePrefix must be one of repo|none|auto: %s", prefix)
	}

	return Context{
		ProjectRoot: root,
		ProjectName: filepath.Base(root),
		WorkBase:    base,
		UsePrefix:   usePrefix,
	}, nil
}

// ResolveContext はディレクトリを作らない。list のような参照系コマンドで
// 副作用が出ないようにするため、作成は worktree を作る側が行う。
