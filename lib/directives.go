package lib

import (
	"strings"
	"unicode"

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
		command.Add:         spaceSeparated(flagsOnOwnLines, nil),
		command.Arg:         formatWords,
		command.Cmd:         formatCmd,
		command.Copy:        spaceSeparated(flagsOnOwnLines, nil),
		command.Entrypoint:  formatCmd,
		command.Env:         formatKeyValues,
		command.Expose:      spaceSeparated(argsOnOwnLines, lowerExposeProtocols),
		command.From:        spaceSeparated(singleLine, upperFromAs),
		command.Healthcheck: formatHealthcheck,
		command.Label:       formatKeyValues,
		command.Maintainer:  formatMaintainer,
		command.Onbuild:     formatOnbuild,
		command.Run:         formatRun,
		command.Shell:       formatCmd,
		command.StopSignal:  formatWords,
		command.User:        formatWords,
		command.Volume:      formatWords,
		command.Workdir:     spaceSeparated(singleLine, nil),
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
			var formatted bool
			content, formatted = formatShellBody(content, c)
			if !formatted && n.multiline() && len(region.items) > 0 {
				// Laying out the flags would re-indent the body's first
				// line and none of the others.
				return n.source
			}
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
		content, _ = formatShellBody(content, c)
	}
	return n.keyword() + " " + withFlags(region.flags(), content, c, n.context().escape)
}

func shellBodyOrAsWritten(body string, c *Config) string {
	out, _ := formatShellBody(body, c)
	return out
}

// bareInstruction formats an instruction that has no arguments after its
// flags.
func bareInstruction(keyword string, flags []string) string {
	return strings.Join(append([]string{keyword}, flags...), " ") + "\n"
}

// formatKeyValues formats ENV and LABEL, whose arguments are key=value
// pairs, rewriting the legacy "key value" form (LegacyKeyValueFormat).
func formatKeyValues(n *node, c *Config) string {
	if n.next == nil {
		return n.keyword() + "\n"
	}
	// BuildKit parses the legacy form into a key/value pair whose third
	// node, the delimiter, is empty.
	if n.next.next != nil && n.next.next.next != nil && n.next.next.next.Value == "" {
		// In the legacy form, whitespace in the value is part of it.
		if out, ok := formatLegacyKeyValue(n); ok && !n.hasCommentLines() {
			return out
		}
		return n.source
	}
	return formatWords(n, c)
}

// formatLegacyKeyValue rewrites "KEY key value" as "KEY key=value", where
// everything after the key, as written, is the value. Whitespace in the value
// must not separate words in the new form, so a value with whitespace is
// quoted if it has no quotes or escapes, and otherwise has that whitespace
// escaped. A value written across several lines is quoted with its line
// breaks.
func formatLegacyKeyValue(n *node) (string, bool) {
	escape := n.context().escape
	content, _ := n.content()
	key := n.next.Value
	value, ok := strings.CutPrefix(trimIndent(content), key)
	if !ok {
		return "", false
	}
	// BuildKit trims the whitespace around the value, including any line
	// break before it.
	value = strings.TrimRightFunc(trimLeadingContinuation(trimIndent(value), escape), unicode.IsSpace)
	// Variables expand the same inside double quotes.
	special := "\"'" + string(escape)
	switch {
	case n.multiline():
		// The continuation lines start inside the quotes, so they are kept
		// as written.
		if strings.ContainsAny(strings.ReplaceAll(value, string(escape)+"\n", ""), special) {
			return "", false
		}
		return n.keyword() + " " + key + "=\"" + value + "\"\n", true
	case !hasUnquotedSpace(value, escape):
		return n.keyword() + " " + key + "=" + value + "\n", true
	case !strings.ContainsAny(value, special):
		return n.keyword() + " " + key + "=\"" + value + "\"\n", true
	default:
		return n.keyword() + " " + key + "=" + escapeUnquotedSpace(value, escape) + "\n", true
	}
}

// hasUnquotedSpace reports whether s has whitespace outside quotes that
// isn't escaped, which would separate words.
func hasUnquotedSpace(s string, escape rune) bool {
	return escapeUnquotedSpace(s, escape) != s
}

