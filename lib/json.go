package lib

import (
	"fmt"
	"strings"
)

// marshalJSONStringArray writes items as a JSON array in the style of a
// Dockerfile exec form: ["a", "b"]. Unlike encoding/json, it doesn't escape
// <, > and & for HTML.
func marshalJSONStringArray(items []string) string {
	return marshalJSONStringArraySep(items, " ")
}

// marshalJSONStringArraySep is marshalJSONStringArray with sep after each
// comma, such as a line continuation.
func marshalJSONStringArraySep(items []string, sep string) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, item := range items {
		if i > 0 {
			b.WriteString("," + sep)
		}
		writeJSONString(&b, item)
	}
	b.WriteByte(']')
	return b.String()
}

// writeJSONString writes s to b as a JSON string literal.
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
