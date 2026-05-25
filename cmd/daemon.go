package cmd

import (
	"github.com/spf13/cobra"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Run the RSS feed background refresh worker in the foreground",
	Run: func(cmd *cobra.Command, args []string) {
		startRefreshWorker()
	},
}

func init() {
	rootCmd.AddCommand(daemonCmd)
}
