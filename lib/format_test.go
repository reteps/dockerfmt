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

func TestFormatComments(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		first    bool
		expected string
	}{
		{"empty slice", []string{}, false, ""},
		{"single comment", []string{"# comment\n"}, false, "# comment\n"},
		{"leading whitespace stripped", []string{"  # comment\n"}, false, "# comment\n"},
		{"blank lines after a comment become one", []string{"# a\n", "\n", "\n", "\n", "# b\n"}, false, "# a\n\n# b\n"},
		{"one blank line preserved", []string{"# a\n", "\n", "# b\n"}, false, "# a\n\n# b\n"},
		{"blank lines after an instruction become one", []string{"\n", "\n", "# x\n", "\n"}, false, "\n# x\n\n"},
		{"would-be directive keeps its indentation", []string{" # syntax=docker/dockerfile:1\n"}, true, " # syntax=docker/dockerfile:1\n"},
		{"would-be directive after the first instruction", []string{" # syntax=x\n"}, false, "# syntax=x\n"},
		{"ordinary comment before the first instruction", []string{"  # hello\n"}, true, "# hello\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, formatComments(tt.input, tt.first))
		})
	}
}

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
			"statements on one line stay on one line",
			"echo  a;echo b",
			false,
			defaultConfig,
			"echo a; echo b\n",
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
			"statements in a block are indented",
			"if true; then \\\n  echo a; echo b; fi",
			false,
			defaultConfig,
			"if true; then \\\n        echo a; echo b; fi\n",
		},
		{
			"comment before operator takes its indentation",
			"apt-get install -y \\\n  curl \\\n  # note\n  && rm -rf x",
			false,
			defaultConfig,
			"apt-get install -y \\\n        curl \\\n    # note\n    && rm -rf x\n",
		},
		{
			"comment before operand of trailing operator",
			"a && \\\n  # note\n  b",
			false,
			defaultConfig,
			"a \\\n    # note\n    && b\n",
		},
		{
			"comment on first line gets a continuation",
			"\\\n  # note\n  echo hi",
			false,
			defaultConfig,
			"\\\n    # note\n    echo hi\n",
		},
		{
			"blank line inside continuation is dropped",
			"apk add \\\n    curl \\\n\n    git",
			false,
			defaultConfig,
			"apk add \\\n    curl \\\n    git\n",
		},
		{
			"comment between arguments",
			"apk add \\\n    curl \\\n    # vcs\n    git",
			false,
			defaultConfig,
			"apk add \\\n    curl \\\n    # vcs\n    git\n",
		},
		{
			"unparseable shell is left as written",
			"echo $((",
			false,
			defaultConfig,
			"echo $((",
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
			var result string
			if tt.hereDoc {
				result = formatScript(tt.content, tt.config, false)
			} else {
				result, _ = formatShellBody(tt.content, tt.config)
			}
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNewNode(t *testing.T) {
	t.Run("nil input returns nil", func(t *testing.T) {
		result := newNode(nil, []string{})
		assert.Nil(t, result)
	})

	t.Run("OriginalMultiline reconstructed", func(t *testing.T) {
		input := "FROM alpine\nRUN echo hello\n"
		lines := strings.SplitAfter(input, "\n")
		result, err := parser.Parse(strings.NewReader(input))
		require.NoError(t, err)

		root := newNode(result.AST, lines)
		require.NotNil(t, root)
		// Root node children should have the FROM and RUN directives
		require.GreaterOrEqual(t, len(root.children), 2)

		fromNode := root.children[0]
		assert.Equal(t, "FROM alpine\n", fromNode.source)

		runNode := root.children[1]
		assert.Equal(t, "RUN echo hello\n", runNode.source)
	})

	t.Run("multiline node", func(t *testing.T) {
		input := "FROM alpine\nRUN echo a \\\n    && echo b\n"
		lines := strings.SplitAfter(input, "\n")
		result, err := parser.Parse(strings.NewReader(input))
		require.NoError(t, err)

		root := newNode(result.AST, lines)
		runNode := root.children[1]
		assert.Equal(t, "RUN echo a \\\n    && echo b\n", runNode.source)
	})
}

func TestHeredocs(t *testing.T) {
	n := firstNonFromNode(t, "FROM alpine\nRUN <<EOF cat - <<END\necho hello\nEOF\nx\nEND\n")
	assert.Equal(t, "<<EOF cat - <<END\necho hello\nEOF\nx\nEND\n", n.heredocs())
	assert.Equal(t, "<<EOF cat - <<END\necho hi\nEOF\nx\nEND\n", n.heredocsWithScript("echo hi\n"))
}

func TestFormatNodeUnknownCommand(t *testing.T) {
	output, ok := formatNode(&node{Node: &parser.Node{Value: "UNKNOWNCMD"}}, defaultConfig)
	assert.False(t, ok)
	assert.Equal(t, "", output)
}

func TestHasIgnoreComment(t *testing.T) {
	tests := []struct {
		name     string
		lines    []string
		expected bool
	}{
		{"exact match", []string{"# dockerfmt-ignore\n"}, true},
		{"with leading spaces", []string{"  # dockerfmt-ignore\n"}, true},
		{"with tab", []string{"\t# dockerfmt-ignore\n"}, true},
		{"no newline", []string{"# dockerfmt-ignore"}, true},
		{"among other comments", []string{"# a comment\n", "# dockerfmt-ignore\n"}, true},
		{"followed by a blank line", []string{"# dockerfmt-ignore\n", "\n"}, true},
		{"not the last comment", []string{"# dockerfmt-ignore\n", "# a comment\n"}, false},
		{"not present", []string{"# a comment\n", "# another\n"}, false},
		{"partial match", []string{"# dockerfmt-ignore this\n"}, false},
		{"empty", []string{}, false},
		{"blank lines only", []string{"\n", "\n"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, hasIgnoreComment(tt.lines))
		})
	}
}

