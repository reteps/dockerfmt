package cmd

import (
	"io"
	"os"
	"testing"

	"github.com/reteps/dockerfmt/lib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureProcessInput runs processInput in --diff mode and returns what it
// printed and whether the input was already formatted.
func captureProcessInput(t *testing.T, name, input string, trailingNewline bool) (string, bool) {
	t.Helper()
	oldCheck, oldDiff, oldStdout := checkFlag, diffFlag, os.Stdout
	t.Cleanup(func() { checkFlag, diffFlag, os.Stdout = oldCheck, oldDiff, oldStdout })
	checkFlag, diffFlag = true, true

	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	formatted := processInput(name, []byte(input), &lib.Config{IndentSize: 4, TrailingNewline: trailingNewline})
	require.NoError(t, w.Close())
	out, err := io.ReadAll(r)
	require.NoError(t, err)
	return string(out), formatted
}

func TestDiffOutput(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		trailingNewline bool
		want            string
		formatted       bool
	}{
		{
			name:            "already formatted prints nothing",
			input:           "FROM alpine\n",
			trailingNewline: true,
			want:            "",
			formatted:       true,
		},
		{
			name:            "changed lines",
			input:           "from alpine\nrun   echo hi\n",
			trailingNewline: true,
			want: "--- a/Dockerfile\n+++ b/Dockerfile\n@@ -1,2 +1,2 @@\n" +
				"-from alpine\n-run   echo hi\n+FROM alpine\n+RUN echo hi\n",
		},
		{
			name:            "missing trailing newline in original",
			input:           "FROM alpine\nRUN echo hi",
			trailingNewline: true,
			want: "--- a/Dockerfile\n+++ b/Dockerfile\n@@ -1,2 +1,2 @@\n" +
				" FROM alpine\n-RUN echo hi\n\\ No newline at end of file\n+RUN echo hi\n",
		},
		{
			name:            "trailing newline removed by formatter",
			input:           "FROM alpine\nRUN echo hi\n",
			trailingNewline: false,
			want: "--- a/Dockerfile\n+++ b/Dockerfile\n@@ -1,2 +1,2 @@\n" +
				" FROM alpine\n-RUN echo hi\n+RUN echo hi\n\\ No newline at end of file\n",
		},
		{
			name:            "already formatted CRLF prints nothing",
			input:           "FROM alpine\r\n# comment\r\nARG VERSION=1\r\nRUN echo hi\r\n",
			trailingNewline: true,
			want:            "",
			formatted:       true,
		},
		{
			name:            "CRLF changed lines keep CRLF",
			input:           "from alpine\r\nrun   echo hi\r\n",
			trailingNewline: true,
			want: "--- a/Dockerfile\n+++ b/Dockerfile\n@@ -1,2 +1,2 @@\n" +
				"-from alpine\r\n-run   echo hi\r\n+FROM alpine\r\n+RUN echo hi\r\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, formatted := captureProcessInput(t, "Dockerfile", tt.input, tt.trailingNewline)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.formatted, formatted)
		})
	}
}

func TestSplitLines(t *testing.T) {
	assert.Nil(t, splitLines(""))
	assert.Equal(t, []string{"a\n", "b\n"}, splitLines("a\nb\n"))
	assert.Equal(t, []string{"a\n", "b" + noNewline + "\n"}, splitLines("a\nb"))
}
