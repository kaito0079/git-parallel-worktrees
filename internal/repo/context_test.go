package repo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kaito0079/git-parallel-worktrees/internal/gitcmd"
)

// fakeRunner は worktree list と config --get だけに応答する Runner を作る。
func fakeRunner(root string, cfg map[string]string) *gitcmd.Fake {
	outs := map[string]string{
		"worktree list --porcelain": "worktree " + root + "\nHEAD 0000\nbranch refs/heads/main\n\n",
	}
	for k, v := range cfg {
		outs["config --get "+k] = v
	}
	return &gitcmd.Fake{Outputs: outs}
}

func TestResolveContext(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "myapp")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		cfg        map[string]string
		wantBase   string
		wantPrefix bool
		wantErr    bool
	}{
		{
			name:     "worktreeDir 未設定: work_base はリポジトリの親",
			cfg:      nil,
			wantBase: parent, wantPrefix: true,
		},
		{
			name:     "worktreeDir=.worktrees: 親配下のサブディレクトリ",
			cfg:      map[string]string{"pwt.worktreeDir": ".worktrees"},
			wantBase: filepath.Join(parent, ".worktrees"), wantPrefix: true,
		},
		{
			name:     "worktreeDir=./.worktrees: main repo 内配置で prefix なし",
			cfg:      map[string]string{"pwt.worktreeDir": "./.worktrees"},
			wantBase: filepath.Join(root, ".worktrees"), wantPrefix: false,
		},
		{
			name: "worktreePrefix=repo: 内配置でも強制付与",
			cfg: map[string]string{
				"pwt.worktreeDir":    "./.worktrees",
				"pwt.worktreePrefix": "repo",
			},
			wantBase: filepath.Join(root, ".worktrees"), wantPrefix: true,
		},
		{
			name: "worktreePrefix=none: 外配置でも省略",
			cfg: map[string]string{
				"pwt.worktreeDir":    ".worktrees",
				"pwt.worktreePrefix": "none",
			},
			wantBase: filepath.Join(parent, ".worktrees"), wantPrefix: false,
		},
		{
			name: "worktreePrefix=auto は未設定と同じ",
			cfg: map[string]string{
				"pwt.worktreeDir":    "./.worktrees",
				"pwt.worktreePrefix": "auto",
			},
			wantBase: filepath.Join(root, ".worktrees"), wantPrefix: false,
		},
		{
			name:    "worktreePrefix: 不正な値はエラー",
			cfg:     map[string]string{"pwt.worktreePrefix": "invalid"},
			wantErr: true,
		},
		{
			name:    "worktreeDir: 絶対パスはエラー",
			cfg:     map[string]string{"pwt.worktreeDir": "/absolute/path"},
			wantErr: true,
		},
		{
			name:    "worktreeDir: .. を含むパスはエラー",
			cfg:     map[string]string{"pwt.worktreeDir": "../escape"},
			wantErr: true,
		},
		{
			name:    "worktreeDir=./: main repo 自身はエラー",
			cfg:     map[string]string{"pwt.worktreeDir": "./"},
			wantErr: true,
		},
		{
			name:    "worktreeDir=./../escape: .. を含むためエラー",
			cfg:     map[string]string{"pwt.worktreeDir": "./../escape"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(EnvWorkBase, "")

			got, err := ResolveContext(fakeRunner(root, tt.cfg), root)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ResolveContext() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveContext() error = %v", err)
			}
			if got.ProjectRoot != root {
				t.Errorf("ProjectRoot = %q, want %q", got.ProjectRoot, root)
			}
			if got.ProjectName != "myapp" {
				t.Errorf("ProjectName = %q, want %q", got.ProjectName, "myapp")
			}
			if got.WorkBase != tt.wantBase {
				t.Errorf("WorkBase = %q, want %q", got.WorkBase, tt.wantBase)
			}
			if got.UsePrefix != tt.wantPrefix {
				t.Errorf("UsePrefix = %v, want %v", got.UsePrefix, tt.wantPrefix)
			}
		})
	}
}

func TestResolveContextEnvWorkBase(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "myapp")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	external := t.TempDir()

	t.Run("絶対パスかつ存在すれば work_base として使う", func(t *testing.T) {
		t.Setenv(EnvWorkBase, external)
		got, err := ResolveContext(fakeRunner(root, nil), root)
		if err != nil {
			t.Fatalf("ResolveContext() error = %v", err)
		}
		if got.WorkBase != external {
			t.Errorf("WorkBase = %q, want %q", got.WorkBase, external)
		}
	})

	t.Run("相対パスはエラー", func(t *testing.T) {
		t.Setenv(EnvWorkBase, "relative/path")
		if _, err := ResolveContext(fakeRunner(root, nil), root); err == nil {
			t.Error("ResolveContext() error = nil, want error")
		}
	})

	t.Run("存在しないディレクトリはエラー", func(t *testing.T) {
		t.Setenv(EnvWorkBase, filepath.Join(external, "missing"))
		if _, err := ResolveContext(fakeRunner(root, nil), root); err == nil {
			t.Error("ResolveContext() error = nil, want error")
		}
	})
}

// shell 版は context 解決のたびに work_base を mkdir していたため、
// pwt list のような参照系でもディレクトリが作られていた。
func TestResolveContextDoesNotCreateWorkBase(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "myapp")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvWorkBase, "")

	ctx, err := ResolveContext(fakeRunner(root, map[string]string{"pwt.worktreeDir": ".worktrees"}), root)
	if err != nil {
		t.Fatalf("ResolveContext() error = %v", err)
	}
	if _, err := os.Stat(ctx.WorkBase); !os.IsNotExist(err) {
		t.Errorf("WorkBase %q should not exist yet, stat err = %v", ctx.WorkBase, err)
	}

	if err := EnsureWorkBase(ctx); err != nil {
		t.Fatalf("EnsureWorkBase() error = %v", err)
	}
	if fi, err := os.Stat(ctx.WorkBase); err != nil || !fi.IsDir() {
		t.Errorf("EnsureWorkBase() did not create %q (err = %v)", ctx.WorkBase, err)
	}
}

func TestFindProjectRootNotARepository(t *testing.T) {
	r := &gitcmd.Fake{OutputErr: os.ErrNotExist}
	if _, err := FindProjectRoot(r, t.TempDir()); err == nil {
		t.Error("FindProjectRoot() error = nil, want error")
	}
}
