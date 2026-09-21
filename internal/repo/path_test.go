package repo

import "testing"

func TestWorktreePath(t *testing.T) {
	tests := []struct {
		name        string
		workBase    string
		projectName string
		usePrefix   bool
		slug        string
		want        string
	}{
		{
			name:     "usePrefix=true: <base>/<repo>--<slug>",
			workBase: "/parent/.worktrees", projectName: "myapp", usePrefix: true, slug: "feature-auth",
			want: "/parent/.worktrees/myapp--feature-auth",
		},
		{
			name:     "usePrefix=false: <base>/<slug> のみ",
			workBase: "/main/.worktrees", projectName: "myapp", usePrefix: false, slug: "feature-auth",
			want: "/main/.worktrees/feature-auth",
		},
		{
			name:     "末尾スラッシュを正規化する",
			workBase: "/parent/.worktrees/", projectName: "myapp", usePrefix: true, slug: "slug",
			want: "/parent/.worktrees/myapp--slug",
		},
		{
			name:     "slug に / を含んでもそのまま組み立てる (auto branch mode)",
			workBase: "/parent/.worktrees", projectName: "myapp", usePrefix: false, slug: "feature/PROJ-123",
			want: "/parent/.worktrees/feature/PROJ-123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WorktreePath(tt.workBase, tt.projectName, tt.usePrefix, tt.slug)
			if got != tt.want {
				t.Errorf("WorktreePath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveAddPath(t *testing.T) {
	const cwd = "/current/dir"

	tests := []struct {
		name      string
		pathArg   string
		workBase  string
		usePrefix bool
		want      string
		wantErr   bool
	}{
		{
			name:    "バレネーム: work_base 配下に prefix 付きで配置",
			pathArg: "review_1", workBase: "/parent/.worktrees", usePrefix: true,
			want: "/parent/.worktrees/myapp--review_1",
		},
		{
			name:    "バレネーム + usePrefix=false: prefix なし",
			pathArg: "review_1", workBase: "/main/.worktrees", usePrefix: false,
			want: "/main/.worktrees/review_1",
		},
		{
			name:    "絶対パス: そのまま返す",
			pathArg: "/tmp/wt", workBase: "/ignored", usePrefix: true,
			want: "/tmp/wt",
		},
		{
			name:    "絶対パス: 末尾スラッシュを落とす",
			pathArg: "/tmp/wt/", workBase: "/ignored", usePrefix: true,
			want: "/tmp/wt",
		},
		{
			name:    "/ を含む相対パス: cwd 起点で絶対パス化",
			pathArg: "sub/wt", workBase: "/ignored", usePrefix: true,
			want: cwd + "/sub/wt",
		},
		{
			name:    "./ 始まりの相対パス: cwd 起点で絶対パス化",
			pathArg: "./wt", workBase: "/ignored", usePrefix: true,
			want: cwd + "/./wt",
		},
		{
			name:    "バレネームに .. を含むとエラー",
			pathArg: "..foo", workBase: "/base", usePrefix: true,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveAddPath(tt.pathArg, tt.workBase, "myapp", tt.usePrefix, cwd)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ResolveAddPath() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveAddPath() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("ResolveAddPath() = %q, want %q", got, tt.want)
			}
		})
	}
}
