package aws

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/homeport/homeport/internal/domain/authz"
)

type ECSAdapter struct {
	mu                  sync.Mutex
	services            map[string]ecsService
	createServiceTokens map[string]ecsCreateServiceReplay
	taskDefinitions     map[string]ecsTaskDefinition
	serviceQuota        int
	authorizer          authz.Authorizer
	auditSink           func(authz.Decision)
}

type ECSOption func(*ECSAdapter)

type ecsService struct {
	Cluster              string
	Name                 string
	TaskDefinition       string
	DesiredCount         int
	LaunchType           string
	Status               string
	AvailabilityRebal    string
	CapacityProviders    any
	DeploymentController any
	EnableManagedTags    any
	HealthCheckGrace     any
	RoleArn              string
	PlatformVersion      string
	SchedulingStrategy   string
	EnableExecuteCommand any
	PropagateTags        string
	LoadBalancers        any
	NetworkConfig        any
	ServiceRegistries    any
	VolumeConfigs        any
	PlacementConstraints any
	PlacementStrategy    any
	DeploymentConfig     any
	DeploymentID         string
	Tags                 map[string]string
}

type ecsCreateServiceReplay struct {
	Key       string
	Signature string
}

type ecsTaskDefinition struct {
	Family                  string
	Revision                int
	Status                  string
	Cpu                     string
	Memory                  string
	NetworkMode             string
	ExecutionRoleArn        string
	TaskRoleArn             string
	ContainerDefinitions    any
	RequiresCompatibilities any
	RuntimePlatform         any
	Volumes                 any
	Tags                    map[string]string
}

func NewECSAdapter(options ...ECSOption) *ECSAdapter {
	adapter := &ECSAdapter{
		services:            map[string]ecsService{},
		createServiceTokens: map[string]ecsCreateServiceReplay{},
		taskDefinitions:     map[string]ecsTaskDefinition{},
		authorizer:          authz.AllowAll,
	}
	for _, option := range options {
		option(adapter)
	}
	return adapter
}

func WithECSServiceQuota(maxServices int) ECSOption {
	return func(adapter *ECSAdapter) {
		adapter.serviceQuota = maxServices
	}
}

func WithECSAuthorizer(authorizer authz.Authorizer) ECSOption {
	return func(adapter *ECSAdapter) {
		if authorizer != nil {
			adapter.authorizer = authorizer
		}
	}
}

func WithECSAuditSink(sink func(authz.Decision)) ECSOption {
	return func(adapter *ECSAdapter) {
		adapter.auditSink = sink
	}
}

func (ECSAdapter) Provider() string { return "aws" }
func (ECSAdapter) Service() string  { return "ecs" }
func (ECSAdapter) Routes() []string { return []string{"POST /compat/aws/ecs"} }
func (ECSAdapter) TargetEnv() map[string]string {
	return map[string]string{
		"AWS_ENDPOINT_URL_ECS":    "http://homeport:8080/api/v1/compat/aws/ecs",
		"HOMEPORT_COMPAT_BACKEND": "docker-compose",
	}
}
func (ECSAdapter) ConformanceChecks() []string {
	return []string{"create-service", "describe-services", "list-services", "update-service", "delete-service"}
}

