package lib

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSplitShellBody(t *testing.T) {
	body, ok := splitShellBody("echo a \\\n  # one\n\n  \\\n  && echo b")
	assert.True(t, ok)
	assert.Equal(t, "echo a \\\n  && echo b\n", body.src)
	start := len("echo a \\\n  ")
	assert.Equal(t, []shellInsert{
		{anchor: start, text: "# one"},
		{anchor: start, text: "\\"},
	}, body.inserts)

	// On the directive line, a comment is shell code: BuildKit drops only
	// comment lines inside a continuation.
	body, ok = splitShellBody("# c \\\n  echo hi")
	assert.True(t, ok)
	assert.Equal(t, "# c \\\n  echo hi\n", body.src)
	assert.Empty(t, body.inserts)
}

func TestFormatShellBodyCommentPlacement(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{
			// Written deeper than the next command: stays with the arguments.
			"comment indented under arguments",
			"apt-get update \\\n    && apt-get install -y \\\n        curl \\\n        # note\n    && rm x",
			"apt-get update \\\n    && apt-get install -y \\\n        curl \\\n        # note\n    && rm x\n",
		},
		{
			// Written at the level of the next command: goes with it.
			"comment at the next command's level",
			"apt-get update \\\n    && apt-get install -y \\\n        curl \\\n    # note\n    && rm x",
			"apt-get update \\\n    && apt-get install -y \\\n        curl \\\n    # note\n    && rm x\n",
		},
		{
			"comments around an operator stack and share indentation",
			"a \\\n    # one\n&& \\\n# two\nb",
			"a \\\n    # one\n    # two\n    && b\n",
		},
		{
			"comment before the first token",
			"\\\n  # c\n  echo hi",
			"\\\n    # c\n    echo hi\n",
		},
		{
			"separator before a pipe",
			"echo a \\\n  \\\n  | grep a",
			"echo a \\\n    \\\n    | grep a\n",
		},
		{
			"comment that can't be anchored to a token start is left as written",
			"echo good\\\n# c\nnight",
			"echo good\\\n# c\nnight",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := formatShellBody(tt.in, defaultConfig)
			assert.Equal(t, tt.want, got)
			// Idempotent.
			if got != tt.in {
				again, _ := formatShellBody(got, defaultConfig)
				assert.Equal(t, got, again)
			}
		})
	}
}
