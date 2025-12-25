# AgnosTech v2 - Plan d'Implementation Adapte

**Status: Domain Layer Complete - Infrastructure Layer Needs Update**

## Fichiers Crees/Modifies

### Domain Layer (Complet)

```
internal/domain/
├── resource/
│   ├── category.go       # NEW - Categories normalisees (compute, storage, database...)
│   └── types.go           # UPDATED - Types GCP/Azure + Provider() method
├── parser/
│   ├── parser.go          # NEW - Interface Parser multi-provider
│   └── registry.go        # NEW - Registry pour parsers
├── target/
│   ├── target.go          # NEW - Platforms (docker-compose, scaleway, ovh...)
│   └── ha.go              # NEW - HA Levels (none, basic, multi-server, cluster, geo)
├── mapper/
│   ├── mapper.go          # CLEANED - Interface seulement, types dans result.go
│   └── result.go          # CLEANED - MappingResult, DockerService, etc.
└── generator/
    └── generator.go       # EXTENDED - TargetGenerator interface + Registry
```

### Infrastructure Layer (A Mettre a Jour)

Les mappers et generators existants utilisent l'ancienne API et doivent etre mis a jour:

1. **Mappers** - Utiliser `NewMappingResult()` et `result.DockerService.Name`
2. **Generators** - Utiliser `result.DockerService` au lieu de `result.Services`
3. **Parsers** - Implementer la nouvelle interface `parser.Parser`

---

## Analyse des Ecarts

### Architecture Existante vs Plan v2

| Aspect | Actuel | Plan v2 | Adaptation |
|--------|--------|---------|------------|
| **Resource Model** | `AWSResource` | `CloudResource` multi-provider | Renommer + ajouter `Provider` |
| **Parsers** | Terraform AWS only | AWS/GCP/Azure | Interface `InfrastructureParser` |
| **Mappers** | AWS -> Self-hosted | Multi-source -> Multi-target | Reorganiser en source/target |
| **Registry** | Single registry | Multi-provider | Composite registry |
| **Generators** | Docker Compose | Compose/Swarm/K8s/Cloud | Interface `TargetGenerator` |
| **HA System** | Non existant | 5 niveaux | Nouveau module |

---

## Phase 1: Abstraction du Resource Model

### 1.1 Renommer AWSResource -> CloudResource

```go
// internal/domain/resource/resource.go

type CloudResource struct {
    ID           string
    Name         string
    Provider     Provider              // AWS, GCP, Azure
    Type         Type                  // aws_s3_bucket, google_storage_bucket, etc.
    Category     Category              // Normalized: object_storage, database, etc.
    ARN          string                // AWS ARN ou equivalent
    Region       string
    Config       map[string]interface{}
    Tags         map[string]string
    Dependencies []string
    CreatedAt    time.Time
}
```

### 1.2 Ajouter les Categories Normalisees

```go
// internal/domain/resource/category.go

type Category string

const (
    CategoryCompute       Category = "compute"
    CategoryContainer     Category = "container"
    CategoryServerless    Category = "serverless"
    CategoryKubernetes    Category = "kubernetes"
    CategoryObjectStorage Category = "object_storage"
    CategoryBlockStorage  Category = "block_storage"
    CategorySQLDatabase   Category = "sql_database"
    CategoryNoSQLDatabase Category = "nosql_database"
    CategoryCache         Category = "cache"
    CategoryQueue         Category = "queue"
    CategoryPubSub        Category = "pubsub"
    CategoryLoadBalancer  Category = "load_balancer"
    CategoryCDN           Category = "cdn"
    CategoryDNS           Category = "dns"
    CategoryAuth          Category = "auth"
    CategorySecrets       Category = "secrets"
    CategoryMonitoring    Category = "monitoring"
)

// CategoryMapping mappe les types specifiques vers les categories
var CategoryMapping = map[Type]Category{
    // AWS
    TypeEC2Instance:    CategoryCompute,
    TypeS3Bucket:       CategoryObjectStorage,
    TypeRDSInstance:    CategorySQLDatabase,
    TypeElastiCache:    CategoryCache,
    TypeSQSQueue:       CategoryQueue,
    TypeLambdaFunction: CategoryServerless,
    TypeALB:            CategoryLoadBalancer,
    TypeCognitoPool:    CategoryAuth,
    // GCP (a ajouter)
    TypeGCEInstance:    CategoryCompute,
    TypeGCSBucket:      CategoryObjectStorage,
    TypeCloudSQL:       CategorySQLDatabase,
    TypePubSubTopic:    CategoryPubSub,
    TypeCloudRun:       CategoryContainer,
    TypeCloudFunction:  CategoryServerless,
    // Azure (a ajouter)
    TypeAzureVM:        CategoryCompute,
    TypeBlobStorage:    CategoryObjectStorage,
    TypeAzureSQL:       CategorySQLDatabase,
    TypeServiceBus:     CategoryQueue,
}
```

