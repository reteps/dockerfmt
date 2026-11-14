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

// cli runs dockerfmt with args and returns the exit status and what was
// written to stdout and stderr.
func cli(stdin string, args ...string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = execute(args, strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

// "FROM alpine\nRUN echo hi\n" in UTF-16 LE with a byte order mark, as
// written by `iconv -t UTF-16` or Windows PowerShell's ">" (#81).
const utf16Dockerfile = "\xFF\xFEF\x00R\x00O\x00M\x00 \x00a\x00l\x00p\x00i\x00n\x00e\x00\n\x00" +
	"R\x00U\x00N\x00 \x00e\x00c\x00h\x00o\x00 \x00h\x00i\x00\n\x00"

func TestCLIRejectsUTF16(t *testing.T) {
	for _, mode := range []string{"--newline", "--write", "--check", "--diff"} {
		t.Run(mode, func(t *testing.T) {
			path := writeTemp(t, "Dockerfile", utf16Dockerfile)
			code, stdout, stderr := cli("", mode, path)
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
	code, stdout, stderr := cli(utf16Dockerfile, "-n")
	assert.Equal(t, 1, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "dockerfmt: stdin: file is encoded as UTF-16 (little-endian)")
}

func TestCLIParseErrorLeavesFileAlone(t *testing.T) {
	const input = "FROM alpine\nRUN <<EOF\necho hi\n"
	path := writeTemp(t, "Dockerfile", input)
	code, stdout, stderr := cli("", "-n", "-w", path)
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
	code, _, stderr := cli("", "-n", "-w", bad, missing, good)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr, "dockerfmt: "+bad+": line 2: invalid UTF-8")
	assert.Contains(t, stderr, "dockerfmt: open "+missing+": no such file or directory")

	got, err := os.ReadFile(good)
	require.NoError(t, err)
	assert.Equal(t, "FROM alpine\n", string(got))
}

func TestCLIWriteCRLF(t *testing.T) {
	path := writeTemp(t, "Dockerfile", "from alpine\r\n# comment\r\nARG VERSION=1\r\nrun   echo hi\r\n")
	code, _, stderr := cli("", "-n", "-w", path)
	assert.Equal(t, 0, code)
	assert.Empty(t, stderr)
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "FROM alpine\r\n# comment\r\nARG VERSION=1\r\nRUN echo hi\r\n", string(got))

	// Now formatted, so --check passes.
	code, stdout, _ := cli("", "-n", "-c", path)
	assert.Equal(t, 0, code)
	assert.Empty(t, stdout)
}

func TestCLIStdout(t *testing.T) {
	code, stdout, stderr := cli("from alpine\r\nrun   echo hi\r\n", "-n")
	assert.Equal(t, 0, code)
	assert.Empty(t, stderr)
	assert.Equal(t, "FROM alpine\r\nRUN echo hi\r\n", stdout)
}

func TestCLIRejectsInvalidFlagCombinations(t *testing.T) {
	path := writeTemp(t, "Dockerfile", "from alpine\n")
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"-w", "-c", path}, "-w/--write can't be combined with -c/--check or -d/--diff"},
		{[]string{"-w", "-d", path}, "-w/--write can't be combined with -c/--check or -d/--diff"},
		{[]string{"-w"}, "-w/--write can't be used when reading from stdin"},
		{[]string{"-w", path, "-"}, "-w/--write can't be used when reading from stdin"},
		{[]string{"-i", "0", path}, "-i/--indent must be a number from 1 to 16, got 0"},
		{[]string{"--indent=17", path}, "-i/--indent must be a number from 1 to 16, got 17"},
	} {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			code, stdout, stderr := cli("", tt.args...)
			assert.Equal(t, 1, code)
			assert.Empty(t, stdout)
			assert.Equal(t, "dockerfmt: "+tt.want+"\nRun 'dockerfmt --help' for usage.\n", stderr)
		})
	}
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "from alpine\n", string(got), "nothing is written")
}

func TestCLIDashReadsStdin(t *testing.T) {
	path := writeTemp(t, "Dockerfile", "from a\n")
	code, stdout, stderr := cli("from b\n", path, "-")
	assert.Equal(t, 0, code)
	assert.Empty(t, stderr)
	assert.Equal(t, "FROM a\nFROM b\n", stdout)
}

func TestCLIEmptyStdinIsAnError(t *testing.T) {
	code, stdout, stderr := cli("")
	assert.Equal(t, 1, code)
	assert.Empty(t, stdout)
	assert.Equal(t, "dockerfmt: stdin: file with no instructions\n", stderr)
}

