package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var routerCount int
var templateName string

var gns3Deploy = &cobra.Command{
	Use:   "gns3-deploy",
	Short: "Deploy network using Terraform",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Generating Terraform configuration...")

		// Ensure terraform directory exists
		err := os.MkdirAll("terraform", os.ModePerm)
		if err != nil {
			fmt.Println("Error creating terraform directory:", err)
			os.Exit(1)
		}

		// Terraform template with improved formatting
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
  name = "netdevops-lab"
}

data "gns3_template_id" "router_template" {
  name = "{{ .Template }}"
}

{{ range $i := seq 1 .RouterCount }}
resource "gns3_template" "router{{ $i }}" {
  name       = "Router{{ $i }}"
  project_id = gns3_project.project1.id
  template_id = data.gns3_template_id.router_template.id
  x          = {{ multiply $i 200 }}   # Auto-spacing routers horizontally
  y          = {{ multiply (mod $i 2) 150 }}  # Stagger routers vertically
}
{{ end }}`

		// Data for the template
		data := struct {
			RouterCount int
			Template    string
		}{
			RouterCount: routerCount,
			Template:    templateName,
		}

		// Generate the Terraform file
		err = generateTerraformFile("terraform/main.tf", tfTemplate, data)
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
	gns3Deploy.Flags().IntVarP(&routerCount, "routers", "r", 1, "Number of routers to deploy")
	gns3Deploy.Flags().StringVarP(&templateName, "template", "t", "c7200", "GNS3 device template")

	rootCmd.AddCommand(gns3Deploy)
}
