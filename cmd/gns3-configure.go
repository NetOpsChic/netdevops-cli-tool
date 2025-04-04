// File: cmd/gns3-configure.go
package cmd

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig/v3"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
)

var verbose bool

// ConfigBlock represents a single configuration block for a router.
type ConfigBlock struct {
	Interface    string `yaml:"interface"`
	IPAddress    string `yaml:"ip_address"`
	StaticRoutes []struct {
		DestNetwork string `yaml:"dest_network"`
		SubnetMask  string `yaml:"subnet_mask"`
		NextHop     string `yaml:"next_hop"`
		Interface   string `yaml:"interface,omitempty"`
	} `yaml:"static_routes"`
	OSPFv3 *struct {
		RouterID   string   `yaml:"router_id"`
		Area       string   `yaml:"area"`
		Networks   []string `yaml:"networks"`
		Interfaces []struct {
			Name    string `yaml:"name"`
			Cost    int    `yaml:"cost"`
			Passive bool   `yaml:"passive"`
		} `yaml:"interfaces"`
		Stub         interface{}      `yaml:"stub"`
		NSSA         interface{}      `yaml:"nssa"`
		Redistribute []Redistribution `yaml:"redistribute"`
	} `yaml:"ospfv3"`
	BGP *struct {
		LocalAS      int              `yaml:"local_as"`
		RouterID     string           `yaml:"router_id"`
		RemoteAS     int              `yaml:"remote_as"`
		Neighbor     string           `yaml:"neighbor"`
		Networks     []string         `yaml:"networks,omitempty"`
		Redistribute []Redistribution `yaml:"redistribute"`
	} `yaml:"bgp"`
}

// UnmarshalYAML allows ConfigList to be unmarshaled from either a single mapping or a list.
func (cl *ConfigList) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// Try unmarshaling as a list.
	var list []*ConfigBlock
	if err := unmarshal(&list); err == nil {
		*cl = list
		return nil
	}
	// Otherwise try unmarshaling as a single mapping.
	var single ConfigBlock
	if err := unmarshal(&single); err != nil {
		return err
	}
	*cl = []*ConfigBlock{&single}
	return nil
}

