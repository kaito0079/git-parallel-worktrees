// Command formula は goreleaser の成果物から Homebrew formula を生成する。
//
//	goreleaser release --clean
//	go run ./tools/formula
//
// 生成物を tap リポジトリ (kaito0079/homebrew-tap) の Formula/pwt.rb に置く。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kaito0079/git-parallel-worktrees/internal/formula"
)

// metadata は goreleaser が出力する dist/metadata.json の必要な部分。
type metadata struct {
	Tag     string `json:"tag"`
	Version string `json:"version"`
}

func main() {
	dist := flag.String("dist", "dist", "goreleaser の出力ディレクトリ")
	repo := flag.String("repo", "kaito0079/git-parallel-worktrees", "リリース元リポジトリ (owner/name)")
	out := flag.String("out", "", "出力先 (既定: <dist>/Formula/pwt.rb)")
	flag.Parse()

	if *out == "" {
		*out = filepath.Join(*dist, "Formula", "pwt.rb")
	}

	if err := run(*dist, *repo, *out); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(dist, repo, out string) error {
	meta, err := readMetadata(filepath.Join(dist, "metadata.json"))
	if err != nil {
		return err
	}

	f, err := os.Open(filepath.Join(dist, "checksums.txt"))
	if err != nil {
		return err
	}
	defer f.Close()

	assets, err := formula.ParseChecksums(f)
	if err != nil {
		return err
	}

	content, err := formula.Render(formula.Input{
		Repo:    repo,
		Tag:     meta.Tag,
		Version: meta.Version,
		Assets:  assets,
	})
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(out, []byte(content), 0o644); err != nil {
		return err
	}

	fmt.Printf("wrote %s (%s)\n", out, meta.Tag)
	return nil
}

func readMetadata(path string) (metadata, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return metadata{}, err
	}
	var m metadata
	if err := json.Unmarshal(b, &m); err != nil {
		return metadata{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	if m.Tag == "" || m.Version == "" {
		return metadata{}, fmt.Errorf("%s has no tag/version", path)
	}
	return m, nil
}
