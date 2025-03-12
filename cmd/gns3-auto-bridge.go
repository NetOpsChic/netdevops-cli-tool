// File: cmd/gns3-auto-bridge.go
package cmd

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"

	"gopkg.in/yaml.v2"

	"github.com/spf13/cobra"
)

// terraformTemplate is the existing Terraform configuration template.
// (Reuse your existing template definition here.)
const terraformTemplate = `terraform {
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
{{ range .Routers }}
resource "gns3_template" "{{ .Name }}" {
  name        = "{{ .Name }}"
  project_id  = gns3_project.project1.id
  template_id = data.gns3_template_id.{{ .Name }}.id
}

data "gns3_template_id" "{{ .Name }}" {
  name = "{{ .Template }}"
}
{{ end }}

# Switches
{{ range .Switches }}
resource "gns3_switch" "{{ .Name }}" {
  name       = "{{ .Name }}"
  project_id = gns3_project.project1.id
}
{{ end }}

# Clouds
{{ range .Clouds }}
resource "gns3_cloud" "{{ .Name }}" {
  name       = "{{ .Name }}"
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
      {{ range $.Routers }}
      "{{ .Name }}" = gns3_template.{{ .Name }}.id,
      {{ end }}
      {{ range $.Switches }}
      "{{ .Name }}" = gns3_switch.{{ .Name }}.id,
      {{ end }}
      {{ range $.Clouds }}
      "{{ .Name }}" = gns3_cloud.{{ .Name }}.id
      {{ end }}
    }),
    "{{ (index .Endpoints 0).Name }}",
    ""
  )
  node_a_adapter = {{ (index .Endpoints 0).Adapter }}
  node_a_port    = {{ (index .Endpoints 0).Port }}

  node_b_id = lookup(
    merge({
      {{ range $.Routers }}
      "{{ .Name }}" = gns3_template.{{ .Name }}.id,
      {{ end }}
      {{ range $.Switches }}
      "{{ .Name }}" = gns3_switch.{{ .Name }}.id,
      {{ end }}
      {{ range $.Clouds }}
      "{{ .Name }}" = gns3_cloud.{{ .Name }}.id
      {{ end }}
    }),
    "{{ (index .Endpoints 1).Name }}",
    ""
  )
  node_b_adapter = {{ (index .Endpoints 1).Adapter }}
  node_b_port    = {{ (index .Endpoints 1).Port }}
}
{{ end }}

# Start all nodes if --start flag is used
{{ if .StartNodes }}
resource "gns3_start_all" "start_nodes" {
  project_id = gns3_project.project1.id

  depends_on = [
    {{ range $.Routers }} gns3_template.{{ .Name }},
    {{ end }}
    {{ range $.Switches }} gns3_switch.{{ .Name }},
    {{ end }}
    {{ range $.Clouds }} gns3_cloud.{{ .Name }},
    {{ end }}
    {{ range $.Links }} gns3_link.{{ (index .Endpoints 0).Name }}_to_{{ (index .Endpoints 1).Name }},
    {{ end }}
  ]
}
{{ end }}
`

