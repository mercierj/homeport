// Package database provides mappers for AWS database services.
package database

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
)

// RDSClusterMapper converts AWS RDS Aurora clusters to PostgreSQL/MySQL with replication.
type RDSClusterMapper struct {
	*mapper.BaseMapper
}

// NewRDSClusterMapper creates a new RDS Cluster mapper.
func NewRDSClusterMapper() *RDSClusterMapper {
	return &RDSClusterMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeRDSCluster, nil),
	}
}

// Map converts an RDS Aurora cluster to a PostgreSQL or MySQL service with replication setup.
func (m *RDSClusterMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	engine := res.GetConfigString("engine")
	dbName := res.GetConfigString("database_name")
	if dbName == "" {
		dbName = res.Name
	}
	clusterID := res.GetConfigString("cluster_identifier")
	if clusterID == "" {
		clusterID = res.Name
	}

	switch {
	case strings.Contains(engine, "aurora-postgresql"), strings.Contains(engine, "postgres"):
		return m.createPostgresClusterService(res, dbName, clusterID, engine)
	case strings.Contains(engine, "aurora-mysql"), strings.Contains(engine, "mysql"):
		return m.createMySQLClusterService(res, dbName, clusterID, engine)
	default:
		return nil, fmt.Errorf("unsupported Aurora engine: %s", engine)
	}
}

func (m *RDSClusterMapper) createPostgresClusterService(res *resource.AWSResource, dbName, clusterID, engine string) (*mapper.MappingResult, error) {
	engineVersion := res.GetConfigString("engine_version")
	version := "16"
	if engineVersion != "" {
		parts := strings.Split(engineVersion, ".")
		if len(parts) > 0 {
			version = parts[0]
		}
	}

	result := mapper.NewMappingResult("postgres-primary")
	svc := result.DockerService

	svc.Image = fmt.Sprintf("postgres:%s-alpine", version)
	svc.Environment = map[string]string{
		"POSTGRES_DB":       dbName,
		"POSTGRES_USER":     "postgres",
		"POSTGRES_PASSWORD": "changeme",
		"PGDATA":            "/var/lib/postgresql/data/pgdata",
	}
	svc.Ports = []string{"5432:5432"}
	svc.Volumes = []string{
		"./data/postgres-primary:/var/lib/postgresql/data",
		"./config/postgres/postgresql.conf:/etc/postgresql/postgresql.conf:ro",
		"./config/postgres/pg_hba.conf:/etc/postgresql/pg_hba.conf:ro",
	}
	svc.Command = []string{
		"postgres",
		"-c", "config_file=/etc/postgresql/postgresql.conf",
		"-c", "hba_file=/etc/postgresql/pg_hba.conf",
	}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD-SHELL", "pg_isready -U postgres"},
		Interval: 10 * time.Second,
		Timeout:  5 * time.Second,
		Retries:  5,
	}
	svc.Restart = "unless-stopped"
	svc.Labels = map[string]string{
		"homeport.source":  "aws_rds_cluster",
		"homeport.engine":  "postgres",
		"homeport.cluster": clusterID,
		"homeport.role":    "primary",
	}

	pgConfig := m.generatePostgresClusterConfig(res)
	result.AddConfig("config/postgres/postgresql.conf", []byte(pgConfig))

	pgHBA := m.generatePgHBAConfig()
	result.AddConfig("config/postgres/pg_hba.conf", []byte(pgHBA))

	replicaConfig := m.generatePostgresReplicaService(dbName, version)
	result.AddConfig("config/postgres/replica-service.yml", []byte(replicaConfig))

	replicationScript := m.generatePostgresReplicationSetup()
	result.AddScript("setup_replication.sh", []byte(replicationScript))

	migrationScript := m.generateClusterMigrationScript(dbName, clusterID, "postgres")
	result.AddScript("migrate_cluster.sh", []byte(migrationScript))

	result.AddWarning("Aurora clusters have built-in replication. See replica-service.yml for manual replication setup.")

	if res.GetConfigBool("storage_encrypted") {
		result.AddWarning("Cluster storage is encrypted. Configure encryption at rest for self-hosted deployment.")
	}

	if res.GetConfigBool("deletion_protection") {
		result.AddWarning("Deletion protection is enabled. Ensure proper backup procedures.")
	}

	if res.GetConfigBool("enable_http_endpoint") {
		result.AddWarning("Aurora Data API (HTTP endpoint) is enabled. This feature is not available in self-hosted PostgreSQL.")
	}

	result.AddManualStep("Update database credentials in docker-compose.yml")
	result.AddManualStep("Add replica service from config/postgres/replica-service.yml if HA is needed")
	result.AddManualStep("Run setup_replication.sh after primary is running to configure replication")
	result.AddManualStep("Import existing database dump from Aurora cluster")

	return result, nil
}

