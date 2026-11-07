package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runCLI runs the root command's logic with the given flag values and args
// and returns the exit status and what was written to stdout and stderr.
func runCLI(t *testing.T, stdin string, write, check, diff bool, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	old := []bool{writeFlag, checkFlag, diffFlag, newlineFlag}
	t.Cleanup(func() {
		writeFlag, checkFlag, diffFlag, newlineFlag = old[0], old[1], old[2], old[3]
		rootCmd.SetIn(nil)
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	})
	writeFlag, checkFlag, diffFlag, newlineFlag = write, check, diff, true

	var out, errOut bytes.Buffer
	rootCmd.SetIn(strings.NewReader(stdin))
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&errOut)
	code = run(rootCmd, args)
	return code, out.String(), errOut.String()
}

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	return path
}

// "FROM alpine\nRUN echo hi\n" in UTF-16 LE with a byte order mark, as
// written by `iconv -t UTF-16` or Windows PowerShell's ">" (#81).
const utf16Dockerfile = "\xFF\xFEF\x00R\x00O\x00M\x00 \x00a\x00l\x00p\x00i\x00n\x00e\x00\n\x00" +
	"R\x00U\x00N\x00 \x00e\x00c\x00h\x00o\x00 \x00h\x00i\x00\n\x00"

func TestCLIRejectsUTF16(t *testing.T) {
	for _, mode := range []struct {
		name               string
		write, check, diff bool
	}{
		{"stdout", false, false, false},
		{"write", true, false, false},
		{"check", false, true, false},
		{"diff", false, false, true},
	} {
		t.Run(mode.name, func(t *testing.T) {
			path := writeTemp(t, "Dockerfile", utf16Dockerfile)
			code, stdout, stderr := runCLI(t, "", mode.write, mode.check, mode.diff, path)
			assert.Equal(t, 1, code)
			assert.Empty(t, stdout)
			assert.Equal(t, "dockerfmt: "+path+": file is encoded as UTF-16 (little-endian), not UTF-8; "+
				"re-save it as UTF-8 (Windows PowerShell's \">\" writes UTF-16 by default)\n", stderr)

			got, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, utf16Dockerfile, string(got), "file must be left untouched")
		})
	}
}

func TestCLIRejectsUTF16Stdin(t *testing.T) {
	code, stdout, stderr := runCLI(t, utf16Dockerfile, false, false, false)
	assert.Equal(t, 1, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "dockerfmt: stdin: file is encoded as UTF-16 (little-endian)")
}

func TestCLIParseErrorLeavesFileAlone(t *testing.T) {
	const input = "FROM alpine\nRUN <<EOF\necho hi\n"
	path := writeTemp(t, "Dockerfile", input)
	code, stdout, stderr := runCLI(t, "", true, false, false, path)
	assert.Equal(t, 1, code)
	assert.Empty(t, stdout)
	assert.Equal(t, "dockerfmt: "+path+": line 2: unterminated heredoc\n", stderr)

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, input, string(got))
}

// A bad file is reported, and the other files are still formatted.
func TestCLIContinuesAfterError(t *testing.T) {
	bad := writeTemp(t, "bad.Dockerfile", "FROM alpine\n# caf\xE9\n")
	good := writeTemp(t, "good.Dockerfile", "from alpine\n")
	missing := filepath.Join(t.TempDir(), "missing")
	code, _, stderr := runCLI(t, "", true, false, false, bad, missing, good)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "dockerfmt: "+bad+": line 2: invalid UTF-8")
	assert.Contains(t, stderr, "dockerfmt: open "+missing+": no such file or directory")

	got, err := os.ReadFile(good)
	require.NoError(t, err)
	assert.Equal(t, "FROM alpine\n", string(got))
}

func TestCLIWriteCRLF(t *testing.T) {
	path := writeTemp(t, "Dockerfile", "from alpine\r\n# comment\r\nARG VERSION=1\r\nrun   echo hi\r\n")
	code, _, stderr := runCLI(t, "", true, false, false, path)
	assert.Equal(t, 0, code)
	assert.Empty(t, stderr)
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "FROM alpine\r\n# comment\r\nARG VERSION=1\r\nRUN echo hi\r\n", string(got))

	// Now formatted, so --check passes.
	code, stdout, _ := runCLI(t, "", false, true, false, path)
	assert.Equal(t, 0, code)
	assert.Empty(t, stdout)
}

func TestCLIStdout(t *testing.T) {
	code, stdout, stderr := runCLI(t, "from alpine\r\nrun   echo hi\r\n", false, false, false)
	assert.Equal(t, 0, code)
	assert.Empty(t, stderr)
	assert.Equal(t, "FROM alpine\r\nRUN echo hi\r\n", stdout)
}
