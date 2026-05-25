package analyzer

import (
	"fmt"

	"github.com/hugomesquita/database-fining/internal/model"
)

// slowMergeSeconds is when an in-flight merge is considered long-running.
const slowMergeSeconds = 600 // 10 minutes

// analyzeMerges inspects in-flight merges for long-running or memory-heavy
// operations and relates the load to the configured pool size.
func analyzeMerges(s *model.Snapshot) []model.Finding {
	var out []model.Finding

	var active, mutations int
	for _, m := range s.Merges {
		if m.IsMutation {
			mutations++
		} else {
			active++
		}
		name := fmt.Sprintf("%s.%s", m.Database, m.Table)

		if m.Elapsed > slowMergeSeconds && m.Progress < 0.5 {
			out = append(out, model.Finding{
				Category:   model.CategoryMerges,
				Severity:   model.SeverityWarning,
				Title:      fmt.Sprintf("%s: merge lento em andamento", name),
				Detail:     fmt.Sprintf("%.0fs decorridos, %.0f%% concluído, %d parts, %s.", m.Elapsed, m.Progress*100, m.NumParts, humanBytes(m.TotalSize)),
				Suggestion: "Merges grandes competem por I/O e slots do pool. Verificar throughput de disco e max_bytes_to_merge_at_max_space_in_pool.",
			})
		}
	}

	// Compare active merges to the pool budget.
	if pool, ok := settingInt(s, "background_pool_size"); ok && pool > 0 {
		used := float64(active+mutations) / float64(pool)
		if used >= 0.9 {
			out = append(out, model.Finding{
				Category:    model.CategoryMerges,
				Severity:    model.SeverityWarning,
				Title:       "Pool de background quase saturado",
				Detail:      fmt.Sprintf("%d merges/mutations ativos para background_pool_size=%d (%.0f%%).", active+mutations, pool, used*100),
				Suggestion:  "Se o host tiver CPU/I/O ociosos, aumentar background_pool_size; caso contrário, reduzir a taxa de inserts/ALTERs.",
				ConfigKeys:  map[string]string{"background_pool_size": fmt.Sprintf("%d", pool*2)},
				ConfigScope: "server",
			})
		}
	}

	if len(s.Merges) == 0 {
		out = append(out, model.Finding{
			Category: model.CategoryMerges,
			Severity: model.SeverityInfo,
			Title:    "Nenhum merge em andamento no momento",
			Detail:   "Snapshot instantâneo; rode periodicamente para capturar picos de atividade de merge.",
		})
	}

	return out
}
