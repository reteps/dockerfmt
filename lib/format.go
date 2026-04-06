package lib

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
	reLineComment         = regexp.MustCompile(`(\n\s*)(#.*)`)
	reCommentContinuation = regexp.MustCompile(`(\\(?:\s*` + "`#.*#`" + `\\){1,}\s*)&&(.[^\\])`)
	reBacktickComment     = regexp.MustCompile(`([ \t]*)(?:&& )?` + "`(#.*)#` " + `\\`)
	reMultipleNewlines    = regexp.MustCompile(`\n{3,}`)
)

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

// extractDirectiveContent returns the text after the directive keyword and any flags.
// Returns ("", false) if there isn't enough content after the keyword.
func extractDirectiveContent(n *ExtendedNode, flagCount int) (string, bool) {
	originalText := n.OriginalMultiline
	if originalText == "" {
		originalText = n.Original
	}
	originalTrimmed := strings.TrimLeft(originalText, " \t")
	parts := reWhitespace.Split(originalTrimmed, 2+flagCount)
	if len(parts) < 2+flagCount {
		return "", false
	}
	return parts[1+flagCount], true
}

// marshalJSONArray formats a string slice as a JSON array with spaces after commas.
func marshalJSONArray(items []string) (string, error) {
	b, err := Marshal(items)
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(string(b), "\",\"", "\", \""), nil
}

var nodeFormatters map[string]func(*ExtendedNode, *Config) string