func (m *RDSClusterMapper) createMySQLClusterService(res *resource.AWSResource, dbName, clusterID, engine string) (*mapper.MappingResult, error) {
	engineVersion := res.GetConfigString("engine_version")
	version := "8.0"
	if engineVersion != "" {
		parts := strings.Split(engineVersion, ".")
		if len(parts) >= 2 {
			version = parts[0] + "." + parts[1]
		}
	}

	result := mapper.NewMappingResult("mysql-primary")
	svc := result.DockerService

	svc.Image = fmt.Sprintf("mysql:%s", version)
	svc.Environment = map[string]string{
		"MYSQL_ROOT_PASSWORD": "changeme",
		"MYSQL_DATABASE":      dbName,
		"MYSQL_USER":          "appuser",
		"MYSQL_PASSWORD":      "changeme",
	}
	svc.Ports = []string{"3306:3306"}
	svc.Volumes = []string{
		"./data/mysql-primary:/var/lib/mysql",
		"./config/mysql/my.cnf:/etc/mysql/conf.d/custom.cnf:ro",
	}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "mysqladmin", "ping", "-h", "localhost"},
		Interval: 10 * time.Second,
		Timeout:  5 * time.Second,
		Retries:  5,
	}
	svc.Restart = "unless-stopped"
	svc.Labels = map[string]string{
		"homeport.source":  "aws_rds_cluster",
		"homeport.engine":  "mysql",
		"homeport.cluster": clusterID,
		"homeport.role":    "primary",
	}

	mysqlConfig := m.generateMySQLClusterConfig(res)
	result.AddConfig("config/mysql/my.cnf", []byte(mysqlConfig))

	replicaConfig := m.generateMySQLReplicaService(dbName, version)
	result.AddConfig("config/mysql/replica-service.yml", []byte(replicaConfig))

	replicationScript := m.generateMySQLReplicationSetup()
	result.AddScript("setup_replication.sh", []byte(replicationScript))

	migrationScript := m.generateClusterMigrationScript(dbName, clusterID, "mysql")
	result.AddScript("migrate_cluster.sh", []byte(migrationScript))

	result.AddWarning("Aurora clusters have built-in replication. See replica-service.yml for manual replication setup.")

	if res.GetConfigBool("storage_encrypted") {
		result.AddWarning("Cluster storage is encrypted. Configure encryption at rest for self-hosted deployment.")
	}

	result.AddManualStep("Update database credentials in docker-compose.yml")
	result.AddManualStep("Add replica service from config/mysql/replica-service.yml if HA is needed")
	result.AddManualStep("Run setup_replication.sh after primary is running to configure replication")
	result.AddManualStep("Import existing database dump from Aurora cluster")

	return result, nil
}

func (m *RDSClusterMapper) generatePostgresClusterConfig(res *resource.AWSResource) string {
	return `# PostgreSQL Primary Configuration for Replication
shared_buffers = 1GB
effective_cache_size = 4GB
maintenance_work_mem = 256MB
work_mem = 16MB

max_connections = 200
superuser_reserved_connections = 3

# WAL and Replication Settings
wal_level = replica
max_wal_senders = 10
max_replication_slots = 10
wal_keep_size = 1GB
hot_standby = on
hot_standby_feedback = on

# WAL Archive (optional, for PITR)
# archive_mode = on
# archive_command = 'cp %p /var/lib/postgresql/archive/%f'

# Logging
log_destination = 'stderr'
logging_collector = on
log_directory = 'log'
log_filename = 'postgresql-%Y-%m-%d_%H%M%S.log'
log_connections = on
log_disconnections = on

# Checkpoints
checkpoint_timeout = 10min
checkpoint_completion_target = 0.9
max_wal_size = 4GB
min_wal_size = 1GB

# Autovacuum
autovacuum = on
autovacuum_max_workers = 3

timezone = 'UTC'
`
}

