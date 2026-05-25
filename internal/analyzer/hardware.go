package analyzer

import (
	"fmt"

	"github.com/hugomesquita/database-fining/internal/model"
)

const gib = 1024 * 1024 * 1024

// analyzeHardware derives pool sizes, cache sizes and memory limits from the
// host's CPU and RAM, comparing them to what the server currently runs with.
func analyzeHardware(s *model.Snapshot) []model.Finding {
	var out []model.Finding
	hw := s.Hardware

	if hw.LogicalCPUs == 0 || hw.TotalMemory == 0 {
		out = append(out, model.Finding{
			Category: model.CategoryHardware,
			Severity: model.SeverityInfo,
			Title:    "Hardware do host não pôde ser lido por completo",
			Detail:   "CPU ou memória retornaram zero; as sugestões abaixo podem estar incompletas.",
		})
		return out
	}

	// background_pool_size: ClickHouse default is 16; a good baseline is
	// ~number of logical cores (capped) for merge-heavy workloads.
	recPool := hw.LogicalCPUs
	if recPool < 8 {
		recPool = 8
	}
	if cur, ok := settingInt(s, "background_pool_size"); ok {
		if cur < int64(recPool) {
			out = append(out, model.Finding{
				Category:    model.CategoryHardware,
				Severity:    model.SeverityWarning,
				Title:       "background_pool_size abaixo do número de núcleos",
				Detail:      fmt.Sprintf("Atual %d, host tem %d vCPUs. Merges podem ficar enfileirados sob carga.", cur, hw.LogicalCPUs),
				Suggestion:  fmt.Sprintf("Aumentar para ~%d para aproveitar os núcleos disponíveis.", recPool),
				ConfigKeys:  map[string]string{"background_pool_size": fmt.Sprintf("%d", recPool)},
				ConfigScope: "server",
			})
		}
	}

	// background_merges_mutations_concurrency_ratio defaults to 2.
	if _, ok := settingInt(s, "background_merges_mutations_concurrency_ratio"); !ok {
		out = append(out, model.Finding{
			Category:    model.CategoryHardware,
			Severity:    model.SeverityInfo,
			Title:       "Avaliar background_merges_mutations_concurrency_ratio",
			Detail:      "Controla quantas tarefas concorrem por slot do pool. Padrão 2.",
			Suggestion:  "Em hosts com muitos núcleos e I/O rápido, 2–4 melhora o paralelismo de merges.",
			ConfigKeys:  map[string]string{"background_merges_mutations_concurrency_ratio": "2"},
			ConfigScope: "server",
		})
	}

	// max_server_memory_usage: recommend ~90% of RAM if unset (0 = auto).
	recMem := uint64(float64(hw.TotalMemory) * 0.90)
	if cur, ok := settingInt(s, "max_server_memory_usage"); ok {
		if cur == 0 {
			out = append(out, model.Finding{
				Category:    model.CategoryHardware,
				Severity:    model.SeverityInfo,
				Title:       "max_server_memory_usage em auto",
				Detail:      fmt.Sprintf("Host tem %s de RAM. O modo auto usa max_server_memory_usage_to_ram_ratio (padrão 0.9).", humanBytes(hw.TotalMemory)),
				Suggestion:  fmt.Sprintf("Se houver outros processos no host, fixar um limite explícito (~%s) evita OOM.", humanBytes(recMem)),
				ConfigKeys:  map[string]string{"max_server_memory_usage": fmt.Sprintf("%d", recMem)},
				ConfigScope: "server",
			})
		} else if uint64(cur) > hw.TotalMemory {
			out = append(out, model.Finding{
				Category:    model.CategoryHardware,
				Severity:    model.SeverityCritical,
				Title:       "max_server_memory_usage maior que a RAM física",
				Detail:      fmt.Sprintf("Atual %s, RAM do host %s. Risco de OOM kill.", humanBytes(uint64(cur)), humanBytes(hw.TotalMemory)),
				Suggestion:  fmt.Sprintf("Reduzir para ~%s (90%% da RAM).", humanBytes(recMem)),
				ConfigKeys:  map[string]string{"max_server_memory_usage": fmt.Sprintf("%d", recMem)},
				ConfigScope: "server",
			})
		}
	}

	// mark_cache_size: default 5 GiB. On large-RAM hosts raising it helps
	// point queries; on small hosts the default may be too large.
	recMark := uint64(float64(hw.TotalMemory) * 0.05)
	if recMark < 1*gib {
		recMark = 1 * gib
	}
	if cur, ok := settingInt(s, "mark_cache_size"); ok && hw.TotalMemory >= 64*gib {
		if uint64(cur) < recMark {
			out = append(out, model.Finding{
				Category:    model.CategoryHardware,
				Severity:    model.SeverityInfo,
				Title:       "mark_cache_size conservador para a RAM disponível",
				Detail:      fmt.Sprintf("Atual %s em host de %s. Marks em cache aceleram leituras seletivas.", humanBytes(uint64(cur)), humanBytes(hw.TotalMemory)),
				Suggestion:  fmt.Sprintf("Considerar ~%s (5%% da RAM) se houver muitas queries pontuais.", humanBytes(recMark)),
				ConfigKeys:  map[string]string{"mark_cache_size": fmt.Sprintf("%d", recMark)},
				ConfigScope: "server",
			})
		}
	}

	// Disk free space guard.
	if hw.DiskTotal > 0 {
		freeRatio := float64(hw.DiskFree) / float64(hw.DiskTotal)
		switch {
		case freeRatio < 0.10:
			out = append(out, model.Finding{
				Category:   model.CategoryHardware,
				Severity:   model.SeverityCritical,
				Title:      "Disco de dados quase cheio",
				Detail:     fmt.Sprintf("%s livres de %s (%.0f%%) em %s. Merges precisam de espaço temporário.", humanBytes(hw.DiskFree), humanBytes(hw.DiskTotal), freeRatio*100, hw.DiskPath),
				Suggestion: "Liberar espaço, mover partições para storage tier ou expandir o volume antes que merges falhem.",
			})
		case freeRatio < 0.20:
			out = append(out, model.Finding{
				Category:   model.CategoryHardware,
				Severity:   model.SeverityWarning,
				Title:      "Espaço em disco baixo",
				Detail:     fmt.Sprintf("%s livres de %s (%.0f%%) em %s.", humanBytes(hw.DiskFree), humanBytes(hw.DiskTotal), freeRatio*100, hw.DiskPath),
				Suggestion: "Planejar expansão; ClickHouse exige folga para merges grandes.",
			})
		}
	}

	return out
}

// humanBytes formats a byte count using binary (GiB/MiB) units.
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
