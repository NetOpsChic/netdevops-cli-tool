package cmd

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var orchestrateCmd = &cobra.Command{
	Use:   "gns3-orchestrate",
	Short: "Runs the full NetDevOps automation pipeline like magic 🧙‍♂️",
	Run: func(cmd *cobra.Command, args []string) {
		topologyFile, _ := cmd.Flags().GetString("source-of-truth")
		inventoryFile := "ansible-inventory/inventory.yaml"

		printBanner()

		runStep("📦 STEP 1: Deploying the Topology",
			"./netdevops",
			[]string{"gns3-deploy-yaml", "--config", topologyFile},
			"Check if the router template exists in GNS3 and all interfaces/links are valid.")

		sleepWithCountdown(2, "🧘 Routers are chilling for a sec...")

		runStep("📋 STEP 3: Fetching Ansible Inventory",
			"./netdevops",
			[]string{"gns3-inventory", "--config", topologyFile},
			"Verify that the ZTP server is running and leases are properly assigned at /inventory.")

		sleepWithCountdown(20, "📦 Waiting for the inventory magic to finalize...")

		waitForSSH("192.168.100.10") // R1 sample wait — add more if needed

		runStep("⚙️ STEP 4: Configuring Routers via Ansible",
			"./netdevops",
			[]string{"gns3-configure", "--config", topologyFile, "--inventory", inventoryFile},
			"Check SSH connectivity, ensure IPs are reachable, and Ansible configs are valid.")

		sleepWithCountdown(45, "🔮 Letting those SSH spells settle...")

		runStep("🧪 STEP 5: Validating the Network",
			"./netdevops",
			[]string{"gns3-validate", "--config", topologyFile},
			"Double-check that router configs are correct and ping tests are allowed between nodes.")

		color.Cyan("\n📊 STEP 6: Observer Tower is external — checking Grafana at http://192.168.100.24:3000 ...")
		openGrafana("http://192.168.100.24:3000")

		printSummary()
	},
}

func init() {
	orchestrateCmd.Flags().String("source-of-truth", "topology.yaml", "YAML file that defines the full topology as the source of truth 📜")
	rootCmd.AddCommand(orchestrateCmd)
}

func runStep(title string, command string, args []string, debugHint string) {
	color.New(color.FgHiCyan, color.Bold).Printf("\n%s\n", title)
	cmd := exec.Command(command, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		color.Red("❌ Failed to run %s: %v", command, err)
		if debugHint != "" {
			color.Yellow("💡 Debug Tip: %s", debugHint)
		}
		os.Exit(1)
	}
	color.Green("✅ Done!")
}

func sleepWithCountdown(seconds int, message string) {
	color.Yellow("%s", message)
	for i := seconds; i > 0; i-- {
		fmt.Printf("  ⏳ %d seconds remaining...\r", i)
		time.Sleep(1 * time.Second)
	}
	fmt.Print("                                 \r")
	color.Green("⏱️ Wait complete!")
}

func waitForSSH(ip string) {
	color.Cyan("🔐 Waiting for SSH on %s:22...", ip)
	for i := 0; i < 10; i++ {
		conn, err := net.DialTimeout("tcp", ip+":22", 2*time.Second)
		if err == nil {
			conn.Close()
			color.Green("✅ SSH is available on %s!", ip)
			return
		}
		fmt.Printf("⏳ Checking SSH... (%d/10)\r", i+1)
		time.Sleep(2 * time.Second)
	}
	fmt.Print("                               \r")
	color.Red("❌ SSH is not reachable at %s:22 after timeout.\n", ip)
	os.Exit(1)
}

func openGrafana(url string) {
	color.Cyan("\n🌐 Checking if Grafana is live at %s", url)
	for i := 0; i < 30; i++ {
		resp, err := http.Get(url)
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			color.Green("✅ Grafana is up! Opening in browser...")
			launchBrowser(url)
			return
		}
		fmt.Printf("🔄 Waiting for Grafana... (%d/30)\r", i+1)
		time.Sleep(2 * time.Second)
	}
	fmt.Print("                                  \r")
	color.Red("❌ Grafana is not reachable after multiple attempts.")
	color.Yellow("💡 Debug Tip: Is the Observer Tower running at 192.168.100.24? Is port 3000 exposed?")
}

func launchBrowser(url string) {
	var err error
	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	default:
		fmt.Println("🚫 Cannot open browser: unsupported OS")
		return
	}

	if err != nil {
		color.Red("❌ Failed to open browser: %v", err)
	}
}

func printBanner() {
	color.New(color.FgHiMagenta, color.Bold).Printf(`
╭──────────────────────────────────────────────╮
│  🔥 NetDevOps CLI: GNS3 Orchestration Mode  │
╰──────────────────────────────────────────────╯
`)
}

func printSummary() {
	color.New(color.FgHiGreen, color.Bold).Printf(`
💅 Pipeline? Handled.
🎯 Routers? Summoned.
📊 Monitoring? Live and lookin' fine.

╭──────────────────────🎉 Deployment Recap 🎉─────────────────────╮
│  📂 Source of Truth : topology.yaml                            │
│  🧙 Inventory      : /ansible-inventory/inventory.yaml         │
│  📈 Grafana        : http://192.168.100.24:3000 (Dashboard's poppin) │
╰────────────────────────────────────────────────────────────────╯

💡 You didn’t just deploy a network...
   You orchestrated a damn symphony 🎻
`)
}
