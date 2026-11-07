package lib

// Formatting of shell-form RUN/CMD/ENTRYPOINT bodies.
//
// A shell-form body spans several physical lines joined by "\" continuations.
// BuildKit drops two kinds of lines inside a continuation before the shell
// ever sees them: comment lines ("# ...") and blank lines. Users also write
// lines holding only "\" as visual separators. None of these are shell syntax,
// so they can't go through the shell printer.
//
// The pipeline is therefore:
//
//  1. splitShellBody classifies each physical line. Code lines form the shell
//     source handed to mvdan/sh; comment and separator lines are set aside,
//     each anchored to the source offset of the token that follows it.
//  2. printShell parses and prints the source with mvdan/sh. The result must
//     still be one continued line; if the printer would split it into several
//     statements, the body is left as written.
//  3. reinsertLines parses the printed output again, maps every anchor to its
//     position in the output by walking both syntax trees in parallel, and
//     re-inserts the set-aside lines there.
//
// When any step can't be done faithfully, the body is returned unchanged.

import (
	"bytes"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// shellBody is a shell-form body split into shell source and the non-shell
// lines that were taken out of it.
type shellBody struct {
	src     string
	inserts []shellInsert // in source order
}

// shellInsert is a comment or separator line to re-insert into the output.
type shellInsert struct {
	// anchor is the offset in src of the token the line came before, or -1
	// if it came after the last token.
	anchor int
	text   string // "# comment" or "\"
	// deeper records that the user indented the line further than the code
	// line after it, which ties it to what came before (e.g. an argument
	// list) rather than to what follows.
	deeper bool
}

// pendingLine is a comment or separator line waiting for its anchor.
type pendingLine struct {
	text   string
	indent int
}

// indentWidth is the width of the leading whitespace of line, counting a tab
// as four columns. It is only compared between lines of one instruction.
func indentWidth(line string) int {
	w := 0
	for _, r := range line {
		switch r {
		case ' ':
			w++
		case '\t':
			w += 4
		default:
			return w
		}
	}
	return w
}

// formatShellBody formats the shell-form body of a RUN, CMD or ENTRYPOINT.
// content is the text after the directive keyword and any flags; its first
// line is the rest of the directive line. The result ends in a newline when
// formatting succeeded; otherwise content is returned unchanged.
func formatShellBody(content string, c *Config) string {
	body, ok := splitShellBody(content)
	if !ok {
		return content
	}
	out, ok := printShell(body.src, c)
	if !ok {
		return content
	}
	out, ok = reinsertLines(body, out, c)
	if !ok {
		return content
	}
	return out
}

// splitShellBody separates the code lines of content from its comment lines,
// blank lines and empty continuation lines ("\" alone).
func splitShellBody(content string) (shellBody, bool) {
	lines := strings.SplitAfter(content, "\n")
	var src strings.Builder
	var body shellBody
	var pending []pendingLine

	for i, line := range lines {
		if line == "" {
			continue
		}
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "#"):
			// BuildKit drops comment lines inside a continuation.
			pending = append(pending, pendingLine{trimmed, indentWidth(line)})
			continue
		case i > 0 && trimmed == "":
			// BuildKit skips blank lines inside a continuation, so they don't
			// end the instruction; dropping them keeps the command together.
			continue
		case isEmptyContinuation(lines, i):
			pending = append(pending, pendingLine{"\\", indentWidth(line)})
			continue
		}

		// A code line. Trailing whitespace after a final "\" still makes a
		// continuation for BuildKit but would escape a space for the shell.
		code := strings.TrimRight(line, " \t\r\n")
		if strings.HasSuffix(line, "\n") {
			code += "\n"
		}
		start := src.Len() + len(code) - len(strings.TrimLeft(code, " \t"))
		codeIndent := indentWidth(code)
		for _, p := range pending {
			body.inserts = append(body.inserts, shellInsert{anchor: start, text: p.text, deeper: p.indent > codeIndent})
		}
		pending = nil
		src.WriteString(code)
	}
	for _, p := range pending {
		body.inserts = append(body.inserts, shellInsert{anchor: -1, text: p.text})
	}

	body.src = src.String()
	if strings.Trim(body.src, " \t\\\n") == "" {
		return shellBody{}, false
	}
	return body, true
}