func (m *RDSClusterMapper) generatePgHBAConfig() string {
	return `# PostgreSQL Client Authentication Configuration
# TYPE  DATABASE        USER            ADDRESS                 METHOD

# Local connections
local   all             all                                     trust
host    all             all             127.0.0.1/32            scram-sha-256
host    all             all             ::1/128                 scram-sha-256

# Replication connections
host    replication     all             0.0.0.0/0               scram-sha-256
host    all             all             0.0.0.0/0               scram-sha-256
`
}

func (m *RDSClusterMapper) generatePostgresReplicaService(dbName, version string) string {
	return fmt.Sprintf(`# PostgreSQL Replica Service
# Add this to your docker-compose.yml for read replicas

postgres-replica:
  image: postgres:%s-alpine
  environment:
    POSTGRES_PASSWORD: changeme
    PGDATA: /var/lib/postgresql/data/pgdata
  volumes:
    - ./data/postgres-replica:/var/lib/postgresql/data
    - ./config/postgres/postgresql-replica.conf:/etc/postgresql/postgresql.conf:ro
  command: >
    bash -c "
      until pg_basebackup -h postgres-primary -D /var/lib/postgresql/data/pgdata -U postgres -Fp -Xs -P -R; do
        echo 'Waiting for primary...';
        sleep 5;
      done;
      postgres -c config_file=/etc/postgresql/postgresql.conf
    "
  depends_on:
    - postgres-primary
  networks:
    - homeport
  labels:
    homeport.role: replica
    homeport.database: %s
`, version, dbName)
}

func (m *RDSClusterMapper) generateMySQLClusterConfig(res *resource.AWSResource) string {
	return `[mysqld]
# MySQL Primary Configuration for Replication

# InnoDB Settings
innodb_buffer_pool_size = 1G
innodb_log_file_size = 512M
innodb_flush_log_at_trx_commit = 1
innodb_flush_method = O_DIRECT
innodb_file_per_table = 1

# Connection Settings
max_connections = 200

# Binary Logging for Replication
server_id = 1
log_bin = mysql-bin
binlog_format = ROW
binlog_expire_logs_seconds = 604800
sync_binlog = 1
gtid_mode = ON
enforce_gtid_consistency = ON

# Replication
log_slave_updates = ON
relay_log = relay-bin
relay_log_recovery = ON

# Performance Schema
performance_schema = ON

# Slow Query Log
slow_query_log = 1
slow_query_log_file = /var/log/mysql/slow-query.log
long_query_time = 2

# Character Set
character_set_server = utf8mb4
collation_server = utf8mb4_unicode_ci

# SQL Mode
sql_mode = STRICT_TRANS_TABLES,NO_ZERO_IN_DATE,NO_ZERO_DATE,ERROR_FOR_DIVISION_BY_ZERO,NO_ENGINE_SUBSTITUTION

[mysql]
default_character_set = utf8mb4

[client]
default_character_set = utf8mb4
`
}

func (m *RDSClusterMapper) generateMySQLReplicaService(dbName, version string) string {
	return fmt.Sprintf(`# MySQL Replica Service
# Add this to your docker-compose.yml for read replicas

mysql-replica:
  image: mysql:%s
  environment:
    MYSQL_ROOT_PASSWORD: changeme
  volumes:
    - ./data/mysql-replica:/var/lib/mysql
    - ./config/mysql/my-replica.cnf:/etc/mysql/conf.d/custom.cnf:ro
  command: >
    --server-id=2
    --log-bin=mysql-bin
    --relay-log=relay-bin
    --read-only=1
    --gtid-mode=ON
    --enforce-gtid-consistency=ON
  depends_on:
    - mysql-primary
  networks:
    - homeport
  labels:
    homeport.role: replica
    homeport.database: %s
`, version, dbName)
}

