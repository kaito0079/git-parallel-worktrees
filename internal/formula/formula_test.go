package formula

import (
	"slices"
	"strings"
	"testing"
)

const sampleChecksums = `118201718636708354372a1e4346082a72d0a0a5b637abf18e0ee097a779b5c9  pwt_0.1.0_darwin_amd64.tar.gz
ce46960a4a7400cb62154a3b8e749e3b5b2837c140cc4d9f079cdf167bb2008e  pwt_0.1.0_darwin_arm64.tar.gz
bb9bfeafdcefb3fe99822dbcf481ef94e6c84d87bd1d5b6198c5046b8586ead3  pwt_0.1.0_linux_amd64.tar.gz
39475af292539b4497a3e0a12b992a07ac59e7d65050f284e23d50e42b04f929  pwt_0.1.0_linux_arm64.tar.gz
`

func sampleInput(t *testing.T) Input {
	t.Helper()
	assets, err := ParseChecksums(strings.NewReader(sampleChecksums))
	if err != nil {
		t.Fatalf("ParseChecksums() error = %v", err)
	}
	return Input{
		Repo:    "kaito0079/git-parallel-worktrees",
		Tag:     "v0.1.0",
		Version: "0.1.0",
		Assets:  assets,
	}
}

func TestParseChecksums(t *testing.T) {
	assets, err := ParseChecksums(strings.NewReader(sampleChecksums))
	if err != nil {
		t.Fatalf("ParseChecksums() error = %v", err)
	}

	var got []string
	for _, a := range assets {
		got = append(got, a.Platform())
	}
	want := []string{"darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64"}
	if !slices.Equal(got, want) {
		t.Errorf("platforms = %v, want %v", got, want)
	}

	if assets[0].SHA256 != "118201718636708354372a1e4346082a72d0a0a5b637abf18e0ee097a779b5c9" {
		t.Errorf("unexpected sha256: %s", assets[0].SHA256)
	}
}

func TestParseChecksumsIgnoresOtherArtifacts(t *testing.T) {
	in := "abc123  pwt_0.1.0_darwin_arm64.tar.gz\ndef456  pwt_0.1.0_checksums.txt\n"
	assets, err := ParseChecksums(strings.NewReader(in))
	if err != nil {
		t.Fatalf("ParseChecksums() error = %v", err)
	}
	if len(assets) != 1 {
		t.Fatalf("len(assets) = %d, want 1", len(assets))
	}
}

func TestParseChecksumsRejectsMalformedLine(t *testing.T) {
	if _, err := ParseChecksums(strings.NewReader("only-one-field\n")); err == nil {
		t.Error("ParseChecksums() error = nil, want error")
	}
}

func TestRender(t *testing.T) {
	got, err := Render(sampleInput(t))
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	// 4 プラットフォームすべての url と sha256 が入る
	for _, want := range []string{
		"https://github.com/kaito0079/git-parallel-worktrees/releases/download/v0.1.0/pwt_0.1.0_darwin_arm64.tar.gz",
		"https://github.com/kaito0079/git-parallel-worktrees/releases/download/v0.1.0/pwt_0.1.0_linux_amd64.tar.gz",
		"ce46960a4a7400cb62154a3b8e749e3b5b2837c140cc4d9f079cdf167bb2008e",
		"39475af292539b4497a3e0a12b992a07ac59e7d65050f284e23d50e42b04f929",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("formula should contain %q", want)
		}
	}

	// シェル統合ファイルと補完を配置する
	for _, want := range []string{
		`pkgshare.install "shell/pwt.sh"`,
		`generate_completions_from_executable(bin/"pwt", "completion")`,
		`version "0.1.0"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("formula should contain %q", want)
		}
	}
}

func TestRenderRequiresAllPlatforms(t *testing.T) {
	in := sampleInput(t)
	in.Assets = in.Assets[:2] // darwin のみ

	if _, err := Render(in); err == nil {
		t.Error("Render() error = nil, want error for missing platforms")
	}
}

func TestRenderRequiresMetadata(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Input)
	}{
		{"repo なし", func(in *Input) { in.Repo = "" }},
		{"tag なし", func(in *Input) { in.Tag = "" }},
		{"version なし", func(in *Input) { in.Version = "" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := sampleInput(t)
			tt.mutate(&in)
			if _, err := Render(in); err == nil {
				t.Error("Render() error = nil, want error")
			}
		})
	}
}

func TestPlatformOf(t *testing.T) {
	tests := []struct {
		file     string
		wantOS   string
		wantArch string
		wantErr  bool
	}{
		{file: "pwt_0.1.0_darwin_arm64.tar.gz", wantOS: "darwin", wantArch: "arm64"},
		{file: "pwt_0.1.0-rc1_linux_amd64.tar.gz", wantOS: "linux", wantArch: "amd64"},
		{file: "broken.tar.gz", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			gotOS, gotArch, err := platformOf(tt.file)
			if tt.wantErr {
				if err == nil {
					t.Fatal("platformOf() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("platformOf() error = %v", err)
			}
			if gotOS != tt.wantOS || gotArch != tt.wantArch {
				t.Errorf("platformOf() = %q/%q, want %q/%q", gotOS, gotArch, tt.wantOS, tt.wantArch)
			}
		})
	}
}
