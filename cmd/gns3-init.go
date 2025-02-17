package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// gns3DeployCmd represents the Terraform deployment command for GNS3
var gns3DeployCmd = &cobra.Command{
	Use:   "gns3-deploy",
	Short: "Deploy network in GNS3 using Terraform",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Generating Terraform configuration for GNS3...")

		// Ensure terraform directory exists
		err := os.MkdirAll("terraform", os.ModePerm)
		if err != nil {
			fmt.Println("Error creating terraform directory:", err)
			os.Exit(1)
		}

		// Terraform template for GNS3
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
  name = "{{ $.Template }}"
}

{{ range $i := (seq 1 .RouterCount) }}
resource "gns3_template" "router{{ $i }}" {
  name       = "Router{{ $i }}"
  project_id = gns3_project.project1.id
  template_id = data.gns3_template_id.router_template.id
}
{{ end }}`

		// Generate main.tf file
		err = generateTerraformFile("terraform/main.tf", tfTemplate, struct {
			RouterCount int
			Template    string
		}{RouterCount: routerCount, Template: templateName})
		if err != nil {
			fmt.Println("Error generating Terraform file:", err)
			os.Exit(1)
		}

		fmt.Println("Applying Terraform configuration for GNS3...")
		runCommandInDir("terraform", []string{"init"}, "terraform/")
		runCommandInDir("terraform", []string{"apply", "-auto-approve"}, "terraform/")
	},
}

func init() {
	gns3DeployCmd.Flags().IntVarP(&routerCount, "routers", "r", 1, "Number of routers to deploy")
	gns3DeployCmd.Flags().StringVarP(&templateName, "template", "t", "c7200", "GNS3 device template")

	rootCmd.AddCommand(gns3DeployCmd)
}
