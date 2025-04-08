package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const autoBridgeTerraformTemplate = `terraform {
  required_providers {
    gns3 = {
      source  = "netopschic/gns3"
      version = "{{ .TerraformVersion }}"
    }
  }
}

provider "gns3" {
  host = "http://localhost:3080"
}

resource "gns3_project" "project1" {
  name = "{{ .Project }}"
}

data "gns3_template_id" "ztp" {
  name = "{{ .ZTPTemplate }}"
}

# ✅ Template now depends on all QEMU nodes so it's deleted last
resource "gns3_template" "ztp" {
  name        = "{{ .ZTPTemplate }}"
  project_id  = gns3_project.project1.id
  template_id = data.gns3_template_id.ztp.template_id
  start       = true

}

resource "gns3_cloud" "cloud" {
  name       = "cloud"
  project_id = gns3_project.project1.id
}

resource "gns3_switch" "mgmt_switch" {
  name       = "mgmt-switch"
  project_id = gns3_project.project1.id
}

variable "network_device_ids" {
  type        = map(string)
  description = "Mapping of network device names to their QEMU node IDs as created by deploy‑yaml."
}

variable "link_ids" {
  type        = map(string)
  description = "Mapping of link resource names to their link UUIDs"
}

# 🧱 Dummy declarations to retain existing QEMU nodes
{{ range $name, $id := .NetworkDeviceIDs }}
resource "gns3_qemu_node" "{{ $name }}" {
  name       = "{{ $name }}"
  project_id = gns3_project.project1.id

  lifecycle {
    ignore_changes = all
  }
}
{{ end }}

# 🧱 Dummy link declarations to avoid destruction of existing links
{{ range $name, $id := .LinkIDs }}
resource "gns3_link" "{{ $name }}" {
  project_id     = gns3_project.project1.id
  node_a_id      = "dummy-node-a"
  node_a_adapter = 0
  node_a_port    = 0
  node_b_id      = "dummy-node-b"
  node_b_adapter = 0
  node_b_port    = 0

  lifecycle {
    ignore_changes = all
  }
}
{{ end }}

# 🔗 Management Links
resource "gns3_link" "ZTP_to_switch" {
  project_id     = gns3_project.project1.id
  node_a_id      = gns3_template.ztp.id
  node_a_adapter = 0
  node_a_port    = 0
  node_b_id      = gns3_switch.mgmt_switch.id
  node_b_adapter = 0
  node_b_port    = 1

  depends_on = [gns3_template.ztp, gns3_switch.mgmt_switch]
}

resource "gns3_link" "Cloud_to_switch" {
  project_id     = gns3_project.project1.id
  node_a_id      = gns3_cloud.cloud.id
  node_a_adapter = 0
  node_a_port    = 0
  node_b_id      = gns3_switch.mgmt_switch.id
  node_b_adapter = 0
  node_b_port    = 2

  depends_on = [gns3_cloud.cloud, gns3_switch.mgmt_switch]
}

# 🔁 Start all
resource "gns3_start_all" "start_nodes" {
  project_id = gns3_project.project1.id
  depends_on = [
    gns3_template.ztp,
    gns3_switch.mgmt_switch,
    gns3_cloud.cloud
  ]
}

# 🔌 Smart switch-to-router link generation
{{ $linkIDs := .LinkIDs }}
{{ range .NetworkDevices }}
{{ $linkName := printf "%s_to_switch" .Name }}
{{ if index $linkIDs $linkName }}
# 🔒 Dummy (imported) link for {{ $linkName }}
resource "gns3_link" "{{ $linkName }}" {
  project_id     = gns3_project.project1.id
  node_a_id      = "dummy-node-a"
  node_a_adapter = 0
  node_a_port    = 0
  node_b_id      = "dummy-node-b"
  node_b_adapter = 0
  node_b_port    = 0

  lifecycle {
    ignore_changes = all
  }
}
{{ else }}
# ✅ Real link for {{ .Name }} <-> mgmt-switch
resource "gns3_link" "{{ $linkName }}" {
  project_id     = gns3_project.project1.id
  node_a_id      = var.network_device_ids["{{ .Name }}"]
  node_a_adapter = 0
  node_a_port    = 0
  node_b_id      = gns3_switch.mgmt_switch.id
  node_b_adapter = 0
  node_b_port    = {{ .Port }}

  depends_on = [gns3_switch.mgmt_switch]
}
{{ end }}
{{ end }}
`

var (
	autoBridgeConfigFile string
	ztpTemplateName      string
	deployStateDir       string
)

