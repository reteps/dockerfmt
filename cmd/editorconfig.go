package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/editorconfig/editorconfig-core-go/v2"
	"github.com/reteps/dockerfmt/lib"
	"github.com/spf13/pflag"
)

// applyEditorConfig returns c with the .editorconfig settings for path
// applied, except where a flag was given explicitly. It returns an error, and
// c as far as it could be applied, if a setting or the file is invalid.
//
// Settings are read from def.Raw rather than the parsed fields such as
// def.IndentSize: the library fills those through case-sensitive struct tags,
// so "INDENT_SIZE = 2" would be ignored, while the specification makes key
// names case-insensitive. Raw has every key lowercased.
func applyEditorConfig(c lib.Config, path string, flags *pflag.FlagSet) (lib.Config, error) {
	def, err := editorconfig.GetDefinitionForFilename(path)
	if err != nil {
		return c, fmt.Errorf("ignoring .editorconfig for %s: %w", path, err)
	}
	var problem error
	if !flags.Changed("indent") {
		if n, ok, err := editorConfigIndent(def); err != nil {
			problem = err
		} else if ok {
			c.IndentSize = n
		}
	}
	if v, set := def.Raw["insert_final_newline"]; set && !flags.Changed("newline") {
		if b, err := strconv.ParseBool(v); err == nil {
			c.TrailingNewline = b
		}
	}
	// space_redirects is specific to dockerfmt.
	if v, set := def.Raw["space_redirects"]; set && !flags.Changed("space-redirects") {
		b, err := strconv.ParseBool(v)
		if err != nil {
			problem = fmt.Errorf("%s: space_redirects = %q is not true or false", path, v)
		} else {
			c.SpaceRedirects = b
		}
	}
	return c, problem
}

// editorConfigIndent returns the indent size def sets, if any. indent_size =
// tab means the tab width.
func editorConfigIndent(def *editorconfig.Definition) (uint, bool, error) {
	v := strings.ToLower(def.Raw["indent_size"])
	if v == "tab" {
		v = strings.ToLower(def.Raw["tab_width"])
	}
	if v == "" || v == editorconfig.UnsetValue {
		return 0, false, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, false, fmt.Errorf("indent_size = %q is not a number", v)
	}
	if n < 1 || n > maxIndent {
		return 0, false, fmt.Errorf("indent_size = %d is not between 1 and %d", n, maxIndent)
	}
	return uint(n), true, nil
}
