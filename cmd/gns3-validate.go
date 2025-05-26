// File: cmd/gns3-validate.go
package cmd

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"text/template"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
)

// PingTestVars is the context we pass into the ping‐through playbook.
type PingTestVars struct {
	Source   string
	TargetIP string
}

var gns3ValidateCmd = &cobra.Command{
	Use:   "gns3-validate",
	Short: "Test connectivity across your GNS3 routers and upload the inventory",
	RunE: func(cmd *cobra.Command, args []string) error {
		if configFile == "" {
			return fmt.Errorf("deployment YAML must be provided with --config")
		}
		data, err := os.ReadFile(configFile)
		if err != nil {
			return fmt.Errorf("reading %s: %w", configFile, err)
		}

		// Mirror your schema: routers under network-device, servers under templates
		var topo struct {
			NetworkDevice struct {
				Routers []NetworkDevice `yaml:"routers"`
			} `yaml:"network-device"`
			Templates struct {
				Servers []struct {
					Name         string `yaml:"name"`
					ZTPServer    string `yaml:"ztp_server,omitempty"`
					ObserveTower string `yaml:"observe-tower,omitempty"`
					Start        bool   `yaml:"start"`
				} `yaml:"servers"`
			} `yaml:"templates"`
		}
		if err := yaml.Unmarshal(data, &topo); err != nil {
			return fmt.Errorf("parsing %s: %w", configFile, err)
		}

		routers := topo.NetworkDevice.Routers
		if len(routers) < 2 {
			return fmt.Errorf("need at least two routers to validate, got %d", len(routers))
		}

		// pick the first & last router
		source := routers[0].Name
		last := routers[len(routers)-1]

		// extract its first IP
		var targetIP string
		if cfgs, ok := last.Config.([]interface{}); ok {
			for _, raw := range cfgs {
				if m, ok := raw.(map[interface{}]interface{}); ok {
					if ipRaw, found := m["ip_address"]; found {
						if s, ok := ipRaw.(string); ok {
							targetIP = strings.Split(s, "/")[0]
							break
						}
					}
				}
			}
		}
		if targetIP == "" {
			return fmt.Errorf("couldn't find an ip_address on last router %s", last.Name)
		}

		// read ZTP and Observer Tower IPs
		var ztpIP, obsIP string
		for _, srv := range topo.Templates.Servers {
			switch srv.Name {
			case "ztp-server":
				ztpIP = srv.ZTPServer
			case "observe-tower":
				obsIP = srv.ObserveTower
			}
		}
		if ztpIP == "" {
			return fmt.Errorf("ztp-server entry missing from templates.servers")
		}
		if obsIP == "" {
			return fmt.Errorf("observe-tower entry missing from templates.servers")
		}
		fmt.Printf("ℹ️  ZTP: %s, Observer Tower: %s, ping %s → %s\n", ztpIP, obsIP, source, targetIP)

		// Enable eAPI
		fmt.Println("🛰️  Enabling eAPI on all routers…")
		if err := renderAndRunAnsible(EnableEapiTemplate, nil, "tests/eapi.yml"); err != nil {
			return fmt.Errorf("eAPI playbook failed: %w", err)
		}

		// Ping through
		fmt.Println("🚀 Running ping test…")
		pv := PingTestVars{Source: source, TargetIP: targetIP}
		if err := renderAndRunAnsible(ValidatePingTemplate, pv, "tests/ping.yml"); err != nil {
			return fmt.Errorf("ping test failed: %w", err)
		}

		// Upload inventory to Observer Tower
		fmt.Printf("📤 Uploading inventory to Observer Tower at %s…\n", obsIP)
		if err := uploadInventory(inventoryFile, obsIP); err != nil {
			return fmt.Errorf("inventory upload failed: %w", err)
		}
		fmt.Println("✅ Validation complete, inventory uploaded.")
		return nil
	},
}

// renderAndRunAnsible renders the given template and executes the resulting playbook.
func renderAndRunAnsible(tmplContent string, data interface{}, outPath string) error {
	tmpl, err := template.New("").Parse(tmplContent)
	if err != nil {
		return fmt.Errorf("template parse error: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("template execute error: %w", err)
	}

	if err := os.MkdirAll("tests", 0755); err != nil {
		return err
	}
	if err := os.WriteFile(outPath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("writing %s: %w", outPath, err)
	}

	fmt.Printf("📡 ansible-playbook -i %s %s\n", inventoryFile, outPath)
	cmd := exec.Command("ansible-playbook", "-i", inventoryFile, outPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// uploadInventory POSTS the inventory file to the Observer Tower API at obsIP.
func uploadInventory(path, obsIP string) error {
	client := http.Client{Timeout: 2 * time.Second}

	// Wait for Observer API to become ready
	fmt.Print("🔄 Waiting for Observer API")
	for i := 0; i < 10; i++ {
		if resp, err := client.Get(fmt.Sprintf("http://%s:5000/", obsIP)); err == nil && resp.StatusCode == 200 {
			fmt.Println(" ✅")
			break
		}
		fmt.Print(".")
		time.Sleep(2 * time.Second)
	}

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	p, _ := w.CreateFormFile("file", "inventory.yaml")
	if _, err := io.Copy(p, f); err != nil {
		return err
	}
	w.Close()

	req, _ := http.NewRequest("POST", fmt.Sprintf("http://%s:5000/upload", obsIP), &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upload failed: %s", body)
	}
	return nil
}

func init() {
	rootCmd.AddCommand(gns3ValidateCmd)
	gns3ValidateCmd.Flags().StringVarP(&configFile, "config", "c", "", "Deployment YAML file")
	gns3ValidateCmd.Flags().StringVar(&inventoryFile, "inventory", "ansible-inventory/inventory.yaml", "Inventory file")
}
