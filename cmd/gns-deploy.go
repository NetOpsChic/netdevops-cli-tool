package cmd

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var routerCount int
var switchCount int
var cloudCount int
var templateName string
var links []string
var projectName string // User-supplied project name

// CLI template for Terraform that expects integer counts (RouterCount, SwitchCount, CloudCount).
const terraformTemplateCLI = `terraform {
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

# Routers
{{ range $i := seq 1 .RouterCount }}
resource "gns3_template" "router{{ $i }}" {
  name        = "R{{ $i }}"
  project_id  = gns3_project.project1.id
  template_id = data.gns3_template_id.router_template.id
}
{{ end }}

data "gns3_template_id" "router_template" {
  name = "{{ .Template }}"
}

# Switches
{{ range $i := seq 1 .SwitchCount }}
resource "gns3_switch" "switch{{ $i }}" {
  name       = "SW{{ $i }}"
  project_id = gns3_project.project1.id
}
{{ end }}

# Clouds
{{ range $i := seq 1 .CloudCount }}
resource "gns3_cloud" "cloud{{ $i }}" {
  name       = "Cloud{{ $i }}"
  project_id = gns3_project.project1.id
}
{{ end }}

# Links
{{ range .Links }}
resource "gns3_link" "{{ (index .Endpoints 0).Name }}_to_{{ (index .Endpoints 1).Name }}" {
  lifecycle {
    create_before_destroy = true
  }

  project_id = gns3_project.project1.id

  node_a_id = lookup(
    merge({
      {{ range $i := seq 1 $.RouterCount }}
      "R{{ $i }}" = gns3_template.router{{ $i }}.id,
      {{ end }}
      {{ range $i := seq 1 $.SwitchCount }}
      "SW{{ $i }}" = gns3_switch.switch{{ $i }}.id,
      {{ end }}
      {{ range $i := seq 1 $.CloudCount }}
      "Cloud{{ $i }}" = gns3_cloud.cloud{{ $i }}.id
      {{ end }}
    }),
    "{{ (index .Endpoints 0).Name }}",
    ""
  )
  node_a_adapter = {{ (index .Endpoints 0).Adapter }}
  node_a_port    = {{ (index .Endpoints 0).Port }}

  node_b_id = lookup(
    merge({
      {{ range $i := seq 1 $.RouterCount }}
      "R{{ $i }}" = gns3_template.router{{ $i }}.id,
      {{ end }}
      {{ range $i := seq 1 $.SwitchCount }}
      "SW{{ $i }}" = gns3_switch.switch{{ $i }}.id,
      {{ end }}
      {{ range $i := seq 1 $.CloudCount }}
      "Cloud{{ $i }}" = gns3_cloud.cloud{{ $i }}.id
      {{ end }}
    }),
    "{{ (index .Endpoints 1).Name }}",
    ""
  )
  node_b_adapter = {{ (index .Endpoints 1).Adapter }}
  node_b_port    = {{ (index .Endpoints 1).Port }}
}
{{ end }}
`

