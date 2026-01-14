package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
)

// Service provides metrics collection and querying capabilities.
type Service struct {
	client *client.Client
	config *Config

	// In-memory storage for metrics with retention
	containerMetrics map[string]*containerMetricsStore
	systemMetrics    *systemMetricsStore
	mu               sync.RWMutex

	// Collection control
	ctx        context.Context
	cancel     context.CancelFunc
	collecting bool
	wg         sync.WaitGroup
}

// containerMetricsStore holds time-series data for a container.
type containerMetricsStore struct {
	containerName string
	dataPoints    []ContainerMetrics
	mu            sync.RWMutex
}

// systemMetricsStore holds time-series data for system metrics.
type systemMetricsStore struct {
	dataPoints []SystemMetrics
	mu         sync.RWMutex
}

// NewService creates a new metrics service.
func NewService(dockerClient *client.Client) (*Service, error) {
	return NewServiceWithConfig(dockerClient, nil)
}

// NewServiceWithConfig creates a new metrics service with custom configuration.
func NewServiceWithConfig(dockerClient *client.Client, cfg *Config) (*Service, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	// If no Docker client provided, try to create one
	if dockerClient == nil {
		opts := []client.Opt{client.WithAPIVersionNegotiation()}
		if host := findDockerHost(); host != "" {
			opts = append(opts, client.WithHost(host))
		}
		opts = append(opts, client.FromEnv)

		cli, err := client.NewClientWithOpts(opts...)
		if err != nil {
			return nil, fmt.Errorf("failed to create Docker client: %w", err)
		}
		dockerClient = cli
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Service{
		client:           dockerClient,
		config:           cfg,
		containerMetrics: make(map[string]*containerMetricsStore),
		systemMetrics:    &systemMetricsStore{dataPoints: make([]SystemMetrics, 0)},
		ctx:              ctx,
		cancel:           cancel,
	}, nil
}

// findDockerHost returns the Docker host URI based on the platform.
func findDockerHost() string {
	switch runtime.GOOS {
	case "windows":
		return "npipe:////./pipe/docker_engine"
	case "darwin":
		home, err := os.UserHomeDir()
		if err == nil {
			sock := filepath.Join(home, ".docker", "run", "docker.sock")
			if _, err := os.Stat(sock); err == nil {
				return "unix://" + sock
			}
			sock = filepath.Join(home, ".colima", "default", "docker.sock")
			if _, err := os.Stat(sock); err == nil {
				return "unix://" + sock
			}
		}
		if _, err := os.Stat("/var/run/docker.sock"); err == nil {
			return "unix:///var/run/docker.sock"
		}
	default:
		if _, err := os.Stat("/var/run/docker.sock"); err == nil {
			return "unix:///var/run/docker.sock"
		}
		if xdgRuntime := os.Getenv("XDG_RUNTIME_DIR"); xdgRuntime != "" {
			sock := filepath.Join(xdgRuntime, "docker.sock")
			if _, err := os.Stat(sock); err == nil {
				return "unix://" + sock
			}
		}
	}
	return ""
}

// Close stops the metrics collection and closes resources.
func (s *Service) Close() error {
	s.StopCollection()
	return s.client.Close()
}

// StartCollection starts the background metrics collection.
func (s *Service) StartCollection() {
	s.mu.Lock()
	if s.collecting {
		s.mu.Unlock()
		return
	}
	s.collecting = true
	s.mu.Unlock()

	s.wg.Add(1)
	go s.collectionLoop()
}

// StopCollection stops the background metrics collection.
func (s *Service) StopCollection() {
	s.mu.Lock()
	if !s.collecting {
		s.mu.Unlock()
		return
	}
	s.collecting = false
	s.mu.Unlock()

	s.cancel()
	s.wg.Wait()
}

// collectionLoop runs the periodic metrics collection.
func (s *Service) collectionLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.config.CollectionInterval)
	defer ticker.Stop()

	// Collect immediately on start
	s.collectMetrics()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.collectMetrics()
		}
	}
}

// collectMetrics collects all metrics (container and system).
func (s *Service) collectMetrics() {
	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
	defer cancel()

	// Collect container metrics
	if s.config.EnableContainerMetrics {
		s.collectContainerMetrics(ctx)
	}

	// Collect system metrics
	if s.config.EnableSystemMetrics {
		s.collectSystemMetrics(ctx)
	}

	// Cleanup old data
	s.cleanupOldData()
}

// collectContainerMetrics collects metrics from all running containers.
func (s *Service) collectContainerMetrics(ctx context.Context) {
	containers, err := s.client.ContainerList(ctx, container.ListOptions{All: false})
	if err != nil {
		return
	}

	for _, c := range containers {
		stats, err := s.client.ContainerStatsOneShot(ctx, c.ID)
		if err != nil {
			continue
		}

		var statsJSON container.StatsResponse
		if err := json.NewDecoder(stats.Body).Decode(&statsJSON); err != nil {
			_ = stats.Body.Close()
			continue
		}
		_ = stats.Body.Close()

		metrics := s.parseContainerStats(c.ID, c.Names, &statsJSON)
		s.storeContainerMetrics(c.ID, metrics)
	}
}