func (a *ECSAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	action, body, err := decodeAWSAction(r)
	if err != nil {
		writeECSError(w, "ClientException", err.Error())
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	switch action {
	case "RegisterTaskDefinition":
		family := ecsField(body, "Family", "family")
		if family == "" {
			writeECSError(w, "ClientException", "Family is required")
			return
		}
		containers := ecsBodyValue(body, "ContainerDefinitions", "containerDefinitions")
		if len(ecsContainerDefinitions(containers)) == 0 {
			writeECSError(w, "ClientException", "ContainerDefinitions is required")
			return
		}
		if !ecsContainerDefinitionsComplete(containers) {
			writeECSError(w, "ClientException", "Container name and image are required")
			return
		}
		tags := ecsTags(ecsBodyValue(body, "Tags", "tags"))
		if !ecsValidTagKeys(tags) {
			writeECSError(w, "InvalidParameterException", "invalid tag key")
			return
		}
		revision := a.nextTaskDefinitionRevision(family)
		taskDefinitionARN := ecsTaskDefinitionARN(family + ":" + strconv.Itoa(revision))
		if !a.authorized(w, r, "RegisterTaskDefinition", taskDefinitionARN) {
			return
		}
		taskDefinition := ecsTaskDefinition{
			Family:                  family,
			Revision:                revision,
			Status:                  "ACTIVE",
			Cpu:                     ecsField(body, "Cpu", "cpu"),
			Memory:                  ecsField(body, "Memory", "memory"),
			NetworkMode:             ecsField(body, "NetworkMode", "networkMode"),
			ExecutionRoleArn:        ecsField(body, "ExecutionRoleArn", "executionRoleArn"),
			TaskRoleArn:             ecsField(body, "TaskRoleArn", "taskRoleArn"),
			ContainerDefinitions:    containers,
			RequiresCompatibilities: ecsTaskDefinitionCompatibilities(body),
			RuntimePlatform:         ecsBodyValue(body, "RuntimePlatform", "runtimePlatform"),
			Volumes:                 ecsBodyValue(body, "Volumes", "volumes"),
			Tags:                    tags,
		}
		a.taskDefinitions[taskDefinitionARN] = taskDefinition
		writeJSON(w, http.StatusOK, map[string]any{"taskDefinition": ecsTaskDefinitionShape(taskDefinition)})
	case "DescribeTaskDefinition":
		arn, taskDefinition, ok := a.taskDefinitionWithARN(ecsField(body, "TaskDefinition", "taskDefinition"))
		if !ok {
			writeECSError(w, "ClientException", "task definition not found")
			return
		}
		if !a.authorized(w, r, "DescribeTaskDefinition", arn) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"taskDefinition": ecsTaskDefinitionShape(taskDefinition), "tags": ecsTagsJSON(taskDefinition.Tags)})
	case "ListTaskDefinitions":
		prefix := ecsField(body, "FamilyPrefix", "familyPrefix")
		status := ecsField(body, "Status", "status")
		if status == "" {
			status = "ACTIVE"
		}
		if !ecsValidTaskDefinitionStatus(status) {
			writeECSError(w, "InvalidParameterException", "invalid task definition status")
			return
		}
		if !a.authorized(w, r, "ListTaskDefinitions", ecsTaskDefinitionListResource(prefix)) {
			return
		}
		arns := make([]string, 0, len(a.taskDefinitions))
		for _, taskDefinition := range a.taskDefinitions {
			if taskDefinition.Status == status && (prefix == "" || strings.HasPrefix(taskDefinition.Family, prefix)) {
				arns = append(arns, ecsTaskDefinitionARN(taskDefinition.Family+":"+strconv.Itoa(taskDefinition.Revision)))
			}
		}
		sort.Strings(arns)
		page, next, ok := ecsStringPage(arns, body, 100)
		if !ok {
			writeECSError(w, "InvalidParameterException", "invalid pagination")
			return
		}
		result := map[string]any{"taskDefinitionArns": page}
		if next != "" {
			result["nextToken"] = next
		}
		writeJSON(w, http.StatusOK, result)
	case "DeregisterTaskDefinition":
		arn, taskDefinition, ok := a.taskDefinitionWithARN(ecsField(body, "TaskDefinition", "taskDefinition"))
		if !ok {
			writeECSError(w, "ClientException", "task definition not found")
			return
		}
		if !a.authorized(w, r, "DeregisterTaskDefinition", arn) {
			return
		}
		taskDefinition.Status = "INACTIVE"
		a.taskDefinitions[arn] = taskDefinition
		writeJSON(w, http.StatusOK, map[string]any{"taskDefinition": ecsTaskDefinitionShape(taskDefinition)})
	case "CreateService":
		cluster := ecsCluster(body)
		name := ecsField(body, "ServiceName", "serviceName")
		if name == "" {
			writeECSError(w, "ClientException", "ServiceName is required")
			return
		}
		taskDefinition := ecsField(body, "TaskDefinition", "taskDefinition")
		if taskDefinition == "" {
			writeECSError(w, "ClientException", "TaskDefinition is required")
			return
		}
		tags := ecsTags(ecsBodyValue(body, "Tags", "tags"))
		if !ecsValidTagKeys(tags) {
			writeECSError(w, "InvalidParameterException", "invalid tag key")
			return
		}
		if !a.authorized(w, r, "CreateService", ecsServiceARN(ecsService{Cluster: cluster, Name: name})) {
			return
		}
		key := ecsServiceKey(cluster, name)
		token := ecsField(body, "ClientToken", "clientToken")
		signature := ecsCreateServiceSignature(body, cluster, name)
		if token != "" {
			if replay, ok := a.createServiceTokens[token]; ok {
				if replay.Signature != signature {
					writeECSError(w, "InvalidParameterException", "client token does not match previous request")
					return
				}
				writeJSON(w, http.StatusOK, map[string]any{"service": ecsServiceShape(a.services[replay.Key])})
				return
			}
		}
		if _, ok := a.services[key]; ok {
			writeECSError(w, "ServiceAlreadyExistsException", "service already exists")
			return
		}
		if a.serviceQuota > 0 && a.activeServiceCount() >= a.serviceQuota {
			writeECSError(w, "LimitExceededException", "service quota exceeded")
			return
		}
		if value, ok := ecsInt(ecsBodyValue(body, "DesiredCount", "desiredCount")); ok && value < 0 {
			writeECSError(w, "InvalidParameterException", "desired count must be greater than or equal to 0")
			return
		}
		healthCheckGrace := ecsBodyValue(body, "HealthCheckGracePeriodSeconds", "healthCheckGracePeriodSeconds")
		if value, ok := ecsInt(healthCheckGrace); ok && value < 0 {
			writeECSError(w, "InvalidParameterException", "health check grace period must be greater than or equal to 0")
			return
		}
		capacityProviders := ecsBodyValue(body, "CapacityProviderStrategy", "capacityProviderStrategy")
		if !ecsValidCapacityProviderStrategy(capacityProviders) {
			writeECSError(w, "InvalidParameterException", "invalid capacity provider strategy")
			return
		}
		launchType := ecsLaunchType(body)
		if !ecsValidLaunchType(launchType) {
			writeECSError(w, "InvalidParameterException", "invalid launch type")
			return
		}
		schedulingStrategy := ecsSchedulingStrategy(body)
		if !ecsValidSchedulingStrategy(schedulingStrategy) {
			writeECSError(w, "InvalidParameterException", "invalid scheduling strategy")
			return
		}
		deploymentController := ecsBodyValue(body, "DeploymentController", "deploymentController")
		if !ecsValidDeploymentController(deploymentController) {
			writeECSError(w, "InvalidParameterException", "invalid deployment controller")
			return
		}
		deploymentConfig := ecsBodyValue(body, "DeploymentConfiguration", "deploymentConfiguration")
		if !ecsValidDeploymentConfiguration(deploymentConfig) {
			writeECSError(w, "InvalidParameterException", "invalid deployment configuration")
			return
		}
		availabilityRebalancing := ecsField(body, "AvailabilityZoneRebalancing", "availabilityZoneRebalancing")
		if !ecsValidAvailabilityZoneRebalancing(availabilityRebalancing) {
			writeECSError(w, "InvalidParameterException", "invalid availability zone rebalancing")
			return
		}
		propagateTags := ecsField(body, "PropagateTags", "propagateTags")
		if !ecsValidPropagateTags(propagateTags) {
			writeECSError(w, "InvalidParameterException", "invalid propagate tags")
			return
		}
		networkConfig := ecsBodyValue(body, "NetworkConfiguration", "networkConfiguration")
		if !ecsValidNetworkConfiguration(networkConfig) {
			writeECSError(w, "InvalidParameterException", "invalid network configuration")
			return
		}
		placementConstraints := ecsBodyValue(body, "PlacementConstraints", "placementConstraints")
		placementStrategy := ecsBodyValue(body, "PlacementStrategy", "placementStrategy")
		if !ecsValidPlacement(placementConstraints, placementStrategy) {
			writeECSError(w, "InvalidParameterException", "invalid placement")
			return
		}
		desiredCount := intValue(body, 1, "DesiredCount", "desiredCount")
		service := ecsService{
			Cluster:              cluster,
			Name:                 name,
			TaskDefinition:       taskDefinition,
			DesiredCount:         desiredCount,
			LaunchType:           launchType,
			Status:               "ACTIVE",
			AvailabilityRebal:    availabilityRebalancing,
			CapacityProviders:    capacityProviders,
			DeploymentController: deploymentController,
			EnableManagedTags:    ecsBodyValue(body, "EnableECSManagedTags", "enableECSManagedTags"),
			HealthCheckGrace:     healthCheckGrace,
			RoleArn:              ecsField(body, "Role", "role"),
			PlatformVersion:      ecsField(body, "PlatformVersion", "platformVersion"),
			SchedulingStrategy:   schedulingStrategy,
			EnableExecuteCommand: ecsBodyValue(body, "EnableExecuteCommand", "enableExecuteCommand"),
			PropagateTags:        propagateTags,
			LoadBalancers:        ecsBodyValue(body, "LoadBalancers", "loadBalancers"),
			NetworkConfig:        networkConfig,
			ServiceRegistries:    ecsBodyValue(body, "ServiceRegistries", "serviceRegistries"),
			VolumeConfigs:        ecsBodyValue(body, "VolumeConfigurations", "volumeConfigurations"),
			PlacementConstraints: placementConstraints,
			PlacementStrategy:    placementStrategy,
			DeploymentConfig:     deploymentConfig,
			Tags:                 tags,
		}
		a.services[key] = service
		if token != "" {
			a.createServiceTokens[token] = ecsCreateServiceReplay{Key: key, Signature: signature}
		}
		writeJSON(w, http.StatusOK, map[string]any{"service": ecsServiceShape(service)})
	case "DescribeServices":
		cluster := ecsCluster(body)
		names := ecsStringList(ecsBodyValue(body, "Services", "services"))
		if !a.authorized(w, r, "DescribeServices", ecsDescribeResource(cluster, names)) {
			return
		}
		services := make([]map[string]any, 0, len(names))
		failures := make([]map[string]string, 0)
		for _, name := range names {
			service, ok := a.services[ecsServiceKey(cluster, ecsServiceName(name))]
			if !ok {
				failures = append(failures, map[string]string{"arn": name, "reason": "MISSING"})
				continue
			}
			services = append(services, ecsServiceShape(service))
		}
		writeJSON(w, http.StatusOK, map[string]any{"services": services, "failures": failures})
	case "ListServices":
		cluster := ecsCluster(body)
		if !a.authorized(w, r, "ListServices", ecsClusterARN(cluster)) {
			return
		}
		arns := make([]string, 0)
		for _, service := range a.services {
			if service.Cluster == cluster && service.Status != "INACTIVE" {
				arns = append(arns, ecsServiceARN(service))
			}
		}
		sort.Strings(arns)
		page, next, ok := ecsStringPage(arns, body, 10)
		if !ok {
			writeECSError(w, "InvalidParameterException", "invalid pagination")
			return
		}
		result := map[string]any{"serviceArns": page}
		if next != "" {
			result["nextToken"] = next
		}
		writeJSON(w, http.StatusOK, result)
	case "UpdateService":
		cluster := ecsCluster(body)
		name := ecsServiceName(ecsField(body, "Service", "service"))
		key := ecsServiceKey(cluster, name)
		service, ok := a.services[key]
		if !ok {
			writeECSError(w, "ServiceNotFoundException", "service not found")
			return
		}
		if !a.authorized(w, r, "UpdateService", ecsServiceARN(service)) {
			return
		}
		if value, ok := ecsInt(ecsBodyValue(body, "DesiredCount", "desiredCount")); ok && value < 0 {
			writeECSError(w, "InvalidParameterException", "desired count must be greater than or equal to 0")
			return
		}
		task := ecsField(body, "TaskDefinition", "taskDefinition")
		if task != "" && task != service.TaskDefinition {
			if _, ok := a.taskDefinition(task); !ok {
				writeECSError(w, "ClientException", "task definition not found")
				return
			}
		}
		placementConstraints := ecsBodyValue(body, "PlacementConstraints", "placementConstraints")
		placementStrategy := ecsBodyValue(body, "PlacementStrategy", "placementStrategy")
		if !ecsValidPlacement(placementConstraints, placementStrategy) {
			writeECSError(w, "InvalidParameterException", "invalid placement")
			return
		}
		deploymentConfig := ecsBodyValue(body, "DeploymentConfiguration", "deploymentConfiguration")
		if !ecsValidDeploymentConfiguration(deploymentConfig) {
			writeECSError(w, "InvalidParameterException", "invalid deployment configuration")
			return
		}
		deploymentController := ecsBodyValue(body, "DeploymentController", "deploymentController")
		if !ecsValidDeploymentController(deploymentController) {
			writeECSError(w, "InvalidParameterException", "invalid deployment controller")
			return
		}
		propagateTags := ecsField(body, "PropagateTags", "propagateTags")
		if !ecsValidPropagateTags(propagateTags) {
			writeECSError(w, "InvalidParameterException", "invalid propagate tags")
			return
		}
		networkConfig := ecsBodyValue(body, "NetworkConfiguration", "networkConfiguration")
		if !ecsValidNetworkConfiguration(networkConfig) {
			writeECSError(w, "InvalidParameterException", "invalid network configuration")
			return
		}
		platformVersion := ecsField(body, "PlatformVersion", "platformVersion")
		enableExecuteCommand := ecsBodyValue(body, "EnableExecuteCommand", "enableExecuteCommand")
		loadBalancers := ecsBodyValue(body, "LoadBalancers", "loadBalancers")
		availabilityRebalancing := ecsField(body, "AvailabilityZoneRebalancing", "availabilityZoneRebalancing")
		if !ecsValidAvailabilityZoneRebalancing(availabilityRebalancing) {
			writeECSError(w, "InvalidParameterException", "invalid availability zone rebalancing")
			return
		}
		capacityProviders := ecsBodyValue(body, "CapacityProviderStrategy", "capacityProviderStrategy")
		if !ecsValidCapacityProviderStrategy(capacityProviders) {
			writeECSError(w, "InvalidParameterException", "invalid capacity provider strategy")
			return
		}
		healthCheckGrace := ecsBodyValue(body, "HealthCheckGracePeriodSeconds", "healthCheckGracePeriodSeconds")
		if value, ok := ecsInt(healthCheckGrace); ok && value < 0 {
			writeECSError(w, "InvalidParameterException", "health check grace period must be greater than or equal to 0")
			return
		}
		enableManagedTags := ecsBodyValue(body, "EnableECSManagedTags", "enableECSManagedTags")
		serviceRegistries := ecsBodyValue(body, "ServiceRegistries", "serviceRegistries")
		volumeConfigs := ecsBodyValue(body, "VolumeConfigurations", "volumeConfigurations")
		service.DesiredCount = intValue(body, service.DesiredCount, "DesiredCount", "desiredCount")
		if task != "" {
			service.TaskDefinition = task
		}
		if placementConstraints != nil {
			service.PlacementConstraints = placementConstraints
		}
		if placementStrategy != nil {
			service.PlacementStrategy = placementStrategy
		}
		if deploymentConfig != nil {
			service.DeploymentConfig = deploymentConfig
		}
		if deploymentController != nil {
			service.DeploymentController = deploymentController
		}
		if platformVersion != "" {
			service.PlatformVersion = platformVersion
		}
		if enableExecuteCommand != nil {
			service.EnableExecuteCommand = enableExecuteCommand
		}
		if propagateTags != "" {
			service.PropagateTags = propagateTags
		}
		if loadBalancers != nil {
			service.LoadBalancers = loadBalancers
		}
		if networkConfig != nil {
			service.NetworkConfig = networkConfig
		}
		if availabilityRebalancing != "" {
			service.AvailabilityRebal = availabilityRebalancing
		}
		if capacityProviders != nil {
			service.CapacityProviders = capacityProviders
		}
		if enableManagedTags != nil {
			service.EnableManagedTags = enableManagedTags
		}
		if healthCheckGrace != nil {
			service.HealthCheckGrace = healthCheckGrace
		}
		if serviceRegistries != nil {
			service.ServiceRegistries = serviceRegistries
		}
		if volumeConfigs != nil {
			service.VolumeConfigs = volumeConfigs
		}
		if ecsUpdateTriggersDeployment(body) {
			service.DeploymentID = "homeport-update"
		}
		a.services[key] = service
		writeJSON(w, http.StatusOK, map[string]any{"service": ecsServiceShape(service)})
	case "DeleteService":
		cluster := ecsCluster(body)
		name := ecsServiceName(ecsField(body, "Service", "service"))
		key := ecsServiceKey(cluster, name)
		service, ok := a.services[key]
		if !ok {
			writeECSError(w, "ServiceNotFoundException", "service not found")
			return
		}
		if !a.authorized(w, r, "DeleteService", ecsServiceARN(service)) {
			return
		}
		service.Status = "INACTIVE"
		a.services[key] = service
		writeJSON(w, http.StatusOK, map[string]any{"service": ecsServiceShape(service)})
	case "ListTagsForResource":
		resourceARN := ecsField(body, "ResourceArn", "resourceArn")
		if _, service, ok := a.serviceByARN(resourceARN); ok {
			if !a.authorized(w, r, "ListTagsForResource", ecsServiceARN(service)) {
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"tags": ecsTagsJSON(service.Tags)})
			return
		}
		if _, taskDefinition, ok := a.taskDefinitionWithARN(resourceARN); ok {
			if !a.authorized(w, r, "ListTagsForResource", resourceARN) {
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"tags": ecsTagsJSON(taskDefinition.Tags)})
			return
		}
		writeECSError(w, "ClientException", "resource not found")
	case "TagResource":
		resourceARN := ecsField(body, "ResourceArn", "resourceArn")
		tags := ecsTags(ecsBodyValue(body, "Tags", "tags"))
		if !ecsValidTagKeys(tags) {
			writeECSError(w, "InvalidParameterException", "invalid tag key")
			return
		}
		if key, service, ok := a.serviceByARN(resourceARN); ok {
			if !a.authorized(w, r, "TagResource", ecsServiceARN(service)) {
				return
			}
			if service.Tags == nil {
				service.Tags = map[string]string{}
			}
			mergeStringMap(service.Tags, tags)
			a.services[key] = service
			writeJSON(w, http.StatusOK, map[string]string{})
			return
		}
		if arn, taskDefinition, ok := a.taskDefinitionWithARN(resourceARN); ok {
			if !a.authorized(w, r, "TagResource", arn) {
				return
			}
			if taskDefinition.Tags == nil {
				taskDefinition.Tags = map[string]string{}
			}
			mergeStringMap(taskDefinition.Tags, tags)
			a.taskDefinitions[arn] = taskDefinition
			writeJSON(w, http.StatusOK, map[string]string{})
			return
		}
		writeECSError(w, "ClientException", "resource not found")
	case "UntagResource":
		resourceARN := ecsField(body, "ResourceArn", "resourceArn")
		if key, service, ok := a.serviceByARN(resourceARN); ok {
			if !a.authorized(w, r, "UntagResource", ecsServiceARN(service)) {
				return
			}
			for _, tagKey := range ecsStringList(ecsBodyValue(body, "TagKeys", "tagKeys")) {
				delete(service.Tags, tagKey)
			}
			a.services[key] = service
			writeJSON(w, http.StatusOK, map[string]string{})
			return
		}
		if arn, taskDefinition, ok := a.taskDefinitionWithARN(resourceARN); ok {
			if !a.authorized(w, r, "UntagResource", arn) {
				return
			}
			for _, tagKey := range ecsStringList(ecsBodyValue(body, "TagKeys", "tagKeys")) {
				delete(taskDefinition.Tags, tagKey)
			}
			a.taskDefinitions[arn] = taskDefinition
			writeJSON(w, http.StatusOK, map[string]string{})
			return
		}
		writeECSError(w, "ClientException", "resource not found")
	default:
		writeECSError(w, "UnsupportedOperation", "ECS action is not implemented")
	}
}
func (a *ECSAdapter) nextTaskDefinitionRevision(family string) int {
	revision := 1
	for _, taskDefinition := range a.taskDefinitions {
		if taskDefinition.Family == family && taskDefinition.Revision >= revision {
			revision = taskDefinition.Revision + 1
		}
	}
	return revision
}