// escapeUnquotedSpace puts the escape character before each whitespace
// character of s that is outside quotes and not already escaped, following
// BuildKit's word splitting.
func escapeUnquotedSpace(s string, escape rune) string {
	var b strings.Builder
	var quote rune
	escaped := false
	for _, ch := range s {
		switch {
		case escaped:
			escaped = false
		case quote == 0 && unicode.IsSpace(ch):
			b.WriteRune(escape)
		case quote == 0 && (ch == '\'' || ch == '"'):
			quote = ch
		case quote == 0 && ch == escape:
			escaped = true
		case quote == 0:
		case ch == quote:
			quote = 0
		case ch == escape && quote != '\'':
			escaped = true
		}
		b.WriteRune(ch)
	}
	return b.String()
}

// formatWords formats instructions whose arguments are words (ARG, LABEL,
// USER, ...), keeping their line breaks, re-indenting continuation lines and
// separating words by single spaces.
func formatWords(n *node, c *Config) string {
	if n.Attributes["json"] && !n.hasCommentLines() {
		// VOLUME's JSON form.
		return n.keyword() + " " + marshalJSONStringArray(n.args()) + "\n"
	}
	content, ok := n.content()
	if !ok {
		return n.keyword() + "\n"
	}
	return n.keyword() + " " + reindentContinuations(content, c.indent(), n.context().escape, true)
}

// formatHealthcheck formats HEALTHCHECK [flags] CMD command, whose command is
// formatted as CMD's is, and HEALTHCHECK NONE.
func formatHealthcheck(n *node, c *Config) string {
	region, ok := scanFlagsRegion(n)
	if !ok || n.next == nil || n.hasCommentLines() {
		return n.source
	}
	content := region.content
	end := wordEnd(content, 0, n.context().escape)
	kind, body := strings.ToUpper(content[:end]), strings.TrimLeft(content[end:], " \t")
	switch {
	case kind == "NONE" && strings.TrimSpace(body) == "":
		content = "NONE\n"
	case kind != "CMD" || strings.TrimSpace(body) == "":
		return n.source
	case n.Attributes["json"]:
		content = "CMD " + marshalJSONStringArray(n.args()[1:]) + "\n"
	case n.context().formatsShell():
		content = "CMD " + trimLeadingContinuation(shellBodyOrAsWritten(body, c), n.context().escape)
	default:
		content = "CMD " + body
	}
	flags := region.flags()
	if region.contentOnOwnLine && len(flags) > 0 {
		// Keep CMD on its own line, as it is usually written when there
		// are several flags.
		return n.keyword() + " " + strings.Join(flags, " ") + " " + string(n.context().escape) + "\n" + c.indent() + content
	}
	return n.keyword() + " " + layoutFlags(flags, content, c, n.context().escape, false)
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
// flags and space-separated words (COPY, ADD, EXPOSE, FROM, WORKDIR). If set,
// normalize rewrites the words in place.
func spaceSeparated(mode multilineMode, normalize func(words []string)) directiveFormatter {
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
			if normalize != nil {
				normalize(args)
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

// upperFromAs writes the AS of "FROM image AS name" in the case of the FROM
// keyword, which the formatter writes in upper case (BuildKit's FromAsCasing
// check).
func upperFromAs(words []string) {
	if len(words) == 3 && strings.EqualFold(words[1], "AS") {
		words[1] = "AS"
	}
}

// lowerExposeProtocols writes EXPOSE protocols in lower case, as BuildKit
// reads them (its ExposeProtoCasing check).
func lowerExposeProtocols(words []string) {
	for i, w := range words {
		words[i] = lowerExposeProtocol(w)
	}
}

// lowerExposeProtocol lowers the protocol of a port spec such as "80/TCP".
// A protocol given by a variable is left alone.
func lowerExposeProtocol(port string) string {
	p, proto, ok := strings.Cut(port, "/")
	switch strings.ToLower(proto) {
	case "tcp", "udp", "sctp":
		if ok {
			return p + "/" + strings.ToLower(proto)
		}
	}
	return port
}

// safeAsWords reports whether JSON-form arguments can be written in the
// space-separated form, which BuildKit splits on whitespace: none may be empty
// or contain whitespace. Arguments with backslashes, such as Windows paths,
// keep the JSON form too: as words, a backslash could escape what follows, or
// be the escape character.
func safeAsWords(args []string) bool {
	for _, a := range args {
		if a == "" || strings.ContainsAny(a, " \t\n\r\v\f\\`") {
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
