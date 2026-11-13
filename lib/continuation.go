package lib

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Quote-aware re-indentation of continuation lines, for instructions whose
// arguments BuildKit splits into words (ENV, LABEL, ARG, ...).
//
// BuildKit joins an instruction's continuation lines by dropping the escape
// character at the end of each line and appending the next line as written,
// leading whitespace included. Outside quotes that whitespace only separates
// words, so it can be replaced by the configured indentation. Inside a quoted
// value it is part of the value:
//
//	ENV MSG="line one \
//	line two"
//
// sets MSG to "line one line two", and indenting "line two" would add spaces
// to it. So a line that starts inside quotes is kept as written.

// lineStart classifies how BuildKit reads the start of a physical line of an
// instruction.
type lineStart int

const (
	// startAfterSpace: outside quotes, and the text joined before this line
	// ends in whitespace that separates words, so the line's leading
	// whitespace (if any) doesn't matter.
	startAfterSpace lineStart = iota
	// startAfterWord: outside quotes, but the text joined before this line
	// ends in the middle of a word. Leading whitespace separates the line
	// from that word; without any, the line continues the word.
	startAfterWord
	// startInQuote: inside a quoted string, so leading whitespace is part of
	// the value.
	startInQuote
	// startDropped: a comment or blank line, which BuildKit drops.
	startDropped
)

// continuationLine is one physical line of an instruction.
type continuationLine struct {
	// text is the line as written, without its trailing "\n".
	text string
	// newline reports whether the line ended in "\n".
	newline bool
	start   lineStart
	// keep is the length of the prefix of text that must be kept: what
	// follows it is whitespace BuildKit ignores (after a line continuation,
	// or between words at the end of the instruction).
	keep int
	// gaps are the byte ranges of the whitespace between words within the
	// line, not counting its indentation; a gap before a line continuation
	// is the last.
	gaps [][2]int
	// continues reports whether the line ends in a line continuation.
	continues bool
}

// isContinuation reports whether line (without its "\n") ends in a line
// continuation, using BuildKit's rule: the escape character, possibly
// followed by spaces or tabs, and not preceded by another escape character
// (a line that is only the escape character also counts). It returns the
// index of the escape character.
func isContinuation(line string, escape rune) (int, bool) {
	t := strings.TrimRight(line, " \t")
	esc := string(escape)
	if !strings.HasSuffix(t, esc) {
		return 0, false
	}
	i := len(t) - len(esc)
	if i > 0 && strings.HasSuffix(t[:i], esc) {
		return 0, false
	}
	return i, true
}

