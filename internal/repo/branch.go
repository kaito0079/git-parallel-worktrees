package repo

import (
	"errors"
	"fmt"
	"strings"
)

// ErrEmptyBranchName はブランチ名が空のときに返る。
var ErrEmptyBranchName = errors.New("branch name is empty")

// ValidateBranchName はブランチ名のうち git を呼ばずに判定できる部分を検証する。
// '-' 始まりの拒否（フラグインジェクション防止）が主目的。
//
// git のリファレンス名としての妥当性（a..b のような形式）は
// git check-ref-format --branch に委ね、gitcmd 側で重ねて検証する。
func ValidateBranchName(branch string) error {
	if branch == "" {
		return ErrEmptyBranchName
	}
	if strings.HasPrefix(branch, "-") {
		return fmt.Errorf("invalid branch name (starts with %q): %s", "-", branch)
	}
	return nil
}
