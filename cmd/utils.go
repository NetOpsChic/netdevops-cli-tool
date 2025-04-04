package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/template"
)

// runCommandInDir runs a command inside a specific directory.
func runCommandInDir(command string, args []string, dir string) {
	cmd := exec.Command(command, args...)
	cmd.Dir = dir // Set the working directory.
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("❌ ERROR running %s in %s: %v\n", command, dir, err)
		os.Exit(1)
	}
}

// multiply returns the product of two integers.
func multiply(a, b int) int {
	return a * b
}

// mod returns the remainder when a is divided by b.
func mod(a, b int) int {
	return a % b
}

// seq returns a slice of integers from start to end inclusive.
// For example, seq 1 3 returns []int{1,2,3}.
func seq(start, end int) []int {
	if start > end {
		return []int{}
	}
	s := make([]int, end-start+1)
	for i := range s {
		s[i] = start + i
	}
	return s
}

// generateTerraformFile creates a Terraform file dynamically based on a template.
func generateTerraformFile(filename, templateContent string, data interface{}) error {
	debugFile := "terraform/debug_template.txt"
	mainFile := "terraform/main.tf"

	// Optional: Keep raw template saved for troubleshooting (no console print)
	err := os.WriteFile(debugFile, []byte(templateContent), 0644)
	if err != nil {
		return fmt.Errorf("could not write debug template: %w", err)
	}

	// Create or truncate the main Terraform file
	file, err := os.Create(mainFile)
	if err != nil {
		return fmt.Errorf("could not create main Terraform file: %w", err)
	}
	defer file.Close()

	// Register custom template functions
	funcMap := template.FuncMap{
		"seq":      seq,
		"multiply": multiply,
		"mod":      mod,
		"add":      func(a, b int) int { return a + b },
	}

	// Parse the template with functions
	tmpl, err := template.New("terraform").Funcs(funcMap).Parse(templateContent)
	if err != nil {
		return fmt.Errorf("template parsing failed (check debug_template.txt): %w", err)
	}

	// Execute template into a buffer
	var expandedTemplate strings.Builder
	err = tmpl.Execute(&expandedTemplate, data)
	if err != nil {
		return fmt.Errorf("template execution failed (check debug_template.txt): %w", err)
	}

	// Write final output to main Terraform file
	err = os.WriteFile(mainFile, []byte(expandedTemplate.String()), 0644)
	if err != nil {
		return fmt.Errorf("could not write final Terraform file: %w", err)
	}

	return nil
}