func (a *ECSAdapter) activeServiceCount() int {
	count := 0
	for _, service := range a.services {
		if service.Status != "INACTIVE" {
			count++
		}
	}
	return count
}

func ecsCreateServiceSignature(body map[string]any, cluster, name string) string {
	return strings.Join([]string{
		cluster,
		name,
		ecsField(body, "TaskDefinition", "taskDefinition"),
		strconv.Itoa(intValue(body, 1, "DesiredCount", "desiredCount")),
		ecsLaunchType(body),
	}, "\x00")
}

func (a *ECSAdapter) authorized(w http.ResponseWriter, r *http.Request, action, resource string) bool {
	req := authz.Request{
		Principal:           awsPrincipal(r),
		PrincipalAttributes: awsPrincipalAttributes(r),
		Action:              "ecs:" + action,
		Resource:            resource,
		Context: map[string]string{
			"provider":     "aws",
			"service":      "ecs",
			"method":       r.Method,
			"request_id":   "homeport",
			"source_ip":    sourceIP(r),
			"current_time": time.Now().UTC().Format(time.RFC3339),
			"user_agent":   r.UserAgent(),
		},
		Claims: awsClaims(r),
	}
	if value := r.Header.Get("X-Homeport-Credential-Age"); value != "" {
		req.Context["credential_age"] = value
	}
	if value := r.Header.Get("X-Homeport-Credential-Expired"); value != "" {
		req.Context["credential_expired"] = value
	}
	decision, err := a.authorizer.Authorize(r.Context(), req)
	if err != nil {
		writeECSErrorStatus(w, http.StatusInternalServerError, "ServerException", err.Error())
		return false
	}
	if a.auditSink != nil {
		a.auditSink(decision)
	}
	if !decision.Allowed {
		writeECSErrorStatus(w, http.StatusForbidden, "AccessDenied", decision.Reason)
		return false
	}
	return true
}

