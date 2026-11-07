package lib

import (
	"log"
	"strings"
)

// FormatFileLines formats a Dockerfile given as lines that each keep their
// line ending (as returned by strings.SplitAfter(content, "\n")). It exits the
// process if the file can't be parsed.
func FormatFileLines(fileLines []string, c *Config) string {
	content := strings.Join(fileLines, "")
	out, err := formatContent(content, c)
	if err != nil {
		log.Printf("%s\n", content)
		log.Fatalf("Error parsing file: %v", err)
	}
	return out
}

// formatContent formats a whole Dockerfile. The output uses the input's line
// ending: "\r\n" if most of its lines end in "\r\n", "\n" otherwise.
//
// BuildKit keeps the "\r" of a "\r\n" inside heredoc bodies and quoted
// strings, so the formatter works on "\n" only and the line ending is restored
// afterwards; a file that consistently uses "\r\n" therefore reads the same to
// BuildKit before and after formatting. A file that mixes line endings is
// converted to the dominant one throughout.
func formatContent(content string, c *Config) (string, error) {
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
