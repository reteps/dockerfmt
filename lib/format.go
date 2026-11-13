package lib

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/moby/buildkit/frontend/dockerfile/parser"
)

// Config controls formatting.
type Config struct {
	// IndentSize is the number of spaces continuation lines are indented by,
	// at most MaxIndentSize. Zero means DefaultIndentSize.
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

const (
	// DefaultIndentSize is the indentation used when Config.IndentSize is
	// zero.
	DefaultIndentSize = 4
	// MaxIndentSize is the largest Config.IndentSize allowed.
	MaxIndentSize = 16
)

func (c *Config) indent() string {
	return strings.Repeat(" ", int(c.IndentSize))
}

// Format formats the contents of a Dockerfile. It returns an error, and no
// output, if the input isn't UTF-8 text, BuildKit can't parse it, or c is
// invalid.
//
// The output keeps the input's line endings and UTF-8 byte order mark.
func Format(content string, c *Config) (string, error) {
	if c.IndentSize > MaxIndentSize {
		return "", fmt.Errorf("indent size %d is larger than %d", c.IndentSize, MaxIndentSize)
	}
	if err := checkEncoding(content); err != nil {
		return "", err
	}
	return formatContent(content, c)
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
	f.out.WriteString(formatComments(lines[f.line:], f.line == 0))

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
	f.out.WriteString(formatComments(between, f.line == 0))
	f.line = n.EndLine

	out := n.source
	if !hasIgnoreComment(between) {
		out = f.checked(n)
	}
	f.out.WriteString(out)
	// The last line of a file may lack a line ending; anywhere else, a missing
	// one would join this instruction with the next.
	if !strings.HasSuffix(out, "\n") {
		f.out.WriteByte('\n')
	}
	f.ctx.update(n.Node)
}

// checked formats instruction n and verifies the result: BuildKit must read
// it the same way as the original, and formatting it again must not change
// it. If either fails, or formatting does, n is returned as written.
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
		reason = verify(n, out, f.config)
	}
	if reason != "" {
		if f.config.OnFallback != nil {
			f.config.OnFallback(n.StartLine, reason)
		}
		return n.source
	}
	return out
}

// endsInContinuation reports whether the last line of s ends in a line
// continuation.
func endsInContinuation(s string, escape rune) bool {
	s = strings.TrimSuffix(s, "\n")
	_, ok := isContinuation(s[strings.LastIndexByte(s, '\n')+1:], escape)
	return ok
}

// verify checks out, the formatted form of n, and returns why it can't be
// used, or "" if it can.
func verify(n *node, out string, c *Config) (reason string) {
	defer func() {
		if r := recover(); r != nil {
			reason = fmt.Sprintf("checking the output failed: %v", r)
		}
	}()
	ctx := n.context()
	if endsInContinuation(out, ctx.escape) && !endsInContinuation(n.source, ctx.escape) {
		// It would run on into the next line of the file, which checking the
		// instruction on its own can't show.
		return "output ends in a line continuation"
	}
	formatted, err := parseFormatted(out, ctx)
	if err != nil {
		return err.Error()
	}
	if same, why := equivalent(n.Node, formatted.Node, ctx); !same {
		return why
	}
	// Comment lines can follow the instruction itself, when the formatter
	// moved them out of a continuation; formatted from then on, they are
	// lines between instructions.
	rest, ok := strings.CutPrefix(out, formatted.source)
	if !ok {
		return "output doesn't start with the instruction"
	}
	if again, _, _ := formatRecovering(formatted, c); again != formatted.source || formatComments(strings.SplitAfter(rest, "\n"), false) != rest {
		return "formatting it again would change it further"
	}
	return ""
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

// hasIgnoreComment reports whether the last comment before an instruction,
// among the lines between it and the previous one, is the ignore comment.
func hasIgnoreComment(lines []string) bool {
	for i := len(lines) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(lines[i]); t != "" {
			return t == ignoreComment
		}
	}
	return false
}

// reDirective matches a comment that would be a parser directive at the
// start of a line, like "# syntax=docker/dockerfile:1".
var reDirective = regexp.MustCompile(`(?i)^#\s*[a-z]+\s*=`)

// formatComments formats the comment and blank lines between instructions:
// leading and trailing whitespace is trimmed, and runs of blank lines become
// one. Before the first instruction, where BuildKit reads parser directives,
// an indented comment that would become a directive keeps its indentation.
func formatComments(lines []string, first bool) string {
	var b strings.Builder
	blank := false
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" {
			if blank {
				continue
			}
			blank = true
		} else {
			blank = false
			if first && reDirective.MatchString(t) {
				t = strings.TrimRightFunc(line, unicode.IsSpace)
			}
		}
		b.WriteString(t)
		if strings.HasSuffix(line, "\n") {
			b.WriteByte('\n')
		}
	}
	return b.String()
}
