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
	// ctx is the file-level state the instruction is interpreted in. nil
	// means the defaults.
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
	return strings.Contains(n.source, "\\\n")
}

// content returns the source text after the keyword and the first flagCount
// flags (with the continuations that follow them). ok is false if there is
// nothing after them.
func (n *node) content(flagCount int) (string, bool) {
	src := strings.TrimLeft(n.text(), " \t")

	if flagCount == 0 {
		i := strings.IndexAny(src, " \t")
		if i < 0 {
			return "", false
		}
		return src[i+1:], true
	}

	// Flags can span lines, so locate the last one in the source.
	lastFlag := n.Flags[flagCount-1]
	i := strings.LastIndex(src, lastFlag)
	if i < 0 {
		return "", false
	}
	rest := src[i+len(lastFlag):]
	for {
		rest = strings.TrimLeft(rest, " \t")
		after, ok := strings.CutPrefix(rest, "\\\n")
		if !ok {
			break
		}
		rest = after
	}
	return rest, rest != ""
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

// heredocs returns the instruction's argument line followed by each heredoc
// body and terminator. If firstBody is set, it replaces the first body. ok is
// false if the instruction has no heredocs.
func (n *node) heredocs(firstBody *string) (string, bool) {
	if len(n.Heredocs) == 0 {
		return "", false
	}
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
		if i == 0 && firstBody != nil {
			body = *firstBody
		}
		b.WriteString(body + h.Name + "\n")
	}
	return b.String(), true
}

// hasEndOfFlags reports whether a "--" follows the flags. BuildKit's parser
// consumes it, but it lets a source path start with "--".
func (n *node) hasEndOfFlags() bool {
	fields := strings.Fields(n.Original)
	i := 1 + len(n.Flags)
	return i < len(fields) && fields[i] == "--"
}
