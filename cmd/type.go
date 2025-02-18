package cmd

// Topology structure shared between CLI and YAML modes
type Topology struct {
	Project  string   `yaml:"project"`
	Routers  []Router `yaml:"routers"`
	Switches []Switch `yaml:"switches"`
	Clouds   []Cloud  `yaml:"clouds"`
	Links    []Link   `yaml:"links"`
}

// Router structure
type Router struct {
	Name     string `yaml:"name"`
	Template string `yaml:"template"`
}

// Switch structure
type Switch struct {
	Name string `yaml:"name"`
}

// Cloud structure
type Cloud struct {
	Name string `yaml:"name"`
}

// Endpoint structure for defining device interfaces
type Endpoint struct {
	Name    string `yaml:"name"`
	Adapter int    `yaml:"adapter"`
	Port    int    `yaml:"port"`
}

// Link structure for connections between nodes
type Link struct {
	Endpoints []Endpoint `yaml:"endpoints"`
}

// CLI-Specific Link Struct
type CLILink struct {
	Endpoints []Endpoint
}