---

## Phase 2: Abstraction des Parsers

### 2.1 Interface InfrastructureParser

```go
// internal/domain/parser/parser.go

package parser

type InfrastructureParser interface {
    // Provider retourne le provider supporte
    Provider() resource.Provider

    // Parse analyse l'infrastructure et retourne les ressources
    Parse(ctx context.Context, path string, opts *ParseOptions) (*resource.Infrastructure, error)

    // Validate verifie que le path est valide pour ce parser
    Validate(path string) error

    // SupportedFormats retourne les formats supportes (terraform, api, etc.)
    SupportedFormats() []Format

    // AutoDetect verifie si ce parser peut gerer le path
    AutoDetect(path string) (bool, float64) // bool + confidence score
}

type ParseOptions struct {
    IncludeSensitive bool              // Inclure les secrets
    FilterTypes      []resource.Type   // Filtrer par type
    FollowModules    bool              // Suivre les modules Terraform
    APICredentials   map[string]string // Pour parsing via API
}

type Format string

const (
    FormatTerraform   Format = "terraform"
    FormatTerragrunt  Format = "terragrunt"
    FormatAPI         Format = "api"
    FormatCloudFormation Format = "cloudformation"  // AWS
    FormatDeploymentManager Format = "deployment_manager" // GCP
    FormatARM         Format = "arm"  // Azure Resource Manager
)
```

### 2.2 Parser Registry

```go
// internal/domain/parser/registry.go

type ParserRegistry struct {
    mu      sync.RWMutex
    parsers map[resource.Provider][]InfrastructureParser
}

func (r *ParserRegistry) Register(p InfrastructureParser) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.parsers[p.Provider()] = append(r.parsers[p.Provider()], p)
}

func (r *ParserRegistry) AutoDetect(path string) (InfrastructureParser, error) {
    r.mu.RLock()
    defer r.mu.RUnlock()

    var best InfrastructureParser
    var bestScore float64

    for _, parsers := range r.parsers {
        for _, p := range parsers {
            if ok, score := p.AutoDetect(path); ok && score > bestScore {
                best = p
                bestScore = score
            }
        }
    }

    if best == nil {
        return nil, ErrNoParserFound
    }
    return best, nil
}
```

### 2.3 Implementation AWS (Refactoring)

```
internal/infrastructure/parser/
├── aws/
│   ├── terraform.go      # Terraform AWS parser (refactored from current)
│   ├── tfstate.go        # State file parsing
│   ├── hcl.go            # HCL parsing
│   └── api.go            # AWS API direct scanning (nouveau)
├── gcp/
│   ├── terraform.go      # Terraform GCP parser
│   └── api.go            # GCP API scanning
├── azure/
│   ├── terraform.go      # Terraform Azure parser
│   ├── arm.go            # ARM template parsing
│   └── api.go            # Azure API scanning
└── registry.go           # Parser registry init
```

---

## Phase 3: Abstraction des Mappers

### 3.1 Interface Mapper Etendue

