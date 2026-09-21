package links

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/kaito0079/git-parallel-worktrees/internal/gitcmd"
)

// ConfigName は worktree ごとに置かれる設定ファイル名。
const ConfigName = ".worktreelinks"

// Result は Sync の結果。
type Result struct {
	Cleaned int // 削除したシンボリックリンクの数
	Linked  int // 作成したシンボリックリンクの数
	Copied  int // コピーしたファイル/ディレクトリの数
}

// Options は Sync の挙動を制御する。
type Options struct {
	// Force は [copy] 対象の宛先に実ファイル/実ディレクトリがある場合に
	// 削除して上書きする。[link] 対象には影響しない。
	Force bool
	// Out は進捗の出力先。nil なら出力しない。
	Out io.Writer
}

// Sync は destRoot の .worktreelinks に従い、srcRoot からリンク/コピーを作る。
// 既存のシンボリックリンクは先にすべて削除してから貼り直す。
func Sync(r gitcmd.Runner, srcRoot, destRoot string, opts Options) (Result, error) {
	var res Result

	f, err := os.Open(filepath.Join(destRoot, ConfigName))
	if err != nil {
		return res, fmt.Errorf("%s not found in %s", ConfigName, destRoot)
	}
	defer f.Close()

	pat, err := Parse(f)
	if err != nil {
		return res, err
	}

	res.Cleaned, err = CleanSymlinks(srcRoot, destRoot)
	if err != nil {
		return res, err
	}

	if HasPatterns(pat.Link) {
		n, err := processEntries(r, ModeLink, srcRoot, destRoot, pat.Link, false, opts.Out)
		res.Linked = n
		if err != nil {
			return res, err
		}
	}
	if HasPatterns(pat.Copy) {
		n, err := processEntries(r, ModeCopy, srcRoot, destRoot, pat.Copy, opts.Force, opts.Out)
		res.Copied = n
		if err != nil {
			return res, err
		}
	}

	return res, nil
}

// CleanSymlinks は destRoot 以下のシンボリックリンクのうち、srcRoot 配下を
// 指すものを削除する。他の場所を指すリンクは残す。
//
// shell 版は find -maxdepth 20 で深さを制限していたが、WalkDir は
// シンボリックリンクを辿らないため深さ制限は不要。
func CleanSymlinks(srcRoot, destRoot string) (int, error) {
	srcRoot = strings.TrimSuffix(srcRoot, "/")
	count := 0

	err := filepath.WalkDir(destRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			// 読めないディレクトリは飛ばす
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() && d.Name() == ".git" {
			return fs.SkipDir
		}
		if d.Type()&fs.ModeSymlink == 0 {
			return nil
		}

		target, rerr := os.Readlink(p)
		if rerr != nil {
			return nil
		}
		resolved := target
		if !filepath.IsAbs(target) {
			resolved = filepath.Clean(filepath.Join(filepath.Dir(p), target))
		}
		if resolved == srcRoot || strings.HasPrefix(resolved, srcRoot+"/") {
			if err := os.Remove(p); err != nil {
				return err
			}
			count++
		}
		return nil
	})

	return count, err
}

// processEntries は git ls-files が列挙したエントリを 1 件ずつ処理する。
// 戻り値は作成できた件数。
func processEntries(r gitcmd.Runner, mode, srcRoot, destRoot string, patterns []string, force bool, out io.Writer) (int, error) {
	entries, err := listEntries(r, srcRoot, patterns)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, entry := range entries {
		src := filepath.Join(srcRoot, entry)
		dest := filepath.Join(destRoot, entry)
		if _, err := os.Lstat(src); err != nil {
			continue
		}

		created, err := placeEntry(mode, src, dest, entry, force, out)
		if err != nil {
			return count, err
		}
		if created {
			count++
		}
	}

	return count, nil
}

