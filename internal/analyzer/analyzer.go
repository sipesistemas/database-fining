// Package analyzer turns a collected Snapshot into a Report of findings and
// tuning recommendations.
package analyzer

import (
	"sort"
	"strconv"
	"time"

	"github.com/hugomesquita/database-fining/internal/model"
)

// rule is one analysis pass over a snapshot.
type rule func(s *model.Snapshot) []model.Finding

// Analyze runs every rule and returns a report sorted by severity (most
// urgent first), then by category.
func Analyze(s *model.Snapshot) *model.Report {
	rules := []rule{
		analyzeHardware,
		analyzeMemory,
		analyzeInserts,
		analyzeParts,
		analyzeMerges,
		analyzeMutations,
	}

	var findings []model.Finding
	for _, r := range rules {
		findings = append(findings, r(s)...)
	}

	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Severity.Weight() != findings[j].Severity.Weight() {
			return findings[i].Severity.Weight() > findings[j].Severity.Weight()
		}
		return findings[i].Category < findings[j].Category
	})

	return &model.Report{
		GeneratedAt: time.Now(),
		Snapshot:    s,
		Findings:    findings,
	}
}

// settingInt reads a numeric server setting, returning ok=false when absent
// or unparseable.
func settingInt(s *model.Snapshot, name string) (int64, bool) {
	v, ok := s.ServerSettings[name]
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseInt(v.Value, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// settingFloat reads a fractional server setting (e.g. a *_ratio), returning
// ok=false when absent or unparseable.
func settingFloat(s *model.Snapshot, name string) (float64, bool) {
	v, ok := s.ServerSettings[name]
	if !ok {
		return 0, false
	}
	f, err := strconv.ParseFloat(v.Value, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}