```go
// internal/domain/mapper/mapper.go

type Mapper interface {
    // Identite
    SourceProvider() resource.Provider  // AWS, GCP, Azure
    SourceCategory() resource.Category  // object_storage, database, etc.
    TargetPlatform() target.Platform    // self-hosted, scaleway, etc.

    // Mapping
    Map(ctx context.Context, res *resource.CloudResource, opts *MappingOptions) (*MappingResult, error)

    // Validation
    Validate(res *resource.CloudResource) error
    CanMap(res *resource.CloudResource) bool

    // Dependances
    Dependencies() []resource.Category

    // Metadata
    Complexity() Complexity           // simple, moderate, complex
    RequiresManualSteps() bool
    SupportedHALevels() []target.HALevel
}

type MappingOptions struct {
    HALevel      target.HALevel
    TargetConfig *target.TargetConfig
    Variables    map[string]string
}

type Complexity string

const (
    ComplexitySimple   Complexity = "simple"
    ComplexityModerate Complexity = "moderate"
    ComplexityComplex  Complexity = "complex"
)
```

### 3.2 Structure des Mappers

```
internal/infrastructure/mapper/
├── aws/                           # Source: AWS
│   ├── self-hosted/              # Target: Self-hosted
│   │   ├── compute/
│   │   │   ├── ec2.go           # EC2 -> Docker
│   │   │   └── lambda.go        # Lambda -> OpenFaaS
│   │   ├── storage/
│   │   │   └── s3.go            # S3 -> MinIO
│   │   ├── database/
│   │   │   ├── rds.go           # RDS -> PostgreSQL/MySQL
│   │   │   └── elasticache.go   # ElastiCache -> Redis
│   │   ├── messaging/
│   │   │   └── sqs.go           # SQS -> RabbitMQ
│   │   ├── networking/
│   │   │   └── alb.go           # ALB -> Traefik
│   │   └── security/
│   │       └── cognito.go       # Cognito -> Keycloak
│   ├── scaleway/                 # Target: Scaleway
│   │   ├── compute.go           # EC2 -> Scaleway Instance
│   │   ├── storage.go           # S3 -> Scaleway Object Storage
│   │   └── database.go          # RDS -> Scaleway Managed DB
│   ├── ovh/                      # Target: OVH
│   └── hetzner/                  # Target: Hetzner
│
├── gcp/                          # Source: GCP
│   ├── self-hosted/
│   │   ├── compute/
│   │   │   ├── gce.go           # Compute Engine -> Docker
│   │   │   └── cloud_run.go     # Cloud Run -> Docker
│   │   ├── storage/
│   │   │   └── gcs.go           # Cloud Storage -> MinIO
│   │   ├── database/
│   │   │   ├── cloud_sql.go     # Cloud SQL -> PostgreSQL
│   │   │   └── firestore.go     # Firestore -> MongoDB
│   │   └── messaging/
│   │       └── pubsub.go        # Pub/Sub -> RabbitMQ/NATS
│   ├── scaleway/
│   └── hetzner/
│
├── azure/                        # Source: Azure
│   ├── self-hosted/
│   │   ├── compute/
│   │   │   └── vm.go            # Azure VM -> Docker
│   │   ├── storage/
│   │   │   └── blob.go          # Blob Storage -> MinIO
│   │   ├── database/
│   │   │   └── azure_sql.go     # Azure SQL -> PostgreSQL
│   │   └── messaging/
│   │       └── service_bus.go   # Service Bus -> RabbitMQ
│   ├── scaleway/
│   └── hetzner/
│
└── registry.go                   # Multi-provider registry
```

### 3.3 Composite Mapper Registry

