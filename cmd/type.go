package cmd

// Topology represents the complete network topology shared between CLI and YAML modes.
type Topology struct {
	Project  string   `yaml:"project"`
	Routers  []Router `yaml:"routers"`
	Switches []Switch `yaml:"switches"`
	Clouds   []Cloud  `yaml:"clouds"`
	Links    []Link   `yaml:"links"`
}

// Router defines a router device.
type Router struct {
	Name     string `yaml:"name"`
	Template string `yaml:"template"`
}

// Switch defines a switch device.
type Switch struct {
	Name string `yaml:"name"`
}

// Cloud defines a cloud device.
type Cloud struct {
	Name string `yaml:"name"`
}

// Endpoint defines a device interface, including adapter and port numbers.
type Endpoint struct {
	Name    string `yaml:"name"`
	Adapter int    `yaml:"adapter"`
	Port    int    `yaml:"port"`
}

// Link defines a connection between two endpoints.
type Link struct {
	Endpoints []Endpoint `yaml:"endpoints"`
}

// CLILink is an alias for Link, used in CLI mode.
// This ensures that CLI code can use the same structure as YAML mode without duplication.
type CLILink = Link
