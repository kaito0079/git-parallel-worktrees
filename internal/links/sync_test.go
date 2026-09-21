package links

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaito0079/git-parallel-worktrees/internal/gitcmd"
)

// 実 git を使う統合テスト。git ls-files --exclude-from の挙動そのものを
// 検証対象にするため、モックではなく本物の git を呼ぶ。

func gitInit(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "init", "--quiet", "-b", "main")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func isSymlink(t *testing.T, path string) bool {
	t.Helper()
	fi, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeSymlink != 0
}

func TestSyncCreatesLinksAndCopies(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	gitInit(t, src)

	writeFile(t, filepath.Join(src, ".env"), "secret")
	writeFile(t, filepath.Join(src, "node_modules/pkg/index.js"), "module")
	writeFile(t, filepath.Join(src, "vendor/autoload.php"), "autoload")
	writeFile(t, filepath.Join(dest, ConfigName), ".env\nnode_modules\n[copy]\nvendor\n")

	// 前回の同期で残った、今はパターンに含まれないリンク
	stale := filepath.Join(dest, "stale")
	if err := os.Symlink(filepath.Join(src, ".env"), stale); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	res, err := Sync(gitcmd.Exec{}, src, dest, Options{Out: &out})
	if err != nil {
		t.Fatalf("Sync() error = %v\n%s", err, out.String())
	}

	if !isSymlink(t, filepath.Join(dest, ".env")) {
		t.Error(".env should be a symlink")
	}
	if target, _ := os.Readlink(filepath.Join(dest, ".env")); target != filepath.Join(src, ".env") {
		t.Errorf(".env target = %q, want %q", target, filepath.Join(src, ".env"))
	}
	if !isSymlink(t, filepath.Join(dest, "node_modules")) {
		t.Error("node_modules should be a symlink")
	}

	if isSymlink(t, filepath.Join(dest, "vendor")) {
		t.Error("vendor should be a real directory, not a symlink")
	}
	if got := readFile(t, filepath.Join(dest, "vendor/autoload.php")); got != "autoload" {
		t.Errorf("copied file content = %q, want %q", got, "autoload")
	}

	if _, err := os.Lstat(stale); !os.IsNotExist(err) {
		t.Error("stale symlink should have been removed")
	}
	if res.Linked != 2 {
		t.Errorf("Linked = %d, want 2", res.Linked)
	}
	if res.Copied != 1 {
		t.Errorf("Copied = %d, want 1", res.Copied)
	}
	if res.Cleaned != 1 {
		t.Errorf("Cleaned = %d, want 1", res.Cleaned)
	}
}

func TestSyncCopySkipsRealEntryUnlessForced(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	gitInit(t, src)

	writeFile(t, filepath.Join(src, "vendor/autoload.php"), "new")
	writeFile(t, filepath.Join(dest, "vendor/autoload.php"), "old")
	writeFile(t, filepath.Join(dest, ConfigName), "[copy]\nvendor\n")

	var out bytes.Buffer
	if _, err := Sync(gitcmd.Exec{}, src, dest, Options{Out: &out}); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if got := readFile(t, filepath.Join(dest, "vendor/autoload.php")); got != "old" {
		t.Errorf("without force: content = %q, want %q", got, "old")
	}
	if !strings.Contains(out.String(), "use -f to overwrite") {
		t.Errorf("without force: output should hint at -f, got:\n%s", out.String())
	}

	out.Reset()
	res, err := Sync(gitcmd.Exec{}, src, dest, Options{Force: true, Out: &out})
	if err != nil {
		t.Fatalf("Sync(force) error = %v", err)
	}
	if got := readFile(t, filepath.Join(dest, "vendor/autoload.php")); got != "new" {
		t.Errorf("with force: content = %q, want %q", got, "new")
	}
	if !strings.Contains(out.String(), "[overwrite]") {
		t.Errorf("with force: output should report overwrite, got:\n%s", out.String())
	}
	if res.Copied != 1 {
		t.Errorf("Copied = %d, want 1", res.Copied)
	}
}

