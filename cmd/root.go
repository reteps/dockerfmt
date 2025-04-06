package cmd

import (
	"bufio"
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

var rootCmd = &cobra.Command{
	Use:   "dockerfmt [Dockerfile]",
	Short: "dockerfmt is a Dockerfile and RUN step formatter.",
	Long:  `A updated version of the excellent dockfmt. Uses the dockerfile parser from moby/buildkit and the shell formatter from mvdan/sh`,
	Run:   Run,
	Args:  cobra.ExactArgs(1),
}

func Run(cmd *cobra.Command, args []string) {
	fileName := args[0]

	parseState, rootNode := BuildParseStateFromFile(fileName)
	// parseState = parseState
	parseState.processNode(rootNode)

	// Write parseState.Output to stdout
	fmt.Printf("%s", parseState.Output)
	// PrintAST(rootNode, 0)

	// nodes := []*parser.Node{ast}
	// if ast.Children != nil {
	// 	nodes = append(nodes, ast.Children...)
	// }
	// Print out the AST
	// We shouldn't have more than two levels of nodes
	/*
		node
			- child
		next
			- child1
			- child2
	*/

}

func init() {
	// todo: debug flag
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

	type Node struct {
		Value       string          // actual content
		Next        *Node           // the next item in the current sexp
		Children    []*Node         // the children of this sexp
		Heredocs    []Heredoc       // extra heredoc content attachments
		Attributes  map[string]bool // special attributes for this node
		Original    string          // original line used before parsing
		Flags       []string        // only top Node should have this set
		StartLine   int             // the line in the original dockerfile where the node begins
		EndLine     int             // the line in the original dockerfile where the node ends
		PrevComment []string
	}
*/
func PrintAST(n *ExtendedNode, indent int) {
	// Print out the AST

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
			PrintAST(c, indent+1)
		}
	}
	if n.Next != nil {
		fmt.Printf("\n%s!!!! Next\n%s==========\n", strings.Repeat("\t", indent), strings.Repeat("\t", indent))
		PrintAST(n.Next, indent+1)
	}

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
	scanner := bufio.NewScanner(f)
	// optionally, resize scanner's capacity for lines over 64K, see next example
	var fileLines []string
	for scanner.Scan() {
		// fmt.Printf("Line: %s\n", scanner.Text())
		fileLines = append(fileLines, scanner.Text()+"\n")
	}
	// fmt.Printf("File lines: %d\n", len(fileLines))

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}

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
		return strings.ToUpper(n.Node.Value) + " " + n.Next.Node.Value + "=" + n.Next.Next.Node.Value
	}
	// Otherwise, we have a valid env command
	return formatNoop(n)
}

func formatBash(s string) string {
	r := strings.NewReader(s)
	f, err := syntax.NewParser(syntax.KeepComments(true)).Parse(r, "")
	if err != nil {
		fmt.Printf("Error parsing: %s\n", s)
		panic(err)
	}
	buf := new(bytes.Buffer)
	syntax.NewPrinter(syntax.Minify(false), syntax.SingleLine(false), syntax.Indent(4), syntax.BinaryNextLine(true)).Print(buf, f)
	return buf.String()
}
func formatRun(n *ExtendedNode) string {
	// Get the original RUN command text

	hereDoc := false
	var content string
	if len(n.Node.Heredocs) > 1 {
		// Not implemented yet
		panic("Multiple Heredocs not implemented yet")
	} else if len(n.Node.Heredocs) == 1 {
		content = n.Node.Heredocs[0].Content
		hereDoc = true
		// doc.FileDescriptor == 0
	} else {
		parts := regexp.MustCompile(" ").Split(n.OriginalMultiline, 2)
		content = parts[1]
	}

	if !hereDoc {
		// Replace line continuations with comments to use bash comment style
		re := regexp.MustCompile(`(\\\s*\n\s*)(#.*)`)
		content = re.ReplaceAllString(content, "$1`$2` \\")

		// Remove whitespace at the end of lines
		content = stripWhitespace(content, true)
	}

	// Now that we have a valid bash-style command, we can format it with shfmt
	content = formatBash(content)

	if !hereDoc {
		// Recover original dockerfile-style comments
		content = regexp.MustCompile(" *`(#.*)`..").ReplaceAllString(content, strings.Repeat(" ", 4)+"$1")
	}

	if hereDoc {
		content = "<<" + n.Node.Heredocs[0].Name + "\n" + content + n.Node.Heredocs[0].Name
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

func formatNoop(n *ExtendedNode) string {
	// noop function that simply uppercases the command
	parts := regexp.MustCompile(" ").Split(n.OriginalMultiline, 2)
	return strings.ToUpper(n.Value) + " " + parts[1]
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
	cmd := getCmd(n.Next)
	return strings.ToUpper(n.Value) + " " + strings.Join(cmd, " ")
}

func formatMaintainer(n *ExtendedNode) string {

	// Get text between quotes
	maintainer := strings.Trim(n.Next.Value, "\"")
	return "LABEL org.opencontainers.image.authors=\"" + maintainer + "\"\n"
}

func (df *ParseState) processNode(ast *ExtendedNode) {

	// We don't want to process nodes that don't have a start or end line.
	if ast.Node.StartLine == 0 || ast.Node.EndLine == 0 {
		return
	}

	// check if we are on the correct line,
	// otherwise get the comments we are missing
	if df.CurrentLine != ast.StartLine {
		missingContent := stripWhitespace(strings.Join(df.AllOriginalLines[df.CurrentLine:ast.StartLine-1], ""), false)
		// Replace multiple newlines with a single newline
		re := regexp.MustCompile(`\n{2,}`)
		missingContent = re.ReplaceAllString(missingContent, "\n")
		df.Output += missingContent
		df.CurrentLine = ast.StartLine
	}

	nodeName := strings.ToLower(ast.Node.Value)

	dispatch := map[string]func(*ExtendedNode) string{
		command.Add:         formatNoop,
		command.Arg:         formatNoop,
		command.Cmd:         formatCmd,
		command.Copy:        formatCopy,
		command.Entrypoint:  formatCmd,
		command.Env:         formatEnv,
		command.Expose:      formatNoop,
		command.From:        formatNoop,
		command.Healthcheck: formatNoop,
		command.Label:       formatNoop,
		command.Maintainer:  formatMaintainer,
		command.Onbuild:     formatNoop,
		command.Run:         formatRun,
		command.Shell:       formatNoop,
		command.StopSignal:  formatNoop,
		command.User:        formatNoop,
		command.Volume:      formatNoop,
		command.Workdir:     formatNoop,
	}

	fmtFunc := dispatch[nodeName]
	if fmtFunc != nil {
		if df.Output != "" {
			// If the previous line isn't a comment or newline, add a newline
			lastTwoChars := df.Output[len(df.Output)-2 : len(df.Output)]
			lastNonTrailingNewline := strings.LastIndex(strings.TrimRight(df.Output, "\n"), "\n")
			if lastTwoChars != "\n\n" && df.Output[lastNonTrailingNewline+1] != '#' {
				df.Output += "\n"
			}
		}

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
