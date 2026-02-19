package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reteps/dockerfmt/lib"
	"github.com/stretchr/testify/assert"
)

func TestFormatter(t *testing.T) {
	// assert equality
	assert.Equal(t, 123, 123, "they should be equal")
	matchingFiles, err := filepath.Glob("tests/in/*.dockerfile")
	if err != nil {
		t.Fatalf("Failed to find test files: %v", err)
	}
	c := &lib.Config{
		IndentSize:      4,
		TrailingNewline: true,
		SpaceRedirects:  false,
		MultilineMounts: true,
	}
	for _, fileName := range matchingFiles {
		t.Run(fileName, func(t *testing.T) {
			outFile := strings.Replace(fileName, "in", "out", 1)
			originalLines, err := lib.GetFileLines(fileName)
			if err != nil {
				t.Fatalf("Failed to read file %s: %v", fileName, err)
			}
			fmt.Printf("Comparing file %s with %s\n", fileName, outFile)
			formattedLines := lib.FormatFileLines(originalLines, c)

			// Write outFile to directory
			// err = os.WriteFile(outFile, []byte(formattedLines), 0644)
			// if err != nil {
			// 	t.Fatalf("Failed to write to file %s: %v", outFile, err)
			// }

			// Read outFile
			outLines, err := lib.GetFileLines(outFile)
			if err != nil {
				t.Fatalf("Failed to read file %s: %v", outFile, err)
			}
			// Compare outLines with formattedLines
			assert.Equal(t, strings.Join(outLines, ""), formattedLines, "Files should be equal")
		})
	}
}
