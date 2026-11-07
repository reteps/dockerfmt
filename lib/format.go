package lib

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/google/shlex"
	"github.com/moby/buildkit/frontend/dockerfile/command"
	"github.com/moby/buildkit/frontend/dockerfile/parser"
	"mvdan.cc/sh/v3/syntax"
)

var (
	reWhitespace          = regexp.MustCompile(`[ \t]`)
	reLeadingSpaces       = regexp.MustCompile(`(?m)^ *`)
	reUnescapedSemicolon  = regexp.MustCompile(`[^\\];`)
	reCommentContinuation = regexp.MustCompile(`(\\(?:\s*` + "`#.*#`" + `\\){1,}\s*)&&(.[^\\])`)
	reBacktickComment     = regexp.MustCompile(`([ \t]*)(?:&& )?` + "`(#.*)#` " + `\\`)
	reMultipleNewlines    = regexp.MustCompile(`\n{3,}`)
)

// emptyContinuationMarker stands in for an empty continuation line ("\" alone
// on a line) while shell content goes through shfmt; see preprocessShellComments.
const emptyContinuationMarker = "# dockerfmt:empty-continuation"

type ExtendedNode struct {
	*parser.Node
	Children          []*ExtendedNode
	Next              *ExtendedNode
	OriginalMultiline string
}

type ParseState struct {
	CurrentLine int
	Output      string
	// Needed to pull in comments
	AllOriginalLines []string
	Config           *Config
}

type Config struct {
	IndentSize      uint
	TrailingNewline bool
	SpaceRedirects  bool
}

// hasIgnoreComment reports whether any line in the block is a "# dockerfmt-ignore" comment.
func hasIgnoreComment(lines []string) bool {
	for _, line := range lines {
		if strings.TrimSpace(line) == "# dockerfmt-ignore" {
			return true
		}
	}
	return false
}

// directive returns the uppercased directive name (e.g. "RUN", "COPY").
func (n *ExtendedNode) directive() string {
	return strings.ToUpper(n.Value)
}

// prependFlags prepends flags (e.g. "--network=host") to content if any exist.
// When any flag starts with "--mount", each flag is placed on its own continuation line.
func prependFlags(flags []string, content string, c *Config) string {
	return prependFlagsImpl(flags, content, c, hasMountFlag(flags))
}

// prependFlagsImpl prepends flags to content. When multiline is true, each flag
// is placed on its own continuation line.
func prependFlagsImpl(flags []string, content string, c *Config, multiline bool) string {
	if len(flags) == 0 {
		return content
	}
	if multiline {
		indent := strings.Repeat(" ", int(c.IndentSize))
		var b strings.Builder
		for _, flag := range flags {
			b.WriteString(flag)
			b.WriteString(" \\\n")
			b.WriteString(indent)
		}
		b.WriteString(trimLeadingContinuation(content))
		return b.String()
	}
	return strings.Join(flags, " ") + " " + content
}

// trimLeadingContinuation drops a leading "\" line continuation (and the
// indentation after it) from content. Formatted shell content starts with one
// when its first line is a comment, so it doesn't merge into the directive
// line; after a flag that already ends in " \", it would be redundant.
func trimLeadingContinuation(content string) string {
	if rest, ok := strings.CutPrefix(content, "\\\n"); ok {
		return strings.TrimLeft(rest, " \t")
	}
	return content
}

// flagsRegionItem is one line-level element between a directive keyword and
// its content: a flag, a comment line, or an empty continuation line ("\").
type flagsRegionItem struct {
	text string
	kind flagsRegionKind
}

type flagsRegionKind int

const (
	regionFlag flagsRegionKind = iota
	regionComment
	regionEmptyContinuation
)

