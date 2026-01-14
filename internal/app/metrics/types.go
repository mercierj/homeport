package metrics

import "time"

// ============================================================================
// Container Metrics Types
// ============================================================================

// ContainerMetrics represents metrics for a single container at a point in time.
type ContainerMetrics struct {
	ContainerID   string    `json:"container_id"`
	ContainerName string    `json:"container_name"`
	Timestamp     time.Time `json:"timestamp"`
	CPU           CPUMetrics    `json:"cpu"`
	Memory        MemoryMetrics `json:"memory"`
	Network       NetworkMetrics `json:"network"`
	DiskIO        DiskIOMetrics  `json:"disk_io"`
}

// CPUMetrics represents CPU usage metrics.
type CPUMetrics struct {
	UsagePercent     float64 `json:"usage_percent"`      // CPU usage percentage (0-100)
	UsageTotal       uint64  `json:"usage_total"`        // Total CPU time consumed in nanoseconds
	SystemUsage      uint64  `json:"system_usage"`       // Host system CPU usage
	OnlineCPUs       uint32  `json:"online_cpus"`        // Number of CPUs available to container
	ThrottledPeriods uint64  `json:"throttled_periods"`  // Number of throttled periods
	ThrottledTime    uint64  `json:"throttled_time"`     // Throttled time in nanoseconds
}

// MemoryMetrics represents memory usage metrics.
type MemoryMetrics struct {
	UsageBytes      uint64  `json:"usage_bytes"`       // Current memory usage in bytes
	LimitBytes      uint64  `json:"limit_bytes"`       // Memory limit in bytes
	UsagePercent    float64 `json:"usage_percent"`     // Memory usage percentage (0-100)
	CacheBytes      uint64  `json:"cache_bytes"`       // Page cache memory
	RSSBytes        uint64  `json:"rss_bytes"`         // Anonymous and swap cache memory
	SwapBytes       uint64  `json:"swap_bytes"`        // Swap usage in bytes
	WorkingSetBytes uint64  `json:"working_set_bytes"` // Working set size
	PageFaults      uint64  `json:"page_faults"`       // Total page faults
	MajorPageFaults uint64  `json:"major_page_faults"` // Major page faults
}

// NetworkMetrics represents network I/O metrics.
type NetworkMetrics struct {
	RxBytes   uint64 `json:"rx_bytes"`   // Bytes received
	RxPackets uint64 `json:"rx_packets"` // Packets received
	RxErrors  uint64 `json:"rx_errors"`  // Receive errors
	RxDropped uint64 `json:"rx_dropped"` // Receive packets dropped
	TxBytes   uint64 `json:"tx_bytes"`   // Bytes transmitted
	TxPackets uint64 `json:"tx_packets"` // Packets transmitted
	TxErrors  uint64 `json:"tx_errors"`  // Transmit errors
	TxDropped uint64 `json:"tx_dropped"` // Transmit packets dropped
}

// DiskIOMetrics represents disk I/O metrics.
type DiskIOMetrics struct {
	ReadBytes       uint64 `json:"read_bytes"`        // Bytes read
	WriteBytes      uint64 `json:"write_bytes"`       // Bytes written
	ReadOps         uint64 `json:"read_ops"`          // Read operations
	WriteOps        uint64 `json:"write_ops"`         // Write operations
	ReadTime        uint64 `json:"read_time"`         // Time spent reading in nanoseconds
	WriteTime       uint64 `json:"write_time"`        // Time spent writing in nanoseconds
	IoServicedOps   uint64 `json:"io_serviced_ops"`   // Total I/O operations
	IoServicedBytes uint64 `json:"io_serviced_bytes"` // Total bytes serviced
}

// ============================================================================
// System Metrics Types
// ============================================================================

// SystemMetrics represents host system metrics at a point in time.
type SystemMetrics struct {
	Timestamp time.Time           `json:"timestamp"`
	CPU       SystemCPUMetrics    `json:"cpu"`
	Memory    SystemMemoryMetrics `json:"memory"`
	Disk      []SystemDiskMetrics `json:"disk"`
	Load      LoadMetrics         `json:"load"`
}

// SystemCPUMetrics represents host CPU metrics.
type SystemCPUMetrics struct {
	UsagePercent float64   `json:"usage_percent"`  // Overall CPU usage percentage
	UserPercent  float64   `json:"user_percent"`   // User space CPU usage
	SystemPercent float64  `json:"system_percent"` // Kernel space CPU usage
	IdlePercent  float64   `json:"idle_percent"`   // Idle percentage
	IOWaitPercent float64  `json:"iowait_percent"` // I/O wait percentage
	NumCores     int       `json:"num_cores"`      // Number of CPU cores
	CoreUsage    []float64 `json:"core_usage"`     // Per-core usage percentage
}

