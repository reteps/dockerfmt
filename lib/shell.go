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
	"slices"
	"strings"
	"unicode"

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
// line is the rest of the directive line. The formatted result ends in a
// newline. ok is false, and content is returned unchanged, if it can't be
// formatted.
func formatShellBody(content string, c *Config) (out string, ok bool) {
	body, ok := splitShellBody(content)
	if !ok {
		return content, false
	}
	if out, ok = printShell(body.src, c); !ok {
		return content, false
	}
	if out, ok = reinsertLines(body, out, c); !ok {
		return content, false
	}
	return out, true
}

// splitShellBody separates the code lines of content from its comment lines,
// blank lines and empty continuation lines ("\" alone).
func splitShellBody(content string) (shellBody, bool) {
	// Blank lines at the end are inside the continuation, which BuildKit
	// drops; keeping them would make the "\" line before them look like a
	// separator rather than the end of the code. The last line gets a line
	// ending, without which a final "\" would be a dangling escape rather than
	// a continuation.
	content = strings.TrimRightFunc(content, unicode.IsSpace) + "\n"
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
		case i > 0 && strings.HasPrefix(trimmed, "#"):
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
		code := strings.TrimRightFunc(line, unicode.IsSpace)
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
	if strings.ContainsAny(body.src, "\v\f\r`") {
		// mvdan/sh reads "\v", "\f" and "\r" as blanks, which the shell
		// doesn't, and prints backquotes as $(...) without undoing the
		// escapes inside them.
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
	if before, ok := strings.CutSuffix(prev, "\\"); ok {
		if strings.HasSuffix(before, " ") || strings.HasSuffix(before, "\t") {
			return true
		}
		if strings.TrimSpace(before) == "" && (i == 1 || isEmptyContinuation(lines, i-1)) {
			// The previous line is the directive tail, which follows the
			// keyword's whitespace, or another separator.
			return true
		}
	}
	next := lines[i+1]
	return next != "" && (next[0] == ' ' || next[0] == '\t')
}

// shellPrinter returns a printer that indents by indent spaces, or with tabs
// if indent is 0.
func shellPrinter(c *Config, indent uint) *syntax.Printer {
	return syntax.NewPrinter(
		syntax.Minify(false),
		syntax.SingleLine(false),
		syntax.SpaceRedirects(c.SpaceRedirects),
		syntax.Indent(indent),
		syntax.BinaryNextLine(true),
	)
}

func parseShell(src string) (*syntax.File, error) {
	return syntax.NewParser(syntax.KeepComments(true)).Parse(strings.NewReader(src), "")
}

// printShell formats src with mvdan/sh. The printer puts statements on
// separate lines (and blocks like if ... fi), but a bare newline would end the
// Dockerfile instruction, so line breaks between statements become ";"
// continuations; see continueStatements. It fails if src doesn't parse, or
// has a construct that needs real line breaks, such as a heredoc.
func printShell(src string, c *Config) (string, bool) {
	f, err := parseShell(src)
	if err != nil || hasInnerComment(f, src) || hasHeredoc(f) {
		return "", false
	}
	var buf bytes.Buffer
	if err := shellPrinter(c, c.shellIndent()).Print(&buf, f); err != nil {
		return "", false
	}
	out := buf.String()
	if strings.HasSuffix(strings.TrimRight(out, "\n"), "\\") {
		// A dangling escape at the end of the command, which the printer
		// follows with a blank line.
		return "", false
	}
	if out, ok := indentFirstCommand(out, c.indent()); ok {
		return continueStatements(src, f, out, c.indent())
	}
	return "", false
}

// indentFirstCommand gives the continuation lines of the first command of an
// operator chain one more level of indentation, as the printer gives those
// of the commands after an operator:
//
//	apt-get update && apt-get install -y \
//	        curl \
//	    && rm -rf /var/lib/apt/lists/*
//
// Without it, the printer puts "curl" level with "&& rm".
func indentFirstCommand(out, indent string) (string, bool) {
	f, err := parseShell(out)
	if err != nil {
		return "", false
	}
	quoted := quotedRanges(f)
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	// A statement's lines are those continued from its first.
	for start := 0; start < len(lines); {
		end := start
		for end < len(lines)-1 && strings.HasSuffix(strings.TrimRight(lines[end], " \t"), "\\") {
			end++
		}
		first := -1 // the first line starting with an operator
		for i := start + 1; i <= end && first < 0; i++ {
			if startsWithOperator(lines[i]) {
				first = i
			}
		}
		for i := start + 1; i < first; i++ {
			if !quoted(lineOffset(lines, i) - 1) {
				lines[i] = indent + lines[i]
			}
		}
		start = end + 1
	}
	return strings.Join(lines, "\n") + "\n", true
}

// lineOffset returns the offset of the start of lines[i] in the text the lines
// were split from.
func lineOffset(lines []string, i int) int {
	n := 0
	for _, l := range lines[:i] {
		n += len(l) + 1
	}
	return n
}

// continueStatements turns out, the printed form of src (parsed as srcFile),
// into a single continued line. A line break the printer put between two
// statements that src has on one line is joined back with "; ". Any other
// line break that doesn't already follow a continuation becomes one, after a
// ";" where the shell needs a separator there: not after "then", "do",
// "else", "{", "(", "in", a case pattern or ";;", nor after "&".
func continueStatements(src string, srcFile *syntax.File, out, indent string) (string, bool) {
	body := strings.TrimSuffix(out, "\n")
	if !hasStatementBreak(body) {
		return out, true
	}
	outFile, err := parseShell(out)
	if err != nil {
		return "", false
	}
	srcPos, outPos := syntaxPositions(srcFile), syntaxPositions(outFile)
	if len(srcPos) != len(outPos) {
		return "", false
	}
	// Token starts in out, in order, with their line in src.
	type token struct{ out, srcLine int }
	tokens := make([]token, len(outPos))
	for i, p := range outPos {
		tokens[i] = token{p, strings.Count(src[:srcPos[i]], "\n")}
	}
	slices.SortFunc(tokens, func(a, b token) int { return a.out - b.out })
	lastBefore := func(at int) (int, bool) { // src line of the last token starting before at
		i, _ := slices.BinarySearchFunc(tokens, at, func(t token, at int) int { return t.out - at })
		if i == 0 {
			return 0, false
		}
		return tokens[i-1].srcLine, true
	}
	startingAt := func(at int) (int, bool) { // src line of the token starting at at
		i, found := slices.BinarySearchFunc(tokens, at, func(t token, at int) int { return t.out - at })
		if !found {
			return 0, false
		}
		return tokens[i].srcLine, true
	}
	openers := blockOpeners(outFile, out)
	quoted := quotedRanges(outFile)

	// The printer indents nested lines relative to the top-level
	// statements, which it puts at the start of a line; after the first,
	// those follow a continuation, so every line gets one more level. A line
	// inside a quoted string is part of the string, and is kept as written.
	printed := strings.Split(body, "\n")
	lines := slices.Clone(printed) // as they will be written
	stmtIndent := indent           // of the line the current statement starts on
	var b strings.Builder
	offset := 0
	for i, line := range printed {
		b.WriteString(lines[i])
		end := offset + len(line)
		offset = end + 1
		if i == len(lines)-1 {
			break
		}
		next := printed[i+1]
		trimmed := strings.TrimRight(line, " \t")
		if strings.HasSuffix(trimmed, "\\") {
			// Already continued: in a && chain, or in a quoted string,
			// whose next line is part of the string.
			switch {
			case quoted(end):
			case strings.HasPrefix(trimIndent(next), ";") && !strings.HasPrefix(trimIndent(next), ";;"):
				// A statement ended on a line of its own, as the official
				// images write it, lines up with the statement.
				lines[i+1] = stmtIndent + trimIndent(next)
			default:
				lines[i+1] = indent + next
			}
			b.WriteString("\n")
			continue
		}
		lines[i+1] = indent + next
		stmtIndent = lines[i+1][:len(lines[i+1])-len(trimIndent(lines[i+1]))]
		closes := strings.HasPrefix(trimIndent(next), ")") // needs no separator before it
		if !closes && !openers[end-(len(line)-len(trimmed))] && !strings.HasSuffix(trimmed, "&") && !strings.HasSuffix(trimmed, ";") {
			b.WriteString(";")
		}
		before, ok1 := lastBefore(end)
		after, ok2 := startingAt(offset + len(next) - len(trimIndent(next)))
		if ok1 && ok2 && before == after {
			lines[i+1] = trimIndent(next)
			b.WriteString(" ")
			continue
		}
		b.WriteString(" \\\n")
	}
	return b.String() + "\n", true
}

// quotedRanges returns a function reporting whether an offset in the printed
// file f is inside a quoted string, and not in code within one, such as a
// command substitution.
func quotedRanges(f *syntax.File) func(int) bool {
	type span struct {
		start, end int
		quoted     bool
	}
	var spans []span
	syntax.Walk(f, func(node syntax.Node) bool {
		switch node.(type) {
		case *syntax.SglQuoted, *syntax.DblQuoted:
			spans = append(spans, span{int(node.Pos().Offset()), int(node.End().Offset()), true})
		case *syntax.CmdSubst, *syntax.ArithmExp, *syntax.ParamExp:
			spans = append(spans, span{int(node.Pos().Offset()), int(node.End().Offset()), false})
		}
		return true
	})
	return func(at int) bool {
		var innermost *span
		for i, s := range spans {
			if at > s.start && at < s.end && (innermost == nil || s.end-s.start < innermost.end-innermost.start) {
				innermost = &spans[i]
			}
		}
		return innermost != nil && innermost.quoted
	}
}

// hasStatementBreak reports whether a line of the printed body ends other than
// in a continuation, where the printer put a line break between statements.
func hasStatementBreak(body string) bool {
	lines := strings.Split(body, "\n")
	for _, l := range lines[:len(lines)-1] {
		if !strings.HasSuffix(strings.TrimRight(l, " \t"), "\\") {
			return true
		}
	}
	return false
}

// blockOpeners returns the offsets in out just after each token that a
// statement can follow without a separator: "then", "do", "else", "{", "(",
// "$(", "in", a case pattern's ")" and ";;".
func blockOpeners(f *syntax.File, out string) map[int]bool {
	ends := make(map[int]bool)
	at := func(p syntax.Pos, n int) {
		if p.IsValid() {
			ends[int(p.Offset())+n] = true
		}
	}
	syntax.Walk(f, func(node syntax.Node) bool {
		switch n := node.(type) {
		case *syntax.IfClause:
			if n.ThenPos.IsValid() {
				at(n.ThenPos, len("then"))
			} else {
				at(n.Position, len("else"))
			}
		case *syntax.WhileClause:
			at(n.DoPos, len("do"))
		case *syntax.ForClause:
			at(n.DoPos, len("do"))
		case *syntax.Block:
			at(n.Lbrace, len("{"))
		case *syntax.Subshell:
			at(n.Lparen, len("("))
		case *syntax.CmdSubst:
			if n.Backquotes {
				at(n.Left, len("`"))
			} else {
				at(n.Left, len("$("))
			}
		case *syntax.CaseClause:
			at(n.In, len("in"))
		case *syntax.CaseItem:
			at(n.OpPos, len(n.Op.String()))
			if len(n.Patterns) > 0 {
				end := int(n.Patterns[len(n.Patterns)-1].End().Offset())
				if i := strings.IndexByte(out[end:], ')'); i >= 0 {
					ends[end+i+1] = true
				}
			}
		}
		return true
	})
	return ends
}

// hasHeredoc reports whether f has a heredoc, whose body needs real line
// breaks.
func hasHeredoc(f *syntax.File) bool {
	found := false
	syntax.Walk(f, func(n syntax.Node) bool {
		if r, ok := n.(*syntax.Redirect); ok && r.Hdoc != nil {
			found = true
		}
		return !found
	})
	return found
}

// hasInnerComment reports whether a shell comment in src is followed by more
// code. BuildKit joins the continued lines before the shell sees them, so such
// a comment runs on to the end of the instruction, while mvdan/sh ends it at
// the line break.
func hasInnerComment(f *syntax.File, src string) bool {
	found := false
	syntax.Walk(f, func(n syntax.Node) bool {
		if cm, ok := n.(*syntax.Comment); ok && strings.TrimSpace(src[cm.End().Offset():]) != "" {
			found = true
		}
		return !found
	})
	return found
}

// syntaxPositions lists the offsets of every node of f, and of the operators
// and reserved words inside them (such as "&&", "then" and "fi"), in walk
// order. Two trees with the same structure give lists of the same length
// whose entries correspond one to one.
func syntaxPositions(f *syntax.File) []int {
	var ps []int
	add := func(positions ...syntax.Pos) {
		for _, p := range positions {
			if p.IsValid() {
				ps = append(ps, int(p.Offset()))
			}
		}
	}
	syntax.Walk(f, func(node syntax.Node) bool {
		if node == nil {
			return true
		}
		add(node.Pos())
		switch n := node.(type) {
		case *syntax.BinaryCmd:
			add(n.OpPos)
		case *syntax.IfClause:
			add(n.ThenPos, n.FiPos)
		case *syntax.WhileClause:
			add(n.DoPos, n.DonePos)
		case *syntax.ForClause:
			add(n.DoPos, n.DonePos)
		case *syntax.Block:
			add(n.Rbrace)
		case *syntax.Subshell:
			add(n.Rparen)
		case *syntax.CaseClause:
			add(n.In, n.Esac)
		case *syntax.CaseItem:
			add(n.OpPos)
		}
		return true
	})
	return ps
}

// reinsertLines puts the comment and separator lines of body back into out,
// the printed form of body.src.
//
// Each group of lines anchored to the same token goes on its own lines before
// the output line holding that token, with that line's indentation. A token in
// the middle of a line first gets a line break before it, and the result is
// printed again so mvdan/sh indents the new line. Before a line starting with
// an operator ("&& cmd"), a group the user indented past the next command
// instead takes the indentation of the line above, so a comment written under
// an argument list stays with the arguments.
func reinsertLines(body shellBody, out string, c *Config) (string, bool) {
	if len(body.inserts) == 0 {
		return out, true
	}
	srcFile, err := parseShell(body.src)
	if err != nil {
		return "", false
	}
	srcPos := syntaxPositions(srcFile)
	tokens := make(map[int]int, len(srcPos)) // source offset -> index in walk order
	for i, p := range srcPos {
		if _, ok := tokens[p]; !ok {
			tokens[p] = i
		}
	}
	nodes := len(srcPos)

	groups, ok := groupInserts(body.inserts, tokens, nodes, out)
	if !ok {
		return "", false
	}
	if breaks := midLineOffsets(groups, out); len(breaks) > 0 {
		for i := len(breaks) - 1; i >= 0; i-- {
			at := breaks[i]
			out = strings.TrimRight(out[:at], " \t") + " \\\n" + out[at:]
		}
		if out, ok = printShell(out, c); !ok {
			return "", false
		}
		if groups, ok = groupInserts(body.inserts, tokens, nodes, out); !ok {
			return "", false
		}
		if len(midLineOffsets(groups, out)) > 0 {
			return "", false
		}
	}
	return placeGroups(groups, out, c), true
}

// insertGroup is a run of inserted lines anchored to the same output offset.
type insertGroup struct {
	offset int
	texts  []string
	deeper bool // of the group's first line
}

// groupInserts maps the anchors of inserts to offsets in out, grouping lines
// that share one. tokens maps source offsets to walk-order indexes, and nodes
// is the number of positions syntaxPositions gives for the source.
func groupInserts(inserts []shellInsert, tokens map[int]int, nodes int, out string) ([]insertGroup, bool) {
	outFile, err := parseShell(out)
	if err != nil {
		return nil, false
	}
	outPos := syntaxPositions(outFile)
	if len(outPos) != nodes {
		return nil, false
	}
	var groups []insertGroup
	end := len(strings.TrimRight(out, "\n"))
	for _, ins := range inserts {
		offset := end
		if ins.anchor >= 0 {
			j, ok := tokens[ins.anchor]
			if !ok {
				return nil, false // anchor isn't a token start (e.g. inside a word)
			}
			offset = outPos[j]
		}
		if n := len(groups); n > 0 && groups[n-1].offset == offset {
			groups[n-1].texts = append(groups[n-1].texts, ins.text)
		} else {
			groups = append(groups, insertGroup{offset, []string{ins.text}, ins.deeper})
		}
	}
	return groups, true
}

// midLineOffsets returns the offsets of the groups whose token is preceded
// on its line by something other than indentation or an operator, in order.
func midLineOffsets(groups []insertGroup, out string) []int {
	end := len(strings.TrimRight(out, "\n"))
	var offsets []int
	for _, g := range groups {
		if g.offset >= end {
			continue
		}
		lineStart := strings.LastIndexByte(out[:g.offset], '\n') + 1
		if prefix := strings.TrimLeft(out[lineStart:g.offset], " \t"); prefix != "" && !isOperator(prefix) {
			offsets = append(offsets, g.offset)
		}
	}
	return offsets
}

// placeGroups inserts groups, none of which is mid-line, into out.
func placeGroups(groups []insertGroup, out string, c *Config) string {
	baseIndent := c.indent()
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
	)
	type placement struct {
		kind      int
		lineStart int
		indent    string
	}
	end := len(strings.TrimRight(out, "\n"))
	places := make([]placement, len(groups))
	for gi, g := range groups {
		lineStart := strings.LastIndexByte(out[:min(g.offset, end)], '\n') + 1
		switch {
		case g.offset >= end:
			// The last line doesn't continue, so these lines come after the
			// instruction, as top-level comments.
			places[gi] = placement{kind: atEnd}
		case lineStart == 0 && strings.TrimSpace(out[:g.offset]) == "":
			places[gi] = placement{atFirstToken, 0, baseIndent}
		default:
			indent := lineIndent(out, lineStart, baseIndent)
			if g.deeper && lineStart > 0 && startsWithOperator(out[lineStart:]) {
				// The user indented these lines past the next command, so
				// they belong with the previous one (typically its argument
				// list): indent them like the line above.
				prevStart := strings.LastIndexByte(out[:lineStart-1], '\n') + 1
				indent = lineIndent(out, prevStart, baseIndent)
			}
			places[gi] = placement{beforeLine, lineStart, indent}
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
		}
	}
	return out
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

// shellOperators are the operators the printer starts continuation lines
// with. "|&" comes before "|", which is a prefix of it.
var shellOperators = []string{"&&", "||", "|&", "|"}

func isOperator(s string) bool {
	return slices.Contains(shellOperators, strings.TrimSpace(s))
}

func startsWithOperator(line string) bool {
	line = strings.TrimLeft(line, " \t")
	return slices.ContainsFunc(shellOperators, func(op string) bool { return strings.HasPrefix(line, op) })
}

// formatScript formats the body of a heredoc that the shell runs as a
// script. Comments are real shell comments here. With tabs, which a "<<-"
// heredoc strips, the script is indented with tabs. The body is returned
// unchanged if it doesn't parse.
func formatScript(body string, c *Config, tabs bool) string {
	if strings.ContainsAny(body, "\v\f`") {
		return body // see splitShellBody
	}
	f, err := parseShell(body)
	if err != nil {
		return body
	}
	var buf bytes.Buffer
	indent := c.shellIndent()
	if tabs {
		indent = 0
	}
	if err := shellPrinter(c, indent).Print(&buf, f); err != nil {
		return body
	}
	return buf.String()
}
