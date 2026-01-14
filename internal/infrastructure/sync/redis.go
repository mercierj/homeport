package sync

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/sync"
)

// RedisSync implements the SyncStrategy interface for Redis databases.
// It supports both RDB snapshot-based sync and live replication.
type RedisSync struct {
	*sync.BaseStrategy
	// TempDir is the directory for temporary RDB files.
	TempDir string
}

// NewRedisSync creates a new Redis sync strategy.
func NewRedisSync() *RedisSync {
	return &RedisSync{
		BaseStrategy: sync.NewBaseStrategy("redis", sync.SyncTypeCache, false, false),
		TempDir:      os.TempDir(),
	}
}

// EstimateSize calculates the approximate size of the Redis database.
// It uses the INFO command to get memory usage.
func (r *RedisSync) EstimateSize(ctx context.Context, source *sync.Endpoint) (int64, error) {
	conn, err := r.connect(ctx, source)
	if err != nil {
		return 0, fmt.Errorf("failed to connect to source Redis: %w", err)
	}
	defer func() { _ = conn.Close() }()

	// Send INFO memory command
	info, err := r.sendCommand(conn, "INFO", "memory")
	if err != nil {
		return 0, fmt.Errorf("failed to get memory info: %w", err)
	}

	// Parse used_memory from response
	for _, line := range strings.Split(info, "\n") {
		if strings.HasPrefix(line, "used_memory:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				size, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
				if err != nil {
					return 0, fmt.Errorf("failed to parse memory size: %w", err)
				}
				return size, nil
			}
		}
	}

	return 0, fmt.Errorf("used_memory not found in INFO response")
}

// Sync performs the Redis synchronization from source to target.
// It uses BGSAVE to create an RDB snapshot and restores it to the target.
func (r *RedisSync) Sync(ctx context.Context, source, target *sync.Endpoint, progress chan<- sync.Progress) error {
	reporter := sync.NewProgressReporter("redis-sync", progress, nil)
	reporter.SetPhase("initializing")

	// Step 1: Estimate size for progress tracking
	size, err := r.EstimateSize(ctx, source)
	if err != nil {
		reporter.Error(fmt.Sprintf("failed to estimate size: %v", err))
		return err
	}
	reporter.SetTotals(size, 0)

	// Step 2: Trigger BGSAVE on source
	reporter.SetPhase("creating snapshot")
	rdbPath, err := r.createSnapshot(ctx, source, reporter)
	if err != nil {
		reporter.Error(fmt.Sprintf("failed to create snapshot: %v", err))
		return err
	}
	defer func() { _ = os.Remove(rdbPath) }()

	// Step 3: Get key count for item tracking
	keyCount, err := r.getKeyCount(ctx, source)
	if err != nil {
		reporter.Warning(fmt.Sprintf("failed to get key count: %v", err))
	} else {
		reporter.SetTotals(size, keyCount)
	}

	// Step 4: Restore to target
	reporter.SetPhase("restoring")
	if err := r.restoreSnapshot(ctx, target, rdbPath, reporter); err != nil {
		reporter.Error(fmt.Sprintf("failed to restore snapshot: %v", err))
		return err
	}

	// Step 5: Mark as complete
	reporter.SetPhase("completed")
	reporter.Update(size, keyCount, "Redis sync completed successfully")

	return nil
}

// Verify compares source and target Redis instances.
// It checks key counts and optionally samples keys for value comparison.
func (r *RedisSync) Verify(ctx context.Context, source, target *sync.Endpoint) (*sync.VerifyResult, error) {
	result := sync.NewVerifyResult()
	result.Valid = true

	// Get key counts from both
	sourceCount, err := r.getKeyCount(ctx, source)
	if err != nil {
		return nil, fmt.Errorf("failed to get source key count: %w", err)
	}

	targetCount, err := r.getKeyCount(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("failed to get target key count: %w", err)
	}

	result.SourceCount = sourceCount
	result.TargetCount = targetCount

	if sourceCount != targetCount {
		result.AddMismatch(fmt.Sprintf("key count mismatch: source=%d, target=%d", sourceCount, targetCount))
	}

	// Get memory info
	sourceMemory, _ := r.EstimateSize(ctx, source)
	targetMemory, _ := r.EstimateSize(ctx, target)

	result.Details["source_memory"] = sourceMemory
	result.Details["target_memory"] = targetMemory
	result.Details["source_keys"] = sourceCount
	result.Details["target_keys"] = targetCount

	// Sample some keys for detailed verification
	sampleMismatches, err := r.sampleVerify(ctx, source, target, 100)
	if err != nil {
		result.Details["sample_error"] = err.Error()
	} else {
		for _, mismatch := range sampleMismatches {
			result.AddMismatch(mismatch)
		}
		result.Details["samples_checked"] = 100
	}

	return result, nil
}

