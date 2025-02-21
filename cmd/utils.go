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

	// STEP 1: Save the raw template for debugging.
	err := os.WriteFile(debugFile, []byte(templateContent), 0644)
	if err != nil {
		fmt.Println("🚨 ERROR: Could not write debug template file!", err)
		return err
	}

	// STEP 2: Print the raw template for debugging.
	fmt.Println("\n🔎 === DEBUG: RAW TEMPLATE BEFORE PARSING ===")
	lines := strings.Split(templateContent, "\n")
	for i, line := range lines {
		fmt.Printf("%d: %s\n", i+1, line)
	}
	fmt.Println("=======================================")

	// STEP 3: Create (or truncate) the Terraform file.
	file, err := os.Create(mainFile)
	if err != nil {
		fmt.Println("🚨 ERROR: Could not create Terraform file!", err)
		return err
	}
	defer file.Close()

	// STEP 4: Register custom functions.
	funcMap := template.FuncMap{
		"seq":      seq,
		"multiply": multiply,
		"mod":      mod,
	}

	// STEP 5: Parse the template with the registered function map.
	tmpl, err := template.New("terraform").Funcs(funcMap).Parse(templateContent)
	if err != nil {
		fmt.Println("\n❌ ERROR: TEMPLATE PARSING FAILED!")
		fmt.Println("📌 Check terraform/debug_template.txt for errors")
		fmt.Println("=======================================")
		return err
	}

	// STEP 6: Execute the template into a buffer.
	var expandedTemplate strings.Builder
	err = tmpl.Execute(&expandedTemplate, data)
	if err != nil {
		fmt.Println("\n❌ ERROR: TEMPLATE EXECUTION FAILED!")
		fmt.Println("⚠️ DEBUG: Template Execution Error:", err)
		fmt.Println("📌 Check terraform/debug_template.txt for errors")
		fmt.Println("=======================================")
		return err
	}

	// STEP 7: Write the expanded template to the main Terraform file.
	err = os.WriteFile(mainFile, []byte(expandedTemplate.String()), 0644)
	if err != nil {
		fmt.Println("🚨 ERROR: Could not write Terraform file!", err)
		return err
	}

	// STEP 8: Print the final generated Terraform file for debugging.
	fmt.Println("\n✅ === DEBUG: FINAL GENERATED TERRAFORM FILE ===")
	lines = strings.Split(expandedTemplate.String(), "\n")
	for i, line := range lines {
		fmt.Printf("%d: %s\n", i+1, line)
	}
	fmt.Println("=======================================")
	fmt.Println("\n✅ SUCCESS: Terraform file written successfully!")
	return nil
}
