package cmd

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// complete runs cobra's hidden __complete command (what the generated shell
// completion scripts call) and returns the suggestions and the directive.
func complete(t *testing.T, args ...string) ([]string, cobra.ShellCompDirective) {
	t.Helper()
	out := new(bytes.Buffer)
	rootCmd.SetOut(out)
	rootCmd.SetErr(new(bytes.Buffer))
	rootCmd.SetArgs(append([]string{cobra.ShellCompRequestCmd}, args...))
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})
	require.NoError(t, rootCmd.Execute())

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	require.NotEmpty(t, lines)
	var directive int
	_, err := fmt.Sscanf(lines[len(lines)-1], ":%d", &directive)
	require.NoError(t, err)
	return lines[:len(lines)-1], cobra.ShellCompDirective(directive)
}

func TestCompleteIndentSuggestsNumbers(t *testing.T) {
	for _, flag := range []string{"-i", "--indent"} {
		t.Run(flag, func(t *testing.T) {
			suggestions, directive := complete(t, flag, "")
			assert.Equal(t, []string{"2", "4", "8"}, suggestions)
			assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive,
				"indent takes a number; it must not fall back to filename completion")
		})
	}
}

func TestCompletePositionalArgsAllowsFiles(t *testing.T) {
	_, directive := complete(t, "")
	assert.Equal(t, cobra.ShellCompDirectiveDefault, directive)

	_, directive = complete(t, "Dockerfile", "")
	assert.Equal(t, cobra.ShellCompDirectiveDefault, directive)
}
