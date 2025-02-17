terraform {
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
  name = "c7200"
}


resource "gns3_template" "router1" {
  name       = "Router1"
  project_id = gns3_project.project1.id
  template_id = data.gns3_template_id.router_template.id
  x          = 200   # Auto-spacing routers horizontally
  y          = 150  # Stagger routers vertically
}

resource "gns3_template" "router2" {
  name       = "Router2"
  project_id = gns3_project.project1.id
  template_id = data.gns3_template_id.router_template.id
  x          = 400   # Auto-spacing routers horizontally
  y          = 0  # Stagger routers vertically
}

resource "gns3_template" "router3" {
  name       = "Router3"
  project_id = gns3_project.project1.id
  template_id = data.gns3_template_id.router_template.id
  x          = 600   # Auto-spacing routers horizontally
  y          = 150  # Stagger routers vertically
}