// listEntries は .worktreelinks のパターンにマッチするエントリを git に列挙させる。
// gitignore 形式の解釈を自前で再実装せず git に委譲するのが pwt の方針。
func listEntries(r gitcmd.Runner, srcRoot string, patterns []string) ([]string, error) {
	tmp, err := os.CreateTemp("", "pwt-patterns-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.WriteString(strings.Join(patterns, "\n") + "\n"); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}

	stdout, err := r.Output(srcRoot, "ls-files", "-z",
		"--others", "--ignored", "--exclude-from="+tmp.Name(), "--directory")
	if err != nil {
		return nil, fmt.Errorf("git ls-files failed: %w", err)
	}

	var out []string
	for _, raw := range strings.Split(stdout, "\x00") {
		entry := strings.TrimSuffix(raw, "/")
		if entry == "" {
			continue
		}
		// 多層防御。git ls-files の出力は安全だが念のため検査する
		if hasDotDotSegment(entry) {
			fmt.Fprintf(os.Stderr, "  [!] skipping invalid path: %s\n", entry)
			continue
		}
		out = append(out, entry)
	}

	return out, nil
}

// placeEntry は 1 エントリをリンクまたはコピーで配置する。
// 戻り値は実際に配置したかどうか（スキップした場合は false）。
func placeEntry(mode, src, dest, entry string, force bool, out io.Writer) (bool, error) {
	destInfo, destErr := os.Lstat(dest)
	destExists := destErr == nil
	destIsSymlink := destExists && destInfo.Mode()&os.ModeSymlink != 0

	if mode == ModeLink {
		// 実体が置かれている場合は触らない
		if destExists && !destIsSymlink {
			printSkip(out, entry, destInfo.IsDir(), false)
			return false, nil
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return false, err
		}
		if err := os.Remove(dest); err != nil && !os.IsNotExist(err) {
			return false, err
		}
		if err := os.Symlink(src, dest); err != nil {
			return false, err
		}
		printAction(out, "link", entry, isDir(src))
		return true, nil
	}

	overwritten := false
	if destExists {
		switch {
		case destIsSymlink:
			// 前回リンクだったものをコピーに切り替える場合
			if err := os.Remove(dest); err != nil {
				return false, err
			}
		case force:
			if err := os.RemoveAll(dest); err != nil {
				return false, err
			}
			overwritten = true
		default:
			printSkip(out, entry, destInfo.IsDir(), true)
			return false, nil
		}
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return false, err
	}
	if err := copyTree(src, dest); err != nil {
		return false, err
	}

	label := "copy"
	if overwritten {
		label = "overwrite"
	}
	printAction(out, label, entry, isDir(src))
	return true, nil
}

// copyTree は src を dest に再帰コピーする。シンボリックリンクは
// 辿らずリンクのままコピーする（cp -RP 相当）。
func copyTree(src, dest string) error {
	fi, err := os.Lstat(src)
	if err != nil {
		return err
	}

	switch {
	case fi.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dest)

	case fi.IsDir():
		if err := os.MkdirAll(dest, fi.Mode().Perm()); err != nil {
			return err
		}
		children, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, c := range children {
			if err := copyTree(filepath.Join(src, c.Name()), filepath.Join(dest, c.Name())); err != nil {
				return err
			}
		}
		return nil

	default:
		return copyFile(src, dest, fi.Mode().Perm())
	}
}

func copyFile(src, dest string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	tmp := dest + ".pwt-tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, in); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}

	return os.Rename(tmp, dest)
}

// hasDotDotSegment はパスに .. のセグメントが含まれるかを返す。
func hasDotDotSegment(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func printAction(out io.Writer, label, entry string, dir bool) {
	if out == nil {
		return
	}
	if dir {
		entry += "/"
	}
	fmt.Fprintf(out, "  [%s] %s\n", label, entry)
}

func printSkip(out io.Writer, entry string, dir, hintForce bool) {
	if out == nil {
		return
	}
	kind := "file"
	if dir {
		kind = "directory"
		entry += "/"
	}
	hint := ""
	if hintForce {
		hint = "; use -f to overwrite"
	}
	fmt.Fprintf(out, "  [skip] %s (real %s exists%s)\n", entry, kind, hint)
}
