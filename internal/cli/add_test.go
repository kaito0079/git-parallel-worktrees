package cli

import (
	"slices"
	"testing"
)

func TestParseAddArgs(t *testing.T) {
	tests := []struct {
		name            string
		args            []string
		wantNewBranch   string
		wantResetBranch string
		wantPassthrough []string
		wantPositional  []string
		wantErr         bool
	}{
		{
			name:           "<path> のみ",
			args:           []string{"review_1"},
			wantPositional: []string{"review_1"},
		},
		{
			name:           "<path> と <commit-ish>",
			args:           []string{"review_1", "main"},
			wantPositional: []string{"review_1", "main"},
		},
		{
			name:           "-b でブランチ指定",
			args:           []string{"-b", "feature/x", "review_1", "main"},
			wantNewBranch:  "feature/x",
			wantPositional: []string{"review_1", "main"},
		},
		{
			name:            "-B でブランチ強制作成",
			args:            []string{"-B", "feature/x", "review_1"},
			wantResetBranch: "feature/x",
			wantPositional:  []string{"review_1"},
		},
		{
			name:            "passthrough フラグはそのまま渡す",
			args:            []string{"--detach", "--no-checkout", "-q", "review_1"},
			wantPassthrough: []string{"--detach", "--no-checkout", "-q"},
			wantPositional:  []string{"review_1"},
		},
		{
			name:            "--reason は値ごと passthrough",
			args:            []string{"--reason", "in review", "review_1"},
			wantPassthrough: []string{"--reason", "in review"},
			wantPositional:  []string{"review_1"},
		},
		{
			name:           "-- 以降はすべて位置引数",
			args:           []string{"--", "-weird-name"},
			wantPositional: []string{"-weird-name"},
		},
		{
			name:           "-- の後のフラグらしき文字列も位置引数",
			args:           []string{"--", "review_1", "--detach"},
			wantPositional: []string{"review_1", "--detach"},
		},
		{
			name:    "-b に値がない",
			args:    []string{"-b"},
			wantErr: true,
		},
		{
			name:    "-B に値がない",
			args:    []string{"-B"},
			wantErr: true,
		},
		{
			name:    "--reason に値がない",
			args:    []string{"--reason"},
			wantErr: true,
		},
		{
			name:    "未知のオプション",
			args:    []string{"--nope", "review_1"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAddArgs(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseAddArgs() error = nil, want error")
				}
				var ue usageError
				if !asUsageError(err, &ue) {
					t.Errorf("parseAddArgs() error should be a usageError, got %T", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseAddArgs() error = %v", err)
			}
			if got.newBranch != tt.wantNewBranch {
				t.Errorf("newBranch = %q, want %q", got.newBranch, tt.wantNewBranch)
			}
			if got.resetBranch != tt.wantResetBranch {
				t.Errorf("resetBranch = %q, want %q", got.resetBranch, tt.wantResetBranch)
			}
			if !slices.Equal(got.passthrough, tt.wantPassthrough) {
				t.Errorf("passthrough = %q, want %q", got.passthrough, tt.wantPassthrough)
			}
			if !slices.Equal(got.positional, tt.wantPositional) {
				t.Errorf("positional = %q, want %q", got.positional, tt.wantPositional)
			}
		})
	}
}

func TestAddArgsHasDetach(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{"-d", []string{"-d"}, true},
		{"--detach", []string{"--detach"}, true},
		{"--no-detach は detach 指定ではない", []string{"--no-detach"}, false},
		{"なし", []string{"-q"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := addArgs{passthrough: tt.args}
			if got := a.hasDetach(); got != tt.want {
				t.Errorf("hasDetach() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsAutoBranchCandidate(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"review_1", true},
		{"feature/PROJ-123", true},
		{"/abs/path", false},
		{"./wt", false},
		{"../wt", false},
		{".", false},
		{"..", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := isAutoBranchCandidate(tt.path); got != tt.want {
				t.Errorf("isAutoBranchCandidate(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}