// scanFlagsRegion walks the original source of n from the directive keyword
// through its flags (n.Flags, in order) and returns the flags interleaved with
// any comment lines and empty continuation lines ("\" alone on a line) found
// among them, plus empty continuation lines directly after the last flag. The
// buildkit parser drops these, so this recovers them from the source.
//
// ok is false if the flags can't be located in the source, in which case the
// caller should fall back to n.Flags.
func scanFlagsRegion(n *ExtendedNode) (items []flagsRegionItem, ok bool) {
	if len(n.Flags) == 0 {
		// Without flags, everything after the keyword is content.
		return nil, false
	}
	rest := n.OriginalMultiline
	if rest == "" {
		rest = n.Original
	}
	rest = strings.TrimLeft(rest, " \t")
	// Skip the directive keyword.
	if i := strings.IndexAny(rest, " \t\\\n"); i >= 0 {
		rest = rest[i:]
	} else {
		return nil, false
	}

	flags := n.Flags
	atLineStart := false // whether rest begins a new physical line
	for {
		rest = strings.TrimLeft(rest, " \t")
		switch {
		case strings.HasPrefix(rest, "\\\n"):
			if atLineStart {
				items = append(items, flagsRegionItem{"\\", regionEmptyContinuation})
			}
			rest = rest[2:]
			atLineStart = true
			continue
		case atLineStart && strings.HasPrefix(rest, "\n"):
			// Blank line inside a continuation; drop it.
			rest = rest[1:]
			continue
		case len(flags) > 0 && atLineStart && strings.HasPrefix(rest, "#"):
			// Comment line among the flags. Comments after the last flag are
			// part of the content and handled by the shell formatter.
			line, after, _ := strings.Cut(rest, "\n")
			items = append(items, flagsRegionItem{strings.TrimRight(line, " \t"), regionComment})
			rest = after
			continue
		case len(flags) > 0 && strings.HasPrefix(rest, flags[0]):
			items = append(items, flagsRegionItem{flags[0], regionFlag})
			rest = rest[len(flags[0]):]
			flags = flags[1:]
			atLineStart = false
			continue
		}
		break
	}
	if len(flags) > 0 {
		return nil, false
	}
	return items, true
}

// prependFlagsRegion is like prependFlags, but also re-emits the comment lines
// and empty continuation lines found among the flags. Each item goes on its own
// line. It returns ok=false when there is nothing beyond the flags themselves,
// so callers keep their usual flag layout.
func prependFlagsRegion(n *ExtendedNode, content string, c *Config) (string, bool) {
	items, ok := scanFlagsRegion(n)
	if !ok {
		return "", false
	}
	extra := false
	for _, item := range items {
		if item.kind != regionFlag {
			extra = true
			break
		}
	}
	if !extra {
		return "", false
	}

	indent := strings.Repeat(" ", int(c.IndentSize))
	var b strings.Builder
	if items[0].kind != regionFlag {
		// A comment or "\" can't share the directive line.
		b.WriteString("\\\n")
		b.WriteString(indent)
	}
	for _, item := range items {
		b.WriteString(item.text)
		switch item.kind {
		case regionFlag:
			b.WriteString(" \\\n")
		default:
			b.WriteString("\n")
		}
		b.WriteString(indent)
	}
	b.WriteString(trimLeadingContinuation(content))
	return b.String(), true
}

// hasLineContinuation reports whether the node's original source spanned multiple
// lines via "\" continuations.
func hasLineContinuation(n *ExtendedNode) bool {
	return strings.Contains(n.OriginalMultiline, "\\\n")
}

func hasMountFlag(flags []string) bool {
	for _, f := range flags {
		if strings.HasPrefix(f, "--mount") {
			return true
		}
	}
	return false
}

