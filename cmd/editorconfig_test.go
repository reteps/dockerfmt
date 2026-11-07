package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/reteps/dockerfmt/lib"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newFlagCmd returns a command with the same formatting flags as rootCmd, so
// tests can control which flags count as explicitly set.
func newFlagCmd(t *testing.T, args ...string) *cobra.Command {
	t.Helper()
	c := &cobra.Command{}
	c.Flags().BoolP("newline", "n", false, "")
	c.Flags().UintP("indent", "i", 4, "")
	c.Flags().BoolP("space-redirects", "s", false, "")
	require.NoError(t, c.Flags().Parse(args))
	return c
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
	base := &lib.Config{IndentSize: 4}

	for _, path := range []string{
		"Dockerfile",
		"docker/Dockerfile",
		"./docker/Dockerfile",
		"docker/nested/Dockerfile",
		filepath.Join("docker", "..", "docker", "Dockerfile"),
	} {
		t.Run(path, func(t *testing.T) {
			got := applyEditorConfig(base, path, newFlagCmd(t))
			assert.Equal(t, uint(2), got.IndentSize)
			assert.True(t, got.TrailingNewline)
			assert.True(t, got.SpaceRedirects)
		})
	}

	t.Run("absolute path", func(t *testing.T) {
		abs, err := filepath.Abs("docker/Dockerfile")
		require.NoError(t, err)
		got := applyEditorConfig(base, abs, newFlagCmd(t))
		assert.Equal(t, uint(2), got.IndentSize)
	})
}

func TestEditorConfigPathGlobs(t *testing.T) {
	for _, section := range []string{"docker/Dockerfile", "**/Dockerfile", "docker/**", "{Dockerfile,*.dockerfile}"} {
		t.Run(section, func(t *testing.T) {
			writeProject(t, "root = true\n\n["+section+"]\nindent_size = 2\n")
			got := applyEditorConfig(&lib.Config{IndentSize: 4}, "docker/Dockerfile", newFlagCmd(t))
			assert.Equal(t, uint(2), got.IndentSize)
		})
	}
}

func TestEditorConfigExplicitFlagsWin(t *testing.T) {
	writeProject(t, "root = true\n\n[Dockerfile]\nindent_size = 2\ninsert_final_newline = false\n")
	base := &lib.Config{IndentSize: 8, TrailingNewline: true}

	got := applyEditorConfig(base, "docker/Dockerfile", newFlagCmd(t, "--indent=8", "--newline"))
	assert.Equal(t, uint(8), got.IndentSize)
	assert.True(t, got.TrailingNewline)
}
