// WASM entry point for the JS bindings. Built with standard Go (GOOS=js
// GOARCH=wasm), not TinyGo. TinyGo produces smaller binaries but has a
// blocking bug:
//   - reflect.AssignableTo panics with interfaces, breaking encoding/json
//     used by the moby/buildkit parser (https://github.com/tinygo-org/tinygo/issues/4277)

//go:build js && wasm

package main

import (
	"strings"
	"syscall/js"

	"github.com/reteps/dockerfmt/lib"
)

func formatBytes(_ js.Value, args []js.Value) any {
	contents := args[0].String()
	indentSize := uint(args[1].Int())
	newlineFlag := args[2].Bool()
	spaceRedirects := args[3].Bool()

	originalLines := strings.SplitAfter(contents, "\n")
	c := &lib.Config{
		IndentSize:      indentSize,
		TrailingNewline: newlineFlag,
		SpaceRedirects:  spaceRedirects,
	}
	return lib.FormatFileLines(originalLines, c)
}

func main() {
	js.Global().Set("__dockerfmt_formatBytes", js.FuncOf(formatBytes))
	// Block forever to keep the Go runtime alive for subsequent calls.
	select {}
}