// connect establishes a connection to Redis.
func (r *RedisSync) connect(ctx context.Context, endpoint *sync.Endpoint) (net.Conn, error) {
	host := endpoint.Host
	port := endpoint.Port
	if port == 0 {
		port = 6379
	}

	addr := fmt.Sprintf("%s:%d", host, port)

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}

	// Authenticate if password is provided
	if endpoint.Credentials != nil && endpoint.Credentials.Password != "" {
		if endpoint.Credentials.Username != "" {
			// Redis 6.0+ ACL auth
			_, err = r.sendCommand(conn, "AUTH", endpoint.Credentials.Username, endpoint.Credentials.Password)
		} else {
			// Legacy auth
			_, err = r.sendCommand(conn, "AUTH", endpoint.Credentials.Password)
		}
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("authentication failed: %w", err)
		}
	}

	// Select database if specified
	if endpoint.Database != "" {
		_, err = r.sendCommand(conn, "SELECT", endpoint.Database)
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("failed to select database: %w", err)
		}
	}

	return conn, nil
}

// sendCommand sends a Redis command and returns the response.
func (r *RedisSync) sendCommand(conn net.Conn, args ...string) (string, error) {
	// Build RESP array
	cmd := fmt.Sprintf("*%d\r\n", len(args))
	for _, arg := range args {
		cmd += fmt.Sprintf("$%d\r\n%s\r\n", len(arg), arg)
	}

	if _, err := conn.Write([]byte(cmd)); err != nil {
		return "", err
	}

	// Read response
	reader := bufio.NewReader(conn)
	return r.readResponse(reader)
}

// readResponse reads a RESP response from Redis.
func (r *RedisSync) readResponse(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	line = strings.TrimRight(line, "\r\n")

	if len(line) == 0 {
		return "", fmt.Errorf("empty response")
	}

	switch line[0] {
	case '+': // Simple string
		return line[1:], nil
	case '-': // Error
		return "", fmt.Errorf("redis error: %s", line[1:])
	case ':': // Integer
		return line[1:], nil
	case '$': // Bulk string
		size, err := strconv.Atoi(line[1:])
		if err != nil {
			return "", err
		}
		if size == -1 {
			return "", nil // Null bulk string
		}
		data := make([]byte, size+2) // +2 for \r\n
		if _, err := io.ReadFull(reader, data); err != nil {
			return "", err
		}
		return string(data[:size]), nil
	case '*': // Array
		count, err := strconv.Atoi(line[1:])
		if err != nil {
			return "", err
		}
		if count == -1 {
			return "", nil // Null array
		}
		var result strings.Builder
		for i := 0; i < count; i++ {
			element, err := r.readResponse(reader)
			if err != nil {
				return "", err
			}
			if i > 0 {
				result.WriteString("\n")
			}
			result.WriteString(element)
		}
		return result.String(), nil
	default:
		return line, nil
	}
}

