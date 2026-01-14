package aws

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/acm"
	"github.com/aws/aws-sdk-go-v2/service/apigateway"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cloudwatchtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/efs"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/kinesis"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/ses"
	sestypes "github.com/aws/aws-sdk-go-v2/service/ses/types"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/homeport/homeport/internal/domain/parser"
	"github.com/homeport/homeport/internal/domain/resource"
)

// APIParser discovers AWS infrastructure via API calls.
type APIParser struct {
	credConfig *CredentialConfig
	awsConfig  aws.Config
	identity   *CallerIdentity
}

// NewAPIParser creates a new AWS API parser.
func NewAPIParser() *APIParser {
	return &APIParser{
		credConfig: NewCredentialConfig(),
	}
}

// WithCredentials sets the credential configuration.
func (p *APIParser) WithCredentials(cfg *CredentialConfig) *APIParser {
	p.credConfig = cfg
	return p
}

// Provider returns the cloud provider.
func (p *APIParser) Provider() resource.Provider {
	return resource.ProviderAWS
}

// SupportedFormats returns the supported formats.
func (p *APIParser) SupportedFormats() []parser.Format {
	return []parser.Format{parser.FormatAPI}
}

// Validate checks if the parser can connect to AWS.
func (p *APIParser) Validate(path string) error {
	// For API parser, path is not used - we validate credentials
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg, err := p.credConfig.LoadConfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to load AWS config: %w", err)
	}

	identity, err := ValidateCredentials(ctx, cfg)
	if err != nil {
		return fmt.Errorf("invalid AWS credentials: %w", err)
	}

	p.awsConfig = cfg
	p.identity = identity
	return nil
}

// AutoDetect checks for AWS credentials availability.
func (p *APIParser) AutoDetect(path string) (bool, float64) {
	// Check if this is an API path for AWS
	if strings.HasPrefix(path, "api://aws") {
		return true, 1.0
	}

	// Check if we have AWS credentials available
	source := DetectCredentialSource()
	if source != CredentialSourceDefault {
		return true, 0.7
	}

	// Try to validate default credentials
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg, err := p.credConfig.LoadConfig(ctx)
	if err != nil {
		return false, 0
	}

	_, err = ValidateCredentials(ctx, cfg)
	if err != nil {
		return false, 0
	}

	return true, 0.6
}

// getAllRegions fetches all enabled AWS regions for the account.
func (p *APIParser) getAllRegions(ctx context.Context, cfg aws.Config) ([]string, error) {
	client := ec2.NewFromConfig(cfg)

	result, err := client.DescribeRegions(ctx, &ec2.DescribeRegionsInput{
		AllRegions: aws.Bool(false), // Only enabled regions
	})
	if err != nil {
		return nil, fmt.Errorf("failed to describe regions: %w", err)
	}

	var regions []string
	for _, r := range result.Regions {
		regions = append(regions, aws.ToString(r.RegionName))
	}

	return regions, nil
}

// Parse discovers AWS infrastructure via API.
func (p *APIParser) Parse(ctx context.Context, path string, opts *parser.ParseOptions) (*resource.Infrastructure, error) {
	// Helper to emit progress events
	emitProgress := func(event parser.ProgressEvent) {
		if opts != nil && opts.OnProgress != nil {
			opts.OnProgress(event)
		}
	}

	// Initialize credential config from options
	if opts != nil && opts.APICredentials != nil {
		p.credConfig = FromParseOptions(opts.APICredentials, opts.Regions)
	}

	// Set a default region for initial connection if not set
	if p.credConfig.Region == "" {
		p.credConfig.Region = "us-east-1"
	}

	emitProgress(parser.ProgressEvent{
		Step:    "init",
		Message: "Validating AWS credentials...",
	})

	// Load AWS configuration
	cfg, err := p.credConfig.LoadConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}
	p.awsConfig = cfg

	// Validate credentials
	identity, err := ValidateCredentials(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to validate credentials: %w", err)
	}
	p.identity = identity

	// Create infrastructure
	infra := resource.NewInfrastructure(resource.ProviderAWS)
	infra.Region = p.credConfig.Region
	infra.Metadata["account_id"] = identity.AccountID
	infra.Metadata["caller_arn"] = identity.ARN

	emitProgress(parser.ProgressEvent{
		Step:    "regions",
		Message: "Fetching available regions...",
	})

	// Determine regions to scan
	var regions []string
	if opts != nil && len(opts.Regions) > 0 {
		regions = opts.Regions
	} else {
		// No regions specified - scan all enabled regions
		allRegions, err := p.getAllRegions(ctx, cfg)
		if err != nil {
			// Fallback to default region if we can't list regions
			regions = []string{p.credConfig.Region}
		} else {
			regions = allRegions
		}
	}

	emitProgress(parser.ProgressEvent{
		Step:         "regions",
		Message:      fmt.Sprintf("Found %d regions to scan", len(regions)),
		TotalRegions: len(regions),
	})

	// Define services to scan
	services := []struct {
		name string
		scan func(context.Context, aws.Config, *resource.Infrastructure, *parser.ParseOptions) error
	}{
		{"EC2", p.scanEC2},
		{"S3", p.scanS3},
		{"RDS", p.scanRDS},
		{"RDSCluster", p.scanRDSCluster},
		{"Lambda", p.scanLambda},
		{"SQS", p.scanSQS},
		{"SNS", p.scanSNS},
		{"ElastiCache", p.scanElastiCache},
		{"ALB", p.scanALB},
		{"DynamoDB", p.scanDynamoDB},
		{"AutoScaling", p.scanASG},
		{"SecretsManager", p.scanSecretsManager},
		{"Route53", p.scanRoute53},
		{"ECS", p.scanECS},
		{"EKS", p.scanEKS},
		{"CloudFront", p.scanCloudFront},
		{"APIGateway", p.scanAPIGateway},
		{"EventBridge", p.scanEventBridge},
		{"Kinesis", p.scanKinesis},
		{"Cognito", p.scanCognito},
		{"IAM", p.scanIAM},
		{"ACM", p.scanACM},
		{"EBS", p.scanEBS},
		{"EFS", p.scanEFS},
		{"VPC", p.scanVPC},
		{"SES", p.scanSES},
		{"KMS", p.scanKMS},
		{"CloudWatchLogGroups", p.scanCloudWatchLogGroups},
		{"CloudWatchMetricAlarms", p.scanCloudWatchMetricAlarms},
		{"CloudWatchDashboards", p.scanCloudWatchDashboards},
	}

	totalServices := len(services)

	// Scan resources across all regions
	for regionIdx, region := range regions {
		// Create region-specific config
		regionCfg := cfg.Copy()
		regionCfg.Region = region

		for serviceIdx, svc := range services {
			emitProgress(parser.ProgressEvent{
				Step:           "scanning",
				Message:        fmt.Sprintf("Scanning %s in %s...", svc.name, region),
				Region:         region,
				Service:        svc.name,
				CurrentRegion:  regionIdx + 1,
				TotalRegions:   len(regions),
				CurrentService: serviceIdx + 1,
				TotalServices:  totalServices,
				ResourcesFound: len(infra.Resources),
			})

			if err := svc.scan(ctx, regionCfg, infra, opts); err != nil && opts != nil && !opts.IgnoreErrors {
				return nil, fmt.Errorf("failed to scan %s: %w", svc.name, err)
			}
		}
	}

	emitProgress(parser.ProgressEvent{
		Step:           "complete",
		Message:        fmt.Sprintf("Discovery complete: found %d resources", len(infra.Resources)),
		ResourcesFound: len(infra.Resources),
		CurrentRegion:  len(regions),
		TotalRegions:   len(regions),
	})

	return infra, nil
}

// scanEC2 discovers EC2 instances.
func (p *APIParser) scanEC2(ctx context.Context, cfg aws.Config, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeEC2Instance, opts) {
		return nil
	}

	client := ec2.NewFromConfig(cfg)

	paginator := ec2.NewDescribeInstancesPaginator(client, &ec2.DescribeInstancesInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to describe instances: %w", err)
		}

		for _, reservation := range page.Reservations {
			for _, instance := range reservation.Instances {
				// Skip terminated instances
				if instance.State != nil && instance.State.Name == ec2types.InstanceStateNameTerminated {
					continue
				}

				res := resource.NewAWSResource(
					aws.ToString(instance.InstanceId),
					p.getTagValue(instance.Tags, "Name"),
					resource.TypeEC2Instance,
				)
				res.Region = cfg.Region
				res.ARN = fmt.Sprintf("arn:aws:ec2:%s:%s:instance/%s", cfg.Region, p.identity.AccountID, aws.ToString(instance.InstanceId))

				// Config
				res.Config["instance_type"] = string(instance.InstanceType)
				res.Config["ami"] = aws.ToString(instance.ImageId)
				res.Config["key_name"] = aws.ToString(instance.KeyName)
				res.Config["availability_zone"] = aws.ToString(instance.Placement.AvailabilityZone)
				res.Config["vpc_id"] = aws.ToString(instance.VpcId)
				res.Config["subnet_id"] = aws.ToString(instance.SubnetId)
				res.Config["private_ip"] = aws.ToString(instance.PrivateIpAddress)
				res.Config["public_ip"] = aws.ToString(instance.PublicIpAddress)
				res.Config["state"] = string(instance.State.Name)

				// Security groups
				var sgIDs []string
				for _, sg := range instance.SecurityGroups {
					sgIDs = append(sgIDs, aws.ToString(sg.GroupId))
				}
				res.Config["security_groups"] = sgIDs

				// Block devices
				var volumes []map[string]interface{}
				for _, bd := range instance.BlockDeviceMappings {
					if bd.Ebs != nil {
						volumes = append(volumes, map[string]interface{}{
							"device_name": aws.ToString(bd.DeviceName),
							"volume_id":   aws.ToString(bd.Ebs.VolumeId),
						})
					}
				}
				res.Config["block_devices"] = volumes

				// Tags
				for _, tag := range instance.Tags {
					res.Tags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
				}

				infra.AddResource(res)
			}
		}
	}

	return nil
}

// scanS3 discovers S3 buckets.
func (p *APIParser) scanS3(ctx context.Context, cfg aws.Config, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeS3Bucket, opts) {
		return nil
	}

	// S3 is a global service, use the default region
	client := s3.NewFromConfig(cfg)

	result, err := client.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return fmt.Errorf("failed to list buckets: %w", err)
	}

	for _, bucket := range result.Buckets {
		bucketName := aws.ToString(bucket.Name)

		// Get bucket location to determine region
		locResult, err := client.GetBucketLocation(ctx, &s3.GetBucketLocationInput{
			Bucket: bucket.Name,
		})
		if err != nil {
			continue // Skip buckets we can't access
		}

		bucketRegion := string(locResult.LocationConstraint)
		if bucketRegion == "" {
			bucketRegion = "us-east-1" // Default region
		}

		// Only include buckets in the current region
		if bucketRegion != cfg.Region {
			continue
		}

		res := resource.NewAWSResource(bucketName, bucketName, resource.TypeS3Bucket)
		res.Region = bucketRegion
		res.ARN = fmt.Sprintf("arn:aws:s3:::%s", bucketName)
		res.CreatedAt = aws.ToTime(bucket.CreationDate)

		// Get versioning
		versioning, err := client.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{
			Bucket: bucket.Name,
		})
		if err == nil {
			res.Config["versioning_enabled"] = versioning.Status == "Enabled"
		}

		// Get encryption
		encryption, err := client.GetBucketEncryption(ctx, &s3.GetBucketEncryptionInput{
			Bucket: bucket.Name,
		})
		if err == nil && len(encryption.ServerSideEncryptionConfiguration.Rules) > 0 {
			res.Config["encryption"] = true
			res.Config["encryption_algorithm"] = string(encryption.ServerSideEncryptionConfiguration.Rules[0].ApplyServerSideEncryptionByDefault.SSEAlgorithm)
		}

		// Get tags
		tags, err := client.GetBucketTagging(ctx, &s3.GetBucketTaggingInput{
			Bucket: bucket.Name,
		})
		if err == nil {
			for _, tag := range tags.TagSet {
				res.Tags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
			}
		}

		infra.AddResource(res)
	}

	return nil
}

