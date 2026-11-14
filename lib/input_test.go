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
	out, err := Format(s, defaultConfig)
	if err != nil {
		panic(err)
	}
	return out
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
	assert.Equal(t, "FROM a\r\nRUN x", mustFormat(t, "FROM a\r\nRUN x\r\n", &c))
}

func TestUsesCRLF(t *testing.T) {
	assert.False(t, usesCRLF(""))
	assert.False(t, usesCRLF("FROM a"))
	assert.False(t, usesCRLF("FROM a\n"))
	assert.True(t, usesCRLF("FROM a\r\n"))
	assert.False(t, usesCRLF("\n"))
	assert.True(t, usesCRLF("\r\n"))
}

func TestFormat(t *testing.T) {
	out, err := Format("from alpine\r\nrun   echo hi\r\n", defaultConfig)
	require.NoError(t, err)
	assert.Equal(t, "FROM alpine\r\nRUN echo hi\r\n", out)

	out, err = Format("from alpine\nrun   echo hi", defaultConfig)
	require.NoError(t, err)
	assert.Equal(t, "FROM alpine\nRUN echo hi\n", out)
}

func TestFormatUTF8BOM(t *testing.T) {
	const bom = "\xEF\xBB\xBF"
	for _, tt := range []struct{ input, want string }{
		{bom + "from alpine\nrun   echo hi\n", bom + "FROM alpine\nRUN echo hi\n"},
		{bom + "# syntax=docker/dockerfile:1\nfrom alpine\n", bom + "# syntax=docker/dockerfile:1\nFROM alpine\n"},
		{bom + "from alpine\r\nrun   echo hi\r\n", bom + "FROM alpine\r\nRUN echo hi\r\n"},
	} {
		out, err := Format(tt.input, defaultConfig)
		require.NoError(t, err)
		assert.Equal(t, tt.want, out)
	}
}

const utf16Source = "FROM alpine\nRUN echo hi\n"

// utf16 returns utf16Source encoded as UTF-16.
func utf16(bigEndian, withBOM bool) string {
	var b strings.Builder
	put := func(r rune) {
		if bigEndian {
			b.WriteByte(byte(r >> 8))
			b.WriteByte(byte(r))
		} else {
			b.WriteByte(byte(r))
			b.WriteByte(byte(r >> 8))
		}
	}
	if withBOM {
		put(0xFEFF)
	}
	for _, r := range utf16Source {
		put(r)
	}
	return b.String()
}

func TestFormatRejectsNonUTF8(t *testing.T) {
	tests := []struct {
		name, input, wantErr string
	}{
		{"UTF-16 LE", utf16(false, true), "file is encoded as UTF-16 (little-endian), not UTF-8"},
		{"UTF-16 BE", utf16(true, true), "file is encoded as UTF-16 (big-endian), not UTF-8"},
		{"UTF-16 LE without BOM", utf16(false, false), "line 1: NUL byte found"},
		{"UTF-32 LE", "\xFF\xFE\x00\x00F\x00\x00\x00", "file is encoded as UTF-32 (little-endian), not UTF-8"},
		{"UTF-32 BE", "\x00\x00\xFE\xFF\x00\x00\x00F", "file is encoded as UTF-32 (big-endian), not UTF-8"},
		{"NUL byte", "FROM alpine\nRUN echo \x00\n", "line 2: NUL byte found"},
		{"Latin-1", "FROM alpine\n# caf\xE9\n", "line 2: invalid UTF-8"},
		{"truncated sequence", "FROM alpine\nRUN echo \xE2\x82\n", "line 2: invalid UTF-8"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := Format(tt.input, defaultConfig)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			assert.Empty(t, out)
		})
	}
	_, err := Format(utf16(false, true), defaultConfig)
	assert.Contains(t, err.Error(), "PowerShell")
}

func TestFormatAcceptsReplacementCharacter(t *testing.T) {
	out, err := Format("FROM alpine\nLABEL x=\"\xEF\xBF\xBD\"\n", defaultConfig)
	require.NoError(t, err)
	assert.Equal(t, "FROM alpine\nLABEL x=\"\xEF\xBF\xBD\"\n", out)
}

func TestFormatParseErrors(t *testing.T) {
	for _, tt := range []struct{ input, wantErr string }{
		{"", "file with no instructions"},
		{"# just a comment\n", "file with no instructions"},
		{"FROM alpine\nRUN <<EOF\necho hi\n", "line 2: unterminated heredoc"},
	} {
		_, err := Format(tt.input, defaultConfig)
		require.Error(t, err, "%q", tt.input)
		assert.Equal(t, tt.wantErr, err.Error())
	}
}
