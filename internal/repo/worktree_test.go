package repo

import (
	"slices"
	"testing"
)

func TestParseWorktrees(t *testing.T) {
	const porcelain = `worktree /repos/myapp
HEAD 0000000000000000000000000000000000000001
branch refs/heads/main

worktree /repos/myapp--feature-auth
HEAD 0000000000000000000000000000000000000002
branch refs/heads/feature/auth

worktree /repos/myapp--detached
HEAD 0000000000000000000000000000000000000003
detached

`

	want := []Worktree{
		{Path: "/repos/myapp", Branch: "main"},
		{Path: "/repos/myapp--feature-auth", Branch: "feature/auth"},
		{Path: "/repos/myapp--detached", Branch: DetachedBranch},
	}

	got := ParseWorktrees(porcelain)
	if !slices.Equal(got, want) {
		t.Errorf("ParseWorktrees() = %+v, want %+v", got, want)
	}
}

func TestParseWorktreesEdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		porcelain string
		want      []Worktree
	}{
		{
			name:      "空入力",
			porcelain: "",
			want:      nil,
		},
		{
			name:      "末尾に改行がない",
			porcelain: "worktree /repos/myapp\nbranch refs/heads/main",
			want:      []Worktree{{Path: "/repos/myapp", Branch: "main"}},
		},
		{
			name:      "branch 行がない単独エントリは detached 扱い",
			porcelain: "worktree /repos/myapp\nHEAD 0000000000000000000000000000000000000001\n",
			want:      []Worktree{{Path: "/repos/myapp", Branch: DetachedBranch}},
		},
		{
			name:      "refs/heads/ 以外の ref はそのまま保持する",
			porcelain: "worktree /repos/myapp\nbranch refs/tags/v1\n",
			want:      []Worktree{{Path: "/repos/myapp", Branch: "refs/tags/v1"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseWorktrees(tt.porcelain)
			if !slices.Equal(got, tt.want) {
				t.Errorf("ParseWorktrees() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
