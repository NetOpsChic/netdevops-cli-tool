package cmd

const terraformTemplate = `terraform {
  required_providers {
    gns3 = {
      source  = "netopschic/gns3"
      version = "{{ .Project.TerraformVersion }}"
    }
  }
}

provider "gns3" {
  host = "http://localhost:3080"
}

resource "gns3_project" "project1" {
  name = "{{ .Project.Name }}"
}

# Templates (e.g., ZTP, Observer)
{{- range .Templates.Servers }}
data "gns3_template_id" "{{ .Name }}" {
  name = "{{ .Name }}"
}

resource "gns3_template" "{{ .Name }}" {
  name        = "{{ .Name }}"
  project_id  = gns3_project.project1.id
  template_id = data.gns3_template_id.{{ .Name }}.template_id
  start       = {{ .Start }}
}
{{- end }}

# Network Devices (Routers)
{{ range .NetworkDevice.Routers }}
resource "gns3_qemu_node" "{{ .Name }}" {
  project_id     = gns3_project.project1.id
  name           = "{{ .Name }}"
  adapter_type   = "e1000"
  adapters       = 10
  hda_disk_image = "{{ .Image }}"
  mac_address    = "{{ .MacAddress }}"
  cpus           = 2
  ram            = 2056
  platform       = "x86_64"
  start_vm       = true
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

# Node ID Lookups
{{ range .NetworkDevice.Routers }}
data "gns3_node_id" "{{ .Name }}" {
  project_id = gns3_project.project1.id
  name       = "{{ .Name }}"
  depends_on = [gns3_qemu_node.{{ .Name }}]
}
{{ end }}

{{ range .Switches }}
data "gns3_node_id" "{{ .Name }}" {
  project_id = gns3_project.project1.id
  name       = "{{ .Name }}"
  depends_on = [gns3_switch.{{ .Name }}]
}
{{ end }}

{{ range .Templates.Servers }}
data "gns3_node_id" "{{ .Name }}" {
  project_id = gns3_project.project1.id
  name       = "{{ .Name }}"
  depends_on = [gns3_template.{{ .Name }}]
}
{{ end }}

{{ range .Clouds }}
data "gns3_node_id" "{{ .Name }}" {
  project_id = gns3_project.project1.id
  name       = "{{ .Name }}"
  depends_on = [gns3_cloud.{{ .Name }}]
}
{{ end }}

# Links
{{ range .Links }}
resource "gns3_link" "{{ (index .Endpoints 0).Name }}_to_{{ (index .Endpoints 1).Name }}" {
  lifecycle {
    create_before_destroy = true
  }
  project_id     = gns3_project.project1.id
  node_a_id      = data.gns3_node_id.{{ (index .Endpoints 0).Name }}.id
  node_a_adapter = {{ (index .Endpoints 0).Adapter }}
  node_a_port    = {{ (index .Endpoints 0).Port }}
  node_b_id      = data.gns3_node_id.{{ (index .Endpoints 1).Name }}.id
  node_b_adapter = {{ (index .Endpoints 1).Adapter }}
  node_b_port    = {{ (index .Endpoints 1).Port }}
}
{{ end }}

{{ if .Project.StartNodes }}
resource "gns3_start_all" "start_nodes" {
  project_id = gns3_project.project1.id
  depends_on = [
    {{- range .NetworkDevice.Routers }}gns3_qemu_node.{{ .Name }},
    {{- end }}
    {{- range .Switches }}gns3_switch.{{ .Name }},
    {{- end }}
    {{- range .Clouds }}gns3_cloud.{{ .Name }},
    {{- end }}
    {{- range .Templates.Servers }}gns3_template.{{ .Name }},
    {{- end }}
    {{- range .Links }}gns3_link.{{ (index .Endpoints 0).Name }}_to_{{ (index .Endpoints 1).Name }},
    {{- end }}
  ]
}
{{ end }}

`
