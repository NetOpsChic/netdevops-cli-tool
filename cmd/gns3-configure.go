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

// Deployment represents the structure of the combined deployment/configuration YAML.
type Deployment struct {
	Project    string `yaml:"project"`
	StartNodes bool   `yaml:"start_nodes"`
	Routers    []struct {
		Name     string `yaml:"name"`
		Template string `yaml:"template"`
		Config   *struct {
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
				Stub interface{} `yaml:"stub"` // Changed from bool to interface{}
				NSSA interface{} `yaml:"nssa"` // Changed from bool to interface{}
			} `yaml:"ospfv3"`
		} `yaml:"config"`
	} `yaml:"routers"`
	// Other fields (switches, clouds, links) can be added as needed.
}

// gns3ConfigureCmd represents the gns3-configure command.
var gns3ConfigureCmd = &cobra.Command{
	Use:   "gns3-configure",
	Short: "Render and execute Ansible playbooks for device configuration based on a deployment YAML",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Ensure the deployment YAML file is provided using the --config flag.
		if configFile == "" {
			return fmt.Errorf("deployment YAML file must be provided using the --config flag")
		}

		// Read and unmarshal the deployment YAML file.
		data, err := ioutil.ReadFile(configFile)
		if err != nil {
			return fmt.Errorf("failed to read deployment file: %v", err)
		}

		var deployment Deployment
		if err := yaml.Unmarshal(data, &deployment); err != nil {
			return fmt.Errorf("failed to parse deployment YAML: %v", err)
		}

		// Parse the playbook template.
		tmpl, err := template.New("playbook").Funcs(sprig.TxtFuncMap()).Funcs(template.FuncMap{
			"maskToPrefix":      maskToPrefix,      // converts dotted mask to CIDR prefix
			"cidrSubnetAddress": cidrSubnetAddress, // extracts subnet address from CIDR
			"cidrToMask":        cidrToMask,        // converts CIDR prefix to dotted-decimal mask
		}).Parse(ConfigureAristaTemplate)
		if err != nil {
			return fmt.Errorf("failed to parse ansible playbook template: %v", err)
		}

		// Iterate over routers that have configuration details.
		for _, router := range deployment.Routers {
			if router.Config != nil && router.Config.Interface != "" && router.Config.IPAddress != "" {
				// Prepare the data to render the playbook.
				pbData := PlaybookData{
					RouterName: router.Name,
					IPConfigs: []IPConfig{
						{
							Interface: router.Config.Interface,
							IPAddress: router.Config.IPAddress,
							Mask:      "255.255.255.0", // Default value; adjust as needed.
							Secondary: true,
						},
					},
					StaticRoutes: []StaticRoute{},
					OSPFv3:       nil,
				}

				// Append static routes if available.
				if router.Config.StaticRoutes != nil && len(router.Config.StaticRoutes) > 0 {
					for _, sr := range router.Config.StaticRoutes {
						pbData.StaticRoutes = append(pbData.StaticRoutes, StaticRoute{
							DestNetwork: sr.DestNetwork,
							SubnetMask:  sr.SubnetMask,
							NextHop:     sr.NextHop,
							Interface:   sr.Interface,
						})
					}
				}
				// Append OSPFv3 configuration if available.
				if router.Config.OSPFv3 != nil {
					ospf := router.Config.OSPFv3
					var ospfIfaces []OSPFv3Interface
					for _, iface := range ospf.Interfaces {
						ospfIfaces = append(ospfIfaces, OSPFv3Interface{
							Name:    iface.Name,
							Cost:    iface.Cost,
							Passive: iface.Passive,
						})
					}
					// Process Stub field: if it's a bool and true, convert to an empty map; if it's a map, use it.
					var stubVal interface{}
					if ospf.Stub != nil {
						if v, ok := ospf.Stub.(bool); ok {
							if v {
								stubVal = map[string]interface{}{}
							} else {
								stubVal = nil
							}
						} else if m, ok := ospf.Stub.(map[interface{}]interface{}); ok {
							// Convert map[interface{}]interface{} to map[string]interface{}
							stubVal = convertMap(m)
						} else if m, ok := ospf.Stub.(map[string]interface{}); ok {
							stubVal = m
						}
					}
					// Process NSSA field similarly.
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
						RouterID:   ospf.RouterID,
						Area:       ospf.Area,
						Networks:   ospf.Networks,
						Interfaces: ospfIfaces,
						Stub:       stubVal,
						NSSA:       nssaVal,
					}
				}

				// Create a temporary file to store the rendered playbook.
				tempFile, err := ioutil.TempFile("", fmt.Sprintf("configure_%s-*.yml", router.Name))
				if err != nil {
					return fmt.Errorf("failed to create temp file for router %s: %v", router.Name, err)
				}
				// Ensure the temporary file is removed after execution.
				defer os.Remove(tempFile.Name())

				// Render the template into the temporary file.
				if err := tmpl.Execute(tempFile, pbData); err != nil {
					return fmt.Errorf("failed to render playbook for router %s: %v", router.Name, err)
				}
				tempFile.Close()

				// Construct the ansible-playbook command.
				ansibleCmd := exec.Command("ansible-playbook", tempFile.Name(), "-i", inventoryFile)
				ansibleCmd.Env = os.Environ()

				fmt.Printf("Executing ansible playbook for router %s with inventory '%s'...\n", router.Name, inventoryFile)
				output, err := ansibleCmd.CombinedOutput()
				fmt.Println(string(output))
				if err != nil {
					return fmt.Errorf("ansible-playbook execution failed for router %s: %v", router.Name, err)
				}
				fmt.Printf("Configuration applied successfully for router %s.\n", router.Name)
			}
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(gns3ConfigureCmd)

	// The --config flag specifies the deployment YAML file.
	gns3ConfigureCmd.Flags().StringVarP(&configFile, "config", "c", "", "Deployment YAML file containing topology and device configuration")
	// The --inventory flag specifies the inventory file.
	gns3ConfigureCmd.Flags().StringVar(&inventoryFile, "inventory", "./ansible-inventory.yaml", "Ansible inventory file path (default is ./ansible-inventory.yaml)")
}

// Helper: convertMap converts a map[interface{}]interface{} to map[string]interface{}.
func convertMap(in map[interface{}]interface{}) map[string]interface{} {
	out := make(map[string]interface{})
	for key, value := range in {
		out[fmt.Sprintf("%v", key)] = value
	}
	return out
}

// maskToPrefix converts a dotted-decimal subnet mask to a CIDR prefix length.
// For example, "255.255.255.0" becomes "24".
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

// cidrSubnetAddress extracts the IP portion of a CIDR (e.g., "192.168.1.0" from "192.168.1.0/24").
func cidrSubnetAddress(cidr string) string {
	parts := strings.Split(cidr, "/")
	if len(parts) != 2 {
		return ""
	}
	return parts[0]
}

// cidrToMask converts a CIDR prefix (from a CIDR string, e.g., "192.168.1.0/24")
// into a dotted-decimal subnet mask (e.g., "255.255.255.0").
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