// extractDirectiveContent returns the text after the directive keyword and any flags.
// Returns ("", false) if there isn't enough content after the keyword.
func extractDirectiveContent(n *ExtendedNode, flagCount int) (string, bool) {
	originalText := n.OriginalMultiline
	if originalText == "" {
		originalText = n.Original
	}
	originalTrimmed := strings.TrimLeft(originalText, " \t")

	if flagCount > 0 {
		// When flags span multiple lines with line continuations, a simple
		// whitespace split can't reliably skip them. Instead, find the last
		// flag in the original text and return everything after it.
		lastFlag := n.Flags[flagCount-1]
		idx := strings.LastIndex(originalTrimmed, lastFlag)
		if idx == -1 {
			return "", false
		}
		rest := originalTrimmed[idx+len(lastFlag):]
		// Skip whitespace and line continuations to reach content.
		for {
			rest = strings.TrimLeft(rest, " \t")
			if strings.HasPrefix(rest, "\\\n") {
				rest = rest[2:]
				continue
			}
			break
		}
		if rest == "" {
			return "", false
		}
		return rest, true
	}

	parts := reWhitespace.Split(originalTrimmed, 2)
	if len(parts) < 2 {
		return "", false
	}
	return parts[1], true
}

var nodeFormatters map[string]func(*ExtendedNode, *Config) string

func init() {
	nodeFormatters = map[string]func(*ExtendedNode, *Config) string{
		command.Add:         spaceSeparated(flagsOnOwnLines),
		command.Arg:         formatBasic,
		command.Cmd:         formatCmd,
		command.Copy:        spaceSeparated(flagsOnOwnLines),
		command.Entrypoint:  formatCmd,
		command.Env:         formatEnv,
		command.Expose:      spaceSeparated(argsOnOwnLines),
		command.From:        spaceSeparated(collapseLines),
		command.Healthcheck: formatBasic,
		command.Label:       formatBasic,
		command.Maintainer:  formatMaintainer,
		command.Onbuild:     FormatOnBuild,
		command.Run:         formatRun,
		command.Shell:       formatCmd,
		command.StopSignal:  formatBasic,
		command.User:        formatBasic,
		command.Volume:      formatBasic,
		command.Workdir:     spaceSeparated(collapseLines),
	}
}

func FormatNode(ast *ExtendedNode, c *Config) (string, bool) {
	nodeName := strings.ToLower(ast.Value)
	fmtFunc := nodeFormatters[nodeName]
	if fmtFunc == nil {
		return "", false
	}
	return fmtFunc(ast, c), true
}

func (df *ParseState) processNode(ast *ExtendedNode) {
	if ast.StartLine == 0 || ast.EndLine == 0 {
		return
	}

	// Collect any comments between the current line and this node.
	ignored := false
	if df.CurrentLine != ast.StartLine {
		commentLines := df.AllOriginalLines[df.CurrentLine : ast.StartLine-1]
		df.Output += FormatComments(commentLines)
		ignored = hasIgnoreComment(commentLines)
		df.CurrentLine = ast.StartLine
	}

	if ignored {
		// # dockerfmt-ignore: emit the directive verbatim.
		df.Output += ast.OriginalMultiline
		df.CurrentLine = ast.EndLine
	} else if output, ok := FormatNode(ast, df.Config); ok {
		df.Output += output
		df.CurrentLine = ast.EndLine
	}

	for _, child := range ast.Children {
		df.processNode(child)
	}

	if ast.Node.Next != nil { // Must use .Node.Next (parser.Node), not .Next (ExtendedNode)
		df.processNode(ast.Next)
	}
}

func FormatOnBuild(n *ExtendedNode, c *Config) string {
	if len(n.Node.Next.Children) == 1 {
		output, ok := FormatNode(n.Next.Children[0], c)
		if ok {
			// Inner directives nested under ONBUILD have StartLine=0, so their
			// OriginalMultiline is empty and formatters that fall back to n.Original
			// (which has no trailing newline) produce output without one.
			if !strings.HasSuffix(output, "\n") {
				output += "\n"
			}
			return n.directive() + " " + output
		}
	}

	return n.OriginalMultiline
}

