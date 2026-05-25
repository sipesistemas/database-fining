package collector

import (
	"context"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/hugomesquita/database-fining/internal/model"
)

// Config holds connection parameters for a ClickHouse server.
type Config struct {
	Addr     string // host:port, e.g. localhost:9000
	Database string
	Username string
	Password string
	DataPath string // host path where ClickHouse stores data (for disk stats)
}

// ClickHouse wraps a native-protocol connection.
type ClickHouse struct {
	conn driver.Conn
	cfg  Config
}

// Connect opens a native-protocol connection and verifies it with a ping.
func Connect(ctx context.Context, cfg Config) (*ClickHouse, error) {
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{cfg.Addr},
		Auth: clickhouse.Auth{
			Database: cfg.Database,
			Username: cfg.Username,
			Password: cfg.Password,
		},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("open connection: %w", err)
	}
	if err := conn.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping %s: %w", cfg.Addr, err)
	}
	return &ClickHouse{conn: conn, cfg: cfg}, nil
}

// Close releases the connection.
func (c *ClickHouse) Close() error { return c.conn.Close() }

// Collect gathers a full snapshot from the connected server plus local
// hardware facts.
func (c *ClickHouse) Collect(ctx context.Context) (*model.Snapshot, error) {
	snap := &model.Snapshot{
		CollectedAt:    time.Now(),
		ServerSettings: map[string]model.ServerSetting{},
		AsyncMetrics:   map[string]float64{},
		Hardware:       CollectHardware(c.dataPath()),
	}

	if err := c.queryVersion(ctx, snap); err != nil {
		return nil, err
	}
	if err := c.queryServerSettings(ctx, snap); err != nil {
		return nil, err
	}
	if err := c.queryParts(ctx, snap); err != nil {
		return nil, err
	}
	if err := c.queryMerges(ctx, snap); err != nil {
		return nil, err
	}
	if err := c.queryMutations(ctx, snap); err != nil {
		return nil, err
	}
	if err := c.queryAsyncMetrics(ctx, snap); err != nil {
		return nil, err
	}
	return snap, nil
}

func (c *ClickHouse) dataPath() string {
	if c.cfg.DataPath != "" {
		return c.cfg.DataPath
	}
	return "/var/lib/clickhouse"
}

func (c *ClickHouse) queryVersion(ctx context.Context, snap *model.Snapshot) error {
	var version string
	var uptime uint32
	row := c.conn.QueryRow(ctx, "SELECT version(), uptime()")
	if err := row.Scan(&version, &uptime); err != nil {
		return fmt.Errorf("version: %w", err)
	}
	snap.Version = version
	snap.Uptime = time.Duration(uptime) * time.Second
	return nil
}

// queryServerSettings pulls the server-level settings that drive most tuning
// decisions. system.server_settings is available from 22.10+; we fall back to
// system.settings names too since merge/pool knobs migrated over versions.
func (c *ClickHouse) queryServerSettings(ctx context.Context, snap *model.Snapshot) error {
	// Each source carries a precedence. Names clash across tables: e.g.
	// parts_to_throw_insert exists both in system.settings (a session override
	// defaulting to 0 = "use the table value") and in system.merge_tree_settings
	// (the effective default, e.g. 3000). We keep the highest-precedence value so
	// the analyzer sees the value that actually governs the server.
	const q = `
		SELECT name, toString(value), changed, 3 AS src
		FROM system.server_settings
		UNION ALL
		SELECT name, toString(value), changed, 2 AS src
		FROM system.merge_tree_settings
		UNION ALL
		SELECT name, toString(value), changed, 1 AS src
		FROM system.settings`
	rows, err := c.conn.Query(ctx, q)
	if err != nil {
		return fmt.Errorf("settings: %w", err)
	}
	defer rows.Close()
	prec := map[string]uint8{}
	for rows.Next() {
		var (
			s   model.ServerSetting
			src uint8
		)
		if err := rows.Scan(&s.Name, &s.Value, &s.Changed, &src); err != nil {
			return fmt.Errorf("scan setting: %w", err)
		}
		if cur, ok := prec[s.Name]; ok && cur >= src {
			continue
		}
		snap.ServerSettings[s.Name] = s
		prec[s.Name] = src
	}
	return rows.Err()
}

func (c *ClickHouse) queryParts(ctx context.Context, snap *model.Snapshot) error {
	const q = `
		SELECT
			database,
			table,
			count() AS part_count,
			sum(rows) AS rows,
			sum(bytes_on_disk) AS bytes_on_disk,
			uniqExact(partition) AS partitions,
			max(parts_in_partition) AS max_parts_in_partition
		FROM (
			SELECT
				database, table, partition, rows, bytes_on_disk,
				count() OVER (PARTITION BY database, table, partition) AS parts_in_partition
			FROM system.parts
			WHERE active
		)
		GROUP BY database, table
		ORDER BY part_count DESC`
	rows, err := c.conn.Query(ctx, q)
	if err != nil {
		return fmt.Errorf("parts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var p model.Part
		if err := rows.Scan(&p.Database, &p.Table, &p.PartCount, &p.Rows,
			&p.BytesOnDisk, &p.Partitions, &p.MaxPartsPart); err != nil {
			return fmt.Errorf("scan part: %w", err)
		}
		snap.Parts = append(snap.Parts, p)
	}
	return rows.Err()
}

func (c *ClickHouse) queryMerges(ctx context.Context, snap *model.Snapshot) error {
	const q = `
		SELECT database, table, elapsed, progress, num_parts,
		       total_size_bytes_compressed, memory_usage, is_mutation
		FROM system.merges`
	rows, err := c.conn.Query(ctx, q)
	if err != nil {
		return fmt.Errorf("merges: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var m model.Merge
		if err := rows.Scan(&m.Database, &m.Table, &m.Elapsed, &m.Progress,
			&m.NumParts, &m.TotalSize, &m.MemoryUsage, &m.IsMutation); err != nil {
			return fmt.Errorf("scan merge: %w", err)
		}
		snap.Merges = append(snap.Merges, m)
	}
	return rows.Err()
}

func (c *ClickHouse) queryMutations(ctx context.Context, snap *model.Snapshot) error {
	const q = `
		SELECT database, table, mutation_id, command, create_time,
		       parts_to_do, is_done, latest_fail_time, latest_fail_reason
		FROM system.mutations
		WHERE NOT is_done OR latest_fail_reason != ''
		ORDER BY create_time ASC`
	rows, err := c.conn.Query(ctx, q)
	if err != nil {
		return fmt.Errorf("mutations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var m model.Mutation
		if err := rows.Scan(&m.Database, &m.Table, &m.MutationID, &m.Command,
			&m.CreateTime, &m.PartsToDo, &m.IsDone, &m.LatestFailTime,
			&m.LatestFailReason); err != nil {
			return fmt.Errorf("scan mutation: %w", err)
		}
		snap.Mutations = append(snap.Mutations, m)
	}
	return rows.Err()
}

func (c *ClickHouse) queryAsyncMetrics(ctx context.Context, snap *model.Snapshot) error {
	const q = `SELECT metric, value FROM system.asynchronous_metrics`
	rows, err := c.conn.Query(ctx, q)
	if err != nil {
		return fmt.Errorf("async metrics: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var m model.AsyncMetric
		if err := rows.Scan(&m.Name, &m.Value); err != nil {
			return fmt.Errorf("scan async metric: %w", err)
		}
		snap.AsyncMetrics[m.Name] = m.Value
	}
	return rows.Err()
}
