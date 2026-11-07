package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/pmezard/go-difflib/difflib"
	"github.com/reteps/dockerfmt/lib"
	"github.com/spf13/cobra"
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
		content, c, err := readInput(cmd, opts, name)
		if err != nil {
			fmt.Fprintf(stderr, "dockerfmt: %v\n", err)
			failed = true
			continue
		}
		if c.warning != "" && !warned[c.warning] {
			warned[c.warning] = true
			fmt.Fprintf(stderr, "dockerfmt: warning: %s\n", c.warning)
		}
		displayName := name
		if name == stdinName {
			displayName = "stdin"
		}
		formatted, err := processInput(displayName, content, c.Config, opts, stdout, stderr)
		switch {
		case err != nil:
			fmt.Fprintf(stderr, "dockerfmt: %s: %v\n", displayName, err)
			failed = true
		case !formatted:
			unformatted = true
		}
	}

	if failed || ((opts.check || opts.diff) && unformatted) {
		return 1
	}
	return 0
}

// inputConfig is the formatting configuration for one input.
type inputConfig struct {
	lib.Config
	// warning is a problem with the .editorconfig settings, if any.
	warning string
}

// readInput reads the named input and works out how to format it: flags
// given explicitly come first, then .editorconfig, then the defaults. Without
// a setting for it, the output ends with a newline if the input does.
func readInput(cmd *cobra.Command, opts *options, name string) ([]byte, inputConfig, error) {
	var content []byte
	var err error
	if name == stdinName {
		content, err = io.ReadAll(cmd.InOrStdin())
		if err != nil {
			err = fmt.Errorf("reading stdin: %w", err)
		}
	} else {
		content, err = os.ReadFile(name)
	}
	if err != nil {
		return nil, inputConfig{}, err
	}

	c := inputConfig{Config: opts.config()}
	if !cmd.Flags().Changed("newline") {
		c.TrailingNewline = len(content) > 0 && content[len(content)-1] == '\n'
	}
	if name != stdinName {
		c.Config, err = applyEditorConfig(c.Config, name, cmd.Flags())
		if err != nil {
			c.warning = err.Error()
		}
	}
	return content, c, nil
}

func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
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
		return true, writeFile(name, content, []byte(out))
	default:
		_, err := io.WriteString(stdout, out)
		return true, err
	}
}

// writeFile replaces the contents of the existing file name, which are
// original, with data. The file is rewritten in place, so its permissions,
// owner and hard links are kept. A backup of original is kept next to it
// while writing, and restored if the write fails.
func writeFile(name string, original, data []byte) (err error) {
	info, err := os.Stat(name)
	if err != nil {
		return err
	}
	perm := info.Mode().Perm()
	backup, err := backupFile(name, original, perm)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			if rerr := os.Rename(backup, name); rerr != nil {
				err = fmt.Errorf("%w (the original contents are in %s)", err, backup)
			}
			return
		}
		err = os.Remove(backup)
	}()

	f, err := os.OpenFile(name, os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// backupFile writes data to a new file next to name and returns its path.
func backupFile(name string, data []byte, perm os.FileMode) (path string, err error) {
	f, err := os.CreateTemp(filepath.Dir(name), filepath.Base(name)+".*.dockerfmt-backup")
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(f.Name())
		}
	}()
	if err := f.Chmod(perm); err != nil {
		_ = f.Close()
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return "", err
	}
	return f.Name(), f.Close()
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
