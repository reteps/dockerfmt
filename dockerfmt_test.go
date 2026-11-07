package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reteps/dockerfmt/lib"
	"github.com/stretchr/testify/assert"
)

// expectedFallbacks lists fixtures with instructions the formatter can't yet
// format without changing their meaning, so they are left as written.
var expectedFallbacks = map[string]bool{
	"label.dockerfile": true, // quoted multi-line ENV value, #78
}

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
	}
	for _, fileName := range matchingFiles {
		t.Run(fileName, func(t *testing.T) {
			// Every fixture must format without the semantic guard falling
			// back, or a formatter bug would hide behind the fallback.
			c := *c
			c.OnFallback = func(line int, reason string) {
				if !expectedFallbacks[filepath.Base(fileName)] {
					t.Errorf("%s:%d: left unformatted: %s", fileName, line, reason)
				}
			}
			outFile := strings.Replace(fileName, "in", "out", 1)
			originalLines, err := lib.GetFileLines(fileName)
			if err != nil {
				t.Fatalf("Failed to read file %s: %v", fileName, err)
			}
			fmt.Printf("Comparing file %s with %s\n", fileName, outFile)
			formattedLines := lib.FormatFileLines(originalLines, &c)

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
