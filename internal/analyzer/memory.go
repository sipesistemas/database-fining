package analyzer

import (
	"fmt"

	"github.com/hugomesquita/database-fining/internal/model"
)

// Memory tuning heuristics. ClickHouse ships defaults sized for beefy servers
// (e.g. a 5 GiB mark cache); on small-RAM hosts those defaults eat into the
// budget that queries and the OS page cache need. We only flag cache sizing on
// hosts below smallRAMThreshold, where the defaults are clearly oversized.
const (
	smallRAMThreshold   = 32 * gib // above this, ClickHouse defaults are reasonable
	cacheMaxRAMFraction = 0.10     // a single cache above this share of RAM is too large
	cacheRecRAMFraction = 0.05     // recommended share per cache on small hosts
	minCacheBytes       = 512 * 1024 * 1024
	memRatioWarn        = 0.85 // effective server limit above this share of RAM starves page cache
	memRatioRec         = 0.80
	concurrencyPerCPU   = 4 // max_concurrent_queries above this * vCPUs is suspicious
)

// analyzeMemory inspects memory limits, cache sizing and query concurrency,
// comparing them against the host RAM and CPU count. It complements
// analyzeHardware, which focuses on pool sizing and disk.
func analyzeMemory(s *model.Snapshot) []model.Finding {
	var out []model.Finding
	hw := s.Hardware
	if hw.TotalMemory == 0 {
		return out // analyzeHardware already warns about incomplete hardware
	}
	ram := hw.TotalMemory

	// Effective server memory limit: min(max_server_memory_usage,
	// ratio * RAM). Either may be set; ClickHouse takes the lower.
	limit := float64(ram) * 0.9 // default ratio
	if v, ok := settingFloat(s, "max_server_memory_usage_to_ram_ratio"); ok {
		limit = float64(ram) * v
	}
	if v, ok := settingInt(s, "max_server_memory_usage"); ok && v > 0 && float64(v) < limit {
		limit = float64(v)
	}

	// Caches that are oversized for the available RAM. On small hosts the 5 GiB
	// default mark/index-mark caches are a large slice of the budget.
	if ram < smallRAMThreshold {
		recCache := uint64(float64(ram) * cacheRecRAMFraction)
		if recCache < minCacheBytes {
			recCache = minCacheBytes
		}
		maxCache := uint64(float64(ram) * cacheMaxRAMFraction)

		for _, key := range []string{"mark_cache_size", "index_mark_cache_size"} {
			if cur, ok := settingInt(s, key); ok && uint64(cur) > maxCache {
				out = append(out, model.Finding{
					Category: model.CategoryHardware,
					Severity: model.SeverityWarning,
					Title:    fmt.Sprintf("%s grande demais para a RAM do host", key),
					Detail: fmt.Sprintf("Atual %s (%.0f%% de %s de RAM). O padrão do ClickHouse é dimensionado para hosts grandes.",
						humanBytes(uint64(cur)), float64(cur)/float64(ram)*100, humanBytes(ram)),
					Suggestion:  fmt.Sprintf("Reduzir para ~%s (5%% da RAM); libera memória para working set de queries e page cache.", humanBytes(recCache)),
					ConfigKeys:  map[string]string{key: fmt.Sprintf("%d", recCache)},
					ConfigScope: "server",
				})
			}
		}
	}

	// Uncompressed cache reserved while disabled at query level. It only
	// populates when use_uncompressed_cache=1, so a large cap is a latent
	// memory reservation waiting to compete with the server limit.
	if use, ok := settingInt(s, "use_uncompressed_cache"); ok && use == 0 {
		if cur, ok := settingInt(s, "uncompressed_cache_size"); ok && uint64(cur) > 1*gib {
			out = append(out, model.Finding{
				Category: model.CategoryHardware,
				Severity: model.SeverityInfo,
				Title:    "uncompressed_cache_size grande com cache desabilitado",
				Detail: fmt.Sprintf("Reservado %s mas use_uncompressed_cache=0. O cache não é usado em workload analítico típico; o teto vira reserva latente se alguém ligar o uso por query.",
					humanBytes(uint64(cur))),
				Suggestion:  "Manter desabilitado e reduzir o teto para ~1 GiB, evitando reserva surpresa de memória.",
				ConfigKeys:  map[string]string{"uncompressed_cache_size": fmt.Sprintf("%d", 1*gib)},
				ConfigScope: "server",
			})
		}
	}

	// Memory ratio too aggressive on a small host: too little left for the OS
	// page cache that ClickHouse relies on for reads.
	if ram < smallRAMThreshold && limit/float64(ram) > memRatioWarn {
		rec := uint64(float64(ram) * memRatioRec)
		out = append(out, model.Finding{
			Category: model.CategoryHardware,
			Severity: model.SeverityWarning,
			Title:    "Limite de memória deixa pouca folga para o page cache",
			Detail: fmt.Sprintf("Limite efetivo ~%s de %s (%.0f%%). Sobra ~%s para SO e page cache, que o ClickHouse usa para acelerar leituras.",
				humanBytes(uint64(limit)), humanBytes(ram), limit/float64(ram)*100, humanBytes(ram-uint64(limit))),
			Suggestion:  fmt.Sprintf("Em host pequeno, baixar max_server_memory_usage_to_ram_ratio para ~%.2f (limite ~%s).", memRatioRec, humanBytes(rec)),
			ConfigKeys:  map[string]string{"max_server_memory_usage_to_ram_ratio": fmt.Sprintf("%.2f", memRatioRec)},
			ConfigScope: "server",
		})
	}

	// High query concurrency with no per-query memory cap: a burst of queries
	// can collectively exhaust the server limit and trigger an OOM.
	if cur, ok := settingInt(s, "max_concurrent_queries"); ok {
		recConc := int64(hw.LogicalCPUs * concurrencyPerCPU)
		if recConc < 100 {
			recConc = 100
		}
		if cur > recConc {
			perQuery, hasCap := settingInt(s, "max_memory_usage")
			detail := fmt.Sprintf("max_concurrent_queries=%d para %d vCPUs.", cur, hw.LogicalCPUs)
			if hasCap && perQuery == 0 {
				detail += " Sem max_memory_usage (teto por query), um pico de queries pode somar até o limite do servidor e causar OOM."
			}
			out = append(out, model.Finding{
				Category:    model.CategoryHardware,
				Severity:    model.SeverityWarning,
				Title:       "max_concurrent_queries alto para o número de núcleos",
				Detail:      detail,
				Suggestion:  fmt.Sprintf("Reduzir para ~%d; concorrência alta não aumenta throughput em host com poucos núcleos e eleva o risco de contenção/OOM.", recConc),
				ConfigKeys:  map[string]string{"max_concurrent_queries": fmt.Sprintf("%d", recConc)},
				ConfigScope: "server",
			})
		}
	}

	// No per-query memory cap: a single runaway query can claim the whole
	// server budget. Recommend ~half the effective limit as a guardrail.
	if cur, ok := settingInt(s, "max_memory_usage"); ok && cur == 0 {
		rec := uint64(limit * 0.5)
		out = append(out, model.Finding{
			Category: model.CategoryHardware,
			Severity: model.SeverityInfo,
			Title:    "Sem teto de memória por query (max_memory_usage=0)",
			Detail:   "Uma única query pode consumir todo o orçamento de memória do servidor antes de ser interrompida.",
			Suggestion: fmt.Sprintf("Definir max_memory_usage ~%s (metade do limite efetivo) no perfil; queries grandes derramam para disco ou falham isoladas em vez de derrubar o servidor.",
				humanBytes(rec)),
			ConfigKeys:  map[string]string{"max_memory_usage": fmt.Sprintf("%d", rec)},
			ConfigScope: "profile",
		})
	}

	return out
}
