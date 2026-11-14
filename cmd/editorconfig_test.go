package cmd

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/reteps/dockerfmt/lib"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parsedFlags returns the root command's flags after parsing args, so tests
// control which flags count as explicitly set.
func parsedFlags(t *testing.T, args ...string) *pflag.FlagSet {
	t.Helper()
	var status int
	flags := newRootCmd(&status).Flags()
	require.NoError(t, flags.Parse(args))
	return flags
}

func mustApplyEditorConfig(t *testing.T, c lib.Config, path string, flags *pflag.FlagSet) lib.Config {
	t.Helper()
	got, warnings := applyEditorConfig(c, path, flags)
	require.Empty(t, warnings)
	return got
}

// writeProject lays out:
//
//	<root>/.editorconfig
//	<root>/Dockerfile
//	<root>/docker/Dockerfile
//	<root>/docker/nested/Dockerfile
//
// and chdirs into <root>.
func writeProject(t *testing.T, editorconfig string) {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, ".editorconfig"), []byte(editorconfig), 0o644))
	for _, dir := range []string{"", "docker", "docker/nested"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, dir), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, dir, "Dockerfile"), []byte("FROM alpine\n"), 0o644))
	}
	t.Chdir(root)
}

func TestEditorConfigAppliesToSubdirectories(t *testing.T) {
	writeProject(t, "root = true\n\n[Dockerfile]\nindent_size = 2\ninsert_final_newline = true\nspace_redirects = true\n")
	base := lib.Config{IndentSize: 4}

	for _, path := range []string{
		"Dockerfile",
		"docker/Dockerfile",
		"./docker/Dockerfile",
		"docker/nested/Dockerfile",
		filepath.Join("docker", "..", "docker", "Dockerfile"),
	} {
		t.Run(path, func(t *testing.T) {
			got := mustApplyEditorConfig(t, base, path, parsedFlags(t))
			assert.Equal(t, uint(2), got.IndentSize)
			assert.True(t, got.TrailingNewline)
			assert.True(t, got.SpaceRedirects)
		})
	}

	t.Run("absolute path", func(t *testing.T) {
		abs, err := filepath.Abs("docker/Dockerfile")
		require.NoError(t, err)
		got := mustApplyEditorConfig(t, base, abs, parsedFlags(t))
		assert.Equal(t, uint(2), got.IndentSize)
	})
}

func TestEditorConfigPathGlobs(t *testing.T) {
	for _, section := range []string{"docker/Dockerfile", "**/Dockerfile", "docker/**", "{Dockerfile,*.dockerfile}"} {
		t.Run(section, func(t *testing.T) {
			writeProject(t, "root = true\n\n["+section+"]\nindent_size = 2\n")
			got := mustApplyEditorConfig(t, lib.Config{IndentSize: 4}, "docker/Dockerfile", parsedFlags(t))
			assert.Equal(t, uint(2), got.IndentSize)
		})
	}
}

func TestEditorConfigExplicitFlagsWin(t *testing.T) {
	writeProject(t, "root = true\n\n[Dockerfile]\nindent_size = 2\ninsert_final_newline = false\n")
	base := lib.Config{IndentSize: 8, TrailingNewline: true}

	got := mustApplyEditorConfig(t, base, "docker/Dockerfile", parsedFlags(t, "--indent=8", "--newline"))
	assert.Equal(t, uint(8), got.IndentSize)
	assert.True(t, got.TrailingNewline)
}

