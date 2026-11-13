package lib

import (
	"path"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/parser"
	"github.com/moby/buildkit/frontend/dockerfile/shell"
)

// instrContext is the file-level state an instruction is interpreted in.
type instrContext struct {
	// escape is the escape character, set by a "# escape=" parser directive.
	escape rune
	// posixShell is false where shell-form commands run in a shell that
	// isn't POSIX sh compatible (PowerShell, cmd): after a SHELL instruction
	// that selects one, or in a stage based on a Windows image or on a stage
	// that was using one.
	posixShell bool
	// stage is the lower-case name of the current build stage, if it has
	// one.
	stage string
	// stageShells records, for each named stage so far, whether it ended
	// with a POSIX shell. A stage built on another inherits its shell.
	stageShells map[string]bool
	// inStage reports that a FROM has been seen.
	inStage bool
	// globalArgs are the defaults of the ARGs before the first FROM, which
	// FROM can use.
	globalArgs map[string]string
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
		ctx.startStage(n)
	case "ARG":
		if !ctx.inStage {
			ctx.addGlobalArgs(n)
		}
	case "SHELL":
		if n.Next != nil {
			ctx.posixShell = isPOSIXShell(n.Next.Value)
		}
	}
	if ctx.stage != "" {
		ctx.stageShells[ctx.stage] = ctx.posixShell
	}
}

func (ctx *instrContext) startStage(from *parser.Node) {
	ctx.posixShell, ctx.stage, ctx.inStage = true, "", true
	if from.Next == nil {
		return
	}
	base := strings.ToLower(ctx.expandGlobalArgs(from.Next.Value))
	if posix, ok := ctx.stageShells[base]; ok {
		ctx.posixShell = posix
	} else if isWindowsImage(base, from.Flags) {
		// Windows images default to cmd.
		ctx.posixShell = false
	}
	if as := from.Next.Next; as != nil && strings.EqualFold(as.Value, "AS") && as.Next != nil {
		ctx.stage = strings.ToLower(as.Next.Value)
		if ctx.stageShells == nil {
			ctx.stageShells = make(map[string]bool)
		}
	}
}

func (ctx *instrContext) addGlobalArgs(arg *parser.Node) {
	if ctx.globalArgs == nil {
		ctx.globalArgs = make(map[string]string)
	}
	for a := arg.Next; a != nil; a = a.Next {
		name, value, ok := strings.Cut(a.Value, "=")
		if !ok {
			continue // no default
		}
		if v, err := shell.NewLex(ctx.escape).ProcessWordWithMatches(value, globalArgs(ctx.globalArgs)); err == nil {
			value = v.Result
		}
		ctx.globalArgs[name] = value
	}
}

// expandGlobalArgs expands the variables in a FROM's image name with the
// defaults of the global ARGs, as BuildKit does without build arguments. If
// that fails, word is returned as is.
func (ctx *instrContext) expandGlobalArgs(word string) string {
	if !strings.Contains(word, "$") {
		return word
	}
	r, err := shell.NewLex(ctx.escape).ProcessWordWithMatches(word, globalArgs(ctx.globalArgs))
	if err != nil {
		return word
	}
	return r.Result
}

type globalArgs map[string]string

func (g globalArgs) Get(k string) (string, bool) { v, ok := g[k]; return v, ok }
func (g globalArgs) Keys() []string {
	keys := make([]string, 0, len(g))
	for k := range g {
		keys = append(keys, k)
	}
	return keys
}

// isWindowsImage reports whether a FROM with the lower-case image name base
// and flags builds on Windows, as far as can be told without pulling it.
func isWindowsImage(base string, flags []string) bool {
	for _, f := range flags {
		if strings.HasPrefix(strings.ToLower(f), "--platform=windows") {
			return true
		}
	}
	for _, s := range []string{"mcr.microsoft.com/windows", "servercore", "nanoserver", "mcr.microsoft.com/dotnet/framework"} {
		if strings.Contains(base, s) {
			return true
		}
	}
	return false
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
	if !ctx.posixShell || !strings.EqualFold(n.Value, "RUN") || len(n.Heredocs) != 1 || n.Next == nil {
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

// stageRef returns name in lower case if it names a stage defined before this
// point. BuildKit stores stage names in lower case and finds the stage of a
// COPY --from or RUN --mount from= ignoring case. FROM looks the name up as
// written, so "FROM Builder" only builds on stage "builder" once lowered.
// Anything else, such as an image name, is returned as is.
func (ctx instrContext) stageRef(name string) string {
	lower := strings.ToLower(name)
	if _, ok := ctx.stageShells[lower]; ok {
		return lower
	}
	return name
}

// lowerStageRefs lowers the stage name in a --from or --mount flag, as
// written in the source or as parsed. A --mount value with quotes is left
// alone: BuildKit reads it as CSV.
func (ctx instrContext) lowerStageRefs(flag string) string {
	if v, ok := strings.CutPrefix(flag, "--from="); ok {
		return "--from=" + ctx.stageRef(v)
	}
	v, ok := strings.CutPrefix(flag, "--mount=")
	if !ok || strings.ContainsAny(v, `"'`) {
		return flag
	}
	fields := strings.Split(v, ",")
	for i, f := range fields {
		if k, val, ok := strings.Cut(f, "="); ok && strings.EqualFold(k, "from") {
			fields[i] = k + "=" + ctx.stageRef(val)
		}
	}
	return "--mount=" + strings.Join(fields, ",")
}