func firstNonFromNode(t *testing.T, input string) *node {
	t.Helper()
	lines := strings.SplitAfter(input, "\n")
	result, err := parser.Parse(strings.NewReader(input))
	require.NoError(t, err)
	root := newNode(result.AST, lines)
	for _, child := range root.children {
		if child.keyword() != "FROM" {
			return child
		}
	}
	t.Fatal("no instruction after FROM")
	return nil
}

func TestScanFlagsRegion(t *testing.T) {
	f := func(s string) flagItem { return flagItem{s, flagItemFlag} }
	c := func(s string) flagItem { return flagItem{s, flagItemComment} }
	sep := flagItem{"\\", flagItemSeparator}

	tests := []struct {
		name        string
		input       string
		want        []flagItem
		wantContent string
		wantEnd     bool
		wantOK      bool
	}{
		{"no flags", "FROM a\nRUN \\\n  \\\n  echo\n", nil, "\\\n  \\\n  echo\n", false, true},
		{"flags on one line", "FROM a\nRUN --network=host --mount=type=cache,target=/a echo\n",
			[]flagItem{f("--network=host"), f("--mount=type=cache,target=/a")}, "echo\n", false, true},
		{"quoted value keeps its quotes", "FROM a\nCOPY --chown=\"my user\" a b\n",
			[]flagItem{f("--chown=\"my user\"")}, "a b\n", false, true},
		{"end of flags", "FROM a\nRUN --network=host -- --x\n",
			[]flagItem{f("--network=host")}, "--x\n", true, true},
		{"leading continuation is not a separator", "FROM a\nRUN \\\n  --network=host \\\n  echo\n",
			[]flagItem{f("--network=host")}, "echo\n", false, true},
		{"separator after last flag", "FROM a\nRUN --network=host \\\n  \\\n  echo\n",
			[]flagItem{f("--network=host"), sep}, "echo\n", false, true},
		{"comment before first flag", "FROM a\nRUN \\\n  # why \\\n  --network=host \\\n  echo\n",
			[]flagItem{c("# why \\"), f("--network=host")}, "echo\n", false, true},
		{"comment and separator between flags", "FROM a\nRUN --mount=type=cache,target=/a \\\n  # c\n  \\\n  --network=host \\\n  echo\n",
			[]flagItem{f("--mount=type=cache,target=/a"), c("# c"), sep, f("--network=host")}, "echo\n", false, true},
		{"comment after last flag belongs to content", "FROM a\nRUN --network=host \\\n  # c\n  echo\n",
			[]flagItem{f("--network=host")}, "\\\n  # c\n  echo\n", false, true},
		{"flag glued to the next line", "FROM a\nRUN --network=ho\\\nst echo\n", nil, "", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := scanFlagsRegion(firstNonFromNode(t, tt.input))
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, got.items)
			assert.Equal(t, tt.wantEnd, got.endOfFlags)
			if tt.wantOK {
				assert.Equal(t, tt.wantContent, got.content)
			}
		})
	}
}