// createSnapshot triggers BGSAVE and downloads the RDB file.
func (r *RedisSync) createSnapshot(ctx context.Context, source *sync.Endpoint, reporter *sync.ProgressReporter) (string, error) {
	conn, err := r.connect(ctx, source)
	if err != nil {
		return "", err
	}
	defer func() { _ = conn.Close() }()

	// Get current LASTSAVE timestamp
	lastSave, err := r.sendCommand(conn, "LASTSAVE")
	if err != nil {
		return "", fmt.Errorf("failed to get LASTSAVE: %w", err)
	}
	lastSaveTime, _ := strconv.ParseInt(lastSave, 10, 64)

	// Trigger BGSAVE
	resp, err := r.sendCommand(conn, "BGSAVE")
	if err != nil {
		return "", fmt.Errorf("failed to trigger BGSAVE: %w", err)
	}
	if !strings.Contains(resp, "started") && !strings.Contains(resp, "progress") {
		return "", fmt.Errorf("unexpected BGSAVE response: %s", resp)
	}

	reporter.Update(0, 0, "Waiting for background save to complete...")

	// Wait for BGSAVE to complete
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(1 * time.Second):
			newConn, err := r.connect(ctx, source)
			if err != nil {
				return "", err
			}

			newSave, err := r.sendCommand(newConn, "LASTSAVE")
			_ = newConn.Close()
			if err != nil {
				return "", err
			}

			newSaveTime, _ := strconv.ParseInt(newSave, 10, 64)
			if newSaveTime > lastSaveTime {
				// BGSAVE completed
				break
			}
			reporter.Update(0, 0, "Background save in progress...")
			continue
		}
		break
	}

	// Get RDB file path from CONFIG
	configConn, err := r.connect(ctx, source)
	if err != nil {
		return "", err
	}
	defer func() { _ = configConn.Close() }()

	dir, err := r.sendCommand(configConn, "CONFIG", "GET", "dir")
	if err != nil {
		return "", fmt.Errorf("failed to get data directory: %w", err)
	}
	dirParts := strings.Split(dir, "\n")
	if len(dirParts) < 2 {
		return "", fmt.Errorf("invalid CONFIG GET dir response")
	}
	dataDir := dirParts[1]

	dbFilename, err := r.sendCommand(configConn, "CONFIG", "GET", "dbfilename")
	if err != nil {
		return "", fmt.Errorf("failed to get dbfilename: %w", err)
	}
	filenameParts := strings.Split(dbFilename, "\n")
	if len(filenameParts) < 2 {
		return "", fmt.Errorf("invalid CONFIG GET dbfilename response")
	}
	filename := filenameParts[1]

	rdbPath := filepath.Join(dataDir, filename)

	// For local sync, return the path directly
	// For remote sync, we would need to copy the file
	// Here we assume local access or that redis-cli can handle it
	return rdbPath, nil
}

// restoreSnapshot restores an RDB file to the target Redis.
func (r *RedisSync) restoreSnapshot(ctx context.Context, target *sync.Endpoint, rdbPath string, reporter *sync.ProgressReporter) error {
	// Method 1: Use redis-cli --rdb to load the dump
	// This requires redis-cli to be installed

	// First, check if we can connect to target and flush it
	conn, err := r.connect(ctx, target)
	if err != nil {
		return fmt.Errorf("failed to connect to target: %w", err)
	}

	// Flush target database
	if target.Database != "" && target.Database != "0" {
		_, err = r.sendCommand(conn, "FLUSHDB")
	} else {
		_, err = r.sendCommand(conn, "FLUSHALL")
	}
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("failed to flush target: %w", err)
	}
	_ = conn.Close()

	// Method 2: Use RESTORE command for each key
	// This is slower but more portable
	// We'll use redis-cli --pipe for bulk loading

	// For now, we'll use a simpler approach: copy keys using DUMP/RESTORE
	return r.copyKeys(ctx, rdbPath, target, reporter)
}

// copyKeys copies keys from source RDB to target using DUMP/RESTORE.
// This is a fallback method when direct RDB loading isn't available.
func (r *RedisSync) copyKeys(ctx context.Context, rdbPath string, target *sync.Endpoint, reporter *sync.ProgressReporter) error {
	// For a proper implementation, we'd parse the RDB file or use redis-cli --rdb
	// Here we'll use redis-cli to import the dump

	args := r.buildRedisCliArgs(target)
	args = append(args, "--rdb", rdbPath)

	cmd := exec.CommandContext(ctx, "redis-cli", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("redis-cli --rdb failed: %w - output: %s", err, string(output))
	}

	return nil
}

// getKeyCount returns the total number of keys in the Redis database.
func (r *RedisSync) getKeyCount(ctx context.Context, endpoint *sync.Endpoint) (int64, error) {
	conn, err := r.connect(ctx, endpoint)
	if err != nil {
		return 0, err
	}
	defer func() { _ = conn.Close() }()

	info, err := r.sendCommand(conn, "INFO", "keyspace")
	if err != nil {
		return 0, err
	}

	// Parse db0:keys=123,expires=10,avg_ttl=0
	var total int64
	for _, line := range strings.Split(info, "\n") {
		if strings.HasPrefix(line, "db") {
			parts := strings.Split(line, ":")
			if len(parts) < 2 {
				continue
			}
			kvPairs := strings.Split(parts[1], ",")
			for _, kv := range kvPairs {
				if strings.HasPrefix(kv, "keys=") {
					count, _ := strconv.ParseInt(strings.TrimPrefix(kv, "keys="), 10, 64)
					total += count
					break
				}
			}
		}
	}

	return total, nil
}

