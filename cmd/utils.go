package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
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
		fmt.Printf("❌ ERROR running %s in %s: %v\n", command, dir, err)
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
	if start > end {
		return []int{}
	}
	s := make([]int, end-start+1)
	for i := range s {
		s[i] = start + i
	}
	return s
}

// generateTerraformFile creates a Terraform file dynamically
// generateTerraformFile creates a Terraform file dynamically
func generateTerraformFile(filename, templateContent string, data interface{}) error {
	debugFile := "terraform/debug_template.txt"
	mainFile := "terraform/main.tf"

	// STEP 1️⃣: Save the raw template for debugging
	err := os.WriteFile(debugFile, []byte(templateContent), 0644)
	if err != nil {
		fmt.Println("🚨 ERROR: Could not write debug template file!", err)
		return err
	}

	// STEP 2️⃣: Print the full template for debugging (before processing)
	fmt.Println("\n🔎 === DEBUG: RAW TEMPLATE BEFORE PARSING ===")
	lines := strings.Split(templateContent, "\n")
	for i, line := range lines {
		fmt.Printf("%d: %s\n", i+1, line)
	}
	fmt.Println("=======================================")

	// STEP 3️⃣: Create Terraform file
	file, err := os.Create(mainFile)
	if err != nil {
		fmt.Println("🚨 ERROR: Could not create Terraform file!", err)
		return err
	}
	defer file.Close()

	// STEP 4️⃣: Register functions
	funcMap := template.FuncMap{
		"seq":      seq,
		"multiply": multiply,
		"mod":      mod,
	}

	// STEP 5️⃣: Parse the template
	tmpl, err := template.New("terraform").Funcs(funcMap).Parse(templateContent)
	if err != nil {
		fmt.Println("\n❌ ERROR: TEMPLATE PARSING FAILED!")
		fmt.Println("📌 Check terraform/debug_template.txt for errors")
		fmt.Println("=======================================")
		return err
	}

	// STEP 6️⃣: Execute template into a buffer first
	var expandedTemplate strings.Builder
	err = tmpl.Execute(&expandedTemplate, data)
	if err != nil {
		fmt.Println("\n❌ ERROR: TEMPLATE EXECUTION FAILED!")
		fmt.Println("⚠️ DEBUG: Template Execution Error:", err)
		fmt.Println("📌 Check terraform/debug_template.txt for errors")
		fmt.Println("=======================================")
		return err
	}

	// STEP 7️⃣: Write expanded template to main.tf
	err = os.WriteFile(mainFile, []byte(expandedTemplate.String()), 0644)
	if err != nil {
		fmt.Println("🚨 ERROR: Could not write Terraform file!", err)
		return err
	}

	// STEP 8️⃣: Print the **FULL** Terraform file for debugging
	fmt.Println("\n✅ === DEBUG: FINAL GENERATED TERRAFORM FILE ===")
	lines = strings.Split(expandedTemplate.String(), "\n")
	for i, line := range lines {
		fmt.Printf("%d: %s\n", i+1, line)
	}
	fmt.Println("=======================================")

	fmt.Println("\n✅ SUCCESS: Terraform file written successfully!")
	return nil
}
