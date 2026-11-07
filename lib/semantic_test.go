package lib

import (
	"strings"
	"testing"

	"github.com/moby/buildkit/frontend/dockerfile/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parseOne(t *testing.T, src string) *parser.Node {
	t.Helper()
	res, err := parser.Parse(strings.NewReader(src))
	require.NoError(t, err)
	require.Len(t, res.AST.Children, 1)
	return res.AST.Children[0]
}

func TestEquivalent(t *testing.T) {
	tests := []struct {
		name      string
		original  string
		formatted string
		want      bool
	}{
		{"shell whitespace", "RUN echo   a &&   echo b\n", "RUN echo a \\\n    && echo b\n", true},
		{"shell command changed", "RUN echo a\n", "RUN echo b\n", false},
		{"comment inside continuation", "RUN a \\\n  # c\n  && b\n", "RUN a \\\n    && b\n", true},
		{"legacy ENV to key=value", "ENV A one two\n", "ENV A=\"one two\"\n", true},
		{"ENV value whitespace changed", "ENV A=\"x \\\ny\"\n", "ENV A=\"x \\\n    y\"\n", false},
		{"ENV unquoted multi-word", "ENV A one two\n", "ENV A=one two\n", false},
		{"COPY paths split", "COPY [\"a b\", \"c\"]\n", "COPY a b c\n", false},
		{"COPY flag dropped", "COPY --chmod=755 a b\n", "COPY a b\n", false},
		{"JSON to shell form", "CMD [\"a\",]\n", "CMD [\"[\\\"a\\\",]\"]\n", false},
		{"output is two instructions", "RUN a \\\n  b\n", "RUN a\nb\n", false},
		{"MAINTAINER to LABEL", "MAINTAINER Jane <j@x.io>\n", "LABEL org.opencontainers.image.authors=\"Jane <j@x.io>\"\n", true},
		{"MAINTAINER to LABEL with expansion", "MAINTAINER $USER\n", "LABEL org.opencontainers.image.authors=\"$USER\"\n", false},
		{"MAINTAINER to LABEL escaped", "MAINTAINER $USER\n", "LABEL org.opencontainers.image.authors=\"\\$USER\"\n", true},
		{"ONBUILD", "ONBUILD RUN echo   a\n", "ONBUILD RUN echo a\n", true},
		{"invalid stays invalid", "ONBUILD arG\n", "ONBUILD ARG\n", true},
		{"heredoc data changed", "COPY <<EOF /x\n  a\nEOF\n", "COPY <<EOF /x\na\nEOF\n", false},
		{"script heredoc reformatted", "RUN <<EOF\n  echo   a\nEOF\n", "RUN <<EOF\necho a\nEOF\n", true},
		{"data heredoc reformatted", "RUN python3 <<EOF\n  print(1)\nEOF\n", "RUN python3 <<EOF\nprint(1)\nEOF\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, reason := equivalent(parseOne(t, tt.original), tt.formatted, defaultContext())
			assert.Equal(t, tt.want, got, reason)
		})
	}
}

func TestEquivalentNonPOSIXShellIsExact(t *testing.T) {
	ctx := defaultContext()
	ctx.posixShell = false
	// A shell parser would read both as the same command, but PowerShell
	// doesn't treat "\ " as an escaped space.
	got, _ := equivalent(parseOne(t, "RUN dir C:\\ | x\n"), "RUN dir C:\\  | x\n", ctx)
	assert.False(t, got)
}

func TestIsPOSIXShell(t *testing.T) {
	for exe, want := range map[string]bool{
		"/bin/sh": true, "bash": true, "/usr/bin/zsh": true, "ash": true,
		"pwsh": false, "powershell.exe": false, "cmd": false, `C:\Windows\System32\cmd.exe`: false,
	} {
		assert.Equal(t, want, isPOSIXShell(exe), exe)
	}
}

