package cmd

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/reteps/dockerfmt/lib"
	"github.com/spf13/cobra"
)

var (
	writeFlag   bool
	checkFlag   bool
	newlineFlag bool
	indentSize  uint
)

var rootCmd = &cobra.Command{
	Use:   "dockerfmt [Dockerfile]",
	Short: "dockerfmt is a Dockerfile and RUN step formatter.",
	Long:  `A updated version of the dockfmt. Uses the dockerfile parser from moby/buildkit and the shell formatter from mvdan/sh.`,
	Run:   Run,
	Args:  cobra.MinimumNArgs(1),
}

func Run(cmd *cobra.Command, args []string) {
	for _, fileName := range args {
		originalLines, err := lib.GetFileLines(fileName)
		if err != nil {
			log.Fatalf("Failed to read file %s: %v", fileName, err)
		}
		formattedLines := lib.FormatFileLines(originalLines, indentSize, newlineFlag)

		if checkFlag {
			// Check if the file is already formatted
			originalContent := strings.Join(originalLines, "")
			if originalContent != formattedLines {
				fmt.Printf("File %s is not formatted\n", fileName)
				os.Exit(1)
			}
		} else if writeFlag {
			// Write the formatted output back to the file
			err := os.WriteFile(fileName, []byte(formattedLines), 0644)
			if err != nil {
				log.Fatalf("Failed to write to file %s: %v", fileName, err)
			}
		} else {
			// Print the formatted output to stdout
			fmt.Printf("%s", formattedLines)
		}
	}
}

func init() {
	rootCmd.Flags().BoolVarP(&writeFlag, "write", "w", false, "Write the formatted output back to the file(s)")
	rootCmd.Flags().BoolVarP(&checkFlag, "check", "c", false, "Check if the file(s) are formatted")
	rootCmd.Flags().BoolVarP(&newlineFlag, "newline", "n", false, "End the file with a trailing newline")
	rootCmd.Flags().UintVarP(&indentSize, "indent", "i", 4, "Number of spaces to use for indentation")
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