func init() {
	nodeFormatters = map[string]func(*ExtendedNode, *Config) string{
		command.Add:         formatSpaceSeparated,
		command.Arg:         formatBasic,
		command.Cmd:         formatCmd,
		command.Copy:        formatSpaceSeparated,
		command.Entrypoint:  formatEntrypoint,
		command.Env:         formatEnv,
		command.Expose:      formatSpaceSeparated,
		command.From:        formatSpaceSeparated,
		command.Healthcheck: formatBasic,
		command.Label:       formatBasic,
		command.Maintainer:  formatMaintainer,
		command.Onbuild:     FormatOnBuild,
		command.Run:         formatRun,
		command.Shell:       formatCmd,
		command.StopSignal:  formatBasic,
		command.User:        formatBasic,
		command.Volume:      formatBasic,
		command.Workdir:     formatSpaceSeparated,
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

	// We don't want to process nodes that don't have a start or end line.
	if ast.StartLine == 0 || ast.EndLine == 0 {
		return
	}

	// check if we are on the correct line,
	// otherwise get the comments we are missing
	if df.CurrentLine != ast.StartLine {
		df.Output += FormatComments(df.AllOriginalLines[df.CurrentLine : ast.StartLine-1])
		df.CurrentLine = ast.StartLine
	}

	output, ok := FormatNode(ast, df.Config)
	if ok {
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
			return strings.ToUpper(n.Value) + " " + output
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
		CurrentLine:      0,
		Output:           "",
		AllOriginalLines: fileLines,
	}
	rootNode := BuildExtendedNode(result.AST, fileLines)
	parseState.Config = c
	parseState.processNode(rootNode)

	// After all directives are processed, we need to check if we have any trailing comments to add.
	if parseState.CurrentLine < len(parseState.AllOriginalLines) {
		// Add the rest of the file
		parseState.Output += FormatComments(parseState.AllOriginalLines[parseState.CurrentLine:])
	}

	parseState.Output = strings.TrimRight(parseState.Output, "\n")
	// Ensure the output ends with a newline if requested
	if c.TrailingNewline {
		parseState.Output += "\n"
	}
	return parseState.Output
}

func BuildExtendedNode(n *parser.Node, fileLines []string) *ExtendedNode {
	// Build an extended node from the parser node
	// This is used to add the original multiline string to the node
	// and to add the original line numbers

	if n == nil {
		return nil
	}

	// Create the extended node with the current parser node
	en := &ExtendedNode{
		Node:              n,
		Next:              nil,
		Children:          nil,
		OriginalMultiline: "", // Default to empty string
	}

	// If we have valid start and end lines, construct the multiline representation
	if n.StartLine > 0 && n.EndLine > 0 {
		// Subtract 1 from StartLine because fileLines is 0-indexed while StartLine is 1-indexed
		for i := n.StartLine - 1; i < n.EndLine; i++ {
			en.OriginalMultiline += fileLines[i]
		}
	}

	// Process all children recursively
	if len(n.Children) > 0 {
		childrenNodes := make([]*ExtendedNode, 0, len(n.Children))
		for _, child := range n.Children {
			extChild := BuildExtendedNode(child, fileLines)
			if extChild != nil {
				childrenNodes = append(childrenNodes, extChild)
			}
		}
		// Replace the children with the processed ones
		en.Children = childrenNodes
	}

	// Process the next node recursively
	if n.Next != nil {
		extNext := BuildExtendedNode(n.Next, fileLines)
		if extNext != nil {
			en.Next = extNext
		}
	}

	return en
}

func formatEnv(n *ExtendedNode, c *Config) string {
	// Handle missing arguments safely
	if n.Next == nil {
		return strings.ToUpper(n.Value)
	}

	// Only the legacy format will have an empty 3rd child
	if n.Next.Next.Next.Value == "" {
		return strings.ToUpper(n.Value) + " " + n.Next.Value + "=" + n.Next.Next.Value + "\n"
	}

	// Otherwise, we have a valid env command; fall back to original if parsing fails
	rawContent, ok := extractDirectiveContent(n, 0)
	if !ok {
		return n.OriginalMultiline
	}
	content := StripWhitespace(rawContent, true)
	// Indent all lines with indentSize spaces
	content = strings.Trim(reLeadingSpaces.ReplaceAllString(content, strings.Repeat(" ", int(c.IndentSize))), " ")
	return strings.ToUpper(n.Value) + " " + content
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

// preprocessShellComments replaces inline comments with backtick-quoted placeholders
// so they survive shfmt formatting. Backticks in comment lines are replaced with ×
// to avoid nesting issues.
func preprocessShellComments(content string) string {
	content = StripWhitespace(content, true)

	// Replace backticks in comment lines to avoid nesting issues
	lines := strings.SplitAfter(content, "\n")
	for i := range lines {
		lineTrim := strings.TrimLeft(lines[i], " \t")
		if len(lineTrim) >= 1 && lineTrim[0] == '#' {
			lines[i] = strings.ReplaceAll(lines[i], "`", "×")
		}
	}
	content = strings.Join(lines, "")

	// Wrap comments in backtick subshell placeholders: # comment -> `# comment#`\
	content = reLineComment.ReplaceAllString(content, "$1`$2#`\\")

	// Move && before comment blocks (see tests/in/andissue.dockerfile)
	content = reCommentContinuation.ReplaceAllString(content, "&&$1$2")

	// Re-attach && to comment placeholders when inside a continuation chain
	lines = strings.SplitAfter(content, "\n")
	inContinuation := false
	for i := range lines {
		lineTrim := strings.Trim(lines[i], " \t\\\n")
		nextLine := ""
		isComment := false
		nextLineIsComment := false
		if i+1 < len(lines) {
			nextLine = strings.Trim(lines[i+1], " \t\\\n")
		}
		if len(nextLine) >= 2 && nextLine[:2] == "`#" {
			nextLineIsComment = true
		}
		if len(lineTrim) >= 2 && lineTrim[:2] == "`#" {
			isComment = true
		}

		if isComment && (inContinuation || nextLineIsComment) {
			lines[i] = strings.Replace(lines[i], "#`\\", "#`&&\\", 1)
		}

		if len(lineTrim) >= 2 && !isComment && lineTrim[len(lineTrim)-2:] == "&&" {
			inContinuation = true
		} else if !isComment {
			inContinuation = false
		}
	}

	return strings.Join(lines, "")
}

// postprocessShellComments restores inline comments from backtick placeholders
// after shfmt formatting, and fixes up their indentation.
func postprocessShellComments(content string, c *Config) string {
	// Remove backtick wrappers: `# comment#` \ -> # comment
	content = reBacktickComment.ReplaceAllString(content, "$1$2")

	// Fixup comment indentation
	lines := strings.SplitAfter(content, "\n")
	prevIsComment := false
	prevCommentSpacing := ""
	firstLineIsComment := false
	for i := range lines {
		lineTrim := strings.TrimLeft(lines[i], " \t")
		if len(lineTrim) >= 1 && lineTrim[0] == '#' {
			if i == 0 {
				firstLineIsComment = true
				lines[i] = strings.Repeat(" ", int(c.IndentSize)) + lineTrim
			}
			lineParts := strings.SplitN(lines[i], "#", 2)

			if prevIsComment {
				lines[i] = prevCommentSpacing + "#" + lineParts[1]
			} else {
				prevCommentSpacing = lineParts[0]
			}
			prevIsComment = true
		} else {
			prevIsComment = false
		}
	}
	// TODO: this formatting isn't perfect (see tests/out/run5.dockerfile)
	if firstLineIsComment {
		lines = slices.Insert(lines, 0, "\\\n")
	}
	content = strings.Join(lines, "")
	content = strings.ReplaceAll(content, "×", "`")

	return content
}

func formatRun(n *ExtendedNode, c *Config) string {
	// Get the original RUN command text
	hereDoc := false
	flags := n.Flags

	var content string
	if len(n.Heredocs) >= 1 {
		content = n.Heredocs[0].Content
		hereDoc = true
		// TODO: check if doc.FileDescriptor == 0?
	} else {
		rawContent, _ := extractDirectiveContent(n, len(flags))
		content = rawContent
	}
	// Try to parse as JSON
	var jsonItems []string
	err := json.Unmarshal([]byte(content), &jsonItems)
	if err == nil {
		outStr, err := marshalJSONArray(jsonItems)
		if err != nil {
			panic(err)
		}
		content = outStr + "\n"
	} else {
		content = formatShell(content, hereDoc, c)
		if hereDoc {
			n.Heredocs[0].Content = content
			content, _ = GetHeredoc(n)
		}
	}

	if len(flags) > 0 {
		content = strings.Join(flags, " ") + " " + content
	}

	return strings.ToUpper(n.Value) + " " + content
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
			return strings.ToUpper(n.Value) + "\n"
		}
		value = strings.TrimLeft(rawContent, " \t")
	}
	return IndentFollowingLines(strings.ToUpper(n.Value)+" "+value, c.IndentSize)
}

