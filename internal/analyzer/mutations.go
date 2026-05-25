package analyzer

import (
	"fmt"
	"time"

	"github.com/hugomesquita/database-fining/internal/model"
)

// stuckMutationAge is when an unfinished mutation is considered stuck.
const stuckMutationAge = 1 * time.Hour

// analyzeMutations flags failed and long-pending mutations, which are a
// common source of silent merge-pool starvation and growing parts.
func analyzeMutations(s *model.Snapshot) []model.Finding {
	var out []model.Finding
	now := s.CollectedAt
	if now.IsZero() {
		now = time.Now()
	}

	var pending, failed int
	for _, m := range s.Mutations {
		name := fmt.Sprintf("%s.%s", m.Database, m.Table)

		if m.LatestFailReason != "" {
			failed++
			out = append(out, model.Finding{
				Category:   model.CategoryMutations,
				Severity:   model.SeverityCritical,
				Title:      fmt.Sprintf("%s: mutation falhando", name),
				Detail:     fmt.Sprintf("mutation_id=%s, parts_to_do=%d, erro: %s", m.MutationID, m.PartsToDo, truncate(m.LatestFailReason, 200)),
				Suggestion: "Mutations que falham repetem indefinidamente e seguram o pool. Investigar o erro, corrigir o comando ou usar KILL MUTATION.",
			})
			continue
		}

		if !m.IsDone {
			pending++
			age := now.Sub(m.CreateTime)
			if age > stuckMutationAge {
				out = append(out, model.Finding{
					Category:   model.CategoryMutations,
					Severity:   model.SeverityWarning,
					Title:      fmt.Sprintf("%s: mutation pendente há %s", name, age.Round(time.Minute)),
					Detail:     fmt.Sprintf("mutation_id=%s, parts_to_do=%d, comando: %s", m.MutationID, m.PartsToDo, truncate(m.Command, 120)),
					Suggestion: "Mutations reescrevem parts inteiros. Verificar progresso vs. carga de merge; agrupar ALTERs e evitar updates frequentes em tabelas grandes.",
				})
			}
		}
	}

	if pending > 10 {
		out = append(out, model.Finding{
			Category:   model.CategoryMutations,
			Severity:   model.SeverityWarning,
			Title:      fmt.Sprintf("%d mutations pendentes acumuladas", pending),
			Detail:     "Muitas mutations concorrentes competem com merges pelo mesmo pool.",
			Suggestion: "Consolidar ALTER UPDATE/DELETE em menos comandos e considerar lightweight DELETE quando aplicável.",
		})
	}

	if len(s.Mutations) == 0 {
		out = append(out, model.Finding{
			Category: model.CategoryMutations,
			Severity: model.SeverityInfo,
			Title:    "Nenhuma mutation pendente ou com falha",
			Detail:   "system.mutations sem registros não concluídos.",
		})
	}

	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
