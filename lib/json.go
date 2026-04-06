// Since our JSON needs are limited to []string arrays (Dockerfile JSON-form
// directives like CMD ["ls", "-la"]), we implement just that here.
//
// This also preserves the non-HTML-escaping behavior: Go's json.Marshal
// escapes <, >, & as \uXXXX for HTML safety, which we don't want.
package lib

import (
	"fmt"
	"strconv"
	"strings"
)

// unmarshalJSONStringArray parses a JSON array of strings.
// Returns the parsed strings and true on success, or nil and false if
// the input is not a valid JSON array of strings.
func unmarshalJSONStringArray(data string) ([]string, bool) {
	s := strings.TrimSpace(data)
	if len(s) < 2 || s[0] != '[' || s[len(s)-1] != ']' {
		return nil, false
	}
	inner := strings.TrimSpace(s[1 : len(s)-1])
	if inner == "" {
		return []string{}, true
	}

	var result []string
	pos := 0
	for {
		pos = skipWhitespace(inner, pos)
		if pos >= len(inner) {
			break
		}
		str, newPos, ok := parseJSONString(inner, pos)
		if !ok {
			return nil, false
		}
		result = append(result, str)
		pos = skipWhitespace(inner, newPos)
		if pos >= len(inner) {
			break
		}
		if inner[pos] != ',' {
			return nil, false
		}
		pos++
	}
	return result, true
}

func skipWhitespace(s string, pos int) int {
	for pos < len(s) && (s[pos] == ' ' || s[pos] == '\t' || s[pos] == '\n' || s[pos] == '\r') {
		pos++
	}
	return pos
}

// parseJSONString parses a JSON string starting at pos.
// Returns the unescaped string, the position after the closing quote, and success.
func parseJSONString(s string, pos int) (string, int, bool) {
	if pos >= len(s) || s[pos] != '"' {
		return "", pos, false
	}
	pos++
	var b strings.Builder
	for pos < len(s) {
		ch := s[pos]
		if ch == '"' {
			return b.String(), pos + 1, true
		}
		if ch == '\\' {
			pos++
			if pos >= len(s) {
				return "", pos, false
			}
			switch s[pos] {
			case '"', '\\', '/':
				b.WriteByte(s[pos])
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case 'b':
				b.WriteByte('\b')
			case 'f':
				b.WriteByte('\f')
			case 'u':
				if pos+4 >= len(s) {
					return "", pos, false
				}
				val, err := strconv.ParseUint(s[pos+1:pos+5], 16, 16)
				if err != nil {
					return "", pos, false
				}
				b.WriteRune(rune(val))
				pos += 4
			default:
				return "", pos, false
			}
		} else if ch < 0x20 {
			return "", pos, false
		} else {
			b.WriteByte(ch)
		}
		pos++
	}
	return "", pos, false
}

// marshalJSONStringArray formats a string slice as a JSON array with spaces
// after commas: ["a", "b"]. Unlike encoding/json, it does not escape HTML
// characters (<, >, &).
func marshalJSONStringArray(items []string) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, item := range items {
		if i > 0 {
			b.WriteString(", ")
		}
		writeJSONString(&b, item)
	}
	b.WriteByte(']')
	return b.String()
}

// writeJSONString writes a JSON-escaped string (including surrounding quotes) to b.
func writeJSONString(b *strings.Builder, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			if r < 0x20 {
				fmt.Fprintf(b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
}
