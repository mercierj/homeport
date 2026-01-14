// Package storage provides mappers for AWS storage services.
package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/policy"
	"github.com/homeport/homeport/internal/domain/resource"
)

// S3Mapper converts AWS S3 buckets to MinIO.
type S3Mapper struct {
	*mapper.BaseMapper
}

// NewS3Mapper creates a new S3 to MinIO mapper.
func NewS3Mapper() *S3Mapper {
	return &S3Mapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeS3Bucket, nil),
	}
}

// Map converts an S3 bucket to a MinIO service.
func (m *S3Mapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	bucketName := res.GetConfigString("bucket")
	if bucketName == "" {
		bucketName = res.Name
	}

	// Create result using new API
	result := mapper.NewMappingResult("minio")
	svc := result.DockerService

	// Configure MinIO service
	svc.Image = "minio/minio:latest"
	svc.Environment = map[string]string{
		"MINIO_ROOT_USER":     "minioadmin",
		"MINIO_ROOT_PASSWORD": "minioadmin",
	}
	svc.Ports = []string{
		"9000:9000", // API
		"9001:9001", // Console
	}
	svc.Volumes = []string{
		"./data/minio:/data",
	}
	svc.Command = []string{"server", "/data", "--console-address", ":9001"}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "curl", "-f", "http://localhost:9000/minio/health/live"},
		Interval: 30 * time.Second,
		Timeout:  5 * time.Second,
		Retries:  3,
	}
	svc.Labels = map[string]string{
		"homeport.source": "aws_s3_bucket",
		"homeport.bucket": bucketName,
		"traefik.enable":   "true",
		"traefik.http.routers.minio.rule":                      "Host(`minio.localhost`)",
		"traefik.http.services.minio.loadbalancer.server.port": "9001",
	}

	// Generate MinIO client (mc) setup script
	mcScript := m.generateMCScript(res, bucketName)
	result.AddScript("setup_minio.sh", []byte(mcScript))

	// Handle versioning
	if res.GetConfigBool("versioning.enabled") || m.hasVersioningBlock(res) {
		result.AddManualStep(
			fmt.Sprintf("Enable versioning on bucket '%s' using: mc version enable local/%s", bucketName, bucketName))
	}

	// Handle lifecycle rules
	if lifecycleRules := m.getLifecycleRules(res); len(lifecycleRules) > 0 {
		lifecycleScript := m.generateLifecycleScript(bucketName, lifecycleRules)
		result.AddScript("configure_lifecycle.sh", []byte(lifecycleScript))
		result.AddWarning("S3 lifecycle rules have been partially mapped. Review the generated lifecycle script for accuracy.")
	}

	// Handle CORS configuration
	if corsRules := m.getCORSRules(res); len(corsRules) > 0 {
		corsConfig := m.generateCORSConfig(bucketName, corsRules)
		result.AddConfig(fmt.Sprintf("config/minio/%s-cors.json", bucketName), []byte(corsConfig))
		result.AddManualStep(
			fmt.Sprintf("Apply CORS configuration: mc anonymous set-json config/minio/%s-cors.json local/%s", bucketName, bucketName))
	}

	// Handle public access settings
	if m.isPublicBucket(res) {
		result.AddManualStep(fmt.Sprintf("Set public read access: mc anonymous set download local/%s", bucketName))
		result.AddWarning("Bucket has public access enabled. Ensure this is intentional in your self-hosted environment.")
	}

	// Handle encryption
	if res.GetConfigString("server_side_encryption_configuration") != "" {
		result.AddWarning("S3 server-side encryption is configured. MinIO supports encryption at rest - configure manually if needed.")
		result.AddManualStep("Configure MinIO encryption: https://min.io/docs/minio/linux/operations/server-side-encryption.html")
	}

	// Handle replication
	if res.GetConfigString("replication_configuration") != "" {
		result.AddWarning("S3 replication is configured. MinIO supports bucket replication - configure manually.")
	}

	return result, nil
}

