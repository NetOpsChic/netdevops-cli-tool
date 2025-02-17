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


resource "gns3_template" "R1" {
  name       = "R1"
  project_id = gns3_project.project1.id
  template_id = data.gns3_template_id.R1.id
  x          = 0   # Auto-space routers horizontally
  y          = 0  # Stagger routers vertically
}

data "gns3_template_id" "R1" {
  name = "c7200"
}

resource "gns3_template" "R2" {
  name       = "R2"
  project_id = gns3_project.project1.id
  template_id = data.gns3_template_id.R2.id
  x          = 200   # Auto-space routers horizontally
  y          = 150  # Stagger routers vertically
}

data "gns3_template_id" "R2" {
  name = "c7200"
}

resource "gns3_template" "R3" {
  name       = "R3"
  project_id = gns3_project.project1.id
  template_id = data.gns3_template_id.R3.id
  x          = 400   # Auto-space routers horizontally
  y          = 0  # Stagger routers vertically
}

data "gns3_template_id" "R3" {
  name = "c7200"
}



resource "gns3_link" "R1_to_R2" {
  project_id     = gns3_project.project1.id
  node_a_id      = gns3_template.R1.id
  node_a_adapter = 0
  node_a_port    = 0
  node_b_id      = gns3_template.R2.id
  node_b_adapter = 0
  node_b_port    = 0

  depends_on = [
    gns3_template.R1,
    gns3_template.R2
  ]
}

resource "gns3_link" "R2_to_R3" {
  project_id     = gns3_project.project1.id
  node_a_id      = gns3_template.R2.id
  node_a_adapter = 0
  node_a_port    = 0
  node_b_id      = gns3_template.R3.id
  node_b_adapter = 0
  node_b_port    = 0

  depends_on = [
    gns3_template.R2,
    gns3_template.R3
  ]
}
