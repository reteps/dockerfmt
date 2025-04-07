package lib

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/command"
	"github.com/moby/buildkit/frontend/dockerfile/parser"
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

	nodeName := strings.ToLower(ast.Node.Value)

	dispatch := map[string]func(*ExtendedNode, *Config) string{
		command.Add:         formatCopy,
		command.Arg:         formatBasic,
		command.Cmd:         formatCmd,
		command.Copy:        formatCopy,
		command.Entrypoint:  formatCmd,
		command.Env:         formatEnv,
		command.Expose:      formatBasic,
		command.From:        formatBasic,
		command.Healthcheck: formatBasic,
		command.Label:       formatBasic, // TODO: order labels?
		command.Maintainer:  formatMaintainer,
		command.Onbuild:     formatBasic,
		command.Run:         formatRun,
		command.Shell:       formatCmd,
		command.StopSignal:  formatBasic,
		command.User:        formatBasic,
		command.Volume:      formatBasic,
		command.Workdir:     formatBasic,
	}

	fmtFunc := dispatch[nodeName]
	if fmtFunc != nil {
		// if df.Output != "" {
		// 	// If the previous line isn't a comment or newline, add a newline
		// 	lastTwoChars := df.Output[len(df.Output)-2 : len(df.Output)]
		// 	lastNonTrailingNewline := strings.LastIndex(strings.TrimRight(df.Output, "\n"), "\n")
		// 	if lastTwoChars != "\n\n" && df.Output[lastNonTrailingNewline+1] != '#' {
		// 		df.Output += "\n"
		// 	}
		// }

		df.Output += fmtFunc(ast, df.Config)
		df.CurrentLine = ast.EndLine
		// fmt.Printf("CurrentLine: %d, %d\n", df.CurrentLine, ast.EndLine)
	}
	// fmt.Printf("Unknown command: %s %s\n", nodeName, ast.OriginalMultiline)

	for _, child := range ast.Children {
		df.processNode(child)
	}

	// fmt.Printf("CurrentLine2: %d, %d\n", df.CurrentLine, ast.EndLine)

	if ast.Node.Next != nil {
		df.processNode(ast.Next)
	}
}

func BuildParseStateFromFileLines(fileLines []string) (*ParseState, *ExtendedNode) {
	// Create a new parser node from the file lines
	result, err := parser.Parse(strings.NewReader(strings.Join(fileLines, "\n")))
	if err != nil {
		panic(err)
	}
	n := result.AST

	parseState := &ParseState{
		CurrentLine:      0,
		Output:           "",
		AllOriginalLines: fileLines,
	}
	node := BuildExtendedNode(n, fileLines)
	return parseState, node
}

