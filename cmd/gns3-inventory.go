// File: cmd/gns3-inventory.go
package cmd

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
)

// ZTPDevice represents the device data from the ZTP server (fallback).
type ZTPDevice struct {
	Name            string `json:"name"`
	IP              string `json:"ip"`
	AnsibleUser     string // e.g. "admin"
	AnsiblePassword string // e.g. "secret"
}

// CLI flags for legacy mode.
var (
	projectID     string
	containerID   string
	ztpIP         string   // IP or IP:port of the ZTP server
	skipZTP       bool     // If true, skip querying ZTP
	manualDevices []string // Manual device entries in NAME=IP format
	vendor        string   // e.g. "cisco", "juniper", or "arista"
	configFile    string   // YAML configuration file for inventory (Deployment)
)

var gns3InventoryCmd = &cobra.Command{
	Use:   "gns3-inventory",
	Short: "Generate an Ansible inventory from the deployment YAML (-c flag) or ZTP/manual input",
	Run: func(cmd *cobra.Command, args []string) {

		// If a configuration file is provided via -c, use it exclusively.
		if configFile != "" {
			data, err := os.ReadFile(configFile)
			if err != nil {
				log.Fatalf("❌ Error reading config file: %v", err)
			}
			var dep Deployment
			if err := yaml.Unmarshal(data, &dep); err != nil {
				log.Fatalf("❌ Error parsing config file: %v", err)
			}
			fmt.Printf("🔄 Generating inventory for project: %s\n", dep.Project)
			// Use the ZTP server IP from the YAML configuration.
			generateInventoryFromRoutersYAML(dep.Routers, dep.ZTPServer)
			return
		}

		// Legacy mode: use manual flags or ZTP querying.
		fmt.Println("🔄 Fetching assigned IPs from deployment YAML or manual input...")

		// 1) Ask for vendor if not set via flag or environment.
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
		vendorLower := strings.ToLower(vendor)

		// Decide ansible_network_os based on vendor.
		ansibleNetworkOS := "linux"
		if vendorLower == "cisco" {
			ansibleNetworkOS = "ios"
		} else if vendorLower == "juniper" {
			ansibleNetworkOS = "junos"
		} else if vendorLower == "arista" || vendorLower == "arishta" {
			ansibleNetworkOS = "eos"
		}

		var devices []ZTPDevice

		// 2) If skipZTP is false, attempt to fetch from ZTP.
		if !skipZTP {
			if ztpIP == "" {
				if ztpEnv := os.Getenv("ZTP_IP"); ztpEnv != "" {
					ztpIP = ztpEnv
				} else {
					fmt.Print("Enter ZTP Server IP (or IP:port): ")
					reader := bufio.NewReader(os.Stdin)
					ipInput, _ := reader.ReadString('\n')
					ztpIP = strings.TrimSpace(ipInput)
				}
			}

			maxWait := 600
			interval := 20
			totalWait := 0
			fmt.Printf("⏳ Waiting up to 5 minutes for ZTP server at http://%s/inventory to become ready...\n", ztpIP)

			for totalWait < maxWait {
				ztpDevices, err := fetchZTPDevices(ztpIP, vendorLower)
				if err == nil && len(ztpDevices) > 0 {
					devices = ztpDevices
					fmt.Printf("✅ Fetched %d devices from ZTP at %s\n", len(devices), ztpIP)
					break
				}
				fmt.Printf("ZTP not ready or no devices. Retrying in %d seconds... (Waited %d/%d)\n", interval, totalWait, maxWait)
				time.Sleep(time.Duration(interval) * time.Second)
				totalWait += interval
			}

			if len(devices) == 0 {
				fmt.Printf("⚠️ ZTP fetch failed or no devices returned after %d seconds\n", maxWait)
				fmt.Println("Falling back to manual input...")
			}
		}

		// 3) If we have no devices from ZTP, parse manual input.
		if len(devices) == 0 && len(manualDevices) > 0 {
			for _, md := range manualDevices {
				parts := strings.SplitN(md, "=", 2)
				if len(parts) != 2 {
					log.Fatalf("Invalid manual device input %q (expected NAME=IP)", md)
				}
				devices = append(devices, ZTPDevice{
					Name: strings.TrimSpace(parts[0]),
					IP:   strings.TrimSpace(parts[1]),
				})
			}
			fmt.Printf("✅ Using %d manual devices.\n", len(devices))
		}

		// 4) If devices is still empty, error out.
		if len(devices) == 0 {
			log.Fatal("❌ No devices found or provided. Exiting.")
		}

		// 5) Build inventory.ini lines.
		var iniLines []string
		iniLines = append(iniLines, "[all]")
		for _, d := range devices {
			ansibleUser := d.AnsibleUser
			if ansibleUser == "" {
				ansibleUser = "admin"
			}
			ansiblePassword := d.AnsiblePassword
			if ansiblePassword == "" {
				ansiblePassword = "admin"
			}
			line := fmt.Sprintf("%s ansible_host=%s ansible_connection=network_cli ansible_become=yes ansible_become_method=enable "+
				"ansible_become_password=ubuntu ansible_user=%s ansible_password=%s "+
				"ansible_ssh_common_args='-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null' ansible_network_os=%s",
				d.Name, d.IP, ansibleUser, ansiblePassword, ansibleNetworkOS)
			iniLines = append(iniLines, line)
		}

		// Write inventory.ini.
		if err := writeINIInventory(iniLines); err != nil {
			log.Fatalf("❌ Error writing inventory.ini: %v", err)
		}

		// Generate YAML inventory directly from devices.
		if err := writeYAMLInventory(devices, ansibleNetworkOS); err != nil {
			log.Fatalf("❌ Error writing ansible-inventory.yaml: %v", err)
		}
	},
}

