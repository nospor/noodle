package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the RSS feed background refresh worker daemon",
	Run: func(cmd *cobra.Command, args []string) {
		// Get path of current running executable
		executable, err := os.Executable()
		if err != nil {
			executable = "noodle"
		}
		c := exec.Command(executable, "daemon")
		configureSysProcAttr(c)
		if err := c.Start(); err != nil {
			fmt.Printf("Error starting daemon: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Background refresh daemon started.")
	},
}

func init() {
	rootCmd.AddCommand(startCmd)
}
