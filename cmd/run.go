package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/pmezard/go-difflib/difflib"
	"github.com/reteps/dockerfmt/lib"
	"github.com/spf13/cobra"
)

// run formats each input and returns the exit status. An input that can't be
// read, formatted or written is reported on stderr, and the others are still
// processed.
func run(cmd *cobra.Command, opts *options, args []string) int {
	stdout, stderr := cmd.OutOrStdout(), cmd.ErrOrStderr()
	failed, unformatted := false, false
	process := func(name string, content []byte, c lib.Config) {
		formatted, err := processInput(name, content, c, opts, stdout, stderr)
		switch {
		case err != nil:
			fmt.Fprintf(stderr, "dockerfmt: %s: %v\n", name, err)
			failed = true
		case !formatted:
			unformatted = true
		}
	}

	if len(args) == 0 {
		if opts.write {
			fmt.Fprintln(stderr, "dockerfmt: cannot use -w/--write when reading from stdin")
			return 1
		}
		content, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			fmt.Fprintf(stderr, "dockerfmt: reading stdin: %v\n", err)
			return 1
		}
		if len(content) <= 1 {
			_ = cmd.Help()
			return 0
		}
		process("stdin", content, opts.config())
	}
	for _, name := range args {
		content, err := os.ReadFile(name)
		if err != nil {
			fmt.Fprintf(stderr, "dockerfmt: %v\n", err)
			failed = true
			continue
		}
		process(name, content, applyEditorConfig(opts.config(), name, cmd.Flags()))
	}

	if failed || ((opts.check || opts.diff) && unformatted) {
		return 1
	}
	return 0
}

// processInput formats one input and prints it, checks it or writes it back.
// It reports whether the input was already formatted, which is only ever false
// when checking. If formatting fails, nothing is printed or written.
func processInput(name string, content []byte, c lib.Config, opts *options, stdout, stderr io.Writer) (formatted bool, err error) {
	c.OnFallback = func(line int, reason string) {
		fmt.Fprintf(stderr, "%s:%d: left unformatted: %s\n", name, line, reason)
	}
	original := string(content)
	out, err := lib.Format(original, &c)
	if err != nil {
		return false, err
	}

	switch {
	case opts.diff:
		if out == original {
			return true, nil
		}
		_, err := io.WriteString(stdout, unifiedDiff(name, original, out))
		return false, err
	case opts.check:
		if out == original {
			return true, nil
		}
		_, err := fmt.Fprintf(stdout, "%s is not formatted\n", name)
		return false, err
	case opts.write:
		if out == original {
			return true, nil
		}
		if err := os.WriteFile(name, []byte(out), 0o644); err != nil {
			return true, err
		}
		return true, nil
	default:
		_, err := io.WriteString(stdout, out)
		return true, err
	}
}

// unifiedDiff returns a git-style unified diff from a to b.
func unifiedDiff(name, a, b string) string {
	diff, _ := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        diffLines(a),
		B:        diffLines(b),
		FromFile: "a/" + name,
		ToFile:   "b/" + name,
		Context:  3,
	})
	return strings.ReplaceAll(diff, noNewline+"\n", "\n\\ No newline at end of file\n")
}

// noNewline marks a last line without a line ending, so that difflib sees it
// as different from the same line with one.
const noNewline = "\x00"

// diffLines splits s into lines that each end in "\n".
func diffLines(s string) []string {
	if s == "" {
		return nil
	}
	if !strings.HasSuffix(s, "\n") {
		s += noNewline + "\n"
	}
	lines := strings.SplitAfter(s, "\n")
	return lines[:len(lines)-1]
}