// parseContainerStats converts Docker stats to our metrics format.
func (s *Service) parseContainerStats(containerID string, names []string, stats *container.StatsResponse) ContainerMetrics {
	name := ""
	if len(names) > 0 {
		name = strings.TrimPrefix(names[0], "/")
	}

	// Calculate CPU percentage
	cpuPercent := 0.0
	cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage - stats.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(stats.CPUStats.SystemUsage - stats.PreCPUStats.SystemUsage)
	if systemDelta > 0 && cpuDelta > 0 {
		cpuPercent = (cpuDelta / systemDelta) * float64(stats.CPUStats.OnlineCPUs) * 100.0
	}

	// Calculate memory percentage
	memPercent := 0.0
	if stats.MemoryStats.Limit > 0 {
		memPercent = (float64(stats.MemoryStats.Usage) / float64(stats.MemoryStats.Limit)) * 100.0
	}

	// Calculate network I/O
	var rxBytes, txBytes, rxPackets, txPackets, rxErrors, txErrors, rxDropped, txDropped uint64
	for _, network := range stats.Networks {
		rxBytes += network.RxBytes
		txBytes += network.TxBytes
		rxPackets += network.RxPackets
		txPackets += network.TxPackets
		rxErrors += network.RxErrors
		txErrors += network.TxErrors
		rxDropped += network.RxDropped
		txDropped += network.TxDropped
	}

	// Calculate disk I/O
	var readBytes, writeBytes, readOps, writeOps uint64
	for _, io := range stats.BlkioStats.IoServiceBytesRecursive {
		switch io.Op {
		case "read", "Read":
			readBytes += io.Value
		case "write", "Write":
			writeBytes += io.Value
		}
	}
	for _, io := range stats.BlkioStats.IoServicedRecursive {
		switch io.Op {
		case "read", "Read":
			readOps += io.Value
		case "write", "Write":
			writeOps += io.Value
		}
	}

	return ContainerMetrics{
		ContainerID:   containerID[:12],
		ContainerName: name,
		Timestamp:     time.Now(),
		CPU: CPUMetrics{
			UsagePercent:     cpuPercent,
			UsageTotal:       stats.CPUStats.CPUUsage.TotalUsage,
			SystemUsage:      stats.CPUStats.SystemUsage,
			OnlineCPUs:       stats.CPUStats.OnlineCPUs,
			ThrottledPeriods: stats.CPUStats.ThrottlingData.ThrottledPeriods,
			ThrottledTime:    stats.CPUStats.ThrottlingData.ThrottledTime,
		},
		Memory: MemoryMetrics{
			UsageBytes:   stats.MemoryStats.Usage,
			LimitBytes:   stats.MemoryStats.Limit,
			UsagePercent: memPercent,
			CacheBytes:   stats.MemoryStats.Stats["cache"],
			RSSBytes:     stats.MemoryStats.Stats["rss"],
		},
		Network: NetworkMetrics{
			RxBytes:   rxBytes,
			RxPackets: rxPackets,
			RxErrors:  rxErrors,
			RxDropped: rxDropped,
			TxBytes:   txBytes,
			TxPackets: txPackets,
			TxErrors:  txErrors,
			TxDropped: txDropped,
		},
		DiskIO: DiskIOMetrics{
			ReadBytes:  readBytes,
			WriteBytes: writeBytes,
			ReadOps:    readOps,
			WriteOps:   writeOps,
		},
	}
}

