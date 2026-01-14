// Package database provides shared utilities for database mapping.
package database

import "github.com/homeport/homeport/internal/infrastructure/mapper/shared/tls"

// DatabaseOptions represents the configuration options for a database instance.
// This struct provides a cloud-agnostic abstraction for database features.
type DatabaseOptions struct {
	// Basic properties
	Name    string
	Engine  string // "postgres", "mysql", "mariadb"
	Version string

	// Encryption - TLS/SSL
	TLSEnabled bool
	TLSOptions *tls.TLSOptions // TLS certificate generation options

	// Encryption - At Rest
	AtRestEnabled bool
	AtRestMethod  string // "pgcrypto", "luks"

	// Replication
	ReplicationEnabled bool
	ReplicationMode    string // "streaming", "logical", "binlog"
	ReplicaCount       int

	// Backup
	BackupEnabled    bool
	BackupRetention  int    // days
	BackupEncryption bool
	BackupSchedule   string // cron expression
	BackupLocation   string // directory for backups

	// Performance (from cloud instance)
	MaxConnections  int
	SharedBuffersMB int
	WorkMemMB       int

	// Cloud metadata (source info)
	CloudProvider string
	MultiAZ       bool
	InstanceClass string
	Region        string
}

// NewDatabaseOptions creates DatabaseOptions with defaults.
func NewDatabaseOptions(name, engine, version string) *DatabaseOptions {
	return &DatabaseOptions{
		Name:    name,
		Engine:  engine,
		Version: version,

		// Secure defaults
		TLSEnabled:    true,
		AtRestEnabled: true,
		AtRestMethod:  "luks",

		// Replication defaults
		ReplicationEnabled: false,
		ReplicationMode:    "",
		ReplicaCount:       0,

		// Backup defaults
		BackupEnabled:    true,
		BackupRetention:  7,
		BackupEncryption: true,
		BackupSchedule:   "0 2 * * *", // 2 AM daily
		BackupLocation:   "/var/lib/homeport/backups",

		// Performance defaults (conservative)
		MaxConnections:  100,
		SharedBuffersMB: 128,
		WorkMemMB:       4,
	}
}

// WithTLS enables or disables TLS with default options.
func (d *DatabaseOptions) WithTLS(enabled bool) *DatabaseOptions {
	if d == nil {
		return nil
	}
	d.TLSEnabled = enabled
	if enabled && d.TLSOptions == nil {
		// Create TLS options for this database service
		d.TLSOptions = tls.NewTLSOptions(d.Name)
	}
	return d
}

// WithTLSOptions sets custom TLS options.
func (d *DatabaseOptions) WithTLSOptions(opts *tls.TLSOptions) *DatabaseOptions {
	if d == nil {
		return nil
	}
	d.TLSEnabled = opts != nil
	d.TLSOptions = opts
	return d
}

// WithAtRestEncryption enables or disables at-rest encryption.
func (d *DatabaseOptions) WithAtRestEncryption(enabled bool, method string) *DatabaseOptions {
	if d == nil {
		return nil
	}
	d.AtRestEnabled = enabled
	if method != "" {
		d.AtRestMethod = method
	}
	return d
}

// WithReplication configures database replication.
func (d *DatabaseOptions) WithReplication(mode string, count int) *DatabaseOptions {
	if d == nil {
		return nil
	}
	d.ReplicationEnabled = mode != "" && count > 0
	d.ReplicationMode = mode
	d.ReplicaCount = count
	return d
}

// WithBackup configures backup settings.
func (d *DatabaseOptions) WithBackup(retentionDays int, encrypted bool) *DatabaseOptions {
	if d == nil {
		return nil
	}
	d.BackupEnabled = retentionDays > 0
	d.BackupRetention = retentionDays
	d.BackupEncryption = encrypted
	return d
}

// WithBackupSchedule sets the backup schedule and location.
func (d *DatabaseOptions) WithBackupSchedule(schedule, location string) *DatabaseOptions {
	if d == nil {
		return nil
	}
	if schedule != "" {
		d.BackupSchedule = schedule
	}
	if location != "" {
		d.BackupLocation = location
	}
	return d
}

