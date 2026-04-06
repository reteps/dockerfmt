package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version is set at build time via ldflags:
//
//	go build -ldflags "-X github.com/reteps/dockerfmt/cmd.Version=v0.4.0"
var Version = "dev"

func init() {
	rootCmd.AddCommand(versionCmd)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number of dockerfmt",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("dockerfmt " + Version)
	},
}
