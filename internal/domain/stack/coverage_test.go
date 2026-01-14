package stack

import (
	"testing"

	"github.com/homeport/homeport/internal/domain/resource"
)

// TestResourceMappingCoverage verifies that all 84 resource types are mapped.
func TestResourceMappingCoverage(t *testing.T) {
	err := ValidateMappingCoverage()
	if err != nil {
		t.Fatalf("Coverage validation failed: %v", err)
	}
}

// TestResourceMappingCount verifies exact count of mapped resources.
func TestResourceMappingCount(t *testing.T) {
	// Expected counts from types.go
	expectedAWS := 30
	expectedGCP := 25
	expectedAzure := 29
	expectedTotal := expectedAWS + expectedGCP + expectedAzure // 84

	actualTotal := len(ResourceMapping)
	if actualTotal != expectedTotal {
		t.Errorf("ResourceMapping has %d entries, expected %d", actualTotal, expectedTotal)
	}

	// Verify CategoryMapping also has 84 entries (our source of truth)
	categoryMappingCount := len(resource.CategoryMapping)
	if categoryMappingCount != expectedTotal {
		t.Errorf("CategoryMapping has %d entries, expected %d", categoryMappingCount, expectedTotal)
	}
}

// TestResourceMappingProviderCounts verifies counts per provider.
func TestResourceMappingProviderCounts(t *testing.T) {
	counts := map[resource.Provider]int{
		resource.ProviderAWS:   0,
		resource.ProviderGCP:   0,
		resource.ProviderAzure: 0,
	}

	for resType := range ResourceMapping {
		provider := resType.Provider()
		counts[provider]++
	}

	expectedAWS := 30
	expectedGCP := 25
	expectedAzure := 29

	if counts[resource.ProviderAWS] != expectedAWS {
		t.Errorf("AWS has %d resource types, expected %d", counts[resource.ProviderAWS], expectedAWS)
	}
	if counts[resource.ProviderGCP] != expectedGCP {
		t.Errorf("GCP has %d resource types, expected %d", counts[resource.ProviderGCP], expectedGCP)
	}
	if counts[resource.ProviderAzure] != expectedAzure {
		t.Errorf("Azure has %d resource types, expected %d", counts[resource.ProviderAzure], expectedAzure)
	}
}

