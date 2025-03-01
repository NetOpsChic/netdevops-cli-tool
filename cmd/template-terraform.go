package cmd

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