func (m *RDSClusterMapper) generatePostgresReplicationSetup() string {
	return `#!/bin/bash
# PostgreSQL Replication Setup Script
set -e

echo "PostgreSQL Replication Setup"
echo "============================"

PRIMARY_HOST="${PRIMARY_HOST:-postgres-primary}"
REPLICA_USER="replicator"
REPLICA_PASSWORD="${REPLICA_PASSWORD:-replicator_password}"

echo "Step 1: Create replication user on primary"
docker exec postgres-primary psql -U postgres -c "
CREATE USER $REPLICA_USER WITH REPLICATION ENCRYPTED PASSWORD '$REPLICA_PASSWORD';
"

echo "Step 2: Create replication slot"
docker exec postgres-primary psql -U postgres -c "
SELECT pg_create_physical_replication_slot('replica_slot_1');
"

echo "Replication setup complete!"
echo "Now add the replica service from config/postgres/replica-service.yml"
`
}

func (m *RDSClusterMapper) generateMySQLReplicationSetup() string {
	return `#!/bin/bash
# MySQL Replication Setup Script
set -e

echo "MySQL Replication Setup"
echo "======================="

PRIMARY_HOST="${PRIMARY_HOST:-mysql-primary}"
REPLICA_USER="replicator"
REPLICA_PASSWORD="${REPLICA_PASSWORD:-replicator_password}"

echo "Step 1: Create replication user on primary"
docker exec mysql-primary mysql -u root -pchangeme -e "
CREATE USER IF NOT EXISTS '$REPLICA_USER'@'%' IDENTIFIED BY '$REPLICA_PASSWORD';
GRANT REPLICATION SLAVE ON *.* TO '$REPLICA_USER'@'%';
FLUSH PRIVILEGES;
"

echo "Step 2: Get primary status"
docker exec mysql-primary mysql -u root -pchangeme -e "SHOW MASTER STATUS\G"

echo "Replication user created!"
echo "Now configure the replica to connect to primary using CHANGE MASTER TO command"
`
}

func (m *RDSClusterMapper) generateClusterMigrationScript(dbName, clusterID, engine string) string {
	if engine == "postgres" {
		return fmt.Sprintf(`#!/bin/bash
# Aurora PostgreSQL Cluster Migration Script
set -e

echo "Aurora PostgreSQL Migration"
echo "==========================="

CLUSTER_ENDPOINT="${CLUSTER_ENDPOINT:-%s.cluster-xxxxx.region.rds.amazonaws.com}"
DB_NAME="%s"
DB_USER="${DB_USER:-postgres}"

echo "Step 1: Create dump from Aurora cluster"
pg_dump -h "$CLUSTER_ENDPOINT" -U "$DB_USER" -d "$DB_NAME" -F c -f "/tmp/${DB_NAME}_aurora.backup"

echo "Step 2: Restore to local PostgreSQL"
pg_restore -h localhost -U postgres -d "$DB_NAME" -c -F c "/tmp/${DB_NAME}_aurora.backup"

echo "Migration complete!"
`, clusterID, dbName)
	}

	return fmt.Sprintf(`#!/bin/bash
# Aurora MySQL Cluster Migration Script
set -e

echo "Aurora MySQL Migration"
echo "======================"

CLUSTER_ENDPOINT="${CLUSTER_ENDPOINT:-%s.cluster-xxxxx.region.rds.amazonaws.com}"
DB_NAME="%s"
DB_USER="${DB_USER:-admin}"

echo "Step 1: Create dump from Aurora cluster"
mysqldump -h "$CLUSTER_ENDPOINT" -u "$DB_USER" -p --databases "$DB_NAME" \
  --single-transaction --routines --triggers --events \
  --set-gtid-purged=OFF > "/tmp/${DB_NAME}_aurora.sql"

echo "Step 2: Restore to local MySQL"
mysql -h localhost -u root -pchangeme < "/tmp/${DB_NAME}_aurora.sql"

echo "Migration complete!"
`, clusterID, dbName)
}