var gns3ConfigureCmd = &cobra.Command{
	Use:   "gns3-configure",
	Short: "Render and execute Ansible playbooks for device configuration based on a deployment YAML",
	RunE: func(cmd *cobra.Command, args []string) error {
		if configFile == "" {
			return fmt.Errorf("deployment YAML file must be provided using the --config flag")
		}

		data, err := ioutil.ReadFile(configFile)
		if err != nil {
			return fmt.Errorf("failed to read deployment file: %v", err)
		}

		var deployment Deployment
		if err := yaml.Unmarshal(data, &deployment); err != nil {
			return fmt.Errorf("failed to parse deployment YAML: %v", err)
		}

		tmpl, err := template.New("playbook").Funcs(sprig.TxtFuncMap()).Funcs(template.FuncMap{
			"maskToPrefix":      maskToPrefix,
			"cidrSubnetAddress": cidrSubnetAddress,
			"cidrToMask":        cidrToMask,
		}).Parse(ConfigureAristaTemplate)
		if err != nil {
			return fmt.Errorf("failed to parse ansible playbook template: %v", err)
		}

		for _, router := range deployment.Routers {
			// Skip if no configuration blocks are provided.
			if len(router.Config) == 0 {
				continue
			}

			pbData := PlaybookData{
				RouterName:   router.Name,
				IPConfigs:    []IPConfig{},
				StaticRoutes: []StaticRoute{},
				OSPFv3:       nil,
				BGP:          nil,
			}

			// Iterate over each configuration block.
			for _, cfg := range router.Config {
				if cfg == nil || cfg.Interface == "" || cfg.IPAddress == "" {
					continue
				}
				// Append IP configuration.
				ipCfg := IPConfig{
					Interface: cfg.Interface,
					IPAddress: cfg.IPAddress,
					Mask:      "255.255.255.0", // Alternatively, derive from cfg.IPAddress using cidrToMask.
					Secondary: true,
				}
				pbData.IPConfigs = append(pbData.IPConfigs, ipCfg)

				// Append static routes if available.
				if cfg.StaticRoutes != nil {
					for _, sr := range cfg.StaticRoutes {
						pbData.StaticRoutes = append(pbData.StaticRoutes, StaticRoute{
							DestNetwork: sr.DestNetwork,
							SubnetMask:  sr.SubnetMask,
							NextHop:     sr.NextHop,
							Interface:   sr.Interface,
						})
					}
				}

				// Use the first available OSPFv3 config.
				if pbData.OSPFv3 == nil && cfg.OSPFv3 != nil {
					ospf := cfg.OSPFv3
					var ospfIfaces []OSPFv3Interface
					for _, iface := range ospf.Interfaces {
						ospfIfaces = append(ospfIfaces, OSPFv3Interface{
							Name:    iface.Name,
							Cost:    iface.Cost,
							Passive: iface.Passive,
						})
					}
					var stubVal interface{}
					if ospf.Stub != nil {
						if v, ok := ospf.Stub.(bool); ok {
							if v {
								stubVal = map[string]interface{}{}
							} else {
								stubVal = nil
							}
						} else if m, ok := ospf.Stub.(map[interface{}]interface{}); ok {
							stubVal = convertMap(m)
						} else if m, ok := ospf.Stub.(map[string]interface{}); ok {
							stubVal = m
						}
					}
					var nssaVal interface{}
					if ospf.NSSA != nil {
						if v, ok := ospf.NSSA.(bool); ok {
							if v {
								nssaVal = map[string]interface{}{
									"default_information_originate": true,
									"no_summary":                    false,
								}
							} else {
								nssaVal = nil
							}
						} else if m, ok := ospf.NSSA.(map[interface{}]interface{}); ok {
							nssaVal = convertMap(m)
						} else if m, ok := ospf.NSSA.(map[string]interface{}); ok {
							nssaVal = m
						}
					}
					pbData.OSPFv3 = &OSPFv3Config{
						RouterID:     ospf.RouterID,
						Area:         ospf.Area,
						Networks:     ospf.Networks,
						Interfaces:   ospfIfaces,
						Stub:         stubVal,
						NSSA:         nssaVal,
						Redistribute: ospf.Redistribute,
					}
				}

				// Use the first available BGP config.
				if pbData.BGP == nil && cfg.BGP != nil {
					bgp := cfg.BGP
					pbData.BGP = &BGPConfig{
						LocalAS:      bgp.LocalAS,
						RouterID:     bgp.RouterID,
						RemoteAS:     bgp.RemoteAS,
						Neighbor:     bgp.Neighbor,
						Networks:     bgp.Networks,
						Redistribute: bgp.Redistribute,
					}
				}
			}

			if len(pbData.IPConfigs) == 0 {
				continue
			}

			tempFile, err := ioutil.TempFile("", fmt.Sprintf("configure_%s-*.yml", router.Name))
			if err != nil {
				return fmt.Errorf("failed to create temp file for router %s: %v", router.Name, err)
			}
			defer os.Remove(tempFile.Name())

			if err := tmpl.Execute(tempFile, pbData); err != nil {
				return fmt.Errorf("failed to render playbook for router %s: %v", router.Name, err)
			}
			tempFile.Close()

			// If verbose flag is enabled, print the rendered playbook.
			if verbose {
				rendered, err := ioutil.ReadFile(tempFile.Name())
				if err != nil {
					fmt.Printf("Warning: unable to read rendered playbook: %v\n", err)
				} else {
					fmt.Println("Rendered Playbook:")
					fmt.Println(string(rendered))
				}
			}

			// Build ansible-playbook command arguments.
			args := []string{tempFile.Name(), "-i", inventoryFile}
			if verbose {
				args = append(args, "-vvv")
			}
			ansibleCmd := exec.Command("ansible-playbook", args...)
			ansibleCmd.Env = os.Environ()

			fmt.Printf("Executing ansible playbook for router %s with inventory '%s'...\n", router.Name, inventoryFile)
			output, err := ansibleCmd.CombinedOutput()
			fmt.Println(string(output))
			if err != nil {
				return fmt.Errorf("ansible-playbook execution failed for router %s: %v", router.Name, err)
			}
			fmt.Printf("Configuration applied successfully for router %s.\n", router.Name)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(gns3ConfigureCmd)
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose logging")
	gns3ConfigureCmd.Flags().StringVarP(&configFile, "config", "c", "", "Deployment YAML file containing topology and device configuration")
	gns3ConfigureCmd.Flags().StringVar(&inventoryFile, "inventory", "./ansible-inventory.yaml", "Ansible inventory file path (default is ./ansible-inventory.yaml)")
}

func convertMap(in map[interface{}]interface{}) map[string]interface{} {
	out := make(map[string]interface{})
	for key, value := range in {
		out[fmt.Sprintf("%v", key)] = value
	}
	return out
}

func maskToPrefix(mask string) string {
	var count int
	for _, octet := range strings.Split(mask, ".") {
		var num int
		fmt.Sscanf(octet, "%d", &num)
		for num > 0 {
			count += num & 1
			num = num >> 1
		}
	}
	return fmt.Sprintf("%d", count)
}

func cidrSubnetAddress(cidr string) string {
	parts := strings.Split(cidr, "/")
	if len(parts) != 2 {
		return ""
	}
	return parts[0]
}

func cidrToMask(cidr string) string {
	parts := strings.Split(cidr, "/")
	if len(parts) != 2 {
		return ""
	}
	prefix, err := strconv.Atoi(parts[1])
	if err != nil {
		return ""
	}
	mask := net.CIDRMask(prefix, 32)
	return net.IP(mask).String()
}
