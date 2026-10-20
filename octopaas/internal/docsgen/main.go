// Command docsgen writes octopaas's command reference for the docs site, one
// page per command group, from the same cobra tree `--help` prints. Run it with
// `task octopaas:docs`; `task octopaas:docs:check` fails when the committed
// pages are stale, so a changed flag or example cannot ship undocumented.
//
// Usage: go run ./internal/docsgen <output dir>
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/juancavallotti/octo/octopaas/internal/cmd"
)

func main() {

	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: docsgen <output dir>")
		os.Exit(2)
	}
	if err := generate(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "docsgen:", err)
		os.Exit(1)
	}
}

func generate(dir string) error {

	root := cmd.NewRoot(&cmd.App{Getenv: func(string) string { return "" }})
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	//we start from an empty folder so a removed command takes its page with it.
	old, _ := filepath.Glob(filepath.Join(dir, "*.mdx"))
	for _, f := range old {
		if err := os.Remove(f); err != nil {
			return err
		}
	}

	pages := []string{"index"}
	if err := write(dir, "index", indexPage(root)); err != nil {
		return err
	}
	for _, group := range groups(root) {
		pages = append(pages, group.Name())
		if err := write(dir, group.Name(), groupPage(group)); err != nil {
			return err
		}
	}

	meta := fmt.Sprintf("{\n  \"title\": \"octopaas CLI\",\n  \"pages\": [%s]\n}\n", quoteAll(pages))
	return os.WriteFile(filepath.Join(dir, "meta.json"), []byte(meta), 0o644)
}

// groups are the top-level commands worth a page: everything but help, in the
// order `--help` groups them, with the help topics last.
func groups(root *cobra.Command) []*cobra.Command {

	rank := map[string]int{}
	for i, g := range root.Groups() {
		rank[g.ID] = i
	}
	var ret []*cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "help" || c.Hidden || !c.IsAvailableCommand() && !c.IsAdditionalHelpTopicCommand() {
			continue
		}
		ret = append(ret, c)
	}
	order := func(c *cobra.Command) int {
		if r, ok := rank[c.GroupID]; ok {
			return r
		}
		return len(rank)
	}
	sort.SliceStable(ret, func(i, j int) bool { return order(ret[i]) < order(ret[j]) })
	return ret
}

const banner = "{/* Generated from the octopaas command tree by `task octopaas:docs`. Edit the commands in octopaas/internal/cmd, not this file. */}\n\n"

func indexPage(root *cobra.Command) string {

	var b strings.Builder
	frontmatter(&b, "octopaas CLI reference", "Every octopaas command, its flags and examples, as `--help` prints them.")
	b.WriteString(banner)
	b.WriteString(prose(root.Long))
	b.WriteString("\n\nFor installing it, signing in and using it in CI/CD, see [the octopaas guide](/platform/octopaas).\n\n")

	b.WriteString("## Commands\n\n| Command | What it does |\n| --- | --- |\n")
	for _, c := range groups(root) {
		fmt.Fprintf(&b, "| [`octopaas %s`](/reference/octopaas/%s) | %s |\n", c.Name(), c.Name(), escape(c.Short))
	}

	b.WriteString("\n## Flags every command takes\n\n")
	flagTable(&b, root.PersistentFlags())
	return b.String()
}

func groupPage(group *cobra.Command) string {

	var b strings.Builder
	frontmatter(&b, "octopaas "+group.Name(), group.Short)
	b.WriteString(banner)
	command(&b, group, 2)
	return b.String()
}

