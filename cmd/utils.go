package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"text/template"
)

// runCommandInDir runs a command inside a specific directory
func runCommandInDir(command string, args []string, dir string) {
	cmd := exec.Command(command, args...)
	cmd.Dir = dir // Set the working directory to terraform/
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		fmt.Printf("Error running %s in %s: %v\n", command, dir, err)
		os.Exit(1)
	}
}

// Custom math functions for Terraform template
func multiply(a, b int) int {
	return a * b
}

func mod(a, b int) int {
	return a % b
}

// Custom sequence generator function (fixes 'seq' function not found)
func seq(start, end int) []int {
	s := make([]int, end-start+1)
	for i := range s {
		s[i] = start + i
	}
	return s
}

// generateTerraformFile creates a Terraform file dynamically
func generateTerraformFile(filename, templateContent string, data interface{}) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	// Register custom functions for template parsing
	funcMap := template.FuncMap{
		"multiply": multiply,
		"mod":      mod,
		"seq":      seq, // Add 'seq' function here
	}

	tmpl, err := template.New("terraform").Funcs(funcMap).Parse(templateContent)
	if err != nil {
		return err
	}

	return tmpl.Execute(file, data)
}