// generateMCScript creates a MinIO client setup script.
func (m *S3Mapper) generateMCScript(res *resource.AWSResource, bucketName string) string {
	region := res.Region
	if region == "" {
		region = "us-east-1"
	}

	return fmt.Sprintf(`#!/bin/bash
# MinIO Client (mc) Setup Script
# This script configures the MinIO client and creates the bucket

set -e

echo "Setting up MinIO client..."

# Configure MinIO alias
mc alias set local http://localhost:9000 minioadmin minioadmin

# Create bucket
echo "Creating bucket: %s"
mc mb --ignore-existing local/%s

# Set region (for compatibility)
mc admin config set local region name=%s

echo "MinIO bucket '%s' is ready!"
echo "Access MinIO Console at: http://localhost:9001"
echo "Credentials: minioadmin / minioadmin"
echo ""
echo "To use with AWS SDK, configure endpoint: http://localhost:9000"
`, bucketName, bucketName, region, bucketName)
}

// generateLifecycleScript creates a script to configure lifecycle policies.
func (m *S3Mapper) generateLifecycleScript(bucketName string, rules []map[string]interface{}) string {
	script := fmt.Sprintf(`#!/bin/bash
# MinIO Lifecycle Configuration Script
# Configures lifecycle policies for bucket: %s

set -e

echo "Configuring lifecycle policies for %s..."

# Note: MinIO lifecycle rules use similar syntax to S3
# You may need to adjust these rules based on your MinIO version

`, bucketName, bucketName)

	for i, rule := range rules {
		ruleID := fmt.Sprintf("rule-%d", i)
		if id, ok := rule["id"].(string); ok {
			ruleID = id
		}
		script += fmt.Sprintf("# Rule: %s\n", ruleID)
		script += "# Review and apply manually using mc ilm add command\n\n"
	}

	script += `
echo "Lifecycle configuration complete!"
echo "Note: Review the rules above and apply them using 'mc ilm add' command"
`
	return script
}

// generateCORSConfig creates a CORS configuration file.
func (m *S3Mapper) generateCORSConfig(bucketName string, rules []map[string]interface{}) string {
	content := "{\n  \"CORSRules\": [\n"

	for i, rule := range rules {
		if i > 0 {
			content += ",\n"
		}
		content += "    {\n"

		if allowedOrigins, ok := rule["allowed_origins"].([]interface{}); ok {
			content += "      \"AllowedOrigins\": ["
			for j, origin := range allowedOrigins {
				if j > 0 {
					content += ", "
				}
				content += fmt.Sprintf("\"%v\"", origin)
			}
			content += "],\n"
		}

		if allowedMethods, ok := rule["allowed_methods"].([]interface{}); ok {
			content += "      \"AllowedMethods\": ["
			for j, method := range allowedMethods {
				if j > 0 {
					content += ", "
				}
				content += fmt.Sprintf("\"%v\"", method)
			}
			content += "]\n"
		}

		content += "    }"
	}

	content += "\n  ]\n}\n"
	return content
}

// hasVersioningBlock checks if the resource has versioning configuration.
func (m *S3Mapper) hasVersioningBlock(res *resource.AWSResource) bool {
	if versioning := res.Config["versioning"]; versioning != nil {
		if vm, ok := versioning.(map[string]interface{}); ok {
			if enabled, ok := vm["enabled"].(bool); ok {
				return enabled
			}
		}
	}
	return false
}

// getLifecycleRules extracts lifecycle rules from the resource.
func (m *S3Mapper) getLifecycleRules(res *resource.AWSResource) []map[string]interface{} {
	var rules []map[string]interface{}

	if lifecycle, ok := res.Config["lifecycle_rule"].(map[string]interface{}); ok {
		rules = append(rules, lifecycle)
	}

	if lifecycleSlice, ok := res.Config["lifecycle_rule"].([]interface{}); ok {
		for _, rule := range lifecycleSlice {
			if ruleMap, ok := rule.(map[string]interface{}); ok {
				rules = append(rules, ruleMap)
			}
		}
	}

	return rules
}

