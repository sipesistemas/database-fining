package report

import (
	"encoding/json"
	"io"

	"github.com/hugomesquita/database-fining/internal/model"
)

// JSON writes the report (with the underlying snapshot) as indented JSON.
func JSON(w io.Writer, r *model.Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	out := struct {
		GeneratedAt string          `json:"generated_at"`
		Snapshot    *model.Snapshot `json:"snapshot"`
		Findings    []model.Finding `json:"findings"`
	}{
		GeneratedAt: r.GeneratedAt.Format("2006-01-02T15:04:05Z07:00"),
		Snapshot:    r.Snapshot,
		Findings:    r.Findings,
	}
	return enc.Encode(out)
}
