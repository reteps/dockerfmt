package cmd

import (
	"strconv"

	"github.com/editorconfig/editorconfig-core-go/v2"
	"github.com/reteps/dockerfmt/lib"
	"github.com/spf13/pflag"
)

// applyEditorConfig returns c with the .editorconfig settings for path
// applied, except where a flag was given explicitly.
func applyEditorConfig(c lib.Config, path string, flags *pflag.FlagSet) lib.Config {
	def, err := editorconfig.GetDefinitionForFilename(path)
	if err != nil {
		return c
	}
	if !flags.Changed("indent") {
		if n, err := strconv.Atoi(def.IndentSize); err == nil && n > 0 {
			c.IndentSize = uint(n)
		}
	}
	if !flags.Changed("newline") && def.InsertFinalNewline != nil {
		c.TrailingNewline = *def.InsertFinalNewline
	}
	// space_redirects is specific to dockerfmt.
	if !flags.Changed("space-redirects") {
		if b, err := strconv.ParseBool(def.Raw["space_redirects"]); err == nil {
			c.SpaceRedirects = b
		}
	}
	return c
}
