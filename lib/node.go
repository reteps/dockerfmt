package lib

import (
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/parser"
)

// node is a BuildKit parse node together with the source text it came from.
// BuildKit's parser joins continuation lines and drops comments, blank lines
// and "\"-only lines inside them, so formatters that preserve layout work from
// source instead.
type node struct {
	*parser.Node
	children []*node
	next     *node
	// source is the instruction's physical lines as written, including the
	// final line ending. It is empty for nodes without a position, such as
	// the instruction inside ONBUILD.
	source string
	// ctx is the file-level state the instruction is interpreted in, set
	// just before it is formatted. nil means the defaults, for nodes built
	// outside a file walk, as in tests.
	ctx *instrContext
}

// newNode wraps the parse tree rooted at n. lines are the file's lines, each
// keeping its "\n".
func newNode(n *parser.Node, lines []string) *node {
	if n == nil {
		return nil
	}
	en := &node{Node: n}
	if n.StartLine > 0 && n.EndLine > 0 {
		en.source = strings.Join(lines[n.StartLine-1:n.EndLine], "")
	}
	for _, child := range n.Children {
		en.children = append(en.children, newNode(child, lines))
	}
	en.next = newNode(n.Next, lines)
	return en
}

func (n *node) context() instrContext {
	if n.ctx == nil {
		return defaultContext()
	}
	return *n.ctx
}

// keyword returns the instruction keyword in upper case, e.g. "RUN".
func (n *node) keyword() string {
	return strings.ToUpper(n.Value)
}

// text returns the instruction's source, or BuildKit's joined line for nodes
// without a position.
func (n *node) text() string {
	if n.source != "" {
		return n.source
	}
	return n.Original
}

// multiline reports whether the instruction spans several lines.
func (n *node) multiline() bool {
	escape := n.context().escape
	for _, line := range strings.Split(n.source, "\n") {
		if _, ok := isContinuation(line, escape); ok {
			return true
		}
	}
	return false
}

// content returns the source text after the keyword and the whitespace
// character following it, if any. ok is false if there is nothing after the
// keyword.
func (n *node) content() (string, bool) {
	src := strings.TrimLeft(n.text(), " \t")
	end := strings.IndexFunc(src, func(r rune) bool { return r == ' ' || r == '\t' || r == n.context().escape })
	if end < 0 {
		return "", false
	}
	rest := src[end:]
	if rest[0] == ' ' || rest[0] == '\t' {
		rest = rest[1:]
	}
	return rest, true
}

// args returns the instruction's arguments as BuildKit parsed them. For the
// JSON form these are the decoded array elements.
func (n *node) args() []string {
	var args []string
	for a := n.next; a != nil; a = a.next {
		args = append(args, a.Value)
	}
	return args
}

// heredocs returns the argument line of an instruction with heredocs,
// followed by each heredoc body and terminator.
func (n *node) heredocs() string {
	return n.heredocsWithScript(n.Heredocs[0].Content)
}

// heredocsWithScript is heredocs, with script in place of the first body.
func (n *node) heredocsWithScript(script string) string {
	var args []string
	for _, a := range n.args() {
		if a != "" {
			args = append(args, a)
		}
	}
	var b strings.Builder
	b.WriteString(strings.Join(args, " ") + "\n")
	for i, h := range n.Heredocs {
		body := h.Content
		if i == 0 {
			body = script
		}
		b.WriteString(body + h.Name + "\n")
	}
	return b.String()
}

// hasCommentLines reports whether there are comment lines among the
// instruction's continuation lines. Heredoc bodies don't count.
func (n *node) hasCommentLines() bool {
	escape := n.context().escape
	lines := strings.SplitAfter(n.source, "\n")
	for i := 1; i < len(lines); i++ {
		prev := strings.TrimSuffix(lines[i-1], "\n")
		if _, ok := isContinuation(prev, escape); !ok && !isDroppedLine(prev) {
			return false // the instruction ended; heredoc bodies may follow
		}
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "#") {
			return true
		}
	}
	return false
}

// isDroppedLine reports whether BuildKit drops line when it appears inside a
// continuation: a comment or blank line.
func isDroppedLine(line string) bool {
	t := strings.TrimSpace(line)
	return t == "" || strings.HasPrefix(t, "#")
}