func TestEditorConfigIndentSize(t *testing.T) {
	tests := []struct {
		name     string
		settings string
		want     uint
		warning  string
	}{
		{"number", "indent_size = 2", 2, ""},
		{"upper-case key", "INDENT_SIZE = 2", 2, ""},
		{"tab uses tab_width", "indent_style = tab\nindent_size = tab\ntab_width = 3", 3, ""},
		{"tab without a width", "indent_size = tab", 4, ""},
		{"unset", "indent_size = unset", 4, ""},
		{"not a number", "indent_size = two", 4, "ignoring .editorconfig setting indent_size = two: must be a number from 1 to 16"},
		{"too large", "indent_size = 40", 4, "ignoring .editorconfig setting indent_size = 40: must be a number from 1 to 16"},
		{"bad tab width", "indent_size = tab\ntab_width = 0", 4, "ignoring .editorconfig setting tab_width = 0: must be a number from 1 to 16"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writeProject(t, "root = true\n\n[Dockerfile]\n"+tt.settings+"\n")
			got, warnings := applyEditorConfig(lib.Config{IndentSize: 4}, "Dockerfile", parsedFlags(t))
			if tt.warning == "" {
				assert.Empty(t, warnings)
			} else {
				assert.Equal(t, []string{tt.warning}, warnings)
			}
			assert.Equal(t, tt.want, got.IndentSize)
		})
	}
}

func TestEditorConfigReportsEveryInvalidSetting(t *testing.T) {
	writeProject(t, "root = true\n\n[Dockerfile]\nindent_size = abc\nSPACE_REDIRECTS = maybe\n")
	got, warnings := applyEditorConfig(lib.Config{IndentSize: 4}, "Dockerfile", parsedFlags(t))
	assert.Equal(t, []string{
		"ignoring .editorconfig setting indent_size = abc: must be a number from 1 to 16",
		"ignoring .editorconfig setting space_redirects = maybe: must be true or false",
	}, warnings)
	assert.Equal(t, uint(4), got.IndentSize)
}

func TestEditorConfigValidSettingsStillApply(t *testing.T) {
	writeProject(t, "root = true\n\n[Dockerfile]\nspace_redirects = maybe\nindent_size = 2\n")
	got, warnings := applyEditorConfig(lib.Config{IndentSize: 4}, "Dockerfile", parsedFlags(t))
	assert.Len(t, warnings, 1)
	assert.Equal(t, uint(2), got.IndentSize)
}

// The library rejects the whole file for some invalid values, as well as for
// syntax errors.
func TestEditorConfigUnreadable(t *testing.T) {
	for _, contents := range []string{
		"root = true\n[Dockerfile\nindent_size = 2\n",
		"root = true\n[Dockerfile]\nindent_size = 2\ninsert_final_newline = perhaps\n",
	} {
		writeProject(t, contents)
		got, warnings := applyEditorConfig(lib.Config{IndentSize: 4}, "Dockerfile", parsedFlags(t))
		require.Len(t, warnings, 1)
		assert.True(t, strings.HasPrefix(warnings[0], "ignoring .editorconfig: "), warnings[0])
		assert.NotContains(t, warnings[0], "\n")
		assert.Equal(t, uint(4), got.IndentSize)
	}
}

func TestCLIWarnsOnceAboutBrokenEditorConfig(t *testing.T) {
	writeProject(t, "root = true\n\n[Dockerfile]\nindent_size = huge\n")
	code, _, stderr := cli("", "-c", "Dockerfile", "docker/Dockerfile")
	assert.Equal(t, 0, code)
	assert.Equal(t, "dockerfmt: warning: ignoring .editorconfig setting indent_size = huge: must be a number from 1 to 16\n", stderr)
}

func TestEditorConfigIndentStyle(t *testing.T) {
	for _, tt := range []struct {
		settings string
		args     []string
		want     bool
	}{
		{"indent_style = tab", nil, true},
		{"INDENT_STYLE = Tab", nil, true},
		{"indent_style = space", []string{"--use-tabs"}, true},
		{"indent_style = tab", []string{"--indent=2"}, false},
		{"indent_style = space", nil, false},
	} {
		writeProject(t, "root = true\n\n[Dockerfile]\n"+tt.settings+"\n")
		got := mustApplyEditorConfig(t, lib.Config{IndentSize: 4, UseTabs: slices.Contains(tt.args, "--use-tabs")}, "Dockerfile", parsedFlags(t, tt.args...))
		assert.Equal(t, tt.want, got.UseTabs, "%s %v", tt.settings, tt.args)
	}
}
