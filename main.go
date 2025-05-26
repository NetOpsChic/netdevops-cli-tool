package main

import (
	"fmt"
	"netdevops-cli-tool/cmd"
	"os"
)

func main() {
	// Magic background reconcile entrypoint, NOT a user CLI command
	if len(os.Args) > 1 && os.Args[1] == "__reconcile_daemon" {
		var configFile, projectID string
		for i := 2; i < len(os.Args)-1; i++ {
			if os.Args[i] == "--config" {
				configFile = os.Args[i+1]
			}
			if os.Args[i] == "--project-id" {
				projectID = os.Args[i+1]
			}
		}
		if configFile == "" || projectID == "" {
			fmt.Fprintln(os.Stderr, "Missing --config or --project-id for reconciliation daemon")
			os.Exit(1)
		}
		// This is your function from cmd/reconcile.go!
		cmd.StartReconcileDaemon(configFile, projectID)
		os.Exit(0)
	}

	// Normal CLI mode
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "\n❌", err)
		os.Exit(1)
	}
}