// WithPerformance configures performance settings.
func (d *DatabaseOptions) WithPerformance(maxConn, sharedBuffers, workMem int) *DatabaseOptions {
	if d == nil {
		return nil
	}
	if maxConn > 0 {
		d.MaxConnections = maxConn
	}
	if sharedBuffers > 0 {
		d.SharedBuffersMB = sharedBuffers
	}
	if workMem > 0 {
		d.WorkMemMB = workMem
	}
	return d
}

// WithCloudMetadata sets the cloud provider metadata.
func (d *DatabaseOptions) WithCloudMetadata(provider, instanceClass, region string, multiAZ bool) *DatabaseOptions {
	if d == nil {
		return nil
	}
	d.CloudProvider = provider
	d.InstanceClass = instanceClass
	d.Region = region
	d.MultiAZ = multiAZ
	return d
}

// DatabaseOptionsFromRDS creates DatabaseOptions from AWS RDS configuration.
func DatabaseOptionsFromRDS(config map[string]interface{}) *DatabaseOptions {
	name := getStringValue(config, "db_instance_identifier", "db_name", "identifier")
	engine := getStringValue(config, "engine")
	version := getStringValue(config, "engine_version")

	opts := NewDatabaseOptions(name, engine, version)

	// Cloud metadata
	opts.CloudProvider = "aws"
	opts.InstanceClass = getStringValue(config, "instance_class", "db_instance_class")
	opts.Region = getStringValue(config, "region", "availability_zone")
	opts.MultiAZ = getBoolValue(config, "multi_az")

	// Encryption
	if encrypted := getBoolValue(config, "storage_encrypted"); encrypted {
		opts.AtRestEnabled = true
		opts.AtRestMethod = "luks"
	}

	// Backup
	if retention := getIntValue(config, "backup_retention_period"); retention > 0 {
		opts.BackupEnabled = true
		opts.BackupRetention = retention
		opts.BackupEncryption = opts.AtRestEnabled
	}

	// Replication
	if replicas := getIntValue(config, "read_replica_count"); replicas > 0 {
		opts.ReplicationEnabled = true
		opts.ReplicaCount = replicas
		// Determine replication mode based on engine
		switch engine {
		case "postgres":
			opts.ReplicationMode = "streaming"
		case "mysql", "mariadb":
			opts.ReplicationMode = "binlog"
		}
	}

	// Performance (estimate from instance class)
	opts.applyPerformanceFromInstanceClass(opts.InstanceClass)

	return opts
}

// DatabaseOptionsFromCloudSQL creates DatabaseOptions from GCP Cloud SQL configuration.
func DatabaseOptionsFromCloudSQL(config map[string]interface{}) *DatabaseOptions {
	name := getStringValue(config, "name", "instance_name")
	engine := mapCloudSQLEngine(getStringValue(config, "database_version"))
	version := extractVersionFromCloudSQL(getStringValue(config, "database_version"))

	opts := NewDatabaseOptions(name, engine, version)

	// Cloud metadata
	opts.CloudProvider = "gcp"
	opts.InstanceClass = getStringValue(config, "tier", "machine_type")
	opts.Region = getStringValue(config, "region")
	opts.MultiAZ = getStringValue(config, "availability_type") == "REGIONAL"

	// Settings block
	if settings, ok := config["settings"].(map[string]interface{}); ok {
		// Backup configuration
		if backupConfig, ok := settings["backup_configuration"].(map[string]interface{}); ok {
			opts.BackupEnabled = getBoolValue(backupConfig, "enabled")
			opts.BackupRetention = getIntValue(backupConfig, "transaction_log_retention_days")
			if opts.BackupRetention == 0 {
				opts.BackupRetention = 7 // default
			}
		}

		// IP configuration (for TLS)
		if ipConfig, ok := settings["ip_configuration"].(map[string]interface{}); ok {
			opts.TLSEnabled = getBoolValue(ipConfig, "require_ssl")
		}

		// Database flags for performance
		if flags, ok := settings["database_flags"].([]interface{}); ok {
			for _, flag := range flags {
				if f, ok := flag.(map[string]interface{}); ok {
					applyDatabaseFlag(opts, f)
				}
			}
		}

		// Tier for performance estimation
		if tier := getStringValue(settings, "tier"); tier != "" {
			opts.InstanceClass = tier
		}
	}

	// Replica configuration
	if replicaConfig, ok := config["replica_configuration"].(map[string]interface{}); ok {
		if replicaConfig != nil {
			opts.ReplicationEnabled = true
			opts.ReplicaCount = 1
			switch engine {
			case "postgres":
				opts.ReplicationMode = "streaming"
			case "mysql":
				opts.ReplicationMode = "binlog"
			}
		}
	}

	opts.applyPerformanceFromInstanceClass(opts.InstanceClass)

	return opts
}

