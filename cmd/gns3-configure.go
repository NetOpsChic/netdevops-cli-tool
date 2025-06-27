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
	OSPF *struct {
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
	} `yaml:"ospf"`
	BGP *struct {
		LocalAS      int              `yaml:"local_as"`
		RouterID     string           `yaml:"router_id"`
		RemoteAS     int              `yaml:"remote_as"`
		Neighbor     string           `yaml:"neighbor"`
		Networks     []string         `yaml:"networks,omitempty"`
		Redistribute []Redistribution `yaml:"redistribute"`
	} `yaml:"bgp"`
}

var gns3ConfigureCmd = &cobra.Command{
	Use:   "gns3-configure",
	Short: "Render and execute Ansible playbooks for device configuration based on a deployment YAML",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("🚀 gns3-configure triggered")

		if configFile == "" {
			return fmt.Errorf("deployment YAML file must be provided using the --config flag")
		}

		// Read raw YAML
		data, err := ioutil.ReadFile(configFile)
		if err != nil {
			return fmt.Errorf("failed to read deployment file: %v", err)
		}

		// // === VALIDATION ===
		// var fullTopo Topology
		// if err := yaml.Unmarshal(data, &fullTopo); err != nil {
		// 	prettyYAMLErrors(err)
		// 	return fmt.Errorf("cannot continue due to invalid YAML")
		// }
		// if err := validateTopology(&fullTopo); err != nil {
		// 	fmt.Println("❌ Topology validation failed:")
		// 	fmt.Println(err)
		// 	return fmt.Errorf("cannot continue due to invalid topology")
		// }
		// === END VALIDATION ===

		// Existing logic: unmarshal just routers
		var deployment struct {
			NetworkDevice struct {
				Routers []Router `yaml:"routers"`
			} `yaml:"network-device"`
		}
		if err := yaml.Unmarshal(data, &deployment); err != nil {
			return fmt.Errorf("failed to parse deployment YAML: %v", err)
		}

		routers := deployment.NetworkDevice.Routers
		fmt.Printf("🔍 Found %d routers in deployment\n", len(routers))

		tmpl, err := template.New("playbook").Funcs(sprig.TxtFuncMap()).Funcs(template.FuncMap{
			"maskToPrefix":      maskToPrefix,
			"cidrSubnetAddress": cidrSubnetAddress,
			"cidrToMask":        cidrToMask,
		}).Parse(ConfigureAristaTemplate)
		if err != nil {
			return fmt.Errorf("failed to parse ansible playbook template: %v", err)
		}

		for _, router := range routers {
			if len(router.Config) == 0 {
				fmt.Printf("⚠️  Skipping router %s: no config block found\n", router.Name)
				continue
			}

			pbData := PlaybookData{
				RouterName:   router.Name,
				IPConfigs:    []IPConfig{},
				StaticRoutes: []StaticRoute{},
			}

			for _, cfg := range router.Config {
				if cfg.Interface != "" && cfg.IPAddress != "" {
					pbData.IPConfigs = append(pbData.IPConfigs, IPConfig{
						Interface: cfg.Interface,
						IPAddress: cfg.IPAddress,
						Mask:      "255.255.255.0",
						Secondary: true,
					})
				}

				for _, sr := range cfg.StaticRoutes {
					pbData.StaticRoutes = append(pbData.StaticRoutes, StaticRoute{
						DestNetwork: sr.DestNetwork,
						SubnetMask:  sr.SubnetMask,
						NextHop:     sr.NextHop,
						Interface:   sr.Interface,
					})
				}

				if pbData.OSPF == nil && cfg.OSPF != nil {
					var ifaces []OSPFInterface
					for _, i := range cfg.OSPF.Interfaces {
						ifaces = append(ifaces, OSPFInterface{
							Name:    i.Name,
							Cost:    i.Cost,
							Passive: i.Passive,
						})
					}
					pbData.OSPF = &OSPFConfig{
						RouterID:     cfg.OSPF.RouterID,
						Area:         cfg.OSPF.Area,
						Networks:     cfg.OSPF.Networks,
						Interfaces:   ifaces,
						Stub:         cfg.OSPF.Stub,
						NSSA:         cfg.OSPF.NSSA,
						Redistribute: cfg.OSPF.Redistribute,
					}
				}

				if pbData.BGP == nil && cfg.BGP != nil {
					pbData.BGP = &BGPConfig{
						LocalAS:      cfg.BGP.LocalAS,
						RouterID:     cfg.BGP.RouterID,
						RemoteAS:     cfg.BGP.RemoteAS,
						Neighbor:     cfg.BGP.Neighbor,
						Networks:     cfg.BGP.Networks,
						Redistribute: cfg.BGP.Redistribute,
					}
				}
			}

			if len(pbData.IPConfigs) == 0 {
				fmt.Printf("⚠️  Skipping router %s: no usable IP configuration\n", router.Name)
				continue
			}

			tempFile, err := ioutil.TempFile("", fmt.Sprintf("configure_%s-*.yml", router.Name))
			if err != nil {
				return fmt.Errorf("failed to create temp file: %v", err)
			}
			defer os.Remove(tempFile.Name())

			if err := tmpl.Execute(tempFile, pbData); err != nil {
				return fmt.Errorf("failed to render playbook: %v", err)
			}
			tempFile.Close()

			args := []string{tempFile.Name(), "-i", inventoryFile}
			if verbose {
				args = append(args, "-vvv")
			}
			ansibleCmd := exec.Command("ansible-playbook", args...)
			ansibleCmd.Env = os.Environ()
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
	gns3ConfigureCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose logging")
	gns3ConfigureCmd.Flags().StringVarP(&configFile, "config", "c", "", "Deployment YAML file")
	gns3ConfigureCmd.Flags().StringVar(&inventoryFile, "inventory", "i", "Ansible inventory file")
}

func maskToPrefix(mask string) string {
	var count int
	for _, octet := range strings.Split(mask, ".") {
		num, _ := strconv.Atoi(octet)
		for num > 0 {
			count += num & 1
			num = num >> 1
		}
	}
	return fmt.Sprintf("%d", count)
}

func cidrSubnetAddress(cidr string) string {
	parts := strings.Split(cidr, "/")
	if len(parts) == 2 {
		return parts[0]
	}
	return ""
}

func cidrToMask(cidr string) string {
	parts := strings.Split(cidr, "/")
	if len(parts) != 2 {
		return ""
	}
	prefix, _ := strconv.Atoi(parts[1])
	return net.IP(net.CIDRMask(prefix, 32)).String()
}
