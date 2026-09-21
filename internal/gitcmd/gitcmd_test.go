package gitcmd

import (
	"os/exec"
	"strings"
	"testing"
)

func TestExecOutput(t *testing.T) {
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "--quiet", "-b", "main").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	got, err := Exec{}.Output(dir, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		t.Fatalf("Output() error = %v", err)
	}
	if strings.TrimSpace(got) != "true" {
		t.Errorf("Output() = %q, want %q", strings.TrimSpace(got), "true")
	}
}

func TestExecOutputFails(t *testing.T) {
	// git リポジトリでないディレクトリでは失敗する
	if _, err := (Exec{}).Output(t.TempDir(), "rev-parse", "--show-toplevel"); err == nil {
		t.Error("Output() error = nil, want error outside a repository")
	}
}

func TestConfigGet(t *testing.T) {
	tests := []struct {
		name string
		fake *Fake
		want string
	}{
		{
			name: "設定済みの値を返す",
			fake: &Fake{Outputs: map[string]string{"config --get pwt.worktreeDir": ".worktrees\n"}},
			want: ".worktrees",
		},
		{
			name: "未設定なら空文字",
			fake: &Fake{},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ConfigGet(tt.fake, "/repo", "pwt.worktreeDir"); got != tt.want {
				t.Errorf("ConfigGet() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSucceeds(t *testing.T) {
	ok := &Fake{Outputs: map[string]string{"show-ref": ""}}
	if !Succeeds(ok, "/repo", "show-ref", "--verify", "--quiet", "refs/heads/main") {
		t.Error("Succeeds() = false, want true")
	}

	if Succeeds(&Fake{}, "/repo", "show-ref", "--verify", "--quiet", "refs/heads/missing") {
		t.Error("Succeeds() = true, want false")
	}
}

func TestFakeRecordsCalls(t *testing.T) {
	f := &Fake{Outputs: map[string]string{"status": ""}}
	if _, err := f.Output("/repo", "status", "--short"); err != nil {
		t.Fatalf("Output() error = %v", err)
	}
	if len(f.Calls) != 1 || strings.Join(f.Calls[0], " ") != "status --short" {
		t.Errorf("Calls = %v, want [[status --short]]", f.Calls)
	}
}
