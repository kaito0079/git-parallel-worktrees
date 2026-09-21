package links

import (
	"slices"
	"strings"
	"testing"
)

// effective はコメント行・空行を除いた有効なパターン行だけを返す（テスト用）。
func effective(lines []string) []string {
	var out []string
	for _, l := range lines {
		if strings.TrimSpace(l) == "" || strings.HasPrefix(l, "#") {
			continue
		}
		out = append(out, l)
	}
	return out
}

func TestParse(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantLink []string
		wantCopy []string
	}{
		{
			name: "セクションごとに分割される",
			input: `# comment
.env
.env.*

[copy]
vendor/
node_modules/

[link]
.docker/
`,
			wantLink: []string{".env", ".env.*", ".docker/"},
			wantCopy: []string{"vendor/", "node_modules/"},
		},
		{
			name:     "セクションヘッダの末尾空白を許容する",
			input:    ".env\n[copy]  \nvendor/\n",
			wantLink: []string{".env"},
			wantCopy: []string{"vendor/"},
		},
		{
			name:     "セクションがなければすべて link",
			input:    ".env\nvendor/\n",
			wantLink: []string{".env", "vendor/"},
			wantCopy: nil,
		},
		{
			name:     "空ファイル",
			input:    "",
			wantLink: nil,
			wantCopy: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(strings.NewReader(tt.input))
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if gotLink := effective(got.Link); !slices.Equal(gotLink, tt.wantLink) {
				t.Errorf("link = %q, want %q", gotLink, tt.wantLink)
			}
			if gotCopy := effective(got.Copy); !slices.Equal(gotCopy, tt.wantCopy) {
				t.Errorf("copy = %q, want %q", gotCopy, tt.wantCopy)
			}
		})
	}
}

// Parse はコメント行と空行を捨てない。分割結果をそのまま
// git ls-files --exclude-from に渡すため。
func TestParseKeepsCommentsAndBlanks(t *testing.T) {
	got, err := Parse(strings.NewReader("# comment\n\n.env\n"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	want := []string{"# comment", "", ".env"}
	if !slices.Equal(got.Link, want) {
		t.Errorf("link = %q, want %q", got.Link, want)
	}
}

func TestHasPatterns(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		want  bool
	}{
		{"空", nil, false},
		{"空行のみ", []string{""}, false},
		{"コメントのみ", []string{"# comment", ""}, false},
		{"空白のみの行", []string{"   ", "  "}, false},
		{"パターンあり", []string{"# comment", ".env"}, true},
		{"末尾空白つきのパターン", []string{"vendor/  "}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasPatterns(tt.lines); got != tt.want {
				t.Errorf("HasPatterns(%q) = %v, want %v", tt.lines, got, tt.want)
			}
		})
	}
}