func FormatFileLines(fileLines []string, c *Config) string {
	result, err := parser.Parse(strings.NewReader(strings.Join(fileLines, "")))
	if err != nil {
		log.Printf("%s\n", strings.Join(fileLines, ""))
		log.Fatalf("Error parsing file: %v", err)
	}

	parseState := &ParseState{
		AllOriginalLines: fileLines,
		Config:           c,
	}
	rootNode := BuildExtendedNode(result.AST, fileLines)
	parseState.processNode(rootNode)

	// Append any trailing comments after the last directive.
	if parseState.CurrentLine < len(parseState.AllOriginalLines) {
		parseState.Output += FormatComments(parseState.AllOriginalLines[parseState.CurrentLine:])
	}

	parseState.Output = strings.TrimRight(parseState.Output, "\n")
	if c.TrailingNewline {
		parseState.Output += "\n"
	}
	return parseState.Output
}

// BuildExtendedNode wraps a parser.Node tree, attaching the original multiline
// text from fileLines to each node for use during formatting.
func BuildExtendedNode(n *parser.Node, fileLines []string) *ExtendedNode {
	if n == nil {
		return nil
	}

	en := &ExtendedNode{Node: n}

	// Reconstruct the original text (StartLine is 1-indexed, fileLines is 0-indexed)
	if n.StartLine > 0 && n.EndLine > 0 {
		for i := n.StartLine - 1; i < n.EndLine; i++ {
			en.OriginalMultiline += fileLines[i]
		}
	}

	if len(n.Children) > 0 {
		en.Children = make([]*ExtendedNode, 0, len(n.Children))
		for _, child := range n.Children {
			if extChild := BuildExtendedNode(child, fileLines); extChild != nil {
				en.Children = append(en.Children, extChild)
			}
		}
	}

	if n.Next != nil {
		en.Next = BuildExtendedNode(n.Next, fileLines)
	}

	return en
}

func formatEnv(n *ExtendedNode, c *Config) string {
	// Handle missing arguments safely
	if n.Next == nil {
		return n.directive()
	}

	// Only the legacy format will have an empty 3rd child
	if n.Next.Next.Next.Value == "" {
		return n.directive() + " " + n.Next.Value + "=" + n.Next.Next.Value + "\n"
	}

	// Otherwise, we have a valid env command; fall back to original if parsing fails
	rawContent, ok := extractDirectiveContent(n, 0)
	if !ok {
		return n.OriginalMultiline
	}
	content := StripWhitespace(rawContent, true)
	// Indent all lines with indentSize spaces
	content = strings.Trim(reLeadingSpaces.ReplaceAllString(content, strings.Repeat(" ", int(c.IndentSize))), " ")
	return n.directive() + " " + content
}

func formatShell(content string, hereDoc bool, c *Config) string {
	// TODO: support semicolons in commands
	if reUnescapedSemicolon.MatchString(content) {
		return content
	}
	// Grouped expressions aren't formatted well
	// See: https://github.com/mvdan/sh/issues/1148
	if strings.Contains(content, "{ \\") {
		return content
	}

	if !hereDoc {
		content = preprocessShellComments(content)
	}

	content = formatBash(content, c)

	if !hereDoc {
		content = postprocessShellComments(content, c)
	}

	return content
}