// storeContainerMetrics stores metrics for a container.
func (s *Service) storeContainerMetrics(containerID string, metrics ContainerMetrics) {
	s.mu.Lock()
	defer s.mu.Unlock()

	store, exists := s.containerMetrics[containerID]
	if !exists {
		store = &containerMetricsStore{
			containerName: metrics.ContainerName,
			dataPoints:    make([]ContainerMetrics, 0, s.config.MaxDataPoints),
		}
		s.containerMetrics[containerID] = store
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	store.dataPoints = append(store.dataPoints, metrics)

	// Limit data points
	if len(store.dataPoints) > s.config.MaxDataPoints {
		store.dataPoints = store.dataPoints[len(store.dataPoints)-s.config.MaxDataPoints:]
	}
}

// collectSystemMetrics collects host system metrics.
func (s *Service) collectSystemMetrics(ctx context.Context) {
	metrics := SystemMetrics{
		Timestamp: time.Now(),
	}

	// CPU metrics
	cpuPercent, err := cpu.PercentWithContext(ctx, 0, false)
	if err == nil && len(cpuPercent) > 0 {
		metrics.CPU.UsagePercent = cpuPercent[0]
	}

	cpuTimes, err := cpu.TimesWithContext(ctx, false)
	if err == nil && len(cpuTimes) > 0 {
		total := cpuTimes[0].User + cpuTimes[0].System + cpuTimes[0].Idle + cpuTimes[0].Iowait
		if total > 0 {
			metrics.CPU.UserPercent = (cpuTimes[0].User / total) * 100
			metrics.CPU.SystemPercent = (cpuTimes[0].System / total) * 100
			metrics.CPU.IdlePercent = (cpuTimes[0].Idle / total) * 100
			metrics.CPU.IOWaitPercent = (cpuTimes[0].Iowait / total) * 100
		}
	}

	cpuCorePercent, err := cpu.PercentWithContext(ctx, 0, true)
	if err == nil {
		metrics.CPU.CoreUsage = cpuCorePercent
		metrics.CPU.NumCores = len(cpuCorePercent)
	}

	// Memory metrics
	memInfo, err := mem.VirtualMemoryWithContext(ctx)
	if err == nil {
		metrics.Memory = SystemMemoryMetrics{
			TotalBytes:     memInfo.Total,
			UsedBytes:      memInfo.Used,
			FreeBytes:      memInfo.Free,
			AvailableBytes: memInfo.Available,
			BuffersBytes:   memInfo.Buffers,
			CachedBytes:    memInfo.Cached,
			UsagePercent:   memInfo.UsedPercent,
		}
	}

	swapInfo, err := mem.SwapMemoryWithContext(ctx)
	if err == nil {
		metrics.Memory.SwapTotalBytes = swapInfo.Total
		metrics.Memory.SwapUsedBytes = swapInfo.Used
		metrics.Memory.SwapFreeBytes = swapInfo.Free
	}

	// Disk metrics
	partitions, err := disk.PartitionsWithContext(ctx, false)
	if err == nil {
		for _, partition := range partitions {
			usage, err := disk.UsageWithContext(ctx, partition.Mountpoint)
			if err == nil {
				metrics.Disk = append(metrics.Disk, SystemDiskMetrics{
					MountPoint:   partition.Mountpoint,
					Device:       partition.Device,
					FSType:       partition.Fstype,
					TotalBytes:   usage.Total,
					UsedBytes:    usage.Used,
					FreeBytes:    usage.Free,
					UsagePercent: usage.UsedPercent,
					InodesTotal:  usage.InodesTotal,
					InodesUsed:   usage.InodesUsed,
					InodesFree:   usage.InodesFree,
				})
			}
		}
	}

	// Load averages
	loadAvg, err := load.AvgWithContext(ctx)
	if err == nil {
		metrics.Load = LoadMetrics{
			Load1:  loadAvg.Load1,
			Load5:  loadAvg.Load5,
			Load15: loadAvg.Load15,
		}
	}

	s.storeSystemMetrics(metrics)
}

// storeSystemMetrics stores system metrics.
func (s *Service) storeSystemMetrics(metrics SystemMetrics) {
	s.systemMetrics.mu.Lock()
	defer s.systemMetrics.mu.Unlock()

	s.systemMetrics.dataPoints = append(s.systemMetrics.dataPoints, metrics)

	// Limit data points
	if len(s.systemMetrics.dataPoints) > s.config.MaxDataPoints {
		s.systemMetrics.dataPoints = s.systemMetrics.dataPoints[len(s.systemMetrics.dataPoints)-s.config.MaxDataPoints:]
	}
}

// cleanupOldData removes metrics older than the retention period.
func (s *Service) cleanupOldData() {
	cutoff := time.Now().Add(-s.config.RetentionPeriod)

	// Clean container metrics
	s.mu.Lock()
	for containerID, store := range s.containerMetrics {
		store.mu.Lock()
		newPoints := make([]ContainerMetrics, 0, len(store.dataPoints))
		for _, point := range store.dataPoints {
			if point.Timestamp.After(cutoff) {
				newPoints = append(newPoints, point)
			}
		}
		store.dataPoints = newPoints
		store.mu.Unlock()

		// Remove empty stores
		if len(store.dataPoints) == 0 {
			delete(s.containerMetrics, containerID)
		}
	}
	s.mu.Unlock()

	// Clean system metrics
	s.systemMetrics.mu.Lock()
	newPoints := make([]SystemMetrics, 0, len(s.systemMetrics.dataPoints))
	for _, point := range s.systemMetrics.dataPoints {
		if point.Timestamp.After(cutoff) {
			newPoints = append(newPoints, point)
		}
	}
	s.systemMetrics.dataPoints = newPoints
	s.systemMetrics.mu.Unlock()
}

// GetContainerMetrics returns the latest metrics for a specific container.
func (s *Service) GetContainerMetrics(ctx context.Context, containerID string) (*ContainerMetrics, error) {
	s.mu.RLock()
	store, exists := s.containerMetrics[containerID]
	s.mu.RUnlock()

	if !exists {
		// Try to collect metrics on demand
		containers, err := s.client.ContainerList(ctx, container.ListOptions{All: false})
		if err != nil {
			return nil, fmt.Errorf("failed to list containers: %w", err)
		}

		for _, c := range containers {
			if strings.HasPrefix(c.ID, containerID) {
				stats, err := s.client.ContainerStatsOneShot(ctx, c.ID)
				if err != nil {
					return nil, fmt.Errorf("failed to get container stats: %w", err)
				}

				var statsJSON container.StatsResponse
				if err := json.NewDecoder(stats.Body).Decode(&statsJSON); err != nil {
					_ = stats.Body.Close()
					return nil, fmt.Errorf("failed to decode stats: %w", err)
				}
				_ = stats.Body.Close()

				metrics := s.parseContainerStats(c.ID, c.Names, &statsJSON)
				return &metrics, nil
			}
		}
		return nil, fmt.Errorf("container not found: %s", containerID)
	}

	store.mu.RLock()
	defer store.mu.RUnlock()

	if len(store.dataPoints) == 0 {
		return nil, fmt.Errorf("no metrics available for container: %s", containerID)
	}

	latest := store.dataPoints[len(store.dataPoints)-1]
	return &latest, nil
}

// GetAllContainerMetrics returns the latest metrics for all containers.
func (s *Service) GetAllContainerMetrics(ctx context.Context) ([]ContainerMetrics, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]ContainerMetrics, 0, len(s.containerMetrics))
	for _, store := range s.containerMetrics {
		store.mu.RLock()
		if len(store.dataPoints) > 0 {
			result = append(result, store.dataPoints[len(store.dataPoints)-1])
		}
		store.mu.RUnlock()
	}

	return result, nil
}