// generateInventoryFromRoutersYAML builds the inventory from routers defined in the Deployment YAML.
func generateInventoryFromRoutersYAML(routers []Router, ztpServer string) {
	// Wait for the inventory to be available.
	maxWait := 600 // seconds
	interval := 20 // seconds
	totalWait := 0
	var deviceList []ZTPDevice

	fmt.Printf("⏳ Waiting up to %d seconds for ZTP inventory from %s...\n", maxWait, ztpServer)
	for totalWait < maxWait {
		devices, err := fetchZTPDevices(ztpServer, "")
		if err == nil && len(devices) > 0 {
			deviceList = devices
			break
		}
		fmt.Printf("ZTP inventory not ready. Retrying in %d seconds... (Waited %d/%d seconds)\n", interval, totalWait, maxWait)
		time.Sleep(time.Duration(interval) * time.Second)
		totalWait += interval
	}

	if len(deviceList) == 0 {
		log.Fatalf("❌ No devices found from ZTP inventory after waiting %d seconds", maxWait)
	}

	// Sort deviceList numerically by IP.
	sort.Slice(deviceList, func(i, j int) bool {
		ip1 := net.ParseIP(deviceList[i].IP).To4()
		ip2 := net.ParseIP(deviceList[j].IP).To4()
		if ip1 == nil || ip2 == nil {
			return deviceList[i].IP < deviceList[j].IP
		}
		return binary.BigEndian.Uint32(ip1) < binary.BigEndian.Uint32(ip2)
	})

	if len(deviceList) < len(routers) {
		log.Printf("Warning: fewer devices returned (%d) than routers defined in YAML (%d)", len(deviceList), len(routers))
	}

	var iniLines []string
	iniLines = append(iniLines, "[all]")
	var yamlLines strings.Builder
	yamlLines.WriteString("all:\n")
	yamlLines.WriteString("  hosts:\n")

	// For each router defined in YAML, assign a device IP sequentially.
	for i, r := range routers {
		vendorLower := strings.ToLower(r.Vendor)
		ansibleNetworkOS := "linux"
		switch vendorLower {
		case "cisco":
			ansibleNetworkOS = "ios"
		case "juniper":
			ansibleNetworkOS = "junos"
		case "arista", "arishta":
			ansibleNetworkOS = "eos"
		}

		var ipAddress string
		if i < len(deviceList) {
			ipAddress = deviceList[i].IP
		} else {
			log.Printf("Warning: not enough devices from ZTP inventory for router %s", r.Name)
			continue
		}

		// Build INI inventory line.
		line := fmt.Sprintf("%s ansible_host=%s ansible_connection=network_cli ansible_become=yes ansible_become_method=enable "+
			"ansible_become_password=ubuntu ansible_user=admin ansible_password=admin "+
			"ansible_ssh_common_args='-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null' ansible_network_os=%s",
			r.Name, ipAddress, ansibleNetworkOS)
		iniLines = append(iniLines, line)

		// Build YAML inventory host entry.
		yamlLines.WriteString(fmt.Sprintf("    %s:\n", r.Name))
		yamlLines.WriteString(fmt.Sprintf("      ansible_host: %s\n", ipAddress))
		yamlLines.WriteString("      ansible_connection: network_cli\n")
		yamlLines.WriteString("      ansible_become: yes\n")
		yamlLines.WriteString("      ansible_become_method: enable\n")
		yamlLines.WriteString("      ansible_become_password: ubuntu\n")
		yamlLines.WriteString("      ansible_user: admin\n")
		yamlLines.WriteString("      ansible_password: admin\n")
		yamlLines.WriteString("      ansible_ssh_common_args: '-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null'\n")
		yamlLines.WriteString(fmt.Sprintf("      ansible_network_os: %s\n", ansibleNetworkOS))
	}

	if err := writeINIInventory(iniLines); err != nil {
		log.Fatalf("Error writing inventory.ini: %v", err)
	}
	yamlPath := inventoryFile
	if err := os.WriteFile(yamlPath, []byte(yamlLines.String()), 0644); err != nil {
		log.Fatalf("Error writing ansible-inventory.yaml: %v", err)
	}
	fmt.Printf("✅ Wrote %s\n", yamlPath)
}