// DatabaseOptionsFromAzureSQL creates DatabaseOptions from Azure SQL configuration.
func DatabaseOptionsFromAzureSQL(config map[string]interface{}) *DatabaseOptions {
	name := getStringValue(config, "name", "database_name")
	// Azure SQL is always SQL Server, but we map to postgres for self-hosted
	engine := "postgres"
	version := "15" // Default to latest stable

	// Check if it's actually a Postgres flexible server
	if serverType := getStringValue(config, "type"); serverType != "" {
		if contains(serverType, "postgres") {
			engine = "postgres"
			version = getStringValue(config, "version")
		} else if contains(serverType, "mysql") {
			engine = "mysql"
			version = getStringValue(config, "version")
		}
	}

	opts := NewDatabaseOptions(name, engine, version)

	// Cloud metadata
	opts.CloudProvider = "azure"
	opts.InstanceClass = getStringValue(config, "sku_name", "sku")
	opts.Region = getStringValue(config, "location", "region")

	// Zone redundancy
	if zoneRedundant := getBoolValue(config, "zone_redundant"); zoneRedundant {
		opts.MultiAZ = true
	}

	// Encryption
	if tde := getBoolValue(config, "transparent_data_encryption_enabled"); tde {
		opts.AtRestEnabled = true
		opts.AtRestMethod = "luks"
	}

	// Backup
	if retention := getIntValue(config, "backup_retention_days"); retention > 0 {
		opts.BackupEnabled = true
		opts.BackupRetention = retention
		opts.BackupEncryption = opts.AtRestEnabled
	}

	// Geo-redundant backup implies replication capability
	if geoRedundant := getBoolValue(config, "geo_redundant_backup_enabled"); geoRedundant {
		opts.ReplicationEnabled = true
		opts.ReplicaCount = 1
		opts.ReplicationMode = "streaming"
	}

	// Read replicas
	if replicas := getIntValue(config, "replica_count", "read_replica_count"); replicas > 0 {
		opts.ReplicationEnabled = true
		opts.ReplicaCount = replicas
		opts.ReplicationMode = "streaming"
	}

	opts.applyPerformanceFromInstanceClass(opts.InstanceClass)

	return opts
}

