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
	MultilineMounts bool // When true, each --mount flag goes on its own line
}

// extractFlagsFormatted extracts flags from original text preserving line continuations.
// Returns: formatted flags string, content after flags, and whether multiline was detected.
func extractFlagsFormatted(original string, flags []string, indentSize uint) (string, string, bool) {
	if len(flags) == 0 || original == "" {
		return "", "", false
	}

	// Find the directive (RUN, COPY, etc.) and skip past it
	trimmed := strings.TrimLeft(original, " \t")
	directiveEnd := strings.IndexAny(trimmed, " \t")
	if directiveEnd == -1 {
		return "", "", false
	}
	afterDirective := trimmed[directiveEnd:]

	// Check if original has line continuations between/after flags
	hasMultiline := false
	remaining := afterDirective

	for _, flag := range flags {
		idx := strings.Index(remaining, flag)
		if idx == -1 {
			return "", "", false // Flag not found, fall back to default
		}
		remaining = remaining[idx+len(flag):]

		// Check if followed by continuation (backslash + newline)
		spacesTrimmed := strings.TrimLeft(remaining, " \t")
		if len(spacesTrimmed) > 0 && spacesTrimmed[0] == '\\' {
			afterBackslash := strings.TrimLeft(spacesTrimmed[1:], " \t")
			if len(afterBackslash) > 0 && afterBackslash[0] == '\n' {
				hasMultiline = true
			}
		}
	}

	if !hasMultiline {
		return "", "", false // No multiline formatting, use default behavior
	}

	// Build formatted flags string preserving line breaks
	indent := strings.Repeat(" ", int(indentSize))
	var result strings.Builder
	remaining = afterDirective
	prevFlagHadContinuation := false

	for i, flag := range flags {
		idx := strings.Index(remaining, flag)
		remaining = remaining[idx+len(flag):]

		if i > 0 && prevFlagHadContinuation {
			result.WriteString(indent)
		}
		result.WriteString(flag)

		// Check if this flag is followed by a continuation
		spacesTrimmed := strings.TrimLeft(remaining, " \t")
		hasContinuation := false
		if len(spacesTrimmed) > 0 && spacesTrimmed[0] == '\\' {
			afterBackslash := strings.TrimLeft(spacesTrimmed[1:], " \t")
			if len(afterBackslash) > 0 && afterBackslash[0] == '\n' {
				hasContinuation = true
				// Skip past the backslash and newline
				remaining = afterBackslash[1:]
			}
		}

		if hasContinuation {
			result.WriteString(" \\\n")
			prevFlagHadContinuation = true
		} else if i < len(flags)-1 {
			// Space between flags on same line
			result.WriteString(" ")
			prevFlagHadContinuation = false
		}
	}

	// Extract content after all flags, skipping any leading whitespace
	content := strings.TrimLeft(remaining, " \t")

	// Add proper separator before content
	if prevFlagHadContinuation {
		// Add indent after continuation
		result.WriteString(indent)
	} else {
		// Just a space if no continuation
		result.WriteString(" ")
	}

	return result.String(), content, true
}

// formatFlagsWithMountSplit formats flags, putting each --mount flag on its own line.
// Returns the formatted flags string with trailing space/continuation ready for content.
func formatFlagsWithMountSplit(flags []string, c *Config) string {
	if len(flags) == 0 {
		return ""
	}

	// Check if there are any --mount flags
	hasMountFlags := false
	for _, flag := range flags {
		if strings.HasPrefix(flag, "--mount") {
			hasMountFlags = true
			break
		}
	}

	if !hasMountFlags {
		// No mount flags, just join with spaces
		return strings.Join(flags, " ") + " "
	}

	// Format with each --mount on its own line
	indent := strings.Repeat(" ", int(c.IndentSize))
	var result strings.Builder

	for i, flag := range flags {
		if i > 0 {
			// Previous flag ended with continuation, add indent
			result.WriteString(indent)
		}
		result.WriteString(flag)

		// Add continuation after each flag (including non-mount flags before a mount)
		if i < len(flags)-1 {
			result.WriteString(" \\\n")
		} else {
			// Last flag - add continuation before content
			result.WriteString(" \\\n")
			result.WriteString(indent)
		}
	}

	return result.String()
}

