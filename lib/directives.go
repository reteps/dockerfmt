package lib

import (
	"strings"

	"github.com/moby/buildkit/frontend/dockerfile/command"
)

// A directiveFormatter returns the formatted source of an instruction,
// ending in a newline.
type directiveFormatter func(n *node, c *Config) string

// formatters maps lower-case instruction keywords to their formatters.
// Instructions without one are kept as written.
var formatters map[string]directiveFormatter

func init() {
	formatters = map[string]directiveFormatter{
		command.Add:         spaceSeparated(flagsOnOwnLines),
		command.Arg:         formatWords,
		command.Cmd:         formatCmd,
		command.Copy:        spaceSeparated(flagsOnOwnLines),
		command.Entrypoint:  formatCmd,
		command.Env:         formatEnv,
		command.Expose:      spaceSeparated(argsOnOwnLines),
		command.From:        spaceSeparated(singleLine),
		command.Healthcheck: formatWords,
		command.Label:       formatWords,
		command.Maintainer:  formatMaintainer,
		command.Onbuild:     formatOnbuild,
		command.Run:         formatRun,
		command.Shell:       formatCmd,
		command.StopSignal:  formatWords,
		command.User:        formatWords,
		command.Volume:      formatWords,
		command.Workdir:     spaceSeparated(singleLine),
	}
}

// formatNode formats instruction n. ok is false if there is no formatter for
// its keyword.
func formatNode(n *node, c *Config) (out string, ok bool) {
	f := formatters[strings.ToLower(n.Value)]
	if f == nil {
		return "", false
	}
	return f(n, c), true
}

func formatRun(n *node, c *Config) string {
	region, ok := scanFlagsRegion(n)
	if !ok {
		return n.source
	}
	ctx := n.context()
	var content string
	switch {
	case len(n.Heredocs) > 0:
		// Only a heredoc the shell runs as a script is shell; one fed to
		// another program is data.
		if isShellScriptHeredoc(n.Node, ctx) && ctx.formatsShell() {
			h := n.Heredocs[0]
			content = n.heredocsWithScript(formatScript(h.Content, c, h.Chomp))
		} else {
			content = n.heredocs()
		}
	case n.Attributes["json"]:
		if n.hasCommentLines() {
			return n.source
		}
		content = marshalJSONStringArray(n.args()) + "\n"
	case strings.TrimSpace(region.content) == "":
		return bareInstruction(n.keyword(), region.flags())
	default:
		content = region.content
		if ctx.formatsShell() {
			content = formatShellBody(content, c)
		}
	}

	if region.hasLines() {
		return n.keyword() + " " + withFlagLines(&region, content, c, ctx.escape)
	}
	return n.keyword() + " " + withFlags(region.flags(), content, c, ctx.escape)
}

// formatCmd formats CMD, ENTRYPOINT and SHELL.
func formatCmd(n *node, c *Config) string {
	region, ok := scanFlagsRegion(n)
	if !ok {
		return n.source
	}
	if n.Attributes["json"] {
		if n.hasCommentLines() {
			return n.source
		}
		// BuildKit decides what is JSON: an argument list it can't decode,
		// like ["a",], is shell form and must stay that way.
		return n.keyword() + " " + withFlags(region.flags(), marshalJSONStringArray(n.args())+"\n", c, n.context().escape)
	}
	content := region.content
	if strings.TrimSpace(content) == "" {
		return bareInstruction(n.keyword(), region.flags())
	}
	if n.context().formatsShell() {
		content = formatShellBody(content, c)
	}
	return n.keyword() + " " + withFlags(region.flags(), content, c, n.context().escape)
}

// bareInstruction formats an instruction that has no arguments after its
// flags.
func bareInstruction(keyword string, flags []string) string {
	return strings.Join(append([]string{keyword}, flags...), " ") + "\n"
}

func formatEnv(n *node, c *Config) string {
	if n.next == nil {
		return n.keyword() + "\n"
	}
	// BuildKit parses the legacy "ENV key value" form into a key/value pair
	// whose third node, the delimiter, is empty.
	if n.next.next.next.Value == "" {
		if n.hasCommentLines() {
			return n.source
		}
		return formatLegacyEnv(n.keyword(), n.next.Value, n.next.next.Value)
	}
	content, ok := n.content()
	if !ok {
		return n.source
	}
	return n.keyword() + " " + reindentContinuations(content, c.IndentSize, n.context().escape, true)
}

