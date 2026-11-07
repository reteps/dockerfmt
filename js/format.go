//go:build js && wasm

// Command format is the WebAssembly module behind the JS bindings. It is
// built with the standard Go toolchain: TinyGo's reflect.AssignableTo panics
// on interfaces, which breaks the encoding/json use in BuildKit's parser
// (https://github.com/tinygo-org/tinygo/issues/4277).
package main

import (
	"syscall/js"

	"github.com/reteps/dockerfmt/lib"
)

// formatBytes is called from format.ts as (contents, indent, trailingNewline,
// spaceRedirects). It returns the formatted Dockerfile, or a JS Error that
// format.ts throws.
func formatBytes(_ js.Value, args []js.Value) any {
	c := &lib.Config{
		IndentSize:      uint(args[1].Int()),
		TrailingNewline: args[2].Bool(),
		SpaceRedirects:  args[3].Bool(),
	}
	out, err := lib.Format(args[0].String(), c)
	if err != nil {
		return js.Global().Get("Error").New(err.Error())
	}
	return out
}

func main() {
	js.Global().Set("__dockerfmt_formatBytes", js.FuncOf(formatBytes))
	select {} // keep the Go runtime alive for later calls
}
