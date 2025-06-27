package cmd

import (
	"fmt"
	"strings"
	"time"
)

// Topology represents the complete network topology shared between CLI and YAML modes.
type Topology struct {
	Project struct {
		Name             string `yaml:"name"`
		StartNodes       bool   `yaml:"start_nodes"`
		GNS3Server       string `yaml:"gns3_server"`
		TerraformVersion string `yaml:"terraform_version"`
	} `yaml:"project"`

	NetworkDevice struct {
		Routers []NetworkDevice `yaml:"routers"`
	} `yaml:"network-device"`

	Switches  []Switch      `yaml:"switches"`
	Clouds    []Cloud       `yaml:"clouds"`
	Templates TemplateGroup `yaml:"templates"`
	Links     []Link        `yaml:"links"`

	ZTPServer        string            `yaml:"-"` // Extracted from ztp-server in templates
	LinkIDs          map[string]string `yaml:"-"`
	NetworkDeviceIDs map[string]string `yaml:"-"`
}

func (cl *ConfigList) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var list []*ConfigBlock
	if err := unmarshal(&list); err == nil {
		*cl = list
		return nil
	}
	var single ConfigBlock
	if err := unmarshal(&single); err != nil {
		return err
	}
	*cl = []*ConfigBlock{&single}
	return nil
}

type NetworkDevice struct {
	Name       string      `yaml:"name"`
	Hostname   string      `yaml:"hostname"`
	Vendor     string      `yaml:"vendor"`
	MacAddress string      `yaml:"mac_address"`
	Image      string      `yaml:"image"`
	Config     interface{} `yaml:"config"`
	Port       int         `yaml:"-"`
}

type TemplateGroup struct {
	Servers []TemplateServer `yaml:"servers"`
	Routers []Router         `yaml:"routers"`
}

type TemplateServer struct {
	Name         string `yaml:"name"`
	TemplateName string `yaml:"template_name"`
	Start        bool   `yaml:"start"`
	ZTPServer    string `yaml:"ztp_server,omitempty"` // Only applicable to ztp-server
}
type TemplateData struct {
	Templates struct {
		Servers []TemplateServer
		Routers []Router
	}
	UniqueRouterTemplates map[string]bool // e.g. {"arista-eos":true}
}

