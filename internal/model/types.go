// Package model defines the shared data structures used across the collector,
// analyzer and report packages.
package model

import "time"

// Hardware describes the host where ClickHouse runs.
type Hardware struct {
	LogicalCPUs  int     `json:"logical_cpus"`
	PhysicalCPUs int     `json:"physical_cpus"`
	TotalMemory  uint64  `json:"total_memory_bytes"`
	FreeMemory   uint64  `json:"free_memory_bytes"`
	DiskTotal    uint64  `json:"disk_total_bytes"`
	DiskFree     uint64  `json:"disk_free_bytes"`
	DiskPath     string  `json:"disk_path"`
	LoadAvg1     float64 `json:"load_avg_1m"`
}

// ServerSetting is a single value read from system.settings or
// system.server_settings.
type ServerSetting struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	Changed bool   `json:"changed"`
}

// Part aggregates per-table active parts from system.parts.
type Part struct {
	Database     string `json:"database"`
	Table        string `json:"table"`
	PartCount    uint64 `json:"part_count"`
	Rows         uint64 `json:"rows"`
	BytesOnDisk  uint64 `json:"bytes_on_disk"`
	Partitions   uint64 `json:"partitions"`
	MaxPartsPart uint64 `json:"max_parts_in_partition"`
}

// Merge is an in-flight merge from system.merges.
type Merge struct {
	Database    string  `json:"database"`
	Table       string  `json:"table"`
	Elapsed     float64 `json:"elapsed_seconds"`
	Progress    float64 `json:"progress"`
	NumParts    uint64  `json:"num_parts"`
	TotalSize   uint64  `json:"total_size_bytes_compressed"`
	MemoryUsage uint64  `json:"memory_usage"`
	IsMutation  bool    `json:"is_mutation"`
}

// Mutation is a row from system.mutations.
type Mutation struct {
	Database         string    `json:"database"`
	Table            string    `json:"table"`
	MutationID       string    `json:"mutation_id"`
	Command          string    `json:"command"`
	CreateTime       time.Time `json:"create_time"`
	PartsToDo        int64     `json:"parts_to_do"`
	IsDone           bool      `json:"is_done"`
	LatestFailTime   time.Time `json:"latest_fail_time"`
	LatestFailReason string    `json:"latest_fail_reason"`
}

// AsyncMetric is a value from system.asynchronous_metrics.
type AsyncMetric struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
}

// Snapshot is the full set of data collected from a ClickHouse host.
type Snapshot struct {
	CollectedAt    time.Time                `json:"collected_at"`
	Version        string                   `json:"version"`
	Hardware       Hardware                 `json:"hardware"`
	ServerSettings map[string]ServerSetting `json:"server_settings"`
	Parts          []Part                   `json:"parts"`
	Merges         []Merge                  `json:"merges"`
	Mutations      []Mutation               `json:"mutations"`
	AsyncMetrics   map[string]float64       `json:"async_metrics"`
	Uptime         time.Duration            `json:"uptime"`
}

// Severity ranks a finding.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// Category groups findings by subsystem.
type Category string

const (
	CategoryHardware  Category = "hardware"
	CategoryParts     Category = "parts"
	CategoryMerges    Category = "merges"
	CategoryMutations Category = "mutations"
)

// Finding is a single diagnostic plus its recommendation.
type Finding struct {
	Category   Category `json:"category"`
	Severity   Severity `json:"severity"`
	Title      string   `json:"title"`
	Detail     string   `json:"detail"`
	Suggestion string   `json:"suggestion"`
	// ConfigKeys maps a ClickHouse setting name to the recommended value.
	// Used to emit a ready-to-apply config snippet. May be empty.
	ConfigKeys map[string]string `json:"config_keys,omitempty"`
	// ConfigScope indicates where the setting belongs: "server" (config.xml)
	// or "profile" (users.xml). Empty when ConfigKeys is empty.
	ConfigScope string `json:"config_scope,omitempty"`
}

// Report bundles all findings produced by the analyzers.
type Report struct {
	GeneratedAt time.Time `json:"generated_at"`
	Snapshot    *Snapshot `json:"-"`
	Findings    []Finding `json:"findings"`
}

// severityWeight orders severities for sorting (higher = more urgent).
func (s Severity) Weight() int {
	switch s {
	case SeverityCritical:
		return 3
	case SeverityWarning:
		return 2
	case SeverityInfo:
		return 1
	default:
		return 0
	}
}