// GetSystemMetrics returns the latest system metrics.
func (s *Service) GetSystemMetrics(ctx context.Context) (*SystemMetrics, error) {
	s.systemMetrics.mu.RLock()
	pointCount := len(s.systemMetrics.dataPoints)
	s.systemMetrics.mu.RUnlock()

	if pointCount == 0 {
		// Collect on demand
		s.collectSystemMetrics(ctx)
	}

	s.systemMetrics.mu.RLock()
	defer s.systemMetrics.mu.RUnlock()

	if len(s.systemMetrics.dataPoints) == 0 {
		return nil, fmt.Errorf("no system metrics available")
	}

	latest := s.systemMetrics.dataPoints[len(s.systemMetrics.dataPoints)-1]
	return &latest, nil
}

// GetMetricsHistory returns historical metrics for a container.
func (s *Service) GetMetricsHistory(ctx context.Context, containerID string, timeRange TimeRange, resolution Resolution) (*ContainerMetricsHistory, error) {
	if containerID == "" {
		return nil, fmt.Errorf("container ID is required")
	}

	s.mu.RLock()
	store, exists := s.containerMetrics[containerID]
	s.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("no metrics found for container: %s", containerID)
	}

	store.mu.RLock()
	defer store.mu.RUnlock()

	// Filter data points by time range
	filteredPoints := make([]ContainerMetrics, 0)
	for _, point := range store.dataPoints {
		if (point.Timestamp.Equal(timeRange.Start) || point.Timestamp.After(timeRange.Start)) &&
			(point.Timestamp.Equal(timeRange.End) || point.Timestamp.Before(timeRange.End)) {
			filteredPoints = append(filteredPoints, point)
		}
	}

	// Aggregate by resolution
	resolutionDuration := parseResolution(resolution)
	aggregatedPoints := aggregateContainerMetrics(filteredPoints, resolutionDuration)

	// Build history response
	history := &ContainerMetricsHistory{
		ContainerID:   containerID,
		ContainerName: store.containerName,
		TimeRange:     timeRange,
		CPU: MetricsHistory{
			ContainerID: containerID,
			MetricType:  "cpu_percent",
			TimeRange:   timeRange,
			Resolution:  resolution,
		},
		Memory: MetricsHistory{
			ContainerID: containerID,
			MetricType:  "memory_bytes",
			TimeRange:   timeRange,
			Resolution:  resolution,
		},
		NetworkRx: MetricsHistory{
			ContainerID: containerID,
			MetricType:  "network_rx_bytes",
			TimeRange:   timeRange,
			Resolution:  resolution,
		},
		NetworkTx: MetricsHistory{
			ContainerID: containerID,
			MetricType:  "network_tx_bytes",
			TimeRange:   timeRange,
			Resolution:  resolution,
		},
		DiskRead: MetricsHistory{
			ContainerID: containerID,
			MetricType:  "disk_read_bytes",
			TimeRange:   timeRange,
			Resolution:  resolution,
		},
		DiskWrite: MetricsHistory{
			ContainerID: containerID,
			MetricType:  "disk_write_bytes",
			TimeRange:   timeRange,
			Resolution:  resolution,
		},
	}

	// Build data points for each metric type
	for _, point := range aggregatedPoints {
		history.CPU.DataPoints = append(history.CPU.DataPoints, MetricPoint{
			Timestamp: point.Timestamp,
			Value:     point.CPU.UsagePercent,
		})
		history.Memory.DataPoints = append(history.Memory.DataPoints, MetricPoint{
			Timestamp: point.Timestamp,
			Value:     float64(point.Memory.UsageBytes),
		})
		history.NetworkRx.DataPoints = append(history.NetworkRx.DataPoints, MetricPoint{
			Timestamp: point.Timestamp,
			Value:     float64(point.Network.RxBytes),
		})
		history.NetworkTx.DataPoints = append(history.NetworkTx.DataPoints, MetricPoint{
			Timestamp: point.Timestamp,
			Value:     float64(point.Network.TxBytes),
		})
		history.DiskRead.DataPoints = append(history.DiskRead.DataPoints, MetricPoint{
			Timestamp: point.Timestamp,
			Value:     float64(point.DiskIO.ReadBytes),
		})
		history.DiskWrite.DataPoints = append(history.DiskWrite.DataPoints, MetricPoint{
			Timestamp: point.Timestamp,
			Value:     float64(point.DiskIO.WriteBytes),
		})
	}

	// Calculate statistics
	history.CPU.Statistics = calculateStats(history.CPU.DataPoints)
	history.Memory.Statistics = calculateStats(history.Memory.DataPoints)
	history.NetworkRx.Statistics = calculateStats(history.NetworkRx.DataPoints)
	history.NetworkTx.Statistics = calculateStats(history.NetworkTx.DataPoints)
	history.DiskRead.Statistics = calculateStats(history.DiskRead.DataPoints)
	history.DiskWrite.Statistics = calculateStats(history.DiskWrite.DataPoints)

	return history, nil
}

