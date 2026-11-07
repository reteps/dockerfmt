package lib

import (
	"path"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/parser"
)

// instrContext is the file-level state an instruction is interpreted in.
type instrContext struct {
	// escape is the escape character, set by a "# escape=" parser directive.
	escape rune
	// posixShell is false after a SHELL instruction selects a shell that isn't
	// POSIX sh compatible (PowerShell, cmd), until the next FROM.
	posixShell bool
}

func defaultContext() instrContext {
	return instrContext{escape: '\\', posixShell: true}
}

// formatsShell reports whether shell-form bodies can be formatted as POSIX
// shell with "\" continuations.
func (ctx instrContext) formatsShell() bool {
	return ctx.posixShell && ctx.escape == '\\'
}

// update advances the context past instruction n.
func (ctx *instrContext) update(n *parser.Node) {
	switch strings.ToUpper(n.Value) {
	case "FROM":
		ctx.posixShell = true
	case "SHELL":
		if n.Next != nil {
			ctx.posixShell = isPOSIXShell(n.Next.Value)
		}
	}
}

// isPOSIXShell reports whether exe names a POSIX sh compatible shell.
func isPOSIXShell(exe string) bool {
	exe = strings.ToLower(path.Base(strings.ReplaceAll(exe, `\`, "/")))
	switch strings.TrimSuffix(exe, ".exe") {
	case "sh", "bash", "ash", "dash", "ksh", "mksh", "zsh", "busybox":
		return true
	}
	return false
}

// hasShellShebang reports whether a script body either has no shebang or has
// one naming a POSIX shell.
func hasShellShebang(body string) bool {
	first, _, _ := strings.Cut(body, "\n")
	interp, ok := strings.CutPrefix(strings.TrimSpace(first), "#!")
	if !ok {
		return true
	}
	fields := strings.Fields(interp)
	if len(fields) == 0 {
		return false
	}
	exe := fields[0]
	if path.Base(exe) == "env" {
		// #!/usr/bin/env [-S] bash
		for _, f := range fields[1:] {
			if !strings.HasPrefix(f, "-") {
				exe = f
				break
			}
		}
	}
	return isPOSIXShell(exe)
}

// isShellScriptHeredoc reports whether n is "RUN <<EOF" with the heredoc as the
// whole command, so the shell runs its body as a script. A heredoc fed to
// another program ("RUN python3 <<EOF", "RUN <<EOF python3") is data.
func isShellScriptHeredoc(n *parser.Node, ctx instrContext) bool {
	if !ctx.posixShell || strings.ToUpper(n.Value) != "RUN" || len(n.Heredocs) != 1 || n.Next == nil {
		return false
	}
	cmd := strings.TrimSpace(n.Next.Value)
	if strings.ContainsAny(cmd, " \t") {
		return false
	}
	name := n.Heredocs[0].Name
	switch strings.TrimPrefix(strings.TrimPrefix(cmd, "<<"), "-") {
	case name, `"` + name + `"`, "'" + name + "'":
	default:
		return false
	}
	return strings.HasPrefix(cmd, "<<") && hasShellShebang(n.Heredocs[0].Content)
}
