// JSON-form directives (CMD ["ls", "-la"]) are decoded by BuildKit's parser;
// we only need to write []string arrays back out, so we implement just that.
//
// This also preserves the non-HTML-escaping behavior: Go's json.Marshal
// escapes <, >, & as \uXXXX for HTML safety, which we don't want.
package lib

import (
	"fmt"
	"strings"
)

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
