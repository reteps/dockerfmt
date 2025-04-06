package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/command"
	"github.com/moby/buildkit/frontend/dockerfile/parser"
	"github.com/spf13/cobra"
	"mvdan.cc/sh/v3/syntax"
)

var (
	writeFlag   bool
	checkFlag   bool
	newlineFlag bool
	indentSize  uint
)

var rootCmd = &cobra.Command{
	Use:   "dockerfmt [Dockerfile]",
	Short: "dockerfmt is a Dockerfile and RUN step formatter.",
	Long:  `A updated version of the dockfmt. Uses the dockerfile parser from moby/buildkit and the shell formatter from mvdan/sh.`,
	Run:   Run,
	Args:  cobra.MinimumNArgs(1),
}

func Run(cmd *cobra.Command, args []string) {
	for _, fileName := range args {
		parseState, rootNode := BuildParseStateFromFile(fileName)
		parseState.processNode(rootNode)

		// After all directives are processed, we need to check if we have any trailing comments to add.
		if parseState.CurrentLine < len(parseState.AllOriginalLines) {
			// Add the rest of the file
			parseState.addLines(parseState.AllOriginalLines[parseState.CurrentLine:])
		}

		// Ensure the output ends with a newline if requested
		if newlineFlag {
			if parseState.Output[len(parseState.Output)-1] != '\n' {
				parseState.Output += "\n"
			}
		} else {
			parseState.Output = strings.TrimSuffix(parseState.Output, "\n")
		}

		if checkFlag {
			// Check if the file is already formatted
			originalContent := strings.Join(parseState.AllOriginalLines, "")
			if originalContent != parseState.Output {
				fmt.Printf("File %s is not formatted\n", fileName)
				os.Exit(1)
			}
		} else if writeFlag {
			// Write the formatted output back to the file
			err := os.WriteFile(fileName, []byte(parseState.Output), 0644)
			if err != nil {
				log.Fatalf("Failed to write to file %s: %v", fileName, err)
			}
		} else {
			// Print the formatted output to stdout
			fmt.Printf("%s", parseState.Output)
		}
	}
}

func init() {
	rootCmd.Flags().BoolVarP(&writeFlag, "write", "w", false, "Write the formatted output back to the file(s)")
	rootCmd.Flags().BoolVarP(&checkFlag, "check", "c", false, "Check if the file(s) are formatted")
	rootCmd.Flags().BoolVarP(&newlineFlag, "newline", "n", false, "End the file with a trailing newline")
	rootCmd.Flags().UintVarP(&indentSize, "indent", "i", 4, "Number of spaces to use for indentation")
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

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
}