func (a *ECSAdapter) taskDefinition(value string) (ecsTaskDefinition, bool) {
	_, taskDefinition, ok := a.taskDefinitionWithARN(value)
	return taskDefinition, ok
}

func ecsValidTaskDefinitionStatus(status string) bool {
	return status == "ACTIVE" || status == "INACTIVE"
}

func (a *ECSAdapter) taskDefinitionWithARN(value string) (string, ecsTaskDefinition, bool) {
	if value == "" {
		return "", ecsTaskDefinition{}, false
	}
	if taskDefinition, ok := a.taskDefinitions[ecsTaskDefinitionARN(value)]; ok {
		return ecsTaskDefinitionARN(value), taskDefinition, true
	}
	var latest ecsTaskDefinition
	found := false
	for _, taskDefinition := range a.taskDefinitions {
		if taskDefinition.Family == value && taskDefinition.Status == "ACTIVE" && (!found || taskDefinition.Revision > latest.Revision) {
			latest = taskDefinition
			found = true
		}
	}
	if found {
		arn := ecsTaskDefinitionARN(latest.Family + ":" + strconv.Itoa(latest.Revision))
		return arn, latest, true
	}
	return "", ecsTaskDefinition{}, false
}

func (a *ECSAdapter) serviceByARN(arn string) (string, ecsService, bool) {
	for key, service := range a.services {
		if ecsServiceARN(service) == arn {
			return key, service, true
		}
	}
	return "", ecsService{}, false
}

