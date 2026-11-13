package lib

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/moby/buildkit/frontend/dockerfile/parser"
	"github.com/moby/buildkit/frontend/dockerfile/shell"
	"mvdan.cc/sh/v3/syntax"
)

// parseFormatted parses formatted, the formatter's output for one
// instruction, in context ctx. It fails unless that is exactly one
// instruction.
func parseFormatted(formatted string, ctx instrContext) (*node, error) {
	src := formatted
	if ctx.escape != '\\' {
		src = "# escape=" + string(ctx.escape) + "\n" + src
	}
	res, err := parser.Parse(strings.NewReader(src))
	if err != nil {
		return nil, fmt.Errorf("output does not parse: %w", err)
	}
	if len(res.AST.Children) != 1 {
		return nil, fmt.Errorf("output parses as %d instructions", len(res.AST.Children))
	}
	n := newNode(res.AST.Children[0], strings.SplitAfter(src, "\n"))
	n.ctx = &ctx
	return n, nil
}

// equivalent reports whether formatted, the formatter's output for original,
// means the same to BuildKit as original. reason explains a mismatch.
func equivalent(original, formatted *parser.Node, ctx instrContext) (ok bool, reason string) {
	want, wantErr := semanticKey(original, ctx)
	got, gotErr := semanticKey(formatted, ctx)
	switch {
	case wantErr != nil && gotErr != nil:
		// An already invalid instruction (e.g. "ONBUILD ARG" with no name) can't
		// break a working build by staying invalid.
		return true, ""
	case wantErr != nil:
		return false, fmt.Sprintf("original is invalid (%v) but output is not", wantErr)
	case gotErr != nil:
		return false, fmt.Sprintf("output is invalid: %v", gotErr)
	case got != want:
		return false, "output differs in meaning"
	}
	return true, ""
}

// sameFile reports whether BuildKit reads the whole files a and b as the same
// instructions with the same meaning. line is the first line in a of an
// instruction that differs.
//
// Format checks each instruction as it formats it, on input with normalized
// line endings. This checks the files as written, which differ from what was
// checked when the input mixes line endings: BuildKit keeps the "\r" of a
// "\r\n" in heredoc bodies and quoted values.
func sameFile(a, b string) (same bool, line int) {
	defer func() {
		if recover() != nil {
			same, line = false, 1
		}
	}()
	ka, la, errA := fileKeys(a)
	kb, _, errB := fileKeys(b)
	if errA != nil || errB != nil {
		return false, 1
	}
	for i := range ka {
		if i >= len(kb) || ka[i] != kb[i] {
			return false, la[i]
		}
	}
	if len(kb) > len(ka) {
		return false, 1
	}
	return true, 0
}

// fileKeys returns the semantic key of each instruction of the file content,
// and the line each starts on.
func fileKeys(content string) (keys []string, lines []int, err error) {
	res, err := parser.Parse(strings.NewReader(content))
	if err != nil {
		return nil, nil, err
	}
	ctx := defaultContext()
	if res.EscapeToken != 0 {
		ctx.escape = res.EscapeToken
	}
	for _, n := range res.AST.Children {
		key, err := semanticKey(n, ctx)
		if err != nil {
			// As in equivalent, an instruction that stays invalid is
			// unchanged. The error can't be compared: its "did you mean"
			// suggestion varies between runs.
			key = "invalid " + strings.ToUpper(n.Value)
		}
		keys = append(keys, key)
		lines = append(lines, n.StartLine)
		ctx.update(n)
	}
	return keys, lines, nil
}

// semanticKey describes what BuildKit does with instruction n, such that two
// instructions get the same key iff they behave the same.
//
// Words are expanded twice, with every variable set and with every variable
// unset, so that forms like ${A:-x} and ${A:+x} differ from what they expand
// to in either case.
func semanticKey(n *parser.Node, ctx instrContext) (string, error) {
	set, err := semanticKeyIn(n, ctx, fakeEnv{})
	if err != nil {
		return "", err
	}
	unset, err := semanticKeyIn(n, ctx, fakeEnv{unset: true})
	if err != nil {
		// ${A:?message} fails when A is unset; that is part of what the
		// instruction does, not a sign it is invalid.
		unset = "error: " + err.Error()
	}
	return set + "\n" + unset, nil
}