// GetSystemMetricsHistory returns historical system metrics.
func (s *Service) GetSystemMetricsHistory(ctx context.Context, timeRange TimeRange, resolution Resolution) (*SystemMetricsHistory, error) {
	s.systemMetrics.mu.RLock()
	defer s.systemMetrics.mu.RUnlock()

	// Filter data points by time range
	filteredPoints := make([]SystemMetrics, 0)
	for _, point := range s.systemMetrics.dataPoints {
		if (point.Timestamp.Equal(timeRange.Start) || point.Timestamp.After(timeRange.Start)) &&
			(point.Timestamp.Equal(timeRange.End) || point.Timestamp.Before(timeRange.End)) {
			filteredPoints = append(filteredPoints, point)
		}
	}

	// Aggregate by resolution
	resolutionDuration := parseResolution(resolution)
	aggregatedPoints := aggregateSystemMetrics(filteredPoints, resolutionDuration)

	history := &SystemMetricsHistory{
		TimeRange: timeRange,
		CPU: MetricsHistory{
			MetricType: "cpu_percent",
			TimeRange:  timeRange,
			Resolution: resolution,
		},
		Memory: MetricsHistory{
			MetricType: "memory_percent",
			TimeRange:  timeRange,
			Resolution: resolution,
		},
		Disk: MetricsHistory{
			MetricType: "disk_percent",
			TimeRange:  timeRange,
			Resolution: resolution,
		},
		Load: MetricsHistory{
			MetricType: "load1",
			TimeRange:  timeRange,
			Resolution: resolution,
		},
	}

	for _, point := range aggregatedPoints {
		history.CPU.DataPoints = append(history.CPU.DataPoints, MetricPoint{
			Timestamp: point.Timestamp,
			Value:     point.CPU.UsagePercent,
		})
		history.Memory.DataPoints = append(history.Memory.DataPoints, MetricPoint{
			Timestamp: point.Timestamp,
			Value:     point.Memory.UsagePercent,
		})

		// Calculate average disk usage
		diskPercent := 0.0
		if len(point.Disk) > 0 {
			for _, d := range point.Disk {
				diskPercent += d.UsagePercent
			}
			diskPercent /= float64(len(point.Disk))
		}
		history.Disk.DataPoints = append(history.Disk.DataPoints, MetricPoint{
			Timestamp: point.Timestamp,
			Value:     diskPercent,
		})

		history.Load.DataPoints = append(history.Load.DataPoints, MetricPoint{
			Timestamp: point.Timestamp,
			Value:     point.Load.Load1,
		})
	}

	history.CPU.Statistics = calculateStats(history.CPU.DataPoints)
	history.Memory.Statistics = calculateStats(history.Memory.DataPoints)
	history.Disk.Statistics = calculateStats(history.Disk.DataPoints)
	history.Load.Statistics = calculateStats(history.Load.DataPoints)

	return history, nil
}