func (df *ParseState) processNode(ast *ExtendedNode) {

	// We don't want to process nodes that don't have a start or end line.
	if ast.Node.StartLine == 0 || ast.Node.EndLine == 0 {
		return
	}

	// check if we are on the correct line,
	// otherwise get the comments we are missing
	if df.CurrentLine != ast.StartLine {
		df.addLines(df.AllOriginalLines[df.CurrentLine : ast.StartLine-1])
		df.CurrentLine = ast.StartLine
	}

	nodeName := strings.ToLower(ast.Node.Value)

	dispatch := map[string]func(*ExtendedNode) string{
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

		df.Output += fmtFunc(ast)
		df.CurrentLine = ast.EndLine
		// fmt.Printf("CurrentLine: %d, %d\n", df.CurrentLine, ast.EndLine)
		// return
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

func (df *ParseState) addLines(lines []string) {
	missingContent := stripWhitespace(strings.Join(lines, ""), false)
	// Replace multiple newlines with a single newline
	re := regexp.MustCompile(`\n{2,}`)
	missingContent = re.ReplaceAllString(missingContent, "\n")
	df.Output += missingContent
}

func BuildParseStateFromFile(fileName string) (*ParseState, *ExtendedNode) {
	f, err := os.Open(fileName)
	if err != nil {
		panic(err)
	}
	result, err := parser.Parse(f)
	if err != nil {
		panic(err)
	}
	f.Seek(0, io.SeekStart)
	defer f.Close()
	n := result.AST
	b := new(strings.Builder)
	io.Copy(b, f)
	fileLines := strings.SplitAfter(b.String(), "\n")

	parseState := &ParseState{
		CurrentLine:      0,
		Output:           "",
		AllOriginalLines: fileLines,
	}
	node := BuildExtendedNode(n, fileLines)
	return parseState, node
}

func BuildExtendedNode(n *parser.Node, fileLines []string) *ExtendedNode {
	// Build an extended node from the parser node
	// This is used to add the original multiline string to the node
	// and to add the original line number

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

func formatEnv(n *ExtendedNode) string {
	// Only the legacy format will have a empty 3rd child
	if n.Next.Next.Next.Value == "" {
		return strings.ToUpper(n.Node.Value) + " " + n.Next.Node.Value + "=" + n.Next.Next.Node.Value + "\n"
	}
	// Otherwise, we have a valid env command
	content := stripWhitespace(regexp.MustCompile(" ").Split(n.OriginalMultiline, 2)[1], true)
	// Indent all lines with indentSize spaces
	re := regexp.MustCompile("(?m)^ *")
	content = strings.Trim(re.ReplaceAllString(content, strings.Repeat(" ", int(indentSize))), " ")
	return strings.ToUpper(n.Value) + " " + content
}

func formatBash(s string) string {
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
		syntax.Indent(indentSize),
		syntax.BinaryNextLine(true),
	).Print(buf, f)
	return buf.String()
}

func formatRun(n *ExtendedNode) string {
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
			content = stripWhitespace(content, true)
			re := regexp.MustCompile(`(\\\n\s+)((?:\s*#.*){1,})`)
			content = re.ReplaceAllString(content, `$1$( $2`+"\n"+`) \`)
			// log.Printf("Content: %s\n", content)
		}

		// Now that we have a valid bash-style command, we can format it with shfmt
		content = formatBash(content)

		if !hereDoc {
			// Recover comments $( #...)
			content = regexp.MustCompile(`\$\(\s+(#[\w\W]*?)\s+\) \\`).ReplaceAllString(content, "$1")
			content = regexp.MustCompile(" *(#.*)").ReplaceAllString(content, strings.Repeat(" ", int(indentSize))+"$1")
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

func stripWhitespace(lines string, rightOnly bool) string {
	// Split the string into lines by newlines
	linesArray := strings.Split(lines, "\n")
	// Create a new slice to hold the stripped lines
	var strippedLines string
	// Iterate over each line
	for i, line := range linesArray {
		// Trim leading and trailing whitespace
		if rightOnly {
			// Only trim trailing whitespace
			line = strings.TrimRight(line, " \t")
		} else {
			// Trim both leading and trailing whitespace
			line = strings.TrimSpace(line)
		}

		if line != "" || i != len(linesArray)-1 {
			strippedLines += line + "\n"
		}
	}
	return strippedLines
}

func formatBasic(n *ExtendedNode) string {
	// Uppercases the command, and indent the following lines
	parts := regexp.MustCompile(" ").Split(n.OriginalMultiline, 2)
	return indentFollowingLines(strings.ToUpper(n.Value) + " " + parts[1])
}

func indentFollowingLines(lines string) string {
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

func formatCmd(n *ExtendedNode) string {
	cmd := getCmd(n.Next)
	b, err := json.Marshal(cmd)
	if err != nil {
		return ""
	}
	return strings.ToUpper(n.Value) + " " + string(b) + "\n"
}

func formatCopy(n *ExtendedNode) string {
	cmd := strings.Join(getCmd(n.Next), " ")
	if len(n.Node.Flags) > 0 {
		cmd = strings.Join(n.Node.Flags, " ") + " " + cmd
	}

	return strings.ToUpper(n.Value) + " " + cmd + "\n"
}

func formatMaintainer(n *ExtendedNode) string {

	// Get text between quotes
	maintainer := strings.Trim(n.Next.Value, "\"")
	return "LABEL org.opencontainers.image.authors=\"" + maintainer + "\"\n"
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
	fmt.Printf("%sOriginalMultiline\n%s=====\n%s%s======\n", strings.Repeat("\t", indent), strings.Repeat("\t", indent), n.OriginalMultiline, strings.Repeat("\t", indent))
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
