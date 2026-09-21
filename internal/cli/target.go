package cli

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kaito0079/git-parallel-worktrees/internal/repo"
)

// match は worktree の検索結果。
type match struct {
	wt repo.Worktree
	// exact は完全一致で決まったかどうか。部分一致の場合 false。
	exact bool
}

// findOptions は resolveTarget の挙動を制御する。
type findOptions struct {
	// allowIndex は一覧の番号での指定を許可する（switch / path 用）。
	allowIndex bool
	// exclude はマッチ対象から除外するパス（remove でメインリポジトリを除く）。
	exclude string
}

// resolveTarget は番号・ブランチ名・ディレクトリ名・パスから worktree を 1 つ選ぶ。
//
// switch / path / remove で共有する。除外条件と番号指定の可否だけが異なる。
func resolveTarget(wts []repo.Worktree, target string, opts findOptions) (match, error) {
	if opts.allowIndex {
		if n, err := strconv.Atoi(target); err == nil {
			if n < 0 || n >= len(wts) {
				return match{}, fmt.Errorf("worktree %s does not exist", target)
			}
			return match{wt: wts[n], exact: true}, nil
		}
	}

	var (
		partial  []repo.Worktree
		filtered = make([]repo.Worktree, 0, len(wts))
	)
	for _, w := range wts {
		if opts.exclude != "" && w.Path == opts.exclude {
			continue
		}
		filtered = append(filtered, w)
	}

	for _, w := range filtered {
		dir := filepath.Base(w.Path)
		if w.Branch == target || dir == target || w.Path == target {
			return match{wt: w, exact: true}, nil
		}
		if strings.Contains(w.Branch, target) || strings.Contains(dir, target) {
			partial = append(partial, w)
		}
	}

	switch len(partial) {
	case 1:
		return match{wt: partial[0], exact: false}, nil
	case 0:
		return match{}, fmt.Errorf("no worktree matches %q (run `pwt list` to see them)", target)
	default:
		var b strings.Builder
		fmt.Fprintf(&b, "%q matches multiple worktrees; be more specific:", target)
		for _, w := range partial {
			fmt.Fprintf(&b, "\n  %s  (%s)", w.Branch, w.Path)
		}
		return match{}, fmt.Errorf("%s", b.String())
	}
}
