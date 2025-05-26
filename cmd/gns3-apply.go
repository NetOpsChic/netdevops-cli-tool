package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// gns3ApplyCmd represents the Terraform apply command for GNS3
var gns3ApplyCmd = &cobra.Command{
	Use:   "gns3-apply",
	Short: "Apply Terraform configuration for GNS3",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Applying Terraform configuration for GNS3...")
		runCommandInDir("terraform", []string{"apply", "-auto-approve"}, "terraform/", nil)
	},
}

func init() {
	rootCmd.AddCommand(gns3ApplyCmd)
}