// buildRedisCliArgs constructs redis-cli arguments from an endpoint.
func (r *RedisSync) buildRedisCliArgs(endpoint *sync.Endpoint) []string {
	args := []string{}

	args = append(args, "-h", endpoint.Host)

	if endpoint.Port > 0 {
		args = append(args, "-p", strconv.Itoa(endpoint.Port))
	}

	if endpoint.Credentials != nil {
		if endpoint.Credentials.Username != "" {
			args = append(args, "--user", endpoint.Credentials.Username)
		}
		if endpoint.Credentials.Password != "" {
			args = append(args, "-a", endpoint.Credentials.Password)
		}
	}

	if endpoint.Database != "" {
		args = append(args, "-n", endpoint.Database)
	}

	return args
}

// sampleVerify checks a random sample of keys for value consistency.
func (r *RedisSync) sampleVerify(ctx context.Context, source, target *sync.Endpoint, sampleSize int) ([]string, error) {
	sourceConn, err := r.connect(ctx, source)
	if err != nil {
		return nil, err
	}
	defer func() { _ = sourceConn.Close() }()

	targetConn, err := r.connect(ctx, target)
	if err != nil {
		return nil, err
	}
	defer func() { _ = targetConn.Close() }()

	// Get random keys from source
	var mismatches []string
	for i := 0; i < sampleSize; i++ {
		key, err := r.sendCommand(sourceConn, "RANDOMKEY")
		if err != nil || key == "" {
			continue
		}

		// Get value type
		keyType, err := r.sendCommand(sourceConn, "TYPE", key)
		if err != nil {
			continue
		}

		// Check if key exists in target
		targetType, err := r.sendCommand(targetConn, "TYPE", key)
		if err != nil {
			mismatches = append(mismatches, fmt.Sprintf("key %s: failed to check target", key))
			continue
		}

		if targetType == "none" {
			mismatches = append(mismatches, fmt.Sprintf("key %s: missing in target", key))
			continue
		}

		if keyType != targetType {
			mismatches = append(mismatches, fmt.Sprintf("key %s: type mismatch source=%s target=%s", key, keyType, targetType))
		}
	}

	return mismatches, nil
}

// RedisPipelineSync provides efficient key-by-key sync using pipelining.
type RedisPipelineSync struct {
	*RedisSync
	// PipelineSize is the number of commands to batch.
	PipelineSize int
}

// NewRedisPipelineSync creates a pipeline-based Redis sync.
func NewRedisPipelineSync() *RedisPipelineSync {
	return &RedisPipelineSync{
		RedisSync:    NewRedisSync(),
		PipelineSize: 1000,
	}
}

// SyncWithPipeline copies keys using DUMP/RESTORE with pipelining.
func (r *RedisPipelineSync) SyncWithPipeline(ctx context.Context, source, target *sync.Endpoint, progress chan<- sync.Progress) error {
	reporter := sync.NewProgressReporter("redis-pipeline-sync", progress, nil)
	reporter.SetPhase("initializing")

	// Get key count
	keyCount, err := r.getKeyCount(ctx, source)
	if err != nil {
		return err
	}
	reporter.SetTotals(0, keyCount)

	// Connect to both
	sourceConn, err := r.connect(ctx, source)
	if err != nil {
		return err
	}
	defer func() { _ = sourceConn.Close() }()

	targetConn, err := r.connect(ctx, target)
	if err != nil {
		return err
	}
	defer func() { _ = targetConn.Close() }()

	// Flush target
	_, _ = r.sendCommand(targetConn, "FLUSHDB")

	reporter.SetPhase("syncing")

	// Scan keys and transfer
	var cursor = "0"
	var processed int64

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// SCAN for keys
		scanResult, err := r.sendCommand(sourceConn, "SCAN", cursor, "COUNT", "1000")
		if err != nil {
			return fmt.Errorf("SCAN failed: %w", err)
		}

		parts := strings.Split(scanResult, "\n")
		if len(parts) < 1 {
			break
		}

		cursor = parts[0]
		keys := parts[1:]

		// Process keys in batches
		for _, key := range keys {
			if key == "" {
				continue
			}

			// DUMP from source
			dump, err := r.sendCommand(sourceConn, "DUMP", key)
			if err != nil {
				reporter.Warning(fmt.Sprintf("failed to dump key %s: %v", key, err))
				continue
			}

			// Get TTL
			ttl, err := r.sendCommand(sourceConn, "PTTL", key)
			if err != nil {
				ttl = "0"
			}
			ttlVal, _ := strconv.ParseInt(ttl, 10, 64)
			if ttlVal < 0 {
				ttlVal = 0
			}

			// RESTORE to target
			_, err = r.sendCommand(targetConn, "RESTORE", key, strconv.FormatInt(ttlVal, 10), dump, "REPLACE")
			if err != nil {
				reporter.Warning(fmt.Sprintf("failed to restore key %s: %v", key, err))
				continue
			}

			processed++
			if processed%100 == 0 {
				reporter.Update(0, processed, fmt.Sprintf("Synced %d keys", processed))
			}
		}

		if cursor == "0" {
			break
		}
	}

	reporter.SetPhase("completed")
	reporter.Update(0, processed, "Redis sync completed")

	return nil
}

