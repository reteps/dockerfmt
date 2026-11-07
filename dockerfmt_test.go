package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reteps/dockerfmt/lib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFixtures formats each tests/in file and compares it with its tests/out
// counterpart, as written and with CRLF line endings. Formatting must also be
// idempotent, and no instruction may fall back to its original text: a
// fallback would hide a formatter bug.
func TestFixtures(t *testing.T) {
	inputs, err := filepath.Glob("tests/in/*.dockerfile")
	require.NoError(t, err)
	require.NotEmpty(t, inputs)

	for _, inFile := range inputs {
		t.Run(filepath.Base(inFile), func(t *testing.T) {
			t.Parallel()
			in := readFile(t, inFile)
			want := readFile(t, filepath.Join("tests", "out", filepath.Base(inFile)))
			c := &lib.Config{IndentSize: 4, TrailingNewline: true, OnFallback: func(line int, reason string) {
				t.Errorf("%s:%d: left unformatted: %s", inFile, line, reason)
			}}

			got, err := lib.Format(in, c)
			require.NoError(t, err)
			assert.Equal(t, want, got)

			again, err := lib.Format(got, c)
			require.NoError(t, err)
			assert.Equal(t, got, again, "formatting is not idempotent")

			toCRLF := func(s string) string { return strings.ReplaceAll(s, "\n", "\r\n") }
			gotCRLF, err := lib.Format(toCRLF(in), c)
			require.NoError(t, err)
			assert.Equal(t, toCRLF(want), gotCRLF, "CRLF input")
		})
	}
}

func readFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	require.NoError(t, err)
	return string(b)
}
