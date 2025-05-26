package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// gns3InitCmd represents the Terraform initialization command for GNS3
var gns3InitCmd = &cobra.Command{
	Use:   "gns3-init",
	Short: "Initialize Terraform for GNS3 inside the terraform/ directory",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Initializing Terraform for GNS3...")
		runCommandInDir("terraform", []string{"init"}, "terraform/", nil)
	},
}

func init() {
	rootCmd.AddCommand(gns3InitCmd)
}
