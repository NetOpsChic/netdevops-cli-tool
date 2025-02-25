package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

// DeviceInfo holds details for inventory generation
type DeviceInfo struct {
	Name      string
	IP        string
	User      string
	Password  string
	NetworkOS string
	SSHArgs   string
}

// gns3InventoryCmd represents the inventory generation command
var gns3InventoryCmd = &cobra.Command{
	Use:   "gns3-inventory",
	Short: "Generate an Ansible inventory from ZTP-assigned IPs",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("🔄 Fetching assigned IPs from ZTP Server...")

		// Get ZTP Server IP from env or ask user
		ztpIP := os.Getenv("ZTP_IP")
		if ztpIP == "" {
			fmt.Print("Enter ZTP Server IP: ")
			fmt.Scanln(&ztpIP)
		}

		// Fetch assigned IPs
		assignedIPs, err := getZTPIps(ztpIP)
		if err != nil {
			log.Fatalf("❌ Failed to fetch IPs from ZTP: %v", err)
		}

		if len(assignedIPs) == 0 {
			log.Fatal("⚠️ No assigned IPs found. Ensure routers have requested DHCP leases.")
		}

		// Interactive inventory setup
		var devices []DeviceInfo
		reader := bufio.NewReader(os.Stdin)

		fmt.Println("🛠️ Setting up Ansible Inventory...")
		for name, ip := range assignedIPs {
			fmt.Printf("\n🔹 Configuring %s (%s)\n", name, ip)

			// Ask for username
			fmt.Print("Enter Ansible username [default: admin]: ")
			user, _ := reader.ReadString('\n')
			user = strings.TrimSpace(user)
			if user == "" {
				user = "admin"
			}

			// Ask for password (hidden input)
			fmt.Print("Enter password: ")
			password, _ := reader.ReadString('\n')
			password = strings.TrimSpace(password)

			// Ask for Ansible network OS
			fmt.Print("Enter network OS (e.g., ios, iosxr, nxos): ")
			networkOS, _ := reader.ReadString('\n')
			networkOS = strings.TrimSpace(networkOS)

			// Ask if SSH key args are needed
			fmt.Print("Require SSH args for old Cisco routers? (yes/no) [default: no]: ")
			sshInput, _ := reader.ReadString('\n')
			sshArgs := ""
			if strings.TrimSpace(strings.ToLower(sshInput)) == "yes" {
				sshArgs = "-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null"
			}

			devices = append(devices, DeviceInfo{
				Name:      name,
				IP:        ip,
				User:      user,
				Password:  password,
				NetworkOS: networkOS,
				SSHArgs:   sshArgs,
			})
		}

		// Save inventory
		saveInventory(devices)
		fmt.Println("✅ Ansible inventory generated successfully!")
	},
}

// getZTPIps fetches assigned IPs from the ZTP server
func getZTPIps(ztpIP string) (map[string]string, error) {
	cmd := exec.Command("curl", "-s", fmt.Sprintf("http://%s/api/dhcp-leases", ztpIP))
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var leases []map[string]interface{}
	err = json.Unmarshal(output, &leases)
	if err != nil {
		return nil, err
	}

	assignedIPs := make(map[string]string)
	for _, lease := range leases {
		if ip, ok := lease["ip-address"].(string); ok {
			if hostname, exists := lease["hostname"].(string); exists {
				assignedIPs[hostname] = ip
			}
		}
	}

	return assignedIPs, nil
}

// saveInventory writes the inventory to INI and YAML formats
func saveInventory(devices []DeviceInfo) {
	// Save as inventory.ini
	iniFile, _ := os.Create("inventory.ini")
	defer iniFile.Close()
	for _, device := range devices {
		iniFile.WriteString(fmt.Sprintf("[%s]\n", device.Name))
		iniFile.WriteString(fmt.Sprintf("%s ansible_user=%s ansible_password=%s ansible_network_os=%s",
			device.IP, device.User, device.Password, device.NetworkOS))
		if device.SSHArgs != "" {
			iniFile.WriteString(fmt.Sprintf(" ansible_ssh_common_args='%s'", device.SSHArgs))
		}
		iniFile.WriteString("\n\n")
	}

	// Save as ansible-inventory.yaml
	yamlFile, _ := os.Create("ansible-inventory.yaml")
	defer yamlFile.Close()
	yamlFile.WriteString("all:\n  hosts:\n")
	for _, device := range devices {
		yamlFile.WriteString(fmt.Sprintf("    %s:\n", device.Name))
		yamlFile.WriteString(fmt.Sprintf("      ansible_host: %s\n", device.IP))
		yamlFile.WriteString(fmt.Sprintf("      ansible_user: %s\n", device.User))
		yamlFile.WriteString(fmt.Sprintf("      ansible_password: %s\n", device.Password))
		yamlFile.WriteString(fmt.Sprintf("      ansible_network_os: %s\n", device.NetworkOS))
		if device.SSHArgs != "" {
			yamlFile.WriteString(fmt.Sprintf("      ansible_ssh_common_args: \"%s\"\n", device.SSHArgs))
		}
		yamlFile.WriteString("\n")
	}
}

func init() {
	rootCmd.AddCommand(gns3InventoryCmd)
}