// applyPerformanceFromInstanceClass estimates performance settings from cloud instance class.
func (d *DatabaseOptions) applyPerformanceFromInstanceClass(instanceClass string) {
	if d == nil || instanceClass == "" {
		return
	}

	// Default conservative values
	memoryMB := 1024
	cpuCount := 1

	// Parse instance class to estimate resources
	// AWS: db.t3.micro, db.r5.large, etc.
	// GCP: db-f1-micro, db-custom-2-4096, etc.
	// Azure: GP_Gen5_2, BC_Gen5_4, etc.

	switch {
	// Micro instances
	case contains(instanceClass, "micro") || contains(instanceClass, "f1"):
		memoryMB = 512
		cpuCount = 1
	// Small instances
	case contains(instanceClass, "small") || contains(instanceClass, "t2") || contains(instanceClass, "t3"):
		memoryMB = 2048
		cpuCount = 2
	// Medium instances
	case contains(instanceClass, "medium") || contains(instanceClass, "m5"):
		memoryMB = 4096
		cpuCount = 2
	// Large instances
	case contains(instanceClass, "large") || contains(instanceClass, "r5") || contains(instanceClass, "GP_Gen5"):
		memoryMB = 8192
		cpuCount = 2
	// XLarge instances
	case contains(instanceClass, "xlarge") || contains(instanceClass, "BC_Gen5"):
		memoryMB = 16384
		cpuCount = 4
	// 2XLarge and above
	case contains(instanceClass, "2xlarge") || contains(instanceClass, "8xlarge"):
		memoryMB = 32768
		cpuCount = 8
	}

	// Apply PostgreSQL tuning guidelines
	// shared_buffers: ~25% of RAM
	d.SharedBuffersMB = memoryMB / 4

	// work_mem: ~4MB per CPU for OLTP workloads
	d.WorkMemMB = 4 * cpuCount

	// max_connections: based on available memory
	// Each connection uses ~5-10MB
	d.MaxConnections = minInt(200, memoryMB/10)
}

// Helper functions

func getStringValue(config map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if val, ok := config[key]; ok {
			if s, ok := val.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

func getBoolValue(config map[string]interface{}, keys ...string) bool {
	for _, key := range keys {
		if val, ok := config[key]; ok {
			switch v := val.(type) {
			case bool:
				return v
			case string:
				return v == "true" || v == "1" || v == "yes"
			}
		}
	}
	return false
}

func getIntValue(config map[string]interface{}, keys ...string) int {
	for _, key := range keys {
		if val, ok := config[key]; ok {
			switch v := val.(type) {
			case int:
				return v
			case int64:
				return int(v)
			case float64:
				return int(v)
			}
		}
	}
	return 0
}

func contains(s, substr string) bool {
	if len(s) < len(substr) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func mapCloudSQLEngine(version string) string {
	switch {
	case contains(version, "POSTGRES"):
		return "postgres"
	case contains(version, "MYSQL"):
		return "mysql"
	case contains(version, "SQLSERVER"):
		return "postgres" // Map to postgres for self-hosted
	default:
		return "postgres"
	}
}

func extractVersionFromCloudSQL(version string) string {
	// POSTGRES_15 -> 15
	// MYSQL_8_0 -> 8.0
	for i := len(version) - 1; i >= 0; i-- {
		if version[i] == '_' {
			result := version[i+1:]
			// Replace underscores with dots for version numbers
			var out []byte
			for _, c := range result {
				if c == '_' {
					out = append(out, '.')
				} else {
					out = append(out, byte(c))
				}
			}
			return string(out)
		}
	}
	return ""
}

func applyDatabaseFlag(opts *DatabaseOptions, flag map[string]interface{}) {
	name := getStringValue(flag, "name")
	value := getStringValue(flag, "value")

	switch name {
	case "max_connections":
		if v := getIntValue(flag, "value"); v > 0 {
			opts.MaxConnections = v
		}
	case "shared_buffers":
		// Parse value like "256MB"
		if v := parseSizeMB(value); v > 0 {
			opts.SharedBuffersMB = v
		}
	case "work_mem":
		if v := parseSizeMB(value); v > 0 {
			opts.WorkMemMB = v
		}
	}
}

func parseSizeMB(s string) int {
	if len(s) < 2 {
		return 0
	}

	// Find where the number ends
	numEnd := 0
	for i, c := range s {
		if c < '0' || c > '9' {
			numEnd = i
			break
		}
	}
	if numEnd == 0 {
		return 0
	}

	var n int
	for _, c := range s[:numEnd] {
		n = n*10 + int(c-'0')
	}

	suffix := s[numEnd:]
	switch suffix {
	case "MB", "mb", "M", "m":
		return n
	case "GB", "gb", "G", "g":
		return n * 1024
	case "KB", "kb", "K", "k":
		return n / 1024
	default:
		return n // Assume MB
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
