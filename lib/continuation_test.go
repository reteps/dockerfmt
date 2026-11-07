package lib

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestScanContinuations(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		escape rune
		starts []lineStart
	}{
		{"single line", "A=1\n", '\\', []lineStart{startAfterSpace}},
		{"unquoted continuation", "A=1 \\\n  B=2\n", '\\', []lineStart{startAfterSpace, startAfterSpace}},
		{"double-quoted break", "A=\"x \\\ny\"\n", '\\', []lineStart{startAfterSpace, startInQuote}},
		{"single-quoted break", "A='x \\\ny'\n", '\\', []lineStart{startAfterSpace, startInQuote}},
		{"quote closed before break", "A=\"x\" \\\n  B=2\n", '\\', []lineStart{startAfterSpace, startAfterSpace}},
		{"break in the middle of a word", "A=foo\\\nbar\n", '\\', []lineStart{startAfterSpace, startAfterWord}},
		{"escaped quote outside quotes", "A=it\\'s \\\n  B=2\n", '\\', []lineStart{startAfterSpace, startAfterSpace}},
		{"escaped quote inside double quotes", "A=\"say \\\"hi\\\" \\\n  there\"\n", '\\', []lineStart{startAfterSpace, startInQuote}},
		{"backslash is literal in single quotes", "A='x\\' \\\n  B=2\n", '\\', []lineStart{startAfterSpace, startAfterSpace}},
		{"escaped space ends a line", "A=x\\ \\\n  B=2\n", '\\', []lineStart{startAfterSpace, startAfterWord}},
		{
			"mixed breaks",
			"A=\"x \\\n  y\" \\\n  B='z \\\nw'\n", '\\',
			[]lineStart{startAfterSpace, startInQuote, startAfterSpace, startInQuote},
		},
		{
			"comment and blank lines are dropped",
			"A=\"x \\\n  # comment\n\n  y\"\n", '\\',
			[]lineStart{startAfterSpace, startDropped, startDropped, startInQuote},
		},
		{"backtick escape", "A=\"x `\ny\" `\n  B=2\n", '`', []lineStart{startAfterSpace, startInQuote, startAfterSpace}},
		{"backslash is literal with backtick escape", "A=\"C:\\\" `\n  B=2\n", '`', []lineStart{startAfterSpace, startAfterSpace}},
		{"escaped quote with backtick escape", "A=\"`\" `\n  B\"\n", '`', []lineStart{startAfterSpace, startInQuote}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var starts []lineStart
			for _, cl := range scanContinuations(tt.raw, tt.escape) {
				starts = append(starts, cl.start)
			}
			assert.Equal(t, tt.starts, starts)
		})
	}
}

func TestReindentContinuations(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		trimRight bool
		expected  string
	}{
		{"single line", "  A=1  \n", true, "A=1\n"},
		{"unquoted lines re-indented", "A=1 \\\n        B=2\n", true, "A=1 \\\n    B=2\n"},
		{"tab indentation re-indented", "A=1 \\\n\tB=2\n", true, "A=1 \\\n    B=2\n"},
		{"quoted line kept", "MSG=\"line one \\\nline two\"\n", true, "MSG=\"line one \\\nline two\"\n"},
		{"indented quoted line kept", "MSG='a \\\n        b'\n", true, "MSG='a \\\n        b'\n"},
		{"whitespace before the backslash kept", "MSG=\"a   \\\n  b\"\n", true, "MSG=\"a   \\\n  b\"\n"},
		{"whitespace after the backslash dropped", "MSG=\"a \\   \n  b\"   \n", true, "MSG=\"a \\\n  b\"\n"},
		{"whitespace kept without trimRight", "MSG=\"a \\   \n  b\"   \n", false, "MSG=\"a \\   \n  b\"   \n"},
		{"escaped trailing space kept", "A=x\\ \n", true, "A=x\\ \n"},
		{"unterminated quote keeps trailing space", "A=\"x \n", true, "A=\"x \n"},
		{
			"mixed breaks",
			"A=\"x \\\n  y\" \\\n      B=2\n", true,
			"A=\"x \\\n  y\" \\\n    B=2\n",
		},
		{
			"comments re-indented",
			"A=1 \\\n  # comment  \n        B=2\n", true,
			"A=1 \\\n    # comment\n    B=2\n",
		},
		{"blank line emptied", "A=1 \\\n   \n  B=2\n", true, "A=1 \\\n\n    B=2\n"},
		{"word continued on next line kept", "A=foo\\\nbar\n", true, "A=foo\\\nbar\n"},
		{"word separated by leading whitespace re-indented", "A=x\\ \\\n  B=2\n", true, "A=x\\ \\\n    B=2\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, reindentContinuations(tt.raw, 4, '\\', tt.trimRight))
		})
	}

	t.Run("no indentation keeps a separating space", func(t *testing.T) {
		assert.Equal(t, "A=x\\ \\\n  B=2\n", reindentContinuations("A=x\\ \\\n  B=2\n", 0, '\\', true))
		assert.Equal(t, "A=1 \\\nB=2\n", reindentContinuations("A=1 \\\n  B=2\n", 0, '\\', true))
	})
	t.Run("backtick escape", func(t *testing.T) {
		raw := "A=\"C:\\ `\n  x\" `\n      B=`\"\n"
		assert.Equal(t, "A=\"C:\\ `\n  x\" `\n    B=`\"\n", reindentContinuations(raw, 4, '`', true))
	})
}

func TestContinuationEscape(t *testing.T) {
	assert.Equal(t, '\\', continuationEscape("ENV A=1\n"))
	assert.Equal(t, '\\', continuationEscape("ENV A=1 \\\n  B=2\n"))
	assert.Equal(t, '`', continuationEscape("ENV A=1 `\n  B=2\n"))
	assert.Equal(t, '`', continuationEscape("ENV A=1 `  \n  B=2\n"))
	// A single-line instruction ending in a backtick doesn't use it as escape.
	assert.Equal(t, '\\', continuationEscape("ENV A=`x`\n"))
}