// isEmptyContinuation reports whether lines[i] is an empty continuation line
// ("\" alone) that separates words, and so can be set aside and re-inserted.
//
// Line 0 is the tail of the directive line, so a "\" there is the directive's
// own continuation. And when no whitespace surrounds the "\" line, as in
//
//	echo good\
//	\
//	night
//
// the continuations join "good" and "night" into one word, so the line must
// stay part of the code.
func isEmptyContinuation(lines []string, i int) bool {
	if i == 0 || i == len(lines)-1 {
		return false
	}
	line := lines[i]
	if strings.TrimSpace(line) != "\\" || !strings.HasSuffix(line, "\n") {
		return false
	}
	if line[0] == ' ' || line[0] == '\t' {
		return true
	}
	prev := strings.TrimRight(lines[i-1], " \t\n")
	if strings.HasPrefix(strings.TrimLeft(prev, " \t"), "#") {
		return true // comment lines are dropped, so they always end a word
	}
	if before, ok := strings.CutSuffix(prev, "\\"); ok && (strings.HasSuffix(before, " ") || strings.HasSuffix(before, "\t")) {
		return true
	}
	next := lines[i+1]
	return next != "" && (next[0] == ' ' || next[0] == '\t')
}

func shellPrinter(c *Config) *syntax.Printer {
	return syntax.NewPrinter(
		syntax.Minify(false),
		syntax.SingleLine(false),
		syntax.SpaceRedirects(c.SpaceRedirects),
		syntax.Indent(c.IndentSize),
		syntax.BinaryNextLine(true),
	)
}

func parseShell(src string) (*syntax.File, error) {
	return syntax.NewParser(syntax.KeepComments(true)).Parse(strings.NewReader(src), "")
}

// printShell formats src with mvdan/sh. It fails if src doesn't parse, or if
// the output isn't a single continued line: a bare newline would end the
// Dockerfile instruction (the printer puts "a; b" on separate lines, for one).
func printShell(src string, c *Config) (string, bool) {
	f, err := parseShell(src)
	if err != nil {
		return "", false
	}
	var buf bytes.Buffer
	if err := shellPrinter(c).Print(&buf, f); err != nil {
		return "", false
	}
	out := buf.String()
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	for _, l := range lines[:len(lines)-1] {
		if !strings.HasSuffix(l, "\\") {
			return "", false
		}
	}
	return out, true
}

// syntaxPositions lists the offsets of every node of f (plus the operator of
// each binary command) in walk order. Two trees with the same structure give
// lists of the same length whose entries correspond one to one.
func syntaxPositions(f *syntax.File) []int {
	var ps []int
	syntax.Walk(f, func(n syntax.Node) bool {
		if n == nil {
			return true
		}
		ps = append(ps, int(n.Pos().Offset()))
		if b, ok := n.(*syntax.BinaryCmd); ok {
			ps = append(ps, int(b.OpPos.Offset()))
		}
		return true
	})
	return ps
}