// SystemMemoryMetrics represents host memory metrics.
type SystemMemoryMetrics struct {
	TotalBytes     uint64  `json:"total_bytes"`     // Total physical memory
	UsedBytes      uint64  `json:"used_bytes"`      // Used memory
	FreeBytes      uint64  `json:"free_bytes"`      // Free memory
	AvailableBytes uint64  `json:"available_bytes"` // Available memory
	BuffersBytes   uint64  `json:"buffers_bytes"`   // Buffer memory
	CachedBytes    uint64  `json:"cached_bytes"`    // Cached memory
	SwapTotalBytes uint64  `json:"swap_total_bytes"` // Total swap
	SwapUsedBytes  uint64  `json:"swap_used_bytes"`  // Used swap
	SwapFreeBytes  uint64  `json:"swap_free_bytes"`  // Free swap
	UsagePercent   float64 `json:"usage_percent"`    // Memory usage percentage
}

// SystemDiskMetrics represents metrics for a single disk/mount.
type SystemDiskMetrics struct {
	MountPoint   string  `json:"mount_point"`   // Mount point path
	Device       string  `json:"device"`        // Device name
	FSType       string  `json:"fs_type"`       // Filesystem type
	TotalBytes   uint64  `json:"total_bytes"`   // Total space
	UsedBytes    uint64  `json:"used_bytes"`    // Used space
	FreeBytes    uint64  `json:"free_bytes"`    // Free space
	UsagePercent float64 `json:"usage_percent"` // Disk usage percentage
	InodesTotal  uint64  `json:"inodes_total"`  // Total inodes
	InodesUsed   uint64  `json:"inodes_used"`   // Used inodes
	InodesFree   uint64  `json:"inodes_free"`   // Free inodes
}

// LoadMetrics represents system load averages.
type LoadMetrics struct {
	Load1  float64 `json:"load1"`  // 1-minute load average
	Load5  float64 `json:"load5"`  // 5-minute load average
	Load15 float64 `json:"load15"` // 15-minute load average
}

// ============================================================================
// Time Series Types
// ============================================================================

// MetricPoint represents a single metric data point.
type MetricPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"`
}

// MetricSeries represents a time series of metrics.
type MetricSeries struct {
	Name       string        `json:"name"`
	Labels     MetricLabels  `json:"labels"`
	DataPoints []MetricPoint `json:"data_points"`
}

// MetricLabels holds key-value pairs for metric identification.
type MetricLabels map[string]string

// ============================================================================
// Query and Aggregation Types
// ============================================================================

// TimeRange represents a time range for querying metrics.
type TimeRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// AggregationType defines how to aggregate metrics.
type AggregationType string

const (
	AggregationAvg   AggregationType = "avg"
	AggregationMax   AggregationType = "max"
	AggregationMin   AggregationType = "min"
	AggregationSum   AggregationType = "sum"
	AggregationCount AggregationType = "count"
	AggregationP50   AggregationType = "p50"
	AggregationP95   AggregationType = "p95"
	AggregationP99   AggregationType = "p99"
)

// Resolution defines the time resolution for metrics aggregation.
type Resolution string

const (
	Resolution1s  Resolution = "1s"
	Resolution10s Resolution = "10s"
	Resolution1m  Resolution = "1m"
	Resolution5m  Resolution = "5m"
	Resolution15m Resolution = "15m"
	Resolution1h  Resolution = "1h"
	Resolution1d  Resolution = "1d"
)

// MetricsQuery represents a query for metrics data.
type MetricsQuery struct {
	ContainerID string          `json:"container_id,omitempty"`
	MetricName  string          `json:"metric_name"`
	TimeRange   TimeRange       `json:"time_range"`
	Aggregation AggregationType `json:"aggregation"`
	Resolution  Resolution      `json:"resolution"`
	Labels      MetricLabels    `json:"labels,omitempty"`
}

// AggregatedMetrics represents the result of an aggregated metrics query.
type AggregatedMetrics struct {
	Query       MetricsQuery   `json:"query"`
	Series      []MetricSeries `json:"series"`
	Statistics  MetricStats    `json:"statistics"`
}

// MetricStats represents statistical summary of metrics.
type MetricStats struct {
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
	Avg    float64 `json:"avg"`
	Sum    float64 `json:"sum"`
	Count  int64   `json:"count"`
	StdDev float64 `json:"std_dev"`
}

// ============================================================================
// Summary Types
// ============================================================================

// ContainerMetricsSummary provides a summary of container metrics.
type ContainerMetricsSummary struct {
	ContainerID    string  `json:"container_id"`
	ContainerName  string  `json:"container_name"`
	Status         string  `json:"status"`
	CPUAvgPercent  float64 `json:"cpu_avg_percent"`
	CPUMaxPercent  float64 `json:"cpu_max_percent"`
	MemoryAvgBytes uint64  `json:"memory_avg_bytes"`
	MemoryMaxBytes uint64  `json:"memory_max_bytes"`
	MemoryLimit    uint64  `json:"memory_limit"`
	NetRxBytes     uint64  `json:"net_rx_bytes"`
	NetTxBytes     uint64  `json:"net_tx_bytes"`
	DiskReadBytes  uint64  `json:"disk_read_bytes"`
	DiskWriteBytes uint64  `json:"disk_write_bytes"`
	Uptime         string  `json:"uptime"`
	DataPoints     int     `json:"data_points"`
}