func FormatNode(ast *ExtendedNode, c *Config) (string, bool) {
	nodeName := strings.ToLower(ast.Node.Value)
	dispatch := map[string]func(*ExtendedNode, *Config) string{
		command.Add:         formatSpaceSeparated,
		command.Arg:         formatBasic,
		command.Cmd:         formatCmd,
		command.Copy:        formatSpaceSeparated,
		command.Entrypoint:  formatEntrypoint,
		command.Env:         formatEnv,
		command.Expose:      formatSpaceSeparated,
		command.From:        formatSpaceSeparated,
		command.Healthcheck: formatBasic,
		command.Label:       formatBasic, // TODO: order labels?
		command.Maintainer:  formatMaintainer,
		command.Onbuild:     FormatOnBuild,
		command.Run:         formatRun,
		command.Shell:       formatCmd,
		command.StopSignal:  formatBasic,
		command.User:        formatBasic,
		command.Volume:      formatBasic,
		command.Workdir:     formatSpaceSeparated,
	}

	fmtFunc := dispatch[nodeName]
	if fmtFunc == nil {
		return "", false
		// log.Fatalf("Unknown command: %s %s\n", nodeName, ast.OriginalMultiline)
	}
	return fmtFunc(ast, c), true
}

func (df *ParseState) processNode(ast *ExtendedNode) {
	// We don't want to process nodes that don't have a start or end line.
	if ast.Node.StartLine == 0 || ast.Node.EndLine == 0 {
		return
	}

	// check if we are on the correct line,
	// otherwise get the comments we are missing
	if df.CurrentLine != ast.StartLine {
		df.Output += FormatComments(df.AllOriginalLines[df.CurrentLine : ast.StartLine-1])
		df.CurrentLine = ast.StartLine
	}
	// if df.Output != "" {
	// 	// If the previous line isn't a comment or newline, add a newline
	// 	lastTwoChars := df.Output[len(df.Output)-2 : len(df.Output)]
	// 	lastNonTrailingNewline := strings.LastIndex(strings.TrimRight(df.Output, "\n"), "\n")
	// 	if lastTwoChars != "\n\n" && df.Output[lastNonTrailingNewline+1] != '#' {
	// 		df.Output += "\n"
	// 	}
	// }

	output, ok := FormatNode(ast, df.Config)
	if ok {
		df.Output += output
		df.CurrentLine = ast.EndLine
	}
	// fmt.Printf("CurrentLine: %d, %d\n", df.CurrentLine, ast.EndLine)
	// fmt.Printf("Unknown command: %s %s\n", nodeName, ast.OriginalMultiline)

	for _, child := range ast.Children {
		df.processNode(child)
	}

	// fmt.Printf("CurrentLine2: %d, %d\n", df.CurrentLine, ast.EndLine)

	if ast.Node.Next != nil {
		df.processNode(ast.Next)
	}
}