```go
// internal/infrastructure/mapper/registry.go

type CompositeRegistry struct {
    mu       sync.RWMutex
    mappers  map[registryKey]Mapper
}

type registryKey struct {
    SourceProvider resource.Provider
    SourceCategory resource.Category
    TargetPlatform target.Platform
}

func (r *CompositeRegistry) Register(m Mapper) {
    r.mu.Lock()
    defer r.mu.Unlock()

    key := registryKey{
        SourceProvider: m.SourceProvider(),
        SourceCategory: m.SourceCategory(),
        TargetPlatform: m.TargetPlatform(),
    }
    r.mappers[key] = m
}

func (r *CompositeRegistry) Get(
    provider resource.Provider,
    category resource.Category,
    platform target.Platform,
) (Mapper, error) {
    r.mu.RLock()
    defer r.mu.RUnlock()

    key := registryKey{provider, category, platform}
    if m, ok := r.mappers[key]; ok {
        return m, nil
    }
    return nil, ErrNoMapperFound
}

func (r *CompositeRegistry) MapAll(
    ctx context.Context,
    infra *resource.Infrastructure,
    platform target.Platform,
    opts *MappingOptions,
) ([]*MappingResult, error) {
    var results []*MappingResult

    for _, res := range infra.Resources {
        category := resource.GetCategory(res.Type)
        mapper, err := r.Get(res.Provider, category, platform)
        if err != nil {
            // Log warning, continue
            continue
        }

        result, err := mapper.Map(ctx, res, opts)
        if err != nil {
            return nil, fmt.Errorf("mapping %s: %w", res.ID, err)
        }
        results = append(results, result)
    }

    return results, nil
}
```

---

## Phase 4: Abstraction des Generators

### 4.1 Interface TargetGenerator

```go
// internal/domain/generator/generator.go

type TargetGenerator interface {
    // Identite
    Platform() target.Platform
    Name() string
    Description() string

    // Generation
    Generate(ctx context.Context, results []*mapper.MappingResult, config *GeneratorConfig) (*Output, error)

    // Validation
    Validate(results []*mapper.MappingResult) error
    SupportedHALevels() []target.HALevel

    // Requirements
    RequiresCredentials() bool
    RequiredCredentials() []string
}

type GeneratorConfig struct {
    HALevel      target.HALevel
    OutputDir    string
    DryRun       bool
    Variables    map[string]string

    // Provider-specific
    Scaleway     *target.ScalewayConfig
    OVH          *target.OVHConfig
    Hetzner      *target.HetznerConfig
}

type Output struct {
    Files        map[string][]byte  // Tous les fichiers generes
    MainFile     string             // Fichier principal (docker-compose.yml, main.tf, etc.)

    // Organisation par type
    DockerFiles    map[string][]byte  // docker-compose.*.yml
    TerraformFiles map[string][]byte  // *.tf
    K8sManifests   map[string][]byte  // *.yaml
    AnsibleFiles   map[string][]byte  // playbook.yml, inventory, etc.
    Scripts        map[string][]byte  // *.sh
    Configs        map[string][]byte  // Configuration files
    Docs           map[string][]byte  // README, guides

    // Metadata
    Warnings     []string
    ManualSteps  []string
    EstimatedCost *CostEstimate
    Summary      string
    GeneratedAt  time.Time
}
```

### 4.2 Structure des Generators

```
internal/infrastructure/generator/
├── self-hosted/
│   ├── compose/
│   │   ├── generator.go         # Docker Compose (Level 0)
│   │   ├── services.go          # Service generation
│   │   ├── networks.go          # Network configuration
│   │   └── templates/
│   ├── swarm/
│   │   ├── generator.go         # Docker Swarm (Level 2-3)
│   │   ├── stack.go             # Stack generation
│   │   └── templates/
│   ├── k8s/
│   │   ├── generator.go         # Kubernetes/K3s (Level 3-4)
│   │   ├── manifests.go         # Manifest generation
│   │   ├── helm.go              # Helm chart generation
│   │   └── templates/
│   └── ansible/
│       ├── generator.go         # Ansible playbooks
│       └── templates/
│
├── scaleway/
│   ├── generator.go             # Scaleway Terraform
│   ├── compute.go               # Instance resources
│   ├── database.go              # RDB resources
│   ├── storage.go               # Object storage
│   ├── kubernetes.go            # Kapsule
│   └── templates/
│
├── ovh/
│   ├── generator.go             # OVH Terraform + OpenStack
│   └── templates/
│
├── hetzner/
│   ├── generator.go             # Hetzner Terraform
│   └── templates/
│
├── common/
│   ├── traefik.go               # Traefik config (shared)
│   ├── monitoring.go            # Prometheus/Grafana
│   ├── backup.go                # Backup scripts
│   └── docs.go                  # Documentation
│
└── registry.go                  # Generator registry
```