// scanRDS discovers RDS instances.
func (p *APIParser) scanRDS(ctx context.Context, cfg aws.Config, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeRDSInstance, opts) {
		return nil
	}

	client := rds.NewFromConfig(cfg)

	paginator := rds.NewDescribeDBInstancesPaginator(client, &rds.DescribeDBInstancesInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to describe DB instances: %w", err)
		}

		for _, db := range page.DBInstances {
			res := resource.NewAWSResource(
				aws.ToString(db.DBInstanceIdentifier),
				aws.ToString(db.DBInstanceIdentifier),
				resource.TypeRDSInstance,
			)
			res.Region = cfg.Region
			res.ARN = aws.ToString(db.DBInstanceArn)

			// Config
			res.Config["engine"] = aws.ToString(db.Engine)
			res.Config["engine_version"] = aws.ToString(db.EngineVersion)
			res.Config["instance_class"] = aws.ToString(db.DBInstanceClass)
			res.Config["allocated_storage"] = db.AllocatedStorage
			res.Config["storage_type"] = aws.ToString(db.StorageType)
			res.Config["multi_az"] = db.MultiAZ
			res.Config["publicly_accessible"] = db.PubliclyAccessible
			res.Config["storage_encrypted"] = db.StorageEncrypted
			res.Config["port"] = db.Endpoint.Port
			res.Config["database_name"] = aws.ToString(db.DBName)
			res.Config["master_username"] = aws.ToString(db.MasterUsername)

			// Track if this instance belongs to a cluster (Aurora)
			// Instances in a cluster share the cluster's password
			if db.DBClusterIdentifier != nil {
				res.Config["db_cluster_identifier"] = aws.ToString(db.DBClusterIdentifier)
			}

			// Master user secret (Secrets Manager integration)
			if db.MasterUserSecret != nil && db.MasterUserSecret.SecretArn != nil {
				res.Config["master_user_secret"] = map[string]interface{}{
					"secret_arn":    aws.ToString(db.MasterUserSecret.SecretArn),
					"secret_status": aws.ToString(db.MasterUserSecret.SecretStatus),
				}
				res.Config["master_user_secret_arn"] = aws.ToString(db.MasterUserSecret.SecretArn)
			}

			if db.Endpoint != nil {
				res.Config["endpoint"] = aws.ToString(db.Endpoint.Address)
			}

			// Security groups
			var sgIDs []string
			for _, sg := range db.VpcSecurityGroups {
				sgIDs = append(sgIDs, aws.ToString(sg.VpcSecurityGroupId))
			}
			res.Config["vpc_security_groups"] = sgIDs

			infra.AddResource(res)
		}
	}

	return nil
}

