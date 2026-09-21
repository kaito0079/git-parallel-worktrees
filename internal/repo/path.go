package repo

import (
	"fmt"
	"strings"
)

// WorktreePath は worktree のディレクトリパスを組み立てる。
// usePrefix が true なら <base>/<project>--<slug>、false なら <base>/<slug>。
func WorktreePath(workBase, projectName string, usePrefix bool, slug string) string {
	base := strings.TrimSuffix(workBase, "/")
	if usePrefix {
		return base + "/" + projectName + "--" + slug
	}
	return base + "/" + slug
}

// ResolveAddPath は pwt add の <path> 引数を絶対パスに解決する。
//
//   - 絶対パス         → そのまま（末尾の / は落とす）
//   - / を含む相対パス → cwd 起点で絶対パス化（git worktree add と同じ挙動）
//   - バレネーム       → work_base 配下に配置
//
// パスの正規化（. や .. の解決）は行わない。git worktree add に渡す文字列を
// そのまま組み立てるのが目的で、解釈は git に委ねる。
func ResolveAddPath(pathArg, workBase, projectName string, usePrefix bool, cwd string) (string, error) {
	if strings.HasPrefix(pathArg, "/") {
		return strings.TrimSuffix(pathArg, "/"), nil
	}
	if strings.Contains(pathArg, "/") {
		return strings.TrimSuffix(cwd, "/") + "/" + strings.TrimSuffix(pathArg, "/"), nil
	}
	// バレネームは work_base 配下に組み立てるため、.. を含むと配置先が
	// work_base の外に出てしまう
	if strings.Contains(pathArg, "..") {
		return "", fmt.Errorf("invalid <path> (contains %q): %s", "..", pathArg)
	}
	return WorktreePath(workBase, projectName, usePrefix, pathArg), nil
}
