// WASM entry point for the JS bindings. Built with standard Go (GOOS=js
// GOARCH=wasm), not TinyGo. TinyGo produces smaller binaries but has a
// blocking bug:
//   - reflect.AssignableTo panics with interfaces, breaking encoding/json
//     used by the moby/buildkit parser (https://github.com/tinygo-org/tinygo/issues/4277)

//go:build js && wasm

package main

import (
	"syscall/js"

	"github.com/reteps/dockerfmt/lib"
)

// formatBytes returns the formatted Dockerfile as a string, or a JS Error if
// it can't be formatted (format.ts throws it).
func formatBytes(_ js.Value, args []js.Value) any {
	contents := args[0].String()
	indentSize := uint(args[1].Int())
	newlineFlag := args[2].Bool()
	spaceRedirects := args[3].Bool()

	c := &lib.Config{
		IndentSize:      indentSize,
		TrailingNewline: newlineFlag,
		SpaceRedirects:  spaceRedirects,
	}
	out, err := lib.Format(contents, c)
	if err != nil {
		return js.Global().Get("Error").New(err.Error())
	}
	return out
}

func main() {
	js.Global().Set("__dockerfmt_formatBytes", js.FuncOf(formatBytes))
	// Block forever to keep the Go runtime alive for subsequent calls.
	select {}
}
