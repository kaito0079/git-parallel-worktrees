package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaito0079/git-parallel-worktrees/internal/gitcmd"
	"github.com/kaito0079/git-parallel-worktrees/internal/repo"
)

// 実 git を使う統合テスト。

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := args
	if dir != "" {
		full = append([]string{"-C", dir}, args...)
	}
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", full, err, out)
	}
	return string(out)
}

func headBranch(t *testing.T, dir string) string {
	t.Helper()
	return strings.TrimSpace(git(t, dir, "symbolic-ref", "--short", "HEAD"))
}

// setupRepo は origin 付きのメインリポジトリを用意する。
//
//	feature/local-existing  ローカルにのみ存在
//	feature/origin-only     origin にのみ存在
func setupRepo(t *testing.T) string {
	t.Helper()

	// git は worktree のパスをシンボリックリンク解決後の形で記録するため、
	// 期待値もそれに合わせる (macOS の /var -> /private/var)
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	remote := filepath.Join(base, "remote.git")
	root := filepath.Join(base, "myapp")

	git(t, "", "init", "--bare", "--quiet", remote)
	git(t, "", "init", "--quiet", "-b", "main", root)
	for _, kv := range [][2]string{
		{"user.email", "test@example.com"},
		{"user.name", "test"},
		{"commit.gpgsign", "false"},
		// 配置先を決め打ちにして、ユーザーの global 設定の影響を消す
		{"pwt.worktreeDir", "./.worktrees"},
		{"pwt.worktreePrefix", "none"},
	} {
		git(t, root, "config", kv[0], kv[1])
	}

	if err := os.WriteFile(filepath.Join(root, "README"), []byte("init"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "README")
	git(t, root, "commit", "--quiet", "-m", "init")
	git(t, root, "remote", "add", "origin", remote)

	git(t, root, "branch", "feature/local-existing")

	git(t, root, "checkout", "--quiet", "-b", "feature/origin-only")
	if err := os.WriteFile(filepath.Join(root, "x"), []byte("v"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "x")
	git(t, root, "commit", "--quiet", "-m", "origin-only")
	git(t, root, "push", "--quiet", "origin", "feature/origin-only")
	git(t, root, "checkout", "--quiet", "main")
	git(t, root, "branch", "-D", "feature/origin-only")
	git(t, root, "fetch", "--quiet", "origin")

	return root
}

func newTestEnv(t *testing.T, cwd string) (*env, *bytes.Buffer) {
	t.Helper()
	t.Setenv(repo.EnvWorkBase, "")

	var out bytes.Buffer
	return &env{
		git:    gitcmd.Exec{Stdout: &out, Stderr: &out},
		stdout: &out,
		stderr: &out,
		stdin:  strings.NewReader(""),
		cwd:    cwd,
	}, &out
}

func TestAddAutoBranchMode(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		wantReport string
	}{
		{
			name: "ローカルブランチが存在すればチェックアウトする",
			path: "feature/local-existing", wantReport: "Checking out existing branch",
		},
		{
			name: "origin にのみ存在すれば追従ブランチを作る",
			path: "feature/origin-only", wantReport: "tracking origin/feature/origin-only",
		},
		{
			name: "どこにも無ければ HEAD ベースで新規作成する",
			path: "feature/brand-new", wantReport: "Creating new branch",
		},
		{
			name: "bare name でも有効",
			path: "hotfix-bare", wantReport: "Creating new branch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := setupRepo(t)
			e, out := newTestEnv(t, root)

			got, err := runAdd(e, []string{tt.path})
			if err != nil {
				t.Fatalf("runAdd() error = %v\n%s", err, out.String())
			}

			want := filepath.Join(root, ".worktrees", tt.path)
			if got != want {
				t.Errorf("path = %q, want %q", got, want)
			}
			// ディレクトリ名とブランチ名が一致する
			if branch := headBranch(t, got); branch != tt.path {
				t.Errorf("branch = %q, want %q", branch, tt.path)
			}
			if !strings.Contains(out.String(), tt.wantReport) {
				t.Errorf("output should report %q, got:\n%s", tt.wantReport, out.String())
			}
		})
	}
}

