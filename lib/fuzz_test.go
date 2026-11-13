package lib

import (
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/txtar"
)

// FuzzFormat checks properties that hold for every input Format accepts:
// formatting is idempotent, and the output has the same instructions as the
// input, each meaning the same to BuildKit. The per-instruction guard in
// Format enforces the latter too; this also catches instructions merging,
// splitting or disappearing at the file level.
func FuzzFormat(f *testing.F) {
	archives, _ := filepath.Glob(filepath.Join("..", "tests", "golden", "*.txtar"))
	for _, name := range archives {
		a, err := txtar.ParseFile(name)
		if err != nil {
			f.Fatal(err)
		}
		for _, file := range a.Files {
			f.Add(string(file.Data), uint(4), false)
		}
	}
	f.Add("FROM a\nRUN echo a \\\n  # c\n  && echo b\n", uint(2), true)
	f.Add("FROM a\r\nENV A=\"x \\\r\ny\"\r\n", uint(4), false)

	f.Fuzz(func(t *testing.T, input string, indent uint, spaceRedirects bool) {
		c := &Config{IndentSize: indent % (MaxIndentSize + 1), TrailingNewline: true, SpaceRedirects: spaceRedirects}
		out, err := Format(input, c)
		if err != nil {
			return
		}
		again, err := Format(out, c)
		if err != nil {
			t.Fatalf("output doesn't parse: %v\ninput:\n%q\noutput:\n%q", err, input, out)
		}
		if again != out {
			t.Fatalf("not idempotent\ninput:\n%q\nfirst:\n%q\nsecond:\n%q", input, out, again)
		}
		assertSameInstructions(t, input, out)
	})
}

func assertSameInstructions(t *testing.T, input, output string) {
	t.Helper()
	if line, reason := sameFile(strings.TrimPrefix(input, utf8BOM), strings.TrimPrefix(output, utf8BOM)); reason != "" {
		t.Fatalf("line %d: %s\ninput:\n%q\noutput:\n%q", line, reason, input, output)
	}
}