// Marshal is a UTF-8 friendly marshaler.  Go's json.Marshal is not UTF-8
// friendly because it replaces the valid UTF-8 and JSON characters "&". "<",
// ">" with the "slash u" unicode escaped forms (e.g. \u0026).  It preemptively
// escapes for HTML friendliness.  Where text may include any of these
// characters, json.Marshal should not be used. Playground of Go breaking a
// title: https://play.golang.org/p/o2hiX0c62oN
// Source: https://stackoverflow.com/a/69502657/5684541
func Marshal(i interface{}) ([]byte, error) {
	buffer := &bytes.Buffer{}
	encoder := json.NewEncoder(buffer)
	encoder.SetEscapeHTML(false)
	err := encoder.Encode(i)
	return bytes.TrimRight(buffer.Bytes(), "\n"), err
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

func formatEntrypoint(n *ExtendedNode, c *Config) string {
	return formatCmd(n, c)
}
func formatCmd(n *ExtendedNode, c *Config) string {
	// Determine JSON form from parser attributes
	isJSON, ok := n.Attributes["json"]
	if !ok {
		isJSON = false
	}

	flags := n.Flags
	content, ok2 := extractDirectiveContent(n, len(flags))
	if !ok2 && len(flags) > 0 {
		return strings.ToUpper(n.Value) + "\n"
	}

	// If JSON form (attribute or decodable), format as JSON array with spaces
	var jsonItems []string
	if isJSON || json.Unmarshal([]byte(content), &jsonItems) == nil {
		items := getCmd(n.Next, false)
		if !isJSON && len(items) == 0 {
			items = jsonItems
		}
		outStr, err := marshalJSONArray(items)
		if err != nil {
			return ""
		}
		return strings.ToUpper(n.Value) + " " + outStr + "\n"
	}

	// Otherwise, format as shell command
	shell := formatShell(content, false, c)
	if len(flags) > 0 {
		shell = strings.Join(flags, " ") + " " + shell
	}
	return strings.ToUpper(n.Value) + " " + shell
}

func formatSpaceSeparated(n *ExtendedNode, c *Config) string {
	isJSON, ok := n.Attributes["json"]
	if !ok {
		isJSON = false
	}
	cmd, success := GetHeredoc(n)
	if !success {
		cmd = strings.Join(getCmd(n.Next, isJSON), " ")
		if len(n.Flags) > 0 {
			cmd = strings.Join(n.Flags, " ") + " " + cmd
		}
		cmd += "\n"
	}

	return strings.ToUpper(n.Value) + " " + cmd
}

func formatMaintainer(n *ExtendedNode, c *Config) string {

	// Get text between quotes
	maintainer := strings.Trim(n.Next.Value, "\"")
	return "LABEL org.opencontainers.image.authors=\"" + maintainer + "\"\n"
}

func GetFileLines(fileName string) ([]string, error) {
	// Open the file
	f, err := os.Open(fileName)
	if err != nil {
		return []string{}, err
	}
	defer f.Close()

	// Read the file contents
	b := new(strings.Builder)
	io.Copy(b, f)
	fileLines := strings.SplitAfter(b.String(), "\n")

	return fileLines, nil
}

func StripWhitespace(lines string, rightOnly bool) string {
	linesArray := strings.SplitAfter(lines, "\n")
	var strippedLines string
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
		strippedLines += line
	}
	return strippedLines
}

func FormatComments(lines []string) string {
	// Adds lines to the output, collapsing multiple newlines into a single newline
	// and removing leading / trailing whitespace. We can do this because
	// we are adding comments and we don't care about the formatting.
	missingContent := StripWhitespace(strings.Join(lines, ""), false)
	// Replace multiple newlines with a single newline
	return reMultipleNewlines.ReplaceAllString(missingContent, "\n")
}

func IndentFollowingLines(lines string, indentSize uint) string {
	// Split the input by lines
	allLines := strings.SplitAfter(lines, "\n")

	// If there's only one line or no lines, return the original
	if len(allLines) <= 1 {
		return lines
	}

	// Keep the first line as is
	result := allLines[0]
	// Indent all subsequent lines
	for i := 1; i < len(allLines); i++ {
		if allLines[i] != "" { // Skip empty lines
			// Remove existing indentation and add new indentation
			trimmedLine := strings.TrimLeft(allLines[i], " \t")
			allLines[i] = strings.Repeat(" ", int(indentSize)) + trimmedLine
		}

		// Add to result (with newline except for the last line)
		result += allLines[i]
	}

	return result
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