// preprocessShellComments wraps shell comments in backtick placeholders so they
// survive shfmt formatting. The placeholder format is `# text#`\, which shfmt
// treats as a command substitution. Backticks inside comments are backslash-escaped
// (\`) to nest safely inside the outer backtick delimiters (restored in postprocess).
//
// Additionally, when a comment sits between && commands:
//
//	cmd1 \
//	    # comment
//	    && cmd2
//
// the && is moved before the comment block so shfmt sees a continuous chain,
// and placeholders inside chains get && attached so shfmt doesn't break them apart.
func preprocessShellComments(content string) string {
	content = StripWhitespace(content, true)
	lines := strings.SplitAfter(content, "\n")

	// Step 1: wrap comment lines as backtick placeholders.
	// Format: `# comment text#`\  — shfmt treats this as a command substitution.
	// Empty continuation lines ("\" alone) are turned into a marker comment
	// first, so they survive in every position (shfmt drops them before "&&"
	// lines and at the start of the content).
	original := slices.Clone(lines)
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if isEmptyContinuation(original, i) {
			trimmed = emptyContinuationMarker + "\n"
		}
		if len(trimmed) == 0 || trimmed[0] != '#' {
			continue
		}
		ws := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		comment := strings.TrimRight(trimmed, " \t\n")
		// Escape backticks so they nest safely inside the backtick placeholder.
		// Inside backtick command substitutions, \` represents a literal backtick.
		comment = strings.ReplaceAll(comment, "`", "\\`")
		nl := ""
		if line[len(line)-1] == '\n' {
			nl = "\n"
		}
		lines[i] = ws + "`" + comment + "#`\\" + nl
	}

	// Step 2: move && before comment blocks.
	// When we see:  code \<nl> placeholder(s) <nl> && cmd
	// transform to: code &&\<nl> placeholder(s) <nl> cmd
	content = strings.Join(lines, "")
	content = reCommentContinuation.ReplaceAllString(content, "&&$1$2")
	lines = strings.SplitAfter(content, "\n")

	// Step 3: attach && to placeholders inside && chains so shfmt keeps them
	// as part of the continuation.
	inChain := false
	for i, line := range lines {
		trimmed := strings.Trim(line, " \t\\\n")

		if strings.HasPrefix(trimmed, "`#") {
			if inChain {
				lines[i] = strings.Replace(lines[i], "#`\\", "#`&&\\", 1)
			}
			continue
		}

		inChain = strings.HasSuffix(trimmed, "&&")
	}

	return strings.Join(lines, "")
}

// isEmptyContinuation reports whether lines[i] is an empty continuation line
// ("\" alone) that separates words, and so can be replaced by a placeholder.
//
// Line 0 is the tail of the directive line, so a "\" there is the directive's
// own continuation. And when no whitespace surrounds the "\" line, as in
//
//	echo good\
//	\
//	night
//
// the continuations join "good" and "night" into one word, so the line must be
// left alone.
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

// postprocessShellComments restores backtick placeholders to real comments and
// fixes up their indentation to align with the surrounding code.
func postprocessShellComments(content string, c *Config) string {
	// Unwrap placeholders. A placeholder after shfmt looks like:
	//   <ws><optional && >`# text#` \
	// The reBacktickComment regex captures the whitespace and comment text.
	content = reBacktickComment.ReplaceAllString(content, "$1$2")

	// Single pass to fix comment indentation, restore escaped backticks,
	// and detect leading comments.
	lines := strings.SplitAfter(content, "\n")
	indent := strings.Repeat(" ", int(c.IndentSize))
	prevIsComment := false
	prevCommentSpacing := ""
	firstLineIsComment := false

	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if len(trimmed) == 0 || trimmed[0] != '#' {
			prevIsComment = false
			continue
		}

		ws := line[:len(line)-len(trimmed)]
		// Restore backticks that were escaped for the backtick placeholder.
		trimmed = strings.ReplaceAll(trimmed, "\\`", "`")

		if i == 0 {
			// First line being a comment means the directive would merge with it
			// (e.g. "RUN # comment"). We'll insert a continuation before it after the loop.
			firstLineIsComment = true
			lines[i] = indent + trimmed
			prevCommentSpacing = indent
		} else if prevIsComment {
			// Consecutive comments share the indentation of the first in the group.
			lines[i] = prevCommentSpacing + trimmed
		} else {
			prevCommentSpacing = ws
			lines[i] = ws + trimmed
		}
		prevIsComment = true
	}

	if firstLineIsComment {
		lines = slices.Insert(lines, 0, "\\\n")
	}

	// Turn empty-continuation markers back into "\" lines. Each one separates
	// what follows from what precedes it, so indent it like the next real line
	// (shfmt may have indented the placeholder as a command argument instead).
	nextIndent := ""
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		trimmed := strings.TrimLeft(line, " \t")
		ws := line[:len(line)-len(trimmed)]
		body, nl := strings.CutSuffix(trimmed, "\n")
		if body != emptyContinuationMarker {
			if body != "" {
				nextIndent = ws
			}
			continue
		}
		if nextIndent == "" {
			nextIndent = ws
		}
		lines[i] = nextIndent + "\\"
		if nl {
			lines[i] += "\n"
		}
	}

	return strings.Join(lines, "")
}

