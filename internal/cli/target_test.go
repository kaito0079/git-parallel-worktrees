package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/kaito0079/git-parallel-worktrees/internal/repo"
)

// asUsageError は errors.As の薄いラッパー（テスト用）。
func asUsageError(err error, target *usageError) bool { return errors.As(err, target) }

var testWorktrees = []repo.Worktree{
	{Path: "/repos/myapp", Branch: "main"},
	{Path: "/repos/myapp--review_1", Branch: "feature/PROJ-123"},
	{Path: "/repos/myapp--hotfix", Branch: "hotfix"},
}

func TestResolveTarget(t *testing.T) {
	tests := []struct {
		name      string
		target    string
		opts      findOptions
		wantPath  string
		wantExact bool
		wantErr   bool
	}{
		{
			name:   "番号で引く",
			target: "1", opts: findOptions{allowIndex: true},
			wantPath: "/repos/myapp--review_1", wantExact: true,
		},
		{
			name:   "範囲外の番号はエラー",
			target: "9", opts: findOptions{allowIndex: true},
			wantErr: true,
		},
		{
			name:   "allowIndex なしなら数字も名前として扱う（review_1 に部分一致）",
			target: "1", opts: findOptions{},
			wantPath: "/repos/myapp--review_1", wantExact: false,
		},
		{
			name:   "ブランチ名の完全一致",
			target: "hotfix", opts: findOptions{allowIndex: true},
			wantPath: "/repos/myapp--hotfix", wantExact: true,
		},
		{
			name:   "ディレクトリ名の完全一致",
			target: "myapp--review_1", opts: findOptions{},
			wantPath: "/repos/myapp--review_1", wantExact: true,
		},
		{
			name:   "パスの完全一致",
			target: "/repos/myapp--hotfix", opts: findOptions{},
			wantPath: "/repos/myapp--hotfix", wantExact: true,
		},
		{
			name:   "部分一致が 1 件なら exact=false で返る",
			target: "PROJ", opts: findOptions{},
			wantPath: "/repos/myapp--review_1", wantExact: false,
		},
		{
			name:   "一致なしはエラー",
			target: "nothing", opts: findOptions{},
			wantErr: true,
		},
		{
			name:   "exclude で除外したものは対象外",
			target: "main", opts: findOptions{exclude: "/repos/myapp"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveTarget(testWorktrees, tt.target, tt.opts)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("resolveTarget() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveTarget() error = %v", err)
			}
			if got.wt.Path != tt.wantPath {
				t.Errorf("path = %q, want %q", got.wt.Path, tt.wantPath)
			}
			if got.exact != tt.wantExact {
				t.Errorf("exact = %v, want %v", got.exact, tt.wantExact)
			}
		})
	}
}

func TestResolveTargetAmbiguous(t *testing.T) {
	wts := []repo.Worktree{
		{Path: "/repos/app--review_1", Branch: "feature/a"},
		{Path: "/repos/app--review_2", Branch: "feature/b"},
	}

	_, err := resolveTarget(wts, "review", findOptions{})
	if err == nil {
		t.Fatal("resolveTarget() error = nil, want error")
	}
	// 候補を列挙して具体的な指定を促す
	for _, want := range []string{"review_1", "review_2", "be more specific"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q, got: %v", want, err)
		}
	}
}