// TestGetStackTypeForResource verifies the helper function works correctly.
func TestGetStackTypeForResource(t *testing.T) {
	tests := []struct {
		resourceType resource.Type
		expected     StackType
		shouldExist  bool
	}{
		// AWS Database types
		{resource.TypeRDSInstance, StackTypeDatabase, true},
		{resource.TypeRDSCluster, StackTypeDatabase, true},
		{resource.TypeDynamoDBTable, StackTypeDatabase, true},

		// AWS Cache
		{resource.TypeElastiCache, StackTypeCache, true},

		// AWS Messaging
		{resource.TypeSQSQueue, StackTypeMessaging, true},
		{resource.TypeSNSTopic, StackTypeMessaging, true},
		{resource.TypeEventBridge, StackTypeMessaging, true},
		{resource.TypeKinesis, StackTypeMessaging, true},
		{resource.TypeSESIdentity, StackTypeMessaging, true},

		// AWS Storage
		{resource.TypeS3Bucket, StackTypeStorage, true},
		{resource.TypeEFSVolume, StackTypeStorage, true},

		// AWS Observability
		{resource.TypeCloudWatchLogGroup, StackTypeObservability, true},
		{resource.TypeCloudWatchMetricAlarm, StackTypeObservability, true},
		{resource.TypeCloudWatchDashboard, StackTypeObservability, true},

		// AWS Auth
		{resource.TypeCognitoPool, StackTypeAuth, true},

		// AWS Secrets
		{resource.TypeSecretsManager, StackTypeSecrets, true},
		{resource.TypeKMSKey, StackTypeSecrets, true},

		// AWS Compute (serverless)
		{resource.TypeLambdaFunction, StackTypeCompute, true},

		// AWS Passthrough
		{resource.TypeEC2Instance, StackTypePassthrough, true},
		{resource.TypeECSService, StackTypePassthrough, true},
		{resource.TypeEKSCluster, StackTypePassthrough, true},
		{resource.TypeEBSVolume, StackTypePassthrough, true},
		{resource.TypeALB, StackTypePassthrough, true},
		{resource.TypeAPIGateway, StackTypePassthrough, true},
		{resource.TypeRoute53Zone, StackTypePassthrough, true},
		{resource.TypeCloudFront, StackTypePassthrough, true},
		{resource.TypeVPC, StackTypePassthrough, true},
		{resource.TypeIAMRole, StackTypePassthrough, true},
		{resource.TypeACMCertificate, StackTypePassthrough, true},

		// GCP Database types
		{resource.TypeCloudSQL, StackTypeDatabase, true},
		{resource.TypeFirestore, StackTypeDatabase, true},
		{resource.TypeBigtable, StackTypeDatabase, true},
		{resource.TypeSpanner, StackTypeDatabase, true},

		// GCP Cache
		{resource.TypeMemorystore, StackTypeCache, true},

		// GCP Messaging
		{resource.TypePubSubTopic, StackTypeMessaging, true},
		{resource.TypePubSubSubscription, StackTypeMessaging, true},
		{resource.TypeCloudTasks, StackTypeMessaging, true},
		{resource.TypeCloudScheduler, StackTypeMessaging, true},

		// GCP Storage
		{resource.TypeGCSBucket, StackTypeStorage, true},
		{resource.TypeFilestore, StackTypeStorage, true},

		// GCP Auth
		{resource.TypeIdentityPlatform, StackTypeAuth, true},

		// GCP Secrets
		{resource.TypeSecretManager, StackTypeSecrets, true},

		// GCP Compute (serverless)
		{resource.TypeCloudFunction, StackTypeCompute, true},

		// GCP Passthrough
		{resource.TypeGCEInstance, StackTypePassthrough, true},
		{resource.TypeCloudRun, StackTypePassthrough, true},
		{resource.TypeGKE, StackTypePassthrough, true},
		{resource.TypeAppEngine, StackTypePassthrough, true},
		{resource.TypePersistentDisk, StackTypePassthrough, true},
		{resource.TypeCloudLB, StackTypePassthrough, true},
		{resource.TypeCloudDNS, StackTypePassthrough, true},
		{resource.TypeCloudCDN, StackTypePassthrough, true},
		{resource.TypeCloudArmor, StackTypePassthrough, true},
		{resource.TypeGCPVPCNetwork, StackTypePassthrough, true},
		{resource.TypeGCPIAM, StackTypePassthrough, true},

		// Azure Database types
		{resource.TypeAzureSQL, StackTypeDatabase, true},
		{resource.TypeAzurePostgres, StackTypeDatabase, true},
		{resource.TypeAzureMySQL, StackTypeDatabase, true},
		{resource.TypeCosmosDB, StackTypeDatabase, true},

		// Azure Cache
		{resource.TypeAzureCache, StackTypeCache, true},

		// Azure Messaging
		{resource.TypeServiceBus, StackTypeMessaging, true},
		{resource.TypeServiceBusQueue, StackTypeMessaging, true},
		{resource.TypeEventHub, StackTypeMessaging, true},
		{resource.TypeEventGrid, StackTypeMessaging, true},
		{resource.TypeLogicApp, StackTypeMessaging, true},

		// Azure Storage
		{resource.TypeBlobStorage, StackTypeStorage, true},
		{resource.TypeAzureStorageAcct, StackTypeStorage, true},
		{resource.TypeAzureFiles, StackTypeStorage, true},

		// Azure Auth
		{resource.TypeAzureADB2C, StackTypeAuth, true},

		// Azure Secrets
		{resource.TypeKeyVault, StackTypeSecrets, true},

		// Azure Compute (serverless)
		{resource.TypeAzureFunction, StackTypeCompute, true},

		// Azure Passthrough
		{resource.TypeAzureVM, StackTypePassthrough, true},
		{resource.TypeAzureVMWindows, StackTypePassthrough, true},
		{resource.TypeContainerInstance, StackTypePassthrough, true},
		{resource.TypeAKS, StackTypePassthrough, true},
		{resource.TypeAppService, StackTypePassthrough, true},
		{resource.TypeManagedDisk, StackTypePassthrough, true},
		{resource.TypeAzureLB, StackTypePassthrough, true},
		{resource.TypeAppGateway, StackTypePassthrough, true},
		{resource.TypeAzureDNS, StackTypePassthrough, true},
		{resource.TypeAzureCDN, StackTypePassthrough, true},
		{resource.TypeFrontDoor, StackTypePassthrough, true},
		{resource.TypeAzureVNet, StackTypePassthrough, true},
		{resource.TypeAzureFirewall, StackTypePassthrough, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.resourceType), func(t *testing.T) {
			stackType, exists := GetStackTypeForResource(tt.resourceType)
			if exists != tt.shouldExist {
				t.Errorf("GetStackTypeForResource(%s) exists = %v, expected %v",
					tt.resourceType, exists, tt.shouldExist)
			}
			if stackType != tt.expected {
				t.Errorf("GetStackTypeForResource(%s) = %s, expected %s",
					tt.resourceType, stackType, tt.expected)
			}
		})
	}
}