// GetMetricsSummary returns a summary of all metrics.
func (s *Service) GetMetricsSummary(ctx context.Context, timeRange TimeRange) (*MetricsSummary, error) {
	summary := &MetricsSummary{
		Timestamp: time.Now(),
		TimeRange: timeRange,
	}

	// System metrics summary
	s.systemMetrics.mu.RLock()
	systemPoints := filterSystemMetricsByTime(s.systemMetrics.dataPoints, timeRange)
	s.systemMetrics.mu.RUnlock()

	if len(systemPoints) > 0 {
		cpuValues := make([]float64, 0, len(systemPoints))
		for _, p := range systemPoints {
			cpuValues = append(cpuValues, p.CPU.UsagePercent)
		}
		summary.System.CPUAvgPercent = average(cpuValues)
		summary.System.CPUMaxPercent = maxFloat64(cpuValues)

		latest := systemPoints[len(systemPoints)-1]
		summary.System.MemoryUsedBytes = latest.Memory.UsedBytes
		summary.System.MemoryTotalBytes = latest.Memory.TotalBytes
		summary.System.MemoryPercent = latest.Memory.UsagePercent
		summary.System.Load1 = latest.Load.Load1
		summary.System.Load5 = latest.Load.Load5
		summary.System.Load15 = latest.Load.Load15

		// Sum disk usage
		for _, d := range latest.Disk {
			summary.System.DiskUsedBytes += d.UsedBytes
			summary.System.DiskTotalBytes += d.TotalBytes
		}
		if summary.System.DiskTotalBytes > 0 {
			summary.System.DiskPercent = float64(summary.System.DiskUsedBytes) / float64(summary.System.DiskTotalBytes) * 100
		}
		summary.System.DataPoints = len(systemPoints)
	}

	// Container metrics summary
	s.mu.RLock()
	summary.System.ContainerCount = len(s.containerMetrics)
	for containerID, store := range s.containerMetrics {
		store.mu.RLock()
		containerPoints := filterContainerMetricsByTime(store.dataPoints, timeRange)
		if len(containerPoints) > 0 {
			cpuValues := make([]float64, 0, len(containerPoints))
			memValues := make([]uint64, 0, len(containerPoints))
			for _, p := range containerPoints {
				cpuValues = append(cpuValues, p.CPU.UsagePercent)
				memValues = append(memValues, p.Memory.UsageBytes)
			}

			latest := containerPoints[len(containerPoints)-1]
			containerSummary := ContainerMetricsSummary{
				ContainerID:    containerID,
				ContainerName:  store.containerName,
				Status:         "running",
				CPUAvgPercent:  average(cpuValues),
				CPUMaxPercent:  maxFloat64(cpuValues),
				MemoryAvgBytes: averageUint64(memValues),
				MemoryMaxBytes: maxUint64(memValues),
				MemoryLimit:    latest.Memory.LimitBytes,
				NetRxBytes:     latest.Network.RxBytes,
				NetTxBytes:     latest.Network.TxBytes,
				DiskReadBytes:  latest.DiskIO.ReadBytes,
				DiskWriteBytes: latest.DiskIO.WriteBytes,
				DataPoints:     len(containerPoints),
			}
			summary.Containers = append(summary.Containers, containerSummary)
		}
		store.mu.RUnlock()
	}
	s.mu.RUnlock()

	// Sort containers by CPU usage
	sort.Slice(summary.Containers, func(i, j int) bool {
		return summary.Containers[i].CPUAvgPercent > summary.Containers[j].CPUAvgPercent
	})

	return summary, nil
}

// AggregateMetrics performs aggregation on metrics data.
func (s *Service) AggregateMetrics(ctx context.Context, query MetricsQuery) (*AggregatedMetrics, error) {
	result := &AggregatedMetrics{
		Query: query,
	}

	if query.ContainerID != "" {
		// Container metrics aggregation
		s.mu.RLock()
		store, exists := s.containerMetrics[query.ContainerID]
		s.mu.RUnlock()

		if !exists {
			return nil, fmt.Errorf("no metrics found for container: %s", query.ContainerID)
		}

		store.mu.RLock()
		defer store.mu.RUnlock()

		filteredPoints := filterContainerMetricsByTime(store.dataPoints, query.TimeRange)
		values := extractContainerMetricValues(filteredPoints, query.MetricName)
		result.Statistics = calculateStatsFromValues(values)

		// Build aggregated series
		resolutionDuration := parseResolution(query.Resolution)
		aggregatedPoints := aggregateValues(filteredPoints, values, resolutionDuration, query.Aggregation)

		series := MetricSeries{
			Name:   query.MetricName,
			Labels: MetricLabels{"container_id": query.ContainerID},
		}
		for ts, val := range aggregatedPoints {
			series.DataPoints = append(series.DataPoints, MetricPoint{
				Timestamp: ts,
				Value:     val,
			})
		}
		// Sort by timestamp
		sort.Slice(series.DataPoints, func(i, j int) bool {
			return series.DataPoints[i].Timestamp.Before(series.DataPoints[j].Timestamp)
		})
		result.Series = append(result.Series, series)
	} else {
		// System metrics aggregation
		s.systemMetrics.mu.RLock()
		defer s.systemMetrics.mu.RUnlock()

		filteredPoints := filterSystemMetricsByTime(s.systemMetrics.dataPoints, query.TimeRange)
		values := extractSystemMetricValues(filteredPoints, query.MetricName)
		result.Statistics = calculateStatsFromValues(values)
	}

	return result, nil
}

