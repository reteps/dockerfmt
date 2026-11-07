package lib

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/command"
	"github.com/moby/buildkit/frontend/dockerfile/parser"
	"mvdan.cc/sh/v3/syntax"
)

var (
	reWhitespace       = regexp.MustCompile(`[ \t]`)
	reMultipleNewlines = regexp.MustCompile(`\n{3,}`)
)

type ExtendedNode struct {
	*parser.Node
	Children          []*ExtendedNode
	Next              *ExtendedNode
	OriginalMultiline string
	// ctx is the file-level state the instruction is interpreted in; nil
	// means the defaults (backslash escape, POSIX shell).
	ctx *instrContext
}

func (n *ExtendedNode) context() instrContext {
	if n.ctx == nil {
		return defaultContext()
	}
	return *n.ctx
}

type ParseState struct {
	CurrentLine int
	Output      string
	// Needed to pull in comments
	AllOriginalLines []string
	Config           *Config
	// ctx tracks file-level state (escape character, current SHELL).
	ctx instrContext
}

type Config struct {
	IndentSize      uint
	TrailingNewline bool
	SpaceRedirects  bool
	// OnFallback, if set, is called when an instruction is left unformatted
	// because formatting it would have changed its meaning (or the formatter
	// failed). line is the instruction's 1-based start line.
	OnFallback func(line int, reason string)
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
	} else if output, ok := df.formatChecked(ast); ok {
		df.Output += output
		df.CurrentLine = ast.EndLine
	} else if ast.Value != "" { // the root node has no keyword
		// No formatter for this instruction (e.g. a misspelled keyword,
		// which BuildKit only rejects later): keep it exactly as written.
		df.Output += ast.OriginalMultiline
		df.CurrentLine = ast.EndLine
	}
	df.ctx.update(ast.Node)

	for _, child := range ast.Children {
		df.processNode(child)
	}

	if ast.Node.Next != nil { // Must use .Node.Next (parser.Node), not .Next (ExtendedNode)
		df.processNode(ast.Next)
	}
}

// formatChecked formats one instruction and verifies that BuildKit reads the
// result the same way as the original. If it doesn't, or the formatter fails,
// the original text is returned unchanged.
func (df *ParseState) formatChecked(ast *ExtendedNode) (string, bool) {
	ctx := df.ctx
	ast.ctx = &ctx
	output, ok, reason := formatRecovering(ast, df.Config)
	if !ok {
		return "", false
	}
	if reason == "" && output != ast.OriginalMultiline {
		if same, why := equivalent(ast.Node, output, ctx); !same {
			reason = why
		}
	}
	if reason != "" {
		if df.Config.OnFallback != nil {
			df.Config.OnFallback(ast.StartLine, reason)
		}
		return ast.OriginalMultiline, true
	}
	return output, true
}

// formatRecovering runs FormatNode, turning a panic into a fallback reason.
func formatRecovering(ast *ExtendedNode, c *Config) (output string, ok bool, reason string) {
	defer func() {
		if r := recover(); r != nil {
			output, ok, reason = "", true, fmt.Sprintf("formatter error: %v", r)
		}
	}()
	output, ok = FormatNode(ast, c)
	return output, ok, ""
}