// TestGetStackTypeForResourceString verifies the string helper works.
func TestGetStackTypeForResourceString(t *testing.T) {
	stackType, exists := GetStackTypeForResourceString("aws_db_instance")
	if !exists {
		t.Error("Expected aws_db_instance to exist in mapping")
	}
	if stackType != StackTypeDatabase {
		t.Errorf("Expected aws_db_instance to map to database, got %s", stackType)
	}

	// Test unknown type returns passthrough
	stackType, exists = GetStackTypeForResourceString("unknown_type")
	if exists {
		t.Error("Expected unknown_type to not exist in mapping")
	}
	if stackType != StackTypePassthrough {
		t.Errorf("Expected unknown_type to default to passthrough, got %s", stackType)
	}
}

// TestGetResourceMappingStats verifies coverage statistics.
func TestGetResourceMappingStats(t *testing.T) {
	stats := GetResourceMappingStats()

	if stats.TotalTypes != 84 {
		t.Errorf("TotalTypes = %d, expected 84", stats.TotalTypes)
	}

	if stats.CoveredTypes != 84 {
		t.Errorf("CoveredTypes = %d, expected 84", stats.CoveredTypes)
	}

	if len(stats.UncoveredTypes) != 0 {
		t.Errorf("UncoveredTypes has %d entries, expected 0: %v",
			len(stats.UncoveredTypes), stats.UncoveredTypes)
	}

	if stats.CoveragePercentage != 100.0 {
		t.Errorf("CoveragePercentage = %.2f, expected 100.00", stats.CoveragePercentage)
	}

	// Verify provider counts
	if stats.ByProvider[resource.ProviderAWS] != 30 {
		t.Errorf("AWS count = %d, expected 30", stats.ByProvider[resource.ProviderAWS])
	}
	if stats.ByProvider[resource.ProviderGCP] != 25 {
		t.Errorf("GCP count = %d, expected 25", stats.ByProvider[resource.ProviderGCP])
	}
	if stats.ByProvider[resource.ProviderAzure] != 29 {
		t.Errorf("Azure count = %d, expected 29", stats.ByProvider[resource.ProviderAzure])
	}
}

// TestAllStackTypesHaveResources verifies each stack type has at least one resource.
func TestAllStackTypesHaveResources(t *testing.T) {
	stats := GetResourceMappingStats()

	for _, st := range AllStackTypes() {
		types := stats.ByStackType[st]
		if len(types) == 0 {
			t.Errorf("Stack type %s has no resources assigned", st)
		}
	}
}

// TestStackTypeDistribution verifies expected distribution of resources.
func TestStackTypeDistribution(t *testing.T) {
	stats := GetResourceMappingStats()

	// Expected minimum counts for each stack type
	// Storage: S3, EFS, GCS, Filestore, BlobStorage, AzureStorageAcct, AzureFiles = 7
	expectedMinimums := map[StackType]int{
		StackTypeDatabase:      10, // RDS, DynamoDB, CloudSQL, Firestore, Bigtable, Spanner, AzureSQL, etc.
		StackTypeCache:         3,  // ElastiCache, Memorystore, AzureCache
		StackTypeMessaging:     13, // SQS, SNS, EventBridge, Kinesis, SES, PubSub, ServiceBus, etc.
		StackTypeStorage:       7,  // S3, EFS, GCS, Filestore, BlobStorage, AzureStorageAcct, AzureFiles
		StackTypeObservability: 3,  // CloudWatch alarms, logs, dashboards
		StackTypeAuth:          3,  // Cognito, IdentityPlatform, AzureADB2C
		StackTypeSecrets:       4,  // SecretsManager, KMS, SecretManager, KeyVault
		StackTypeCompute:       3,  // Lambda, CloudFunction, AzureFunction
		StackTypePassthrough:   30, // VMs, containers, networking, etc.
	}

	for st, minCount := range expectedMinimums {
		actualCount := len(stats.ByStackType[st])
		if actualCount < minCount {
			t.Errorf("Stack type %s has %d resources, expected at least %d",
				st, actualCount, minCount)
		}
	}
}

// TestAllResourceTypesMapToValidStackTypes verifies no invalid stack types.
func TestAllResourceTypesMapToValidStackTypes(t *testing.T) {
	for resType, stackType := range ResourceMapping {
		if !stackType.IsValid() {
			t.Errorf("Resource type %s maps to invalid stack type: %s", resType, stackType)
		}
	}
}

