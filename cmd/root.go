// Package cmd implements the dockerfmt command line.
package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"slices"

	"github.com/reteps/dockerfmt/lib"
	"github.com/spf13/cobra"
)

// Version is set at build time:
//
//	go build -ldflags "-X github.com/reteps/dockerfmt/cmd.Version=v0.6.1"
//
// Builds made with "go install" take it from the module version instead.
var Version = "dev"

func version() string {
	if Version != "dev" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return Version
}

// Execute runs dockerfmt with the process's arguments and exits with its
// status.
func Execute() {
	os.Exit(execute(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// options holds the command-line flags.
type options struct {
	write, check, diff bool
	newline            bool
	indent             uint
	spaceRedirects     bool
}

const maxIndent = 16

func (o *options) config() lib.Config {
	return lib.Config{IndentSize: o.indent, TrailingNewline: o.newline, SpaceRedirects: o.spaceRedirects}
}

func (o *options) validate(args []string) error {
	if o.write && (o.check || o.diff) {
		return errors.New("-w/--write can't be combined with -c/--check or -d/--diff")
	}
	if o.write && (len(args) == 0 || slices.Contains(args, "-")) {
		return errors.New("-w/--write can't be used when reading from stdin")
	}
	if o.indent < 1 || o.indent > maxIndent {
		return fmt.Errorf("-i/--indent must be between 1 and %d, got %d", maxIndent, o.indent)
	}
	return nil
}

// execute runs dockerfmt and returns the exit status: 1 if an input couldn't
// be read, formatted or written, or (with --check or --diff) isn't formatted.
func execute(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	status := 0
	root := newRootCmd(&status)
	root.SetArgs(args)
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)
	if err := root.Execute(); err != nil {
		fmt.Fprintf(stderr, "dockerfmt: %v\nRun 'dockerfmt --help' for usage.\n", err)
		return 1
	}
	return status
}

func newRootCmd(status *int) *cobra.Command {
	var opts options
	root := &cobra.Command{
		Use:   "dockerfmt [Dockerfile...]",
		Short: "Format Dockerfiles and shell commands within RUN steps",
		Long: "Format Dockerfiles and shell commands within RUN steps. If no files are given, or a file is \"-\", " +
			"input is read from stdin.\n\n" +
			"The output ends with a newline if the input does, unless --newline or .editorconfig says otherwise.",
		Args: cobra.ArbitraryArgs,
		PreRunE: func(_ *cobra.Command, args []string) error {
			return opts.validate(args)
		},
		Run: func(cmd *cobra.Command, args []string) {
			*status = run(cmd, &opts, args)
		},
		// Positional arguments are Dockerfiles. Without this, cobra offers only
		// subcommand names, because the root command has subcommands.
		ValidArgsFunction: func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
			return nil, cobra.ShellCompDirectiveDefault
		},
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	flags := root.Flags()
	flags.BoolVarP(&opts.write, "write", "w", false, "Write the formatted output back to the file(s)")
	flags.BoolVarP(&opts.check, "check", "c", false, "Check if the file(s) are formatted")
	flags.BoolVarP(&opts.diff, "diff", "d", false, "Print a unified diff of the changes needed (implies --check)")
	flags.BoolVarP(&opts.newline, "newline", "n", false, "End the file with a trailing newline (--newline=false: without one)")
	flags.UintVarP(&opts.indent, "indent", "i", 4, "Number of spaces to use for indentation")
	flags.BoolVarP(&opts.spaceRedirects, "space-redirects", "s", false, "Redirect operators will be followed by a space")
	_ = root.RegisterFlagCompletionFunc("indent", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return []string{"2", "4", "8"}, cobra.ShellCompDirectiveNoFileComp
	})

	root.AddCommand(&cobra.Command{
		Use:               "version",
		Short:             "Print the version number of dockerfmt",
		Args:              cobra.NoArgs,
		ValidArgsFunction: cobra.NoFileCompletions,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintln(cmd.OutOrStdout(), "dockerfmt "+version())
		},
	})
	return root
}
