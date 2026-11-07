package lib

import (
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/parser"
)

// Config controls formatting.
type Config struct {
	// IndentSize is the number of spaces continuation lines are indented by.
	IndentSize uint
	// TrailingNewline ends the output with a newline.
	TrailingNewline bool
	// SpaceRedirects puts a space after shell redirect operators ("> file").
	SpaceRedirects bool
	// OnFallback, if set, is called for each instruction left as written
	// because formatting it would have changed its meaning, or failed. line
	// is the instruction's first line, counting from 1.
	OnFallback func(line int, reason string)
}

func (c *Config) indent() string {
	return strings.Repeat(" ", int(c.IndentSize))
}

// Format formats the contents of a Dockerfile. It returns an error, and no
// output, if the input isn't UTF-8 text or BuildKit can't parse it.
//
// The output keeps the input's line endings and UTF-8 byte order mark.
func Format(content string, c *Config) (string, error) {
	if err := checkEncoding(content); err != nil {
		return "", err
	}
	return formatContent(content, c)
}

// FormatFileLines formats a Dockerfile given as lines that each keep their
// line ending, as returned by strings.SplitAfter(content, "\n"). Unlike
// Format, it doesn't check the encoding, and it exits the process if the
// file can't be parsed.
//
// Deprecated: Use Format.
func FormatFileLines(fileLines []string, c *Config) string {
	content := strings.Join(fileLines, "")
	out, err := formatContent(content, c)
	if err != nil {
		log.Fatalf("Error parsing file: %v", err)
	}
	return out
}

// formatLines formats a Dockerfile given as lines that each end in "\n",
// except possibly the last.
func formatLines(lines []string, c *Config) (string, error) {
	result, err := parser.Parse(strings.NewReader(strings.Join(lines, "")))
	if err != nil {
		return "", parseError(err)
	}
	f := &fileFormatter{lines: lines, config: c, ctx: defaultContext()}
	if result.EscapeToken != 0 {
		f.ctx.escape = result.EscapeToken
	}
	for _, n := range newNode(result.AST, lines).children {
		f.instruction(n)
	}
	f.out.WriteString(formatComments(lines[f.line:]))

	out := strings.TrimRight(f.out.String(), "\n")
	if c.TrailingNewline {
		out += "\n"
	}
	return out, nil
}

// fileFormatter formats a file one instruction at a time, copying the comments
// and blank lines between instructions.
type fileFormatter struct {
	lines  []string
	config *Config
	ctx    instrContext
	out    strings.Builder
	// line is the number of lines consumed so far.
	line int
}

const ignoreComment = "# dockerfmt-ignore"

func (f *fileFormatter) instruction(n *node) {
	between := f.lines[f.line : n.StartLine-1]
	f.out.WriteString(formatComments(between))
	f.line = n.EndLine

	if hasIgnoreComment(between) {
		f.out.WriteString(n.source)
	} else {
		f.out.WriteString(f.checked(n))
	}
	f.ctx.update(n.Node)
}

// checked formats instruction n and verifies that BuildKit reads the result
// the same way as the original. If it doesn't, or formatting fails, n is
// returned as written.
func (f *fileFormatter) checked(n *node) string {
	ctx := f.ctx
	n.ctx = &ctx
	out, ok, reason := formatRecovering(n, f.config)
	if !ok {
		// No formatter for this keyword. BuildKit rejects unknown
		// instructions only when building, so keep it.
		return n.source
	}
	if reason == "" && out != n.source {
		if same, why := equivalent(n.Node, out, ctx); !same {
			reason = why
		}
	}
	if reason != "" {
		if f.config.OnFallback != nil {
			f.config.OnFallback(n.StartLine, reason)
		}
		return n.source
	}
	return out
}

// formatRecovering is formatNode, with a panic reported as a fallback reason.
func formatRecovering(n *node, c *Config) (out string, ok bool, reason string) {
	defer func() {
		if r := recover(); r != nil {
			out, ok, reason = "", true, fmt.Sprintf("formatter error: %v", r)
		}
	}()
	out, ok = formatNode(n, c)
	return out, ok, ""
}

func hasIgnoreComment(lines []string) bool {
	for _, line := range lines {
		if strings.TrimSpace(line) == ignoreComment {
			return true
		}
	}
	return false
}

var reBlankLines = regexp.MustCompile(`\n{3,}`)

// formatComments formats the lines between instructions: whitespace is
// trimmed and runs of blank lines are collapsed to one.
func formatComments(lines []string) string {
	var b strings.Builder
	for _, line := range lines {
		b.WriteString(strings.Trim(line, " \t\n"))
		if strings.HasSuffix(line, "\n") {
			b.WriteByte('\n')
		}
	}
	return reBlankLines.ReplaceAllString(b.String(), "\n")
}
