package repo

import (
	"errors"
	"testing"
)

func TestValidateBranchName(t *testing.T) {
	tests := []struct {
		name    string
		branch  string
		wantErr bool
	}{
		{"有効: feature/foo", "feature/foo", false},
		{"有効: main", "main", false},
		{"有効: fix/123", "fix/123", false},
		{"無効: 空文字", "", true},
		{"無効: -d (ダッシュ始まり)", "-d", true},
		{"無効: --force", "--force", true},
		// a..b のような git のリファレンス名としての不正は
		// git check-ref-format に委ねるため、ここでは通す
		{"git に委ねる: a..b", "a..b", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateBranchName(tt.branch)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateBranchName(%q) error = %v, wantErr %v", tt.branch, err, tt.wantErr)
			}
		})
	}
}

func TestValidateBranchNameEmptyIsSentinel(t *testing.T) {
	if err := ValidateBranchName(""); !errors.Is(err, ErrEmptyBranchName) {
		t.Errorf("ValidateBranchName(\"\") = %v, want ErrEmptyBranchName", err)
	}
}
