package cmd

// Topology represents the complete network topology shared between CLI and YAML modes.
type Topology struct {
	Project          string            `yaml:"project"`
	Routers          []Router          `yaml:"routers"`
	Switches         []Switch          `yaml:"switches"`
	Clouds           []Cloud           `yaml:"clouds"`
	Links            []Link            `yaml:"links"`
	StartNodes       bool              `yaml:"start_nodes"`
	ZTPTemplate      string            `yaml:"-"`
	ZTPServer        string            `yaml:"ztp_server"`
	NetworkDevices   []NetworkDevice   `yaml:"network-device"`
	TerraformVersion string            `yaml:"terraform_version"`
	LinkIDs          map[string]string `yaml:"-"`
}
type NetworkDevice struct {
	Name       string      `yaml:"name"`
	Hostname   string      `yaml:"hostname"`
	Vendor     string      `yaml:"vendor"`
	MacAddress string      `yaml:"mac_address"`
	Image      string      `yaml:"image"`  // formerly "template"
	Config     interface{} `yaml:"config"` // or a more detailed type if needed
}

// Router defines a router device.
type Router struct {
	Name     string     `yaml:"name"`
	Vendor   string     `yaml:"vendor"` // Added to support YAML input (e.g., "arista")
	Template string     `yaml:"template"`
	Config   ConfigList `yaml:"config"`
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
type CLILink = Link

var (
	// configFile holds the path to the deployment YAML file.

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
	Redistribute []Redistribution
}

type PlaybookData struct {
	RouterName   string
	IPConfigs    []IPConfig
	StaticRoutes []StaticRoute
	OSPFv3       *OSPFv3Config
	BGP          *BGPConfig
}

// BGPConfig represents the BGP configuration.
type BGPConfig struct {
	RouterID     string
	LocalAS      int
	RemoteAS     int
	Neighbor     string
	Networks     []string
	Redistribute []Redistribution
}

// Redistribution represents a redistribution rule.
type Redistribution struct {
	Protocol  string `yaml:"protocol"`
	Metric    int    `yaml:"metric,omitempty"`
	RouteMap  string `yaml:"route_map,omitempty"`
	IsisLevel string `yaml:"isis_level,omitempty"` // Optional; if provided, passed to module
	OspfRoute string `yaml:"ospf_route,omitempty"`
}

// Deployment represents the deployment configuration for CLI/YAML mode.
type Deployment struct {
	Project    string   `yaml:"project"`
	StartNodes bool     `yaml:"start_nodes"`
	ZTPServer  string   `yaml:"ztp-server"` // Added to read the ZTP server IP from YAML.
	Routers    []Router `yaml:"routers"`
}

// ConfigList is a list of configuration blocks, supporting a single block or a list.
type ConfigList []*ConfigBlock
