// File: cmd/gns3-inventory.go
package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
)

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

var gns3InventoryCmd = &cobra.Command{
	Use:   "gns3-inventory",
	Short: "Generate an Ansible inventory from deployment YAML or ZTP",
	Run: func(cmd *cobra.Command, args []string) {
		if configFile != "" {
			data, err := os.ReadFile(configFile)
			if err != nil {
				log.Fatalf("❌ Error reading config: %v", err)
			}
			var dep Deployment
			if err := yaml.Unmarshal(data, &dep); err != nil {
				log.Fatalf("❌ YAML parse error: %v", err)
			}
			fmt.Printf("🔄 Generating inventory for project: %s\n", dep.Project)
			generateInventoryFromYAML(dep)
			return
		}

		fmt.Println("🔄 Fetching assigned IPs from manual input or ZTP...")

		if vendor == "" {
			if vEnv := os.Getenv("VENDOR"); vEnv != "" {
				vendor = vEnv
			} else {
				fmt.Print("Enter device vendor (cisco, juniper, arista): ")
				reader := bufio.NewReader(os.Stdin)
				vInput, _ := reader.ReadString('\n')
				vendor = strings.TrimSpace(vInput)
			}
		}
		ansibleNetworkOS := detectNetworkOS(vendor)

		var devices []ZTPDevice

		if !skipZTP {
			if ztpIP == "" {
				if env := os.Getenv("ZTP_IP"); env != "" {
					ztpIP = env
				} else {
					fmt.Print("Enter ZTP Server IP: ")
					ip, _ := bufio.NewReader(os.Stdin).ReadString('\n')
					ztpIP = strings.TrimSpace(ip)
				}
			}

			fmt.Printf("📡 Fetching inventory from http://%s/inventory\n", ztpIP)
			raw, err := fetchZTPInventoryMapWithRetry(ztpIP, 120)
			if err != nil {
				log.Fatalf("❌ No devices found from ZTP inventory: %v", err)
			}
			writeInventoryFromZTPMap(raw)
			return
		}

		if len(devices) == 0 && len(manualDevices) > 0 {
			for _, md := range manualDevices {
				parts := strings.SplitN(md, "=", 2)
				if len(parts) != 2 {
					log.Fatalf("Invalid device: %s", md)
				}
				devices = append(devices, ZTPDevice{
					Name: parts[0],
					IP:   parts[1],
				})
			}
			fmt.Printf("✅ Using %d manual devices.\n", len(devices))
		}

		if len(devices) == 0 {
			log.Fatal("❌ No devices found. Exiting.")
		}

		// Fallback if ZTP not used
		writeManualInventory(devices, ansibleNetworkOS)
	},
}

func detectNetworkOS(vendor string) string {
	switch strings.ToLower(vendor) {
	case "cisco":
		return "ios"
	case "juniper":
		return "junos"
	case "arista", "arishta":
		return "eos"
	default:
		return "linux"
	}
}

func fetchZTPInventoryMapWithRetry(ztpIP string, maxWait int) (map[string]interface{}, error) {
	url := fmt.Sprintf("http://%s:5000/inventory", ztpIP)
	fmt.Printf("Attempting ZTP fetch from %s\n", url)

	interval := 10
	waited := 0

	for waited < maxWait {
		resp, err := http.Get(url)
		if err == nil && resp.StatusCode == 200 {
			defer resp.Body.Close()
			var raw map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&raw); err == nil {
				if all, ok := raw["all"].(map[string]interface{}); ok {
					if hostsRaw, ok := all["hosts"].([]interface{}); ok && len(hostsRaw) > 0 {
						fmt.Printf("✅ Inventory ready with %d hosts\n", len(hostsRaw))
						return raw, nil
					}
				}
			}
		}
		fmt.Printf("⏳ Waiting for inventory to populate... (%d/%d seconds)\n", waited, maxWait)
		time.Sleep(time.Duration(interval) * time.Second)
		waited += interval
	}

	return nil, fmt.Errorf("inventory was still empty after %d seconds", maxWait)
}

