package cmd

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/editorconfig/editorconfig-core-go/v2"
	"github.com/pmezard/go-difflib/difflib"
	"github.com/reteps/dockerfmt/lib"
	"github.com/spf13/cobra"
)

var (
	writeFlag      bool
	checkFlag      bool
	diffFlag       bool
	newlineFlag    bool
	indentSize     uint
	spaceRedirects bool
)

var rootCmd = &cobra.Command{
	Use:   "dockerfmt [Dockerfile...]",
	Short: "Format Dockerfiles and shell commands within RUN steps",
	Long:  `Format Dockerfiles and shell commands within RUN steps. If no files are specified, input is read from stdin.`,
	Run:   Run,
	Args:  cobra.ArbitraryArgs,

	ValidArgsFunction: completeArgs,
}

func Run(cmd *cobra.Command, args []string) {
	if code := run(cmd, args); code != 0 {
		os.Exit(code)
	}
}

// run does the work of Run and returns the exit status: 1 if any input
// couldn't be read or formatted, or (with --check) isn't formatted. An input
// that can't be formatted is reported on stderr and left alone; the remaining
// inputs are still processed.
func run(cmd *cobra.Command, args []string) int {
	stdout, stderr := cmd.OutOrStdout(), cmd.ErrOrStderr()
	config := &lib.Config{
		IndentSize:      indentSize,
		TrailingNewline: newlineFlag,
		SpaceRedirects:  spaceRedirects,
	}

	allFormatted, failed := true, false
	if diffFlag {
		checkFlag = true
	}
	process := func(name string, inputBytes []byte, config *lib.Config) {
		formatted, err := processInput(name, inputBytes, config, stdout, stderr)
		if err != nil {
			fmt.Fprintf(stderr, "dockerfmt: %s: %v\n", name, err)
			failed = true
		} else if !formatted {
			allFormatted = false
		}
	}

	if len(args) == 0 {
		if writeFlag {
			fmt.Fprintln(stderr, "dockerfmt: cannot use -w/--write when reading from stdin")
			return 1
		}

		inputBytes, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			fmt.Fprintf(stderr, "dockerfmt: failed to read from stdin: %v\n", err)
			return 1
		}

		if len(inputBytes) <= 1 {
			cmd.Help()
			return 0
		}

		process("stdin", inputBytes, config)
	} else {
		for _, fileName := range args {
			inputBytes, err := os.ReadFile(fileName)
			if err != nil {
				fmt.Fprintf(stderr, "dockerfmt: %v\n", err)
				failed = true
				continue
			}
			process(fileName, inputBytes, applyEditorConfig(config, fileName, cmd))
		}
	}

	if failed || (checkFlag && !allFormatted) {
		return 1
	}
	return 0
}

// applyEditorConfig returns a Config with editorconfig properties applied for
// the given file path. Explicitly-set CLI flags take precedence.
func applyEditorConfig(base *lib.Config, filePath string, cmd *cobra.Command) *lib.Config {
	def, err := editorconfig.GetDefinitionForFilename(filePath)
	if err != nil {
		return base
	}

	// Start from a copy of the base config (which has CLI defaults/flags).
	c := *base

	// indent_size — only apply if the CLI flag was not explicitly set.
	if !cmd.Flags().Changed("indent") && def.IndentSize != "" {
		if n, err := strconv.Atoi(def.IndentSize); err == nil && n > 0 {
			c.IndentSize = uint(n)
		}
	}

	// insert_final_newline — only apply if the CLI flag was not explicitly set.
	if !cmd.Flags().Changed("newline") && def.InsertFinalNewline != nil {
		c.TrailingNewline = *def.InsertFinalNewline
	}

	// space_redirects — dockerfmt-specific property, not a standard editorconfig key.
	if !cmd.Flags().Changed("space-redirects") {
		if v, ok := def.Raw["space_redirects"]; ok {
			if b, err := strconv.ParseBool(v); err == nil {
				c.SpaceRedirects = b
			}
		}
	}

	return &c
}

// processInput formats one input and then prints it, checks it, or writes it
// back (--write). It reports whether the input was already formatted, which
// is only ever false when checking. If the input can't be formatted, it
// returns the error without printing or writing anything.
func processInput(inputName string, inputBytes []byte, config *lib.Config, stdout, stderr io.Writer) (formatted bool, err error) {
	c := *config
	c.OnFallback = func(line int, reason string) {
		fmt.Fprintf(stderr, "%s:%d: left unformatted: %s\n", inputName, line, reason)
	}
	originalContent := string(inputBytes)

	formattedContent, err := lib.Format(originalContent, &c)
	if err != nil {
		return false, err
	}

	if checkFlag {
		if originalContent != formattedContent {
			diff := ""
			if diffFlag {
				diff, _ = difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
					A:        splitLines(originalContent),
					B:        splitLines(formattedContent),
					FromFile: "a/" + inputName,
					ToFile:   "b/" + inputName,
					Context:  3,
				})
			}
			if diff != "" {
				fmt.Fprint(stdout, strings.ReplaceAll(diff, noNewline+"\n", "\n\\ No newline at end of file\n"))
			} else {
				fmt.Fprintf(stdout, "%s is not formatted\n", inputName)
			}
			return false, nil
		}
		return true, nil
	} else if writeFlag {
		if originalContent != formattedContent {
			if err := os.WriteFile(inputName, []byte(formattedContent), 0644); err != nil {
				return true, fmt.Errorf("failed to write file: %w", err)
			}
		}
	} else {
		if _, err := io.WriteString(stdout, formattedContent); err != nil {
			return true, fmt.Errorf("failed to write to stdout: %w", err)
		}
	}
	return true, nil
}

// noNewline marks a final line lacking "\n" so difflib sees it as different from the same line with one.
const noNewline = "\x00"

// splitLines splits s into newline-terminated lines (difflib.SplitLines adds a spurious trailing blank line).
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	if !strings.HasSuffix(s, "\n") {
		s += noNewline + "\n"
	}
	lines := strings.SplitAfter(s, "\n")
	return lines[:len(lines)-1]
}

func init() {
	rootCmd.Flags().BoolVarP(&writeFlag, "write", "w", false, "Write the formatted output back to the file(s)")
	rootCmd.Flags().BoolVarP(&checkFlag, "check", "c", false, "Check if the file(s) are formatted")
	rootCmd.Flags().BoolVarP(&diffFlag, "diff", "d", false, "Print a unified diff of the changes needed (implies --check)")
	rootCmd.Flags().BoolVarP(&newlineFlag, "newline", "n", false, "End the file with a trailing newline")
	rootCmd.Flags().UintVarP(&indentSize, "indent", "i", 4, "Number of spaces to use for indentation")
	rootCmd.Flags().BoolVarP(&spaceRedirects, "space-redirects", "s", false, "Redirect operators will be followed by a space")

	// --indent takes a number, so suggest common widths instead of falling
	// back to filename completion.
	_ = rootCmd.RegisterFlagCompletionFunc("indent", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"2", "4", "8"}, cobra.ShellCompDirectiveNoFileComp
	})
}

// completeArgs completes positional arguments (Dockerfiles) as file paths.
// Without it, cobra only offers the subcommand names and disables file
// completion, because the root command has subcommands.
func completeArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return nil, cobra.ShellCompDirectiveDefault
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
