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

var gns3ConfigureCmd = &cobra.Command{
	Use:   "gns3-configure",
	Short: "Render and execute Ansible playbooks for device configuration based on a deployment YAML",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("🚀 gns3-configure triggered")

		if configFile == "" {
			return fmt.Errorf("deployment YAML file must be provided using the --config flag")
		}

		data, err := ioutil.ReadFile(configFile)
		if err != nil {
			return fmt.Errorf("failed to read deployment file: %v", err)
		}

		// Modified to read from `network-device` instead of `routers`
		var deployment struct {
			NetworkDevices []Router `yaml:"network-device"`
		}
		if err := yaml.Unmarshal(data, &deployment); err != nil {
			return fmt.Errorf("failed to parse deployment YAML: %v", err)
		}

		fmt.Printf("🔍 Found %d network devices in deployment\n", len(deployment.NetworkDevices))

		tmpl, err := template.New("playbook").Funcs(sprig.TxtFuncMap()).Funcs(template.FuncMap{
			"maskToPrefix":      maskToPrefix,
			"cidrSubnetAddress": cidrSubnetAddress,
			"cidrToMask":        cidrToMask,
		}).Parse(ConfigureAristaTemplate)
		if err != nil {
			return fmt.Errorf("failed to parse ansible playbook template: %v", err)
		}

		for _, router := range deployment.NetworkDevices {
			if len(router.Config) == 0 {
				fmt.Printf("⚠️  Skipping router %s: no config block found\n", router.Name)
				continue
			}

			pbData := PlaybookData{
				RouterName:   router.Name,
				IPConfigs:    []IPConfig{},
				StaticRoutes: []StaticRoute{},
				OSPFv3:       nil,
				BGP:          nil,
			}

			for _, cfg := range router.Config {
				if cfg == nil || cfg.Interface == "" || cfg.IPAddress == "" {
					continue
				}
				ipCfg := IPConfig{
					Interface: cfg.Interface,
					IPAddress: cfg.IPAddress,
					Mask:      "255.255.255.0",
					Secondary: true,
				}
				pbData.IPConfigs = append(pbData.IPConfigs, ipCfg)

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
						switch v := ospf.Stub.(type) {
						case bool:
							if v {
								stubVal = map[string]interface{}{}
							}
						case map[interface{}]interface{}:
							stubVal = convertMap(v)
						case map[string]interface{}:
							stubVal = v
						}
					}
					var nssaVal interface{}
					if ospf.NSSA != nil {
						switch v := ospf.NSSA.(type) {
						case bool:
							if v {
								nssaVal = map[string]interface{}{
									"default_information_originate": true,
									"no_summary":                    false,
								}
							}
						case map[interface{}]interface{}:
							nssaVal = convertMap(v)
						case map[string]interface{}:
							nssaVal = v
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
				fmt.Printf("⚠️  Skipping router %s: no usable IP configuration\n", router.Name)
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

			if verbose {
				rendered, err := ioutil.ReadFile(tempFile.Name())
				if err == nil {
					fmt.Println("📄 Rendered Playbook:\n" + string(rendered))
				}
			}

			args := []string{tempFile.Name(), "-i", inventoryFile}
			if verbose {
				args = append(args, "-vvv")
			}
			ansibleCmd := exec.Command("ansible-playbook", args...)
			ansibleCmd.Env = os.Environ()

			fmt.Printf("▶️  Executing playbook for %s using inventory '%s'...\n", router.Name, inventoryFile)
			output, err := ansibleCmd.CombinedOutput()
			fmt.Println(string(output))
			if err != nil {
				return fmt.Errorf("❌ Playbook failed for router %s: %v", router.Name, err)
			}
			fmt.Printf("✅ Configuration applied successfully for %s.\n", router.Name)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(gns3ConfigureCmd)
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose logging")
	gns3ConfigureCmd.Flags().StringVarP(&configFile, "config", "c", "", "Deployment YAML file containing topology and device configuration")
	gns3ConfigureCmd.Flags().StringVar(&inventoryFile, "inventory", "./ansible-inventory.yaml", "Ansible inventory file path")
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