func TestIsEmptyContinuation(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		i     int
		want  bool
	}{
		{"directive tail", []string{"\\\n", "  echo\n"}, 0, false},
		{"indented", []string{"echo a \\\n", "  \\\n", "  && echo b\n"}, 1, true},
		{"trailing whitespace", []string{"echo a \\\n", "  \\  \n", "  && echo b\n"}, 1, true},
		{"space before previous backslash", []string{"echo a \\\n", "\\\n", "&& echo b\n"}, 1, true},
		{"next line indented", []string{"echo a\\\n", "\\\n", "  b\n"}, 1, true},
		{"after comment", []string{"echo a \\\n", "# c\n", "\\\n", "b\n"}, 2, true},
		{"joins a word", []string{"echo good\\\n", "\\\n", "night\n"}, 1, false},
		{"last line", []string{"echo a \\\n", "  \\\n"}, 1, false},
		{"not a backslash line", []string{"echo a \\\n", "  b \\\n", "  c\n"}, 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isEmptyContinuation(tt.lines, tt.i))
		})
	}
}
func TestIndentSizeTooLarge(t *testing.T) {
	_, err := Format("FROM a\n", &Config{IndentSize: MaxIndentSize + 1})
	assert.EqualError(t, err, "indent size 17 is larger than 16")
}

func TestZeroIndentSizeMeansDefault(t *testing.T) {
	out := mustFormat(t, "FROM a\nRUN a \\\n && b\n", &Config{})
	assert.Equal(t, "FROM a\nRUN a \\\n    && b", out)
}

func TestMixedLineEndingsThatMatterAreKept(t *testing.T) {
	// The heredoc body would get the file's dominant "\r\n" line endings,
	// which BuildKit keeps in the script.
	input := "FROM a\r\nRUN  echo a\r\nRUN  echo b\r\nRUN <<EOF\necho x\nEOF\n"
	var fallbacks []int
	c := &Config{IndentSize: 4, OnFallback: func(line int, _ string) { fallbacks = append(fallbacks, line) }}
	assert.Equal(t, input, mustFormat(t, input, c))
	assert.Equal(t, []int{4}, fallbacks)
}

// Byte-level cases that golden files can't show clearly.
func TestFormatByteEdgeCases(t *testing.T) {
	c := &Config{IndentSize: 4, TrailingNewline: true}
	for _, tt := range []struct{ name, input, want string }{
		{"vertical tab inside a word", "FROM a\nRUN  echo a\vb\n", "FROM a\nRUN  echo a\vb\n"},
		{"stray carriage returns", "FROM a\r\r\nRUN  echo a\r", "FROM a\r\nRUN echo a\r\n"},
	} {
		out := mustFormat(t, tt.input, c)
		assert.Equal(t, tt.want, out, tt.name)
		assert.Equal(t, out, mustFormat(t, out, c), "%s: idempotent", tt.name)
	}
}
