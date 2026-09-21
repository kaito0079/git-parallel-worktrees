package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kaito0079/git-parallel-worktrees/internal/links"
)

// configTemplate は pwt init が生成する .worktreelinks の冒頭。
const configTemplate = `# .worktreelinks - patterns for files shared with worktrees
#
# Uncomment the patterns you want to share.
# Patterns use .gitignore syntax:
#   .env            matches at any depth (no slash in the pattern)
#   path/to/file    relative to the repository root
#   *.log           wildcards
#
# [Libraries (node_modules, vendor, ...)]
# Symlinking them lets a new worktree start working right away.
# To install separately in one worktree:
#   1. comment the pattern out in this file
#   2. pwt sync      the symlink is dropped
#   3. npm install   install into this worktree only
# Other worktrees are unaffected.
#
# [Version control]
# Commit this file to share the configuration with your team.
# Either way, each worktree keeps its own copy.
#
# [Copy mode]
# When symlinks do not work (inside Docker, for example), put the pattern
# under [copy] to copy the real files instead.
#   .env              <- symlink (default)
#   [copy]
#   vendor/           <- copy
# [link] switches back to symlink mode.
#
# [Limitation]
# Only entries reported by ` + "`git ls-files --others --ignored`" + ` can be shared,
# that is, entries ignored by the main repository's .gitignore (or
# .git/info/exclude).

`

func newInitCommand(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create .worktreelinks in the main repository",
		Long: "Create .worktreelinks in the main repository.\n\n" +
			"The repository's .gitignore is transcribed as comments so that you\n" +
			"can uncomment the entries you want to share.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return runInit(e) },
	}
}

func runInit(e *env) error {
	ctx, err := e.context()
	if err != nil {
		return err
	}

	config := filepath.Join(ctx.ProjectRoot, links.ConfigName)
	if _, err := os.Stat(config); err == nil {
		fmt.Fprintf(e.stdout, "%s already exists: %s\n", links.ConfigName, config)
		return nil
	}

	fmt.Fprintf(e.stdout, "==> Creating %s for %s\n", links.ConfigName, ctx.ProjectName)

	var b strings.Builder
	b.WriteString(configTemplate)
	b.WriteString(gitignoreSection(filepath.Join(ctx.ProjectRoot, ".gitignore")))

	if err := os.WriteFile(config, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", config, err)
	}

	fmt.Fprintf(e.stdout, "  [+] %s\n", config)
	fmt.Fprintln(e.stdout, "\n  next steps:")
	fmt.Fprintf(e.stdout, "    1. edit %s and uncomment what you want to share\n", config)
	fmt.Fprintln(e.stdout, "    2. pwt add <path>")

	return nil
}

// gitignoreSection は .gitignore の各行をコメントとして転記する。
// パターンをそのまま有効にせず、利用者がコメントを外して選ぶ形にする。
func gitignoreSection(gitignore string) string {
	f, err := os.Open(gitignore)
	if err != nil {
		return "# .gitignore not found; add patterns manually\n"
	}
	defer f.Close()

	var b strings.Builder
	b.WriteString("# --- transcribed from .gitignore ---\n")

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			b.WriteString("\n")
		case strings.HasPrefix(line, "#"):
			b.WriteString(line + "\n")
		default:
			b.WriteString("# " + line + "\n")
		}
	}

	return b.String()
}