func ecsCluster(body map[string]any) string {
	if cluster := ecsField(body, "Cluster", "cluster"); cluster != "" {
		return cluster
	}
	return "default"
}

func ecsLaunchType(body map[string]any) string {
	if value := ecsField(body, "LaunchType", "launchType"); value != "" {
		return value
	}
	return "EXTERNAL"
}

func ecsSchedulingStrategy(body map[string]any) string {
	if value := ecsField(body, "SchedulingStrategy", "schedulingStrategy"); value != "" {
		return value
	}
	return "REPLICA"
}

func ecsValidSchedulingStrategy(value string) bool {
	return value == "REPLICA" || value == "DAEMON"
}

func ecsValidDeploymentController(value any) bool {
	if value == nil {
		return true
	}
	controller, _ := value.(map[string]any)
	switch ecsField(controller, "Type", "type") {
	case "", "ECS", "CODE_DEPLOY", "EXTERNAL":
		return true
	default:
		return false
	}
}

func ecsValidDeploymentConfiguration(value any) bool {
	if value == nil {
		return true
	}
	config, _ := value.(map[string]any)
	if maximum, ok := ecsInt(ecsBodyValue(config, "MaximumPercent", "maximumPercent")); ok && (maximum < 100 || maximum > 200) {
		return false
	}
	if minimum, ok := ecsInt(ecsBodyValue(config, "MinimumHealthyPercent", "minimumHealthyPercent")); ok && (minimum < 0 || minimum > 100) {
		return false
	}
	return true
}

