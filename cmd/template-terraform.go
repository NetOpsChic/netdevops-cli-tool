package cmd

const terraformTemplate = `terraform {
  required_providers {
    gns3 = {
      source  = "netopschic/gns3"
      version = "{{ .Topology.Project.TerraformVersion }}"
    }
  }
}

provider "gns3" {
  host = "{{ .Topology.Project.GNS3Server }}"
}

resource "gns3_project" "project1" {
  name = "{{ .Topology.Project.Name }}"
}

# --- Unique GNS3 Template Lookups (for routers & servers) ---
{{- range $templateName, $_ := .UniqueTemplateNames }}
data "gns3_template_id" "{{ $templateName }}" {
  name = "{{ $templateName }}"
}
{{- end }}

# --- Template Routers ---
{{- range .TemplateRouters }}
resource "gns3_template" "{{ .Name }}" {
  name        = "{{ .Name }}"
  project_id  = gns3_project.project1.id
  template_id = data.gns3_template_id.{{ .TemplateName }}.template_id
  start       = {{ if .Start }}{{ .Start }}{{ else }}true{{ end }}
}
data "gns3_node_id" "{{ .Name }}" {
  project_id = gns3_project.project1.id
  name       = "{{ .Name }}"
  depends_on = [gns3_template.{{ .Name }}]
}
{{- end }}

# --- Template Servers ---
{{- range .TemplateServers }}
resource "gns3_template" "{{ .Name }}" {
  name        = "{{ .Name }}"
  project_id  = gns3_project.project1.id
  template_id = data.gns3_template_id.{{ .Name }}.template_id
  start       = {{ if .Start }}{{ .Start }}{{ else }}true{{ end }}
}
data "gns3_node_id" "{{ .Name }}" {
  project_id = gns3_project.project1.id
  name       = "{{ .Name }}"
  depends_on = [gns3_template.{{ .Name }}]
}
{{- end }}

# --- QEMU Routers (Network Device) ---
{{- range .Topology.NetworkDevice.Routers }}
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
data "gns3_node_id" "{{ .Name }}" {
  project_id = gns3_project.project1.id
  name       = "{{ .Name }}"
  depends_on = [gns3_qemu_node.{{ .Name }}]
}
{{- end }}

# --- Switches ---
{{- range .Topology.Switches }}
resource "gns3_switch" "{{ .Name }}" {
  name       = "{{ .Name }}"
  project_id = gns3_project.project1.id
}
data "gns3_node_id" "{{ .Name }}" {
  project_id = gns3_project.project1.id
  name       = "{{ .Name }}"
  depends_on = [gns3_switch.{{ .Name }}]
}
{{- end }}

# --- Clouds ---
{{- range .Topology.Clouds }}
resource "gns3_cloud" "{{ .Name }}" {
  name       = "{{ .Name }}"
  project_id = gns3_project.project1.id
}
data "gns3_node_id" "{{ .Name }}" {
  project_id = gns3_project.project1.id
  name       = "{{ .Name }}"
  depends_on = [gns3_cloud.{{ .Name }}]
}
{{- end }}

# --- Links ---
{{- range .Topology.Links }}
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
{{- end }}

{{ if .Topology.Project.StartNodes }}
resource "gns3_start_all" "start_nodes" {
  project_id = gns3_project.project1.id
  depends_on = [
    {{- range .Topology.NetworkDevice.Routers }}gns3_qemu_node.{{ .Name }},
    {{- end }}
    {{- range .TemplateRouters }}gns3_template.{{ .Name }},
    {{- end }}
    {{- range .TemplateServers }}gns3_template.{{ .Name }},
    {{- end }}
    {{- range .Topology.Switches }}gns3_switch.{{ .Name }},
    {{- end }}
    {{- range .Topology.Clouds }}gns3_cloud.{{ .Name }},
    {{- end }}
    {{- range .Topology.Links }}gns3_link.{{ (index .Endpoints 0).Name }}_to_{{ (index .Endpoints 1).Name }},
    {{- end }}
  ]
}
{{ end }}

`