func TestHasShellShebang(t *testing.T) {
	for body, want := range map[string]bool{
		"echo hi\n":                         true,
		"#!/bin/sh\necho hi\n":              true,
		"#!/usr/bin/env bash\necho hi\n":    true,
		"#!/usr/bin/env -S bash -e\necho\n": true,
		"#!/usr/bin/env python3\nprint()\n": false,
		"#!/usr/bin/perl\nprint;\n":         false,
	} {
		assert.Equal(t, want, hasShellShebang(body), body)
	}
}

func TestIsShellScriptHeredoc(t *testing.T) {
	for src, want := range map[string]bool{
		"RUN <<EOF\necho\nEOF\n":            true,
		"RUN <<-EOF\necho\nEOF\n":           true,
		"RUN <<\"EOF\"\necho\nEOF\n":        true,
		"RUN <<EOF python3\nprint()\nEOF\n": false,
		"RUN python3 <<EOF\nprint()\nEOF\n": false,
		"RUN cat <<EOF > f\nx\nEOF\n":       false,
		"RUN <<A <<B cat\na\nA\nb\nB\n":     false,
		"COPY <<EOF /x\necho\nEOF\n":        false,
	} {
		assert.Equal(t, want, isShellScriptHeredoc(parseOne(t, src), defaultContext()), src)
	}
	ctx := defaultContext()
	ctx.posixShell = false
	assert.False(t, isShellScriptHeredoc(parseOne(t, "RUN <<EOF\necho\nEOF\n"), ctx))
}

func TestFallbackLeavesInstructionAsWritten(t *testing.T) {
	// A meaning-changing format must never reach the output. Force one by
	// formatting an instruction whose formatter misbehaves: here the
	// fallback is exercised through a formatter panic.
	orig := formatters["stopsignal"]
	t.Cleanup(func() { formatters["stopsignal"] = orig })
	formatters["stopsignal"] = func(*node, *Config) string { panic("boom") }

	var reasons []string
	c := &Config{IndentSize: 4, TrailingNewline: true, OnFallback: func(_ int, reason string) {
		reasons = append(reasons, reason)
	}}
	out := mustFormat(t, "FROM a\nSTOPSIGNAL   SIGTERM\nrun   echo  hi\n", c)
	assert.Equal(t, "FROM a\nSTOPSIGNAL   SIGTERM\nRUN echo hi\n", out)
	require.Len(t, reasons, 1)
	assert.Contains(t, reasons[0], "boom")
}

func TestUnknownInstructionKeptAsWritten(t *testing.T) {
	for _, src := range []string{
		"FROM a\nFORM b\nRUN echo  hi\n",
		"FROM a\nRUN echo  hi\nFORM   b \\\n   c\n",
	} {
		out := mustFormat(t, src, &Config{IndentSize: 4, TrailingNewline: true})
		assert.Contains(t, out, "FORM", src)
		assert.Contains(t, out, "RUN echo hi", src)
	}
	out := mustFormat(t, "FROM a\nFORM   b \\\n   c\n", &Config{IndentSize: 4, TrailingNewline: true})
	assert.Equal(t, "FROM a\nFORM   b \\\n   c\n", out)
}

func TestCmdJSONKeepsFlags(t *testing.T) {
	out := mustFormat(t, "FROM a\nCMD --doit [ \"a\", \"b\" ]\n", &Config{IndentSize: 4, TrailingNewline: true})
	assert.Equal(t, "FROM a\nCMD --doit [\"a\", \"b\"]\n", out)
}

func TestCopyKeepsEndOfFlags(t *testing.T) {
	c := &Config{IndentSize: 4, TrailingNewline: true, OnFallback: func(line int, reason string) {
		t.Errorf("line %d: unexpected fallback: %s", line, reason)
	}}
	out := mustFormat(t, "FROM a\nCOPY --chown=1  --  --weird /tmp/\nCOPY [\"a\\\"b\", \"c\"]\n", c)
	assert.Equal(t, "FROM a\nCOPY --chown=1 -- --weird /tmp/\nCOPY a\"b c\n", out)
}

func mustFormat(t *testing.T, input string, c *Config) string {
	t.Helper()
	out, err := Format(input, c)
	require.NoError(t, err)
	return out
}