// Router defines a router device.
type Router struct {
	Name         string     `yaml:"name"`
	Vendor       string     `yaml:"vendor"` // Added to support YAML input (e.g., "arista")
	Template     string     `yaml:"template"`
	Config       ConfigList `yaml:"config"`
	Start        bool       `yaml:"start"`
	TemplateName string     `yaml:"template_name"`
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
type OSPFInterface struct {
	Name    string
	Cost    int
	Passive bool
}

type OSPFConfig struct {
	RouterID     string
	Area         string
	Networks     []string
	Interfaces   []OSPFInterface
	Stub         interface{}
	NSSA         interface{}
	Redistribute []Redistribution
}

type PlaybookData struct {
	RouterName   string
	IPConfigs    []IPConfig
	StaticRoutes []StaticRoute
	OSPF         *OSPFConfig
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

// ConfigList is a list of configuration blocks, supporting a single block or a list.
type ConfigList []*ConfigBlock

// Deployment holds the deployment configuration from YAML.
type Deployment struct {
	Project struct {
		Name             string `yaml:"name"`
		StartNodes       bool   `yaml:"start_nodes"`
		TerraformVersion string `yaml:"terraform_version"`
	} `yaml:"project"`

	Templates struct {
		Servers []struct {
			Name      string `yaml:"name"`
			Start     bool   `yaml:"start"`
			ZTPServer string `yaml:"ztp_server,omitempty"`
		} `yaml:"servers"`
	} `yaml:"templates"`

	NetworkDevice struct {
		Routers []struct {
			Name       string      `yaml:"name"`
			Hostname   string      `yaml:"hostname"`
			Vendor     string      `yaml:"vendor"`
			MacAddress string      `yaml:"mac_address"`
			Image      string      `yaml:"image"`
			Config     interface{} `yaml:"config"`
		} `yaml:"routers"`
	} `yaml:"network-device"`
}

type DeviceConfig struct {
	Name       string     `yaml:"name"`
	Hostname   string     `yaml:"hostname"`
	Vendor     string     `yaml:"vendor"`
	MACAddress string     `yaml:"mac_address"`
	Image      string     `yaml:"image"`
	Config     ConfigList `yaml:"config"`
}
type ZTPDevice struct {
	Name            string `json:"name"`
	IP              string `json:"ip"`
	AnsibleUser     string `json:"ansible_user"`
	AnsiblePassword string `json:"ansible_password"`
}

var (
	projectID     string
	containerID   string
	ztpIP         string
	skipZTP       bool
	manualDevices []string
	vendor        string
	configFile    string
	inventoryFile string = "ansible-inventory/inventory.yaml"
)

const (
	colorRed    = "\x1b[31m"
	colorYellow = "\x1b[33m"
	colorCyan   = "\x1b[36m"
	colorReset  = "\x1b[0m"
)

// prettyYAMLErrors prints a multiline yaml.Unmarshal error with colorized bullets.
func prettyYAMLErrors(err error) {
	fmt.Printf("%s❌ Failed to parse topology YAML. Please fix the following errors:%s\n", colorRed, colorReset)
	for _, line := range strings.Split(err.Error(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fmt.Printf("  %s•%s %s%s%s\n",
			colorCyan, // bullet color
			colorReset,
			colorYellow, // message color
			line,
			colorReset,
		)
	}
}

var (
	gns3Server        string
	reconcileInterval = 30 * time.Second
	detach            bool
)

// RawLink is built directly from your YAML (uses node names).
type RawLink struct {
	Nodes []struct {
		NodeName      string `json:"node_name"`
		AdapterNumber int    `json:"adapter_number"`
		PortNumber    int    `json:"port_number"`
	} `json:"nodes"`
}

type projectTemplate struct {
	ID   string `json:"template_id"`
	Name string `json:"name"`
}

// ─── Payloads you already use in reconcile.go ─────────────────────────────────

type NodeCreatePayload struct {
	Name         string                 `json:"name"`
	TemplateID   string                 `json:"template_id,omitempty"`
	NodeType     string                 `json:"node_type,omitempty"`
	ComputeId    string                 `json:"compute_id,omitempty"`
	Properties   map[string]interface{} `json:"properties,omitempty"`
	TemplateName string                 `json:"-"`
	ResourceType string
}
type linkEndpoint struct {
	NodeName      string `json:"node_name,omitempty"`
	NodeID        string `json:"node_id,omitempty"`
	AdapterNumber int    `json:"adapter_number"`
	PortNumber    int    `json:"port_number"`
}

type LinkCreatePayload struct {
	Nodes []linkEndpoint `json:"nodes"`
}

// ─── Observed (GNS3 API) ──────────────────────────────────────────────────────

type ObservedNode struct {
	ID           string `json:"node_id"`
	Name         string `json:"name"`
	Status       string `json:"status"`
	NodeType     string `json:"node_type"`
	ResourceType string
}

type ObservedLink struct {
	ID    string                 `json:"link_id"`
	Nodes []ObservedLinkEndpoint `json:"nodes"`
}

type ObservedLinkEndpoint struct {
	NodeID        string `json:"node_id"`
	AdapterNumber int    `json:"adapter_number"`
	PortNumber    int    `json:"port_number"`
}
type TerraformLinkResource struct {
	ID        string
	EndpointA string
	EndpointB string
}

// ─── Templates (GNS3 API) ─────────────────────────────────────────────────────

type Template struct {
	TemplateID string `json:"template_id"`
	Name       string `json:"name"`
}

type ProjectTemplate struct {
	TemplateID string `json:"template_id"`
	Name       string `json:"name"`
}
type TerraformResource struct {
	Type string // e.g. "gns3_qemu_node", "gns3_switch", etc.
	Name string // e.g. "R1"
	ID   string // GNS3 node ID
}

func UniqueTemplateNames(tg TemplateGroup) map[string]bool {
	unique := make(map[string]bool)
	for _, r := range tg.Routers {
		if r.TemplateName != "" {
			unique[r.TemplateName] = true
		}
	}
	for _, s := range tg.Servers {
		if s.Name != "" {
			unique[s.Name] = true
		}
		if s.TemplateName != "" {
			unique[s.TemplateName] = true
		}
	}
	return unique
}

type TemplateContext struct {
	Topology            *Topology
	Templates           TemplateGroup
	UniqueTemplateNames map[string]bool
}
