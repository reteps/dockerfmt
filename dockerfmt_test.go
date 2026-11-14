package main

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/reteps/dockerfmt/lib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/txtar"
)

// TestGolden runs the golden tests in tests/golden, one archive per area.
// An archive's comment describes it, and may set options for all its tests,
// one "option: value" per line (indent, space-redirects, trailing-newline,
// use-tabs).
// Its files come in pairs: an input, then "want", what it formats to. Most
// areas combine their small cases in one input, each under a comment naming
// it.
func TestGolden(t *testing.T) {
	archives, err := filepath.Glob("tests/golden/*.txtar")
	require.NoError(t, err)
	require.NotEmpty(t, archives)

	for _, name := range archives {
		a, err := txtar.ParseFile(name)
		require.NoError(t, err)
		c, err := goldenConfig(string(a.Comment))
		require.NoError(t, err, name)
		require.Zero(t, len(a.Files)%2, "%s: files must come in input/want pairs", name)

		for i := 0; i < len(a.Files); i += 2 {
			in, want := a.Files[i], a.Files[i+1]
			require.Equal(t, "want", want.Name, "%s: %q must be followed by \"want\"", name, in.Name)
			t.Run(strings.TrimSuffix(filepath.Base(name), ".txtar")+"/"+in.Name, func(t *testing.T) {
				t.Parallel()
				out := string(want.Data)
				if !c.TrailingNewline {
					out = strings.TrimSuffix(out, "\n")
				}
				checkFormat(t, string(in.Data), out, c)
			})
		}
	}
}

// goldenConfig reads the options in an archive's comment. Other lines are
// its description.
func goldenConfig(comment string) (lib.Config, error) {
	c := lib.Config{IndentSize: 4, TrailingNewline: true, SpaceRedirects: true}
	for _, line := range strings.Split(comment, "\n") {
		key, value, _ := strings.Cut(line, ":")
		value = strings.TrimSpace(value)
		var err error
		switch key {
		case "indent":
			var n uint64
			n, err = strconv.ParseUint(value, 10, 0)
			c.IndentSize = uint(n)
		case "space-redirects":
			c.SpaceRedirects, err = strconv.ParseBool(value)
		case "trailing-newline":
			c.TrailingNewline, err = strconv.ParseBool(value)
		case "use-tabs":
			c.UseTabs, err = strconv.ParseBool(value)
		}
		if err != nil {
			return c, err
		}
	}
	return c, nil
}

// checkFormat checks that in formats to want, as written, without its final
// newline and with CRLF line endings; that formatting is idempotent; and that
// no instruction falls back to its original text: a fallback would hide a
// formatter bug.
func checkFormat(t *testing.T, in, want string, c lib.Config) {
	t.Helper()
	c.OnFallback = func(line int, reason string) {
		t.Errorf("line %d: left unformatted: %s", line, reason)
	}
	format := func(s string) string {
		t.Helper()
		out, err := lib.Format(s, &c)
		require.NoError(t, err)
		return out
	}

	got := format(in)
	assert.Equal(t, want, got)
	assert.Equal(t, got, format(got), "formatting is not idempotent")
	if trimmed, ok := strings.CutSuffix(in, "\n"); ok && c.TrailingNewline {
		assert.Equal(t, want, format(trimmed), "input without its final newline")
	}
	toCRLF := func(s string) string { return strings.ReplaceAll(s, "\n", "\r\n") }
	assert.Equal(t, toCRLF(want), format(toCRLF(in)), "CRLF input")
}