func FormatFileLines(fileLines []string, indentSize uint, trailingNewline bool) string {
	// Top level library function that formats file contents given the lines and the config.
	// Since we want this to work exposed to WASM, each config flag should be passed in separately
	// and not as a struct.
	parseState, rootNode := BuildParseStateFromFileLines(fileLines)
	parseState.Config = &Config{
		IndentSize:      indentSize,
		TrailingNewline: trailingNewline,
	}
	parseState.processNode(rootNode)

	// After all directives are processed, we need to check if we have any trailing comments to add.
	if parseState.CurrentLine < len(parseState.AllOriginalLines) {
		// Add the rest of the file
		parseState.Output += FormatComments(parseState.AllOriginalLines[parseState.CurrentLine:])
	}

	// Ensure the output ends with a newline if requested
	if trailingNewline {
		if parseState.Output[len(parseState.Output)-1] != '\n' {
			parseState.Output += "\n"
		}
	} else {
		parseState.Output = strings.TrimSuffix(parseState.Output, "\n")
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
	// Only the legacy format will have a empty 3rd child
	if n.Next.Next.Next.Value == "" {
		return strings.ToUpper(n.Node.Value) + " " + n.Next.Node.Value + "=" + n.Next.Next.Node.Value + "\n"
	}
	// Otherwise, we have a valid env command
	content := StripWhitespace(regexp.MustCompile(" ").Split(n.OriginalMultiline, 2)[1], true)
	// Indent all lines with indentSize spaces
	re := regexp.MustCompile("(?m)^ *")
	content = strings.Trim(re.ReplaceAllString(content, strings.Repeat(" ", int(c.IndentSize))), " ")
	return strings.ToUpper(n.Value) + " " + content
}

func formatRun(n *ExtendedNode, c *Config) string {
	// Get the original RUN command text
	hereDoc := false
	flags := n.Node.Flags

	var content string
	if len(n.Node.Heredocs) > 1 {
		// Not implemented yet
		panic("Multiple Heredocs not implemented yet")
	} else if len(n.Node.Heredocs) == 1 {
		content = n.Node.Heredocs[0].Content
		hereDoc = true
		// TODO: check if doc.FileDescriptor == 0?
	} else {
		// We split the original multiline string by whitespace
		parts := regexp.MustCompile("[ \t]").Split(n.OriginalMultiline, 2+len(flags))
		content = parts[1+len(flags)]
	}
	// Try to parse as JSON
	var jsonItems []string
	err := json.Unmarshal([]byte(content), &jsonItems)
	if err == nil {
		out, err := json.Marshal(jsonItems)
		if err != nil {
			panic(err)
		}
		outStr := strings.ReplaceAll(string(out), "\",\"", "\", \"")
		content = outStr + "\n"
	} else {
		if !hereDoc {
			// Replace comments with a subshell evaluation -- they won't be run so we can do this.
			content = StripWhitespace(content, true)
			re := regexp.MustCompile(`(\\\n\s+)((?:\s*#.*){1,})`)
			content = re.ReplaceAllString(content, `$1$( $2`+"\n"+`) \`)
			// log.Printf("Content: %s\n", content)
		}

		// Now that we have a valid bash-style command, we can format it with shfmt
		content = formatBash(content, c.IndentSize)

		if !hereDoc {
			// Recover comments $( #...)
			content = regexp.MustCompile(`\$\(\s+(#[\w\W]*?)\s+\) \\`).ReplaceAllString(content, "$1")
			content = regexp.MustCompile(" *(#.*)").ReplaceAllString(content, strings.Repeat(" ", int(c.IndentSize))+"$1")
		}

		if hereDoc {
			content = "<<" + n.Node.Heredocs[0].Name + "\n" + content + n.Node.Heredocs[0].Name
		}
	}

	if len(flags) > 0 {
		content = strings.Join(flags, " ") + " " + content
	}

	return strings.ToUpper(n.Value) + " " + content
}

func formatBasic(n *ExtendedNode, c *Config) string {
	// Uppercases the command, and indent the following lines
	parts := regexp.MustCompile(" ").Split(n.OriginalMultiline, 2)
	return IndentFollowingLines(strings.ToUpper(n.Value)+" "+parts[1], c.IndentSize)
}

func getCmd(n *ExtendedNode) []string {
	cmd := []string{}
	for node := n; node != nil; node = node.Next {
		cmd = append(cmd, node.Value)
		if len(node.Flags) > 0 {
			cmd = append(cmd, node.Flags...)
		}
	}
	return cmd
}

func formatCmd(n *ExtendedNode, c *Config) string {
	cmd := getCmd(n.Next)
	b, err := json.Marshal(cmd)
	if err != nil {
		return ""
	}
	return strings.ToUpper(n.Value) + " " + string(b) + "\n"
}

func formatCopy(n *ExtendedNode, c *Config) string {
	cmd := strings.Join(getCmd(n.Next), " ")
	if len(n.Node.Flags) > 0 {
		cmd = strings.Join(n.Node.Flags, " ") + " " + cmd
	}

	return strings.ToUpper(n.Value) + " " + cmd + "\n"
}

func formatMaintainer(n *ExtendedNode, c *Config) string {

	// Get text between quotes
	maintainer := strings.Trim(n.Next.Value, "\"")
	return "LABEL org.opencontainers.image.authors=\"" + maintainer + "\"\n"
}
