package analyzer

import (
	"fmt"

	"github.com/hugomesquita/database-fining/internal/model"
)

// ClickHouse defaults. parts_to_throw_insert defaults to 300; values well above
// it usually mean someone raised the limit to silence "too many parts" instead
// of fixing the insert pattern that creates the parts.
const (
	defaultPartsToThrow = 300
	raisedPartsToThrow  = 1000 // above this we treat the limit as deliberately inflated
)

// analyzeInserts looks for the micro-insert anti-pattern behind most "too many
// parts" errors: many tiny inserts each create a part, and the fix is batching
// (client-side) or async inserts (server-side), not a higher throw threshold.
func analyzeInserts(s *model.Snapshot) []model.Finding {
	var out []model.Finding

	throw, hasThrow := settingInt(s, "parts_to_throw_insert")
	async, hasAsync := settingInt(s, "async_insert")
	asyncOff := hasAsync && async == 0

	// A throw threshold raised far above the default is a band-aid: it delays
	// the error without reducing how fast parts are created.
	if hasThrow && throw >= raisedPartsToThrow {
		f := model.Finding{
			Category: model.CategoryParts,
			Severity: model.SeverityWarning,
			Title:    "parts_to_throw_insert elevado mascara a pressão de parts",
			Detail: fmt.Sprintf("Atual %d (padrão %d). Subir o teto adia o erro 'too many parts' mas não reduz a criação de parts; em picos o limite ainda é atingido.",
				throw, defaultPartsToThrow),
			Suggestion: "Atacar a causa: inserir em lotes maiores (batch no cliente) e/ou habilitar async_insert para o servidor agrupar inserts pequenos.",
		}
		// If async inserts are off, point straight at the most effective fix.
		if asyncOff {
			f.Suggestion = "Habilitar async_insert=1: o servidor agrupa inserts pequenos em poucos parts grandes, eliminando a causa do 'too many parts'. Mantenha wait_for_async_insert=1 para preservar a confirmação de gravação."
			f.ConfigKeys = map[string]string{"async_insert": "1"}
			f.ConfigScope = "profile"
		}
		out = append(out, f)
		return out
	}

	// Even with default thresholds, async inserts off plus a high-ingestion
	// workload is worth flagging as an option.
	if asyncOff {
		out = append(out, model.Finding{
			Category:    model.CategoryParts,
			Severity:    model.SeverityInfo,
			Title:       "async_insert desabilitado",
			Detail:      "Cada INSERT cria ao menos um part. Com muitos inserts pequenos e frequentes isso leva a 'too many parts'.",
			Suggestion:  "Se a ingestão é de muitos inserts pequenos, habilitar async_insert=1 (com wait_for_async_insert=1) agrupa-os em parts maiores.",
			ConfigKeys:  map[string]string{"async_insert": "1"},
			ConfigScope: "profile",
		})
	}

	return out
}