func FormatOnBuild(n *ExtendedNode, c *Config) string {
	if len(n.Node.Next.Children) == 1 {
		// The trigger runs with the shell this image ends up with, which is
		// the one in effect here.
		inner := n.Next.Children[0]
		inner.ctx = n.ctx
		output, ok := FormatNode(inner, c)
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

// formatLines formats a Dockerfile given as "\n"-terminated lines. Line
// endings must already be normalized; see formatContent.
func formatLines(fileLines []string, c *Config) (string, error) {
	result, err := parser.Parse(strings.NewReader(strings.Join(fileLines, "")))
	if err != nil {
		return "", parseError(err)
	}

	parseState := &ParseState{
		AllOriginalLines: fileLines,
		Config:           c,
		ctx:              defaultContext(),
	}
	if result.EscapeToken != 0 {
		parseState.ctx.escape = result.EscapeToken
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
	return parseState.Output, nil
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
		return formatLegacyEnv(n.directive(), n.Next.Value, n.Next.Next.Value)
	}

	// Otherwise, we have a valid env command; fall back to original if parsing fails
	rawContent, ok := extractDirectiveContent(n, 0)
	if !ok {
		return n.OriginalMultiline
	}
	escape := continuationEscape(n.OriginalMultiline)
	return n.directive() + " " + reindentContinuations(rawContent, c.IndentSize, escape, true)
}

// formatLegacyEnv converts the legacy `ENV key value` form to `key=value`.
// In the legacy form everything after the key is the value, so a value with
// whitespace must be quoted in the new form or it would be read as more pairs.
func formatLegacyEnv(directive, key, value string) string {
	if !strings.ContainsAny(value, " \t") || isSingleQuoted(value) {
		return directive + " " + key + "=" + value + "\n"
	}
	// Quotes or backslashes inside a value with spaces can't be re-quoted
	// without changing what it means, so keep the legacy form (still valid).
	if strings.ContainsAny(value, "\"'\\") {
		return directive + " " + key + " " + value + "\n"
	}
	return directive + " " + key + "=\"" + value + "\"\n"
}

// isSingleQuoted reports whether s is one token wrapped in matching quotes.
func isSingleQuoted(s string) bool {
	if len(s) < 2 || (s[0] != '"' && s[0] != '\'') || s[len(s)-1] != s[0] {
		return false
	}
	return !strings.ContainsAny(s[1:len(s)-1], string(s[0])+"\\")
}

func formatRun(n *ExtendedNode, c *Config) string {
	flags := n.Flags
	ctx := n.context()

	var content string
	switch {
	case len(n.Heredocs) > 0:
		// Only a heredoc the shell runs as a script ("RUN <<EOF") is shell;
		// a heredoc fed to another program is data and stays as written.
		var script *string
		if isShellScriptHeredoc(n.Node, ctx) && ctx.escape == '\\' {
			formatted := formatHeredocScript(n.Heredocs[0].Content, c)
			script = &formatted
		}
		content, _ = getHeredoc(n, script)
	case n.Attributes["json"]:
		content = marshalJSONStringArray(jsonArgs(n.Next)) + "\n"
	default:
		content, _ = extractDirectiveContent(n, len(flags))
		if ctx.formatsShell() {
			content = formatShellBody(content, c)
		}
	}

	if out, ok := prependFlagsRegion(n, content, c); ok {
		return n.directive() + " " + out
	}
	return n.directive() + " " + prependFlags(flags, content, c)
}

// GetHeredoc returns the directive's arguments followed by every heredoc body
// and terminator, as written.
func GetHeredoc(n *ExtendedNode) (string, bool) {
	return getHeredoc(n, nil)
}

// getHeredoc is GetHeredoc, with the first body replaced by firstBody if set.
func getHeredoc(n *ExtendedNode, firstBody *string) (string, bool) {
	if len(n.Heredocs) == 0 {
		return "", false
	}

	args := []string{}
	for cur := n.Next; cur != nil; cur = cur.Next {
		if cur.Value != "" {
			args = append(args, cur.Value)
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

func formatBasic(n *ExtendedNode, c *Config) string {
	if value, ok := GetHeredoc(n); ok {
		return IndentFollowingLines(n.directive()+" "+value, c.IndentSize)
	}
	rawContent, ok := extractDirectiveContent(n, 0)
	if !ok {
		return n.directive() + "\n"
	}
	escape := continuationEscape(n.OriginalMultiline)
	return n.directive() + " " + reindentContinuations(rawContent, c.IndentSize, escape, false)
}

// shellFormArgs returns the arguments of a space-separated directive.
func shellFormArgs(n *ExtendedNode) []string {
	args := []string{}
	for node := n; node != nil; node = node.Next {
		args = append(args, node.Flags...)
		args = append(args, strings.Trim(node.Value, " \t"))
	}
	return args
}

func formatCmd(n *ExtendedNode, c *Config) string {
	flags := n.Flags
	if n.Attributes["json"] {
		// BuildKit decides what is JSON: an argument list it can't decode
		// (like ["a",]) is shell form, and must stay that way.
		return n.directive() + " " + prependFlags(flags, marshalJSONStringArray(jsonArgs(n.Next))+"\n", c)
	}
	content, ok := extractDirectiveContent(n, len(flags))
	if !ok && len(flags) > 0 {
		return n.directive() + "\n"
	}
	if n.context().formatsShell() {
		content = formatShellBody(content, c)
	}
	return n.directive() + " " + prependFlags(flags, content, c)
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
		if success {
			// The flags go on the heredoc's header line.
			cmd = prependFlagsImpl(n.Flags, cmd, c, false)
		} else {
			argSep := " "
			if mode == argsOnOwnLines && hasLineContinuation(n) {
				argSep = " \\\n" + strings.Repeat(" ", int(c.IndentSize))
			}
			var content string
			switch args := jsonArgs(n.Next); {
			case isJSON && !safeAsShellWords(args):
				// The JSON form is the only way to pass these arguments.
				content = marshalJSONStringArray(args)
			case isJSON:
				// BuildKit processes each argument the same way in both
				// forms; only the splitting on whitespace differs.
				content = strings.Join(args, argSep)
			default:
				content = strings.Join(shellFormArgs(n.Next), argSep)
			}
			flags := n.Flags
			if hasEndOfFlags(n) {
				// "--" ends the flags, so a source may start with "--".
				flags = append(slices.Clone(flags), "--")
			}
			flagsMultiline := mode == flagsOnOwnLines && (hasLineContinuation(n) || hasMountFlag(n.Flags))
			cmd = prependFlagsImpl(flags, content, c, flagsMultiline) + "\n"
		}

		return n.directive() + " " + cmd
	}
}

// hasEndOfFlags reports whether the instruction has a "--" right after its
// flags (BuildKit's parser consumes it).
func hasEndOfFlags(n *ExtendedNode) bool {
	fields := strings.Fields(n.Original)
	i := 1 + len(n.Flags)
	return i < len(fields) && fields[i] == "--"
}

// jsonArgs returns the decoded arguments of a JSON-form directive.
func jsonArgs(n *ExtendedNode) []string {
	var args []string
	for node := n; node != nil; node = node.Next {
		args = append(args, node.Value)
	}
	return args
}

// safeAsShellWords reports whether JSON-form arguments can be written in the
// space-separated form without changing them. BuildKit splits that form on
// whitespace, so an argument containing whitespace, or an empty one, needs
// the JSON form.
func safeAsShellWords(args []string) bool {
	for _, a := range args {
		if a == "" || strings.ContainsAny(a, " \t\n\r\v\f") {
			return false
		}
	}
	return true
}

func formatMaintainer(n *ExtendedNode, c *Config) string {
	maintainer := unwrapMaintainerQuotes(n.Next.Value)
	// MAINTAINER takes the raw rest of the line, so escape what would
	// break the double-quoted LABEL value. LABEL also expands variables while
	// MAINTAINER does not, so "$" must be escaped too.
	maintainer = strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`).Replace(maintainer)
	return "LABEL org.opencontainers.image.authors=\"" + maintainer + "\"\n"
}

// unwrapMaintainerQuotes drops one pair of double quotes wrapping a
// MAINTAINER value ("Jane Doe"), which already reads as quoted.
func unwrapMaintainerQuotes(maintainer string) string {
	if len(maintainer) >= 2 && maintainer[0] == '"' && maintainer[len(maintainer)-1] == '"' &&
		!strings.Contains(maintainer[1:len(maintainer)-1], `"`) {
		return maintainer[1 : len(maintainer)-1]
	}
	return maintainer
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
		panic(fmt.Errorf("parsing shell: %w", err))
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
