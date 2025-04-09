//go:build js || wasm
// +build js wasm

package main

import (
	"strings"

	"github.com/reteps/dockerfmt/lib"
)

//export formatBytes
func formatBytes(contents []byte, indentSize uint, newlineFlag bool, spaceRedirects bool) *byte {
	originalLines := strings.SplitAfter(string(contents), "\n")
	c := &lib.Config{
		IndentSize:      indentSize,
		TrailingNewline: newlineFlag,
		SpaceRedirects:  spaceRedirects,
	}
	result := lib.FormatFileLines(originalLines, c)
	bytes := []byte(result)
	return &bytes[0]
}

// Required to build
func main() {}
