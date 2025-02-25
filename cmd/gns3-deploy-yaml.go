package cmd

import (
	"fmt"
	"io/ioutil"
	"os"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var configFile string

// gns3DeployYamlCmd represents the YAML-based GNS3 deployment command
var gns3DeployYamlCmd = &cobra.Command{
	Use:   "gns3-deploy-yaml",
	Short: "Deploy GNS3 topology from a YAML file",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("📂 Reading YAML topology...")

		// Read the YAML file
		yamlData, err := ioutil.ReadFile(configFile)
		if err != nil {
			fmt.Println("❌ Error reading YAML file:", err)
			os.Exit(1)
		}

		// Parse the YAML file into a Topology struct
		var topology Topology
		err = yaml.Unmarshal(yamlData, &topology)
		if err != nil {
			fmt.Println("❌ Error parsing YAML:", err)
			os.Exit(1)
		}

		// Print an ASCII visualization of the topology
		fmt.Println("📡 Visualizing YAML topology...")
		visualizeTopology(topology)

		fmt.Println("⚙️ Generating Terraform configuration from YAML...")

		// Ensure the terraform directory exists
		err = os.MkdirAll("terraform", os.ModePerm)
		if err != nil {
			fmt.Println("❌ Error creating terraform directory:", err)
			os.Exit(1)
		}

		// Generate the Terraform file
		err = generateTerraformFile("terraform/main.tf", terraformTemplate, topology)
		if err != nil {
			fmt.Println("❌ Error generating Terraform file:", err)
			os.Exit(1)
		}

		// Apply Terraform
		fmt.Println("🚀 Applying Terraform configuration...")
		runCommandInDir("terraform", []string{"init"}, "terraform/")
		runCommandInDir("terraform", []string{"apply", "-auto-approve"}, "terraform/")

		// Start nodes if the YAML flag is set
		if topology.StartNodes {
			fmt.Println("🔌 Starting all nodes...")
			runCommandInDir("terraform", []string{"apply", "-auto-approve", "-target=gns3_start_all.start_nodes"}, "terraform/")
		}
	},
}

func init() {
	gns3DeployYamlCmd.Flags().StringVarP(&configFile, "config", "c", "topology.yaml", "YAML topology file")
	rootCmd.AddCommand(gns3DeployYamlCmd)
}

// Function to visualize the YAML topology in ASCII format
func visualizeTopology(topology Topology) {
	fmt.Println("\n📡 **Topology Visualization**")
	fmt.Println("==================================")

	// Print routers
	fmt.Println("🖥️ Routers:")
	for _, router := range topology.Routers {
		fmt.Printf("🔹 [ %s ]\n", router.Name)
	}

	// Print switches
	if len(topology.Switches) > 0 {
		fmt.Println("\n🖧 Switches:")
		for _, sw := range topology.Switches {
			fmt.Printf("🟦 [ %s ]\n", sw.Name)
		}
	}

	// Print clouds
	if len(topology.Clouds) > 0 {
		fmt.Println("\n☁️ Clouds:")
		for _, cloud := range topology.Clouds {
			fmt.Printf("🌥️ [ %s ]\n", cloud.Name)
		}
	}

	// Print links
	fmt.Println("\n🔗 Links:")
	for _, link := range topology.Links {
		fmt.Printf("🔌 %s <---> %s\n", link.Endpoints[0].Name, link.Endpoints[1].Name)
	}
	fmt.Println("==================================")
}
