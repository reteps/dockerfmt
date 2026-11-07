package lib

import (
	"strings"
	"testing"

	"github.com/moby/buildkit/frontend/dockerfile/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func crlf(s string) string { return strings.ReplaceAll(s, "\n", "\r\n") }

func formatString(s string) string {
	return FormatFileLines(strings.SplitAfter(s, "\n"), defaultConfig)
}

func TestFormatPreservesCRLF(t *testing.T) {
	tests := []struct {
		name, input, want string
	}{
		{
			name:  "issue 73",
			input: "FROM alpine\n# comment\nARG VERSION=1\nRUN echo hi\n",
			want:  "FROM alpine\n# comment\nARG VERSION=1\nRUN echo hi\n",
		},
		{
			name:  "formatting changes",
			input: "from alpine\nrun   echo a && \\\n  echo b\n\n\n\nENV A b\n",
			want:  "FROM alpine\nRUN echo a \\\n    && echo b\n\nENV A=b\n",
		},
		{
			name:  "heredoc",
			input: "FROM alpine\nRUN <<EOF\necho hi\nEOF\nCOPY <<EOF /x\nhi\nEOF\n",
			want:  "FROM alpine\nRUN <<EOF\necho hi\nEOF\nCOPY <<EOF /x\nhi\nEOF\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, formatString(tt.input), "LF input")
			assert.Equal(t, crlf(tt.want), formatString(crlf(tt.input)), "CRLF input")
		})
	}
}

// BuildKit keeps the "\r" in heredoc bodies, so a CRLF file must come out
// with the same heredoc contents it went in with.
func TestFormatCRLFHeredocUnchangedForBuildKit(t *testing.T) {
	input := crlf("FROM alpine\ncopy <<EOF /x\nhi\nEOF\nrun <<EOF\necho hi\nEOF\n")
	out := formatString(input)

	heredocs := func(s string) []string {
		res, err := parser.Parse(strings.NewReader(s))
		require.NoError(t, err)
		var bodies []string
		for _, n := range res.AST.Children {
			for _, h := range n.Heredocs {
				bodies = append(bodies, h.Content)
			}
		}
		return bodies
	}
	assert.Equal(t, []string{"hi\r\n", "echo hi\r\n"}, heredocs(input))
	assert.Equal(t, heredocs(input), heredocs(out))
}

func TestFormatMixedLineEndings(t *testing.T) {
	// Mostly CRLF: everything becomes CRLF.
	assert.Equal(t, "FROM a\r\nRUN x\r\nRUN y\r\n", formatString("FROM a\r\nRUN x\nRUN y\r\n"))
	// Mostly LF: everything becomes LF.
	assert.Equal(t, "FROM a\nRUN x\nRUN y\n", formatString("FROM a\nRUN x\r\nRUN y\n"))
	// A tie goes to the first line ending.
	assert.Equal(t, "FROM a\r\nRUN x\r\n", formatString("FROM a\r\nRUN x\n"))
	assert.Equal(t, "FROM a\nRUN x\n", formatString("FROM a\nRUN x\r\n"))
}

func TestFormatCRLFTrailingNewline(t *testing.T) {
	c := *defaultConfig
	c.TrailingNewline = false
	assert.Equal(t, "FROM a\r\nRUN x", FormatFileLines([]string{"FROM a\r\n", "RUN x\r\n"}, &c))
}

func TestUsesCRLF(t *testing.T) {
	assert.False(t, usesCRLF(""))
	assert.False(t, usesCRLF("FROM a"))
	assert.False(t, usesCRLF("FROM a\n"))
	assert.True(t, usesCRLF("FROM a\r\n"))
	assert.False(t, usesCRLF("\n"))
	assert.True(t, usesCRLF("\r\n"))
}
