// Command chfining connects to a ClickHouse server, collects hardware and
// runtime facts (parts, merges, mutations, settings) and prints tuning
// recommendations.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/hugomesquita/database-fining/internal/analyzer"
	"github.com/hugomesquita/database-fining/internal/collector"
	"github.com/hugomesquita/database-fining/internal/report"
)

func main() {
	var (
		addr     = flag.String("addr", envOr("CH_ADDR", "localhost:9000"), "endereço host:port (protocolo nativo)")
		database = flag.String("database", envOr("CH_DATABASE", "default"), "database")
		username = flag.String("user", envOr("CH_USER", "default"), "usuário")
		password = flag.String("password", os.Getenv("CH_PASSWORD"), "senha (ou env CH_PASSWORD)")
		dataPath = flag.String("data-path", envOr("CH_DATA_PATH", "/var/lib/clickhouse"), "caminho de dados no host (para stats de disco)")
		format   = flag.String("format", "cli", "formato de saída: cli | json | config")
		noColor  = flag.Bool("no-color", false, "desabilita cores no formato cli")
		timeout  = flag.Duration("timeout", 30*time.Second, "timeout da coleta")
	)
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	ch, err := collector.Connect(ctx, collector.Config{
		Addr:     *addr,
		Database: *database,
		Username: *username,
		Password: *password,
		DataPath: *dataPath,
	})
	if err != nil {
		fatalf("conexão: %v", err)
	}
	defer ch.Close()

	snap, err := ch.Collect(ctx)
	if err != nil {
		fatalf("coleta: %v", err)
	}

	rep := analyzer.Analyze(snap)

	switch *format {
	case "cli":
		report.CLI(os.Stdout, rep, *noColor)
	case "json":
		if err := report.JSON(os.Stdout, rep); err != nil {
			fatalf("json: %v", err)
		}
	case "config":
		report.Config(os.Stdout, rep)
	default:
		fatalf("formato inválido: %q (use cli, json ou config)", *format)
	}

	// Exit non-zero when there is at least one critical finding, useful for CI.
	for _, f := range rep.Findings {
		if f.Severity == "critical" {
			os.Exit(2)
		}
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "erro: "+format+"\n", args...)
	os.Exit(1)
}