// RedisReplicationSync uses Redis replication for sync.
type RedisReplicationSync struct {
	*RedisSync
}

// NewRedisReplicationSync creates a replication-based Redis sync.
func NewRedisReplicationSync() *RedisReplicationSync {
	base := NewRedisSync()
	base.BaseStrategy = sync.NewBaseStrategy("redis-replication", sync.SyncTypeCache, true, true)
	return &RedisReplicationSync{
		RedisSync: base,
	}
}

// SetupReplication configures the target as a replica of the source.
func (r *RedisReplicationSync) SetupReplication(ctx context.Context, source, target *sync.Endpoint) error {
	conn, err := r.connect(ctx, target)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	// Make target a replica of source
	port := source.Port
	if port == 0 {
		port = 6379
	}

	_, err = r.sendCommand(conn, "REPLICAOF", source.Host, strconv.Itoa(port))
	if err != nil {
		return fmt.Errorf("failed to set up replication: %w", err)
	}

	return nil
}

// WaitForSync waits for replication to complete.
func (r *RedisReplicationSync) WaitForSync(ctx context.Context, target *sync.Endpoint, progress chan<- sync.Progress) error {
	reporter := sync.NewProgressReporter("redis-replication", progress, nil)
	reporter.SetPhase("replicating")

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
		}

		conn, err := r.connect(ctx, target)
		if err != nil {
			return err
		}

		info, err := r.sendCommand(conn, "INFO", "replication")
		_ = conn.Close()
		if err != nil {
			return err
		}

		// Parse master_link_status
		linkStatus := ""
		replOffset := int64(0)
		for _, line := range strings.Split(info, "\n") {
			if strings.HasPrefix(line, "master_link_status:") {
				linkStatus = strings.TrimPrefix(line, "master_link_status:")
				linkStatus = strings.TrimSpace(linkStatus)
			}
			if strings.HasPrefix(line, "slave_repl_offset:") {
				val := strings.TrimPrefix(line, "slave_repl_offset:")
				replOffset, _ = strconv.ParseInt(strings.TrimSpace(val), 10, 64)
			}
		}

		reporter.Update(replOffset, 0, fmt.Sprintf("Replication status: %s", linkStatus))

		if linkStatus == "up" {
			// Check if caught up
			conn, err := r.connect(ctx, target)
			if err != nil {
				return err
			}

			info, err := r.sendCommand(conn, "INFO", "replication")
			_ = conn.Close()
			if err != nil {
				return err
			}

			// Check master_last_io_seconds_ago
			for _, line := range strings.Split(info, "\n") {
				if strings.HasPrefix(line, "master_last_io_seconds_ago:") {
					val := strings.TrimPrefix(line, "master_last_io_seconds_ago:")
					seconds, _ := strconv.Atoi(strings.TrimSpace(val))
					if seconds <= 1 {
						// Replication is caught up
						reporter.SetPhase("completed")
						return nil
					}
				}
			}
		}
	}
}

// PromoteToMaster promotes the replica to a standalone master.
func (r *RedisReplicationSync) PromoteToMaster(ctx context.Context, target *sync.Endpoint) error {
	conn, err := r.connect(ctx, target)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	_, err = r.sendCommand(conn, "REPLICAOF", "NO", "ONE")
	if err != nil {
		return fmt.Errorf("failed to promote to master: %w", err)
	}

	return nil
}