// gns3DeployCmd represents the CLI-based deployment command.
var gns3DeployCmd = &cobra.Command{
	Use:   "gns3-deploy",
	Short: "Deploy topology using CLI flags (without YAML)",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Deploying topology using CLI flags...")

		// Validate required flag.
		if templateName == "" {
			log.Fatal("Error: --template flag is required.")
		}

		fmt.Printf("Project: %s\n", projectName)
		fmt.Printf("Routers: %d, Switches: %d, Clouds: %d\n", routerCount, switchCount, cloudCount)
		fmt.Printf("Template: %s\n", templateName)
		fmt.Printf("Links: %v\n", links)

		// Ensure the terraform directory exists.
		if err := os.MkdirAll("terraform", os.ModePerm); err != nil {
			log.Fatalf("Error creating terraform directory: %v", err)
		}

		// We'll maintain per-device, per-adapter auto-assignment.
		// nextPort: map[device]map[int]int – next available port per adapter.
		// usedPorts: map[device]map[int]map[int]bool – tracks used ports.
		nextPort := make(map[string]map[int]int)
		usedPorts := make(map[string]map[int]map[int]bool)

		initDevice := func(name string) {
			if nextPort[name] == nil {
				nextPort[name] = make(map[int]int)
				nextPort[name][0] = 0
				nextPort[name][1] = 0
			}
			if usedPorts[name] == nil {
				usedPorts[name] = make(map[int]map[int]bool)
				usedPorts[name][0] = make(map[int]bool)
				usedPorts[name][1] = make(map[int]bool)
			}
		}

		var parsedLinks []CLILink
		for _, linkStr := range links {
			for _, l := range strings.Split(linkStr, ",") {
				l = strings.TrimSpace(l)
				if l == "" {
					continue
				}
				nodes := strings.Split(l, "-")
				if len(nodes) != 2 {
					log.Fatalf("Invalid link format: %s", l)
				}
				var endpoints []Endpoint
				for _, nodeStr := range nodes {
					nodeStr = strings.TrimSpace(nodeStr)
					var ep Endpoint

					if strings.Contains(nodeStr, ":") {
						// Detailed format: Node:adapter/port
						parts := strings.Split(nodeStr, ":")
						if len(parts) != 2 {
							log.Fatalf("Invalid endpoint format: %s", nodeStr)
						}
						ep.Name = strings.TrimSpace(parts[0])
						apParts := strings.Split(strings.TrimSpace(parts[1]), "/")
						if len(apParts) != 2 {
							log.Fatalf("Invalid adapter/port format: %s", nodeStr)
						}
						adapter, err := strconv.Atoi(apParts[0])
						if err != nil {
							log.Fatalf("Invalid adapter number in: %s", nodeStr)
						}
						port, err := strconv.Atoi(apParts[1])
						if err != nil {
							log.Fatalf("Invalid port number in: %s", nodeStr)
						}
						ep.Adapter = adapter
						ep.Port = port
						initDevice(ep.Name)
						if current, exists := nextPort[ep.Name][adapter]; !exists || port >= current {
							nextPort[ep.Name][adapter] = port + 1
						}
						usedPorts[ep.Name][adapter][port] = true
					} else {
						// Auto-assign
						ep.Name = nodeStr
						initDevice(ep.Name)
						p0 := nextPort[ep.Name][0]
						if !usedPorts[ep.Name][0][p0] {
							ep.Adapter = 0
							ep.Port = p0
							usedPorts[ep.Name][0][p0] = true
							nextPort[ep.Name][0] = p0 + 1
						} else {
							p1 := nextPort[ep.Name][1]
							ep.Adapter = 1
							ep.Port = p1
							usedPorts[ep.Name][1][p1] = true
							nextPort[ep.Name][1] = p1 + 1
						}
					}
					endpoints = append(endpoints, ep)
				}
				parsedLinks = append(parsedLinks, CLILink{Endpoints: endpoints})
			}
		}

		// Build the data structure for the CLI-based template
		data := struct {
			Project     string
			RouterCount int
			SwitchCount int
			CloudCount  int
			Template    string
			Links       []CLILink
		}{
			Project:     projectName,
			RouterCount: routerCount,
			SwitchCount: switchCount,
			CloudCount:  cloudCount,
			Template:    templateName,
			Links:       parsedLinks,
		}

		// Generate the Terraform file with the CLI template (counts-based).
		err := generateTerraformFile("terraform/main.tf", terraformTemplateCLI, data)
		if err != nil {
			log.Fatalf("Error generating Terraform file: %v", err)
		}

		// Print the generated file with line numbers.
		fmt.Println("\nGenerated Terraform File Content:")
		content, readErr := os.ReadFile("terraform/main.tf")
		if readErr != nil {
			log.Fatalf("Error reading generated Terraform file: %v", readErr)
		}
		lines := strings.Split(string(content), "\n")
		for i, line := range lines {
			fmt.Printf("%d: %s\n", i+1, line)
		}

		// Apply Terraform
		fmt.Println("Applying Terraform configuration...")
		runCommandInDir("terraform", []string{"init"}, "terraform/")
		runCommandInDir("terraform", []string{"apply", "-auto-approve"}, "terraform/")
	},
}

func init() {
	gns3DeployCmd.Flags().IntVarP(&routerCount, "routers", "r", 1, "Number of routers to deploy")
	gns3DeployCmd.Flags().IntVarP(&switchCount, "switches", "s", 0, "Number of switches to deploy")
	gns3DeployCmd.Flags().IntVarP(&cloudCount, "clouds", "c", 0, "Number of cloud nodes to deploy")
	gns3DeployCmd.Flags().StringVarP(&templateName, "template", "t", "", "GNS3 device template (required)")
	gns3DeployCmd.Flags().StringSliceVarP(&links, "links", "l", []string{}, "Comma-separated list of links in the format 'NodeA-NodeB' or 'NodeA:adapter/port-NodeB:adapter/port'")
	gns3DeployCmd.Flags().StringVarP(&projectName, "project", "p", "netdevops", "GNS3 project name")
	rootCmd.AddCommand(gns3DeployCmd)
}