var gns3AutoBridgeCmd = &cobra.Command{
	Use:   "gns3-auto-bridge",
	Short: "Deploy management network (auto‑bridge) on top of an existing deploy‑yaml topology",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("📂 Reading simplified topology...")

		yamlData, err := ioutil.ReadFile(autoBridgeConfigFile)
		if err != nil {
			fmt.Println("❌ Error reading YAML file:", err)
			os.Exit(1)
		}

		var simpleTopology Topology
		if err := yaml.Unmarshal(yamlData, &simpleTopology); err != nil {
			fmt.Println("❌ Error parsing YAML:", err)
			os.Exit(1)
		}
		simpleTopology.ZTPTemplate = ztpTemplateName
		topology := generateAutoBridgeTopology(simpleTopology)

		fmt.Println("📡 Visualizing augmented topology with auto‑bridge...")
		visualizeTopology(topology)

		if err := os.MkdirAll("terraform", os.ModePerm); err != nil {
			fmt.Println("❌ Error creating terraform directory:", err)
			os.Exit(1)
		}

		// Load .auto.tfvars.json BEFORE generating Terraform file
		tfvarsPath := ""
		files, err := ioutil.ReadDir("terraform")
		if err != nil {
			fmt.Println("❌ Error reading terraform directory:", err)
			os.Exit(1)
		}
		for _, f := range files {
			if !f.IsDir() && strings.HasSuffix(f.Name(), ".auto.tfvars.json") {
				tfvarsPath = filepath.Join("terraform", f.Name())
				break
			}
		}

		if tfvarsPath == "" {
			fmt.Println("❌ No .auto.tfvars.json file found in terraform directory. Please ensure deploy‑yaml was run.")
			os.Exit(1)
		}

		// 🔥 Parse tfvars and inject LinkIDs + NetworkDeviceIDs into topology
		tfvarsContent, err := ioutil.ReadFile(tfvarsPath)
		if err != nil {
			fmt.Println("❌ Error reading tfvars:", err)
			os.Exit(1)
		}

		var parsedTfvars struct {
			LinkIDs          map[string]string `json:"link_ids"`
			NetworkDeviceIDs map[string]string `json:"network_device_ids"`
			ProjectDetails   struct {
				ProjectID string `json:"project_id"`
			} `json:"project_details"`
		}

		if err := json.Unmarshal(tfvarsContent, &parsedTfvars); err != nil {
			fmt.Println("❌ Error parsing tfvars JSON:", err)
			os.Exit(1)
		}

		topology.LinkIDs = parsedTfvars.LinkIDs
		topology.NetworkDeviceIDs = parsedTfvars.NetworkDeviceIDs

		// ✅ Now generate the Terraform file with LinkIDs injected
		if err := generateTerraformFile("terraform/main.tf", autoBridgeTerraformTemplate, topology); err != nil {
			fmt.Println("❌ Error generating Terraform file:", err)
			os.Exit(1)
		}

		fmt.Println("🚀 Initializing Terraform configuration...")
		runCommandInDir("terraform", []string{"init"}, "terraform")

		if topology.StartNodes {
			fmt.Println("🚀 Applying Terraform configuration (targeted for ZTP + start_all)...")
			runCommandInDir("terraform", []string{"apply", "-auto-approve", "-compact-warnings"}, "terraform")
		} else {
			fmt.Println("🚀 Applying full Terraform configuration...")
			runCommandInDir("terraform", []string{"apply", "-auto-approve"}, "terraform")
		}

		fmt.Println("🔄 Importing link resources into Terraform state...")
		if err := importOnlyLinks(tfvarsPath); err != nil {
			fmt.Printf("❌ Import failed: %v\n", err)
			os.Exit(1)
		}

		ztpIP := topology.ZTPServer
		if ztpIP == "" {
			fmt.Println("❌ ZTP server IP not found in topology YAML; cannot upload topology")
			os.Exit(1)
		}
		endpoint := fmt.Sprintf("http://%s:5000/upload-yaml", ztpIP)
		fmt.Printf("🚀 Uploading topology YAML to API endpoint %s ...\n", endpoint)
		if err := uploadTopologyUntilSuccess(autoBridgeConfigFile, endpoint); err != nil {
			fmt.Println("❌ Upload failed:", err)
			os.Exit(1)
		}
		fmt.Println("✅ Topology YAML successfully uploaded!")
	},
}

