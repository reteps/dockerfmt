package cmd

import (
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/editorconfig/editorconfig-core-go/v2"
	"github.com/reteps/dockerfmt/lib"
	"github.com/spf13/cobra"
)

var (
	writeFlag      bool
	checkFlag      bool
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
}

func Run(cmd *cobra.Command, args []string) {
	config := &lib.Config{
		IndentSize:      indentSize,
		TrailingNewline: newlineFlag,
		SpaceRedirects:  spaceRedirects,
	}

	allFormatted := true

	if len(args) == 0 {
		if writeFlag {
			log.Fatal("Error: Cannot use -w/--write flag when reading from stdin")
		}

		inputBytes, err := io.ReadAll(os.Stdin)
		if err != nil {
			log.Fatalf("Failed to read from stdin: %v", err)
		}

		if len(inputBytes) <= 1 {
			cmd.Help()
			os.Exit(0)
		}

		if !processInput("stdin", inputBytes, config) {
			allFormatted = false // Mark as not formatted if check fails
		}

	} else {
		for _, fileName := range args {
			inputBytes, err := os.ReadFile(fileName)
			if err != nil {
				log.Fatalf("Failed to read file %s: %v", fileName, err)
			}

			fileConfig := applyEditorConfig(config, fileName, cmd)
			if !processInput(fileName, inputBytes, fileConfig) {
				allFormatted = false
			}
		}
	}

	// If check mode was enabled and any input was not formatted, exit with status 1
	if checkFlag && !allFormatted {
		os.Exit(1)
	}
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

func processInput(inputName string, inputBytes []byte, config *lib.Config) (formatted bool) {
	originalContent := string(inputBytes)
	lines := strings.SplitAfter(strings.TrimSuffix(originalContent, "\n"), "\n")

	formattedContent := lib.FormatFileLines(lines, config)

	if checkFlag {
		if originalContent != formattedContent {
			fmt.Printf("%s is not formatted\n", inputName)
			return false
		}
		return true
	} else if writeFlag {
		if originalContent != formattedContent {
			err := os.WriteFile(inputName, []byte(formattedContent), 0644)
			if err != nil {
				log.Fatalf("Failed to write to file %s: %v", inputName, err)
			}
		}
	} else {
		_, err := os.Stdout.Write([]byte(formattedContent))
		if err != nil {
			log.Fatalf("Failed to write to stdout: %v", err)
		}
	}
	return true
}

func init() {
	rootCmd.Flags().BoolVarP(&writeFlag, "write", "w", false, "Write the formatted output back to the file(s)")
	rootCmd.Flags().BoolVarP(&checkFlag, "check", "c", false, "Check if the file(s) are formatted")
	rootCmd.Flags().BoolVarP(&newlineFlag, "newline", "n", false, "End the file with a trailing newline")
	rootCmd.Flags().UintVarP(&indentSize, "indent", "i", 4, "Number of spaces to use for indentation")
	rootCmd.Flags().BoolVarP(&spaceRedirects, "space-redirects", "s", false, "Redirect operators will be followed by a space")
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
