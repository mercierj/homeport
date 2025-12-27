package aws

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/agnostech/agnostech/internal/domain/parser"
	"github.com/agnostech/agnostech/internal/domain/resource"
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
	// Initialize credential config from options
	if opts != nil && opts.APICredentials != nil {
		p.credConfig = FromParseOptions(opts.APICredentials, opts.Regions)
	}

	// Set a default region for initial connection if not set
	if p.credConfig.Region == "" {
		p.credConfig.Region = "us-east-1"
	}

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

	// Scan resources across all regions
	for _, region := range regions {
		// Create region-specific config
		regionCfg := cfg.Copy()
		regionCfg.Region = region

		// Scan all resource types
		if err := p.scanEC2(ctx, regionCfg, infra, opts); err != nil && !opts.IgnoreErrors {
			return nil, fmt.Errorf("failed to scan EC2: %w", err)
		}

		if err := p.scanS3(ctx, regionCfg, infra, opts); err != nil && !opts.IgnoreErrors {
			return nil, fmt.Errorf("failed to scan S3: %w", err)
		}

		if err := p.scanRDS(ctx, regionCfg, infra, opts); err != nil && !opts.IgnoreErrors {
			return nil, fmt.Errorf("failed to scan RDS: %w", err)
		}

		if err := p.scanLambda(ctx, regionCfg, infra, opts); err != nil && !opts.IgnoreErrors {
			return nil, fmt.Errorf("failed to scan Lambda: %w", err)
		}

		if err := p.scanSQS(ctx, regionCfg, infra, opts); err != nil && !opts.IgnoreErrors {
			return nil, fmt.Errorf("failed to scan SQS: %w", err)
		}

		if err := p.scanSNS(ctx, regionCfg, infra, opts); err != nil && !opts.IgnoreErrors {
			return nil, fmt.Errorf("failed to scan SNS: %w", err)
		}

		if err := p.scanElastiCache(ctx, regionCfg, infra, opts); err != nil && !opts.IgnoreErrors {
			return nil, fmt.Errorf("failed to scan ElastiCache: %w", err)
		}

		if err := p.scanALB(ctx, regionCfg, infra, opts); err != nil && !opts.IgnoreErrors {
			return nil, fmt.Errorf("failed to scan ALB: %w", err)
		}

		if err := p.scanDynamoDB(ctx, regionCfg, infra, opts); err != nil && !opts.IgnoreErrors {
			return nil, fmt.Errorf("failed to scan DynamoDB: %w", err)
		}

		if err := p.scanASG(ctx, regionCfg, infra, opts); err != nil && !opts.IgnoreErrors {
			return nil, fmt.Errorf("failed to scan Auto Scaling: %w", err)
		}
	}

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