func init() {
	gns3AutoBridgeCmd.Flags().StringVarP(&autoBridgeConfigFile, "config", "c", "topology.yaml", "YAML topology file")
	gns3AutoBridgeCmd.Flags().StringVarP(&ztpTemplateName, "ztp-template", "z", "ztp", "Name of the ZTP template to use")
	gns3AutoBridgeCmd.Flags().StringVarP(&deployStateDir, "deploy-state-dir", "d", "terraform-deploy", "Directory containing the deploy‑yaml Terraform state")
	rootCmd.AddCommand(gns3AutoBridgeCmd)
}

func generateAutoBridgeTopology(simple Topology) Topology {
	topology := Topology{
		Project:          simple.Project,
		NetworkDevices:   simple.NetworkDevices, // already there
		StartNodes:       simple.StartNodes,
		ZTPTemplate:      simple.ZTPTemplate,
		Links:            simple.Links,
		ZTPServer:        simple.ZTPServer,
		TerraformVersion: simple.TerraformVersion,
	}

	// Add extra cloud and switch for management
	topology.Clouds = append(topology.Clouds, Cloud{Name: "Auto-Cloud"})
	topology.Switches = append(topology.Switches, Switch{Name: "Auto-Switch"})

	// Link: ZTP -> Auto-Switch
	topology.Links = append(topology.Links, Link{
		Endpoints: []Endpoint{
			{Name: "ZTP", Adapter: 0, Port: 0},
			{Name: "Auto-Switch", Adapter: 0, Port: 1},
		},
	})

	// Link: Cloud -> Auto-Switch
	topology.Links = append(topology.Links, Link{
		Endpoints: []Endpoint{
			{Name: "Auto-Cloud", Adapter: 0, Port: 0},
			{Name: "Auto-Switch", Adapter: 0, Port: 2},
		},
	})

	// 🔌 Link each network device to Auto-Switch starting from port 100
	port := 3
	// Instead of appending, just assign Port for each device.
	for i := range topology.NetworkDevices {
		topology.NetworkDevices[i].Port = port
		port++
	}

	return topology
}

func importOnlyLinks(tfvarsPath string) error {
	data, err := ioutil.ReadFile(tfvarsPath)
	if err != nil {
		return fmt.Errorf("read tfvars: %w", err)
	}

	var vars struct {
		LinkIDs        map[string]string `json:"link_ids"`
		ProjectDetails struct {
			ProjectID string `json:"project_id"`
		} `json:"project_details"`
	}
	if err := json.Unmarshal(data, &vars); err != nil {
		return fmt.Errorf("unmarshal tfvars: %w", err)
	}

	// 🔍 Get all resources already in state
	stateCmd := exec.Command("terraform", "state", "list")
	stateCmd.Dir = "terraform"
	stateOut, err := stateCmd.Output()
	if err != nil {
		return fmt.Errorf("terraform state list failed: %w", err)
	}
	stateResources := strings.Split(string(stateOut), "\n")

	// Convert state list into a map for fast lookup
	stateSet := make(map[string]bool)
	for _, res := range stateResources {
		stateSet[strings.TrimSpace(res)] = true
	}

	// 🔄 Loop through link_ids and import only if missing
	for name, id := range vars.LinkIDs {
		resourceName := fmt.Sprintf("gns3_link.%s", name)
		if stateSet[resourceName] {
			fmt.Printf("✅ Link %s already in state, skipping import.\n", name)
			continue
		}

		fmt.Printf("🔄 Importing link %s...\n", name)
		importCmd := exec.Command("terraform", "import", resourceName, fmt.Sprintf("%s:%s", vars.ProjectDetails.ProjectID, id))
		importCmd.Dir = "terraform"
		output, err := importCmd.CombinedOutput()
		if err != nil {
			fmt.Printf("❌ Import failed for link %s: %s\n", name, output)
			continue
		}
		fmt.Printf("✅ Imported link %s\n", name)
	}

	return nil
}

func uploadTopologyUntilSuccess(filePath, endpoint string) error {
	for {
		file, err := os.Open(filePath)
		if err != nil {
			return fmt.Errorf("open file: %w", err)
		}
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, err := writer.CreateFormFile("file", filePath)
		if err != nil {
			return fmt.Errorf("create form: %w", err)
		}
		_, err = io.Copy(part, file)
		file.Close()
		if err != nil {
			return fmt.Errorf("copy: %w", err)
		}
		writer.Close()

		req, err := http.NewRequest("POST", endpoint, &body)
		if err != nil {
			return fmt.Errorf("new request: %w", err)
		}
		req.Header.Set("Content-Type", writer.FormDataContentType())

		resp, err := http.DefaultClient.Do(req)
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			return nil
		}
		if resp != nil {
			resp.Body.Close()
		}
		fmt.Println("❌ Upload failed. Retrying...")
		time.Sleep(5 * time.Second)
	}
}
