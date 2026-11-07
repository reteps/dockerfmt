package lib

// Semantic equivalence checking. After an instruction is formatted, the output
// is parsed again with BuildKit and compared with BuildKit's reading of the
// original. If they differ, the formatter changed what the instruction does,
// and the caller emits the original text instead. This turns formatter bugs
// into "left unformatted" rather than "broke the build".

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"reflect"
	"slices"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"
	"github.com/moby/buildkit/frontend/dockerfile/shell"
	"mvdan.cc/sh/v3/syntax"
)

// instrContext is the file-level state an instruction is interpreted in.
type instrContext struct {
	// escape is the escape character (from a "# escape=" parser directive).
	escape rune
	// posixShell is false after a SHELL instruction selects a shell that
	// isn't POSIX sh (e.g. PowerShell or cmd), until the next FROM.
	posixShell bool
}

func defaultContext() instrContext {
	return instrContext{escape: '\\', posixShell: true}
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

// isPOSIXShell reports whether the shell executable at path is a POSIX sh
// compatible shell that mvdan/sh can parse.
func isPOSIXShell(exe string) bool {
	exe = strings.ToLower(path.Base(strings.ReplaceAll(exe, `\`, "/")))
	switch strings.TrimSuffix(exe, ".exe") {
	case "sh", "bash", "ash", "dash", "ksh", "mksh", "zsh", "busybox":
		return true
	}
	return false
}

// shellScriptShebang reports whether a heredoc body without a shebang, or with
// one naming a POSIX shell, is a shell script.
func shellScriptShebang(body string) bool {
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
// whole command, so its body is a script run by the default shell. Bodies fed
// to another program (RUN python3 <<EOF, RUN <<EOF python3) are data.
func isShellScriptHeredoc(n *parser.Node, ctx instrContext) bool {
	if !ctx.posixShell || strings.ToUpper(n.Value) != "RUN" || len(n.Heredocs) != 1 || n.Next == nil {
		return false
	}
	cmd := strings.TrimSpace(n.Next.Value)
	if cmd != "<<"+n.Heredocs[0].Name && cmd != "<<-"+n.Heredocs[0].Name &&
		!strings.HasPrefix(cmd, "<<\"") && !strings.HasPrefix(cmd, "<<'") &&
		!strings.HasPrefix(cmd, "<<-\"") && !strings.HasPrefix(cmd, "<<-'") {
		return false
	}
	if len(strings.Fields(cmd)) != 1 {
		return false
	}
	return shellScriptShebang(n.Heredocs[0].Content)
}

type fakeEnv struct{}

// Get resolves every variable to a placeholder, so expansion is visible in the
// result (and "$X" never compares equal to a literal "X").
func (fakeEnv) Get(k string) (string, bool) { return "\x00" + k + "\x00", true }
func (fakeEnv) Keys() []string              { return nil }

func expander(escape rune) instructions.SingleWordExpander {
	lex := shell.NewLex(escape)
	return func(word string) (string, error) {
		r, _, err := lex.ProcessWord(word, fakeEnv{})
		return r, err
	}
}

// canonicalShell returns s in a whitespace-insensitive canonical form, so two
// shell commands that differ only in layout compare equal.
func canonicalShell(s string) string {
	f, err := syntax.NewParser().Parse(strings.NewReader(s), "")
	if err != nil {
		return "unparsed:" + s
	}
	var b bytes.Buffer
	if err := syntax.NewPrinter(syntax.Minify(true)).Print(&b, f); err != nil {
		return "unparsed:" + s
	}
	return strings.TrimSpace(b.String())
}

// semanticKey describes what BuildKit does with instruction n, in a form where
// two instructions compare equal iff they behave the same.
func semanticKey(n *parser.Node, ctx instrContext) (string, error) {
	v, err := instructions.ParseInstruction(n)
	if err != nil {
		return "", err
	}
	exp := expander(ctx.escape)

	switch c := v.(type) {
	case *instructions.Stage:
		return fmt.Sprintf("FROM %q AS %q PLATFORM %q", c.BaseName, c.Name, c.Platform), nil
	case *instructions.MaintainerCommand:
		// dockerfmt rewrites MAINTAINER as this LABEL; see formatMaintainer.
		value := unwrapMaintainerQuotes(c.Maintainer)
		return fmt.Sprintf("LABEL [%q]", "org.opencontainers.image.authors="+value), nil
	case *instructions.LabelCommand:
		if err := c.Expand(exp); err != nil {
			return "", err
		}
		var pairs []string
		for _, kv := range c.Labels {
			pairs = append(pairs, kv.Key+"="+kv.Value)
		}
		return fmt.Sprintf("LABEL %q", pairs), nil
	case *instructions.OnbuildCommand:
		res, err := parser.Parse(strings.NewReader(c.Expression))
		if err != nil {
			return "", err
		}
		if len(res.AST.Children) != 1 {
			return "", fmt.Errorf("ONBUILD expression has %d instructions", len(res.AST.Children))
		}
		inner, err := semanticKey(res.AST.Children[0], ctx)
		return "ONBUILD " + inner, err
	}

	if e, ok := v.(interface {
		Expand(instructions.SingleWordExpander) error
	}); ok {
		if err := e.Expand(exp); err != nil {
			return "", err
		}
	}

	canonCmdLine := func(cl *instructions.ShellDependantCmdLine) {
		if cl.PrependShell && ctx.posixShell {
			cl.CmdLine = []string{canonicalShell(strings.Join(cl.CmdLine, " "))}
		}
	}
	switch c := v.(type) {
	case *instructions.EnvCommand:
		// NoDelim only records the legacy "ENV key value" syntax.
		for i := range c.Env {
			c.Env[i].NoDelim = false
		}
	case *instructions.RunCommand:
		canonCmdLine(&c.ShellDependantCmdLine)
		if isShellScriptHeredoc(n, ctx) {
			c.Files[0].Data = canonicalShell(c.Files[0].Data)
		}
		// Flags are compared as written; their order doesn't matter.
		return fmt.Sprintf("RUN flags=%q %s", sortedCopy(n.Flags), mustJSON(c)), nil
	case *instructions.CmdCommand:
		canonCmdLine(&c.ShellDependantCmdLine)
	case *instructions.EntrypointCommand:
		canonCmdLine(&c.ShellDependantCmdLine)
	case *instructions.HealthCheckCommand:
		if c.Health != nil && len(c.Health.Test) == 2 && c.Health.Test[0] == "CMD-SHELL" && ctx.posixShell {
			c.Health.Test[1] = canonicalShell(c.Health.Test[1])
		}
	}
	return reflect.TypeOf(v).String() + " " + mustJSON(v), nil
}

func sortedCopy(s []string) []string {
	out := slices.Clone(s)
	slices.Sort(out)
	return out
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("unmarshalable(%v)", err)
	}
	return string(b)
}

// equivalent reports whether formatted (the formatter's output for original)
// means the same to BuildKit as original. reason explains a mismatch.
func equivalent(original *parser.Node, formatted string, ctx instrContext) (ok bool, reason string) {
	want, wantErr := semanticKey(original, ctx)

	src := formatted
	if ctx.escape != '\\' {
		src = "# escape=" + string(ctx.escape) + "\n" + src
	}
	res, err := parser.Parse(strings.NewReader(src))
	if err != nil {
		return false, fmt.Sprintf("output does not parse: %v", err)
	}
	if len(res.AST.Children) != 1 {
		return false, fmt.Sprintf("output parses as %d instructions", len(res.AST.Children))
	}
	got, gotErr := semanticKey(res.AST.Children[0], ctx)
	switch {
	case wantErr != nil && gotErr != nil:
		// The original is already invalid (e.g. "ONBUILD ARG" with no name);
		// output that is invalid too can't break a working build.
		return true, ""
	case wantErr != nil:
		return false, fmt.Sprintf("original is invalid (%v) but output is not", wantErr)
	case gotErr != nil:
		return false, fmt.Sprintf("output is invalid: %v", gotErr)
	}
	if got != want {
		return false, "output differs in meaning"
	}
	return true, ""
}