// Helper functions

func parseResolution(res Resolution) time.Duration {
	switch res {
	case Resolution1s:
		return time.Second
	case Resolution10s:
		return 10 * time.Second
	case Resolution1m:
		return time.Minute
	case Resolution5m:
		return 5 * time.Minute
	case Resolution15m:
		return 15 * time.Minute
	case Resolution1h:
		return time.Hour
	case Resolution1d:
		return 24 * time.Hour
	default:
		return time.Minute
	}
}

func aggregateContainerMetrics(points []ContainerMetrics, resolution time.Duration) []ContainerMetrics {
	if len(points) == 0 || resolution == 0 {
		return points
	}

	buckets := make(map[int64][]ContainerMetrics)
	for _, p := range points {
		bucket := p.Timestamp.UnixNano() / int64(resolution)
		buckets[bucket] = append(buckets[bucket], p)
	}

	result := make([]ContainerMetrics, 0, len(buckets))
	for bucket, bucketPoints := range buckets {
		if len(bucketPoints) == 0 {
			continue
		}

		// Average the metrics
		avg := ContainerMetrics{
			ContainerID:   bucketPoints[0].ContainerID,
			ContainerName: bucketPoints[0].ContainerName,
			Timestamp:     time.Unix(0, bucket*int64(resolution)),
		}

		for _, p := range bucketPoints {
			avg.CPU.UsagePercent += p.CPU.UsagePercent
			avg.Memory.UsageBytes += p.Memory.UsageBytes
			avg.Network.RxBytes += p.Network.RxBytes
			avg.Network.TxBytes += p.Network.TxBytes
			avg.DiskIO.ReadBytes += p.DiskIO.ReadBytes
			avg.DiskIO.WriteBytes += p.DiskIO.WriteBytes
		}

		n := float64(len(bucketPoints))
		avg.CPU.UsagePercent /= n
		avg.Memory.UsageBytes /= uint64(len(bucketPoints))
		avg.Network.RxBytes /= uint64(len(bucketPoints))
		avg.Network.TxBytes /= uint64(len(bucketPoints))
		avg.DiskIO.ReadBytes /= uint64(len(bucketPoints))
		avg.DiskIO.WriteBytes /= uint64(len(bucketPoints))

		result = append(result, avg)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Timestamp.Before(result[j].Timestamp)
	})

	return result
}

func aggregateSystemMetrics(points []SystemMetrics, resolution time.Duration) []SystemMetrics {
	if len(points) == 0 || resolution == 0 {
		return points
	}

	buckets := make(map[int64][]SystemMetrics)
	for _, p := range points {
		bucket := p.Timestamp.UnixNano() / int64(resolution)
		buckets[bucket] = append(buckets[bucket], p)
	}

	result := make([]SystemMetrics, 0, len(buckets))
	for bucket, bucketPoints := range buckets {
		if len(bucketPoints) == 0 {
			continue
		}

		avg := SystemMetrics{
			Timestamp: time.Unix(0, bucket*int64(resolution)),
		}

		for _, p := range bucketPoints {
			avg.CPU.UsagePercent += p.CPU.UsagePercent
			avg.Memory.UsagePercent += p.Memory.UsagePercent
			avg.Load.Load1 += p.Load.Load1
		}

		n := float64(len(bucketPoints))
		avg.CPU.UsagePercent /= n
		avg.Memory.UsagePercent /= n
		avg.Load.Load1 /= n

		// Use latest disk metrics
		avg.Disk = bucketPoints[len(bucketPoints)-1].Disk

		result = append(result, avg)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Timestamp.Before(result[j].Timestamp)
	})

	return result
}

func filterContainerMetricsByTime(points []ContainerMetrics, timeRange TimeRange) []ContainerMetrics {
	result := make([]ContainerMetrics, 0)
	for _, p := range points {
		if (p.Timestamp.Equal(timeRange.Start) || p.Timestamp.After(timeRange.Start)) &&
			(p.Timestamp.Equal(timeRange.End) || p.Timestamp.Before(timeRange.End)) {
			result = append(result, p)
		}
	}
	return result
}

func filterSystemMetricsByTime(points []SystemMetrics, timeRange TimeRange) []SystemMetrics {
	result := make([]SystemMetrics, 0)
	for _, p := range points {
		if (p.Timestamp.Equal(timeRange.Start) || p.Timestamp.After(timeRange.Start)) &&
			(p.Timestamp.Equal(timeRange.End) || p.Timestamp.Before(timeRange.End)) {
			result = append(result, p)
		}
	}
	return result
}

