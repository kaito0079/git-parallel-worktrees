// Command pwt は git worktree の薄いラッパー。
//
// worktree 間の移動はシェルの作業ディレクトリを変える必要があるため、
// pwt シェル関数（shim）と組み合わせて使う。shim なしでも
// `cd "$(pwt path <target>)"` で移動できる。
package main

import (
	"os"

	"github.com/kaito0079/git-parallel-worktrees/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