// TestResourceTypesForStack verifies GetResourceTypesForStack returns correct types.
func TestGetResourceTypesForStack(t *testing.T) {
	tests := []struct {
		stackType    StackType
		mustContain  []resource.Type
		mustNotHave  []resource.Type
	}{
		{
			stackType: StackTypeDatabase,
			mustContain: []resource.Type{
				resource.TypeRDSInstance,
				resource.TypeCloudSQL,
				resource.TypeAzureSQL,
			},
			mustNotHave: []resource.Type{
				resource.TypeEC2Instance,
				resource.TypeS3Bucket,
			},
		},
		{
			stackType: StackTypeCache,
			mustContain: []resource.Type{
				resource.TypeElastiCache,
				resource.TypeMemorystore,
				resource.TypeAzureCache,
			},
		},
		{
			stackType: StackTypeMessaging,
			mustContain: []resource.Type{
				resource.TypeSQSQueue,
				resource.TypePubSubTopic,
				resource.TypeServiceBus,
			},
		},
		{
			stackType: StackTypeAuth,
			mustContain: []resource.Type{
				resource.TypeCognitoPool,
				resource.TypeIdentityPlatform,
				resource.TypeAzureADB2C,
			},
		},
		{
			stackType: StackTypeSecrets,
			mustContain: []resource.Type{
				resource.TypeSecretsManager,
				resource.TypeSecretManager,
				resource.TypeKeyVault,
			},
		},
		{
			stackType: StackTypeCompute,
			mustContain: []resource.Type{
				resource.TypeLambdaFunction,
				resource.TypeCloudFunction,
				resource.TypeAzureFunction,
			},
			mustNotHave: []resource.Type{
				resource.TypeEC2Instance, // VMs are passthrough
				resource.TypeGCEInstance,
				resource.TypeAzureVM,
			},
		},
	}

	for _, tt := range tests {
		t.Run(string(tt.stackType), func(t *testing.T) {
			types := GetResourceTypesForStack(tt.stackType)
			typeSet := make(map[resource.Type]bool)
			for _, rt := range types {
				typeSet[rt] = true
			}

			for _, required := range tt.mustContain {
				if !typeSet[required] {
					t.Errorf("Stack %s should contain %s but doesn't", tt.stackType, required)
				}
			}

			for _, excluded := range tt.mustNotHave {
				if typeSet[excluded] {
					t.Errorf("Stack %s should not contain %s but does", tt.stackType, excluded)
				}
			}
		})
	}
}

// TestGetResourceTypesForStackByProvider verifies provider filtering works.
func TestGetResourceTypesForStackByProvider(t *testing.T) {
	// Get database types for AWS only
	awsDbTypes := GetResourceTypesForStackByProvider(StackTypeDatabase, resource.ProviderAWS)
	for _, rt := range awsDbTypes {
		if rt.Provider() != resource.ProviderAWS {
			t.Errorf("Expected AWS provider for %s, got %s", rt, rt.Provider())
		}
	}

	// Should have RDS, Aurora, DynamoDB
	if len(awsDbTypes) < 3 {
		t.Errorf("Expected at least 3 AWS database types, got %d", len(awsDbTypes))
	}

	// Get cache types for each provider
	awsCache := GetResourceTypesForStackByProvider(StackTypeCache, resource.ProviderAWS)
	gcpCache := GetResourceTypesForStackByProvider(StackTypeCache, resource.ProviderGCP)
	azureCache := GetResourceTypesForStackByProvider(StackTypeCache, resource.ProviderAzure)

	if len(awsCache) != 1 {
		t.Errorf("Expected 1 AWS cache type (ElastiCache), got %d", len(awsCache))
	}
	if len(gcpCache) != 1 {
		t.Errorf("Expected 1 GCP cache type (Memorystore), got %d", len(gcpCache))
	}
	if len(azureCache) != 1 {
		t.Errorf("Expected 1 Azure cache type (AzureCache), got %d", len(azureCache))
	}
}

// TestAllResourceTypes verifies AllResourceTypes returns all mapped types.
func TestAllResourceTypes(t *testing.T) {
	types := AllResourceTypes()
	if len(types) != 84 {
		t.Errorf("AllResourceTypes returned %d types, expected 84", len(types))
	}

	// Verify each type is actually in the mapping
	for _, rt := range types {
		if _, ok := ResourceMapping[rt]; !ok {
			t.Errorf("Type %s returned by AllResourceTypes but not in ResourceMapping", rt)
		}
	}
}