func writeInventoryFromZTPMap(raw map[string]interface{}) {
	os.MkdirAll("ansible-inventory", 0755)

	var iniLines []string
	iniLines = append(iniLines, "[all]")

	var yamlBuilder strings.Builder
	yamlBuilder.WriteString("all:\n  hosts:\n")

	hosts := []string{}
	if all, ok := raw["all"].(map[string]interface{}); ok {
		if hostList, ok := all["hosts"].([]interface{}); ok {
			for _, h := range hostList {
				if name, ok := h.(string); ok {
					hosts = append(hosts, name)
				}
			}
		}
	}

	// Sort hostnames
	sort.Slice(hosts, func(i, j int) bool {
		return hosts[i] < hosts[j]
	})

	for _, name := range hosts {
		info, ok := raw[name].(map[string]interface{})
		if !ok {
			continue
		}
		host := fmt.Sprint(info["ansible_host"])
		user := fmt.Sprint(info["ansible_user"])
		pass := fmt.Sprint(info["ansible_password"])
		osType := fmt.Sprint(info["ansible_network_os"])

		// INI
		iniLines = append(iniLines, fmt.Sprintf(
			"%s ansible_host=%s ansible_connection=network_cli ansible_become=yes ansible_become_method=enable "+
				"ansible_become_password=ubuntu ansible_user=%s ansible_password=%s "+
				"ansible_ssh_common_args='-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null' "+
				"ansible_network_os=%s",
			name, host, user, pass, osType,
		))

		// YAML
		yamlBuilder.WriteString(fmt.Sprintf("    %s:\n", name))
		yamlBuilder.WriteString(fmt.Sprintf("      ansible_host: %s\n", host))
		yamlBuilder.WriteString("      ansible_connection: network_cli\n")
		yamlBuilder.WriteString("      ansible_become: yes\n")
		yamlBuilder.WriteString("      ansible_become_method: enable\n")
		yamlBuilder.WriteString("      ansible_become_password: ubuntu\n")
		yamlBuilder.WriteString(fmt.Sprintf("      ansible_user: %s\n", user))
		yamlBuilder.WriteString(fmt.Sprintf("      ansible_password: %s\n", pass))
		yamlBuilder.WriteString("      ansible_ssh_common_args: '-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null'\n")
		yamlBuilder.WriteString(fmt.Sprintf("      ansible_network_os: %s\n", osType))
	}

	yamlPath := "ansible-inventory/inventory.yaml"
	iniPath := "ansible-inventory/inventory.ini"

	_ = os.WriteFile(iniPath, []byte(strings.Join(iniLines, "\n")+"\n"), 0644)
	_ = os.WriteFile(yamlPath, []byte(yamlBuilder.String()), 0644)

	fmt.Println("✅ Inventory written to ansible-inventory/inventory.yaml and inventory.ini")
}

func writeManualInventory(devices []ZTPDevice, osType string) {
	os.MkdirAll("ansible-inventory", 0755)

	var iniLines []string
	iniLines = append(iniLines, "[all]")

	var yamlBuilder strings.Builder
	yamlBuilder.WriteString("all:\n  hosts:\n")

	for _, d := range devices {
		user := d.AnsibleUser
		if user == "" {
			user = "admin"
		}
		pass := d.AnsiblePassword
		if pass == "" {
			pass = "admin"
		}

		iniLines = append(iniLines, fmt.Sprintf(
			"%s ansible_host=%s ansible_connection=network_cli ansible_become=yes "+
				"ansible_become_method=enable ansible_become_password=ubuntu "+
				"ansible_user=%s ansible_password=%s "+
				"ansible_ssh_common_args='-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null' "+
				"ansible_network_os=%s",
			d.Name, d.IP, user, pass, osType,
		))

		yamlBuilder.WriteString(fmt.Sprintf("    %s:\n", d.Name))
		yamlBuilder.WriteString(fmt.Sprintf("      ansible_host: %s\n", d.IP))
		yamlBuilder.WriteString("      ansible_connection: network_cli\n")
		yamlBuilder.WriteString("      ansible_become: yes\n")
		yamlBuilder.WriteString("      ansible_become_method: enable\n")
		yamlBuilder.WriteString("      ansible_become_password: ubuntu\n")
		yamlBuilder.WriteString(fmt.Sprintf("      ansible_user: %s\n", user))
		yamlBuilder.WriteString(fmt.Sprintf("      ansible_password: %s\n", pass))
		yamlBuilder.WriteString("      ansible_ssh_common_args: '-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null'\n")
		yamlBuilder.WriteString(fmt.Sprintf("      ansible_network_os: %s\n", osType))
	}

	_ = os.WriteFile("ansible-inventory/inventory.ini", []byte(strings.Join(iniLines, "\n")+"\n"), 0644)
	_ = os.WriteFile(inventoryFile, []byte(yamlBuilder.String()), 0644)

	fmt.Println("✅ Inventory written to ansible-inventory/inventory.yaml and inventory.ini")
}

func generateInventoryFromYAML(dep Deployment) {
	ztpIP := dep.ZTPServer
	fmt.Printf("📡 Fetching inventory from http://%s:5000/inventory\n", ztpIP)
	raw, err := fetchZTPInventoryMapWithRetry(ztpIP, 500)
	if err != nil {
		log.Fatalf("❌ Could not get ZTP devices: %v", err)
	}
	writeInventoryFromZTPMap(raw)
}

func init() {
	gns3InventoryCmd.Flags().StringVarP(&projectID, "project-id", "", "", "GNS3 project ID (optional)")
	gns3InventoryCmd.Flags().StringVarP(&containerID, "container-id", "", "", "Container ID (optional)")
	gns3InventoryCmd.Flags().StringVarP(&ztpIP, "ztp", "", "", "ZTP IP or host")
	gns3InventoryCmd.Flags().BoolVarP(&skipZTP, "skip-ztp", "", false, "Skip querying ZTP server")
	gns3InventoryCmd.Flags().StringSliceVarP(&manualDevices, "devices", "d", []string{}, "Manual NAME=IP device entries")
	gns3InventoryCmd.Flags().StringVarP(&vendor, "vendor", "V", "", "Device vendor")
	gns3InventoryCmd.Flags().StringVarP(&configFile, "config", "c", "", "YAML config file for inventory")
	rootCmd.AddCommand(gns3InventoryCmd)
}