// reinsertLines puts the comment and separator lines of body back into out,
// the printed form of body.src.
//
// Each group of lines anchored to the same token goes on its own lines before
// the output line holding that token, with that line's indentation. Before a
// line starting with an operator ("&& cmd"), a group the user indented past
// the next command instead takes the indentation of the line above, so a
// comment written under an argument list stays with the arguments.
func reinsertLines(body shellBody, out string, c *Config) (string, bool) {
	if len(body.inserts) == 0 {
		return out, true
	}
	srcFile, err := parseShell(body.src)
	if err != nil {
		return "", false
	}
	outFile, err := parseShell(out)
	if err != nil {
		return "", false
	}
	srcPos, outPos := syntaxPositions(srcFile), syntaxPositions(outFile)
	if len(srcPos) != len(outPos) {
		return "", false
	}

	// Map anchors to output offsets, grouping lines that share one.
	type group struct {
		offset int
		texts  []string
		deeper bool // of the group's first line
	}
	var groups []group
	end := len(strings.TrimRight(out, "\n"))
	for _, ins := range body.inserts {
		offset := end
		if ins.anchor >= 0 {
			j := indexOf(srcPos, ins.anchor)
			if j < 0 {
				return "", false // anchor isn't a token start (e.g. inside a word)
			}
			offset = outPos[j]
		}
		if n := len(groups); n > 0 && groups[n-1].offset == offset {
			groups[n-1].texts = append(groups[n-1].texts, ins.text)
		} else {
			groups = append(groups, group{offset, []string{ins.text}, ins.deeper})
		}
	}

	baseIndent := strings.Repeat(" ", int(c.IndentSize))
	render := func(texts []string, indent string) string {
		var b strings.Builder
		for _, t := range texts {
			b.WriteString(indent + t + "\n")
		}
		return b.String()
	}

	// Decide where each group goes before changing anything, so offsets and
	// line indentation are read from the printed output as is.
	const (
		atEnd = iota
		atFirstToken
		beforeLine
		midLine
	)
	type placement struct {
		kind      int
		lineStart int
		indent    string
	}
	places := make([]placement, len(groups))
	for gi, g := range groups {
		if g.offset >= end {
			lastStart := strings.LastIndexByte(out[:end], '\n') + 1
			places[gi] = placement{atEnd, lastStart, lineIndent(out, lastStart, baseIndent)}
			continue
		}
		lineStart := strings.LastIndexByte(out[:g.offset], '\n') + 1
		prefix := strings.TrimLeft(out[lineStart:g.offset], " \t")
		switch {
		case lineStart == 0 && prefix == "":
			places[gi] = placement{atFirstToken, 0, baseIndent}
		case prefix == "" || isOperator(prefix):
			indent := lineIndent(out, lineStart, baseIndent)
			if g.deeper && lineStart > 0 && startsWithOperator(out[lineStart:]) {
				// The user indented these lines past the next command, so
				// they belong with the previous one (typically its argument
				// list): indent them like the line above.
				prevStart := strings.LastIndexByte(out[:lineStart-1], '\n') + 1
				indent = lineIndent(out, prevStart, baseIndent)
			}
			places[gi] = placement{beforeLine, lineStart, indent}
		default:
			places[gi] = placement{midLine, lineStart, lineIndent(out, lineStart, baseIndent)}
		}
	}
	// Groups that end up stacked before the same line share the indentation
	// of the first one.
	for gi := 1; gi < len(groups); gi++ {
		if places[gi].kind == beforeLine && places[gi-1].kind == beforeLine && places[gi].lineStart == places[gi-1].lineStart {
			places[gi].indent = places[gi-1].indent
		}
	}

	// Insert from the end so earlier offsets stay valid.
	for gi := len(groups) - 1; gi >= 0; gi-- {
		g, p := groups[gi], places[gi]
		switch p.kind {
		case atEnd:
			// After the last token: append below the last line.
			out = out[:end] + "\n" + render(g.texts, p.indent)
		case atFirstToken:
			// Before the first token: the lines can't share the directive
			// line, so start with a continuation.
			out = "\\\n" + render(g.texts, baseIndent) + baseIndent + strings.TrimLeft(out, " \t")
		case beforeLine:
			out = out[:p.lineStart] + render(g.texts, p.indent) + out[p.lineStart:]
		case midLine:
			// Break the line before the token.
			out = strings.TrimRight(out[:g.offset], " \t") + " \\\n" + render(g.texts, p.indent) + p.indent + out[g.offset:]
		}
	}
	return out, true
}

func indexOf(s []int, v int) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

// lineIndent returns the indentation of the output line starting at
// lineStart. The first line follows the directive keyword, so lines placed
// relative to it use the base indentation.
func lineIndent(out string, lineStart int, base string) string {
	if lineStart == 0 {
		return base
	}
	line := out[lineStart:]
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}

func isOperator(s string) bool {
	switch strings.TrimSpace(s) {
	case "&&", "||", "|", "|&":
		return true
	}
	return false
}

func startsWithOperator(line string) bool {
	line = strings.TrimLeft(line, " \t")
	for _, op := range []string{"&&", "||", "|&", "|"} {
		if strings.HasPrefix(line, op) {
			return true
		}
	}
	return false
}

// formatScript formats the body of a heredoc that the shell runs as a
// script. Comments are real shell comments here. The body is returned
// unchanged if it doesn't parse.
func formatScript(body string, c *Config) string {
	f, err := parseShell(body)
	if err != nil {
		return body
	}
	var buf bytes.Buffer
	if err := shellPrinter(c).Print(&buf, f); err != nil {
		return body
	}
	return buf.String()
}
