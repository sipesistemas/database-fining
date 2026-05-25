package analyzer

import (
	"fmt"

	"github.com/hugomesquita/database-fining/internal/model"
)

// Thresholds for parts-per-partition. ClickHouse starts throttling inserts
// around parts_to_throw_insert (default 300) and delaying at
// parts_to_delay_insert (default 150).
const (
	partsWarnPerPartition = 150
	partsCritPerPartition = 300
	smallPartRowThreshold = 1_000_000 // avg rows/part below this hints over-partitioning
)

// analyzeParts flags tables with too many parts per partition, a classic
// "too many parts" precursor, and tables whose parts are tiny (over-
// partitioning or too-frequent small inserts).
func analyzeParts(s *model.Snapshot) []model.Finding {
	var out []model.Finding

	for _, p := range s.Parts {
		name := fmt.Sprintf("%s.%s", p.Database, p.Table)

		switch {
		case p.MaxPartsPart >= partsCritPerPartition:
			out = append(out, model.Finding{
				Category:   model.CategoryParts,
				Severity:   model.SeverityCritical,
				Title:      fmt.Sprintf("%s: partição com %d parts", name, p.MaxPartsPart),
				Detail:     "Próximo de parts_to_throw_insert (300). Inserts podem ser rejeitados com 'Too many parts'.",
				Suggestion: "Reduzir frequência/aumentar tamanho dos inserts (batch), revisar a chave de PARTITION BY e garantir que merges estão acompanhando.",
			})
		case p.MaxPartsPart >= partsWarnPerPartition:
			out = append(out, model.Finding{
				Category:   model.CategoryParts,
				Severity:   model.SeverityWarning,
				Title:      fmt.Sprintf("%s: partição com %d parts", name, p.MaxPartsPart),
				Detail:     "Acima de parts_to_delay_insert (150); inserts começam a ser atrasados.",
				Suggestion: "Agrupar inserts em lotes maiores e verificar a taxa de merges.",
			})
		}

		// Over-partitioning: many partitions with few rows each.
		if p.Partitions > 0 && p.PartCount > 0 {
			avgRowsPerPart := p.Rows / p.PartCount
			if p.Partitions >= 100 && avgRowsPerPart < smallPartRowThreshold {
				out = append(out, model.Finding{
					Category:   model.CategoryParts,
					Severity:   model.SeverityWarning,
					Title:      fmt.Sprintf("%s: muitas partições com poucas linhas", name),
					Detail:     fmt.Sprintf("%d partições, %d parts, média de %d linhas/part. Partições pequenas demais geram muitos parts pequenos.", p.Partitions, p.PartCount, avgRowsPerPart),
					Suggestion: "Duas causas possíveis: (1) PARTITION BY granular demais (ex: por dia/hora) — considerar agrupar por mês; ou (2) timestamps anômalos (datas fora da faixa real) espalhando linhas por meses fantasma. Verifique a distribuição com 'SELECT partition, sum(rows) FROM system.parts WHERE active AND table=... GROUP BY partition'; se a chave já for mensal, valide os valores de data na ingestão.",
				})
			}
		}
	}

	// Cross-check engine throttling settings against observed pressure.
	if maxParts, ok := settingInt(s, "parts_to_throw_insert"); ok && hasHotPartition(s) {
		out = append(out, model.Finding{
			Category:    model.CategoryParts,
			Severity:    model.SeverityInfo,
			Title:       "Pressão de parts detectada",
			Detail:      fmt.Sprintf("parts_to_throw_insert=%d. Aumentar o limite mascara o problema sem resolvê-lo.", maxParts),
			Suggestion:  "Preferir corrigir o padrão de inserts e merges; só ajustar o limite como medida temporária e consciente.",
			ConfigScope: "server",
		})
	}

	return out
}

func hasHotPartition(s *model.Snapshot) bool {
	for _, p := range s.Parts {
		if p.MaxPartsPart >= partsWarnPerPartition {
			return true
		}
	}
	return false
}
