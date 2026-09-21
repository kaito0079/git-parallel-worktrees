// Package shell はシェル統合 (pwt.sh) のテストだけを持つ。
//
// pwt.sh は bash と zsh の両方で読み込まれる。zsh では status や path が
// 特殊変数であるなど、片方でしか起きない問題があるため、実際に両方の
// シェルを起動して検証する。
package shell

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := args
	if dir != "" {
		full = append([]string{"-C", dir}, args...)
	}
	if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", full, err, out)
	}
}

// buildBinary は pwt を一時ディレクトリにビルドし、その bin ディレクトリを返す。
func buildBinary(t *testing.T) string {
	t.Helper()
	bin := t.TempDir()
	cmd := exec.Command("go", "build", "-o", filepath.Join(bin, "pwt"), "../cmd/pwt")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// setupRepo はメインリポジトリと worktree を 1 つ用意する。
func setupRepo(t *testing.T, bin string) (root, worktree string) {
	t.Helper()

	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root = filepath.Join(base, "myapp")

	git(t, "", "init", "--quiet", "-b", "main", root)
	for _, kv := range [][2]string{
		{"user.email", "test@example.com"},
		{"user.name", "test"},
		{"commit.gpgsign", "false"},
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

	add := exec.Command(filepath.Join(bin, "pwt"), "add", "review_1")
	add.Dir = root
	add.Env = append(os.Environ(), "GIT_PARALLEL_WORKTREES_BASE=")
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("pwt add: %v\n%s", err, out)
	}

	return root, filepath.Join(root, ".worktrees", "review_1")
}

// runShell は shim を読み込んだ上で script を実行し、標準出力を返す。
func runShell(t *testing.T, shell, bin, dir, script string) string {
	t.Helper()

	shim, err := filepath.Abs("pwt.sh")
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(shell, "-c", "source "+shim+"\n"+script)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GIT_PARALLEL_WORKTREES_BASE=",
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", shell, err, out)
	}
	return string(out)
}

func TestShimChangesDirectory(t *testing.T) {
	bin := buildBinary(t)
	root, worktree := setupRepo(t, bin)

	for _, shell := range []string{"bash", "zsh"} {
		t.Run(shell, func(t *testing.T) {
			if _, err := exec.LookPath(shell); err != nil {
				t.Skipf("%s is not installed", shell)
			}

			out := runShell(t, shell, bin, root, "pwt switch review_1\npwd")
			if got := strings.TrimSpace(lastLine(out)); got != worktree {
				t.Errorf("pwd = %q, want %q\nfull output:\n%s", got, worktree, out)
			}
		})
	}
}

// 移動を伴わないサブコマンドは素通しする。
func TestShimPassesThroughOtherCommands(t *testing.T) {
	bin := buildBinary(t)
	root, worktree := setupRepo(t, bin)

	for _, shell := range []string{"bash", "zsh"} {
		t.Run(shell, func(t *testing.T) {
			if _, err := exec.LookPath(shell); err != nil {
				t.Skipf("%s is not installed", shell)
			}

			out := runShell(t, shell, bin, root, "pwt path review_1")
			if got := strings.TrimSpace(lastLine(out)); got != worktree {
				t.Errorf("output = %q, want %q", got, worktree)
			}
		})
	}
}

// 存在しない対象では移動せず、終了コードが伝わる。
func TestShimPropagatesFailure(t *testing.T) {
	bin := buildBinary(t)
	root, _ := setupRepo(t, bin)

	for _, shell := range []string{"bash", "zsh"} {
		t.Run(shell, func(t *testing.T) {
			if _, err := exec.LookPath(shell); err != nil {
				t.Skipf("%s is not installed", shell)
			}

			shim, err := filepath.Abs("pwt.sh")
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(shell, "-c", "source "+shim+"\npwt switch no-such-worktree\necho rc=$?\npwd")
			cmd.Dir = root
			cmd.Env = append(os.Environ(),
				"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
				"GIT_PARALLEL_WORKTREES_BASE=",
			)
			out, _ := cmd.CombinedOutput()

			if !strings.Contains(string(out), "rc=1") {
				t.Errorf("exit code should be propagated, got:\n%s", out)
			}
			if got := strings.TrimSpace(lastLine(string(out))); got != root {
				t.Errorf("should stay in %q, got %q", root, got)
			}
		})
	}
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return lines[len(lines)-1]
}