func TestSyncCopyReplacesExistingSymlink(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	other := t.TempDir()
	gitInit(t, src)

	writeFile(t, filepath.Join(src, "vendor/autoload.php"), "autoload")
	writeFile(t, filepath.Join(dest, ConfigName), "[copy]\nvendor\n")
	if err := os.Symlink(other, filepath.Join(dest, "vendor")); err != nil {
		t.Fatal(err)
	}

	if _, err := Sync(gitcmd.Exec{}, src, dest, Options{}); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	if isSymlink(t, filepath.Join(dest, "vendor")) {
		t.Error("vendor should have been replaced by a real directory")
	}
	if got := readFile(t, filepath.Join(dest, "vendor/autoload.php")); got != "autoload" {
		t.Errorf("content = %q, want %q", got, "autoload")
	}
}

// force は [copy] 専用で、[link] 対象の実ファイルは壊さない。
func TestSyncForceDoesNotTouchLinkEntries(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	gitInit(t, src)

	writeFile(t, filepath.Join(src, ".env"), "secret")
	writeFile(t, filepath.Join(dest, ".env"), "local")
	writeFile(t, filepath.Join(dest, ConfigName), ".env\n")

	if _, err := Sync(gitcmd.Exec{}, src, dest, Options{Force: true}); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	if isSymlink(t, filepath.Join(dest, ".env")) {
		t.Error(".env should stay a real file even with force")
	}
	if got := readFile(t, filepath.Join(dest, ".env")); got != "local" {
		t.Errorf("content = %q, want %q", got, "local")
	}
}

func TestSyncMissingConfig(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	if _, err := Sync(gitcmd.Exec{}, src, dest, Options{}); err == nil {
		t.Error("Sync() error = nil, want error when .worktreelinks is missing")
	}
}

func TestCleanSymlinks(t *testing.T) {
	src := t.TempDir()
	dest := t.TempDir()
	other := t.TempDir()

	writeFile(t, filepath.Join(src, "linked_file"), "content")
	if err := os.Mkdir(filepath.Join(src, "linked_dir"), 0o755); err != nil {
		t.Fatal(err)
	}

	mustSymlink := func(target, link string) {
		t.Helper()
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
	}
	mustSymlink(filepath.Join(src, "linked_file"), filepath.Join(dest, "linked_file"))
	mustSymlink(filepath.Join(src, "linked_dir"), filepath.Join(dest, "linked_dir"))
	mustSymlink(other, filepath.Join(dest, "other_link"))

	// ネストした位置のリンクも対象になる
	if err := os.Mkdir(filepath.Join(dest, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustSymlink(filepath.Join(src, "linked_file"), filepath.Join(dest, "nested/deep_link"))

	count, err := CleanSymlinks(src, dest)
	if err != nil {
		t.Fatalf("CleanSymlinks() error = %v", err)
	}

	for _, p := range []string{"linked_file", "linked_dir", "nested/deep_link"} {
		if _, err := os.Lstat(filepath.Join(dest, p)); !os.IsNotExist(err) {
			t.Errorf("%s should have been removed", p)
		}
	}
	if !isSymlink(t, filepath.Join(dest, "other_link")) {
		t.Error("other_link should remain")
	}
	if count != 3 {
		t.Errorf("count = %d, want 3", count)
	}
}

// リンク先が相対パスでも src 配下と判定できる。
func TestCleanSymlinksRelativeTarget(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "src")
	sub := filepath.Join(base, "dest", "sub")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(src, "linked_file"), "content")

	rel, err := filepath.Rel(sub, filepath.Join(src, "linked_file"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(rel, filepath.Join(sub, "rel_link")); err != nil {
		t.Fatal(err)
	}

	count, err := CleanSymlinks(src, filepath.Join(base, "dest"))
	if err != nil {
		t.Fatalf("CleanSymlinks() error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(sub, "rel_link")); !os.IsNotExist(err) {
		t.Error("relative symlink pointing into src should have been removed")
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
}
