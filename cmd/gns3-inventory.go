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
	Name string `json:"name"`
	IP   string `json:"ip"`
}

// CLI flags
var projectID string
var containerID string
var ztpIP string
var skipZTP bool
var manualDevices []string

// gns3InventoryCmd is the Cobra command for generating Ansible inventory
var gns3InventoryCmd = &cobra.Command{
	Use:   "gns3-inventory",
	Short: "Generate an Ansible inventory from the ZTP server or manual input",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("🔄 Fetching assigned IPs from ZTP Server or manual input...")

		var devices []ZTPDevice

		// 1) If skipZTP is false, attempt to fetch from ZTP (with a 5-min wait)
		if !skipZTP {
			if ztpIP == "" {
				// check environment or prompt
				ztpIP = os.Getenv("ZTP_IP")
				if ztpIP == "" {
					fmt.Print("Enter ZTP Server IP (or IP:port): ")
					reader := bufio.NewReader(os.Stdin)
					ipInput, _ := reader.ReadString('\n')
					ztpIP = strings.TrimSpace(ipInput)
				}
			}

			// Attempt up to 5 minutes (300 seconds) in a loop
			maxWait := 300
			interval := 10
			totalWait := 0
			fmt.Printf("⏳ Waiting up to 5 minutes for ZTP server at http://%s/inventory to become ready...\n", ztpIP)

			for totalWait < maxWait {
				ztpDevices, err := fetchZTPDevices(ztpIP)
				if err == nil && len(ztpDevices) > 0 {
					devices = ztpDevices
					fmt.Printf("✅ Fetched %d devices from ZTP at %s\n", len(devices), ztpIP)
					break
				}
				// Otherwise, keep waiting
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

		// 2) If we have no devices from ZTP, parse manual input
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

		// 3) If devices is still empty, error out
		if len(devices) == 0 {
			log.Fatal("❌ No devices found or provided. Exiting.")
		}

		// 4) Build final inventory lines
		var lines []string
		lines = append(lines, "[ztp-routers]") // or any group name
		for _, d := range devices {
			// Example line: "R1 ansible_host=10.0.0.1 ansible_user=admin ansible_password=admin"
			line := fmt.Sprintf("%s ansible_host=%s ansible_user=admin ansible_password=admin", d.Name, d.IP)
			lines = append(lines, line)
		}

		// 5) Write inventory files
		if err := writeInventories(lines); err != nil {
			log.Fatalf("❌ Error writing inventories: %v", err)
		}
	},
}

// fetchZTPDevices queries `http://{ztpIP}/inventory` and parses JSON of the form:
// { "unknown": ["192.168.100.100 ansible_user=admin ansible_password=admin", ...] }
func fetchZTPDevices(ztpIP string) ([]ZTPDevice, error) {
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

	// We'll parse a JSON object that looks like: {"unknown": [...]}
	// Each line in "unknown" e.g. "192.168.100.100 ansible_user=admin ansible_password=admin"
	var ztpResp struct {
		Unknown []string `json:"unknown"`
	}
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&ztpResp); err != nil {
		return nil, fmt.Errorf("JSON decode error: %v", err)
	}

	// Convert each line in Unknown[] to a ZTPDevice
	var devices []ZTPDevice
	for _, line := range ztpResp.Unknown {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		// First field is IP
		ip := fields[0]
		// We'll just call the device "ZTP" or "unknown" for the name.
		dev := ZTPDevice{
			Name: "unknown",
			IP:   ip,
		}
		devices = append(devices, dev)
	}

	return devices, nil
}

// writeInventories writes inventory.ini and ansible-inventory.yaml
func writeInventories(lines []string) error {
	// 1) INI
	iniPath := "inventory.ini"
	if err := os.WriteFile(iniPath, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		return err
	}
	fmt.Printf("✅ Wrote %s\n", iniPath)

	// 2) YAML
	yamlPath := "ansible-inventory.yaml"
	// Quick YAML: single group "ztp-routers"
	var sb strings.Builder
	sb.WriteString("all:\n")
	sb.WriteString("  hosts:\n")

	// parse lines => e.g. "R1 ansible_host=1.1.1.1 ..."
	for _, l := range lines[1:] { // skip the group name line
		fields := strings.Fields(l)
		if len(fields) < 2 {
			continue
		}
		hostName := fields[0]
		sb.WriteString(fmt.Sprintf("    %s:\n", hostName))
		// parse "ansible_host=1.1.1.1"
		for _, f := range fields[1:] {
			pair := strings.SplitN(f, "=", 2)
			if len(pair) == 2 {
				sb.WriteString(fmt.Sprintf("      %s: %s\n", pair[0], pair[1]))
			}
		}
	}
	if err := os.WriteFile(yamlPath, []byte(sb.String()), 0644); err != nil {
		return err
	}
	fmt.Printf("✅ Wrote %s\n", yamlPath)

	return nil
}

func init() {
	gns3InventoryCmd.Flags().StringVar(&projectID, "project-id", "", "GNS3 project ID (optional if not needed)")
	gns3InventoryCmd.Flags().StringVar(&containerID, "container-id", "", "Docker container ID in GNS3 (optional if not needed)")
	gns3InventoryCmd.Flags().StringVar(&ztpIP, "ztp", "", "ZTP server IP/host (overrides environment ZTP_IP)")
	gns3InventoryCmd.Flags().BoolVar(&skipZTP, "skip-ztp", false, "Skip querying ZTP server if set")
	gns3InventoryCmd.Flags().StringSliceVar(&manualDevices, "devices", []string{}, "Manual device entries in NAME=IP format")

	rootCmd.AddCommand(gns3InventoryCmd)
}