func writeINIInventory(lines []string) error {
	iniPath := "inventory.ini"
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(iniPath, []byte(content), 0644); err != nil {
		return err
	}
	fmt.Printf("✅ Wrote %s\n", iniPath)
	return nil
}

func writeYAMLInventory(devices []ZTPDevice, ansibleNetworkOS string) error {
	var sb strings.Builder
	sb.WriteString("all:\n")
	sb.WriteString("  hosts:\n")
	for _, d := range devices {
		ansibleUser := d.AnsibleUser
		if ansibleUser == "" {
			ansibleUser = "admin"
		}
		ansiblePassword := d.AnsiblePassword
		if ansiblePassword == "" {
			ansiblePassword = "admin"
		}
		sb.WriteString(fmt.Sprintf("    %s:\n", d.Name))
		sb.WriteString(fmt.Sprintf("      ansible_host: %s\n", d.IP))
		sb.WriteString("      ansible_connection: network_cli\n")
		sb.WriteString("      ansible_become: yes\n")
		sb.WriteString("      ansible_become_method: enable\n")
		sb.WriteString("      ansible_become_password: ubuntu\n")
		sb.WriteString(fmt.Sprintf("      ansible_user: %s\n", ansibleUser))
		sb.WriteString(fmt.Sprintf("      ansible_password: %s\n", ansiblePassword))
		sb.WriteString("      ansible_ssh_common_args: '-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null'\n")
		sb.WriteString(fmt.Sprintf("      ansible_network_os: %s\n", ansibleNetworkOS))
	}
	yamlPath := inventoryFile
	if err := os.WriteFile(yamlPath, []byte(sb.String()), 0644); err != nil {
		return err
	}
	fmt.Printf("✅ Wrote %s\n", yamlPath)
	return nil
}

func fetchZTPDevices(ztpIP, vendorFilter string) ([]ZTPDevice, error) {
	url := fmt.Sprintf("http://%s/inventory", ztpIP)
	fmt.Printf("Attempting ZTP fetch from %s\n", url)

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("HTTP GET failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := ioutil.ReadAll(resp.Body)
		return nil, fmt.Errorf("Non-200 status %d: %s", resp.StatusCode, string(body))
	}

	var response map[string][]string
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&response); err != nil {
		return nil, fmt.Errorf("JSON decode error: %v", err)
	}

	var devices []ZTPDevice
	for key, lines := range response {
		// If a vendor filter is provided, only process matching keys (case-insensitive).
		if vendorFilter != "" && strings.ToLower(key) != vendorFilter {
			continue
		}
		for _, line := range lines {
			fields := strings.Fields(line)
			if len(fields) == 0 {
				continue
			}
			// Initialize device using the key as default name.
			dev := ZTPDevice{
				Name: key,
			}
			// Scan all tokens for ansible_host, ansible_user, ansible_password.
			for _, token := range fields {
				if strings.HasPrefix(token, "ansible_host=") {
					dev.IP = strings.TrimPrefix(token, "ansible_host=")
				} else if strings.HasPrefix(token, "ansible_user=") {
					dev.AnsibleUser = strings.TrimPrefix(token, "ansible_user=")
				} else if strings.HasPrefix(token, "ansible_password=") {
					dev.AnsiblePassword = strings.TrimPrefix(token, "ansible_password=")
				}
			}
			// Fallback: if ansible_host was not found, use the first token.
			if dev.IP == "" {
				dev.IP = fields[0]
			}
			devices = append(devices, dev)
		}
	}
	return devices, nil
}

func init() {
	gns3InventoryCmd.Flags().StringVarP(&projectID, "project-id", "", "", "GNS3 project ID (optional if not needed)")
	gns3InventoryCmd.Flags().StringVarP(&containerID, "container-id", "", "", "Docker container ID in GNS3 (optional if not needed)")
	gns3InventoryCmd.Flags().StringVarP(&ztpIP, "ztp", "", "", "ZTP server IP/host (overrides environment ZTP_IP)")
	gns3InventoryCmd.Flags().BoolVarP(&skipZTP, "skip-ztp", "", false, "Skip querying ZTP server if set")
	gns3InventoryCmd.Flags().StringSliceVarP(&manualDevices, "devices", "d", []string{}, "Manual device entries in NAME=IP format")
	gns3InventoryCmd.Flags().StringVarP(&vendor, "vendor", "V", "", "Device vendor (e.g. 'cisco', 'juniper', 'arista')")
	gns3InventoryCmd.Flags().StringVarP(&configFile, "config", "c", "", "YAML configuration file for inventory")
	rootCmd.AddCommand(gns3InventoryCmd)
}