func TestCLITrailingNewline(t *testing.T) {
	for _, tt := range []struct {
		name, input string
		args        []string
		want        string
	}{
		{"kept", "from a\n", nil, "FROM a\n"},
		{"kept absent", "from a", nil, "FROM a"},
		{"added", "from a", []string{"-n"}, "FROM a\n"},
		{"removed", "from a\n", []string{"--newline=false"}, "FROM a"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := cli(tt.input, tt.args...)
			assert.Equal(t, 0, code)
			assert.Empty(t, stderr)
			assert.Equal(t, tt.want, stdout)
		})
	}
}

func TestCLIWriteKeepsModeAndHardLinks(t *testing.T) {
	path := writeTemp(t, "Dockerfile", "from alpine\n")
	require.NoError(t, os.Chmod(path, 0o600))
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Link(path, link))

	code, _, stderr := cli("", "-n", "-w", path)
	assert.Equal(t, 0, code)
	assert.Empty(t, stderr)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	got, err := os.ReadFile(link)
	require.NoError(t, err)
	assert.Equal(t, "FROM alpine\n", string(got), "the hard link sees the new contents")

	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no backup file is left behind")
}

// writeFailure is a file --write can't write, with a hard link to it.
func writeFailure(t *testing.T) (path, link string) {
	t.Helper()
	if os.Getuid() == 0 {
		t.Skip("root can write read-only files")
	}
	path = writeTemp(t, "Dockerfile", "from alpine\n")
	link = filepath.Join(filepath.Dir(path), "link")
	require.NoError(t, os.Link(path, link))
	require.NoError(t, os.Chmod(path, 0o400))
	return path, link
}

func TestCLIWriteFailureLeavesFileAlone(t *testing.T) {
	path, link := writeFailure(t)
	before, err := os.Stat(path)
	require.NoError(t, err)

	code, _, stderr := cli("", "-w", path)
	assert.Equal(t, 1, code)
	assert.Equal(t, "dockerfmt: open "+path+": permission denied\n", stderr)

	after, err := os.Stat(path)
	require.NoError(t, err)
	assert.True(t, os.SameFile(before, after), "the file must not be replaced")
	linked, err := os.Stat(link)
	require.NoError(t, err)
	assert.True(t, os.SameFile(after, linked), "the hard link must be kept")
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "from alpine\n", string(got))
}

func TestCLIWriteFailureKeepsSymlink(t *testing.T) {
	path, _ := writeFailure(t)
	symlink := filepath.Join(t.TempDir(), "Dockerfile")
	require.NoError(t, os.Symlink(path, symlink))

	code, _, _ := cli("", "-w", symlink)
	assert.Equal(t, 1, code)
	info, err := os.Lstat(symlink)
	require.NoError(t, err)
	assert.Equal(t, os.ModeSymlink, info.Mode().Type(), "the symlink must not be replaced")
}

func TestCLIWriteThroughSymlink(t *testing.T) {
	path := writeTemp(t, "Dockerfile", "from alpine\n")
	symlink := filepath.Join(t.TempDir(), "Dockerfile")
	require.NoError(t, os.Symlink(path, symlink))

	code, _, stderr := cli("", "-w", symlink)
	assert.Equal(t, 0, code)
	assert.Empty(t, stderr)
	info, err := os.Lstat(symlink)
	require.NoError(t, err)
	assert.Equal(t, os.ModeSymlink, info.Mode().Type())
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "FROM alpine\n", string(got))
}

func TestCLIWriteInReadOnlyDirectory(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can write read-only directories")
	}
	path := writeTemp(t, "Dockerfile", "from alpine\n")
	dir := filepath.Dir(path)
	require.NoError(t, os.Chmod(dir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	code, _, stderr := cli("", "-w", path)
	assert.Equal(t, 0, code)
	assert.Empty(t, stderr)
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "FROM alpine\n", string(got))
}

func TestIsTerminal(t *testing.T) {
	devNull, err := os.Open(os.DevNull)
	require.NoError(t, err)
	defer devNull.Close()
	assert.False(t, isTerminal(devNull), "/dev/null is a character device, not a terminal")
	assert.False(t, isTerminal(strings.NewReader("")))
}

func TestCLIUseTabs(t *testing.T) {
	code, stdout, stderr := cli("FROM a\nRUN a \\\n  && b\n", "--use-tabs")
	assert.Equal(t, 0, code)
	assert.Empty(t, stderr)
	assert.Equal(t, "FROM a\nRUN a \\\n\t&& b\n", stdout)
}
