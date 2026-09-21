package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

const switchUsage = "usage: pwt switch <index|branch|name>\n" +
	"       pwt switch -c [-b <branch>] [-B <branch>] [--detach] <path> [<commit-ish>]"

func newSwitchCommand(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "switch <index|branch|name>",
		Short: "Move to a worktree (requires shell integration)",
		Long: "Move to a worktree.\n\n" +
			"With -c, the remaining arguments are passed to `pwt add` and the\n" +
			"newly created worktree becomes the destination.\n\n" +
			"Changing the shell's directory requires the pwt shell function.\n" +
			"Without it, use `cd \"$(pwt path <target>)\"` instead.",
		// -c 以降は pwt add の引数をそのまま受け取る
		DisableFlagParsing: true,
		RunE:               func(cmd *cobra.Command, args []string) error { return runSwitch(e, args) },
	}
}

func runSwitch(e *env, args []string) error {
	if len(args) == 0 {
		return usagef("%s", switchUsage)
	}

	if args[0] == "-c" {
		rest := args[1:]
		if len(rest) == 0 {
			return usagef("-c requires arguments\n%s", switchUsage)
		}
		path, err := runAdd(e, rest)
		if err != nil {
			return err
		}
		return moveTo(e, path)
	}

	if len(args) > 1 {
		return usagef("too many arguments: %s", args[1])
	}

	path, err := lookupWorktree(e, args[0])
	if err != nil {
		return err
	}
	return moveTo(e, path)
}

// moveTo は shim に移動を依頼する。シェル統合が無ければ代替手段を案内する。
func moveTo(e *env, path string) error {
	if !e.shellIntegration() {
		return fmt.Errorf("shell integration is not enabled, so pwt cannot change the directory\n"+
			"  move with:  cd %q\n"+
			"  or enable the pwt shell function (see README)", path)
	}
	return e.requestCD(path)
}
