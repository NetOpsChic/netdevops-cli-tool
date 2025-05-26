package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var gns3DeployCmd = &cobra.Command{
	Use:   "gns3-deploy",
	Short: "Deploy GNS3 topology from a YAML file",
	RunE:  runGNS3Deploy,
}

func init() {
	gns3DeployCmd.Flags().StringVarP(&configFile, "config", "c", "topology.yaml", "YAML topology file")
	gns3DeployCmd.Flags().BoolVarP(&detach, "detach", "d", false, "run in background (daemonize)")
	rootCmd.AddCommand(gns3DeployCmd)
}

func runGNS3Deploy(cmd *cobra.Command, args []string) error {
	// 1) Read & parse topology
	fmt.Println("📂 Reading YAML topology...")
	data, err := ioutil.ReadFile(configFile)
	if err != nil {
		return fmt.Errorf("error reading YAML file %q: %w", configFile, err)
	}
	var topo Topology
	if err := yaml.Unmarshal(data, &topo); err != nil {
		prettyYAMLErrors(err)
		return fmt.Errorf("invalid YAML: %w", err)
	}

	// 2) Validate
	if err := validateTopology(&topo); err != nil {
		fmt.Println("❌ Validation failed:")
		fmt.Println(err)
		return err
	}

	// 3) GNS3 server must be set
	if topo.Project.GNS3Server == "" {
		return fmt.Errorf("project.gns3_server must be set in your YAML")
	}
	gns3Server = topo.Project.GNS3Server

	// 4) Create project directories
	baseDir := filepath.Join("projects", topo.Project.Name)
	logDir := filepath.Join(baseDir, "logs")
	tfDir := filepath.Join(baseDir, "terraform")
	ansDir := filepath.Join(baseDir, "ansible")
	pbDir := filepath.Join(ansDir, "playbooks")

	fmt.Printf("📁 Creating project dirs under %s\n", baseDir)
	for _, d := range []string{tfDir, ansDir, pbDir, logDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("failed to create %s: %w", d, err)
		}
	}

	// Open log file (for subprocess logs)
	logFile := filepath.Join(logDir, topo.Project.Name+".log")
	logF, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("cannot open log file %s: %w", logFile, err)
	}
	defer logF.Close()

	// 5) Visualize
	fmt.Println("📡 Visualizing YAML topology...")
	visualizeTopology(topo)

	// --- Detach if requested ---
	if detach {
		fmt.Println("🔁 Detaching to background... logs will go to:")
		fmt.Printf("    %s\n", logFile)
		return forkAndDetach(topo.Project.Name, configFile)
	}

	// 6) Generate Terraform
	fmt.Println("⚙️ Generating Terraform configuration from YAML...")
	tfMain := filepath.Join(tfDir, "main.tf")
	if err := generateTerraformFile(tfMain, terraformTemplate, topo); err != nil {
		return fmt.Errorf("error generating Terraform file: %w", err)
	}

	// 7) Terraform init & apply
	fmt.Println("🚀 Initializing Terraform configuration...")
	if err := runCommandInDir("terraform", []string{"init"}, tfDir, logF); err != nil {
		fmt.Println("❌ Terraform init failed. See log for details.")
		return err
	}
	fmt.Println("🚀 Applying Terraform configuration...")
	if err := runCommandInDir("terraform", []string{"apply", "-auto-approve"}, tfDir, logF); err != nil {
		fmt.Println("❌ Terraform apply failed. See log for details.")
		return err
	}
	if topo.Project.StartNodes {
		fmt.Println("🔌 Starting all nodes...")
		if err := runCommandInDir("terraform", []string{"apply", "-auto-approve", "-compact-warnings"}, tfDir, logF); err != nil {
			fmt.Println("❌ Terraform apply (start nodes) failed. See log for details.")
			return err
		}
	}

	// 8) Fetch & format Terraform outputs
	fmt.Println("🚀 Fetching and formatting Terraform outputs...")
	outFile := filepath.Join(tfDir, "terraform.auto.tfvars.json")
	if err := formatAndSaveTerraformOutputs(tfDir, outFile); err != nil {
		fmt.Println("❌ Error processing Terraform outputs. See log for details.")
		return err
	}
	fmt.Println("✅ Terraform outputs saved to", outFile)

	// 9) Upload YAML to ZTP (if defined)
	for _, srv := range topo.Templates.Servers {
		if srv.ZTPServer != "" {
			endpoint := fmt.Sprintf("http://%s:5000/upload-yaml", srv.ZTPServer)
			fmt.Printf("🚀 Uploading topology YAML to %s\n", endpoint)
			if err := uploadTopologyUntilSuccess(configFile, endpoint); err != nil {
				fmt.Println("❌ Upload to ZTP failed. See log for details.")
				return err
			}
			fmt.Println("✅ YAML uploaded!")
			break
		}
	}

	// 10) Generate Ansible inventory
	fmt.Println("📦 Generating Ansible inventory...")
	if err := generateInventoryFromYAML(topo, ansDir); err != nil {
		fmt.Printf("Error: ansible inventory generation failed: %v\n", err)
		fmt.Printf("\n❌ ansible inventory generation failed: %v\n", err)
		return err
	}

	// 11) Lookup project ID
	projectID, err := lookupProjectID(gns3Server, topo.Project.Name)
	if err != nil {
		fmt.Printf("Error: could not find project %q: %v\n", topo.Project.Name, err)
		return err
	}
	fmt.Printf("🔎 Found project %q → %s\n", topo.Project.Name, projectID)

	// 12) Start reconcile daemon
	fmt.Println("🔁 Starting reconciliation daemon…")
	// Detach *after* here, if -d was passed
	// (Not applicable now, but if you want, move detach here!)
	startReconcileDaemon(configFile, projectID)

	return nil
}