func FormatOnBuild(n *ExtendedNode, c *Config) string {
	if len(n.Node.Next.Children) == 1 {
		// fmt.Printf("Onbuild: %s\n", n.Node.Next.Children[0].Value)
		output, ok := FormatNode(n.Next.Children[0], c)
		if ok {
			return strings.ToUpper(n.Node.Value) + " " + output
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
		return strings.ToUpper(n.Node.Value)
	}

	// Only the legacy format will have an empty 3rd child
	if n.Next.Next.Next.Value == "" {
		return strings.ToUpper(n.Node.Value) + " " + n.Next.Node.Value + "=" + n.Next.Next.Node.Value + "\n"
	}

	// Otherwise, we have a valid env command; fall back to original if parsing fails
	originalTrimmed := strings.TrimLeft(n.OriginalMultiline, " \t")
	parts := regexp.MustCompile("[ \t]").Split(originalTrimmed, 2)
	if len(parts) < 2 {
		return n.OriginalMultiline
	}
	content := StripWhitespace(parts[1], true)
	// Indent all lines with indentSize spaces
	re := regexp.MustCompile("(?m)^ *")
	content = strings.Trim(re.ReplaceAllString(content, strings.Repeat(" ", int(c.IndentSize))), " ")
	return strings.ToUpper(n.Value) + " " + content
}

func formatShell(content string, hereDoc bool, c *Config) string {
	// Semicolons require special handling so we don't break the command
	// TODO: support semicolons in commands

	// check for [^\;]
	if regexp.MustCompile(`[^\\];`).MatchString(content) {
		return content
	}
	// Grouped expressions aren't formatted well
	// See: https://github.com/mvdan/sh/issues/1148
	if strings.Contains(content, "{ \\") {
		return content
	}

	if !hereDoc {
		// Here lies some cursed magic. Be careful.

		// Replace comments with a subshell evaluation -- they won't be run so we can do this.
		content = StripWhitespace(content, true)
		lineComment := regexp.MustCompile(`(\n\s*)(#.*)`)
		lines := strings.SplitAfter(content, "\n")
		for i := range lines {
			lineTrim := strings.TrimLeft(lines[i], " \t")
			if len(lineTrim) >= 1 && lineTrim[0] == '#' {
				lines[i] = strings.ReplaceAll(lines[i], "`", "×")
			}
		}
		content = strings.Join(lines, "")

		content = lineComment.ReplaceAllString(content, "$1`$2#`\\")
		// fmt.Printf("Content-1: %s\n", content)

		/*
			```
			foo \
			`#comment#`\
			&& bar
			```

			```
			foo && \
			`#comment#` \
			bar
			```
		*/

		// The (.[^\\]) prevents an edge case with '&& \'. See tests/in/andissue.dockerfile
		commentContinuation := regexp.MustCompile(`(\\(?:\s*` + "`#.*#`" + `\\){1,}\s*)&&(.[^\\])`)
		content = commentContinuation.ReplaceAllString(content, "&&$1$2")

		// fmt.Printf("Content0: %s\n", content)
		lines = strings.SplitAfter(content, "\n")
		/**
		if the next line is not a comment, and we didn't start with a continuation, don't add the `&&`.
		*/
		inContinuation := false
		for i := range lines {
			lineTrim := strings.Trim(lines[i], " \t\\\n")
			// fmt.Printf("LineTrim: %s\n", lineTrim)
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

			// fmt.Printf("isComment: %v, nextLineIsComment: %v, inContinuation: %v\n", isComment, nextLineIsComment, inContinuation)
			if isComment && (inContinuation || nextLineIsComment) {
				lines[i] = strings.Replace(lines[i], "#`\\", "#`&&\\", 1)
			}

			if len(lineTrim) >= 2 && !isComment && lineTrim[len(lineTrim)-2:] == "&&" {
				inContinuation = true
			} else if !isComment {
				inContinuation = false
			}
		}

		content = strings.Join(lines, "")
	}

	// Now that we have a valid bash-style command, we can format it with shfmt
	// log.Printf("Content1: %s\n", content)
	content = formatBash(content, c)

	// log.Printf("Content2: %s\n", content)

	if !hereDoc {
		reBacktickComment := regexp.MustCompile(`([ \t]*)(?:&& )?` + "`(#.*)#` " + `\\`)
		content = reBacktickComment.ReplaceAllString(content, "$1$2")

		// Fixup the comment indentation
		lines := strings.SplitAfter(content, "\n")
		prevIsComment := false
		prevCommentSpacing := ""
		firstLineIsComment := false
		for i := range lines {
			lineTrim := strings.TrimLeft(lines[i], " \t")
			// fmt.Printf("LineTrim: %s, %v\n", lineTrim, prevIsComment)
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

	}
	return content
}

func formatRun(n *ExtendedNode, c *Config) string {
	// Get the original RUN command text
	hereDoc := false
	flags := n.Node.Flags

	var content string
	var formattedFlags string
	var hasMultilineFlags bool

	if len(n.Node.Heredocs) >= 1 {
		content = n.Node.Heredocs[0].Content
		hereDoc = true
		// TODO: check if doc.FileDescriptor == 0?
	} else {
		originalText := n.OriginalMultiline
		if n.OriginalMultiline == "" {
			originalText = n.Node.Original
		}

		// Try to extract flags with multiline formatting preserved
		if len(flags) > 0 {
			formattedFlags, content, hasMultilineFlags = extractFlagsFormatted(originalText, flags, c.IndentSize)
		}

		if !hasMultilineFlags {
			// Fall back to naive whitespace splitting
			originalTrimmed := strings.TrimLeft(originalText, " \t")
			parts := regexp.MustCompile("[ \t]").Split(originalTrimmed, 2+len(flags))
			content = parts[1+len(flags)]
		}
	}

	// Try to parse as JSON
	var jsonItems []string
	err := json.Unmarshal([]byte(content), &jsonItems)
	if err == nil {
		out, err := Marshal(jsonItems)
		if err != nil {
			panic(err)
		}
		outStr := strings.ReplaceAll(string(out), "\",\"", "\", \"")
		content = outStr + "\n"
	} else {
		content = formatShell(content, hereDoc, c)
		if hereDoc {
			n.Node.Heredocs[0].Content = content
			content, _ = GetHeredoc(n)
		}
	}

	if len(flags) > 0 {
		// Check if we should auto-split mount flags
		hasMountFlags := false
		for _, flag := range flags {
			if strings.HasPrefix(flag, "--mount") {
				hasMountFlags = true
				break
			}
		}

		if c.MultilineMounts && hasMountFlags {
			content = formatFlagsWithMountSplit(flags, c) + content
		} else if hasMultilineFlags {
			content = formattedFlags + content
		} else {
			content = strings.Join(flags, " ") + " " + content
		}
	}

	return strings.ToUpper(n.Value) + " " + content
}

func GetHeredoc(n *ExtendedNode) (string, bool) {
	if len(n.Node.Heredocs) == 0 {
		return "", false
	}

	// printAST(n, 0)
	args := []string{}
	cur := n.Next
	for cur != nil {
		if cur.Node.Value != "" {
			args = append(args, cur.Node.Value)
		}
		cur = cur.Next
	}
	content := strings.Join(args, " ") + "\n" + n.Node.Heredocs[0].Content + n.Node.Heredocs[0].Name + "\n"
	return content, true
}

func formatBasic(n *ExtendedNode, c *Config) string {
	// Uppercases the command, and indent the following lines
	originalTrimmed := strings.TrimLeft(n.OriginalMultiline, " \t")

	value, success := GetHeredoc(n)
	if !success {
		parts := regexp.MustCompile("[ \t]").Split(originalTrimmed, 2)
		if len(parts) < 2 {
			// No argument after directive; just return the directive itself
			return strings.ToUpper(n.Value) + "\n"
		}
		value = strings.TrimLeft(parts[1], " \t")
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
		rawValue := strings.Trim(node.Node.Value, " \t")
		if len(node.Node.Flags) > 0 {
			cmd = append(cmd, node.Node.Flags...)
		}
		// log.Printf("ShouldSplitNode: %v\n", shouldSplitNode)
		if shouldSplitNode {
			parts, err := shlex.Split(rawValue)
			if err != nil {
				log.Fatalf("Error splitting: %s\n", node.Node.Value)
			}
			cmd = append(cmd, parts...)
		} else {
			cmd = append(cmd, rawValue)
		}
	}
	// log.Printf("getCmd: %v\n", cmd)
	return cmd
}

func formatEntrypoint(n *ExtendedNode, c *Config) string {
	return formatCmd(n, c)
}

func formatCmd(n *ExtendedNode, c *Config) string {
	// Determine JSON form from parser attributes
	isJSON, ok := n.Node.Attributes["json"]
	if !ok {
		isJSON = false
	}

	// Extract raw content after directive (and any flags)
	flags := n.Node.Flags
	originalText := n.OriginalMultiline
	if originalText == "" {
		originalText = n.Node.Original
	}

	var content string
	var formattedFlags string
	var hasMultilineFlags bool

	// Try to extract flags with multiline formatting preserved
	if len(flags) > 0 {
		formattedFlags, content, hasMultilineFlags = extractFlagsFormatted(originalText, flags, c.IndentSize)
	}

	if !hasMultilineFlags {
		// Fall back to naive whitespace splitting
		originalTrimmed := strings.TrimLeft(originalText, " \t")
		parts := regexp.MustCompile("[ \t]").Split(originalTrimmed, 2+len(flags))
		if len(parts) < 1+len(flags) {
			return strings.ToUpper(n.Value) + "\n"
		}
		if len(parts) >= 2+len(flags) {
			content = parts[1+len(flags)]
		}
	}

	// If JSON form (attribute or decodable), format as JSON array with spaces
	var jsonItems []string
	if isJSON || json.Unmarshal([]byte(content), &jsonItems) == nil {
		items := getCmd(n.Next, false)
		if !isJSON && len(items) == 0 {
			items = jsonItems
		}
		b, err := Marshal(items)
		if err != nil {
			return ""
		}
		bWithSpace := strings.ReplaceAll(string(b), "\",\"", "\", \"")
		return strings.ToUpper(n.Node.Value) + " " + bWithSpace + "\n"
	}

	// Otherwise, format as shell command
	shell := formatShell(content, false, c)
	if len(flags) > 0 {
		// Check if we should auto-split mount flags
		hasMountFlags := false
		for _, flag := range flags {
			if strings.HasPrefix(flag, "--mount") {
				hasMountFlags = true
				break
			}
		}

		if c.MultilineMounts && hasMountFlags {
			shell = formatFlagsWithMountSplit(flags, c) + shell
		} else if hasMultilineFlags {
			shell = formattedFlags + shell
		} else {
			shell = strings.Join(flags, " ") + " " + shell
		}
	}
	return strings.ToUpper(n.Node.Value) + " " + shell
}

func formatSpaceSeparated(n *ExtendedNode, c *Config) string {
	isJSON, ok := n.Node.Attributes["json"]
	if !ok {
		isJSON = false
	}
	cmd, success := GetHeredoc(n)
	if !success {
		cmd = strings.Join(getCmd(n.Next, isJSON), " ")
		if len(n.Node.Flags) > 0 {
			// Check if we should auto-split mount flags
			hasMountFlags := false
			for _, flag := range n.Node.Flags {
				if strings.HasPrefix(flag, "--mount") {
					hasMountFlags = true
					break
				}
			}

			if c.MultilineMounts && hasMountFlags {
				cmd = formatFlagsWithMountSplit(n.Node.Flags, c) + cmd
			} else if formatted, _, hasMultiline := extractFlagsFormatted(n.OriginalMultiline, n.Node.Flags, c.IndentSize); hasMultiline {
				cmd = formatted + cmd
			} else {
				cmd = strings.Join(n.Node.Flags, " ") + " " + cmd
			}
		}
		cmd += "\n"
	}

	return strings.ToUpper(n.Node.Value) + " " + cmd
}

func formatMaintainer(n *ExtendedNode, c *Config) string {
	// Get text between quotes
	maintainer := strings.Trim(n.Next.Node.Value, "\"")
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
	// Split the string into lines by newlines
	// log.Printf("Lines: .%s.\n", lines)
	linesArray := strings.SplitAfter(lines, "\n")
	// Create a new slice to hold the stripped lines
	var strippedLines string
	// Iterate over each line
	for _, line := range linesArray {
		// Trim leading and trailing whitespace
		// log.Printf("Line .%s.\n", line)
		hadNewline := len(line) > 0 && line[len(line)-1] == '\n'
		if rightOnly {
			// Only trim trailing whitespace
			line = strings.TrimRight(line, " \t\n")
		} else {
			// Trim both leading and trailing whitespace
			line = strings.Trim(line, " \t\n")
		}

		// log.Printf("Line2 .%s.", line)
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
	re := regexp.MustCompile(`\n{3,}`)
	return re.ReplaceAllString(missingContent, "\n")
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

/*
*
// Node is a structure used to represent a parse tree.
//
// In the node there are three fields, Value, Next, and Children. Value is the
// current token's string value. Next is always the next non-child token, and
// children contains all the children. Here's an example:
//
// (value next (child child-next child-next-next) next-next)
//
*/
func printAST(n *ExtendedNode, indent int) {
	fmt.Printf("\n%sNode: %s\n", strings.Repeat("\t", indent), n.Node.Value)
	fmt.Printf("%sOriginal: %s\n", strings.Repeat("\t", indent), n.Node.Original)
	fmt.Printf(
		"%sOriginalMultiline\n%s=====\n%s%s======\n",
		strings.Repeat("\t", indent),
		strings.Repeat("\t", indent),
		n.OriginalMultiline,
		strings.Repeat("\t", indent),
	)
	fmt.Printf("%sAttributes: %v\n", strings.Repeat("\t", indent), n.Node.Attributes)
	fmt.Printf("%sHeredocs: %v\n", strings.Repeat("\t", indent), n.Node.Heredocs)
	// n.PrevComment
	fmt.Printf("%sPrevComment: %v\n", strings.Repeat("\t", indent), n.Node.PrevComment)
	fmt.Printf("%sStartLine: %d\n", strings.Repeat("\t", indent), n.Node.StartLine)
	fmt.Printf("%sEndLine: %d\n", strings.Repeat("\t", indent), n.Node.EndLine)
	fmt.Printf("%sFlags: %v\n", strings.Repeat("\t", indent), n.Node.Flags)

	if n.Children != nil {
		fmt.Printf("\n%s!!!! Children\n%s==========\n", strings.Repeat("\t", indent), strings.Repeat("\t", indent))
		for _, c := range n.Children {
			printAST(c, indent+1)
		}
	}
	if n.Next != nil {
		fmt.Printf("\n%s!!!! Next\n%s==========\n", strings.Repeat("\t", indent), strings.Repeat("\t", indent))
		printAST(n.Next, indent+1)
	}
}
