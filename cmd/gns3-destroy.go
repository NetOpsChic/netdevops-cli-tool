package cmd

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	cleanUpAll bool
)

var gns3DestroyCmd = &cobra.Command{
	Use:   "gns3-destroy",
	Short: "Destroy GNS3 resources for the project defined in the YAML",
	Run: func(cmd *cobra.Command, args []string) {
		// 1) Read & parse topology YAML
		fmt.Println("📂 Reading YAML topology for destroy...")
		data, err := ioutil.ReadFile(configFile)
		if err != nil {
			fmt.Println("❌ Error reading YAML file:", err)
			os.Exit(1)
		}
		var topology Topology
		if err := yaml.Unmarshal(data, &topology); err != nil {
			fmt.Println("❌ Error parsing YAML:", err)
			os.Exit(1)
		}

		// 2) Compute project directories
		baseDir := path.Join("projects", topology.Project.Name)
		tfDir := path.Join(baseDir, "terraform")

		// 3) Refresh state
		fmt.Println("🔄 Refreshing Terraform state...")
		runCommandInDir("terraform", []string{"refresh"}, tfDir, nil)

		// 4) Remove GNS3 links from state
		fmt.Println("🗑️  Pruning GNS3 link resources from state…")
		removeAllLinksFromState(tfDir)

		// 5) Destroy
		fmt.Println("💥 Destroying the full topology…")
		runCommandInDir("terraform", []string{"destroy", "-auto-approve"}, tfDir, nil)

		// 6) Clean up entire project if requested
		if cleanUpAll {
			fmt.Printf("🧹 Removing entire project directory: %s\n", baseDir)
			if err := os.RemoveAll(baseDir); err != nil {
				fmt.Printf("❌ Failed to remove %s: %v\n", baseDir, err)
			} else {
				fmt.Println("✅ Project directory removed.")
			}
		} else {
			fmt.Println("⚠️  Skipping project directory cleanup (use --clean-up-all to remove it)")
		}
	},
}

func init() {
	gns3DestroyCmd.Flags().StringVarP(&configFile, "config", "c", "topology.yaml", "YAML topology file")
	gns3DestroyCmd.Flags().BoolVar(&cleanUpAll, "clean-up-all", false, "Also remove the entire project directory after destroy")
	rootCmd.AddCommand(gns3DestroyCmd)
}

func removeAllLinksFromState(tfDir string) {
	cmd := exec.Command("terraform", "state", "list")
	cmd.Dir = tfDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Println("❌ Error listing Terraform state:", err)
		return
	}
	for _, res := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(res, "gns3_link.") {
			fmt.Println("🗑️  Removing from state:", res)
			runCommandInDir("terraform", []string{"state", "rm", res}, tfDir, nil)
		}
	}
}
