package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"
	"text/template"
)

// funcMap holds custom template functions for Terraform rendering.
var funcMap = template.FuncMap{
	"seq":      seq,
	"multiply": multiply,
	"mod":      mod,
	"add":      func(a, b int) int { return a + b },
}

// Change function signature
func runCommandInDir(cmdName string, args []string, dir string, logFile *os.File) error {
	cmd := exec.Command(cmdName, args...)
	cmd.Dir = dir
	if logFile != nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	} else {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	return cmd.Run()
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
	// parse the template with custom functions
	tmpl, err := template.New("terraform").Funcs(funcMap).Parse(templateContent)
	if err != nil {
		return fmt.Errorf("template parsing failed: %w", err)
	}

	// execute the template into a builder
	var expanded strings.Builder
	if err := tmpl.Execute(&expanded, data); err != nil {
		return fmt.Errorf("template execution failed: %w", err)
	}

	// ensure the directory exists
	if err := os.MkdirAll(path.Dir(filename), 0755); err != nil {
		return fmt.Errorf("could not create directory for %s: %w", filename, err)
	}

	// write the rendered template to the target filename
	if err := os.WriteFile(filename, []byte(expanded.String()), 0644); err != nil {
		return fmt.Errorf("could not write Terraform file %s: %w", filename, err)
	}

	return nil
}
