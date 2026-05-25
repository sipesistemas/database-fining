package analyzer

import (
	"testing"
	"time"

	"github.com/hugomesquita/database-fining/internal/model"
)

// baseSnapshot returns a healthy snapshot that produces no warning/critical
// findings; tests mutate it to trigger specific rules.
func baseSnapshot() *model.Snapshot {
	return &model.Snapshot{
		CollectedAt: time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC),
		Version:     "26.5.1",
		Hardware: model.Hardware{
			LogicalCPUs:  16,
			PhysicalCPUs: 16,
			TotalMemory:  32 * gib,
			FreeMemory:   16 * gib,
			DiskTotal:    1000 * gib,
			DiskFree:     800 * gib,
			DiskPath:     "/var/lib/clickhouse",
		},
		ServerSettings: map[string]model.ServerSetting{
			"background_pool_size":                          {Name: "background_pool_size", Value: "32"},
			"background_merges_mutations_concurrency_ratio": {Name: "background_merges_mutations_concurrency_ratio", Value: "2"},
			"max_server_memory_usage":                       {Name: "max_server_memory_usage", Value: "28000000000"},
			"mark_cache_size":                               {Name: "mark_cache_size", Value: "5368709120"},
		},
	}
}

// has reports whether any finding matches the category at the given severity.
func has(findings []model.Finding, cat model.Category, sev model.Severity) bool {
	for _, f := range findings {
		if f.Category == cat && f.Severity == sev {
			return true
		}
	}
	return false
}