// scanContinuations splits raw, an instruction's source (or the part after
// its keyword, which must start between words), into physical lines and
// reports for each line whether it starts inside a quoted string, following
// BuildKit's word splitting (see parseWords in BuildKit's parser): single and
// double quotes, with the escape character escaping the next character
// outside quotes and inside double quotes.
func scanContinuations(raw string, escape rune) []continuationLine {
	var lines []continuationLine
	var quote rune  // the open quote character, or 0
	atSpace := true // the joined text so far ends in word-separating whitespace
	escaped := false
	rawLines := strings.SplitAfter(raw, "\n")
	if rawLines[len(rawLines)-1] == "" {
		rawLines = rawLines[:len(rawLines)-1]
	}
	for i, l := range rawLines {
		text, newline := strings.CutSuffix(l, "\n")
		cl := continuationLine{text: text, newline: newline}
		if i > 0 {
			if trimmed := strings.TrimLeftFunc(text, unicode.IsSpace); trimmed == "" || trimmed[0] == '#' {
				// BuildKit drops comment and blank lines inside a continuation.
				cl.start = startDropped
				cl.keep = len(strings.TrimRightFunc(text, unicode.IsSpace))
				if trimmed == "" {
					cl.keep = 0
				}
				lines = append(lines, cl)
				continue
			}
		}
		switch {
		case quote != 0:
			cl.start = startInQuote
		case atSpace:
			cl.start = startAfterSpace
		default:
			cl.start = startAfterWord
		}

		// The part of the line BuildKit appends to the joined text.
		segment := text
		cont := false
		if i < len(rawLines)-1 {
			if j, ok := isContinuation(text, escape); ok {
				segment = text[:j]
				cl.keep = j + utf8.RuneLen(escape)
				cont = true
			}
		}
		cl.continues = cont
		gapStart, inWord := -1, false
		for pos, ch := range segment {
			sep := false
			switch {
			case escaped:
				escaped = false
			case quote == 0 && unicode.IsSpace(ch):
				sep = true
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
			atSpace = sep
			if !sep && !cont {
				cl.keep = pos + utf8.RuneLen(ch)
			}
			switch {
			case sep && inWord && gapStart < 0:
				gapStart = pos
			case !sep && gapStart >= 0:
				cl.gaps = append(cl.gaps, [2]int{gapStart, pos})
				gapStart = -1
			}
			inWord = inWord || !sep
		}
		if gapStart >= 0 && cont {
			cl.gaps = append(cl.gaps, [2]int{gapStart, len(segment)})
		}
		if !cont && quote != 0 {
			// An unterminated quote runs to the end of the instruction, so
			// trailing whitespace is part of the value.
			cl.keep = len(text)
		}
		lines = append(lines, cl)
	}
	return lines
}

// reindentContinuations re-indents the continuation lines of raw, the text
// after an instruction's keyword, to indent, and trims the first
// line's leading whitespace. Lines that start inside a quoted value, or in
// the middle of a word, keep their leading whitespace as written.
//
// With words, for instructions whose arguments BuildKit splits into words on
// whitespace outside quotes (ENV, LABEL, ARG, ...), the whitespace between
// words becomes a single space, a space goes before each line continuation
// that ends a word, and whitespace BuildKit ignores at the end of lines is
// removed.
func reindentContinuations(raw, indent string, escape rune, words bool) string {
	lines := scanContinuations(raw, escape)
	var b strings.Builder
	b.Grow(len(raw))
	for i, cl := range lines {
		text := cl.text
		if words {
			text = wordLine(cl, nextStartsAfterGap(lines[i+1:]), escape)
		}
		switch {
		case i == 0:
			text = trimIndent(text)
		case cl.start == startDropped:
			trimmed := trimIndent(text)
			if trimmed == "" {
				// A blank line, which BuildKit warns about
				// (NoEmptyContinuation).
				continue
			}
			text = indent + trimmed
		case cl.start == startAfterSpace:
			text = indent + trimIndent(text)
		case cl.start == startAfterWord:
			// Leading whitespace separates this line from the previous
			// word; re-indenting must keep some, and must not add any.
			if indent != "" && trimIndent(text) != text {
				text = indent + trimIndent(text)
			}
		case cl.start == startInQuote:
			// Part of a quoted value: keep as written.
		}
		b.WriteString(text)
		if cl.newline {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// wordLine returns the text of cl with each gap between words made a single
// space and the whitespace BuildKit ignores at its end removed. If the line
// ends in a word and a continuation, and the next line starts with separating
// whitespace (nextGap), a space is put before the continuation.
func wordLine(cl continuationLine, nextGap bool, escape rune) string {
	text := cl.text
	if !createsContinuation(text, text[:cl.keep], escape) {
		text = text[:cl.keep]
	}
	if cl.start == startDropped {
		return text
	}
	if end := len(text) - utf8.RuneLen(escape); cl.continues && nextGap && end > 0 {
		if last, _ := utf8.DecodeLastRuneInString(text[:end]); !unicode.IsSpace(last) {
			text = text[:end] + " " + text[end:]
		}
	}
	for i := len(cl.gaps) - 1; i >= 0; i-- {
		g := cl.gaps[i]
		text = text[:g[0]] + " " + text[g[1]:]
	}
	return text
}

// nextStartsAfterGap reports whether the first of lines that BuildKit keeps
// starts with whitespace that separates it from the word before it.
func nextStartsAfterGap(lines []continuationLine) bool {
	for _, cl := range lines {
		if cl.start == startDropped {
			continue
		}
		return cl.start == startAfterWord && trimIndent(cl.text) != cl.text
	}
	return false
}

// createsContinuation reports whether trimming line to trimmed would make it
// end in a line continuation: BuildKit only counts spaces and tabs after the
// escape character, and other whitespace, such as "\r", ends the line.
func createsContinuation(line, trimmed string, escape rune) bool {
	_, was := isContinuation(line, escape)
	_, is := isContinuation(trimmed, escape)
	return is && !was
}

// trimIndent removes leading whitespace. BuildKit splits words on any
// Unicode whitespace, so all of it is indentation.
func trimIndent(s string) string {
	return strings.TrimLeftFunc(s, unicode.IsSpace)
}
