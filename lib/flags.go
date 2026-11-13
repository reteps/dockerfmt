package lib

import (
	"strings"
	"unicode"
)

// flagsRegion is the part of an instruction between its keyword and its
// content, as written. BuildKit's parse tree has the flags with their quotes
// removed, and none of the comment or separator lines among them.
type flagsRegion struct {
	// items are the flags as written, with the comment and separator lines
	// among them and any separators right after the last flag.
	items []flagItem
	// endOfFlags reports a "--" after the flags, which lets content start
	// with "--".
	endOfFlags bool
	// content is the source after the region. Its first line is trimmed.
	content string
	// contentOnOwnLine reports that content starts on a line after the last
	// flag.
	contentOnOwnLine bool
}

type flagItem struct {
	text string
	kind flagItemKind
}

type flagItemKind int

const (
	flagItemFlag flagItemKind = iota
	flagItemComment
	flagItemSeparator // a line holding only the escape character
)

// flags returns the flags as written, followed by "--" if there was one.
func (r *flagsRegion) flags() []string {
	var flags []string
	for _, item := range r.items {
		if item.kind == flagItemFlag {
			flags = append(flags, item.text)
		}
	}
	if r.endOfFlags {
		flags = append(flags, "--")
	}
	return flags
}

// hasLines reports whether there are comment or separator lines among the
// flags.
func (r *flagsRegion) hasLines() bool {
	for _, item := range r.items {
		if item.kind != flagItemFlag {
			return true
		}
	}
	return false
}

// scanFlagsRegion reads n's flags region from its source. ok is false if the
// flags can't be read back as written, for example a quoted flag value that
// continues on the next line; the instruction is then best left alone.
func scanFlagsRegion(n *node) (flagsRegion, bool) {
	r, ok := scanFlagLines(n)
	if ok && len(n.Flags) == 0 && !r.endOfFlags {
		// Keep content as written after the keyword, including a leading
		// continuation, which the shell formatter places comments after.
		content, _ := n.content()
		return flagsRegion{content: content}, true
	}
	return r, ok
}

func scanFlagLines(n *node) (r flagsRegion, ok bool) {
	escape := n.context().escape
	lines := strings.SplitAfter(n.text(), "\n")
	read := 0
	for li, line := range lines {
		if line == "" {
			continue
		}
		text := strings.TrimSuffix(line, "\n")
		body, continues := text, false
		if li < len(lines)-1 {
			if j, ok := isContinuation(text, escape); ok {
				body, continues = text[:j], true
			}
		}

		pos := 0
		if li == 0 {
			pos = len(body) - len(strings.TrimLeft(body, " \t"))
			pos += wordEnd(body, pos, escape) // the keyword
		} else {
			switch trimmed := strings.TrimSpace(body); {
			case strings.HasPrefix(strings.TrimSpace(text), "#"):
				if read == len(n.Flags) {
					// A comment after the flags belongs to the content. It
					// is on a continuation line, which the content starts
					// with, as content after the keyword does.
					r.content = string(escape) + "\n" + strings.Join(lines[li:], "")
					return r, true
				}
				r.items = append(r.items, flagItem{strings.TrimSpace(text), flagItemComment})
				continue
			case strings.TrimSpace(text) == "":
				continue // blank lines inside a continuation are dropped
			case trimmed == "" && continues:
				r.items = append(r.items, flagItem{"\\", flagItemSeparator})
				continue
			}
		}

		for {
			pos += len(body[pos:]) - len(strings.TrimLeft(body[pos:], " \t"))
			if pos == len(body) {
				break
			}
			end := pos + wordEnd(body, pos, escape)
			word := body[pos:end]
			if read == len(n.Flags) {
				if word == "--" && !r.endOfFlags {
					r.endOfFlags = true
					pos = end
					continue
				}
				r.content = text[pos:] + strings.Join(lines[li:], "")[len(text):]
				r.contentOnOwnLine = li > 0 && pos == len(text)-len(trimIndent(text))
				return r, true
			}
			if !strings.HasPrefix(word, "--") || end == len(body) && continues && gluesToNext(text, lines[li+1:]) {
				// Not a flag, or a flag joined to the next line's first word.
				return flagsRegion{}, false
			}
			r.items = append(r.items, flagItem{n.context().lowerStageRefs(word), flagItemFlag})
			read++
			pos = end
		}
		if !continues {
			break
		}
	}
	return r, read == len(n.Flags)
}