// scanRDSCluster discovers RDS Aurora clusters.
func (p *APIParser) scanRDSCluster(ctx context.Context, cfg aws.Config, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeRDSCluster, opts) {
		return nil
	}

	client := rds.NewFromConfig(cfg)

	paginator := rds.NewDescribeDBClustersPaginator(client, &rds.DescribeDBClustersInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to describe DB clusters: %w", err)
		}

		for _, cluster := range page.DBClusters {
			clusterID := aws.ToString(cluster.DBClusterIdentifier)

			res := resource.NewAWSResource(
				clusterID,
				clusterID,
				resource.TypeRDSCluster,
			)
			res.Region = cfg.Region
			res.ARN = aws.ToString(cluster.DBClusterArn)

			// Config
			res.Config["cluster_identifier"] = clusterID
			res.Config["engine"] = aws.ToString(cluster.Engine)
			res.Config["engine_version"] = aws.ToString(cluster.EngineVersion)
			res.Config["engine_mode"] = aws.ToString(cluster.EngineMode)
			res.Config["database_name"] = aws.ToString(cluster.DatabaseName)
			res.Config["master_username"] = aws.ToString(cluster.MasterUsername)
			res.Config["port"] = cluster.Port
			res.Config["status"] = aws.ToString(cluster.Status)

			// Endpoints
			res.Config["endpoint"] = aws.ToString(cluster.Endpoint)
			res.Config["reader_endpoint"] = aws.ToString(cluster.ReaderEndpoint)

			// High availability
			res.Config["multi_az"] = cluster.MultiAZ
			res.Config["availability_zones"] = cluster.AvailabilityZones

			// Storage
			res.Config["storage_encrypted"] = cluster.StorageEncrypted
			res.Config["allocated_storage"] = cluster.AllocatedStorage
			if cluster.KmsKeyId != nil {
				res.Config["kms_key_id"] = aws.ToString(cluster.KmsKeyId)
			}

			// Security
			res.Config["iam_database_authentication_enabled"] = cluster.IAMDatabaseAuthenticationEnabled
			res.Config["deletion_protection"] = cluster.DeletionProtection

			// VPC security groups
			var sgIDs []string
			for _, sg := range cluster.VpcSecurityGroups {
				sgIDs = append(sgIDs, aws.ToString(sg.VpcSecurityGroupId))
			}
			res.Config["vpc_security_groups"] = sgIDs

			// DB cluster members (instances)
			var members []map[string]interface{}
			for _, member := range cluster.DBClusterMembers {
				members = append(members, map[string]interface{}{
					"db_instance_identifier": aws.ToString(member.DBInstanceIdentifier),
					"is_cluster_writer":      member.IsClusterWriter,
					"promotion_tier":         member.PromotionTier,
				})
			}
			res.Config["cluster_members"] = members

			// Backup and maintenance
			res.Config["backup_retention_period"] = cluster.BackupRetentionPeriod
			res.Config["preferred_backup_window"] = aws.ToString(cluster.PreferredBackupWindow)
			res.Config["preferred_maintenance_window"] = aws.ToString(cluster.PreferredMaintenanceWindow)

			// Serverless v2 scaling configuration
			if cluster.ServerlessV2ScalingConfiguration != nil {
				res.Config["serverless_v2_scaling"] = map[string]interface{}{
					"min_capacity": cluster.ServerlessV2ScalingConfiguration.MinCapacity,
					"max_capacity": cluster.ServerlessV2ScalingConfiguration.MaxCapacity,
				}
			}

			// Global cluster info
			if cluster.GlobalWriteForwardingStatus != "" {
				res.Config["global_write_forwarding_status"] = string(cluster.GlobalWriteForwardingStatus)
			}

			// Master user secret (Secrets Manager integration)
			if cluster.MasterUserSecret != nil && cluster.MasterUserSecret.SecretArn != nil {
				res.Config["master_user_secret"] = map[string]interface{}{
					"secret_arn":    aws.ToString(cluster.MasterUserSecret.SecretArn),
					"secret_status": aws.ToString(cluster.MasterUserSecret.SecretStatus),
				}
				res.Config["master_user_secret_arn"] = aws.ToString(cluster.MasterUserSecret.SecretArn)
			}

			// Tags
			for _, tag := range cluster.TagList {
				res.Tags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanLambda discovers Lambda functions.
func (p *APIParser) scanLambda(ctx context.Context, cfg aws.Config, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeLambdaFunction, opts) {
		return nil
	}

	client := lambda.NewFromConfig(cfg)

	paginator := lambda.NewListFunctionsPaginator(client, &lambda.ListFunctionsInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list functions: %w", err)
		}

		for _, fn := range page.Functions {
			res := resource.NewAWSResource(
				aws.ToString(fn.FunctionName),
				aws.ToString(fn.FunctionName),
				resource.TypeLambdaFunction,
			)
			res.Region = cfg.Region
			res.ARN = aws.ToString(fn.FunctionArn)

			// Config
			res.Config["runtime"] = string(fn.Runtime)
			res.Config["handler"] = aws.ToString(fn.Handler)
			res.Config["memory_size"] = fn.MemorySize
			res.Config["timeout"] = fn.Timeout
			res.Config["code_size"] = fn.CodeSize
			res.Config["description"] = aws.ToString(fn.Description)
			res.Config["role"] = aws.ToString(fn.Role)

			// VPC config
			if fn.VpcConfig != nil {
				res.Config["vpc_subnet_ids"] = fn.VpcConfig.SubnetIds
				res.Config["vpc_security_group_ids"] = fn.VpcConfig.SecurityGroupIds
			}

			// Environment variables (excluding sensitive values)
			if fn.Environment != nil && !opts.IncludeSensitive {
				envKeys := make([]string, 0, len(fn.Environment.Variables))
				for k := range fn.Environment.Variables {
					envKeys = append(envKeys, k)
				}
				res.Config["environment_keys"] = envKeys
			} else if fn.Environment != nil && opts.IncludeSensitive {
				res.Config["environment"] = fn.Environment.Variables
			}

			// Layers
			if len(fn.Layers) > 0 {
				layers := make([]map[string]interface{}, 0, len(fn.Layers))
				for _, layer := range fn.Layers {
					layers = append(layers, map[string]interface{}{
						"arn":       aws.ToString(layer.Arn),
						"code_size": layer.CodeSize,
					})
				}
				res.Config["layers"] = layers
			}

			// Get function code location for migration
			fnDetails, err := client.GetFunction(ctx, &lambda.GetFunctionInput{
				FunctionName: fn.FunctionName,
			})
			if err == nil && fnDetails.Code != nil {
				res.Config["code_location"] = aws.ToString(fnDetails.Code.Location)
				res.Config["code_repository_type"] = aws.ToString(fnDetails.Code.RepositoryType)
			}

			// Get tags
			tagsResult, err := client.ListTags(ctx, &lambda.ListTagsInput{
				Resource: fn.FunctionArn,
			})
			if err == nil {
				for k, v := range tagsResult.Tags {
					res.Tags[k] = v
				}
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanSQS discovers SQS queues.
func (p *APIParser) scanSQS(ctx context.Context, cfg aws.Config, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeSQSQueue, opts) {
		return nil
	}

	client := sqs.NewFromConfig(cfg)

	result, err := client.ListQueues(ctx, &sqs.ListQueuesInput{})
	if err != nil {
		return fmt.Errorf("failed to list queues: %w", err)
	}

	for _, queueURL := range result.QueueUrls {
		// Get queue name from URL
		parts := strings.Split(queueURL, "/")
		queueName := parts[len(parts)-1]

		res := resource.NewAWSResource(queueName, queueName, resource.TypeSQSQueue)
		res.Region = cfg.Region
		res.Config["url"] = queueURL

		// Get queue attributes
		attrs, err := client.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
			QueueUrl: aws.String(queueURL),
			AttributeNames: []sqstypes.QueueAttributeName{
				sqstypes.QueueAttributeNameQueueArn,
				sqstypes.QueueAttributeNameVisibilityTimeout,
				sqstypes.QueueAttributeNameMaximumMessageSize,
				sqstypes.QueueAttributeNameMessageRetentionPeriod,
				sqstypes.QueueAttributeNameDelaySeconds,
				sqstypes.QueueAttributeNameReceiveMessageWaitTimeSeconds,
				sqstypes.QueueAttributeNameFifoQueue,
				sqstypes.QueueAttributeNameContentBasedDeduplication,
			},
		})
		if err == nil {
			res.ARN = attrs.Attributes["QueueArn"]
			res.Config["visibility_timeout"] = attrs.Attributes["VisibilityTimeout"]
			res.Config["max_message_size"] = attrs.Attributes["MaximumMessageSize"]
			res.Config["message_retention"] = attrs.Attributes["MessageRetentionPeriod"]
			res.Config["delay_seconds"] = attrs.Attributes["DelaySeconds"]
			res.Config["receive_wait_time"] = attrs.Attributes["ReceiveMessageWaitTimeSeconds"]
			res.Config["fifo_queue"] = attrs.Attributes["FifoQueue"] == "true"
			res.Config["content_based_deduplication"] = attrs.Attributes["ContentBasedDeduplication"] == "true"
		}

		// Get tags
		tagsResult, err := client.ListQueueTags(ctx, &sqs.ListQueueTagsInput{
			QueueUrl: aws.String(queueURL),
		})
		if err == nil {
			for k, v := range tagsResult.Tags {
				res.Tags[k] = v
			}
		}

		infra.AddResource(res)
	}

	return nil
}

// scanSNS discovers SNS topics.
func (p *APIParser) scanSNS(ctx context.Context, cfg aws.Config, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeSNSTopic, opts) {
		return nil
	}

	client := sns.NewFromConfig(cfg)

	paginator := sns.NewListTopicsPaginator(client, &sns.ListTopicsInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list topics: %w", err)
		}

		for _, topic := range page.Topics {
			topicARN := aws.ToString(topic.TopicArn)
			parts := strings.Split(topicARN, ":")
			topicName := parts[len(parts)-1]

			res := resource.NewAWSResource(topicName, topicName, resource.TypeSNSTopic)
			res.Region = cfg.Region
			res.ARN = topicARN

			// Get topic attributes
			attrs, err := client.GetTopicAttributes(ctx, &sns.GetTopicAttributesInput{
				TopicArn: topic.TopicArn,
			})
			if err == nil {
				res.Config["display_name"] = attrs.Attributes["DisplayName"]
				res.Config["fifo_topic"] = attrs.Attributes["FifoTopic"] == "true"
				res.Config["content_based_deduplication"] = attrs.Attributes["ContentBasedDeduplication"] == "true"
			}

			// Get tags
			tagsResult, err := client.ListTagsForResource(ctx, &sns.ListTagsForResourceInput{
				ResourceArn: topic.TopicArn,
			})
			if err == nil {
				for _, tag := range tagsResult.Tags {
					res.Tags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
				}
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanElastiCache discovers ElastiCache clusters.
func (p *APIParser) scanElastiCache(ctx context.Context, cfg aws.Config, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeElastiCache, opts) {
		return nil
	}

	client := elasticache.NewFromConfig(cfg)

	paginator := elasticache.NewDescribeCacheClustersPaginator(client, &elasticache.DescribeCacheClustersInput{
		ShowCacheNodeInfo: aws.Bool(true),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to describe cache clusters: %w", err)
		}

		for _, cluster := range page.CacheClusters {
			res := resource.NewAWSResource(
				aws.ToString(cluster.CacheClusterId),
				aws.ToString(cluster.CacheClusterId),
				resource.TypeElastiCache,
			)
			res.Region = cfg.Region
			res.ARN = aws.ToString(cluster.ARN)

			// Config
			res.Config["engine"] = aws.ToString(cluster.Engine)
			res.Config["engine_version"] = aws.ToString(cluster.EngineVersion)
			res.Config["cache_node_type"] = aws.ToString(cluster.CacheNodeType)
			res.Config["num_cache_nodes"] = cluster.NumCacheNodes

			if len(cluster.CacheNodes) > 0 {
				node := cluster.CacheNodes[0]
				if node.Endpoint != nil {
					res.Config["endpoint"] = aws.ToString(node.Endpoint.Address)
					res.Config["port"] = node.Endpoint.Port
				}
			}

			// Security groups
			var sgIDs []string
			for _, sg := range cluster.SecurityGroups {
				sgIDs = append(sgIDs, aws.ToString(sg.SecurityGroupId))
			}
			res.Config["security_groups"] = sgIDs

			infra.AddResource(res)
		}
	}

	return nil
}

// scanALB discovers Application Load Balancers.
func (p *APIParser) scanALB(ctx context.Context, cfg aws.Config, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeALB, opts) {
		return nil
	}

	client := elasticloadbalancingv2.NewFromConfig(cfg)

	paginator := elasticloadbalancingv2.NewDescribeLoadBalancersPaginator(client, &elasticloadbalancingv2.DescribeLoadBalancersInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to describe load balancers: %w", err)
		}

		for _, lb := range page.LoadBalancers {
			res := resource.NewAWSResource(
				aws.ToString(lb.LoadBalancerName),
				aws.ToString(lb.LoadBalancerName),
				resource.TypeALB,
			)
			res.Region = cfg.Region
			res.ARN = aws.ToString(lb.LoadBalancerArn)

			// Config
			res.Config["type"] = string(lb.Type)
			res.Config["scheme"] = string(lb.Scheme)
			res.Config["dns_name"] = aws.ToString(lb.DNSName)
			res.Config["vpc_id"] = aws.ToString(lb.VpcId)
			res.Config["state"] = string(lb.State.Code)

			// Availability zones
			var azs []string
			var subnets []string
			for _, az := range lb.AvailabilityZones {
				azs = append(azs, aws.ToString(az.ZoneName))
				subnets = append(subnets, aws.ToString(az.SubnetId))
			}
			res.Config["availability_zones"] = azs
			res.Config["subnets"] = subnets

			// Security groups
			res.Config["security_groups"] = lb.SecurityGroups

			// Get listeners
			listeners, err := client.DescribeListeners(ctx, &elasticloadbalancingv2.DescribeListenersInput{
				LoadBalancerArn: lb.LoadBalancerArn,
			})
			if err == nil {
				var listenerConfigs []map[string]interface{}
				for _, l := range listeners.Listeners {
					listenerConfigs = append(listenerConfigs, map[string]interface{}{
						"port":     l.Port,
						"protocol": string(l.Protocol),
					})
				}
				res.Config["listeners"] = listenerConfigs
			}

			// Get tags
			tagsResult, err := client.DescribeTags(ctx, &elasticloadbalancingv2.DescribeTagsInput{
				ResourceArns: []string{aws.ToString(lb.LoadBalancerArn)},
			})
			if err == nil && len(tagsResult.TagDescriptions) > 0 {
				for _, tag := range tagsResult.TagDescriptions[0].Tags {
					res.Tags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
				}
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanDynamoDB discovers DynamoDB tables.
func (p *APIParser) scanDynamoDB(ctx context.Context, cfg aws.Config, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeDynamoDBTable, opts) {
		return nil
	}

	client := dynamodb.NewFromConfig(cfg)

	paginator := dynamodb.NewListTablesPaginator(client, &dynamodb.ListTablesInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list tables: %w", err)
		}

		for _, tableName := range page.TableNames {
			// Get table details
			table, err := client.DescribeTable(ctx, &dynamodb.DescribeTableInput{
				TableName: aws.String(tableName),
			})
			if err != nil {
				continue
			}

			res := resource.NewAWSResource(tableName, tableName, resource.TypeDynamoDBTable)
			res.Region = cfg.Region
			res.ARN = aws.ToString(table.Table.TableArn)

			// Config (with nil checks for optional fields)
			if table.Table.TableClassSummary != nil {
				res.Config["table_class"] = string(table.Table.TableClassSummary.TableClass)
			}
			if table.Table.BillingModeSummary != nil {
				res.Config["billing_mode"] = string(table.Table.BillingModeSummary.BillingMode)
			}
			res.Config["item_count"] = table.Table.ItemCount
			res.Config["table_size_bytes"] = table.Table.TableSizeBytes

			// Key schema
			var keySchema []map[string]string
			for _, key := range table.Table.KeySchema {
				keySchema = append(keySchema, map[string]string{
					"attribute_name": aws.ToString(key.AttributeName),
					"key_type":       string(key.KeyType),
				})
			}
			res.Config["key_schema"] = keySchema

			// Attribute definitions
			var attributes []map[string]string
			for _, attr := range table.Table.AttributeDefinitions {
				attributes = append(attributes, map[string]string{
					"attribute_name": aws.ToString(attr.AttributeName),
					"attribute_type": string(attr.AttributeType),
				})
			}
			res.Config["attributes"] = attributes

			// Provisioned throughput
			if table.Table.ProvisionedThroughput != nil {
				res.Config["read_capacity"] = table.Table.ProvisionedThroughput.ReadCapacityUnits
				res.Config["write_capacity"] = table.Table.ProvisionedThroughput.WriteCapacityUnits
			}

			// Global secondary indexes
			var gsiConfigs []map[string]interface{}
			for _, gsi := range table.Table.GlobalSecondaryIndexes {
				gsiConfigs = append(gsiConfigs, map[string]interface{}{
					"index_name": aws.ToString(gsi.IndexName),
					"index_arn":  aws.ToString(gsi.IndexArn),
				})
			}
			res.Config["global_secondary_indexes"] = gsiConfigs

			// Get tags
			tagsResult, err := client.ListTagsOfResource(ctx, &dynamodb.ListTagsOfResourceInput{
				ResourceArn: table.Table.TableArn,
			})
			if err == nil {
				for _, tag := range tagsResult.Tags {
					res.Tags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
				}
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanASG discovers Auto Scaling Groups.
func (p *APIParser) scanASG(ctx context.Context, cfg aws.Config, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	client := autoscaling.NewFromConfig(cfg)

	paginator := autoscaling.NewDescribeAutoScalingGroupsPaginator(client, &autoscaling.DescribeAutoScalingGroupsInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to describe auto scaling groups: %w", err)
		}

		for _, asg := range page.AutoScalingGroups {
			asgName := aws.ToString(asg.AutoScalingGroupName)

			// Add dependency from ASG to EC2 instances
			for _, instance := range asg.Instances {
				instanceID := aws.ToString(instance.InstanceId)
				if existingRes, err := infra.GetResource(instanceID); err == nil {
					existingRes.Config["auto_scaling_group"] = asgName
				}
			}
		}
	}

	return nil
}

// scanSecretsManager discovers Secrets Manager secrets.
func (p *APIParser) scanSecretsManager(ctx context.Context, cfg aws.Config, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeSecretsManager, opts) {
		return nil
	}

	client := secretsmanager.NewFromConfig(cfg)

	paginator := secretsmanager.NewListSecretsPaginator(client, &secretsmanager.ListSecretsInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list secrets: %w", err)
		}

		for _, secret := range page.SecretList {
			secretName := aws.ToString(secret.Name)

			res := resource.NewAWSResource(secretName, secretName, resource.TypeSecretsManager)
			res.Region = cfg.Region
			res.ARN = aws.ToString(secret.ARN)

			// Config (metadata only, never include secret values)
			res.Config["description"] = aws.ToString(secret.Description)
			res.Config["kms_key_id"] = aws.ToString(secret.KmsKeyId)
			res.Config["rotation_enabled"] = aws.ToBool(secret.RotationEnabled)
			res.Config["last_changed_date"] = secret.LastChangedDate
			res.Config["last_accessed_date"] = secret.LastAccessedDate

			if aws.ToBool(secret.RotationEnabled) {
				res.Config["rotation_lambda_arn"] = aws.ToString(secret.RotationLambdaARN)
				if secret.RotationRules != nil {
					res.Config["rotation_days"] = secret.RotationRules.AutomaticallyAfterDays
				}
			}

			// Tags
			for _, tag := range secret.Tags {
				res.Tags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanRoute53 discovers Route53 hosted zones.
func (p *APIParser) scanRoute53(ctx context.Context, cfg aws.Config, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeRoute53Zone, opts) {
		return nil
	}

	// Route53 is a global service, only scan once from us-east-1
	if cfg.Region != "us-east-1" {
		return nil
	}

	client := route53.NewFromConfig(cfg)

	// List hosted zones with pagination
	var marker *string
	for {
		input := &route53.ListHostedZonesInput{
			MaxItems: aws.Int32(100),
		}
		if marker != nil {
			input.Marker = marker
		}

		result, err := client.ListHostedZones(ctx, input)
		if err != nil {
			return fmt.Errorf("failed to list hosted zones: %w", err)
		}

		for _, zone := range result.HostedZones {
			zoneID := aws.ToString(zone.Id)
			// Zone ID comes as /hostedzone/Z123... - extract just the ID
			zoneID = strings.TrimPrefix(zoneID, "/hostedzone/")

			zoneName := aws.ToString(zone.Name)
			// Remove trailing dot from zone name for display
			displayName := strings.TrimSuffix(zoneName, ".")

			res := resource.NewAWSResource(zoneID, displayName, resource.TypeRoute53Zone)
			res.Region = "global" // Route53 is a global service
			res.ARN = fmt.Sprintf("arn:aws:route53:::hostedzone/%s", zoneID)

			// Config
			res.Config["name"] = zoneName
			res.Config["comment"] = aws.ToString(zone.Config.Comment)
			res.Config["private_zone"] = zone.Config.PrivateZone
			res.Config["record_count"] = zone.ResourceRecordSetCount
			res.Config["caller_reference"] = aws.ToString(zone.CallerReference)

			// Get zone details for additional info
			zoneDetails, err := client.GetHostedZone(ctx, &route53.GetHostedZoneInput{
				Id: zone.Id,
			})
			if err == nil {
				// Add delegation set info if available
				if zoneDetails.DelegationSet != nil {
					res.Config["name_servers"] = zoneDetails.DelegationSet.NameServers
					if zoneDetails.DelegationSet.Id != nil {
						res.Config["delegation_set_id"] = aws.ToString(zoneDetails.DelegationSet.Id)
					}
				}

				// Add VPC associations for private zones
				if len(zoneDetails.VPCs) > 0 {
					var vpcs []map[string]string
					for _, vpc := range zoneDetails.VPCs {
						vpcs = append(vpcs, map[string]string{
							"vpc_id":     aws.ToString(vpc.VPCId),
							"vpc_region": string(vpc.VPCRegion),
						})
					}
					res.Config["vpcs"] = vpcs
				}
			}

			// Get tags for the hosted zone
			tagsResult, err := client.ListTagsForResource(ctx, &route53.ListTagsForResourceInput{
				ResourceId:   aws.String(zoneID),
				ResourceType: "hostedzone",
			})
			if err == nil && tagsResult.ResourceTagSet != nil {
				for _, tag := range tagsResult.ResourceTagSet.Tags {
					res.Tags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
				}
			}

			infra.AddResource(res)
		}

		// Check if there are more pages
		if !result.IsTruncated {
			break
		}
		marker = result.NextMarker
	}

	return nil
}

// scanECS discovers ECS services.
func (p *APIParser) scanECS(ctx context.Context, cfg aws.Config, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeECSService, opts) {
		return nil
	}

	client := ecs.NewFromConfig(cfg)

	// List all ECS clusters
	clusterPaginator := ecs.NewListClustersPaginator(client, &ecs.ListClustersInput{})
	for clusterPaginator.HasMorePages() {
		clusterPage, err := clusterPaginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list ECS clusters: %w", err)
		}

		if len(clusterPage.ClusterArns) == 0 {
			continue
		}

		// Describe clusters
		describeClustersOut, err := client.DescribeClusters(ctx, &ecs.DescribeClustersInput{
			Clusters: clusterPage.ClusterArns,
		})
		if err != nil {
			continue
		}

		for _, cluster := range describeClustersOut.Clusters {
			clusterName := aws.ToString(cluster.ClusterName)
			clusterArn := aws.ToString(cluster.ClusterArn)

			// List services in this cluster
			servicePaginator := ecs.NewListServicesPaginator(client, &ecs.ListServicesInput{
				Cluster: aws.String(clusterArn),
			})

			for servicePaginator.HasMorePages() {
				servicePage, err := servicePaginator.NextPage(ctx)
				if err != nil {
					continue
				}

				if len(servicePage.ServiceArns) == 0 {
					continue
				}

				// Describe services
				describeServicesOut, err := client.DescribeServices(ctx, &ecs.DescribeServicesInput{
					Cluster:  aws.String(clusterArn),
					Services: servicePage.ServiceArns,
				})
				if err != nil {
					continue
				}

				for _, svc := range describeServicesOut.Services {
					serviceName := aws.ToString(svc.ServiceName)

					res := resource.NewAWSResource(
						serviceName,
						serviceName,
						resource.TypeECSService,
					)
					res.Region = cfg.Region
					res.ARN = aws.ToString(svc.ServiceArn)

					// Config
					res.Config["cluster_name"] = clusterName
					res.Config["cluster_arn"] = clusterArn
					res.Config["task_definition"] = aws.ToString(svc.TaskDefinition)
					res.Config["desired_count"] = svc.DesiredCount
					res.Config["running_count"] = svc.RunningCount
					res.Config["pending_count"] = svc.PendingCount
					res.Config["launch_type"] = string(svc.LaunchType)
					res.Config["scheduling_strategy"] = string(svc.SchedulingStrategy)
					res.Config["status"] = aws.ToString(svc.Status)

					// Network configuration
					if svc.NetworkConfiguration != nil && svc.NetworkConfiguration.AwsvpcConfiguration != nil {
						vpcConfig := svc.NetworkConfiguration.AwsvpcConfiguration
						res.Config["subnets"] = vpcConfig.Subnets
						res.Config["security_groups"] = vpcConfig.SecurityGroups
						res.Config["assign_public_ip"] = string(vpcConfig.AssignPublicIp)
					}

					// Load balancers
					if len(svc.LoadBalancers) > 0 {
						var lbs []map[string]interface{}
						for _, lb := range svc.LoadBalancers {
							lbs = append(lbs, map[string]interface{}{
								"target_group_arn": aws.ToString(lb.TargetGroupArn),
								"container_name":   aws.ToString(lb.ContainerName),
								"container_port":   lb.ContainerPort,
							})
						}
						res.Config["load_balancers"] = lbs
					}

					// Get task definition details
					taskDefOut, err := client.DescribeTaskDefinition(ctx, &ecs.DescribeTaskDefinitionInput{
						TaskDefinition: svc.TaskDefinition,
					})
					if err == nil && taskDefOut.TaskDefinition != nil {
						taskDef := taskDefOut.TaskDefinition
						res.Config["cpu"] = aws.ToString(taskDef.Cpu)
						res.Config["memory"] = aws.ToString(taskDef.Memory)

						// Container definitions
						var containers []map[string]interface{}
						for _, container := range taskDef.ContainerDefinitions {
							containers = append(containers, map[string]interface{}{
								"name":   aws.ToString(container.Name),
								"image":  aws.ToString(container.Image),
								"cpu":    container.Cpu,
								"memory": container.Memory,
							})
						}
						res.Config["container_definitions"] = containers
					}

					// Tags
					for _, tag := range svc.Tags {
						res.Tags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
					}

					infra.AddResource(res)
				}
			}
		}
	}

	return nil
}

// scanEKS discovers EKS clusters.
func (p *APIParser) scanEKS(ctx context.Context, cfg aws.Config, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeEKSCluster, opts) {
		return nil
	}

	client := eks.NewFromConfig(cfg)

	// List all EKS clusters
	paginator := eks.NewListClustersPaginator(client, &eks.ListClustersInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list EKS clusters: %w", err)
		}

		for _, clusterName := range page.Clusters {
			// Describe cluster
			describeOut, err := client.DescribeCluster(ctx, &eks.DescribeClusterInput{
				Name: aws.String(clusterName),
			})
			if err != nil {
				continue
			}

			cluster := describeOut.Cluster
			res := resource.NewAWSResource(
				clusterName,
				clusterName,
				resource.TypeEKSCluster,
			)
			res.Region = cfg.Region
			res.ARN = aws.ToString(cluster.Arn)

			// Config
			res.Config["name"] = clusterName
			res.Config["version"] = aws.ToString(cluster.Version)
			res.Config["status"] = string(cluster.Status)
			res.Config["endpoint"] = aws.ToString(cluster.Endpoint)
			res.Config["role_arn"] = aws.ToString(cluster.RoleArn)
			res.Config["platform_version"] = aws.ToString(cluster.PlatformVersion)

			// VPC config
			if cluster.ResourcesVpcConfig != nil {
				vpcConfig := cluster.ResourcesVpcConfig
				res.Config["vpc_id"] = aws.ToString(vpcConfig.VpcId)
				res.Config["subnet_ids"] = vpcConfig.SubnetIds
				res.Config["security_group_ids"] = vpcConfig.SecurityGroupIds
				res.Config["cluster_security_group_id"] = aws.ToString(vpcConfig.ClusterSecurityGroupId)
				res.Config["endpoint_public_access"] = vpcConfig.EndpointPublicAccess
				res.Config["endpoint_private_access"] = vpcConfig.EndpointPrivateAccess
			}

			// Encryption config
			if len(cluster.EncryptionConfig) > 0 {
				var encryptionConfigs []map[string]interface{}
				for _, ec := range cluster.EncryptionConfig {
					encryptionConfigs = append(encryptionConfigs, map[string]interface{}{
						"resources": ec.Resources,
						"key_arn":   aws.ToString(ec.Provider.KeyArn),
					})
				}
				res.Config["encryption_config"] = encryptionConfigs
			}

			// Kubernetes network config
			if cluster.KubernetesNetworkConfig != nil {
				res.Config["service_ipv4_cidr"] = aws.ToString(cluster.KubernetesNetworkConfig.ServiceIpv4Cidr)
				res.Config["ip_family"] = string(cluster.KubernetesNetworkConfig.IpFamily)
			}

			// Logging
			if cluster.Logging != nil && cluster.Logging.ClusterLogging != nil {
				var enabledLogTypes []string
				for _, logSetup := range cluster.Logging.ClusterLogging {
					if aws.ToBool(logSetup.Enabled) {
						for _, logType := range logSetup.Types {
							enabledLogTypes = append(enabledLogTypes, string(logType))
						}
					}
				}
				res.Config["enabled_cluster_log_types"] = enabledLogTypes
			}

			// List node groups
			nodeGroupPaginator := eks.NewListNodegroupsPaginator(client, &eks.ListNodegroupsInput{
				ClusterName: aws.String(clusterName),
			})

			var nodeGroups []map[string]interface{}
			for nodeGroupPaginator.HasMorePages() {
				ngPage, err := nodeGroupPaginator.NextPage(ctx)
				if err != nil {
					break
				}

				for _, ngName := range ngPage.Nodegroups {
					ngOut, err := client.DescribeNodegroup(ctx, &eks.DescribeNodegroupInput{
						ClusterName:   aws.String(clusterName),
						NodegroupName: aws.String(ngName),
					})
					if err != nil {
						continue
					}

					ng := ngOut.Nodegroup
					nodeGroups = append(nodeGroups, map[string]interface{}{
						"name":           ngName,
						"status":         string(ng.Status),
						"capacity_type":  string(ng.CapacityType),
						"ami_type":       string(ng.AmiType),
						"instance_types": ng.InstanceTypes,
						"disk_size":      ng.DiskSize,
						"desired_size":   ng.ScalingConfig.DesiredSize,
						"min_size":       ng.ScalingConfig.MinSize,
						"max_size":       ng.ScalingConfig.MaxSize,
					})
				}
			}
			res.Config["node_groups"] = nodeGroups

			// Tags
			for k, v := range cluster.Tags {
				res.Tags[k] = v
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanCloudFront discovers CloudFront distributions.
func (p *APIParser) scanCloudFront(ctx context.Context, cfg aws.Config, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeCloudFront, opts) {
		return nil
	}

	// CloudFront is a global service, only scan once from us-east-1
	if cfg.Region != "us-east-1" {
		return nil
	}

	client := cloudfront.NewFromConfig(cfg)

	paginator := cloudfront.NewListDistributionsPaginator(client, &cloudfront.ListDistributionsInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list CloudFront distributions: %w", err)
		}

		if page.DistributionList == nil || page.DistributionList.Items == nil {
			continue
		}

		for _, dist := range page.DistributionList.Items {
			distID := aws.ToString(dist.Id)
			res := resource.NewAWSResource(
				distID,
				aws.ToString(dist.DomainName),
				resource.TypeCloudFront,
			)
			res.Region = "global"
			res.ARN = aws.ToString(dist.ARN)

			// Config
			res.Config["domain_name"] = aws.ToString(dist.DomainName)
			res.Config["enabled"] = dist.Enabled
			res.Config["status"] = aws.ToString(dist.Status)
			res.Config["price_class"] = string(dist.PriceClass)
			res.Config["http_version"] = string(dist.HttpVersion)
			res.Config["is_ipv6_enabled"] = dist.IsIPV6Enabled

			// Aliases (CNAMEs)
			if dist.Aliases != nil && dist.Aliases.Items != nil {
				res.Config["aliases"] = dist.Aliases.Items
			}

			// Origins
			if dist.Origins != nil && dist.Origins.Items != nil {
				var origins []map[string]interface{}
				for _, origin := range dist.Origins.Items {
					originConfig := map[string]interface{}{
						"id":          aws.ToString(origin.Id),
						"domain_name": aws.ToString(origin.DomainName),
					}
					if origin.OriginPath != nil {
						originConfig["origin_path"] = aws.ToString(origin.OriginPath)
					}
					if origin.S3OriginConfig != nil {
						originConfig["origin_type"] = "s3"
					}
					if origin.CustomOriginConfig != nil {
						originConfig["origin_type"] = "custom"
						originConfig["http_port"] = origin.CustomOriginConfig.HTTPPort
						originConfig["https_port"] = origin.CustomOriginConfig.HTTPSPort
						originConfig["origin_protocol_policy"] = string(origin.CustomOriginConfig.OriginProtocolPolicy)
					}
					origins = append(origins, originConfig)
				}
				res.Config["origins"] = origins
			}

			// Default cache behavior
			if dist.DefaultCacheBehavior != nil {
				dcb := dist.DefaultCacheBehavior
				res.Config["default_cache_behavior"] = map[string]interface{}{
					"target_origin_id":       aws.ToString(dcb.TargetOriginId),
					"viewer_protocol_policy": string(dcb.ViewerProtocolPolicy),
					"compress":               dcb.Compress,
					"cache_policy_id":        aws.ToString(dcb.CachePolicyId),
				}
			}

			// Viewer certificate
			if dist.ViewerCertificate != nil {
				vc := dist.ViewerCertificate
				certConfig := map[string]interface{}{
					"cloudfront_default_certificate": vc.CloudFrontDefaultCertificate,
					"minimum_protocol_version":       string(vc.MinimumProtocolVersion),
					"ssl_support_method":             string(vc.SSLSupportMethod),
				}
				if vc.ACMCertificateArn != nil {
					certConfig["acm_certificate_arn"] = aws.ToString(vc.ACMCertificateArn)
				}
				res.Config["viewer_certificate"] = certConfig
			}

			// Custom error responses
			if dist.CustomErrorResponses != nil && dist.CustomErrorResponses.Items != nil {
				var errorResponses []map[string]interface{}
				for _, er := range dist.CustomErrorResponses.Items {
					errorResponses = append(errorResponses, map[string]interface{}{
						"error_code":            er.ErrorCode,
						"response_code":         aws.ToString(er.ResponseCode),
						"response_page_path":    aws.ToString(er.ResponsePagePath),
						"error_caching_min_ttl": er.ErrorCachingMinTTL,
					})
				}
				res.Config["custom_error_responses"] = errorResponses
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanAPIGateway discovers API Gateway REST APIs.
func (p *APIParser) scanAPIGateway(ctx context.Context, cfg aws.Config, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeAPIGateway, opts) {
		return nil
	}

	client := apigateway.NewFromConfig(cfg)

	paginator := apigateway.NewGetRestApisPaginator(client, &apigateway.GetRestApisInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list REST APIs: %w", err)
		}

		for _, api := range page.Items {
			apiID := aws.ToString(api.Id)
			apiName := aws.ToString(api.Name)

			res := resource.NewAWSResource(
				apiID,
				apiName,
				resource.TypeAPIGateway,
			)
			res.Region = cfg.Region
			res.ARN = fmt.Sprintf("arn:aws:apigateway:%s::/restapis/%s", cfg.Region, apiID)

			// Config
			res.Config["name"] = apiName
			res.Config["description"] = aws.ToString(api.Description)
			res.Config["api_key_source"] = string(api.ApiKeySource)
			res.Config["created_date"] = api.CreatedDate

			// Endpoint configuration
			if api.EndpointConfiguration != nil {
				res.Config["endpoint_types"] = api.EndpointConfiguration.Types
				if api.EndpointConfiguration.VpcEndpointIds != nil {
					res.Config["vpc_endpoint_ids"] = api.EndpointConfiguration.VpcEndpointIds
				}
			}

			// Tags
			for k, v := range api.Tags {
				res.Tags[k] = v
			}

			// Get stages
			stagesOut, err := client.GetStages(ctx, &apigateway.GetStagesInput{
				RestApiId: api.Id,
			})
			if err == nil && stagesOut.Item != nil {
				var stages []map[string]interface{}
				for _, stage := range stagesOut.Item {
					stageConfig := map[string]interface{}{
						"stage_name":            aws.ToString(stage.StageName),
						"deployment_id":         aws.ToString(stage.DeploymentId),
						"description":           aws.ToString(stage.Description),
						"cache_cluster_enabled": stage.CacheClusterEnabled,
						"cache_cluster_size":    string(stage.CacheClusterSize),
					}
					if stage.Variables != nil {
						stageConfig["variables"] = stage.Variables
					}
					stages = append(stages, stageConfig)
				}
				res.Config["stages"] = stages
			}

			// Get resources (API paths)
			resourcesOut, err := client.GetResources(ctx, &apigateway.GetResourcesInput{
				RestApiId: api.Id,
			})
			if err == nil && resourcesOut.Items != nil {
				var resources []map[string]interface{}
				for _, r := range resourcesOut.Items {
					resourceConfig := map[string]interface{}{
						"id":   aws.ToString(r.Id),
						"path": aws.ToString(r.Path),
					}
					if r.ResourceMethods != nil {
						var methods []string
						for method := range r.ResourceMethods {
							methods = append(methods, method)
						}
						resourceConfig["methods"] = methods
					}
					resources = append(resources, resourceConfig)
				}
				res.Config["resources"] = resources
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanEventBridge discovers EventBridge rules.
func (p *APIParser) scanEventBridge(ctx context.Context, cfg aws.Config, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeEventBridge, opts) {
		return nil
	}

	client := eventbridge.NewFromConfig(cfg)

	// List all event buses
	busesOut, err := client.ListEventBuses(ctx, &eventbridge.ListEventBusesInput{})
	if err != nil {
		return fmt.Errorf("failed to list event buses: %w", err)
	}

	for _, bus := range busesOut.EventBuses {
		busName := aws.ToString(bus.Name)

		// List rules for this event bus with manual pagination
		var nextToken *string
		for {
			rulesOut, err := client.ListRules(ctx, &eventbridge.ListRulesInput{
				EventBusName: bus.Name,
				NextToken:    nextToken,
			})
			if err != nil {
				break
			}

			for _, rule := range rulesOut.Rules {
				ruleName := aws.ToString(rule.Name)

				res := resource.NewAWSResource(
					ruleName,
					ruleName,
					resource.TypeEventBridge,
				)
				res.Region = cfg.Region
				res.ARN = aws.ToString(rule.Arn)

				// Config
				res.Config["name"] = ruleName
				res.Config["event_bus_name"] = busName
				res.Config["state"] = string(rule.State)
				res.Config["description"] = aws.ToString(rule.Description)

				if rule.ScheduleExpression != nil {
					res.Config["schedule_expression"] = aws.ToString(rule.ScheduleExpression)
				}

				if rule.EventPattern != nil {
					res.Config["event_pattern"] = aws.ToString(rule.EventPattern)
				}

				// List targets for this rule
				targetsOut, err := client.ListTargetsByRule(ctx, &eventbridge.ListTargetsByRuleInput{
					Rule:         rule.Name,
					EventBusName: bus.Name,
				})
				if err == nil && targetsOut.Targets != nil {
					var targets []map[string]interface{}
					for _, target := range targetsOut.Targets {
						targetConfig := map[string]interface{}{
							"id":  aws.ToString(target.Id),
							"arn": aws.ToString(target.Arn),
						}
						if target.RoleArn != nil {
							targetConfig["role_arn"] = aws.ToString(target.RoleArn)
						}
						if target.Input != nil {
							targetConfig["input"] = aws.ToString(target.Input)
						}
						if target.InputPath != nil {
							targetConfig["input_path"] = aws.ToString(target.InputPath)
						}
						targets = append(targets, targetConfig)
					}
					res.Config["targets"] = targets
				}

				infra.AddResource(res)
			}

			// Check for more pages
			if rulesOut.NextToken == nil {
				break
			}
			nextToken = rulesOut.NextToken
		}
	}

	return nil
}

// scanKinesis discovers Kinesis data streams.
func (p *APIParser) scanKinesis(ctx context.Context, cfg aws.Config, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeKinesis, opts) {
		return nil
	}

	client := kinesis.NewFromConfig(cfg)

	paginator := kinesis.NewListStreamsPaginator(client, &kinesis.ListStreamsInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list Kinesis streams: %w", err)
		}

		for _, streamSummary := range page.StreamSummaries {
			streamName := aws.ToString(streamSummary.StreamName)

			res := resource.NewAWSResource(
				streamName,
				streamName,
				resource.TypeKinesis,
			)
			res.Region = cfg.Region
			res.ARN = aws.ToString(streamSummary.StreamARN)

			// Config from summary
			res.Config["stream_name"] = streamName
			res.Config["stream_status"] = string(streamSummary.StreamStatus)
			res.Config["stream_mode"] = string(streamSummary.StreamModeDetails.StreamMode)
			res.Config["stream_creation_timestamp"] = streamSummary.StreamCreationTimestamp

			// Get detailed stream description
			describeOut, err := client.DescribeStreamSummary(ctx, &kinesis.DescribeStreamSummaryInput{
				StreamName: aws.String(streamName),
			})
			if err == nil && describeOut.StreamDescriptionSummary != nil {
				summary := describeOut.StreamDescriptionSummary
				res.Config["open_shard_count"] = summary.OpenShardCount
				res.Config["retention_period_hours"] = summary.RetentionPeriodHours
				res.Config["encryption_type"] = string(summary.EncryptionType)

				if summary.KeyId != nil {
					res.Config["key_id"] = aws.ToString(summary.KeyId)
				}

				res.Config["consumer_count"] = summary.ConsumerCount
			}

			// Get tags
			tagsOut, err := client.ListTagsForStream(ctx, &kinesis.ListTagsForStreamInput{
				StreamName: aws.String(streamName),
			})
			if err == nil {
				for _, tag := range tagsOut.Tags {
					res.Tags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
				}
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanCognito discovers Cognito User Pools.
func (p *APIParser) scanCognito(ctx context.Context, cfg aws.Config, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeCognitoPool, opts) {
		return nil
	}

	client := cognitoidentityprovider.NewFromConfig(cfg)

	paginator := cognitoidentityprovider.NewListUserPoolsPaginator(client, &cognitoidentityprovider.ListUserPoolsInput{
		MaxResults: aws.Int32(60),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list user pools: %w", err)
		}

		for _, pool := range page.UserPools {
			poolID := aws.ToString(pool.Id)
			poolName := aws.ToString(pool.Name)

			// Get detailed pool information
			describeOut, err := client.DescribeUserPool(ctx, &cognitoidentityprovider.DescribeUserPoolInput{
				UserPoolId: pool.Id,
			})
			if err != nil {
				continue
			}

			userPool := describeOut.UserPool
			res := resource.NewAWSResource(
				poolID,
				poolName,
				resource.TypeCognitoPool,
			)
			res.Region = cfg.Region
			res.ARN = aws.ToString(userPool.Arn)
			res.CreatedAt = aws.ToTime(userPool.CreationDate)

			// Config
			res.Config["name"] = poolName
			res.Config["user_pool_id"] = poolID
			res.Config["status"] = string(userPool.Status)
			res.Config["domain"] = aws.ToString(userPool.Domain)
			res.Config["custom_domain"] = aws.ToString(userPool.CustomDomain)
			res.Config["estimated_number_of_users"] = userPool.EstimatedNumberOfUsers

			// MFA configuration
			res.Config["mfa_configuration"] = string(userPool.MfaConfiguration)

			// Password policy
			if userPool.Policies != nil && userPool.Policies.PasswordPolicy != nil {
				pp := userPool.Policies.PasswordPolicy
				res.Config["password_policy"] = map[string]interface{}{
					"minimum_length":    pp.MinimumLength,
					"require_lowercase": pp.RequireLowercase,
					"require_uppercase": pp.RequireUppercase,
					"require_numbers":   pp.RequireNumbers,
					"require_symbols":   pp.RequireSymbols,
				}
			}

			// Auto verified attributes
			res.Config["auto_verified_attributes"] = userPool.AutoVerifiedAttributes

			// Schema attributes
			if userPool.SchemaAttributes != nil {
				var schema []map[string]interface{}
				for _, attr := range userPool.SchemaAttributes {
					schema = append(schema, map[string]interface{}{
						"name":           aws.ToString(attr.Name),
						"attribute_type": string(attr.AttributeDataType),
						"required":       attr.Required,
						"mutable":        attr.Mutable,
					})
				}
				res.Config["schema_attributes"] = schema
			}

			// List app clients
			clientsOut, err := client.ListUserPoolClients(ctx, &cognitoidentityprovider.ListUserPoolClientsInput{
				UserPoolId: pool.Id,
			})
			if err == nil && clientsOut.UserPoolClients != nil {
				var clients []map[string]interface{}
				for _, c := range clientsOut.UserPoolClients {
					clients = append(clients, map[string]interface{}{
						"client_id":   aws.ToString(c.ClientId),
						"client_name": aws.ToString(c.ClientName),
					})
				}
				res.Config["app_clients"] = clients
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanIAM discovers IAM roles.
func (p *APIParser) scanIAM(ctx context.Context, cfg aws.Config, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeIAMRole, opts) {
		return nil
	}

	// IAM is a global service, only scan once from us-east-1
	if cfg.Region != "us-east-1" {
		return nil
	}

	client := iam.NewFromConfig(cfg)

	paginator := iam.NewListRolesPaginator(client, &iam.ListRolesInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list IAM roles: %w", err)
		}

		for _, role := range page.Roles {
			roleName := aws.ToString(role.RoleName)

			res := resource.NewAWSResource(
				roleName,
				roleName,
				resource.TypeIAMRole,
			)
			res.Region = "global"
			res.ARN = aws.ToString(role.Arn)
			res.CreatedAt = aws.ToTime(role.CreateDate)

			// Config
			res.Config["name"] = roleName
			res.Config["path"] = aws.ToString(role.Path)
			res.Config["description"] = aws.ToString(role.Description)
			res.Config["max_session_duration"] = role.MaxSessionDuration

			// Assume role policy document
			if role.AssumeRolePolicyDocument != nil {
				res.Config["assume_role_policy_document"] = aws.ToString(role.AssumeRolePolicyDocument)
			}

			// Get attached policies
			attachedPoliciesOut, err := client.ListAttachedRolePolicies(ctx, &iam.ListAttachedRolePoliciesInput{
				RoleName: role.RoleName,
			})
			if err == nil {
				var attachedPolicies []map[string]string
				for _, policy := range attachedPoliciesOut.AttachedPolicies {
					attachedPolicies = append(attachedPolicies, map[string]string{
						"policy_name": aws.ToString(policy.PolicyName),
						"policy_arn":  aws.ToString(policy.PolicyArn),
					})
				}
				res.Config["attached_policies"] = attachedPolicies
			}

			// Get inline policies
			inlinePoliciesOut, err := client.ListRolePolicies(ctx, &iam.ListRolePoliciesInput{
				RoleName: role.RoleName,
			})
			if err == nil {
				res.Config["inline_policy_names"] = inlinePoliciesOut.PolicyNames
			}

			// Tags
			tagsOut, err := client.ListRoleTags(ctx, &iam.ListRoleTagsInput{
				RoleName: role.RoleName,
			})
			if err == nil {
				for _, tag := range tagsOut.Tags {
					res.Tags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
				}
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanACM discovers ACM certificates.
func (p *APIParser) scanACM(ctx context.Context, cfg aws.Config, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeACMCertificate, opts) {
		return nil
	}

	client := acm.NewFromConfig(cfg)

	paginator := acm.NewListCertificatesPaginator(client, &acm.ListCertificatesInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list certificates: %w", err)
		}

		for _, cert := range page.CertificateSummaryList {
			certARN := aws.ToString(cert.CertificateArn)
			domainName := aws.ToString(cert.DomainName)

			// Get certificate details
			describeOut, err := client.DescribeCertificate(ctx, &acm.DescribeCertificateInput{
				CertificateArn: cert.CertificateArn,
			})
			if err != nil {
				continue
			}

			certDetail := describeOut.Certificate
			res := resource.NewAWSResource(
				certARN,
				domainName,
				resource.TypeACMCertificate,
			)
			res.Region = cfg.Region
			res.ARN = certARN
			res.CreatedAt = aws.ToTime(certDetail.CreatedAt)

			// Config
			res.Config["domain_name"] = domainName
			res.Config["status"] = string(certDetail.Status)
			res.Config["type"] = string(certDetail.Type)
			res.Config["key_algorithm"] = string(certDetail.KeyAlgorithm)
			res.Config["issuer"] = aws.ToString(certDetail.Issuer)
			res.Config["serial"] = aws.ToString(certDetail.Serial)
			res.Config["subject"] = aws.ToString(certDetail.Subject)

			// Subject alternative names
			res.Config["subject_alternative_names"] = certDetail.SubjectAlternativeNames

			// Validity dates
			if certDetail.NotBefore != nil {
				res.Config["not_before"] = certDetail.NotBefore
			}
			if certDetail.NotAfter != nil {
				res.Config["not_after"] = certDetail.NotAfter
			}

			// Renewal eligibility
			res.Config["renewal_eligibility"] = string(certDetail.RenewalEligibility)

			// In use by
			res.Config["in_use_by"] = certDetail.InUseBy

			// Domain validation options
			if certDetail.DomainValidationOptions != nil {
				var validations []map[string]interface{}
				for _, dv := range certDetail.DomainValidationOptions {
					validations = append(validations, map[string]interface{}{
						"domain_name":       aws.ToString(dv.DomainName),
						"validation_status": string(dv.ValidationStatus),
						"validation_method": string(dv.ValidationMethod),
					})
				}
				res.Config["domain_validation_options"] = validations
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanEBS discovers EBS volumes.
func (p *APIParser) scanEBS(ctx context.Context, cfg aws.Config, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeEBSVolume, opts) {
		return nil
	}

	client := ec2.NewFromConfig(cfg)

	paginator := ec2.NewDescribeVolumesPaginator(client, &ec2.DescribeVolumesInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to describe EBS volumes: %w", err)
		}

		for _, volume := range page.Volumes {
			volumeID := aws.ToString(volume.VolumeId)
			volumeName := p.getTagValue(volume.Tags, "Name")
			if volumeName == "" {
				volumeName = volumeID
			}

			res := resource.NewAWSResource(
				volumeID,
				volumeName,
				resource.TypeEBSVolume,
			)
			res.Region = cfg.Region
			res.ARN = fmt.Sprintf("arn:aws:ec2:%s:%s:volume/%s", cfg.Region, p.identity.AccountID, volumeID)
			res.CreatedAt = aws.ToTime(volume.CreateTime)

			// Config
			res.Config["volume_id"] = volumeID
			res.Config["volume_type"] = string(volume.VolumeType)
			res.Config["size"] = volume.Size
			res.Config["iops"] = volume.Iops
			res.Config["throughput"] = volume.Throughput
			res.Config["encrypted"] = volume.Encrypted
			res.Config["availability_zone"] = aws.ToString(volume.AvailabilityZone)
			res.Config["state"] = string(volume.State)
			res.Config["multi_attach_enabled"] = volume.MultiAttachEnabled

			if volume.KmsKeyId != nil {
				res.Config["kms_key_id"] = aws.ToString(volume.KmsKeyId)
			}

			if volume.SnapshotId != nil && *volume.SnapshotId != "" {
				res.Config["snapshot_id"] = aws.ToString(volume.SnapshotId)
			}

			// Attachments
			if len(volume.Attachments) > 0 {
				var attachments []map[string]interface{}
				for _, attachment := range volume.Attachments {
					attachments = append(attachments, map[string]interface{}{
						"instance_id":           aws.ToString(attachment.InstanceId),
						"device":                aws.ToString(attachment.Device),
						"state":                 string(attachment.State),
						"delete_on_termination": attachment.DeleteOnTermination,
					})
				}
				res.Config["attachments"] = attachments
			}

			// Tags
			for _, tag := range volume.Tags {
				res.Tags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanEFS discovers EFS file systems.
func (p *APIParser) scanEFS(ctx context.Context, cfg aws.Config, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeEFSVolume, opts) {
		return nil
	}

	client := efs.NewFromConfig(cfg)

	paginator := efs.NewDescribeFileSystemsPaginator(client, &efs.DescribeFileSystemsInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to describe EFS file systems: %w", err)
		}

		for _, fs := range page.FileSystems {
			fsID := aws.ToString(fs.FileSystemId)
			fsName := aws.ToString(fs.Name)
			if fsName == "" {
				fsName = fsID
			}

			res := resource.NewAWSResource(
				fsID,
				fsName,
				resource.TypeEFSVolume,
			)
			res.Region = cfg.Region
			res.ARN = aws.ToString(fs.FileSystemArn)
			res.CreatedAt = aws.ToTime(fs.CreationTime)

			// Config
			res.Config["file_system_id"] = fsID
			res.Config["performance_mode"] = string(fs.PerformanceMode)
			res.Config["throughput_mode"] = string(fs.ThroughputMode)
			res.Config["encrypted"] = fs.Encrypted
			res.Config["life_cycle_state"] = string(fs.LifeCycleState)
			res.Config["size_in_bytes"] = fs.SizeInBytes.Value
			res.Config["number_of_mount_targets"] = fs.NumberOfMountTargets

			if fs.ProvisionedThroughputInMibps != nil {
				res.Config["provisioned_throughput_mibps"] = *fs.ProvisionedThroughputInMibps
			}

			if fs.KmsKeyId != nil {
				res.Config["kms_key_id"] = aws.ToString(fs.KmsKeyId)
			}

			if fs.AvailabilityZoneName != nil {
				res.Config["availability_zone"] = aws.ToString(fs.AvailabilityZoneName)
			}

			// Get mount targets
			mountTargetsOut, err := client.DescribeMountTargets(ctx, &efs.DescribeMountTargetsInput{
				FileSystemId: fs.FileSystemId,
			})
			if err == nil && mountTargetsOut.MountTargets != nil {
				var mountTargets []map[string]interface{}
				for _, mt := range mountTargetsOut.MountTargets {
					mountTargets = append(mountTargets, map[string]interface{}{
						"mount_target_id":      aws.ToString(mt.MountTargetId),
						"ip_address":           aws.ToString(mt.IpAddress),
						"subnet_id":            aws.ToString(mt.SubnetId),
						"availability_zone_id": aws.ToString(mt.AvailabilityZoneId),
						"life_cycle_state":     string(mt.LifeCycleState),
					})
				}
				res.Config["mount_targets"] = mountTargets
			}

			// Tags
			for _, tag := range fs.Tags {
				res.Tags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanVPC discovers VPCs with their subnets, route tables, and security groups.
func (p *APIParser) scanVPC(ctx context.Context, cfg aws.Config, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeVPC, opts) {
		return nil
	}

	client := ec2.NewFromConfig(cfg)

	// Describe VPCs
	vpcsOut, err := client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{})
	if err != nil {
		return fmt.Errorf("failed to describe VPCs: %w", err)
	}

	for _, vpc := range vpcsOut.Vpcs {
		vpcID := aws.ToString(vpc.VpcId)
		vpcName := p.getTagValue(vpc.Tags, "Name")
		if vpcName == "" {
			vpcName = vpcID
		}

		res := resource.NewAWSResource(
			vpcID,
			vpcName,
			resource.TypeVPC,
		)
		res.Region = cfg.Region
		res.ARN = fmt.Sprintf("arn:aws:ec2:%s:%s:vpc/%s", cfg.Region, p.identity.AccountID, vpcID)

		// Config
		res.Config["vpc_id"] = vpcID
		res.Config["cidr_block"] = aws.ToString(vpc.CidrBlock)
		res.Config["state"] = string(vpc.State)
		res.Config["is_default"] = vpc.IsDefault
		res.Config["instance_tenancy"] = string(vpc.InstanceTenancy)

		// DNS settings
		// Get VPC attributes for DNS support
		dnsSupport, err := client.DescribeVpcAttribute(ctx, &ec2.DescribeVpcAttributeInput{
			VpcId:     vpc.VpcId,
			Attribute: ec2types.VpcAttributeNameEnableDnsSupport,
		})
		if err == nil && dnsSupport.EnableDnsSupport != nil {
			res.Config["enable_dns_support"] = dnsSupport.EnableDnsSupport.Value
		}

		dnsHostnames, err := client.DescribeVpcAttribute(ctx, &ec2.DescribeVpcAttributeInput{
			VpcId:     vpc.VpcId,
			Attribute: ec2types.VpcAttributeNameEnableDnsHostnames,
		})
		if err == nil && dnsHostnames.EnableDnsHostnames != nil {
			res.Config["enable_dns_hostnames"] = dnsHostnames.EnableDnsHostnames.Value
		}

		// Secondary CIDR blocks
		if vpc.CidrBlockAssociationSet != nil {
			var cidrBlocks []map[string]interface{}
			for _, cidr := range vpc.CidrBlockAssociationSet {
				cidrBlocks = append(cidrBlocks, map[string]interface{}{
					"cidr_block": aws.ToString(cidr.CidrBlock),
					"state":      string(cidr.CidrBlockState.State),
				})
			}
			res.Config["cidr_block_associations"] = cidrBlocks
		}

		// IPv6 CIDR blocks
		if vpc.Ipv6CidrBlockAssociationSet != nil {
			var ipv6Cidrs []map[string]interface{}
			for _, cidr := range vpc.Ipv6CidrBlockAssociationSet {
				ipv6Cidrs = append(ipv6Cidrs, map[string]interface{}{
					"ipv6_cidr_block": aws.ToString(cidr.Ipv6CidrBlock),
					"state":           string(cidr.Ipv6CidrBlockState.State),
				})
			}
			res.Config["ipv6_cidr_block_associations"] = ipv6Cidrs
		}

		// Get Subnets
		subnetsOut, err := client.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{
			Filters: []ec2types.Filter{
				{
					Name:   aws.String("vpc-id"),
					Values: []string{vpcID},
				},
			},
		})
		if err == nil {
			var subnets []map[string]interface{}
			for _, subnet := range subnetsOut.Subnets {
				subnetName := p.getTagValue(subnet.Tags, "Name")
				if subnetName == "" {
					subnetName = aws.ToString(subnet.SubnetId)
				}
				subnets = append(subnets, map[string]interface{}{
					"subnet_id":                  aws.ToString(subnet.SubnetId),
					"name":                       subnetName,
					"cidr_block":                 aws.ToString(subnet.CidrBlock),
					"availability_zone":          aws.ToString(subnet.AvailabilityZone),
					"available_ip_address_count": subnet.AvailableIpAddressCount,
					"map_public_ip_on_launch":    subnet.MapPublicIpOnLaunch,
					"default_for_az":             subnet.DefaultForAz,
				})
			}
			res.Config["subnets"] = subnets
		}

		// Get Route Tables
		routeTablesOut, err := client.DescribeRouteTables(ctx, &ec2.DescribeRouteTablesInput{
			Filters: []ec2types.Filter{
				{
					Name:   aws.String("vpc-id"),
					Values: []string{vpcID},
				},
			},
		})
		if err == nil {
			var routeTables []map[string]interface{}
			for _, rt := range routeTablesOut.RouteTables {
				rtName := p.getTagValue(rt.Tags, "Name")
				if rtName == "" {
					rtName = aws.ToString(rt.RouteTableId)
				}

				var routes []map[string]interface{}
				for _, route := range rt.Routes {
					routes = append(routes, map[string]interface{}{
						"destination_cidr_block": aws.ToString(route.DestinationCidrBlock),
						"gateway_id":             aws.ToString(route.GatewayId),
						"nat_gateway_id":         aws.ToString(route.NatGatewayId),
						"state":                  string(route.State),
					})
				}

				var associations []string
				for _, assoc := range rt.Associations {
					if assoc.SubnetId != nil {
						associations = append(associations, aws.ToString(assoc.SubnetId))
					}
				}

				routeTables = append(routeTables, map[string]interface{}{
					"route_table_id":     aws.ToString(rt.RouteTableId),
					"name":               rtName,
					"is_main":            len(rt.Associations) > 0 && rt.Associations[0].Main != nil && *rt.Associations[0].Main,
					"routes":             routes,
					"associated_subnets": associations,
				})
			}
			res.Config["route_tables"] = routeTables
		}

		// Get Security Groups
		sgsOut, err := client.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
			Filters: []ec2types.Filter{
				{
					Name:   aws.String("vpc-id"),
					Values: []string{vpcID},
				},
			},
		})
		if err == nil {
			var securityGroups []map[string]interface{}
			for _, sg := range sgsOut.SecurityGroups {
				sgName := aws.ToString(sg.GroupName)

				var ingressRules []map[string]interface{}
				for _, rule := range sg.IpPermissions {
					ingressRules = append(ingressRules, map[string]interface{}{
						"protocol":  aws.ToString(rule.IpProtocol),
						"from_port": rule.FromPort,
						"to_port":   rule.ToPort,
					})
				}

				var egressRules []map[string]interface{}
				for _, rule := range sg.IpPermissionsEgress {
					egressRules = append(egressRules, map[string]interface{}{
						"protocol":  aws.ToString(rule.IpProtocol),
						"from_port": rule.FromPort,
						"to_port":   rule.ToPort,
					})
				}

				securityGroups = append(securityGroups, map[string]interface{}{
					"group_id":      aws.ToString(sg.GroupId),
					"group_name":    sgName,
					"description":   aws.ToString(sg.Description),
					"ingress_rules": ingressRules,
					"egress_rules":  egressRules,
				})
			}
			res.Config["security_groups"] = securityGroups
		}

		// Tags
		for _, tag := range vpc.Tags {
			res.Tags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
		}

		infra.AddResource(res)
	}

	return nil
}

// scanSES discovers SES identities (email and domain).
func (p *APIParser) scanSES(ctx context.Context, cfg aws.Config, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeSESIdentity, opts) {
		return nil
	}

	client := ses.NewFromConfig(cfg)

	// List all identities (both email and domain)
	var nextToken *string
	for {
		input := &ses.ListIdentitiesInput{
			MaxItems: aws.Int32(100),
		}
		if nextToken != nil {
			input.NextToken = nextToken
		}

		result, err := client.ListIdentities(ctx, input)
		if err != nil {
			return fmt.Errorf("failed to list SES identities: %w", err)
		}

		if len(result.Identities) == 0 {
			break
		}

		// Get verification attributes for all identities
		verifyResult, err := client.GetIdentityVerificationAttributes(ctx, &ses.GetIdentityVerificationAttributesInput{
			Identities: result.Identities,
		})
		if err != nil {
			return fmt.Errorf("failed to get identity verification attributes: %w", err)
		}

		// Get DKIM attributes for all identities
		dkimResult, err := client.GetIdentityDkimAttributes(ctx, &ses.GetIdentityDkimAttributesInput{
			Identities: result.Identities,
		})
		if err != nil {
			// Don't fail if DKIM attributes can't be fetched
			dkimResult = &ses.GetIdentityDkimAttributesOutput{
				DkimAttributes: make(map[string]sestypes.IdentityDkimAttributes),
			}
		}

		for _, identity := range result.Identities {
			// Determine identity type (email or domain)
			identityType := "domain"
			if strings.Contains(identity, "@") {
				identityType = "email"
			}

			res := resource.NewAWSResource(identity, identity, resource.TypeSESIdentity)
			res.Region = cfg.Region
			res.ARN = fmt.Sprintf("arn:aws:ses:%s:%s:identity/%s", cfg.Region, p.identity.AccountID, identity)

			// Config
			res.Config["identity"] = identity
			res.Config["identity_type"] = identityType

			// Verification status
			if verifyAttrs, ok := verifyResult.VerificationAttributes[identity]; ok {
				res.Config["verification_status"] = string(verifyAttrs.VerificationStatus)
				if verifyAttrs.VerificationToken != nil {
					res.Config["verification_token"] = aws.ToString(verifyAttrs.VerificationToken)
				}
			}

			// DKIM attributes
			if dkimAttrs, ok := dkimResult.DkimAttributes[identity]; ok {
				res.Config["dkim_enabled"] = dkimAttrs.DkimEnabled
				res.Config["dkim_verification_status"] = string(dkimAttrs.DkimVerificationStatus)
				if len(dkimAttrs.DkimTokens) > 0 {
					res.Config["dkim_tokens"] = dkimAttrs.DkimTokens
				}
			}

			infra.AddResource(res)
		}

		nextToken = result.NextToken
		if nextToken == nil {
			break
		}
	}

	return nil
}

// scanKMS discovers KMS keys (excluding AWS-managed keys).
func (p *APIParser) scanKMS(ctx context.Context, cfg aws.Config, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeKMSKey, opts) {
		return nil
	}

	client := kms.NewFromConfig(cfg)

	paginator := kms.NewListKeysPaginator(client, &kms.ListKeysInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list KMS keys: %w", err)
		}

		for _, key := range page.Keys {
			keyID := aws.ToString(key.KeyId)
			keyARN := aws.ToString(key.KeyArn)

			// Get key details
			describeResult, err := client.DescribeKey(ctx, &kms.DescribeKeyInput{
				KeyId: key.KeyId,
			})
			if err != nil {
				continue // Skip keys we can't describe
			}

			keyMetadata := describeResult.KeyMetadata

			// Skip AWS-managed keys (only include customer-managed keys)
			if keyMetadata.KeyManager == kmstypes.KeyManagerTypeAws {
				continue
			}

			// Skip keys pending deletion
			if keyMetadata.KeyState == kmstypes.KeyStatePendingDeletion {
				continue
			}

			// Use description as name if available, otherwise use key ID
			keyName := keyID
			if keyMetadata.Description != nil && aws.ToString(keyMetadata.Description) != "" {
				keyName = aws.ToString(keyMetadata.Description)
			}

			res := resource.NewAWSResource(keyID, keyName, resource.TypeKMSKey)
			res.Region = cfg.Region
			res.ARN = keyARN

			// Config
			res.Config["key_id"] = keyID
			res.Config["key_state"] = string(keyMetadata.KeyState)
			res.Config["key_usage"] = string(keyMetadata.KeyUsage)
			res.Config["key_spec"] = string(keyMetadata.KeySpec)
			res.Config["description"] = aws.ToString(keyMetadata.Description)
			res.Config["enabled"] = keyMetadata.Enabled
			res.Config["key_manager"] = string(keyMetadata.KeyManager)
			res.Config["origin"] = string(keyMetadata.Origin)
			res.Config["multi_region"] = keyMetadata.MultiRegion

			if keyMetadata.CreationDate != nil {
				res.Config["creation_date"] = keyMetadata.CreationDate.Format(time.RFC3339)
				res.CreatedAt = *keyMetadata.CreationDate
			}

			if keyMetadata.DeletionDate != nil {
				res.Config["deletion_date"] = keyMetadata.DeletionDate.Format(time.RFC3339)
			}

			// Encryption algorithms
			if len(keyMetadata.EncryptionAlgorithms) > 0 {
				var algorithms []string
				for _, alg := range keyMetadata.EncryptionAlgorithms {
					algorithms = append(algorithms, string(alg))
				}
				res.Config["encryption_algorithms"] = algorithms
			}

			// Signing algorithms
			if len(keyMetadata.SigningAlgorithms) > 0 {
				var algorithms []string
				for _, alg := range keyMetadata.SigningAlgorithms {
					algorithms = append(algorithms, string(alg))
				}
				res.Config["signing_algorithms"] = algorithms
			}

			// Get key aliases
			aliasResult, err := client.ListAliases(ctx, &kms.ListAliasesInput{
				KeyId: key.KeyId,
			})
			if err == nil && len(aliasResult.Aliases) > 0 {
				var aliases []string
				for _, alias := range aliasResult.Aliases {
					aliases = append(aliases, aws.ToString(alias.AliasName))
				}
				res.Config["aliases"] = aliases
			}

			// Get tags
			tagsResult, err := client.ListResourceTags(ctx, &kms.ListResourceTagsInput{
				KeyId: key.KeyId,
			})
			if err == nil {
				for _, tag := range tagsResult.Tags {
					res.Tags[aws.ToString(tag.TagKey)] = aws.ToString(tag.TagValue)
				}
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanCloudWatchLogGroups discovers CloudWatch Log Groups.
func (p *APIParser) scanCloudWatchLogGroups(ctx context.Context, cfg aws.Config, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeCloudWatchLogGroup, opts) {
		return nil
	}

	client := cloudwatchlogs.NewFromConfig(cfg)

	paginator := cloudwatchlogs.NewDescribeLogGroupsPaginator(client, &cloudwatchlogs.DescribeLogGroupsInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to describe log groups: %w", err)
		}

		for _, logGroup := range page.LogGroups {
			logGroupName := aws.ToString(logGroup.LogGroupName)

			res := resource.NewAWSResource(
				logGroupName,
				logGroupName,
				resource.TypeCloudWatchLogGroup,
			)
			res.Region = cfg.Region
			res.ARN = aws.ToString(logGroup.Arn)

			// Config
			if logGroup.RetentionInDays != nil {
				res.Config["retention_in_days"] = *logGroup.RetentionInDays
			}
			if logGroup.StoredBytes != nil {
				res.Config["stored_bytes"] = *logGroup.StoredBytes
			}
			if logGroup.KmsKeyId != nil {
				res.Config["kms_key_id"] = aws.ToString(logGroup.KmsKeyId)
			}
			if logGroup.CreationTime != nil {
				res.Config["creation_time"] = *logGroup.CreationTime
			}
			if logGroup.MetricFilterCount != nil {
				res.Config["metric_filter_count"] = *logGroup.MetricFilterCount
			}
			res.Config["log_group_class"] = string(logGroup.LogGroupClass)

			// Get tags for the log group
			tagsResult, err := client.ListTagsForResource(ctx, &cloudwatchlogs.ListTagsForResourceInput{
				ResourceArn: logGroup.Arn,
			})
			if err == nil && tagsResult.Tags != nil {
				for k, v := range tagsResult.Tags {
					res.Tags[k] = v
				}
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanCloudWatchMetricAlarms discovers CloudWatch Metric Alarms.
func (p *APIParser) scanCloudWatchMetricAlarms(ctx context.Context, cfg aws.Config, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeCloudWatchMetricAlarm, opts) {
		return nil
	}

	client := cloudwatch.NewFromConfig(cfg)

	paginator := cloudwatch.NewDescribeAlarmsPaginator(client, &cloudwatch.DescribeAlarmsInput{
		AlarmTypes: []cloudwatchtypes.AlarmType{
			cloudwatchtypes.AlarmTypeMetricAlarm,
		},
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to describe alarms: %w", err)
		}

		for _, alarm := range page.MetricAlarms {
			alarmName := aws.ToString(alarm.AlarmName)

			res := resource.NewAWSResource(
				alarmName,
				alarmName,
				resource.TypeCloudWatchMetricAlarm,
			)
			res.Region = cfg.Region
			res.ARN = aws.ToString(alarm.AlarmArn)

			// Config - basic alarm properties
			res.Config["alarm_name"] = alarmName
			res.Config["alarm_description"] = aws.ToString(alarm.AlarmDescription)
			res.Config["namespace"] = aws.ToString(alarm.Namespace)
			res.Config["metric_name"] = aws.ToString(alarm.MetricName)
			res.Config["statistic"] = string(alarm.Statistic)
			res.Config["extended_statistic"] = aws.ToString(alarm.ExtendedStatistic)
			if alarm.Period != nil {
				res.Config["period"] = *alarm.Period
			}
			if alarm.EvaluationPeriods != nil {
				res.Config["evaluation_periods"] = *alarm.EvaluationPeriods
			}
			if alarm.DatapointsToAlarm != nil {
				res.Config["datapoints_to_alarm"] = *alarm.DatapointsToAlarm
			}
			if alarm.Threshold != nil {
				res.Config["threshold"] = *alarm.Threshold
			}
			res.Config["comparison_operator"] = string(alarm.ComparisonOperator)
			res.Config["treat_missing_data"] = aws.ToString(alarm.TreatMissingData)
			res.Config["evaluate_low_sample_count_percentile"] = aws.ToString(alarm.EvaluateLowSampleCountPercentile)

			// Current state
			res.Config["state_value"] = string(alarm.StateValue)
			res.Config["state_reason"] = aws.ToString(alarm.StateReason)
			if alarm.StateUpdatedTimestamp != nil {
				res.Config["state_updated_timestamp"] = alarm.StateUpdatedTimestamp.Unix()
			}

			// Actions
			res.Config["actions_enabled"] = alarm.ActionsEnabled
			if len(alarm.AlarmActions) > 0 {
				res.Config["alarm_actions"] = alarm.AlarmActions
			}
			if len(alarm.OKActions) > 0 {
				res.Config["ok_actions"] = alarm.OKActions
			}
			if len(alarm.InsufficientDataActions) > 0 {
				res.Config["insufficient_data_actions"] = alarm.InsufficientDataActions
			}

			// Dimensions
			if len(alarm.Dimensions) > 0 {
				var dimensions []map[string]string
				for _, dim := range alarm.Dimensions {
					dimensions = append(dimensions, map[string]string{
						"name":  aws.ToString(dim.Name),
						"value": aws.ToString(dim.Value),
					})
				}
				res.Config["dimensions"] = dimensions
			}

			// Unit
			res.Config["unit"] = string(alarm.Unit)

			// Get tags for the alarm
			tagsResult, err := client.ListTagsForResource(ctx, &cloudwatch.ListTagsForResourceInput{
				ResourceARN: alarm.AlarmArn,
			})
			if err == nil && tagsResult.Tags != nil {
				for _, tag := range tagsResult.Tags {
					res.Tags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
				}
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// scanCloudWatchDashboards discovers CloudWatch dashboards.
func (p *APIParser) scanCloudWatchDashboards(ctx context.Context, cfg aws.Config, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	if !p.shouldScanType(resource.TypeCloudWatchDashboard, opts) {
		return nil
	}

	client := cloudwatch.NewFromConfig(cfg)

	paginator := cloudwatch.NewListDashboardsPaginator(client, &cloudwatch.ListDashboardsInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to list dashboards: %w", err)
		}

		for _, dashboard := range page.DashboardEntries {
			dashboardName := aws.ToString(dashboard.DashboardName)

			res := resource.NewAWSResource(dashboardName, dashboardName, resource.TypeCloudWatchDashboard)
			res.Region = cfg.Region
			res.ARN = aws.ToString(dashboard.DashboardArn)

			// Config from list response
			if dashboard.LastModified != nil {
				res.Config["last_modified"] = dashboard.LastModified.Format(time.RFC3339)
			}
			if dashboard.Size != nil {
				res.Config["size"] = *dashboard.Size
			}

			// Get dashboard details for widget count
			details, err := client.GetDashboard(ctx, &cloudwatch.GetDashboardInput{
				DashboardName: dashboard.DashboardName,
			})
			if err == nil && details.DashboardBody != nil {
				dashboardBody := aws.ToString(details.DashboardBody)

				// Parse body to count widgets
				var body struct {
					Widgets []interface{} `json:"widgets"`
				}
				if json.Unmarshal([]byte(dashboardBody), &body) == nil {
					res.Config["widget_count"] = len(body.Widgets)
				}
			}

			infra.AddResource(res)
		}
	}

	return nil
}

// shouldScanType checks if a resource type should be scanned based on filters.
func (p *APIParser) shouldScanType(t resource.Type, opts *parser.ParseOptions) bool {
	if opts == nil {
		return true
	}

	// Check type filters
	if len(opts.FilterTypes) > 0 {
		for _, ft := range opts.FilterTypes {
			if ft == t {
				return true
			}
		}
		return false
	}

	// Check category filters
	if len(opts.FilterCategories) > 0 {
		category := t.GetCategory()
		for _, fc := range opts.FilterCategories {
			if fc == category {
				return true
			}
		}
		return false
	}

	return true
}

// getTagValue retrieves a tag value from EC2 tags.
func (p *APIParser) getTagValue(tags []ec2types.Tag, key string) string {
	for _, tag := range tags {
		if aws.ToString(tag.Key) == key {
			return aws.ToString(tag.Value)
		}
	}
	return ""
}