// getCORSRules extracts CORS rules from the resource.
func (m *S3Mapper) getCORSRules(res *resource.AWSResource) []map[string]interface{} {
	var rules []map[string]interface{}

	if corsSlice, ok := res.Config["cors_rule"].([]interface{}); ok {
		for _, rule := range corsSlice {
			if ruleMap, ok := rule.(map[string]interface{}); ok {
				rules = append(rules, ruleMap)
			}
		}
	}

	return rules
}

// isPublicBucket determines if the bucket has public access.
func (m *S3Mapper) isPublicBucket(res *resource.AWSResource) bool {
	// Check ACL
	acl := res.GetConfigString("acl")
	if acl == "public-read" || acl == "public-read-write" {
		return true
	}

	// Check public access block configuration
	if pab, ok := res.Config["public_access_block_configuration"].(map[string]interface{}); ok {
		blockPublicAcls := true
		if val, ok := pab["block_public_acls"].(bool); ok {
			blockPublicAcls = val
		}
		return !blockPublicAcls
	}

	// Check bucket policy for public statements
	if policyDoc := res.GetConfigString("policy"); policyDoc != "" {
		return strings.Contains(strings.ToLower(policyDoc), "\"effect\":\"allow\"") &&
			strings.Contains(strings.ToLower(policyDoc), "\"principal\":\"*\"")
	}

	return false
}

// ExtractPolicies extracts bucket policies and ACLs from the S3 bucket.
func (m *S3Mapper) ExtractPolicies(ctx context.Context, res *resource.AWSResource) ([]*policy.Policy, error) {
	var policies []*policy.Policy

	bucketName := res.GetConfigString("bucket")
	if bucketName == "" {
		bucketName = res.Name
	}

	// Extract bucket policy
	if policyDoc := res.GetConfigString("policy"); policyDoc != "" {
		p := policy.NewPolicy(
			res.ID+"-bucket-policy",
			bucketName+" Bucket Policy",
			policy.PolicyTypeResource,
			policy.ProviderAWS,
		)
		p.ResourceID = res.ID
		p.ResourceType = "aws_s3_bucket"
		p.ResourceName = bucketName
		p.OriginalDocument = json.RawMessage(policyDoc)
		p.OriginalFormat = "json"
		p.NormalizedPolicy = m.normalizeBucketPolicy(policyDoc)

		// Check for public access warnings
		if m.isPublicBucket(res) {
			p.AddWarning("Bucket policy allows public access")
		}

		policies = append(policies, p)
	}

	// Extract ACL as pseudo-policy if present
	if acl := res.GetConfigString("acl"); acl != "" && acl != "private" {
		p := policy.NewPolicy(
			res.ID+"-acl",
			bucketName+" ACL",
			policy.PolicyTypeResource,
			policy.ProviderAWS,
		)
		p.ResourceID = res.ID
		p.ResourceType = "aws_s3_bucket"
		p.ResourceName = bucketName
		p.OriginalDocument = json.RawMessage(fmt.Sprintf(`{"acl": "%s"}`, acl))
		p.OriginalFormat = "json"
		p.NormalizedPolicy = m.normalizeACL(acl)

		if acl == "public-read" || acl == "public-read-write" {
			p.AddWarning("ACL grants public access to bucket")
		}

		policies = append(policies, p)
	}

	// Extract public access block settings
	if pab, ok := res.Config["public_access_block_configuration"].(map[string]interface{}); ok {
		pabJSON, _ := json.Marshal(pab)
		p := policy.NewPolicy(
			res.ID+"-public-access-block",
			bucketName+" Public Access Block",
			policy.PolicyTypeResource,
			policy.ProviderAWS,
		)
		p.ResourceID = res.ID
		p.ResourceType = "aws_s3_bucket_public_access_block"
		p.ResourceName = bucketName
		p.OriginalDocument = pabJSON
		p.OriginalFormat = "json"

		policies = append(policies, p)
	}

	return policies, nil
}