// TestResourceMappingConsistency verifies ResourceMapping matches CategoryMapping.
func TestResourceMappingConsistency(t *testing.T) {
	// Every type in CategoryMapping should be in ResourceMapping
	for resType := range resource.CategoryMapping {
		if _, ok := ResourceMapping[resType]; !ok {
			t.Errorf("Resource type %s is in CategoryMapping but not in ResourceMapping", resType)
		}
	}

	// Every type in ResourceMapping should be in CategoryMapping
	for resType := range ResourceMapping {
		if _, ok := resource.CategoryMapping[resType]; !ok {
			t.Errorf("Resource type %s is in ResourceMapping but not in CategoryMapping", resType)
		}
	}
}

// TestServerlessOnlyInCompute verifies only serverless functions are in StackTypeCompute.
func TestServerlessOnlyInCompute(t *testing.T) {
	computeTypes := GetResourceTypesForStack(StackTypeCompute)

	expectedServerless := map[resource.Type]bool{
		resource.TypeLambdaFunction: true,
		resource.TypeCloudFunction:  true,
		resource.TypeAzureFunction:  true,
	}

	if len(computeTypes) != len(expectedServerless) {
		t.Errorf("Expected exactly %d serverless types in compute, got %d",
			len(expectedServerless), len(computeTypes))
	}

	for _, rt := range computeTypes {
		if !expectedServerless[rt] {
			t.Errorf("Unexpected type %s in StackTypeCompute - only serverless should be here", rt)
		}
	}
}

// TestVMsArePassthrough verifies all VM/compute instances are passthrough.
func TestVMsArePassthrough(t *testing.T) {
	vmTypes := []resource.Type{
		resource.TypeEC2Instance,
		resource.TypeGCEInstance,
		resource.TypeAzureVM,
		resource.TypeAzureVMWindows,
	}

	for _, rt := range vmTypes {
		stackType, exists := GetStackTypeForResource(rt)
		if !exists {
			t.Errorf("VM type %s not found in ResourceMapping", rt)
			continue
		}
		if stackType != StackTypePassthrough {
			t.Errorf("VM type %s should be passthrough, got %s", rt, stackType)
		}
	}
}

// TestKubernetesArePassthrough verifies all Kubernetes clusters are passthrough.
func TestKubernetesArePassthrough(t *testing.T) {
	k8sTypes := []resource.Type{
		resource.TypeEKSCluster,
		resource.TypeGKE,
		resource.TypeAKS,
	}

	for _, rt := range k8sTypes {
		stackType, exists := GetStackTypeForResource(rt)
		if !exists {
			t.Errorf("Kubernetes type %s not found in ResourceMapping", rt)
			continue
		}
		if stackType != StackTypePassthrough {
			t.Errorf("Kubernetes type %s should be passthrough, got %s", rt, stackType)
		}
	}
}

// TestNetworkingArePassthrough verifies all networking resources are passthrough.
func TestNetworkingArePassthrough(t *testing.T) {
	networkTypes := []resource.Type{
		// AWS
		resource.TypeALB,
		resource.TypeAPIGateway,
		resource.TypeRoute53Zone,
		resource.TypeCloudFront,
		resource.TypeVPC,
		// GCP
		resource.TypeCloudLB,
		resource.TypeCloudDNS,
		resource.TypeCloudCDN,
		resource.TypeGCPVPCNetwork,
		// Azure
		resource.TypeAzureLB,
		resource.TypeAppGateway,
		resource.TypeAzureDNS,
		resource.TypeAzureCDN,
		resource.TypeFrontDoor,
		resource.TypeAzureVNet,
	}

	for _, rt := range networkTypes {
		stackType, exists := GetStackTypeForResource(rt)
		if !exists {
			t.Errorf("Networking type %s not found in ResourceMapping", rt)
			continue
		}
		if stackType != StackTypePassthrough {
			t.Errorf("Networking type %s should be passthrough, got %s", rt, stackType)
		}
	}
}

// TestCoveragePercentageIs100 is the final validation that we have 100% coverage.
func TestCoveragePercentageIs100(t *testing.T) {
	stats := GetResourceMappingStats()

	if stats.CoveragePercentage != 100.0 {
		t.Fatalf("CRITICAL: Coverage is only %.2f%%, expected 100%%\n"+
			"Missing types: %v",
			stats.CoveragePercentage, stats.UncoveredTypes)
	}

	t.Logf("SUCCESS: 100%% coverage achieved for all %d resource types", stats.TotalTypes)
	t.Logf("Distribution:")
	for _, st := range AllStackTypes() {
		count := len(stats.ByStackType[st])
		t.Logf("  - %s: %d types", st.DisplayName(), count)
	}
}
