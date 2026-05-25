// Package report renders an analyzer Report in CLI, JSON and config formats.
package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/hugomesquita/database-fining/internal/model"
)

// ANSI colors; disabled when noColor is set.
const (
	cReset  = "\033[0m"
	cRed    = "\033[31m"
	cYellow = "\033[33m"
	cCyan   = "\033[36m"
	cGray   = "\033[90m"
	cBold   = "\033[1m"
)

// CLI writes a human-readable report to w. When noColor is true, ANSI codes
// are omitted.
func CLI(w io.Writer, r *model.Report, noColor bool) {
	color := func(c, s string) string {
		if noColor {
			return s
		}
		return c + s + cReset
	}

	s := r.Snapshot
	fmt.Fprintln(w, color(cBold, "ClickHouse Tuning Advisor"))
	fmt.Fprintf(w, "Servidor: %s | Uptime: %s | Coletado: %s\n",
		s.Version, s.Uptime, s.CollectedAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(w, "Host: %d vCPU (%d físicos) | RAM %s | Disco %s livres de %s (%s)\n\n",
		s.Hardware.LogicalCPUs, s.Hardware.PhysicalCPUs,
		humanBytes(s.Hardware.TotalMemory),
		humanBytes(s.Hardware.DiskFree), humanBytes(s.Hardware.DiskTotal), s.Hardware.DiskPath)

	counts := map[model.Severity]int{}
	for _, f := range r.Findings {
		counts[f.Severity]++
	}
	fmt.Fprintf(w, "%s: %d  %s: %d  %s: %d\n\n",
		color(cRed, "críticos"), counts[model.SeverityCritical],
		color(cYellow, "avisos"), counts[model.SeverityWarning],
		color(cCyan, "info"), counts[model.SeverityInfo])

	for _, f := range r.Findings {
		var sevColor, label string
		switch f.Severity {
		case model.SeverityCritical:
			sevColor, label = cRed, "CRÍTICO"
		case model.SeverityWarning:
			sevColor, label = cYellow, "AVISO"
		default:
			sevColor, label = cCyan, "INFO"
		}
		fmt.Fprintf(w, "%s %s\n", color(sevColor, fmt.Sprintf("[%s/%s]", label, f.Category)), color(cBold, f.Title))
		if f.Detail != "" {
			fmt.Fprintf(w, "  %s\n", f.Detail)
		}
		if f.Suggestion != "" {
			fmt.Fprintf(w, "  %s %s\n", color(cGray, "→"), f.Suggestion)
		}
		if len(f.ConfigKeys) > 0 {
			var pairs []string
			for k, v := range f.ConfigKeys {
				pairs = append(pairs, fmt.Sprintf("%s=%s", k, v))
			}
			fmt.Fprintf(w, "  %s %s\n", color(cGray, "config:"), strings.Join(pairs, " "))
		}
		fmt.Fprintln(w)
	}

	if len(r.Findings) == 0 {
		fmt.Fprintln(w, "Nenhum achado. Configuração parece saudável para o snapshot atual.")
	}
}

func humanBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
