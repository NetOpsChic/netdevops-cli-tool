package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// ZTPDevice represents the device data from the ZTP server
type ZTPDevice struct {
	Name            string `json:"name"`
	IP              string `json:"ip"`
	AnsibleUser     string // extracted from API response, e.g. "admin"
	AnsiblePassword string // extracted from API response, e.g. "secret"
}

// CLI flags
var (
	projectID     string
	containerID   string
	ztpIP         string   // IP or IP:port of the ZTP server
	skipZTP       bool     // If you want to skip ZTP fetch
	manualDevices []string // Manual device entries in NAME=IP format
	vendor        string   // e.g. "cisco", "juniper", or "arista"
)

var gns3InventoryCmd = &cobra.Command{
	Use:   "gns3-inventory",
	Short: "Generate an Ansible inventory from the ZTP server or manual input",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("🔄 Fetching assigned IPs from ZTP Server or manual input...")

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
			ansibleNetworkOS = "cisco.ios.ios"
		} else if vendorLower == "juniper" {
			ansibleNetworkOS = "juniper.junos.junos"
		} else if vendorLower == "arista" || vendorLower == "arishta" {
			ansibleNetworkOS = "eos"
		}

		var devices []ZTPDevice

		// 2) If skipZTP is false, attempt to fetch from ZTP with a maximum wait time.
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
				fmt.Printf("ZTP not ready or no devices. Retrying in %d seconds... (Waited %d/%d)\n",
					interval, totalWait, maxWait)
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
			// Build each host line; the ssh common args are added in one piece.
			line := fmt.Sprintf("%s ansible_host=%s ansible_connection=network_cli ansible_become=yes ansible_become_method=enable ansible_become_password=ubuntu ansible_user=%s ansible_password=%s ansible_ssh_common_args='-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null' ansible_network_os=%s",
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

// fetchZTPDevices queries http://{ztpIP}/inventory and parses JSON of the form:
// { "VendorName": [ "192.168.x.x ansible_user=admin ansible_password=admin", ... ] }
// If vendorFilter is non-empty, only devices matching that vendor (case-insensitive) are returned.
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

	// Decode JSON into a map where keys are vendor names and values are arrays of device strings.
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
			// First token is assumed to be the IP address.
			ip := fields[0]
			dev := ZTPDevice{
				// Use the vendor key from the JSON (preserving its original case).
				Name: key,
				IP:   ip,
			}
			// Parse remaining tokens to look for ansible_user and ansible_password.
			for _, token := range fields[1:] {
				if strings.HasPrefix(token, "ansible_user=") {
					dev.AnsibleUser = strings.TrimPrefix(token, "ansible_user=")
				} else if strings.HasPrefix(token, "ansible_password=") {
					dev.AnsiblePassword = strings.TrimPrefix(token, "ansible_password=")
				}
			}
			devices = append(devices, dev)
		}
	}
	return devices, nil
}

// writeINIInventory writes the inventory.ini file.
func writeINIInventory(lines []string) error {
	iniPath := "inventory.ini"
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(iniPath, []byte(content), 0644); err != nil {
		return err
	}
	fmt.Printf("✅ Wrote %s\n", iniPath)
	return nil
}

// writeYAMLInventory generates a YAML inventory directly from the devices slice.
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
		// Use the device name as the hostname (ensure uniqueness as needed).
		sb.WriteString(fmt.Sprintf("    %s:\n", d.Name))
		sb.WriteString(fmt.Sprintf("      ansible_host: %s\n", d.IP))
		sb.WriteString("      ansible_connection: network_cli\n")
		sb.WriteString("      ansible_become: yes\n")
		sb.WriteString("      ansible_become_method: enable\n")
		sb.WriteString("      ansible_become_password: ubuntu\n")
		sb.WriteString(fmt.Sprintf("      ansible_user: %s\n", ansibleUser))
		sb.WriteString(fmt.Sprintf("      ansible_password: %s\n", ansiblePassword))
		// Write the ssh common args on a single line using single quotes.
		sb.WriteString("      ansible_ssh_common_args: '-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null'\n")
		sb.WriteString(fmt.Sprintf("      ansible_network_os: %s\n", ansibleNetworkOS))
	}
	yamlPath := "ansible-inventory.yaml"
	if err := os.WriteFile(yamlPath, []byte(sb.String()), 0644); err != nil {
		return err
	}
	fmt.Printf("✅ Wrote %s\n", yamlPath)
	return nil
}

func init() {
	gns3InventoryCmd.Flags().StringVarP(&projectID, "project-id", "", "", "GNS3 project ID (optional if not needed)")
	gns3InventoryCmd.Flags().StringVarP(&containerID, "container-id", "", "", "Docker container ID in GNS3 (optional if not needed)")
	gns3InventoryCmd.Flags().StringVarP(&ztpIP, "ztp", "", "", "ZTP server IP/host (overrides environment ZTP_IP)")
	gns3InventoryCmd.Flags().BoolVarP(&skipZTP, "skip-ztp", "", false, "Skip querying ZTP server if set")
	gns3InventoryCmd.Flags().StringSliceVarP(&manualDevices, "devices", "d", []string{}, "Manual device entries in NAME=IP format")
	gns3InventoryCmd.Flags().StringVarP(&vendor, "vendor", "v", "", "Device vendor (e.g. 'cisco', 'juniper', 'arista')")
	rootCmd.AddCommand(gns3InventoryCmd)
}
