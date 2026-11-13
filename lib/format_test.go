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
			"printer splitting statements bails out",
			"if true; then \\\n  echo a; echo b; fi",
			false,
			defaultConfig,
			"if true; then \\\n  echo a; echo b; fi",
		},
		{
			"comment before operator keeps argument indentation",
			"apt-get install -y \\\n  curl \\\n  # note\n  && rm -rf x",
			false,
			defaultConfig,
			"apt-get install -y \\\n    curl \\\n    # note\n    && rm -rf x\n",
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
				result = formatShellBody(tt.content, tt.config)
			}
			assert.Equal(t, tt.expected, result)
		})
	}
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
			result := mustFormat(t, tt.input, tt.config)
			if tt.expected != "" {
				assert.Equal(t, tt.expected, result)
			}
			if tt.contains != "" {
				assert.Contains(t, result, tt.contains)
			}
		})
	}
}

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
			"ENV legacy value with spaces is quoted",
			"FROM alpine\nENV JAVA_OPTS -Xms256m -Xmx512m\n",
			"FROM alpine\nENV JAVA_OPTS=\"-Xms256m -Xmx512m\"\n",
		},
		{
			"ENV legacy value with tabs and repeated spaces",
			"FROM alpine\nENV A one\t two   three\n",
			"FROM alpine\nENV A=\"one\t two   three\"\n",
		},
		{
			"ENV legacy value with variable",
			"FROM alpine\nENV PATH $PATH:/opt/bin /usr/x\n",
			"FROM alpine\nENV PATH=\"$PATH:/opt/bin /usr/x\"\n",
		},
		{
			"ENV legacy already quoted value",
			"FROM alpine\nENV A \"one two\"\n",
			"FROM alpine\nENV A=\"one two\"\n",
		},
		{
			"ENV legacy value with embedded quotes stays legacy",
			"FROM alpine\nENV A say \"hi\" there\n",
			"FROM alpine\nENV A say \"hi\" there\n",
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
			"MAINTAINER with embedded quotes",
			"FROM alpine\nMAINTAINER Jane Doe \"jane@example.com\"\n",
			"FROM alpine\nLABEL org.opencontainers.image.authors=\"Jane Doe \\\"jane@example.com\\\"\"\n",
		},
		{
			"MAINTAINER wrapped in one pair of quotes",
			"FROM alpine\nMAINTAINER \"Jane Doe\"\n",
			"FROM alpine\nLABEL org.opencontainers.image.authors=\"Jane Doe\"\n",
		},
		{
			"MAINTAINER with backslash",
			"FROM alpine\nMAINTAINER a\\\\b\n",
			"FROM alpine\nLABEL org.opencontainers.image.authors=\"a\\\\\\\\b\"\n",
		},
		{
			"MAINTAINER with variable is not expanded by LABEL",
			"FROM alpine\nMAINTAINER $USER\n",
			"FROM alpine\nLABEL org.opencontainers.image.authors=\"\\$USER\"\n",
		},
		{
			"MAINTAINER with email in angle brackets",
			"FROM alpine\nMAINTAINER Jane Doe <jane@example.com>\n",
			"FROM alpine\nLABEL org.opencontainers.image.authors=\"Jane Doe <jane@example.com>\"\n",
		},
		{
			"MAINTAINER trailing spaces",
			"FROM alpine\nMAINTAINER me   \n",
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
			result := mustFormat(t, tt.input, defaultConfig)
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

func TestIgnoreDirective(t *testing.T) {
	output := mustFormat(t, "FROM    scratch\n# dockerfmt-ignore\nRUN   echo   hello\nRUN   echo   world\n", defaultConfig)
	expected := "FROM scratch\n# dockerfmt-ignore\nRUN   echo   hello\nRUN echo world\n"
	assert.Equal(t, expected, output)
}

// firstNonFromNode parses input and returns its first instruction other
// than FROM.
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

// Inputs the fuzzer or bug hunts found mishandled.
func TestRegressions(t *testing.T) {
	tests := []struct {
		name, input, want string
	}{
		{"quoted flag value keeps its quotes", "FROM a\nCOPY --chown=\"my user\"  a b\n",
			"FROM a\nCOPY --chown=\"my user\" a b\n"},
		{"quoted mount target", "FROM a\nRUN --mount=type=cache,target=\"/x y\"   echo hi\n",
			"FROM a\nRUN --mount=type=cache,target=\"/x y\" \\\n    echo hi\n"},
		{"comment among COPY arguments is kept", "FROM a\nCOPY --chown=me \\\n  # why\n  a b\n",
			"FROM a\nCOPY --chown=me \\\n  # why\n  a b\n"},
		{"end of flags without flags", "FROM a\nCOPY -- foo /tmp/\n", "FROM a\nCOPY -- foo /tmp/\n"},
		{"end of flags before JSON", "FROM a\nCMD --doit=true -- [ \"a\" ]\n", "FROM a\nCMD --doit=true -- [\"a\"]\n"},
		{"empty body", "FROM a\nRUN\n0\n", "FROM a\nRUN\n0\n"},
		{"MAINTAINER without a value", "FROM a\nMAINTAINER\n", "FROM a\nMAINTAINER\n"},
		{"separator after the directive line", "FROM a\nRUN \\\n\\\n echo hi\n",
			"FROM a\nRUN \\\n    \\\n    echo hi\n"},
		{"trailing comment is not indented", "FROM a\nRUN echo a \\\n#\n", "FROM a\nRUN echo a \\\n#\n"},
		{"shell comment running into the next line", "FROM a\nRUN echo a&#\\\necho b\\\n#\n echo c\n",
			"FROM a\nRUN echo a&#\\\necho b\\\n#\n echo c\n"},
		{"dangling escape", "FROM a\nRUN  echo a\\\\\\\necho b\n", "FROM a\nRUN  echo a\\\\\\\necho b\n"},
		{"vertical tab inside a word", "FROM a\nRUN  echo a\vb\n", "FROM a\nRUN  echo a\vb\n"},
		{"comment on the directive line is shell code", "FROM a\nRUN # c \\\n  echo hi\n", "FROM a\nRUN # c \\\n  echo hi\n"},
		{"backquotes are left alone", "FROM a\nRUN  echo \"`echo \\\"hi\\\"`\"\n", "FROM a\nRUN  echo \"`echo \\\"hi\\\"`\"\n"},
		{"stage built on a PowerShell stage", "FROM a AS base\nSHELL [\"pwsh\", \"-Command\"]\nFROM base\nRUN  Write-Host   x\n",
			"FROM a AS base\nSHELL [\"pwsh\", \"-Command\"]\nFROM base\nRUN  Write-Host   x\n"},
		{"stage built on a POSIX stage", "FROM a AS Base\nFROM base\nRUN  echo   x\n", "FROM a AS Base\nFROM base\nRUN echo x\n"},
		{"Windows base image", "FROM mcr.microsoft.com/windows/servercore:ltsc2022\nRUN echo hello   world> C:\\out.txt\n",
			"FROM mcr.microsoft.com/windows/servercore:ltsc2022\nRUN echo hello   world> C:\\out.txt\n"},
		{"indented would-be syntax directive", " # syntax=docker/dockerfile:1\nFROM a\n", " # syntax=docker/dockerfile:1\nFROM a\n"},
		{"blank lines after a comment", "FROM a\n# section\n\n\nRUN b\n", "FROM a\n# section\n\nRUN b\n"},
		{"ignore comment must be the last comment", "# dockerfmt-ignore\n# other\nFROM   a\n", "# dockerfmt-ignore\n# other\nFROM a\n"},
		{"chomped heredoc is indented with tabs", "FROM a\nRUN <<-EOF\n\tif true; then\n\t\techo   x\n\tfi\nEOF\n",
			"FROM a\nRUN <<-EOF\nif true; then\n\techo x\nfi\nEOF\n"},
		{"FROM AS takes the case of FROM", "from a as b\nFROM c As d\n", "FROM a AS b\nFROM c AS d\n"},
		{"EXPOSE protocols are lower case", "FROM a\nEXPOSE 80/TCP 53/Udp ${P}/SCTP 9/${PROTO} 8080\n",
			"FROM a\nEXPOSE 80/tcp 53/udp ${P}/sctp 9/${PROTO} 8080\n"},
		{"blank lines inside a continuation are removed", "FROM a\nENV A=1 \\\n\n  B=2\nLABEL a=1 \\\n  \n  # c\n  b=2\n",
			"FROM a\nENV A=1 \\\n    B=2\nLABEL a=1 \\\n    # c\n    b=2\n"},
		{"stray carriage returns", "FROM a\r\r\nRUN  echo a\r", "FROM a\r\nRUN echo a\r\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Config{IndentSize: 4, TrailingNewline: true}
			out := mustFormat(t, tt.input, c)
			assert.Equal(t, tt.want, out)
			assert.Equal(t, out, mustFormat(t, out, c), "idempotent")
		})
	}
}

func TestBacktickEscapeFlagLayout(t *testing.T) {
	c := &Config{IndentSize: 4, TrailingNewline: true, OnFallback: func(line int, reason string) {
		t.Errorf("line %d left unformatted: %s", line, reason)
	}}
	for _, tt := range []struct{ input, want string }{
		{"# escape=`\nFROM a\nRUN --mount=type=cache,target=/x echo hi\n",
			"# escape=`\nFROM a\nRUN --mount=type=cache,target=/x `\n    echo hi\n"},
		{"# escape=`\nFROM a\nCOPY --chown=a:b C:\\src\\ C:\\dst\\\n",
			"# escape=`\nFROM a\nCOPY --chown=a:b C:\\src\\ C:\\dst\\\n"},
		{"# escape=`\nFROM a\nCOPY --chown=a:b `\n  a b\n",
			"# escape=`\nFROM a\nCOPY --chown=a:b `\n    a b\n"},
	} {
		out := mustFormat(t, tt.input, c)
		assert.Equal(t, tt.want, out)
		assert.Equal(t, out, mustFormat(t, out, c), "idempotent")
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
