package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/pmezard/go-difflib/difflib"
	"github.com/reteps/dockerfmt/lib"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// stdinName is the file name that stands for stdin.
const stdinName = "-"

// run formats each input and returns the exit status. An input that can't be
// read, formatted or written is reported on stderr, and the others are still
// processed.
func run(cmd *cobra.Command, opts *options, args []string) int {
	stdout, stderr := cmd.OutOrStdout(), cmd.ErrOrStderr()
	if len(args) == 0 {
		if isTerminal(cmd.InOrStdin()) {
			// Nothing is piped in, so the user most likely wants help.
			_ = cmd.Help()
			return 0
		}
		args = []string{stdinName}
	}

	failed, unformatted := false, false
	warned := make(map[string]bool)
	for _, name := range args {
		content, c, warnings, err := readInput(cmd, opts, name)
		for _, w := range warnings {
			// Many files share an .editorconfig, so each problem with one
			// is reported once.
			if !warned[w] {
				warned[w] = true
				fmt.Fprintf(stderr, "dockerfmt: warning: %s\n", w)
			}
		}
		if err == nil {
			var formatted bool
			formatted, err = processInput(displayName(name), content, c, opts, stdout, stderr)
			unformatted = unformatted || !formatted
		}
		if err != nil {
			fmt.Fprintf(stderr, "dockerfmt: %v\n", err)
			failed = true
		}
	}

	if failed || ((opts.check || opts.diff) && unformatted) {
		return 1
	}
	return 0
}

func displayName(name string) string {
	if name == stdinName {
		return "stdin"
	}
	return name
}

// readInput reads the named input and works out how to format it: flags
// given explicitly come first, then .editorconfig, then the defaults. Without
// a setting for it, the output ends with a newline if the input does.
func readInput(cmd *cobra.Command, opts *options, name string) (content []byte, c lib.Config, warnings []string, err error) {
	if name == stdinName {
		content, err = io.ReadAll(cmd.InOrStdin())
		if err != nil {
			err = fmt.Errorf("reading stdin: %w", err)
		}
	} else {
		content, err = os.ReadFile(name)
	}
	if err != nil {
		return nil, c, nil, err
	}

	c = opts.config()
	if !cmd.Flags().Changed("newline") {
		c.TrailingNewline = len(content) > 0 && content[len(content)-1] == '\n'
	}
	if name != stdinName {
		c, warnings = applyEditorConfig(c, name, cmd.Flags())
	}
	return content, c, warnings, nil
}

func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// processInput formats one input and prints it, checks it or writes it back.
// It reports whether the input was already formatted, which is only ever false
// when checking. If formatting fails, nothing is printed or written. Errors
// name the input.
func processInput(name string, content []byte, c lib.Config, opts *options, stdout, stderr io.Writer) (formatted bool, err error) {
	c.OnFallback = func(line int, reason string) {
		fmt.Fprintf(stderr, "%s:%d: left unformatted: %s\n", name, line, reason)
	}
	original := string(content)
	out, err := lib.Format(original, &c)
	if err != nil {
		return false, fmt.Errorf("%s: %w", name, err)
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
		return true, writeFile(name, content, []byte(out))
	default:
		_, err := io.WriteString(stdout, out)
		return true, err
	}
}

// writeFile replaces the contents of the existing file name, which are
// original, with data. The file is rewritten in place, through any symbolic
// link, so its permissions, owner and hard links are kept. If writing fails
// after the file was truncated, original is written back; if that fails too,
// it is saved to a temporary file, which the error names.
func writeFile(name string, original, data []byte) error {
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		return f.Close()
	}
	if restoreErr := rewrite(f, original); restoreErr != nil {
		err = fmt.Errorf("%w; restoring the original contents also failed: %w", err, restoreErr)
		if saved, saveErr := saveCopy(name, original); saveErr == nil {
			err = fmt.Errorf("%w; they are saved in %s", err, saved)
		}
	}
	_ = f.Close()
	return err
}

// rewrite replaces the contents of f with data.
func rewrite(f *os.File, data []byte) error {
	if err := f.Truncate(0); err != nil {
		return err
	}
	_, err := f.WriteAt(data, 0)
	return err
}

// saveCopy writes data to a new temporary file named after name, and returns
// its path.
func saveCopy(name string, data []byte) (string, error) {
	f, err := os.CreateTemp("", filepath.Base(name)+".*.orig")
	if err != nil {
		return "", err
	}
	_, err = f.Write(data)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", errors.Join(err, os.Remove(f.Name()))
	}
	return f.Name(), nil
}

// unifiedDiff returns a git-style unified diff from a to b.
func unifiedDiff(name, a, b string) string {
	diff, _ := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        diffLines(a),
		B:        diffLines(b),
		FromFile: "a/" + diffPath(name),
		ToFile:   "b/" + diffPath(name),
		Context:  3,
	})
	return strings.ReplaceAll(diff, noNewline+"\n", "\n\\ No newline at end of file\n")
}

// diffPath returns name as a path for a diff header, which is relative.
func diffPath(name string) string {
	return strings.TrimLeft(filepath.ToSlash(name), "/")
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
