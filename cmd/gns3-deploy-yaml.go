package cmd

import (
	"fmt"
	"io/ioutil"
	"os"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// Structs for parsing YAML
type Topology struct {
	Project string   `yaml:"project"`
	Routers []Router `yaml:"routers"`
	Links   []Link   `yaml:"links"`
}

type Router struct {
	Name     string `yaml:"name"`
	Template string `yaml:"template"`
}

type Link struct {
	Endpoints []string `yaml:"endpoints"`
}

var configFile string

// gns3DeployYamlCmd represents the YAML-based GNS3 deployment command
var gns3DeployYamlCmd = &cobra.Command{
	Use:   "gns3-deploy-yaml",
	Short: "Deploy GNS3 topology from a YAML file",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Reading YAML topology...")

		// Read the YAML file
		yamlData, err := ioutil.ReadFile(configFile)
		if err != nil {
			fmt.Println("Error reading YAML file:", err)
			os.Exit(1)
		}

		// Parse the YAML file into a Topology struct
		var topology Topology
		err = yaml.Unmarshal(yamlData, &topology)
		if err != nil {
			fmt.Println("Error parsing YAML:", err)
			os.Exit(1)
		}

		// Print an ASCII visualization of the topology
		fmt.Println("Visualizing YAML topology...")
		visualizeTopology(topology)

		fmt.Println("Generating Terraform configuration from YAML...")

		// Terraform template with routers and links
		tfTemplate := `terraform {
  required_providers {
    gns3 = {
      source  = "netopschic/gns3"
      version = "~> 1.0"
    }
  }
}

provider "gns3" {
  host = "http://localhost:3080"
}

resource "gns3_project" "project1" {
  name = "{{ .Project }}"
}

{{ range $index, $router := .Routers }}
resource "gns3_template" "{{ $router.Name }}" {
  name       = "{{ $router.Name }}"
  project_id = gns3_project.project1.id
  template_id = data.gns3_template_id.{{ $router.Name }}.id
  x          = {{ multiply $index 200 }}   # Auto-space routers horizontally
  y          = {{ multiply (mod $index 2) 150 }}  # Stagger routers vertically
}

data "gns3_template_id" "{{ $router.Name }}" {
  name = "{{ $router.Template }}"
}
{{ end }}

{{ range .Links }}
resource "gns3_link" "{{ index .Endpoints 0 }}_to_{{ index .Endpoints 1 }}" {
  project_id     = gns3_project.project1.id
  node_a_id      = gns3_template.{{ index .Endpoints 0 }}.id
  node_a_adapter = 0
  node_a_port    = 0
  node_b_id      = gns3_template.{{ index .Endpoints 1 }}.id
  node_b_adapter = 0
  node_b_port    = 0

  depends_on = [
    gns3_template.{{ index .Endpoints 0 }},
    gns3_template.{{ index .Endpoints 1 }}
  ]
}
{{ end }}`

		// Ensure the terraform directory exists
		err = os.MkdirAll("terraform", os.ModePerm)
		if err != nil {
			fmt.Println("Error creating terraform directory:", err)
			os.Exit(1)
		}

		// Generate the Terraform file
		err = generateTerraformFile("terraform/main.tf", tfTemplate, topology)
		if err != nil {
			fmt.Println("Error generating Terraform file:", err)
			os.Exit(1)
		}

		fmt.Println("Applying Terraform configuration...")
		runCommandInDir("terraform", []string{"init"}, "terraform/")
		runCommandInDir("terraform", []string{"apply", "-auto-approve"}, "terraform/")
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
	for _, router := range topology.Routers {
		fmt.Printf("[ %s ]\n", router.Name)
	}

	// Print links
	fmt.Println("\n🔗 Links:")
	for _, link := range topology.Links {
		fmt.Printf("%s <---> %s\n", link.Endpoints[0], link.Endpoints[1])
	}
	fmt.Println("==================================\n")
}