func ecsValidAvailabilityZoneRebalancing(value string) bool {
	return value == "" || value == "ENABLED" || value == "DISABLED"
}

func ecsValidPropagateTags(value string) bool {
	return value == "" || value == "TASK_DEFINITION" || value == "SERVICE" || value == "NONE"
}

func ecsValidNetworkConfiguration(value any) bool {
	config, _ := value.(map[string]any)
	awsvpc, _ := ecsBodyValue(config, "AwsvpcConfiguration", "awsvpcConfiguration").(map[string]any)
	assignPublicIP := ecsField(awsvpc, "AssignPublicIp", "assignPublicIp")
	return assignPublicIP == "" || assignPublicIP == "ENABLED" || assignPublicIP == "DISABLED"
}

func ecsValidPlacement(constraintsValue, strategyValue any) bool {
	constraints, _ := constraintsValue.([]any)
	for _, item := range constraints {
		constraint, _ := item.(map[string]any)
		switch ecsField(constraint, "Type", "type") {
		case "", "distinctInstance", "memberOf":
		default:
			return false
		}
	}
	strategies, _ := strategyValue.([]any)
	for _, item := range strategies {
		strategy, _ := item.(map[string]any)
		switch ecsField(strategy, "Type", "type") {
		case "", "random", "spread", "binpack":
		default:
			return false
		}
	}
	return true
}

