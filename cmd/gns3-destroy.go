package cmd

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

// gns3DestroyCmd represents the Terraform destroy command for GNS3
var gns3DestroyCmd = &cobra.Command{
	Use:   "gns3-destroy",
	Short: "Destroy GNS3 resources using Terraform",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Refreshing Terraform state to detect existing resources...")
		runCommandInDir("terraform", []string{"refresh"}, "terraform/")

		fmt.Println("Identifying all GNS3 links to remove before destroying the topology...")
		removeAllLinksFromState()

		fmt.Println("Now destroying the full topology...")
		runCommandInDir("terraform", []string{"destroy", "-auto-approve"}, "terraform/")
	},
}

func init() {
	rootCmd.AddCommand(gns3DestroyCmd)
}

// removeAllLinksFromState removes all links dynamically before destruction
func removeAllLinksFromState() {
	cmd := exec.Command("terraform", "state", "list")
	cmd.Dir = "terraform/"
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Println("Error listing Terraform state:", err)
		return
	}

	// Convert output to a slice of strings
	stateResources := strings.Split(string(output), "\n")

	// Remove all GNS3 links from Terraform state before destroying
	for _, resource := range stateResources {
		if strings.HasPrefix(resource, "gns3_link.") {
			fmt.Println("Removing link from state:", resource)
			runCommandInDir("terraform", []string{"state", "rm", resource}, "terraform/")
		}
	}
}