func findTitle(findings []model.Finding, substr string) *model.Finding {
	for i := range findings {
		if contains(findings[i].Title, substr) {
			return &findings[i]
		}
	}
	return nil
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestAnalyzeHardware_PoolBelowCores(t *testing.T) {
	s := baseSnapshot()
	s.ServerSettings["background_pool_size"] = model.ServerSetting{Name: "background_pool_size", Value: "8"}

	f := findTitle(analyzeHardware(s), "background_pool_size abaixo")
	if f == nil {
		t.Fatal("esperava finding de pool abaixo dos núcleos")
	}
	if got := f.ConfigKeys["background_pool_size"]; got != "16" {
		t.Errorf("config sugerida = %q, quer 16", got)
	}
	if f.Severity != model.SeverityWarning {
		t.Errorf("severidade = %q, quer warning", f.Severity)
	}
}

func TestAnalyzeHardware_MemoryOverRAMIsCritical(t *testing.T) {
	s := baseSnapshot()
	s.ServerSettings["max_server_memory_usage"] = model.ServerSetting{Name: "max_server_memory_usage", Value: "64000000000"}

	if !has(analyzeHardware(s), model.CategoryHardware, model.SeverityCritical) {
		t.Fatal("esperava finding crítico para memória acima da RAM")
	}
}

func TestAnalyzeHardware_DiskAlmostFullIsCritical(t *testing.T) {
	s := baseSnapshot()
	s.Hardware.DiskFree = 50 * gib // 5% de 1000 GiB

	if !has(analyzeHardware(s), model.CategoryHardware, model.SeverityCritical) {
		t.Fatal("esperava finding crítico para disco quase cheio")
	}
}

func TestAnalyzeHardware_HealthyHasNoWarnings(t *testing.T) {
	s := baseSnapshot()
	for _, f := range analyzeHardware(s) {
		if f.Severity == model.SeverityCritical || f.Severity == model.SeverityWarning {
			t.Errorf("snapshot saudável produziu finding %s: %s", f.Severity, f.Title)
		}
	}
}

func TestAnalyzeParts_TooManyPartsThrow(t *testing.T) {
	s := baseSnapshot()
	s.Parts = []model.Part{{
		Database: "db", Table: "t", PartCount: 350, Rows: 350_000_000,
		Partitions: 1, MaxPartsPart: 350,
	}}

	if !has(analyzeParts(s), model.CategoryParts, model.SeverityCritical) {
		t.Fatal("esperava finding crítico de too-many-parts")
	}
}

func TestAnalyzeParts_OverPartitioning(t *testing.T) {
	s := baseSnapshot()
	s.Parts = []model.Part{{
		Database: "db", Table: "t", PartCount: 500, Rows: 1000,
		Partitions: 365, MaxPartsPart: 3,
	}}

	if findTitle(analyzeParts(s), "over-partitioning") == nil {
		t.Fatal("esperava finding de over-partitioning")
	}
}

func TestAnalyzeMutations_FailedIsCritical(t *testing.T) {
	s := baseSnapshot()
	s.Mutations = []model.Mutation{{
		Database: "db", Table: "t", MutationID: "0001",
		Command: "UPDATE", CreateTime: s.CollectedAt.Add(-2 * time.Minute),
		PartsToDo: 3, LatestFailReason: "DB::Exception: bad cast",
	}}

	if !has(analyzeMutations(s), model.CategoryMutations, model.SeverityCritical) {
		t.Fatal("esperava finding crítico de mutation falhando")
	}
}

func TestAnalyzeMutations_StuckPending(t *testing.T) {
	s := baseSnapshot()
	s.Mutations = []model.Mutation{{
		Database: "db", Table: "t", MutationID: "0002",
		Command: "DELETE", CreateTime: s.CollectedAt.Add(-3 * time.Hour),
		PartsToDo: 10, IsDone: false,
	}}

	if !has(analyzeMutations(s), model.CategoryMutations, model.SeverityWarning) {
		t.Fatal("esperava finding de mutation pendente há muito tempo")
	}
}

func TestAnalyzeMerges_PoolSaturated(t *testing.T) {
	s := baseSnapshot()
	s.ServerSettings["background_pool_size"] = model.ServerSetting{Name: "background_pool_size", Value: "4"}
	for i := 0; i < 4; i++ {
		s.Merges = append(s.Merges, model.Merge{Database: "db", Table: "t", Progress: 0.3})
	}

	if findTitle(analyzeMerges(s), "Pool de background quase saturado") == nil {
		t.Fatal("esperava finding de pool saturado")
	}
}

// prodSnapshot mirrors the small-RAM production host (24 GiB / 12 vCPU) with
// the cache/concurrency settings observed on it.
func prodSnapshot() *model.Snapshot {
	s := baseSnapshot()
	s.Hardware.LogicalCPUs = 12
	s.Hardware.PhysicalCPUs = 12
	s.Hardware.TotalMemory = 24 * gib
	s.ServerSettings["max_server_memory_usage"] = model.ServerSetting{Name: "max_server_memory_usage", Value: "22675979059"}
	s.ServerSettings["max_server_memory_usage_to_ram_ratio"] = model.ServerSetting{Name: "max_server_memory_usage_to_ram_ratio", Value: "0.9"}
	s.ServerSettings["mark_cache_size"] = model.ServerSetting{Name: "mark_cache_size", Value: "5368709120"}
	s.ServerSettings["index_mark_cache_size"] = model.ServerSetting{Name: "index_mark_cache_size", Value: "5368709120"}
	s.ServerSettings["uncompressed_cache_size"] = model.ServerSetting{Name: "uncompressed_cache_size", Value: "8589934592"}
	s.ServerSettings["use_uncompressed_cache"] = model.ServerSetting{Name: "use_uncompressed_cache", Value: "0"}
	s.ServerSettings["max_concurrent_queries"] = model.ServerSetting{Name: "max_concurrent_queries", Value: "1000"}
	s.ServerSettings["max_memory_usage"] = model.ServerSetting{Name: "max_memory_usage", Value: "0"}
	return s
}

func TestAnalyzeMemory_HealthyBaseHasNoFindings(t *testing.T) {
	if f := analyzeMemory(baseSnapshot()); len(f) != 0 {
		t.Fatalf("snapshot saudável produziu %d findings de memória: %+v", len(f), f)
	}
}

func TestAnalyzeMemory_MarkCacheTooLargeOnSmallRAM(t *testing.T) {
	f := findTitle(analyzeMemory(prodSnapshot()), "mark_cache_size grande demais")
	if f == nil {
		t.Fatal("esperava finding de mark cache grande demais")
	}
	if f.Severity != model.SeverityWarning {
		t.Errorf("severidade = %q, quer warning", f.Severity)
	}
	if got := f.ConfigKeys["mark_cache_size"]; got == "" || got == "5368709120" {
		t.Errorf("config sugerida = %q, esperava valor reduzido", got)
	}
}

func TestAnalyzeMemory_RatioStarvesPageCache(t *testing.T) {
	f := findTitle(analyzeMemory(prodSnapshot()), "pouca folga para o page cache")
	if f == nil {
		t.Fatal("esperava finding de page cache faminta")
	}
	if f.ConfigKeys["max_server_memory_usage_to_ram_ratio"] != "0.80" {
		t.Errorf("ratio sugerido = %q, quer 0.80", f.ConfigKeys["max_server_memory_usage_to_ram_ratio"])
	}
}

func TestAnalyzeMemory_ConcurrencyTooHigh(t *testing.T) {
	f := findTitle(analyzeMemory(prodSnapshot()), "max_concurrent_queries alto")
	if f == nil {
		t.Fatal("esperava finding de concorrência alta")
	}
	if f.Severity != model.SeverityWarning {
		t.Errorf("severidade = %q, quer warning", f.Severity)
	}
}

func TestAnalyzeMemory_NoPerQueryCap(t *testing.T) {
	f := findTitle(analyzeMemory(prodSnapshot()), "Sem teto de memória por query")
	if f == nil {
		t.Fatal("esperava finding de max_memory_usage=0")
	}
	if f.ConfigScope != "profile" {
		t.Errorf("escopo = %q, quer profile", f.ConfigScope)
	}
}

func TestAnalyzeInserts_HealthyBaseHasNoFindings(t *testing.T) {
	if f := analyzeInserts(baseSnapshot()); len(f) != 0 {
		t.Fatalf("snapshot saudável produziu %d findings de insert: %+v", len(f), f)
	}
}

func TestAnalyzeInserts_RaisedThrowWithAsyncOff(t *testing.T) {
	s := baseSnapshot()
	s.ServerSettings["parts_to_throw_insert"] = model.ServerSetting{Name: "parts_to_throw_insert", Value: "3000"}
	s.ServerSettings["async_insert"] = model.ServerSetting{Name: "async_insert", Value: "0"}

	f := findTitle(analyzeInserts(s), "parts_to_throw_insert elevado")
	if f == nil {
		t.Fatal("esperava finding de parts_to_throw_insert elevado")
	}
	if f.Severity != model.SeverityWarning {
		t.Errorf("severidade = %q, quer warning", f.Severity)
	}
	if f.ConfigKeys["async_insert"] != "1" || f.ConfigScope != "profile" {
		t.Errorf("esperava sugerir async_insert=1 no perfil, veio %+v / %q", f.ConfigKeys, f.ConfigScope)
	}
}

func TestAnalyzeInserts_AsyncOffWithDefaultThresholds(t *testing.T) {
	s := baseSnapshot()
	s.ServerSettings["async_insert"] = model.ServerSetting{Name: "async_insert", Value: "0"}

	f := findTitle(analyzeInserts(s), "async_insert desabilitado")
	if f == nil {
		t.Fatal("esperava finding info de async_insert desabilitado")
	}
	if f.Severity != model.SeverityInfo {
		t.Errorf("severidade = %q, quer info", f.Severity)
	}
}

func TestAnalyze_SortsBySeverity(t *testing.T) {
	s := baseSnapshot()
	s.ServerSettings["max_server_memory_usage"] = model.ServerSetting{Name: "max_server_memory_usage", Value: "64000000000"} // crítico
	s.ServerSettings["background_pool_size"] = model.ServerSetting{Name: "background_pool_size", Value: "8"}                 // warning

	rep := Analyze(s)
	if len(rep.Findings) < 2 {
		t.Fatalf("esperava ao menos 2 findings, veio %d", len(rep.Findings))
	}
	for i := 1; i < len(rep.Findings); i++ {
		if rep.Findings[i-1].Severity.Weight() < rep.Findings[i].Severity.Weight() {
			t.Fatalf("findings fora de ordem em %d: %s antes de %s",
				i, rep.Findings[i-1].Severity, rep.Findings[i].Severity)
		}
	}
}