func semanticKeyIn(n *parser.Node, ctx instrContext, env fakeEnv) (string, error) {
	v, err := instructions.ParseInstruction(n)
	if err != nil {
		return "", err
	}
	exp := expander(ctx.escape, env)

	switch c := v.(type) {
	case *instructions.Stage:
		return fmt.Sprintf("FROM %q AS %q PLATFORM %q", c.BaseName, c.Name, c.Platform), nil
	case *instructions.MaintainerCommand:
		// formatMaintainer rewrites MAINTAINER as this LABEL, a deliberate
		// change the key accepts. It shares unwrapMaintainerQuotes with the
		// formatter, so it can't catch a bug there; the tests do.
		return labelKey([]string{maintainerLabel + "=" + unwrapMaintainerQuotes(c.Maintainer)}), nil
	case *instructions.LabelCommand:
		if err := c.Expand(exp); err != nil {
			return "", err
		}
		var pairs []string
		for _, kv := range c.Labels {
			pairs = append(pairs, kv.Key+"="+kv.Value)
		}
		return labelKey(pairs), nil
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
		// NoDelim only records whether the legacy "ENV key value" form was used.
		for i := range c.Env {
			c.Env[i].NoDelim = false
		}
	case *instructions.RunCommand:
		canonCmdLine(&c.ShellDependantCmdLine)
		// BuildKit collects these from a map, in no fixed order.
		slices.Sort(c.FlagsUsed)
		if isShellScriptHeredoc(n, ctx) {
			c.Files[0].Data = canonicalShell(c.Files[0].Data)
		}
		// The order of flags can matter (overlapping mounts), so it is kept.
		return fmt.Sprintf("RUN flags=%q %s", n.Flags, mustJSON(c)), nil
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

func labelKey(pairs []string) string {
	return fmt.Sprintf("LABEL %q", pairs)
}

// fakeEnv resolves every variable to a placeholder, so that expansion shows
// in a word's expanded form and "$X" never equals a literal "X". With unset,
// every variable is unset instead.
type fakeEnv struct{ unset bool }

func (e fakeEnv) Get(k string) (string, bool) {
	if e.unset {
		return "", false
	}
	return "\x00" + k + "\x00", true
}
func (fakeEnv) Keys() []string { return nil }

func expander(escape rune, env fakeEnv) instructions.SingleWordExpander {
	lex := shell.NewLex(escape)
	return func(word string) (string, error) {
		r, _, err := lex.ProcessWord(word, env)
		return r, err
	}
}

// canonicalShell returns s in a form that ignores layout, so two commands
// that differ only in whitespace and line breaks compare equal.
func canonicalShell(s string) string {
	// mvdan/sh reads "\r", "\v" and "\f" as blanks, while the shell takes them
	// literally. A script with "\r\n" line endings keeps them when formatted,
	// so only their presence counts; anywhere else, the layout around these
	// characters matters.
	prefix := ""
	if strings.Contains(s, "\r\n") {
		prefix = "crlf:"
		s = strings.ReplaceAll(s, "\r\n", "\n")
	}
	if strings.ContainsAny(s, "\v\f\r`") {
		// mvdan/sh also prints backquotes as $(...) without undoing the
		// escapes inside them, which changes the command.
		return "unparsed:" + prefix + s
	}
	return prefix + printCanonical(s)
}

func printCanonical(s string) string {
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

// mustJSON marshals v, one of BuildKit's instruction types, which hold only
// strings, numbers, booleans and slices of them.
func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("marshaling %T: %v", v, err))
	}
	return string(b)
}
