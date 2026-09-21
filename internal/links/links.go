// Package links は .worktreelinks の解釈を担う。
//
// .worktreelinks は gitignore 形式のパターンを並べたファイルで、
// [copy] / [link] セクションでモードを切り替える。パターンのマッチング自体は
// git ls-files --exclude-from に委譲するため、ここではセクション分割と
// 「有効なパターンがあるか」の判定だけを行う。
package links

import (
	"bufio"
	"io"
	"strings"
)

// モード名。
const (
	ModeLink = "link"
	ModeCopy = "copy"
)

// Patterns は .worktreelinks をセクションごとに分割した結果。
//
// コメント行・空行もそのまま保持する。分割結果は git ls-files --exclude-from に
// そのまま渡され、git 側が gitignore 形式として解釈するため。
type Patterns struct {
	Link []string
	Copy []string
}

// sectionOf はセクションヘッダ行を判定する。
// 行頭から始まる必要があり、末尾の空白は許容する。
func sectionOf(line string) (mode string, ok bool) {
	switch strings.TrimRight(line, " \t") {
	case "[" + ModeCopy + "]":
		return ModeCopy, true
	case "[" + ModeLink + "]":
		return ModeLink, true
	}
	return "", false
}

// Parse は .worktreelinks を読み、[link] / [copy] のパターン行に分割する。
// 既定のモードは link。セクションヘッダ行自体は出力に含めない。
func Parse(r io.Reader) (Patterns, error) {
	var p Patterns
	mode := ModeLink

	sc := bufio.NewScanner(r)
	// .gitignore 由来の長い行に備えてバッファを広げる
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for sc.Scan() {
		line := sc.Text()
		if m, ok := sectionOf(line); ok {
			mode = m
			continue
		}
		if mode == ModeCopy {
			p.Copy = append(p.Copy, line)
		} else {
			p.Link = append(p.Link, line)
		}
	}
	if err := sc.Err(); err != nil {
		return Patterns{}, err
	}
	return p, nil
}

// HasPatterns は有効なパターン行が 1 つでもあるかを返す。
// 空行・空白のみの行と、行頭が # の行はパターンとみなさない。
//
// 先頭に空白を挟んだ "  # foo" はパターン扱いになる。gitignore 形式では
// 行頭の空白が意味を持つため。
func HasPatterns(lines []string) bool {
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		return true
	}
	return false
}