func ecsValidCapacityProviderStrategy(value any) bool {
	items, _ := value.([]any)
	for _, item := range items {
		strategy, _ := item.(map[string]any)
		if base, ok := ecsInt(ecsBodyValue(strategy, "Base", "base")); ok && (base < 0 || base > 100000) {
			return false
		}
		if weight, ok := ecsInt(ecsBodyValue(strategy, "Weight", "weight")); ok && (weight < 0 || weight > 1000) {
			return false
		}
	}
	return true
}

func ecsUpdateTriggersDeployment(body map[string]any) bool {
	for _, name := range []string{"TaskDefinition", "taskDefinition", "LoadBalancers", "loadBalancers", "NetworkConfiguration", "networkConfiguration", "PlatformVersion", "platformVersion", "ServiceRegistries", "serviceRegistries", "VolumeConfigurations", "volumeConfigurations"} {
		if _, ok := body[name]; ok {
			return true
		}
	}
	return false
}

func ecsValidLaunchType(value string) bool {
	return value == "EC2" || value == "FARGATE" || value == "EXTERNAL" || value == "MANAGED_INSTANCES"
}

func ecsDescribeResource(cluster string, names []string) string {
	if len(names) == 0 {
		return ecsClusterARN(cluster)
	}
	return ecsServiceARN(ecsService{Cluster: cluster, Name: ecsServiceName(names[0])})
}

func ecsField(body map[string]any, names ...string) string {
	return stringValue(ecsBodyValue(body, names...))
}

func ecsBodyValue(body map[string]any, names ...string) any {
	for _, name := range names {
		if value, ok := body[name]; ok {
			return value
		}
	}
	return nil
}

func ecsStringList(value any) []string {
	switch items := value.(type) {
	case []string:
		return items
	case []any:
		out := make([]string, 0, len(items))
		for _, item := range items {
			if value := stringValue(item); value != "" {
				out = append(out, value)
			}
		}
		return out
	case string:
		if items == "" {
			return nil
		}
		return strings.Split(items, ",")
	default:
		return nil
	}
}

func ecsContainerDefinitions(value any) []any {
	items, _ := value.([]any)
	return items
}

func ecsContainerDefinitionsComplete(value any) bool {
	for _, item := range ecsContainerDefinitions(value) {
		container, _ := item.(map[string]any)
		if ecsField(container, "Name", "name") == "" || ecsField(container, "Image", "image") == "" {
			return false
		}
	}
	return true
}

func ecsTaskDefinitionCompatibilities(body map[string]any) any {
	if value := ecsBodyValue(body, "RequiresCompatibilities", "requiresCompatibilities"); value != nil {
		return value
	}
	return []string{"EXTERNAL"}
}

func ecsStringPage(items []string, body map[string]any, defaultMax int) ([]string, string, bool) {
	start := 0
	if token := ecsBodyValue(body, "NextToken", "nextToken"); token != nil {
		parsed, err := strconv.Atoi(stringValue(token))
		if err != nil || parsed < 0 {
			return nil, "", false
		}
		start = parsed
	}
	if start > len(items) {
		start = len(items)
	}
	maxResults := defaultMax
	if value := ecsBodyValue(body, "MaxResults", "maxResults"); value != nil {
		parsed, ok := ecsInt(value)
		if !ok || parsed < 1 || parsed > 100 {
			return nil, "", false
		}
		maxResults = parsed
	}
	end := start + maxResults
	if end > len(items) {
		end = len(items)
	}
	next := ""
	if end < len(items) {
		next = strconv.Itoa(end)
	}
	return items[start:end], next, true
}