func TestAddAutoBranchNonTriggers(t *testing.T) {
	t.Run("-b 指定時は <path> をバレネームとして解決する", func(t *testing.T) {
		root := setupRepo(t)
		e, out := newTestEnv(t, root)

		got, err := runAdd(e, []string{"-b", "tmp/explicit", "explicit_dir"})
		if err != nil {
			t.Fatalf("runAdd() error = %v\n%s", err, out.String())
		}
		if want := filepath.Join(root, ".worktrees", "explicit_dir"); got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		if branch := headBranch(t, got); branch != "tmp/explicit" {
			t.Errorf("branch = %q, want %q", branch, "tmp/explicit")
		}
	})

	t.Run("./<path> は cwd 起点のファイルシステムパス", func(t *testing.T) {
		root := setupRepo(t)
		e, out := newTestEnv(t, root)

		if _, err := runAdd(e, []string{"./local/dot-relative"}); err != nil {
			t.Fatalf("runAdd() error = %v\n%s", err, out.String())
		}
		if fi, err := os.Stat(filepath.Join(root, "local", "dot-relative")); err != nil || !fi.IsDir() {
			t.Errorf("worktree should be created under cwd (err = %v)", err)
		}
	})

	t.Run("<commit-ish> 指定時は auto branch にならない", func(t *testing.T) {
		root := setupRepo(t)
		e, out := newTestEnv(t, root)
		sha := strings.TrimSpace(git(t, root, "rev-parse", "HEAD"))

		if _, err := runAdd(e, []string{"sub/wt_with_commit", sha}); err != nil {
			t.Fatalf("runAdd() error = %v\n%s", err, out.String())
		}
		if fi, err := os.Stat(filepath.Join(root, "sub", "wt_with_commit")); err != nil || !fi.IsDir() {
			t.Errorf("worktree should be created under cwd (err = %v)", err)
		}
	})
}

func TestAddRejectsExistingPath(t *testing.T) {
	root := setupRepo(t)
	e, _ := newTestEnv(t, root)

	if _, err := runAdd(e, []string{"dup"}); err != nil {
		t.Fatalf("runAdd() error = %v", err)
	}
	if _, err := runAdd(e, []string{"dup"}); err == nil {
		t.Error("runAdd() error = nil, want error for an existing path")
	}
}

// パス幅は最長のエントリに合わせて揃う。
func TestListAlignsToLongestPath(t *testing.T) {
	root := setupRepo(t)
	e, out := newTestEnv(t, root)

	if _, err := runAdd(e, []string{"a-very-long-worktree-name"}); err != nil {
		t.Fatalf("runAdd() error = %v", err)
	}
	out.Reset()

	if err := runList(e); err != nil {
		t.Fatalf("runList() error = %v", err)
	}

	var cols []int
	for _, line := range strings.Split(strings.TrimRight(out.String(), "\n"), "\n") {
		if i := strings.LastIndex(line, "  ("); i >= 0 {
			cols = append(cols, i)
		}
	}
	if len(cols) != 2 {
		t.Fatalf("expected 2 worktree lines, got %d:\n%s", len(cols), out.String())
	}
	if cols[0] != cols[1] {
		t.Errorf("branch column should line up: %v\n%s", cols, out.String())
	}
}

func TestSwitchWithoutShellIntegration(t *testing.T) {
	root := setupRepo(t)
	e, _ := newTestEnv(t, root)

	if _, err := runAdd(e, []string{"target"}); err != nil {
		t.Fatalf("runAdd() error = %v", err)
	}

	err := runSwitch(e, []string{"target"})
	if err == nil {
		t.Fatal("runSwitch() error = nil, want error without shell integration")
	}
	// 代替手段を案内する
	if !strings.Contains(err.Error(), "cd ") {
		t.Errorf("error should suggest cd, got: %v", err)
	}
}