### 4.3 Generator Registry

```go
// internal/infrastructure/generator/registry.go

type GeneratorRegistry struct {
    mu         sync.RWMutex
    generators map[target.Platform]TargetGenerator
}

func NewRegistry() *GeneratorRegistry {
    r := &GeneratorRegistry{
        generators: make(map[target.Platform]TargetGenerator),
    }
    r.registerDefaults()
    return r
}

func (r *GeneratorRegistry) registerDefaults() {
    // Self-hosted
    r.Register(compose.New())
    r.Register(swarm.New())
    r.Register(k8s.New())

    // EU Cloud
    r.Register(scaleway.New())
    r.Register(ovh.New())
    r.Register(hetzner.New())
}
```

---

## Phase 5: Systeme HA Levels

### 5.1 Domain Model

```go
// internal/domain/target/ha.go

package target

type HALevel string

const (
    HALevelNone        HALevel = "none"         // Level 0: Single server
    HALevelBasic       HALevel = "basic"        // Level 1: Backups + monitoring
    HALevelMultiServer HALevel = "multi-server" // Level 2: Active-passive
    HALevelCluster     HALevel = "cluster"      // Level 3: Active-active
    HALevelGeo         HALevel = "geo"          // Level 4: Multi-datacenter
)

type HAConfig struct {
    Level           HALevel

    // Level 1+
    BackupSchedule  string  // cron expression
    BackupRetention int     // days

    // Level 2+
    ServerCount     int
    FloatingIP      bool

    // Level 3+
    DBReplicas      int
    StorageReplicas int

    // Level 4
    Datacenters     []string
    GeoDNS          bool
}

func (h HALevel) RequiresMultiServer() bool {
    return h == HALevelMultiServer || h == HALevelCluster || h == HALevelGeo
}

func (h HALevel) RequiresCluster() bool {
    return h == HALevelCluster || h == HALevelGeo
}

func (h HALevel) RequiresGeo() bool {
    return h == HALevelGeo
}

// HARequirements pour chaque niveau
var HARequirements = map[HALevel]HARequirement{
    HALevelNone: {
        MinServers:  1,
        DBReplicas:  0,
        RTO:         "hours",
        RPO:         "hours",
        Description: "Single server with manual recovery",
    },
    HALevelBasic: {
        MinServers:  1,
        DBReplicas:  1,  // Async replica
        RTO:         "1h",
        RPO:         "minutes",
        Description: "Single server with automated backups",
    },
    HALevelMultiServer: {
        MinServers:  2,
        DBReplicas:  1,  // Sync replica
        RTO:         "minutes",
        RPO:         "seconds",
        Description: "Active-passive with floating IP failover",
    },
    HALevelCluster: {
        MinServers:  3,
        DBReplicas:  2,  // Cluster mode
        RTO:         "seconds",
        RPO:         "~0",
        Description: "Active-active cluster with load balancing",
    },
    HALevelGeo: {
        MinServers:  4,   // 2+ per DC
        DBReplicas:  3,
        RTO:         "seconds",
        RPO:         "seconds",
        Description: "Multi-datacenter with geo-redundancy",
    },
}

type HARequirement struct {
    MinServers  int
    DBReplicas  int
    RTO         string
    RPO         string
    Description string
}
```

### 5.2 HA-Aware Generation