func formatRun(n *ExtendedNode, c *Config) string {
	hereDoc := false
	flags := n.Flags

	var content string
	if len(n.Heredocs) >= 1 {
		content = n.Heredocs[0].Content
		hereDoc = true
	} else {
		content, _ = extractDirectiveContent(n, len(flags))
	}

	if jsonItems, ok := unmarshalJSONStringArray(content); ok {
		content = marshalJSONStringArray(jsonItems) + "\n"
	} else {
		content = formatShell(content, hereDoc, c)
		if hereDoc {
			n.Heredocs[0].Content = content
			content, _ = GetHeredoc(n)
		}
	}

	if out, ok := prependFlagsRegion(n, content, c); ok {
		return n.directive() + " " + out
	}
	return n.directive() + " " + prependFlags(flags, content, c)
}

func GetHeredoc(n *ExtendedNode) (string, bool) {
	if len(n.Heredocs) == 0 {
		return "", false
	}

	args := []string{}
	cur := n.Next
	for cur != nil {
		if cur.Value != "" {
			args = append(args, cur.Value)
		}
		cur = cur.Next
	}
	content := strings.Join(args, " ") + "\n" + n.Heredocs[0].Content + n.Heredocs[0].Name + "\n"
	return content, true
}
func formatBasic(n *ExtendedNode, c *Config) string {
	value, success := GetHeredoc(n)
	if !success {
		rawContent, ok := extractDirectiveContent(n, 0)
		if !ok {
			return n.directive() + "\n"
		}
		value = strings.TrimLeft(rawContent, " \t")
	}
	return IndentFollowingLines(n.directive()+" "+value, c.IndentSize)
}

func getCmd(n *ExtendedNode, shouldSplitNode bool) []string {
	cmd := []string{}
	for node := n; node != nil; node = node.Next {
		// Split value by whitespace
		rawValue := strings.Trim(node.Value, " \t")
		if len(node.Flags) > 0 {
			cmd = append(cmd, node.Flags...)
		}
		if shouldSplitNode {
			parts, err := shlex.Split(rawValue)
			if err != nil {
				log.Fatalf("Error splitting: %s\n", node.Value)
			}
			cmd = append(cmd, parts...)
		} else {
			cmd = append(cmd, rawValue)
		}
	}
	return cmd
}

func formatCmd(n *ExtendedNode, c *Config) string {
	isJSON := n.Attributes["json"]

	flags := n.Flags
	content, ok := extractDirectiveContent(n, len(flags))
	if !ok && len(flags) > 0 {
		return n.directive() + "\n"
	}

	// If JSON form (attribute or decodable), format as JSON array with spaces
	jsonItems, jsonOK := unmarshalJSONStringArray(content)
	if isJSON || jsonOK {
		items := getCmd(n.Next, false)
		if !isJSON && len(items) == 0 {
			items = jsonItems
		}
		return n.directive() + " " + marshalJSONStringArray(items) + "\n"
	}

	// Otherwise, format as shell command
	shell := formatShell(content, false, c)
	return n.directive() + " " + prependFlags(flags, shell, c)
}

// multilineMode controls how a space-separated directive that the author wrote
// across multiple "\" continuation lines is re-emitted. The modes differ because
// the natural break point differs per directive: COPY/ADD break before each flag
// but keep "<src> <dst>" together, while EXPOSE breaks before each port.
type multilineMode int