// wordEnd returns the length of the word starting at s[pos:], following
// BuildKit's word splitting: whitespace separates words except inside quotes,
// and the escape character escapes the next character except inside single
// quotes. An unterminated quote extends the word to the end of s.
func wordEnd(s string, pos int, escape rune) int {
	var quote rune
	escaped := false
	for i, ch := range s[pos:] {
		switch {
		case escaped:
			escaped = false
		case ch == escape && quote != '\'':
			escaped = true
		case quote != 0:
			if ch == quote {
				quote = 0
			}
		case ch == '\'' || ch == '"':
			quote = ch
		case unicode.IsSpace(ch):
			return i
		}
	}
	return len(s) - pos
}

// gluesToNext reports whether the last word of line, which ends in a
// continuation, runs on into the first word of the next line: BuildKit joins
// the lines without adding whitespace.
func gluesToNext(line string, next []string) bool {
	t := strings.TrimRight(line, " \t")
	t = t[:len(t)-1] // the escape character
	if strings.HasSuffix(t, " ") || strings.HasSuffix(t, "\t") || len(next) == 0 {
		return false
	}
	return next[0] != "" && !strings.ContainsRune(" \t\n", rune(next[0][0]))
}

// argsOnLines reports whether the content after the flags, as written,
// continues onto more lines: its arguments are then laid out one per line.
func (r *flagsRegion) argsOnLines(escape rune) bool {
	lines := strings.Split(strings.TrimRight(trimLeadingContinuation(r.content, escape), "\n"), "\n")
	for _, l := range lines[:len(lines)-1] {
		if _, ok := isContinuation(l, escape); ok && !isDroppedLine(l) {
			return true
		}
	}
	return false
}

// continuedSep returns the separator that puts what follows on a new
// continuation line.
func continuedSep(c *Config, escape rune) string {
	return " " + string(escape) + "\n" + c.indent()
}

// withFlags puts flags before content. Each flag goes on its own continuation
// line if any of them is a --mount flag, which tend to be long.
func withFlags(flags []string, content string, c *Config, escape rune) string {
	return layoutFlags(flags, content, c, escape, hasMountFlag(flags))
}

// layoutFlags puts flags before content, either on one line or each on its
// own continuation line.
func layoutFlags(flags []string, content string, c *Config, escape rune, onOwnLines bool) string {
	if len(flags) == 0 {
		return content
	}
	if !onOwnLines {
		return strings.Join(flags, " ") + " " + content
	}
	next := " " + string(escape) + "\n" + c.indent()
	var b strings.Builder
	for _, flag := range flags {
		b.WriteString(flag + next)
	}
	b.WriteString(trimLeadingContinuation(content, escape))
	return b.String()
}

// withFlagLines lays out a flags region that has comment or separator lines:
// each flag, comment and separator goes on its own line.
func withFlagLines(r *flagsRegion, content string, c *Config, escape rune) string {
	indent := c.indent()
	esc := string(escape)
	var b strings.Builder
	if r.items[0].kind != flagItemFlag {
		// A comment or separator can't share the keyword's line.
		b.WriteString(esc + "\n" + indent)
	}
	for _, item := range r.items {
		switch item.kind {
		case flagItemFlag:
			b.WriteString(item.text + " " + esc)
		case flagItemSeparator:
			b.WriteString(esc)
		default:
			b.WriteString(item.text)
		}
		b.WriteString("\n" + indent)
	}
	if r.endOfFlags {
		b.WriteString("-- ")
	}
	b.WriteString(trimLeadingContinuation(content, escape))
	return b.String()
}

// trimLeadingContinuation drops a leading continuation and the indentation
// after it. Formatted shell content starts with one when its first line is a
// comment; after a flag line, which already ends in one, it would be doubled.
func trimLeadingContinuation(content string, escape rune) string {
	if rest, ok := strings.CutPrefix(content, string(escape)+"\n"); ok {
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
