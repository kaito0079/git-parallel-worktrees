// Package repo は git リポジトリと worktree の情報を扱う。
package repo

import "strings"

// DetachedBranch は detached HEAD の worktree に与えるブランチ名。
const DetachedBranch = "detached"

// Worktree は git worktree list --porcelain の 1 エントリ。
type Worktree struct {
	Path string
	// Branch はチェックアウト中のブランチ名（refs/heads/ を除いたもの）。
	// detached HEAD の場合は DetachedBranch。
	Branch string
}

// ParseWorktrees は git worktree list --porcelain の出力をパースする。
// git の仕様上、最初のエントリは常にメインリポジトリを指す。
func ParseWorktrees(porcelain string) []Worktree {
	var (
		out  []Worktree
		cur  Worktree
		open bool
	)

	flush := func() {
		if !open {
			return
		}
		// porcelain は各 worktree について "branch <ref>" または "detached" の
		// いずれかを出力する。detached では branch 行が出ないためここで補う。
		if cur.Branch == "" {
			cur.Branch = DetachedBranch
		}
		out = append(out, cur)
		cur = Worktree{}
		open = false
	}

	for _, line := range strings.Split(porcelain, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			cur.Path = strings.TrimPrefix(line, "worktree ")
			open = true
		case strings.HasPrefix(line, "branch "):
			ref := strings.TrimPrefix(line, "branch ")
			cur.Branch = strings.TrimPrefix(ref, "refs/heads/")
		}
	}
	flush()

	return out
}