const (
	// collapseLines always joins everything onto a single line (FROM, WORKDIR).
	collapseLines multilineMode = iota
	// flagsOnOwnLines keeps each flag on its own continuation line (COPY, ADD).
	flagsOnOwnLines
	// argsOnOwnLines keeps each argument on its own continuation line (EXPOSE).
	argsOnOwnLines
)

// spaceSeparated returns a formatter for directives whose payload is a list of
// flags and space-separated arguments (COPY, ADD, EXPOSE, FROM, WORKDIR). The
// mode selects how multiline source is preserved; see multilineMode.
func spaceSeparated(mode multilineMode) func(*ExtendedNode, *Config) string {
	return func(n *ExtendedNode, c *Config) string {
		isJSON := n.Attributes["json"]
		cmd, success := GetHeredoc(n)
		if !success {
			argSep := " "
			if mode == argsOnOwnLines && hasLineContinuation(n) {
				argSep = " \\\n" + strings.Repeat(" ", int(c.IndentSize))
			}
			content := strings.Join(getCmd(n.Next, isJSON), argSep)
			flagsMultiline := mode == flagsOnOwnLines && (hasLineContinuation(n) || hasMountFlag(n.Flags))
			cmd = prependFlagsImpl(n.Flags, content, c, flagsMultiline) + "\n"
		}

		return n.directive() + " " + cmd
	}
}

func formatMaintainer(n *ExtendedNode, c *Config) string {
	maintainer := strings.Trim(n.Next.Value, "\"")
	return "LABEL org.opencontainers.image.authors=\"" + maintainer + "\"\n"
}

func GetFileLines(fileName string) ([]string, error) {
	b, err := os.ReadFile(fileName)
	if err != nil {
		return nil, err
	}
	return strings.SplitAfter(string(b), "\n"), nil
}

func StripWhitespace(lines string, rightOnly bool) string {
	linesArray := strings.SplitAfter(lines, "\n")
	var b strings.Builder
	b.Grow(len(lines))
	for _, line := range linesArray {
		hadNewline := len(line) > 0 && line[len(line)-1] == '\n'
		if rightOnly {
			line = strings.TrimRight(line, " \t\n")
		} else {
			line = strings.Trim(line, " \t\n")
		}
		if hadNewline {
			line += "\n"
		}
		b.WriteString(line)
	}
	return b.String()
}

// FormatComments strips whitespace and collapses 3+ consecutive newlines into one.
func FormatComments(lines []string) string {
	content := StripWhitespace(strings.Join(lines, ""), false)
	return reMultipleNewlines.ReplaceAllString(content, "\n")
}

// IndentFollowingLines re-indents all lines after the first to indentSize spaces.
func IndentFollowingLines(lines string, indentSize uint) string {
	allLines := strings.SplitAfter(lines, "\n")
	if len(allLines) <= 1 {
		return lines
	}

	indent := strings.Repeat(" ", int(indentSize))
	var b strings.Builder
	b.Grow(len(lines) + len(indent)*len(allLines))
	b.WriteString(allLines[0])
	for _, line := range allLines[1:] {
		if line != "" {
			line = indent + strings.TrimLeft(line, " \t")
		}
		b.WriteString(line)
	}
	return b.String()
}

func formatBash(s string, c *Config) string {
	r := strings.NewReader(s)
	f, err := syntax.NewParser(syntax.KeepComments(true)).Parse(r, "")
	if err != nil {
		fmt.Printf("Error parsing: %s\n", s)
		panic(err)
	}
	buf := new(bytes.Buffer)
	syntax.NewPrinter(
		syntax.Minify(false),
		syntax.SingleLine(false),
		syntax.SpaceRedirects(c.SpaceRedirects),
		syntax.Indent(c.IndentSize),
		syntax.BinaryNextLine(true),
	).Print(buf, f)
	return buf.String()
}
