package lib

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/moby/buildkit/frontend/dockerfile/parser"
)

const utf8BOM = "\xEF\xBB\xBF"

// formatContent formats a whole Dockerfile. The formatter works on "\n"
// line endings and a file without a byte order mark; both are restored
// afterwards. The output uses "\r\n" if most of the input's lines do, so a
// file that mixes line endings gets the dominant one throughout.
//
// BuildKit keeps the "\r" of a "\r\n" in heredoc bodies and quoted values,
// so a file that consistently uses "\r\n" reads the same before and after.
func formatContent(content string, c *Config) (string, error) {
	bom := strings.HasPrefix(content, utf8BOM)
	content = strings.TrimPrefix(content, utf8BOM)
	crlf := usesCRLF(content)
	content = strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.SplitAfter(strings.TrimSuffix(content, "\n"), "\n")

	out, err := formatLines(lines, c)
	if err != nil {
		return "", err
	}
	if crlf {
		out = strings.ReplaceAll(out, "\n", "\r\n")
	}
	if bom {
		out = utf8BOM + out
	}
	return out, nil
}

// usesCRLF reports whether content's line endings are mostly "\r\n". A tie is
// decided by the first line ending.
func usesCRLF(content string) bool {
	crlf := strings.Count(content, "\r\n")
	lf := strings.Count(content, "\n") - crlf
	if crlf != lf {
		return crlf > lf
	}
	i := strings.IndexByte(content, '\n')
	return i > 0 && content[i-1] == '\r'
}

// checkEncoding returns an error if content is not UTF-8 text: a UTF-16 or
// UTF-32 byte order mark, a NUL byte, or an invalid UTF-8 sequence.
func checkEncoding(content string) error {
	// The UTF-32 LE mark starts with the UTF-16 LE one, so check it first.
	const powershell = ` (Windows PowerShell's ">" writes UTF-16 by default)`
	for _, bom := range []struct{ mark, name, hint string }{
		{"\x00\x00\xFE\xFF", "UTF-32 (big-endian)", ""},
		{"\xFF\xFE\x00\x00", "UTF-32 (little-endian)", ""},
		{"\xFE\xFF", "UTF-16 (big-endian)", powershell},
		{"\xFF\xFE", "UTF-16 (little-endian)", powershell},
	} {
		if strings.HasPrefix(content, bom.mark) {
			return fmt.Errorf("file is encoded as %s, not UTF-8; re-save it as UTF-8%s", bom.name, bom.hint)
		}
	}
	if i := strings.IndexByte(content, 0); i >= 0 {
		return fmt.Errorf("line %d: NUL byte found; the file is binary or not UTF-8 (UTF-16?), re-save it as UTF-8",
			lineAt(content, i))
	}
	for i := 0; i < len(content); {
		r, size := utf8.DecodeRuneInString(content[i:])
		if r == utf8.RuneError && size == 1 {
			return fmt.Errorf("line %d: invalid UTF-8; re-save the file as UTF-8", lineAt(content, i))
		}
		i += size
	}
	return nil
}

// lineAt returns the 1-based line number of byte offset i in content.
func lineAt(content string, i int) int {
	return strings.Count(content[:i], "\n") + 1
}

// parseError adds the line number to a BuildKit parse error when it points at
// one, such as an unterminated heredoc.
func parseError(err error) error {
	var le *parser.LocationError
	if errors.As(err, &le) && len(le.Locations) > 0 && len(le.Locations[0]) > 0 {
		// "file with no instructions" points past the end of the file.
		if line := le.Locations[0][0].Start.Line; line > 0 && !strings.Contains(err.Error(), "no instructions") {
			return fmt.Errorf("line %d: %w", line, err)
		}
	}
	return err
}