// gns3AutoBridgeCmd creates an extended topology by adding Cloud and ZTP connectivity
// and then calls Terraform to apply the configuration.
var gns3AutoBridgeCmd = &cobra.Command{
	Use:   "gns3-auto-bridge",
	Short: "Automatically extend the topology YAML by adding cloud and ZTP connectivity",
	RunE: func(cmd *cobra.Command, args []string) error {
		// 1. Read the base topology YAML.
		if configFile == "" {
			return fmt.Errorf("deployment YAML file must be provided using the --config flag")
		}
		baseData, err := ioutil.ReadFile(configFile)
		if err != nil {
			return fmt.Errorf("failed to read deployment file: %v", err)
		}

		// 2. Unmarshal into a generic map.
		var topo map[string]interface{}
		if err := yaml.Unmarshal(baseData, &topo); err != nil {
			return fmt.Errorf("failed to parse YAML: %v", err)
		}

		// 3. Ensure that extra connectivity is added.
		//    Add default Cloud device if not present.
		if _, ok := topo["clouds"]; !ok {
			topo["clouds"] = []interface{}{
				map[string]interface{}{"name": "Cloud"},
			}
		}
		//    Add default ZTP device if not present.
		if _, ok := topo["ztp"]; !ok {
			topo["ztp"] = []interface{}{
				map[string]interface{}{"name": "ZTP"},
			}
		}
		//    Add default links if not present.
		if _, ok := topo["links"]; !ok {
			// Here we add two links:
			// - Cloud <--> Switch (we'll assume a switch exists in the topology; if not, you might choose a router)
			// - ZTP <--> Switch
			// For simplicity, we assume the switch name is "Core-SW". You could make this dynamic.
			topo["links"] = []interface{}{
				// Link: Cloud to Core-SW
				map[string]interface{}{
					"endpoints": []interface{}{
						map[string]interface{}{"name": "Cloud", "adapter": 0, "port": 0},
						map[string]interface{}{"name": "Core-SW", "adapter": 0, "port": 1},
					},
				},
				// Link: ZTP to Core-SW
				map[string]interface{}{
					"endpoints": []interface{}{
						map[string]interface{}{"name": "ZTP", "adapter": 0, "port": 0},
						map[string]interface{}{"name": "Core-SW", "adapter": 0, "port": 3},
					},
				},
			}
		}

		// 4. Marshal the extended topology back to YAML.
		extendedData, err := yaml.Marshal(topo)
		if err != nil {
			return fmt.Errorf("failed to marshal extended topology: %v", err)
		}

		// 5. Create a temporary directory for the Terraform configuration.
		tempDir, err := ioutil.TempDir("", "terraform-bridge")
		if err != nil {
			return fmt.Errorf("failed to create temporary directory: %v", err)
		}
		// Optional: defer os.RemoveAll(tempDir) to clean up after
		defer os.RemoveAll(tempDir)

		// 6. Render the Terraform template using the extended topology.
		tfFilePath := filepath.Join(tempDir, "main.tf")
		tfFile, err := os.Create(tfFilePath)
		if err != nil {
			return fmt.Errorf("failed to create Terraform file: %v", err)
		}
		defer tfFile.Close()

		tmpl, err := template.New("terraform").Parse(terraformTemplate)
		if err != nil {
			return fmt.Errorf("failed to parse Terraform template: %v", err)
		}

		// Create a structure that matches the fields used in your terraformTemplate.
		// Here, we assume your YAML already has keys for Project, Routers, Switches, Clouds, and Links.
		// If not, you may need to set defaults for missing sections.
		type TerraformData struct {
			Project    string
			Routers    []interface{}
			Switches   []interface{}
			Clouds     []interface{}
			Links      []interface{}
			StartNodes bool
		}
		// Gather data from the extended topology.
		tfData := TerraformData{
			StartNodes: false,
		}
		if p, ok := topo["project"].(string); ok {
			tfData.Project = p
		}
		if routers, ok := topo["routers"].([]interface{}); ok {
			tfData.Routers = routers
		}
		if sw, ok := topo["switches"].([]interface{}); ok {
			tfData.Switches = sw
		}
		if clouds, ok := topo["clouds"].([]interface{}); ok {
			tfData.Clouds = clouds
		}
		if links, ok := topo["links"].([]interface{}); ok {
			tfData.Links = links
		}

		// Render the terraform template into the main.tf file.
		if err := tmpl.Execute(tfFile, tfData); err != nil {
			return fmt.Errorf("failed to render Terraform configuration: %v", err)
		}
		tfFile.Close()

		// If verbose is enabled, print the rendered Terraform configuration.
		if verbose {
			fmt.Println("Rendered Terraform Configuration:")
			fmt.Println(string(extendedData))
			tfContent, err := ioutil.ReadFile(tfFilePath)
			if err == nil {
				fmt.Println(string(tfContent))
			}
		}

		// 7. Run Terraform commands: terraform init and terraform apply -auto-approve.
		// Change to the temporary directory.
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %v", err)
		}
		defer os.Chdir(cwd)
		if err := os.Chdir(tempDir); err != nil {
			return fmt.Errorf("failed to change directory to temp dir: %v", err)
		}

		fmt.Println("Running: terraform init")
		initCmd := exec.Command("terraform", "init")
		initCmd.Env = os.Environ()
		initOut, err := initCmd.CombinedOutput()
		fmt.Println(string(initOut))
		if err != nil {
			return fmt.Errorf("terraform init failed: %v", err)
		}

		fmt.Println("Running: terraform apply -auto-approve")
		applyCmd := exec.Command("terraform", "apply", "-auto-approve")
		applyCmd.Env = os.Environ()
		applyOut, err := applyCmd.CombinedOutput()
		fmt.Println(string(applyOut))
		if err != nil {
			return fmt.Errorf("terraform apply failed: %v", err)
		}

		fmt.Println("Auto-bridge complete; the topology has been extended with cloud and ZTP connectivity.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(gns3AutoBridgeCmd)
	gns3AutoBridgeCmd.Flags().StringVarP(&configFile, "config", "c", "", "Deployment YAML file containing topology and device configuration")
	// No inventory flag is needed.
}
