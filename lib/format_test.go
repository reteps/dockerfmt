package lib

import (
	"strings"
	"testing"

	"github.com/moby/buildkit/frontend/dockerfile/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var defaultConfig = &Config{
	IndentSize:      4,
	TrailingNewline: true,
	SpaceRedirects:  false,
}

// --- StripWhitespace ---

func TestStripWhitespace(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		rightOnly bool
		expected  string
	}{
		{"empty string", "", false, ""},
		{"single line no whitespace", "hello", false, "hello"},
		{"strip both sides", "  hello  ", false, "hello"},
		{"right only preserves leading", "  hello  ", true, "  hello"},
		{"multiline strip both", "  a  \n  b  \n", false, "a\nb\n"},
		{"multiline right only", "  a  \n  b  \n", true, "  a\n  b\n"},
		{"tabs stripped", "\thello\t\n", false, "hello\n"},
		{"tabs right only", "\thello\t\n", true, "\thello\n"},
		{"preserves internal newlines", "a\n\nb\n", false, "a\n\nb\n"},
		{"trailing newline preserved", "a\n", false, "a\n"},
		{"only whitespace", "   \n", false, "\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := StripWhitespace(tt.input, tt.rightOnly)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// --- FormatComments ---

func TestFormatComments(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected string
	}{
		{"empty slice", []string{}, ""},
		{"single comment", []string{"# comment\n"}, "# comment\n"},
		{"leading whitespace stripped", []string{"  # comment\n"}, "# comment\n"},
		{"three newlines collapsed to one", []string{"# a\n", "\n", "\n", "\n", "# b\n"}, "# a\n# b\n"},
		{"two newlines preserved", []string{"# a\n", "\n", "# b\n"}, "# a\n\n# b\n"},
		{"blank line around comment", []string{"\n", "# x\n", "\n"}, "\n# x\n\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatComments(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// --- IndentFollowingLines ---

func TestIndentFollowingLines(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		indentSize uint
		expected   string
	}{
		{"empty string", "", 4, ""},
		{"single line unchanged", "RUN echo\n", 4, "RUN echo\n"},
		{"two lines indent 4", "A\nB\n", 4, "A\n    B\n"},
		{"two lines indent 2", "A\nB\n", 2, "A\n  B\n"},
		{"two lines indent 8", "A\nB\n", 8, "A\n        B\n"},
		{"empty lines indented", "A\n\nB\n", 4, "A\n    \n    B\n"},
		{"existing indent replaced", "A\n  B\n", 4, "A\n    B\n"},
		{"three lines", "A\nB\nC\n", 4, "A\n    B\n    C\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IndentFollowingLines(tt.input, tt.indentSize)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// --- marshalJSONStringArray ---

func TestMarshalJSONStringArray(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected string
	}{
		{"string slice", []string{"a", "b"}, `["a", "b"]`},
		{"angle brackets not escaped", []string{"<foo>"}, `["<foo>"]`},
		{"ampersand not escaped", []string{"a&b"}, `["a&b"]`},
		{"empty slice", []string{}, `[]`},
		{"single item", []string{"hello"}, `["hello"]`},
		{"with quotes", []string{`say "hi"`}, `["say \"hi\""]`},
		{"with backslash", []string{`a\b`}, `["a\\b"]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := marshalJSONStringArray(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// --- unmarshalJSONStringArray ---

func TestUnmarshalJSONStringArray(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
		ok       bool
	}{
		{"simple array", `["a", "b"]`, []string{"a", "b"}, true},
		{"no spaces", `["a","b"]`, []string{"a", "b"}, true},
		{"empty array", `[]`, []string{}, true},
		{"single item", `["hello"]`, []string{"hello"}, true},
		{"with escapes", `["say \"hi\""]`, []string{`say "hi"`}, true},
		{"not json", `echo hello`, nil, false},
		{"not array", `"hello"`, nil, false},
		{"mixed types", `["a", 1]`, nil, false},
		{"nested array", `[["a"]]`, nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, ok := unmarshalJSONStringArray(tt.input)
			assert.Equal(t, tt.ok, ok)
			if ok {
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

// --- formatBash ---

func TestFormatBash(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		config   *Config
		expected string
	}{
		{
			"simple command",
			"echo hello",
			defaultConfig,
			"echo hello\n",
		},
		{
			"redirect no space",
			"echo foo >>bar",
			&Config{IndentSize: 4, SpaceRedirects: false},
			"echo foo >>bar\n",
		},
		{
			"redirect with space",
			"echo foo >>bar",
			&Config{IndentSize: 4, SpaceRedirects: true},
			"echo foo >> bar\n",
		},
		{
			"multiline binary next line",
			"echo a \\\n&& echo b",
			defaultConfig,
			"echo a \\\n    && echo b\n",
		},
		{
			"indent 2",
			"if true; then\necho hi\nfi",
			&Config{IndentSize: 2, SpaceRedirects: false},
			"if true; then\n  echo hi\nfi\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatBash(tt.input, tt.config)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// --- formatShell ---

func TestFormatShell(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		hereDoc  bool
		config   *Config
		expected string
	}{
		{
			"simple command passthrough",
			"echo hello",
			false,
			defaultConfig,
			"echo hello\n",
		},
		{
			"unescaped semicolons bail out",
			"echo a; echo b",
			false,
			defaultConfig,
			"echo a; echo b",
		},
		{
			"grouped expressions bail out",
			"{ \\ foo",
			false,
			defaultConfig,
			"{ \\ foo",
		},
		{
			"heredoc mode simple",
			"echo hi\necho bye\n",
			true,
			defaultConfig,
			"echo hi\necho bye\n",
		},
		{
			"space redirects passed through",
			"echo foo >>bar",
			false,
			&Config{IndentSize: 4, SpaceRedirects: true},
			"echo foo >> bar\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatShell(tt.content, tt.hereDoc, tt.config)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// --- FormatFileLines: Config variations ---

func formatDockerfile(input string, c *Config) string {
	lines := strings.SplitAfter(input, "\n")
	return FormatFileLines(lines, c)
}

func TestConfigVariations(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		config   *Config
		contains string
		expected string
	}{
		{
			"trailing newline true",
			"FROM alpine\n",
			&Config{IndentSize: 4, TrailingNewline: true},
			"",
			"FROM alpine\n",
		},
		{
			"trailing newline false",
			"FROM alpine\n",
			&Config{IndentSize: 4, TrailingNewline: false},
			"",
			"FROM alpine",
		},
		{
			"space redirects true",
			"FROM alpine\nRUN echo foo >>bar\n",
			&Config{IndentSize: 4, TrailingNewline: true, SpaceRedirects: true},
			">> bar",
			"",
		},
		{
			"space redirects false",
			"FROM alpine\nRUN echo foo >>bar\n",
			&Config{IndentSize: 4, TrailingNewline: true, SpaceRedirects: false},
			">>bar",
			"",
		},
		{
			"indent 2",
			"FROM alpine\nRUN echo a \\\n  && echo b\n",
			&Config{IndentSize: 2, TrailingNewline: true},
			"",
			"FROM alpine\nRUN echo a \\\n  && echo b\n",
		},
		{
			"indent 8",
			"FROM alpine\nRUN echo a \\\n  && echo b\n",
			&Config{IndentSize: 8, TrailingNewline: true},
			"",
			"FROM alpine\nRUN echo a \\\n        && echo b\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatDockerfile(tt.input, tt.config)
			if tt.expected != "" {
				assert.Equal(t, tt.expected, result)
			}
			if tt.contains != "" {
				assert.Contains(t, result, tt.contains)
			}
		})
	}
}

// --- FormatFileLines: Per-directive tests ---

func TestPerDirective(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			"FROM basic",
			"from  alpine\n",
			"FROM alpine\n",
		},
		{
			"FROM with tag",
			"FROM nginx:latest\n",
			"FROM nginx:latest\n",
		},
		{
			"ENV legacy format",
			"FROM alpine\nENV MY_VAR my-value\n",
			"FROM alpine\nENV MY_VAR=my-value\n",
		},
		{
			"ENV modern format",
			"FROM alpine\nENV MY_VAR=my-value\n",
			"FROM alpine\nENV MY_VAR=my-value\n",
		},
		{
			"MAINTAINER converted to LABEL",
			"FROM alpine\nMAINTAINER me\n",
			"FROM alpine\nLABEL org.opencontainers.image.authors=\"me\"\n",
		},
		{
			"CMD JSON form adds spaces",
			"FROM alpine\nCMD [\"ls\",\"-la\"]\n",
			"FROM alpine\nCMD [\"ls\", \"-la\"]\n",
		},
		{
			"CMD shell form",
			"FROM alpine\nCMD echo hello\n",
			"FROM alpine\nCMD echo hello\n",
		},
		{
			"ENTRYPOINT JSON form",
			"FROM alpine\nENTRYPOINT [\"nginx\"]\n",
			"FROM alpine\nENTRYPOINT [\"nginx\"]\n",
		},
		{
			"RUN JSON form adds spaces",
			"FROM alpine\nRUN [\"echo\",\"hello\"]\n",
			"FROM alpine\nRUN [\"echo\", \"hello\"]\n",
		},
		{
			"RUN with flags",
			"FROM alpine\nRUN --network=host echo hello\n",
			"FROM alpine\nRUN --network=host echo hello\n",
		},
		{
			"COPY normalizes whitespace",
			"FROM alpine\nCOPY  .   /app\n",
			"FROM alpine\nCOPY . /app\n",
		},
		{
			"ARG basic",
			"FROM alpine\nARG FOO=bar\n",
			"FROM alpine\nARG FOO=bar\n",
		},
		{
			"HEALTHCHECK NONE uppercased",
			"FROM alpine\nhealthcheck NONE\n",
			"FROM alpine\nHEALTHCHECK NONE\n",
		},
		{
			"WORKDIR",
			"FROM alpine\nworkdir /app\n",
			"FROM alpine\nWORKDIR /app\n",
		},
		{
			"EXPOSE",
			"FROM alpine\nexpose 8080\n",
			"FROM alpine\nEXPOSE 8080\n",
		},
		{
			"USER",
			"FROM alpine\nuser nobody\n",
			"FROM alpine\nUSER nobody\n",
		},
		{
			"STOPSIGNAL",
			"FROM alpine\nstopsignal SIGTERM\n",
			"FROM alpine\nSTOPSIGNAL SIGTERM\n",
		},
		{
			"LABEL basic",
			"FROM alpine\nLABEL version=\"1.0\"\n",
			"FROM alpine\nLABEL version=\"1.0\"\n",
		},
		{
			"ONBUILD COPY",
			"FROM alpine\nONBUILD COPY . /app\n",
			"FROM alpine\nONBUILD COPY . /app\n",
		},
		{
			"SHELL directive",
			"FROM alpine\nSHELL [\"/bin/bash\", \"-c\"]\n",
			"FROM alpine\nSHELL [\"/bin/bash\", \"-c\"]\n",
		},
		{
			"VOLUME",
			"FROM alpine\nvolume /data\n",
			"FROM alpine\nVOLUME /data\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatDockerfile(tt.input, defaultConfig)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// --- BuildExtendedNode ---

func TestBuildExtendedNode(t *testing.T) {
	t.Run("nil input returns nil", func(t *testing.T) {
		result := BuildExtendedNode(nil, []string{})
		assert.Nil(t, result)
	})

	t.Run("OriginalMultiline reconstructed", func(t *testing.T) {
		input := "FROM alpine\nRUN echo hello\n"
		lines := strings.SplitAfter(input, "\n")
		result, err := parser.Parse(strings.NewReader(input))
		require.NoError(t, err)

		root := BuildExtendedNode(result.AST, lines)
		require.NotNil(t, root)
		// Root node children should have the FROM and RUN directives
		require.GreaterOrEqual(t, len(root.Children), 2)

		fromNode := root.Children[0]
		assert.Equal(t, "FROM alpine\n", fromNode.OriginalMultiline)

		runNode := root.Children[1]
		assert.Equal(t, "RUN echo hello\n", runNode.OriginalMultiline)
	})

	t.Run("multiline node", func(t *testing.T) {
		input := "FROM alpine\nRUN echo a \\\n    && echo b\n"
		lines := strings.SplitAfter(input, "\n")
		result, err := parser.Parse(strings.NewReader(input))
		require.NoError(t, err)

		root := BuildExtendedNode(result.AST, lines)
		runNode := root.Children[1]
		assert.Equal(t, "RUN echo a \\\n    && echo b\n", runNode.OriginalMultiline)
	})
}

// --- GetHeredoc ---

func TestGetHeredoc(t *testing.T) {
	t.Run("node without heredoc returns false", func(t *testing.T) {
		input := "FROM alpine\nRUN echo hello\n"
		lines := strings.SplitAfter(input, "\n")
		result, err := parser.Parse(strings.NewReader(input))
		require.NoError(t, err)

		root := BuildExtendedNode(result.AST, lines)
		runNode := root.Children[1]
		_, ok := GetHeredoc(runNode)
		assert.False(t, ok)
	})

	t.Run("node with heredoc returns content", func(t *testing.T) {
		input := "FROM alpine\nRUN <<EOF\necho hello\nEOF\n"
		lines := strings.SplitAfter(input, "\n")
		result, err := parser.Parse(strings.NewReader(input))
		require.NoError(t, err)

		root := BuildExtendedNode(result.AST, lines)
		runNode := root.Children[1]
		content, ok := GetHeredoc(runNode)
		assert.True(t, ok)
		assert.Contains(t, content, "echo hello")
		assert.Contains(t, content, "EOF")
	})
}

// --- GetFileLines ---

func TestGetFileLines(t *testing.T) {
	t.Run("reads existing fixture", func(t *testing.T) {
		lines, err := GetFileLines("../tests/in/comment.dockerfile")
		require.NoError(t, err)
		require.NotEmpty(t, lines)
		// Each line except possibly the last should end with \n (SplitAfter semantics)
		for i, line := range lines[:len(lines)-1] {
			if line != "" {
				assert.True(t, strings.HasSuffix(line, "\n"), "line %d should end with newline: %q", i, line)
			}
		}
	})

	t.Run("nonexistent file returns error", func(t *testing.T) {
		_, err := GetFileLines("nonexistent.dockerfile")
		assert.Error(t, err)
	})
}

// --- FormatNode: unknown command ---

func TestFormatNodeUnknownCommand(t *testing.T) {
	// FormatNode returns ("", false) for unknown commands
	node := &ExtendedNode{
		Node: &parser.Node{Value: "UNKNOWNCMD"},
	}
	output, ok := FormatNode(node, defaultConfig)
	assert.False(t, ok)
	assert.Equal(t, "", output)
}
