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
// applied, except where a flag was given explicitly. It also returns a
// warning for each setting that is invalid and so ignored, or for the whole
// file if the library can't read it.
//
// Settings are read from def.Raw rather than the parsed fields such as
// def.IndentSize: the library fills those through case-sensitive struct tags,
// so "INDENT_SIZE = 2" would be ignored, while the specification makes key
// names case-insensitive. Raw has every key lowercased.
func applyEditorConfig(c lib.Config, path string, flags *pflag.FlagSet) (lib.Config, []string) {
	def, err := editorconfig.GetDefinitionForFilename(path)
	if err != nil {
		return c, []string{"ignoring .editorconfig: " + strings.Join(strings.Fields(err.Error()), " ")}
	}
	var warnings []string
	if style := strings.ToLower(def.Raw["indent_style"]); !flags.Changed("use-tabs") && !flags.Changed("indent") {
		switch style {
		case "tab":
			c.UseTabs = true
		case "space":
			c.UseTabs = false
		case "", editorconfig.UnsetValue:
		default:
			warnings = append(warnings, fmt.Sprintf("ignoring .editorconfig setting indent_style = %s: must be tab or space", style))
		}
	}
	if !flags.Changed("indent") {
		if n, ok, err := editorConfigIndent(def); err != nil {
			warnings = append(warnings, err.Error())
		} else if ok {
			c.IndentSize = n
		}
	}
	if b, ok, err := editorConfigBool(def, "insert_final_newline"); err != nil {
		warnings = append(warnings, err.Error())
	} else if ok && !flags.Changed("newline") {
		c.TrailingNewline = b
	}
	// space_redirects is specific to dockerfmt.
	if b, ok, err := editorConfigBool(def, "space_redirects"); err != nil {
		warnings = append(warnings, err.Error())
	} else if ok && !flags.Changed("space-redirects") {
		c.SpaceRedirects = b
	}
	return c, warnings
}

// editorConfigIndent returns the indent size def sets, if any. indent_size =
// tab means the tab width.
func editorConfigIndent(def *editorconfig.Definition) (uint, bool, error) {
	key := "indent_size"
	v := strings.ToLower(def.Raw[key])
	if v == "tab" {
		key = "tab_width"
		v = strings.ToLower(def.Raw[key])
	}
	if v == "" || v == editorconfig.UnsetValue {
		return 0, false, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > lib.MaxIndentSize {
		return 0, false, fmt.Errorf("ignoring .editorconfig setting %s = %s: must be a number from 1 to %d", key, v, lib.MaxIndentSize)
	}
	return uint(n), true, nil
}

// editorConfigBool returns the boolean setting key of def, if set.
func editorConfigBool(def *editorconfig.Definition, key string) (value, ok bool, err error) {
	v, set := def.Raw[key]
	if !set || strings.EqualFold(v, editorconfig.UnsetValue) {
		return false, false, nil
	}
	switch strings.ToLower(v) {
	case "true":
		return true, true, nil
	case "false":
		return false, true, nil
	}
	return false, false, fmt.Errorf("ignoring .editorconfig setting %s = %s: must be true or false", key, v)
}
