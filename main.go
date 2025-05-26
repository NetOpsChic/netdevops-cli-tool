/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>
*/
package main

import (
	"fmt"
	"netdevops-cli-tool/cmd"
	"os"
)

func main() {
	if err := cmd.Execute(); err != nil {
		// Cobra prints: Error: <message>
		// Print again with emoji as summary
		fmt.Fprintln(os.Stderr, "\n❌", err)
		os.Exit(1)
	}
}
