//go:build js || wasm
// +build js wasm

package main

import (
	"strings"

	"github.com/reteps/dockerfmt/lib"
)

//export formatBytes
func formatBytes(contents []byte, indentSize uint, newlineFlag bool) *byte {
	originalLines := strings.SplitAfter(string(contents), "\n")
	result := lib.FormatFileLines(originalLines, indentSize, newlineFlag)
	bytes := []byte(result)
	return &bytes[0]
}

// Required to build
func main() {}
