//go:build js && wasm

// Command format is the WebAssembly module behind the JS bindings. It is
// built with the standard Go toolchain: TinyGo's reflect.AssignableTo panics
// on interfaces, which breaks the encoding/json use in BuildKit's parser
// (https://github.com/tinygo-org/tinygo/issues/4277).
package main

import (
	"fmt"
	"syscall/js"

	"github.com/reteps/dockerfmt/lib"
)

// formatBytes is called from format.ts as (contents, options), with options
// already validated there. It returns {output} with the formatted Dockerfile,
// or {error} with a value that format.ts throws.
//
// It must never panic: a panic in a js.FuncOf callback exits the Go program,
// and every later call into the instance would fail.
func formatBytes(_ js.Value, args []js.Value) (result any) {
	defer func() {
		if r := recover(); r != nil {
			result = failure(r)
		}
	}()
	if len(args) != 2 || args[0].Type() != js.TypeString || args[1].Type() != js.TypeObject {
		return map[string]any{"error": newError("TypeError", "formatBytes expects (string, object)")}
	}
	opts := args[1]
	c := &lib.Config{
		IndentSize:      uint(opts.Get("indent").Int()),
		TrailingNewline: opts.Get("trailingNewline").Bool(),
		SpaceRedirects:  opts.Get("spaceRedirects").Bool(),
	}
	if onFallback := opts.Get("onFallback"); onFallback.Type() == js.TypeFunction {
		c.OnFallback = func(line int, reason string) {
			onFallback.Invoke(line, reason)
		}
	}
	out, err := lib.Format(args[0].String(), c)
	if err != nil {
		return map[string]any{"error": newError("Error", err.Error())}
	}
	return map[string]any{"output": out}
}

// failure converts a recovered panic into a formatBytes error result. A
// js.Error is an exception thrown by JS code we called (onFallback), which is
// passed through unchanged.
func failure(r any) map[string]any {
	if jsErr, ok := r.(js.Error); ok {
		return map[string]any{"error": jsErr.Value}
	}
	return map[string]any{"error": newError("Error", fmt.Sprintf("dockerfmt: internal error: %v", r))}
}

func newError(constructor, message string) js.Value {
	return js.Global().Get(constructor).New(message)
}

func main() {
	js.Global().Set("__dockerfmt_formatBytes", js.FuncOf(formatBytes))
	select {} // keep the Go runtime alive for later calls
}
