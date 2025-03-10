package cmd

// Topology represents the complete network topology shared between CLI and YAML modes.
type Topology struct {
	Project    string   `yaml:"project"`
	Routers    []Router `yaml:"routers"`
	Switches   []Switch `yaml:"switches"`
	Clouds     []Cloud  `yaml:"clouds"`
	Links      []Link   `yaml:"links"`
	StartNodes bool     `yaml:"start_nodes"`
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

var (
	// configFile holds the path to the deployment YAML file.
	configFile string

	// inventoryFile holds the path to the ansible inventory file.
	inventoryFile string = "./ansible-inventory.yaml"
)

// IPConfig represents a single interface IP configuration.
type IPConfig struct {
	Interface string
	IPAddress string
	Mask      string // You can set a default (e.g., "255.255.255.0") if not provided.
	Secondary bool
}

// StaticRoute represents a static route configuration.
type StaticRoute struct {
	DestNetwork string
	SubnetMask  string
	NextHop     string
	Interface   string
}

// OSPFv3Interface represents OSPFv3 settings for a given interface.
type OSPFv3Interface struct {
	Name    string
	Cost    int
	Passive bool
}

type OSPFv3Config struct {
	RouterID     string
	Area         string
	Networks     []string
	Interfaces   []OSPFv3Interface
	Stub         interface{}
	NSSA         interface{}
	Redistribute []Redistribution // Newly added
}

type BGPConfig struct {
	LocalAS      int
	Neighbor     string
	Redistribute []Redistribution
}

type PlaybookData struct {
	RouterName   string
	IPConfigs    []IPConfig
	StaticRoutes []StaticRoute
	OSPFv3       *OSPFv3Config
	BGP          *BGPConfig // Newly added
}
