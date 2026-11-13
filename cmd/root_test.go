package cmd

import (
	"bytes"
	"fmt"
	"io"
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
	var out bytes.Buffer
	require.Equal(t, 0, execute(append([]string{cobra.ShellCompRequestCmd}, args...), strings.NewReader(""), &out, io.Discard))

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

func TestFlagErrorsGoToStderr(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := execute([]string{"--bogus"}, strings.NewReader(""), &stdout, &stderr)
	assert.Equal(t, 1, code)
	assert.Empty(t, stdout.String())
	assert.Equal(t, "dockerfmt: unknown flag: --bogus\nRun 'dockerfmt --help' for usage.\n", stderr.String())
}

func TestVersion(t *testing.T) {
	old := Version
	t.Cleanup(func() { Version = old })
	Version = "v1.2.3"

	var stdout bytes.Buffer
	code := execute([]string{"version"}, strings.NewReader(""), &stdout, io.Discard)
	assert.Equal(t, 0, code)
	assert.Equal(t, "dockerfmt v1.2.3\n", stdout.String())
}

func TestCompleteVersionOffersNoFiles(t *testing.T) {
	_, directive := complete(t, "version", "")
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
}