func TestSwitchWritesCDFile(t *testing.T) {
	root := setupRepo(t)
	e, out := newTestEnv(t, root)
	cdFile := filepath.Join(t.TempDir(), "cd")
	e.cdFile = cdFile

	want, err := runAdd(e, []string{"target"})
	if err != nil {
		t.Fatalf("runAdd() error = %v\n%s", err, out.String())
	}
	if err := runSwitch(e, []string{"target"}); err != nil {
		t.Fatalf("runSwitch() error = %v", err)
	}

	got, err := os.ReadFile(cdFile)
	if err != nil {
		t.Fatalf("reading cd file: %v", err)
	}
	if string(got) != want {
		t.Errorf("cd file = %q, want %q", got, want)
	}
}

// switch -c は add の結果をそのまま移動先に使う。
func TestSwitchCreateThenMove(t *testing.T) {
	root := setupRepo(t)
	e, out := newTestEnv(t, root)
	cdFile := filepath.Join(t.TempDir(), "cd")
	e.cdFile = cdFile

	if err := runSwitch(e, []string{"-c", "-b", "feature/x", "review_1", "main"}); err != nil {
		t.Fatalf("runSwitch() error = %v\n%s", err, out.String())
	}

	want := filepath.Join(root, ".worktrees", "review_1")
	got, err := os.ReadFile(cdFile)
	if err != nil {
		t.Fatalf("reading cd file: %v", err)
	}
	if string(got) != want {
		t.Errorf("cd file = %q, want %q", got, want)
	}
	if branch := headBranch(t, want); branch != "feature/x" {
		t.Errorf("branch = %q, want %q", branch, "feature/x")
	}
}

func TestRemoveRefusesMainRepository(t *testing.T) {
	root := setupRepo(t)
	e, _ := newTestEnv(t, root)

	if err := runRemove(e, "."); err == nil {
		t.Error("runRemove(\".\") error = nil, want error in the main repository")
	}
}

func TestRemovePartialMatchAsksForConfirmation(t *testing.T) {
	root := setupRepo(t)
	e, out := newTestEnv(t, root)

	wtPath, err := runAdd(e, []string{"review_1"})
	if err != nil {
		t.Fatalf("runAdd() error = %v", err)
	}
	out.Reset()

	// 空の stdin は「いいえ」とみなされる
	err = runRemove(e, "review")
	if err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("runRemove() error = %v, want cancelled", err)
	}
	if !strings.Contains(out.String(), "Remove it?") {
		t.Errorf("output should ask for confirmation, got:\n%s", out.String())
	}
	if _, err := os.Stat(wtPath); err != nil {
		t.Errorf("worktree should still exist after cancelling: %v", err)
	}
}

func TestRemoveExactMatchRemovesWithoutPrompt(t *testing.T) {
	root := setupRepo(t)
	e, out := newTestEnv(t, root)

	wtPath, err := runAdd(e, []string{"review_1"})
	if err != nil {
		t.Fatalf("runAdd() error = %v", err)
	}
	out.Reset()

	if err := runRemove(e, "review_1"); err != nil {
		t.Fatalf("runRemove() error = %v\n%s", err, out.String())
	}
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Errorf("worktree should have been removed, stat err = %v", err)
	}
}

func TestInitCreatesConfigWithGitignore(t *testing.T) {
	root := setupRepo(t)
	e, out := newTestEnv(t, root)

	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("node_modules/\n\n# comment\n.env\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runInit(e); err != nil {
		t.Fatalf("runInit() error = %v\n%s", err, out.String())
	}

	got, err := os.ReadFile(filepath.Join(root, ".worktreelinks"))
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}
	content := string(got)

	// .gitignore のパターンはコメントアウトして転記する
	for _, want := range []string{"# node_modules/", "# .env", "# comment"} {
		if !strings.Contains(content, want) {
			t.Errorf("config should contain %q", want)
		}
	}
	// 二度目は上書きしない
	out.Reset()
	if err := runInit(e); err != nil {
		t.Fatalf("runInit() second call error = %v", err)
	}
	if !strings.Contains(out.String(), "already exists") {
		t.Errorf("second run should report the existing file, got:\n%s", out.String())
	}
}

func TestSyncRequiresWorktree(t *testing.T) {
	root := setupRepo(t)
	e, _ := newTestEnv(t, root)

	if err := runSync(e, false); err == nil {
		t.Error("runSync() error = nil, want error in the main repository")
	}
}