```go
// internal/infrastructure/generator/self-hosted/compose/ha.go

type HAEnhancer struct {
    level  target.HALevel
    config *target.HAConfig
}

func (h *HAEnhancer) Enhance(services map[string]*DockerService) map[string]*DockerService {
    switch h.level {
    case target.HALevelBasic:
        return h.addBackupServices(services)
    case target.HALevelMultiServer:
        return h.addReplicationServices(services)
    case target.HALevelCluster:
        return h.addClusterServices(services)
    case target.HALevelGeo:
        return h.addGeoServices(services)
    default:
        return services
    }
}

func (h *HAEnhancer) addBackupServices(services map[string]*DockerService) map[string]*DockerService {
    // Ajouter restic/borg pour backups
    services["backup"] = &DockerService{
        Image: "restic/restic:latest",
        // ...
    }

    // Ajouter monitoring
    services["prometheus"] = &DockerService{
        Image: "prom/prometheus:latest",
        // ...
    }

    return services
}

func (h *HAEnhancer) addClusterServices(services map[string]*DockerService) map[string]*DockerService {
    // Transformer PostgreSQL en Patroni cluster
    if db, ok := services["postgres"]; ok {
        services["postgres"] = h.toPatroniCluster(db)
    }

    // Transformer Redis en cluster
    if redis, ok := services["redis"]; ok {
        services["redis"] = h.toRedisSentinel(redis)
    }

    // Transformer MinIO en distributed mode
    if minio, ok := services["minio"]; ok {
        services["minio"] = h.toMinioCluster(minio)
    }

    // Ajouter PgPool pour connection pooling
    services["pgpool"] = &DockerService{
        Image: "bitnami/pgpool:latest",
        // ...
    }

    return services
}
```

---

## Phase 6: CLI Extensions

### 6.1 Nouvelles Commandes

```go
// internal/cli/root.go

func init() {
    rootCmd.AddCommand(
        analyzeCmd(),
        migrateCmd(),
        validateCmd(),
        compareCmd(),    // Nouveau: compare sources/targets
        estimateCmd(),   // Nouveau: estimation des couts
        versionCmd(),
    )
}
```

### 6.2 Flags Etendus

```go
// internal/cli/migrate.go

func migrateCmd() *cobra.Command {
    var (
        sourceFlag      string  // --source=aws|gcp|azure|auto
        targetFlag      string  // --target=self-hosted|scaleway|ovh|hetzner
        haFlag          string  // --ha=none|basic|multi-server|cluster|geo
        deploymentFlag  string  // --deployment=compose|swarm|k8s
        outputFlag      string
        interactiveFlag bool
        dryRunFlag      bool
    )

    cmd := &cobra.Command{
        Use:   "migrate [path]",
        Short: "Migrate cloud infrastructure to target platform",
        Example: `
  # Auto-detect source, migrate to self-hosted
  agnostech migrate ./terraform

  # Explicit source and target
  agnostech migrate ./terraform --source=aws --target=scaleway

  # With HA configuration
  agnostech migrate ./terraform --target=self-hosted --ha=cluster --deployment=swarm

  # Interactive wizard
  agnostech migrate ./terraform --interactive
`,
        RunE: func(cmd *cobra.Command, args []string) error {
            // ...
        },
    }

    cmd.Flags().StringVarP(&sourceFlag, "source", "s", "auto",
        "Source cloud provider (aws, gcp, azure, auto)")
    cmd.Flags().StringVarP(&targetFlag, "target", "t", "self-hosted",
        "Target platform (self-hosted, scaleway, ovh, hetzner)")
    cmd.Flags().StringVar(&haFlag, "ha", "none",
        "HA level (none, basic, multi-server, cluster, geo)")
    cmd.Flags().StringVar(&deploymentFlag, "deployment", "compose",
        "Deployment type for self-hosted (compose, swarm, k8s)")
    cmd.Flags().StringVarP(&outputFlag, "output", "o", "./output",
        "Output directory")
    cmd.Flags().BoolVarP(&interactiveFlag, "interactive", "i", false,
        "Interactive wizard mode")
    cmd.Flags().BoolVar(&dryRunFlag, "dry-run", false,
        "Show what would be generated")

    return cmd
}
```

### 6.3 Mode Interactif

```go
// internal/cli/wizard/wizard.go

type MigrationWizard struct {
    infra *resource.Infrastructure

    // Selections
    sourceProvider  resource.Provider
    targetPlatform  target.Platform
    haLevel         target.HALevel
    deploymentType  string
}

func (w *MigrationWizard) Run() error {
    // Step 1: Show detected resources
    w.showResourceSummary()

    // Step 2: Confirm/select source provider
    if err := w.selectSource(); err != nil {
        return err
    }

    // Step 3: Select target platform
    if err := w.selectTarget(); err != nil {
        return err
    }

    // Step 4: Select HA level
    if err := w.selectHALevel(); err != nil {
        return err
    }

    // Step 5: Show mapping preview
    w.showMappingPreview()

    // Step 6: Estimate costs
    w.showCostEstimate()

    // Step 7: Confirm and generate
    if w.confirm() {
        return w.generate()
    }

    return nil
}
```