// SystemMetricsSummary provides a summary of system metrics.
type SystemMetricsSummary struct {
	CPUAvgPercent    float64 `json:"cpu_avg_percent"`
	CPUMaxPercent    float64 `json:"cpu_max_percent"`
	MemoryUsedBytes  uint64  `json:"memory_used_bytes"`
	MemoryTotalBytes uint64  `json:"memory_total_bytes"`
	MemoryPercent    float64 `json:"memory_percent"`
	DiskUsedBytes    uint64  `json:"disk_used_bytes"`
	DiskTotalBytes   uint64  `json:"disk_total_bytes"`
	DiskPercent      float64 `json:"disk_percent"`
	Load1            float64 `json:"load1"`
	Load5            float64 `json:"load5"`
	Load15           float64 `json:"load15"`
	ContainerCount   int     `json:"container_count"`
	DataPoints       int     `json:"data_points"`
}

// MetricsSummary provides an overall summary of all metrics.
type MetricsSummary struct {
	Timestamp  time.Time                  `json:"timestamp"`
	System     SystemMetricsSummary       `json:"system"`
	Containers []ContainerMetricsSummary  `json:"containers"`
	TimeRange  TimeRange                  `json:"time_range"`
}

// ============================================================================
// History Types
// ============================================================================

// MetricsHistory represents historical metrics data.
type MetricsHistory struct {
	ContainerID string             `json:"container_id,omitempty"`
	MetricType  string             `json:"metric_type"`
	TimeRange   TimeRange          `json:"time_range"`
	Resolution  Resolution         `json:"resolution"`
	DataPoints  []MetricPoint      `json:"data_points"`
	Statistics  MetricStats        `json:"statistics"`
}

// ContainerMetricsHistory represents the history of all metrics for a container.
type ContainerMetricsHistory struct {
	ContainerID   string           `json:"container_id"`
	ContainerName string           `json:"container_name"`
	TimeRange     TimeRange        `json:"time_range"`
	CPU           MetricsHistory   `json:"cpu"`
	Memory        MetricsHistory   `json:"memory"`
	NetworkRx     MetricsHistory   `json:"network_rx"`
	NetworkTx     MetricsHistory   `json:"network_tx"`
	DiskRead      MetricsHistory   `json:"disk_read"`
	DiskWrite     MetricsHistory   `json:"disk_write"`
}

// SystemMetricsHistory represents the history of system metrics.
type SystemMetricsHistory struct {
	TimeRange TimeRange      `json:"time_range"`
	CPU       MetricsHistory `json:"cpu"`
	Memory    MetricsHistory `json:"memory"`
	Disk      MetricsHistory `json:"disk"`
	Load      MetricsHistory `json:"load"`
}

// ============================================================================
// Configuration Types
// ============================================================================

// Config holds configuration for the metrics service.
type Config struct {
	// CollectionInterval is how often to collect metrics
	CollectionInterval time.Duration `json:"collection_interval"`
	// RetentionPeriod is how long to keep metrics data
	RetentionPeriod time.Duration `json:"retention_period"`
	// MaxDataPoints is the maximum number of data points to store per metric
	MaxDataPoints int `json:"max_data_points"`
	// EnableSystemMetrics enables collection of host system metrics
	EnableSystemMetrics bool `json:"enable_system_metrics"`
	// EnableContainerMetrics enables collection of container metrics
	EnableContainerMetrics bool `json:"enable_container_metrics"`
}

// DefaultConfig returns the default metrics configuration.
func DefaultConfig() *Config {
	return &Config{
		CollectionInterval:     10 * time.Second,
		RetentionPeriod:        24 * time.Hour,
		MaxDataPoints:          8640, // 24 hours at 10 second intervals
		EnableSystemMetrics:    true,
		EnableContainerMetrics: true,
	}
}

// ============================================================================
// Alert Types
// ============================================================================

// AlertThreshold defines a threshold for alerting.
type AlertThreshold struct {
	MetricName  string          `json:"metric_name"`
	ContainerID string          `json:"container_id,omitempty"` // Empty means system-level
	Operator    string          `json:"operator"`               // gt, lt, gte, lte, eq
	Value       float64         `json:"value"`
	Duration    time.Duration   `json:"duration"` // How long threshold must be exceeded
	Severity    AlertSeverity   `json:"severity"`
}

// AlertSeverity represents the severity of an alert.
type AlertSeverity string

const (
	AlertSeverityInfo     AlertSeverity = "info"
	AlertSeverityWarning  AlertSeverity = "warning"
	AlertSeverityCritical AlertSeverity = "critical"
)

// Alert represents an active or historical alert.
type Alert struct {
	ID          string         `json:"id"`
	Threshold   AlertThreshold `json:"threshold"`
	TriggeredAt time.Time      `json:"triggered_at"`
	ResolvedAt  *time.Time     `json:"resolved_at,omitempty"`
	CurrentValue float64       `json:"current_value"`
	Message     string         `json:"message"`
}
