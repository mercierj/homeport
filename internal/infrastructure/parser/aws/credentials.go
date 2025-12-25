// Package aws provides AWS infrastructure parsing via API and CloudFormation.
package aws

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// CredentialSource defines how credentials are obtained.
type CredentialSource string

const (
	// CredentialSourceEnv uses AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY environment variables.
	CredentialSourceEnv CredentialSource = "env"
	// CredentialSourceProfile uses a named profile from ~/.aws/credentials.
	CredentialSourceProfile CredentialSource = "profile"
	// CredentialSourceRole uses IAM role assumption.
	CredentialSourceRole CredentialSource = "role"
	// CredentialSourceStatic uses explicitly provided credentials.
	CredentialSourceStatic CredentialSource = "static"
	// CredentialSourceDefault uses the default credential chain.
	CredentialSourceDefault CredentialSource = "default"
)

// CredentialConfig holds AWS credential configuration.
type CredentialConfig struct {
	// Source determines how credentials are obtained.
	Source CredentialSource

	// Profile is the AWS profile name (for CredentialSourceProfile).
	Profile string

	// Region is the AWS region to use.
	Region string

	// AccessKeyID for static credentials.
	AccessKeyID string

	// SecretAccessKey for static credentials.
	SecretAccessKey string

	// SessionToken for temporary credentials.
	SessionToken string

	// RoleARN for role assumption.
	RoleARN string

	// ExternalID for role assumption.
	ExternalID string
}

// NewCredentialConfig creates a default credential configuration.
func NewCredentialConfig() *CredentialConfig {
	return &CredentialConfig{
		Source: CredentialSourceDefault,
		Region: "us-east-1",
	}
}

// WithProfile sets the AWS profile to use.
func (c *CredentialConfig) WithProfile(profile string) *CredentialConfig {
	c.Source = CredentialSourceProfile
	c.Profile = profile
	return c
}

// WithRegion sets the AWS region.
func (c *CredentialConfig) WithRegion(region string) *CredentialConfig {
	c.Region = region
	return c
}

// WithStaticCredentials sets explicit credentials.
func (c *CredentialConfig) WithStaticCredentials(accessKeyID, secretAccessKey, sessionToken string) *CredentialConfig {
	c.Source = CredentialSourceStatic
	c.AccessKeyID = accessKeyID
	c.SecretAccessKey = secretAccessKey
	c.SessionToken = sessionToken
	return c
}

// WithRole sets the role to assume.
func (c *CredentialConfig) WithRole(roleARN string, externalID string) *CredentialConfig {
	c.Source = CredentialSourceRole
	c.RoleARN = roleARN
	c.ExternalID = externalID
	return c
}

// LoadConfig loads AWS SDK configuration based on the credential config.
func (c *CredentialConfig) LoadConfig(ctx context.Context) (aws.Config, error) {
	var opts []func(*config.LoadOptions) error

	// Set region
	if c.Region != "" {
		opts = append(opts, config.WithRegion(c.Region))
	}

	switch c.Source {
	case CredentialSourceEnv:
		// Environment variables are checked automatically by default chain
		// but we can be explicit
		accessKey := os.Getenv("AWS_ACCESS_KEY_ID")
		secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
		sessionToken := os.Getenv("AWS_SESSION_TOKEN")
		if accessKey == "" || secretKey == "" {
			return aws.Config{}, fmt.Errorf("AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY must be set")
		}
		opts = append(opts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKey, secretKey, sessionToken),
		))

	case CredentialSourceProfile:
		if c.Profile != "" {
			opts = append(opts, config.WithSharedConfigProfile(c.Profile))
		}

	case CredentialSourceStatic:
		if c.AccessKeyID == "" || c.SecretAccessKey == "" {
			return aws.Config{}, fmt.Errorf("access key ID and secret access key are required for static credentials")
		}
		opts = append(opts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(c.AccessKeyID, c.SecretAccessKey, c.SessionToken),
		))

	case CredentialSourceRole:
		// First load base config, then wrap with role assumption
		baseCfg, err := config.LoadDefaultConfig(ctx, opts...)
		if err != nil {
			return aws.Config{}, fmt.Errorf("failed to load base config for role assumption: %w", err)
		}

		stsClient := sts.NewFromConfig(baseCfg)
		assumeRoleOpts := func(o *stscreds.AssumeRoleOptions) {
			if c.ExternalID != "" {
				o.ExternalID = aws.String(c.ExternalID)
			}
		}
		creds := stscreds.NewAssumeRoleProvider(stsClient, c.RoleARN, assumeRoleOpts)
		return aws.Config{
			Region:      c.Region,
			Credentials: aws.NewCredentialsCache(creds),
		}, nil

	case CredentialSourceDefault:
		// Use default credential chain
	}

	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return aws.Config{}, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return cfg, nil
}

// ValidateCredentials verifies that the credentials are valid by calling STS GetCallerIdentity.
func ValidateCredentials(ctx context.Context, cfg aws.Config) (*CallerIdentity, error) {
	client := sts.NewFromConfig(cfg)
	result, err := client.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to validate credentials: %w", err)
	}

	return &CallerIdentity{
		AccountID: aws.ToString(result.Account),
		ARN:       aws.ToString(result.Arn),
		UserID:    aws.ToString(result.UserId),
	}, nil
}

// CallerIdentity contains AWS account identity information.
type CallerIdentity struct {
	AccountID string
	ARN       string
	UserID    string
}

// DetectCredentialSource attempts to detect the best credential source available.
func DetectCredentialSource() CredentialSource {
	// Check for explicit environment variables first
	if os.Getenv("AWS_ACCESS_KEY_ID") != "" && os.Getenv("AWS_SECRET_ACCESS_KEY") != "" {
		return CredentialSourceEnv
	}

	// Check for profile environment variable
	if os.Getenv("AWS_PROFILE") != "" {
		return CredentialSourceProfile
	}

	// Check for role ARN environment variable
	if os.Getenv("AWS_ROLE_ARN") != "" {
		return CredentialSourceRole
	}

	// Fall back to default chain
	return CredentialSourceDefault
}

// FromParseOptions creates a CredentialConfig from parser options.
func FromParseOptions(opts map[string]string, regions []string) *CredentialConfig {
	cfg := NewCredentialConfig()

	if profile, ok := opts["profile"]; ok && profile != "" {
		cfg.WithProfile(profile)
	}

	if accessKey, ok := opts["access_key_id"]; ok && accessKey != "" {
		secretKey := opts["secret_access_key"]
		sessionToken := opts["session_token"]
		cfg.WithStaticCredentials(accessKey, secretKey, sessionToken)
	}

	if roleARN, ok := opts["role_arn"]; ok && roleARN != "" {
		externalID := opts["external_id"]
		cfg.WithRole(roleARN, externalID)
	}

	if len(regions) > 0 {
		cfg.WithRegion(regions[0])
	}

	return cfg
}