---

## Phase 7: Ordre d'Implementation

### Iteration 1: Refactoring Core (2-3 jours)
1. [ ] Renommer `AWSResource` -> `CloudResource`
2. [ ] Ajouter le systeme de `Category`
3. [ ] Creer l'interface `InfrastructureParser`
4. [ ] Refactorer le parser AWS existant
5. [ ] Mettre a jour les tests

### Iteration 2: Multi-Target Self-Hosted (3-4 jours)
1. [ ] Etendre l'interface `Mapper` avec source/target
2. [ ] Creer le `CompositeRegistry`
3. [ ] Ajouter le systeme HA Levels
4. [ ] Implementer le generator Docker Swarm
5. [ ] Implementer le generator K8s/K3s

### Iteration 3: Scaleway Target (2-3 jours)
1. [ ] Creer le generator Scaleway Terraform
2. [ ] Mapper AWS -> Scaleway pour compute, storage, database
3. [ ] Ajouter l'estimation des couts Scaleway
4. [ ] Tests avec infrastructure reelle

### Iteration 4: GCP Parser (3-4 jours)
1. [ ] Implementer le parser Terraform GCP
2. [ ] Creer les mappers GCP -> Self-hosted
3. [ ] Creer les mappers GCP -> Scaleway
4. [ ] Tests avec exemples GCP

### Iteration 5: Azure Parser (3-4 jours)
1. [ ] Implementer le parser Terraform Azure
2. [ ] Creer les mappers Azure -> Self-hosted
3. [ ] Creer les mappers Azure -> Scaleway
4. [ ] Tests avec exemples Azure

### Iteration 6: OVH & Hetzner (2-3 jours)
1. [ ] Implementer le generator OVH
2. [ ] Implementer le generator Hetzner
3. [ ] Mappers source -> OVH/Hetzner

### Iteration 7: CLI & Polish (2-3 jours)
1. [ ] Mode interactif (wizard)
2. [ ] Commande `compare`
3. [ ] Commande `estimate`
4. [ ] Documentation

---

## Fichiers a Creer/Modifier

### Nouveaux Fichiers
```
internal/domain/resource/category.go
internal/domain/parser/parser.go
internal/domain/parser/registry.go
internal/domain/target/ha.go
internal/domain/target/platform.go
internal/infrastructure/parser/aws/api.go
internal/infrastructure/parser/gcp/terraform.go
internal/infrastructure/parser/gcp/api.go
internal/infrastructure/parser/azure/terraform.go
internal/infrastructure/parser/azure/arm.go
internal/infrastructure/mapper/registry.go (refactor)
internal/infrastructure/generator/self-hosted/swarm/
internal/infrastructure/generator/self-hosted/k8s/
internal/infrastructure/generator/scaleway/
internal/infrastructure/generator/ovh/
internal/infrastructure/generator/hetzner/
internal/cli/wizard/
```

### Fichiers a Modifier
```
internal/domain/resource/resource.go     # AWSResource -> CloudResource
internal/domain/mapper/mapper.go         # Etendre interface
internal/domain/generator/generator.go   # Etendre interface
internal/infrastructure/parser/*         # Reorganiser en aws/
internal/infrastructure/mapper/*         # Reorganiser par source/target
internal/cli/migrate.go                  # Nouveaux flags
internal/cli/analyze.go                  # Support multi-provider
```

---

## Commencer Implementation

Pour demarrer, voici l'ordre recommande:

```bash
# 1. Creer la branche
git checkout -b feature/multi-cloud-v2

# 2. Phase 1 - Resource Model
# Modifier internal/domain/resource/

# 3. Phase 2 - Parser Abstraction
# Creer internal/domain/parser/

# 4. Tests au fur et a mesure
go test ./...
```

Voulez-vous que je commence l'implementation par une phase specifique?
