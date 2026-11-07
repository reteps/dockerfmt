package lib

import "strings"

// withFlags puts flags before content. Each flag goes on its own continuation
// line if any of them is a --mount flag, which tend to be long.
func withFlags(flags []string, content string, c *Config) string {
	return layoutFlags(flags, content, c, hasMountFlag(flags))
}

// layoutFlags puts flags before content, either on one line or each on its
// own continuation line.
func layoutFlags(flags []string, content string, c *Config, onOwnLines bool) string {
	if len(flags) == 0 {
		return content
	}
	if !onOwnLines {
		return strings.Join(flags, " ") + " " + content
	}
	indent := c.indent()
	var b strings.Builder
	for _, flag := range flags {
		b.WriteString(flag + " \\\n" + indent)
	}
	b.WriteString(trimLeadingContinuation(content))
	return b.String()
}

// trimLeadingContinuation drops a leading "\" continuation and the indentation
// after it. Formatted shell content starts with one when its first line is a
// comment; after a flag line, which already ends in "\", it would be doubled.
func trimLeadingContinuation(content string) string {
	if rest, ok := strings.CutPrefix(content, "\\\n"); ok {
		return strings.TrimLeft(rest, " \t")
	}
	return content
}

func hasMountFlag(flags []string) bool {
	for _, f := range flags {
		if strings.HasPrefix(f, "--mount") {
			return true
		}
	}
	return false
}

// flagItem is one line-level element of the flags region, the part of an
// instruction between its keyword and its content.
type flagItem struct {
	text string
	kind flagItemKind
}

type flagItemKind int

const (
	flagItemFlag flagItemKind = iota
	flagItemComment
	flagItemSeparator // a line holding only "\"
)

// scanFlags walks n's source from the keyword through its flags and returns
// the flags interleaved with the comment and separator lines among them, plus
// separators right after the last flag. BuildKit drops those lines, so they are
// recovered from the source. ok is false if n has no flags or they can't be
// found in the source.
func scanFlags(n *node) (items []flagItem, ok bool) {
	if len(n.Flags) == 0 {
		return nil, false
	}
	rest := strings.TrimLeft(n.text(), " \t")
	i := strings.IndexAny(rest, " \t\\\n")
	if i < 0 {
		return nil, false
	}
	rest = rest[i:]

	flags := n.Flags
	atLineStart := false
	for {
		rest = strings.TrimLeft(rest, " \t")
		switch {
		case strings.HasPrefix(rest, "\\\n"):
			if atLineStart {
				items = append(items, flagItem{"\\", flagItemSeparator})
			}
			rest = rest[2:]
			atLineStart = true
			continue
		case atLineStart && strings.HasPrefix(rest, "\n"):
			rest = rest[1:] // blank line inside the continuation
			continue
		case len(flags) > 0 && atLineStart && strings.HasPrefix(rest, "#"):
			// A comment after the last flag belongs to the content.
			line, after, _ := strings.Cut(rest, "\n")
			items = append(items, flagItem{strings.TrimRight(line, " \t"), flagItemComment})
			rest = after
			continue
		case len(flags) > 0 && strings.HasPrefix(rest, flags[0]):
			items = append(items, flagItem{flags[0], flagItemFlag})
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

// withFlagLines is withFlags for an instruction whose flags region has comment
// or separator lines: each flag, comment and separator goes on its own line.
// ok is false if there are no such lines, so the usual layout applies.
func withFlagLines(n *node, content string, c *Config) (string, bool) {
	items, ok := scanFlags(n)
	if !ok {
		return "", false
	}
	onlyFlags := true
	for _, item := range items {
		if item.kind != flagItemFlag {
			onlyFlags = false
			break
		}
	}
	if onlyFlags {
		return "", false
	}

	indent := c.indent()
	var b strings.Builder
	if items[0].kind != flagItemFlag {
		// A comment or separator can't share the keyword's line.
		b.WriteString("\\\n" + indent)
	}
	for _, item := range items {
		b.WriteString(item.text)
		if item.kind == flagItemFlag {
			b.WriteString(" \\")
		}
		b.WriteString("\n" + indent)
	}
	b.WriteString(trimLeadingContinuation(content))
	return b.String(), true
}