// Detach after main prints, not at the start.
func forkAndDetach(projectName, configFile string) error {
	logDir := filepath.Join("projects", projectName, "logs")
	os.MkdirAll(logDir, 0755)
	logFile := filepath.Join(logDir, projectName+".log")
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("cannot open log file %s: %w", logFile, err)
	}
	devNull, _ := os.OpenFile(os.DevNull, os.O_RDWR, 0)

	// Remove -d/--detach from args
	orig := os.Args
	childArgs := []string{orig[0]}
	for _, a := range orig[1:] {
		if a == "-d" || a == "--detach" {
			continue
		}
		childArgs = append(childArgs, a)
	}
	attrs := &syscall.ProcAttr{
		Files: []uintptr{devNull.Fd(), f.Fd(), f.Fd()},
		Env:   os.Environ(),
	}
	if _, err := syscall.ForkExec(orig[0], childArgs, attrs); err != nil {
		return fmt.Errorf("fork failed: %w", err)
	}
	return nil
}

func lookupProjectID(serverURL, desiredName string) (string, error) {
	url := fmt.Sprintf("%s/v2/projects", strings.TrimRight(serverURL, "/"))
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := ioutil.ReadAll(resp.Body)
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}
	var list []struct {
		ID   string `json:"project_id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return "", err
	}
	for _, p := range list {
		if p.Name == desiredName {
			return p.ID, nil
		}
	}
	return "", fmt.Errorf("project %q not found", desiredName)
}

func formatAndSaveTerraformOutputs(dir, outputFile string) error {
	cmd := exec.Command("terraform", "output", "-json")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("error fetching Terraform outputs: %w", err)
	}
	var raw map[string]map[string]interface{}
	if err := json.Unmarshal(out, &raw); err != nil {
		return fmt.Errorf("error decoding outputs: %w", err)
	}
	simple := make(map[string]interface{})
	for k, v := range raw {
		if val, ok := v["value"]; ok {
			simple[k] = val
		}
	}
	b, err := json.MarshalIndent(simple, "", "  ")
	if err != nil {
		return fmt.Errorf("error marshalling outputs: %w", err)
	}
	if err := ioutil.WriteFile(outputFile, b, 0644); err != nil {
		return fmt.Errorf("error writing outputs: %w", err)
	}
	return nil
}

func uploadTopologyUntilSuccess(filePath, endpoint string) error {
	for {
		f, err := os.Open(filePath)
		if err != nil {
			return fmt.Errorf("open file: %w", err)
		}
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		part, err := w.CreateFormFile("file", filePath)
		if err != nil {
			f.Close()
			return fmt.Errorf("create form file: %w", err)
		}
		if _, err := io.Copy(part, f); err != nil {
			f.Close()
			return fmt.Errorf("copy file: %w", err)
		}
		f.Close()
		w.Close()

		req, err := http.NewRequest("POST", endpoint, &buf)
		if err != nil {
			return fmt.Errorf("new request: %w", err)
		}
		req.Header.Set("Content-Type", w.FormDataContentType())

		resp, err := http.DefaultClient.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return nil
		}
		if resp != nil {
			resp.Body.Close()
		}
		// retry forever
		time.Sleep(5 * time.Second)
	}
}

func generateInventoryFromYAML(topology Topology, ansDir string) error {
	// find ZTP server
	var ztpIP string
	for _, srv := range topology.Templates.Servers {
		if srv.Name == "ztp-server" && srv.ZTPServer != "" {
			ztpIP = srv.ZTPServer
			break
		}
	}
	if ztpIP == "" {
		fmt.Println("⚠️ No ZTP server; skipping inventory.")
		return nil
	}

	vendor := ""
	if len(topology.NetworkDevice.Routers) > 0 {
		vendor = topology.NetworkDevice.Routers[0].Vendor
	}
	osType := detectNetworkOS(vendor)

	fmt.Printf("🌐 Polling ZTP at http://%s:5000/inventory\n", ztpIP)
	raw, err := fetchZTPInventoryMapWithRetry(ztpIP, 200)
	if err != nil {
		return fmt.Errorf("failed to fetch inventory from ZTP: %w", err)
	}
	if err := writeInventoryFromZTPMap(raw, osType, ansDir); err != nil {
		return fmt.Errorf("failed to write Ansible inventory: %w", err)
	}
	fmt.Println("✅ Inventory written to", ansDir)
	return nil
}

func detectNetworkOS(vendor string) string {
	switch strings.ToLower(vendor) {
	case "cisco":
		return "ios"
	case "juniper":
		return "junos"
	case "arista":
		return "eos"
	default:
		return "linux"
	}
}

func fetchZTPInventoryMapWithRetry(ztpIP string, maxWait int) (map[string]interface{}, error) {
	url := fmt.Sprintf("http://%s:5000/inventory", ztpIP)
	interval, waited := 10, 0
	for waited < maxWait {
		resp, err := http.Get(url)
		if err == nil && resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			var raw map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&raw); err == nil {
				if all, ok := raw["all"].(map[string]interface{}); ok {
					if hosts, ok := all["hosts"].([]interface{}); ok && len(hosts) > 0 {
						fmt.Printf("✅ Found %d hosts\n", len(hosts))
						return raw, nil
					}
				}
			}
		}
		time.Sleep(time.Duration(interval) * time.Second)
		waited += interval
	}
	return nil, fmt.Errorf("inventory empty after %d seconds", maxWait)
}

func writeInventoryFromZTPMap(raw map[string]interface{}, osType, ansDir string) error {
	if err := os.MkdirAll(ansDir, 0755); err != nil {
		return fmt.Errorf("cannot create ansible dir %s: %w", ansDir, err)
	}

	iniLines := []string{"[all]"}
	var yamlB strings.Builder
	yamlB.WriteString("all:\n  hosts:\n")

	var names []string
	if all, ok := raw["all"].(map[string]interface{}); ok {
		if hosts, ok := all["hosts"].([]interface{}); ok {
			for _, h := range hosts {
				if n, ok := h.(string); ok {
					names = append(names, n)
				}
			}
		}
	}
	sort.Strings(names)

	for _, name := range names {
		info := raw[name].(map[string]interface{})
		host := fmt.Sprint(info["ansible_host"])
		user := fmt.Sprint(info["ansible_user"])
		pass := fmt.Sprint(info["ansible_password"])
		netos := fmt.Sprint(info["ansible_network_os"])
		if netos == "" {
			netos = osType
		}

		iniLines = append(iniLines, fmt.Sprintf(
			"%s ansible_host=%s ansible_connection=network_cli ansible_become=yes ansible_become_method=enable "+
				"ansible_user=%s ansible_password=%s ansible_network_os=%s",
			name, host, user, pass, netos,
		))

		yamlB.WriteString(fmt.Sprintf("    %s:\n", name))
		yamlB.WriteString(fmt.Sprintf("      ansible_host: %s\n", host))
		yamlB.WriteString("      ansible_connection: network_cli\n")
		yamlB.WriteString("      ansible_become: yes\n")
		yamlB.WriteString("      ansible_become_method: enable\n")
		yamlB.WriteString(fmt.Sprintf("      ansible_user: %s\n", user))
		yamlB.WriteString(fmt.Sprintf("      ansible_password: %s\n", pass))
		yamlB.WriteString("      ansible_ssh_common_args: '-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null'\n")
		yamlB.WriteString(fmt.Sprintf("      ansible_network_os: %s\n", netos))
	}

	yamlPath := filepath.Join(ansDir, "inventory.yml")
	iniPath := filepath.Join(ansDir, "inventory.ini")

	if err := ioutil.WriteFile(yamlPath, []byte(yamlB.String()), 0644); err != nil {
		return fmt.Errorf("writing %s: %w", yamlPath, err)
	}
	if err := ioutil.WriteFile(iniPath, []byte(strings.Join(iniLines, "\n")+"\n"), 0644); err != nil {
		return fmt.Errorf("writing %s: %w", iniPath, err)
	}
	return nil
}
func visualizeTopology(t Topology) {
	fmt.Println("\n📡 **Topology Visualization**")
	fmt.Println("==================================")
	fmt.Println("🖥️ Routers:")
	for _, r := range t.NetworkDevice.Routers {
		fmt.Printf("🔹 [ %s ]\n", r.Name)
	}
	if len(t.Switches) > 0 {
		fmt.Println("\n🖧 Switches:")
		for _, s := range t.Switches {
			fmt.Printf("🟦 [ %s ]\n", s.Name)
		}
	}
	if len(t.Clouds) > 0 {
		fmt.Println("\n☁️ Clouds:")
		for _, c := range t.Clouds {
			fmt.Printf("🌥️ [ %s ]\n", c.Name)
		}
	}
	if len(t.Links) > 0 {
		fmt.Println("\n🔗 Links:")
		for _, l := range t.Links {
			if len(l.Endpoints) == 2 {
				fmt.Printf("🔌 %s <---> %s\n",
					l.Endpoints[0].Name, l.Endpoints[1].Name,
				)
			}
		}
	}
	fmt.Println("==================================")
}