// formatLegacyEnv rewrites "ENV key value" as "ENV key=value". Everything after
// the key is the value, so a value with whitespace is quoted, or the new form
// would read it as several pairs. A value that also contains quotes or
// backslashes can't be quoted without changing it, so it keeps the legacy form.
func formatLegacyEnv(keyword, key, value string) string {
	if !strings.ContainsAny(value, " \t") || isQuotedWord(value) {
		return keyword + " " + key + "=" + value + "\n"
	}
	if strings.ContainsAny(value, "\"'\\") {
		return keyword + " " + key + " " + value + "\n"
	}
	return keyword + " " + key + "=\"" + value + "\"\n"
}

// isQuotedWord reports whether s is a single word wrapped in matching quotes.
func isQuotedWord(s string) bool {
	if len(s) < 2 || (s[0] != '"' && s[0] != '\'') || s[len(s)-1] != s[0] {
		return false
	}
	return !strings.ContainsAny(s[1:len(s)-1], string(s[0])+"\\")
}

// formatWords formats instructions whose arguments are words (ARG, LABEL,
// USER, ...), keeping their layout and re-indenting continuation lines.
func formatWords(n *node, c *Config) string {
	content, ok := n.content()
	if !ok {
		return n.keyword() + "\n"
	}
	return n.keyword() + " " + reindentContinuations(content, c.IndentSize, n.context().escape, false)
}

// multilineMode says how a space-separated instruction written across
// several lines is laid out. COPY/ADD break before each flag but keep
// "<src> <dst>" together, while EXPOSE breaks before each port.
type multilineMode int

const (
	singleLine      multilineMode = iota // FROM, WORKDIR
	flagsOnOwnLines                      // COPY, ADD
	argsOnOwnLines                       // EXPOSE
)

// spaceSeparated returns a formatter for instructions whose arguments are
// flags and space-separated words (COPY, ADD, EXPOSE, FROM, WORKDIR).
func spaceSeparated(mode multilineMode) directiveFormatter {
	return func(n *node, c *Config) string {
		region, ok := scanFlagsRegion(n)
		if !ok || n.hasCommentLines() {
			// The arguments are rebuilt from BuildKit's parse, which has no
			// comments.
			return n.source
		}
		flags := region.flags()
		escape := n.context().escape
		if len(n.Heredocs) > 0 {
			return n.keyword() + " " + layoutFlags(flags, n.heredocs(), c, escape, false)
		}

		sep := " "
		if mode == argsOnOwnLines && n.multiline() {
			sep = " " + string(escape) + "\n" + c.indent()
		}
		args := n.args()
		var content string
		switch {
		case n.Attributes["json"] && !safeAsWords(args):
			content = marshalJSONStringArray(args)
		case n.Attributes["json"]:
			// BuildKit expands each argument the same way in both forms.
			content = strings.Join(args, sep)
		default:
			for i, a := range args {
				args[i] = strings.Trim(a, " \t")
			}
			content = strings.Join(args, sep)
		}
		if content == "" {
			return bareInstruction(n.keyword(), flags)
		}
		onOwnLines := mode == flagsOnOwnLines && (n.multiline() || hasMountFlag(flags))
		return n.keyword() + " " + layoutFlags(flags, content, c, escape, onOwnLines) + "\n"
	}
}

// safeAsWords reports whether JSON-form arguments can be written in the
// space-separated form, which BuildKit splits on whitespace: none may be empty
// or contain whitespace.
func safeAsWords(args []string) bool {
	for _, a := range args {
		if a == "" || strings.ContainsAny(a, " \t\n\r\v\f") {
			return false
		}
	}
	return true
}

const maintainerLabel = "org.opencontainers.image.authors"

// formatMaintainer rewrites the deprecated MAINTAINER as the equivalent LABEL.
func formatMaintainer(n *node, _ *Config) string {
	if n.next == nil || n.hasCommentLines() {
		return n.source
	}
	// MAINTAINER takes the rest of the line as is, while LABEL processes
	// quotes, escapes and variables, so those are escaped.
	value := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`).Replace(unwrapMaintainerQuotes(n.next.Value))
	return "LABEL " + maintainerLabel + "=\"" + value + "\"\n"
}

// unwrapMaintainerQuotes drops one pair of double quotes wrapping a MAINTAINER
// value, as in MAINTAINER "Jane Doe".
func unwrapMaintainerQuotes(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' && !strings.Contains(s[1:len(s)-1], `"`) {
		return s[1 : len(s)-1]
	}
	return s
}

func formatOnbuild(n *node, c *Config) string {
	if n.next == nil || len(n.next.children) != 1 || n.hasCommentLines() {
		return n.source
	}
	// The trigger runs with the shell the image ends up with, which is the
	// one in effect here.
	inner := n.next.children[0]
	inner.ctx = n.ctx
	out, ok := formatNode(inner, c)
	if !ok {
		return n.source
	}
	// The inner instruction has no position, so formatters that fall back to
	// its text return it without a line ending.
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return n.keyword() + " " + out
}
