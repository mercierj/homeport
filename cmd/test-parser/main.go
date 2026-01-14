package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/homeport/homeport/internal/infrastructure/parser"
)

func main() {
	// Get the project root directory
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

	// Path to test fixture
	fixturePath := filepath.Join(cwd, "test", "fixtures", "simple-webapp")

	fmt.Println("Homeport Terraform Parser Test")
	fmt.Println("================================")
	fmt.Println()

	// Test parsing the Terraform project
	fmt.Printf("Parsing Terraform project at: %s\n", fixturePath)
	infra, err := parser.ParseTerraformProject(fixturePath)
	if err != nil {
		log.Fatalf("Failed to parse Terraform project: %v", err)
	}

	fmt.Printf("\nSuccessfully parsed infrastructure!\n")
	fmt.Printf("Provider: %s\n", infra.Provider)
	fmt.Printf("Resources: %d\n", len(infra.Resources))
	fmt.Printf("Metadata entries: %d\n", len(infra.Metadata))
	fmt.Println()

	// Print resources by category
	categories := make(map[string][]string)
	for id, res := range infra.Resources {
		category := res.Type.GetCategory().String()
		categories[category] = append(categories[category], id)
	}

	fmt.Println("Resources by category:")
	fmt.Println("----------------------")
	for category, resources := range categories {
		fmt.Printf("\n%s (%d):\n", category, len(resources))
		for _, id := range resources {
			res, _ := infra.GetResource(id)
			fmt.Printf("  - %s (name: %s)\n", id, res.Name)

			// Print some key attributes
			switch res.Type {
			case "aws_instance":
				fmt.Printf("    Instance type: %s\n", res.GetConfigString("instance_type"))
				fmt.Printf("    AMI: %s\n", res.GetConfigString("ami"))
			case "aws_db_instance":
				fmt.Printf("    Engine: %s %s\n", res.GetConfigString("engine"), res.GetConfigString("engine_version"))
				fmt.Printf("    Instance class: %s\n", res.GetConfigString("instance_class"))
			case "aws_s3_bucket":
				fmt.Printf("    Bucket: %s\n", res.GetConfigString("bucket"))
			case "aws_lb":
				fmt.Printf("    Type: %s\n", res.GetConfigString("load_balancer_type"))
				fmt.Printf("    DNS: %s\n", res.GetConfigString("dns_name"))
			}

			// Print dependencies
			if len(res.Dependencies) > 0 {
				fmt.Printf("    Dependencies: %v\n", res.Dependencies)
			}

			// Print tags
			if len(res.Tags) > 0 {
				fmt.Printf("    Tags: ")
				for k, v := range res.Tags {
					fmt.Printf("%s=%s ", k, v)
				}
				fmt.Println()
			}
		}
	}

	// Validate infrastructure
	fmt.Println("\nValidating infrastructure...")
	if err := infra.Validate(); err != nil {
		log.Fatalf("Validation failed: %v", err)
	}
	fmt.Println("Validation passed!")

	// Print metadata
	if len(infra.Metadata) > 0 {
		fmt.Println("\nMetadata:")
		fmt.Println("---------")
		for k, v := range infra.Metadata {
			fmt.Printf("  %s: %s\n", k, v)
		}
	}

	fmt.Println("\nTest completed successfully!")
}