func ecsInt(value any) (int, bool) {
	switch value := value.(type) {
	case int:
		return value, true
	case int32:
		return int(value), true
	case int64:
		return int(value), true
	case float64:
		return int(value), true
	case string:
		parsed, err := strconv.Atoi(value)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func ecsServiceKey(cluster, name string) string {
	return cluster + "/" + name
}

func ecsServiceName(value string) string {
	if strings.Contains(value, "/service/") {
		return value[strings.LastIndex(value, "/")+1:]
	}
	return value
}

func ecsClusterARN(cluster string) string {
	return "arn:aws:ecs:us-east-1:000000000000:cluster/" + cluster
}

func ecsServiceARN(service ecsService) string {
	return ecsClusterARN(service.Cluster) + "/service/" + service.Name
}

func ecsServiceShape(service ecsService) map[string]any {
	taskDefinition := ecsTaskDefinitionARN(service.TaskDefinition)
	deploymentID := service.DeploymentID
	if deploymentID == "" {
		deploymentID = "homeport"
	}
	return map[string]any{
		"clusterArn":                    ecsClusterARN(service.Cluster),
		"serviceArn":                    ecsServiceARN(service),
		"serviceName":                   service.Name,
		"status":                        service.Status,
		"desiredCount":                  service.DesiredCount,
		"runningCount":                  service.DesiredCount,
		"pendingCount":                  0,
		"launchType":                    service.LaunchType,
		"availabilityZoneRebalancing":   service.AvailabilityRebal,
		"capacityProviderStrategy":      service.CapacityProviders,
		"deploymentController":          service.DeploymentController,
		"enableECSManagedTags":          service.EnableManagedTags,
		"healthCheckGracePeriodSeconds": service.HealthCheckGrace,
		"roleArn":                       service.RoleArn,
		"platformVersion":               service.PlatformVersion,
		"schedulingStrategy":            service.SchedulingStrategy,
		"enableExecuteCommand":          service.EnableExecuteCommand,
		"propagateTags":                 service.PropagateTags,
		"loadBalancers":                 service.LoadBalancers,
		"networkConfiguration":          service.NetworkConfig,
		"serviceRegistries":             service.ServiceRegistries,
		"placementConstraints":          service.PlacementConstraints,
		"placementStrategy":             service.PlacementStrategy,
		"deploymentConfiguration":       service.DeploymentConfig,
		"tags":                          ecsTagsJSON(service.Tags),
		"taskDefinition":                taskDefinition,
		"deployments": []map[string]any{{
			"id":                   deploymentID,
			"status":               "PRIMARY",
			"desiredCount":         service.DesiredCount,
			"runningCount":         service.DesiredCount,
			"pendingCount":         0,
			"rolloutState":         "COMPLETED",
			"taskDefinition":       taskDefinition,
			"volumeConfigurations": service.VolumeConfigs,
		}},
	}
}

func ecsTags(value any) map[string]string {
	tags := map[string]string{}
	items, _ := value.([]any)
	for _, item := range items {
		tag, _ := item.(map[string]any)
		key := stringValue(ecsBodyValue(tag, "Key", "key"))
		if key != "" {
			tags[key] = stringValue(ecsBodyValue(tag, "Value", "value"))
		}
	}
	return tags
}

func ecsValidTagKeys(tags map[string]string) bool {
	for key := range tags {
		if key == "" || strings.HasPrefix(strings.ToLower(key), "aws:") {
			return false
		}
	}
	return true
}

func ecsTagsJSON(tags map[string]string) []map[string]string {
	keys := make([]string, 0, len(tags))
	for key := range tags {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]map[string]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, map[string]string{"key": key, "value": tags[key]})
	}
	return out
}

func ecsTaskDefinitionShape(taskDefinition ecsTaskDefinition) map[string]any {
	return map[string]any{
		"taskDefinitionArn":       ecsTaskDefinitionARN(taskDefinition.Family + ":" + strconv.Itoa(taskDefinition.Revision)),
		"family":                  taskDefinition.Family,
		"revision":                taskDefinition.Revision,
		"status":                  taskDefinition.Status,
		"cpu":                     taskDefinition.Cpu,
		"memory":                  taskDefinition.Memory,
		"networkMode":             taskDefinition.NetworkMode,
		"executionRoleArn":        taskDefinition.ExecutionRoleArn,
		"taskRoleArn":             taskDefinition.TaskRoleArn,
		"containerDefinitions":    taskDefinition.ContainerDefinitions,
		"requiresCompatibilities": taskDefinition.RequiresCompatibilities,
		"runtimePlatform":         taskDefinition.RuntimePlatform,
		"volumes":                 taskDefinition.Volumes,
	}
}

func ecsTaskDefinitionARN(taskDefinition string) string {
	if strings.HasPrefix(taskDefinition, "arn:") {
		return taskDefinition
	}
	if taskDefinition == "" {
		taskDefinition = "homeport:1"
	}
	return "arn:aws:ecs:us-east-1:000000000000:task-definition/" + taskDefinition
}

func ecsTaskDefinitionListResource(prefix string) string {
	if prefix == "" {
		return ecsTaskDefinitionARN("*")
	}
	return ecsTaskDefinitionARN(prefix + "*")
}

func writeECSError(w http.ResponseWriter, code, message string) {
	writeECSErrorStatus(w, http.StatusBadRequest, code, message)
}

func writeECSErrorStatus(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"__type": code, "message": message})
}