// command documents c and, under it, every subcommand.
func command(b *strings.Builder, c *cobra.Command, depth int) {

	if depth > 2 {
		fmt.Fprintf(b, "%s %s\n\n", strings.Repeat("#", depth), "`"+c.CommandPath()+"`")
	}
	//the Long text is the whole story when there is one; the Short line is the
	//summary of a command that needs no more.
	if c.Long != "" {
		b.WriteString(prose(c.Long) + "\n\n")
	} else {
		b.WriteString(escape(c.Short) + ".\n\n")
	}
	if c.Runnable() {
		fmt.Fprintf(b, "```bash\n%s\n```\n\n", c.UseLine())
	}
	if len(c.Aliases) > 0 {
		fmt.Fprintf(b, "Also: %s.\n\n", inlineAll(c.Aliases))
	}
	if c.Example != "" {
		fmt.Fprintf(b, "**Examples**\n\n```bash\n%s\n```\n\n", strings.TrimRight(dedent(c.Example), "\n"))
	}
	if c.HasAvailableLocalFlags() {
		b.WriteString("**Flags**\n\n")
		flagTable(b, c.LocalNonPersistentFlags())
		b.WriteString("\n")
	}
	for _, sub := range c.Commands() {
		if !sub.IsAvailableCommand() {
			continue
		}
		command(b, sub, depth+1)
	}
}

func flagTable(b *strings.Builder, flags *pflag.FlagSet) {

	var rows []string
	flags.VisitAll(func(f *pflag.Flag) {
		if f.Hidden || f.Name == "help" {
			return
		}
		name := "`--" + f.Name + "`"
		if f.Shorthand != "" {
			name = "`-" + f.Shorthand + "`, " + name
		}
		def := ""
		if f.DefValue != "" && f.DefValue != "false" && f.DefValue != "[]" && f.DefValue != "0" {
			def = "`" + f.DefValue + "`"
		}
		rows = append(rows, fmt.Sprintf("| %s | %s | %s |", name, escape(f.Usage), def))
	})
	sort.Strings(rows)
	b.WriteString("| Flag | What it does | Default |\n| --- | --- | --- |\n")
	b.WriteString(strings.Join(rows, "\n") + "\n")
}

// prose turns a command's Long text into MDX: paragraphs stay paragraphs, and
// indented blocks become code blocks.
func prose(s string) string {

	var out []string
	var code []string
	flush := func() {
		if len(code) > 0 {
			out = append(out, "```\n"+strings.TrimRight(dedent(strings.Join(code, "\n")), "\n")+"\n```")
			code = nil
		}
	}
	for _, para := range strings.Split(strings.TrimSpace(s), "\n\n") {
		if strings.HasPrefix(para, "  ") {
			code = append(code, para)
			continue
		}
		flush()
		out = append(out, escape(strings.Join(strings.Fields(para), " ")))
	}
	flush()
	return strings.Join(out, "\n\n")
}

// escape keeps MDX from reading <angle brackets> and {braces} outside inline
// code as JSX.
func escape(s string) string {

	parts := strings.Split(s, "`")
	for i := 0; i < len(parts); i += 2 {
		r := strings.NewReplacer("<", "&lt;", ">", "&gt;", "{", "&#123;", "}", "&#125;", "|", "\\|")
		parts[i] = r.Replace(parts[i])
	}
	return strings.Join(parts, "`")
}

func dedent(s string) string {

	lines := strings.Split(s, "\n")
	indent := -1
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		n := len(l) - len(strings.TrimLeft(l, " "))
		if indent < 0 || n < indent {
			indent = n
		}
	}
	for i, l := range lines {
		if len(l) >= indent && indent > 0 {
			lines[i] = l[indent:]
		}
	}
	return strings.Join(lines, "\n")
}

func frontmatter(b *strings.Builder, title, description string) {
	fmt.Fprintf(b, "---\ntitle: %q\ndescription: %q\n---\n\n", title, description)
}

func write(dir, name, content string) error {
	return os.WriteFile(filepath.Join(dir, name+".mdx"), []byte(content), 0o644)
}

func quoteAll(names []string) string {

	ret := make([]string, len(names))
	for i, n := range names {
		ret[i] = fmt.Sprintf("%q", n)
	}
	return strings.Join(ret, ", ")
}

func inlineAll(names []string) string {

	ret := make([]string, len(names))
	for i, n := range names {
		ret[i] = "`" + n + "`"
	}
	return strings.Join(ret, ", ")
}
