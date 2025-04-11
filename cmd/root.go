package cmd

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"

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
	Short: "dockerfmt is a Dockerfile and RUN step formatter.",
	Long:  `A updated version of the dockfmt. Uses the dockerfile parser from moby/buildkit and the shell formatter from mvdan/sh.`,
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

		if !processInput("stdin", inputBytes, config) {
			allFormatted = false // Mark as not formatted if check fails
		}

	} else {
		for _, fileName := range args {
			inputBytes, err := os.ReadFile(fileName)
			if err != nil {
				log.Fatalf("Failed to read file %s: %v", fileName, err)
			}

			if !processInput(fileName, inputBytes, config) {
				allFormatted = false
			}
		}
	}

	// If check mode was enabled and any input was not formatted, exit with status 1
	if checkFlag && !allFormatted {
		os.Exit(1)
	}
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
		err := os.WriteFile(inputName, []byte(formattedContent), 0644)
		if err != nil {
			log.Fatalf("Failed to write to file %s: %v", inputName, err)
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
