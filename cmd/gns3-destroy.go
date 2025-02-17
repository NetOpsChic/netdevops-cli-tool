package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// gns3DestroyCmd represents the Terraform destroy command for GNS3
var gns3DestroyCmd = &cobra.Command{
	Use:   "gns3-destroy",
	Short: "Destroy GNS3 resources using Terraform",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Destroying Terraform-managed GNS3 resources...")
		runCommandInDir("terraform", []string{"destroy", "-auto-approve"}, "terraform/")
	},
}

func init() {
	rootCmd.AddCommand(gns3DestroyCmd)
}