func extractContainerMetricValues(points []ContainerMetrics, metricName string) []float64 {
	values := make([]float64, 0, len(points))
	for _, p := range points {
		switch metricName {
		case "cpu_percent":
			values = append(values, p.CPU.UsagePercent)
		case "memory_bytes":
			values = append(values, float64(p.Memory.UsageBytes))
		case "memory_percent":
			values = append(values, p.Memory.UsagePercent)
		case "network_rx_bytes":
			values = append(values, float64(p.Network.RxBytes))
		case "network_tx_bytes":
			values = append(values, float64(p.Network.TxBytes))
		case "disk_read_bytes":
			values = append(values, float64(p.DiskIO.ReadBytes))
		case "disk_write_bytes":
			values = append(values, float64(p.DiskIO.WriteBytes))
		}
	}
	return values
}

func extractSystemMetricValues(points []SystemMetrics, metricName string) []float64 {
	values := make([]float64, 0, len(points))
	for _, p := range points {
		switch metricName {
		case "cpu_percent":
			values = append(values, p.CPU.UsagePercent)
		case "memory_percent":
			values = append(values, p.Memory.UsagePercent)
		case "memory_used_bytes":
			values = append(values, float64(p.Memory.UsedBytes))
		case "load1":
			values = append(values, p.Load.Load1)
		case "load5":
			values = append(values, p.Load.Load5)
		case "load15":
			values = append(values, p.Load.Load15)
		}
	}
	return values
}

func aggregateValues(points []ContainerMetrics, values []float64, resolution time.Duration, aggType AggregationType) map[time.Time]float64 {
	if len(points) == 0 {
		return nil
	}

	buckets := make(map[int64][]float64)
	for i, p := range points {
		if i < len(values) {
			bucket := p.Timestamp.UnixNano() / int64(resolution)
			buckets[bucket] = append(buckets[bucket], values[i])
		}
	}

	result := make(map[time.Time]float64)
	for bucket, bucketValues := range buckets {
		ts := time.Unix(0, bucket*int64(resolution))
		switch aggType {
		case AggregationAvg:
			result[ts] = average(bucketValues)
		case AggregationMax:
			result[ts] = maxFloat64(bucketValues)
		case AggregationMin:
			result[ts] = minFloat64(bucketValues)
		case AggregationSum:
			result[ts] = sum(bucketValues)
		case AggregationCount:
			result[ts] = float64(len(bucketValues))
		case AggregationP50:
			result[ts] = percentile(bucketValues, 50)
		case AggregationP95:
			result[ts] = percentile(bucketValues, 95)
		case AggregationP99:
			result[ts] = percentile(bucketValues, 99)
		default:
			result[ts] = average(bucketValues)
		}
	}

	return result
}

func calculateStats(points []MetricPoint) MetricStats {
	if len(points) == 0 {
		return MetricStats{}
	}

	values := make([]float64, 0, len(points))
	for _, p := range points {
		values = append(values, p.Value)
	}

	return calculateStatsFromValues(values)
}

func calculateStatsFromValues(values []float64) MetricStats {
	if len(values) == 0 {
		return MetricStats{}
	}

	stats := MetricStats{
		Min:   values[0],
		Max:   values[0],
		Count: int64(len(values)),
	}

	for _, v := range values {
		if v < stats.Min {
			stats.Min = v
		}
		if v > stats.Max {
			stats.Max = v
		}
		stats.Sum += v
	}

	stats.Avg = stats.Sum / float64(len(values))

	// Calculate standard deviation
	var variance float64
	for _, v := range values {
		diff := v - stats.Avg
		variance += diff * diff
	}
	stats.StdDev = math.Sqrt(variance / float64(len(values)))

	return stats
}

func average(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var s float64
	for _, v := range values {
		s += v
	}
	return s / float64(len(values))
}

func maxFloat64(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	m := values[0]
	for _, v := range values[1:] {
		if v > m {
			m = v
		}
	}
	return m
}

func minFloat64(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	m := values[0]
	for _, v := range values[1:] {
		if v < m {
			m = v
		}
	}
	return m
}

func sum(values []float64) float64 {
	var s float64
	for _, v := range values {
		s += v
	}
	return s
}

func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}

	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)

	index := (p / 100) * float64(len(sorted)-1)
	lower := int(index)
	upper := lower + 1

	if upper >= len(sorted) {
		return sorted[len(sorted)-1]
	}

	weight := index - float64(lower)
	return sorted[lower]*(1-weight) + sorted[upper]*weight
}

func averageUint64(values []uint64) uint64 {
	if len(values) == 0 {
		return 0
	}
	var s uint64
	for _, v := range values {
		s += v
	}
	return s / uint64(len(values))
}

func maxUint64(values []uint64) uint64 {
	if len(values) == 0 {
		return 0
	}
	m := values[0]
	for _, v := range values[1:] {
		if v > m {
			m = v
		}
	}
	return m
}