// normalizeBucketPolicy converts an AWS S3 bucket policy to normalized format.
func (m *S3Mapper) normalizeBucketPolicy(policyDoc string) *policy.NormalizedPolicy {
	normalized := &policy.NormalizedPolicy{
		Statements: make([]policy.Statement, 0),
	}

	var awsPolicy struct {
		Version   string `json:"Version"`
		Statement []struct {
			Sid       string      `json:"Sid"`
			Effect    string      `json:"Effect"`
			Principal interface{} `json:"Principal"`
			Action    interface{} `json:"Action"`
			Resource  interface{} `json:"Resource"`
			Condition interface{} `json:"Condition"`
		} `json:"Statement"`
	}

	if err := json.Unmarshal([]byte(policyDoc), &awsPolicy); err != nil {
		return normalized
	}

	normalized.Version = awsPolicy.Version

	for _, stmt := range awsPolicy.Statement {
		normalizedStmt := policy.Statement{
			SID:    stmt.Sid,
			Effect: policy.Effect(stmt.Effect),
		}

		// Parse principals
		normalizedStmt.Principals = m.parsePrincipals(stmt.Principal)

		// Parse actions
		normalizedStmt.Actions = m.parseStringOrSlice(stmt.Action)

		// Parse resources
		normalizedStmt.Resources = m.parseStringOrSlice(stmt.Resource)

		// Parse conditions
		normalizedStmt.Conditions = m.parseConditions(stmt.Condition)

		normalized.Statements = append(normalized.Statements, normalizedStmt)
	}

	return normalized
}

// normalizeACL converts an S3 ACL to a normalized policy.
func (m *S3Mapper) normalizeACL(acl string) *policy.NormalizedPolicy {
	normalized := &policy.NormalizedPolicy{
		Statements: make([]policy.Statement, 0),
	}

	switch acl {
	case "public-read":
		normalized.Statements = append(normalized.Statements, policy.Statement{
			Effect: policy.EffectAllow,
			Principals: []policy.Principal{
				{Type: "*", ID: "*"},
			},
			Actions:   []string{"s3:GetObject"},
			Resources: []string{"*"},
		})
	case "public-read-write":
		normalized.Statements = append(normalized.Statements, policy.Statement{
			Effect: policy.EffectAllow,
			Principals: []policy.Principal{
				{Type: "*", ID: "*"},
			},
			Actions:   []string{"s3:GetObject", "s3:PutObject"},
			Resources: []string{"*"},
		})
	case "authenticated-read":
		normalized.Statements = append(normalized.Statements, policy.Statement{
			Effect: policy.EffectAllow,
			Principals: []policy.Principal{
				{Type: "AWS", ID: "AuthenticatedUsers"},
			},
			Actions:   []string{"s3:GetObject"},
			Resources: []string{"*"},
		})
	}

	return normalized
}

// parsePrincipals converts AWS principal format to normalized principals.
func (m *S3Mapper) parsePrincipals(principal interface{}) []policy.Principal {
	var principals []policy.Principal

	if principal == nil {
		return principals
	}

	switch p := principal.(type) {
	case string:
		if p == "*" {
			principals = append(principals, policy.Principal{Type: "*", ID: "*"})
		} else {
			principals = append(principals, policy.Principal{Type: "AWS", ID: p})
		}
	case map[string]interface{}:
		for pType, pValue := range p {
			ids := m.parseStringOrSlice(pValue)
			for _, id := range ids {
				principals = append(principals, policy.Principal{Type: pType, ID: id})
			}
		}
	}

	return principals
}

// parseStringOrSlice handles AWS policy fields that can be string or array.
func (m *S3Mapper) parseStringOrSlice(value interface{}) []string {
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case string:
		return []string{v}
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}

	return nil
}

// parseConditions converts AWS conditions to normalized format.
func (m *S3Mapper) parseConditions(condition interface{}) []policy.Condition {
	var conditions []policy.Condition

	if condition == nil {
		return conditions
	}

	condMap, ok := condition.(map[string]interface{})
	if !ok {
		return conditions
	}

	for operator, keys := range condMap {
		keyMap, ok := keys.(map[string]interface{})
		if !ok {
			continue
		}

		for key, values := range keyMap {
			cond := policy.Condition{
				Operator: operator,
				Key:      key,
				Values:   m.parseStringOrSlice(values),
			}
			conditions = append(conditions, cond)
		}
	}

	return conditions
}
