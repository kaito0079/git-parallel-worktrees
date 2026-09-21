package gitcmd

import (
	"fmt"
	"strings"
)

// Fake はテスト用の Runner。
//
// Outputs は引数を空白で連結した文字列（例: "config --get pwt.worktreeDir"）を
// キーとする応答表。前方一致で引く。該当がなければ OutputErr を返す。
type Fake struct {
	Outputs   map[string]string
	OutputErr error

	// RunErr は Run の戻り値。
	RunErr error

	// Calls は Output / Run に渡された引数の記録。
	Calls [][]string
}

var _ Runner = (*Fake)(nil)

// Output は Outputs から前方一致で応答を引く。
func (f *Fake) Output(dir string, args ...string) (string, error) {
	f.Calls = append(f.Calls, args)
	key := strings.Join(args, " ")
	for prefix, out := range f.Outputs {
		if strings.HasPrefix(key, prefix) {
			return out, nil
		}
	}
	if f.OutputErr != nil {
		return "", f.OutputErr
	}
	return "", fmt.Errorf("fake: no response for %q", key)
}

// Run は RunErr を返すだけ。
func (f *Fake) Run(dir string, args ...string) error {
	f.Calls = append(f.Calls, args)
	return f.RunErr
}
