// Package gitcmd は git コマンドの実行を抽象化する。
//
// Runner を挟むことで、git を呼ぶロジックをテストから差し替えられる。
// shell 版は git 関数を文字列マッチで差し替えるモックに頼っており、
// 引数を変えるとテストが壊れていた。
package gitcmd

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Runner は git の実行を抽象化する。dir は git -C に渡す作業ディレクトリで、
// 空文字ならカレントディレクトリで実行する。
type Runner interface {
	// Output は git を実行し、標準出力を返す。標準エラーは捨てる。
	Output(dir string, args ...string) (string, error)
	// Run は git を実行し、標準出力・標準エラーをそのまま流す。
	// worktree add のように進捗をユーザーに見せたいコマンドで使う。
	Run(dir string, args ...string) error
}

// Exec は実際に git を起動する Runner。
type Exec struct {
	Stdout io.Writer // nil なら os.Stdout
	Stderr io.Writer // nil なら os.Stderr
}

var _ Runner = Exec{}

func (e Exec) command(dir string, args ...string) *exec.Cmd {
	full := args
	if dir != "" {
		full = append([]string{"-C", dir}, args...)
	}
	return exec.Command("git", full...)
}

// Output は git を実行し、標準出力を返す。
func (e Exec) Output(dir string, args ...string) (string, error) {
	cmd := e.command(dir, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return out.String(), nil
}

// Run は git を実行し、出力を親プロセスに流す。
func (e Exec) Run(dir string, args ...string) error {
	cmd := e.command(dir, args...)
	cmd.Stdout = e.Stdout
	if cmd.Stdout == nil {
		cmd.Stdout = os.Stdout
	}
	cmd.Stderr = e.Stderr
	if cmd.Stderr == nil {
		cmd.Stderr = os.Stderr
	}
	return cmd.Run()
}

// Succeeds は git の終了ステータスだけを見る。
// show-ref --verify や check-ref-format のように、成否だけが意味を持つコマンドで使う。
func Succeeds(r Runner, dir string, args ...string) bool {
	_, err := r.Output(dir, args...)
	return err == nil
}

// ConfigGet は git config --get の値を返す。未設定なら空文字。
func ConfigGet(r Runner, dir, key string) string {
	out, err := r.Output(dir, "config", "--get", key)
	if err != nil {
		return ""
	}
	return strings.TrimRight(out, "\n")
}
