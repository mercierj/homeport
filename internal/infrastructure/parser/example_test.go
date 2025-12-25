package parser_test

import (
	"fmt"
	"log"

	"github.com/cloudexit/cloudexit/internal/infrastructure/parser"
)

// ExampleParseState demonstrates how to parse a Terraform state file
func ExampleParseState() {
	// Parse a Terraform state file
	infra, err := parser.ParseState("/path/to/terraform.tfstate")
	if err != nil {
		log.Fatal(err)
	}

	// Print resource count
	fmt.Printf("Found %d resources\n", len(infra.Resources))

	// Iterate over resources
	for id, res := range infra.Resources {
		fmt.Printf("Resource: %s (type: %s)\n", id, res.Type)
	}
}

// ExampleParseHCL demonstrates how to parse Terraform HCL files
func ExampleParseHCL() {
	// Parse Terraform .tf files from a directory
	infra, err := parser.ParseHCL("/path/to/terraform")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Found %d resources\n", len(infra.Resources))
}

// ExampleBuildInfrastructure demonstrates how to build infrastructure from both state and HCL
func ExampleBuildInfrastructure() {
	// Build infrastructure from both state file and HCL files
	infra, err := parser.BuildInfrastructure(
		"/path/to/terraform.tfstate",
		"/path/to/terraform",
	)
	if err != nil {
		log.Fatal(err)
	}

	// Get all EC2 instances
	ec2Instances := infra.GetResourcesByType("aws_instance")
	fmt.Printf("Found %d EC2 instances\n", len(ec2Instances))

	// Get a specific resource
	res, err := infra.GetResource("aws_instance.web")
	if err != nil {
		log.Fatal(err)
	}

	// Access resource properties
	fmt.Printf("Instance type: %s\n", res.GetConfigString("instance_type"))
	fmt.Printf("Region: %s\n", res.Region)

	// Check tags
	if name := res.Tags["Name"]; name != "" {
		fmt.Printf("Instance name: %s\n", name)
	}

	// Get dependencies
	deps, err := infra.GetDependencies(res.ID)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Dependencies: %d\n", len(deps))
}

// ExampleParseTerraformProject demonstrates auto-detection of Terraform files
func ExampleParseTerraformProject() {
	// Automatically find and parse terraform.tfstate and .tf files
	infra, err := parser.ParseTerraformProject("/path/to/terraform/project")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Found %d resources\n", len(infra.Resources))
}

// ExampleParseWithOptions demonstrates advanced parsing with options
func ExampleParseWithOptions() {
	opts := parser.ParseOptions{
		StatePath:           "/path/to/terraform.tfstate",
		TerraformDir:        "/path/to/terraform",
		ExtractDependencies: true,
		ValidateResources:   true,
	}

	infra, err := parser.ParseWithOptions(opts)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Found %d resources\n", len(infra.Resources))
}

// ExampleWorkingWithResources demonstrates various resource operations
func ExampleWorkingWithResources() {
	infra, err := parser.ParseState("/path/to/terraform.tfstate")
	if err != nil {
		log.Fatal(err)
	}

	// Get all resources of a specific type
	databases := infra.GetResourcesByType("aws_db_instance")
	for _, db := range databases {
		fmt.Printf("Database: %s\n", db.Name)
		fmt.Printf("  Engine: %s\n", db.GetConfigString("engine"))
		fmt.Printf("  Instance class: %s\n", db.GetConfigString("instance_class"))
	}

	// Get all S3 buckets
	buckets := infra.GetResourcesByType("aws_s3_bucket")
	for _, bucket := range buckets {
		fmt.Printf("Bucket: %s\n", bucket.GetConfigString("bucket"))
	}

	// Get all load balancers
	lbs := infra.GetResourcesByType("aws_lb")
	for _, lb := range lbs {
		fmt.Printf("Load balancer: %s\n", lb.Name)
		fmt.Printf("  Type: %s\n", lb.GetConfigString("load_balancer_type"))
		fmt.Printf("  DNS: %s\n", lb.GetConfigString("dns_name"))
	}
}
