package report

import (
	"fmt"
	"io"
	"sort"

	"github.com/hugomesquita/database-fining/internal/model"
)

// Config emits ready-to-apply ClickHouse XML snippets gathered from every
// finding that carries ConfigKeys, split by scope (server vs profile). The
// last value wins when multiple findings touch the same key.
func Config(w io.Writer, r *model.Report) {
	server := map[string]string{}
	profile := map[string]string{}

	for _, f := range r.Findings {
		if len(f.ConfigKeys) == 0 {
			continue
		}
		target := server
		if f.ConfigScope == "profile" {
			target = profile
		}
		for k, v := range f.ConfigKeys {
			target[k] = v
		}
	}

	if len(server) == 0 && len(profile) == 0 {
		fmt.Fprintln(w, "<!-- Nenhuma alteração de config sugerida para o snapshot atual. -->")
		return
	}

	fmt.Fprintln(w, "<!-- Gerado por database-fining. Revise antes de aplicar. -->")

	if len(server) > 0 {
		fmt.Fprintln(w, "<!-- config.xml (configurações de servidor) -->")
		fmt.Fprintln(w, "<clickhouse>")
		for _, k := range sortedKeys(server) {
			fmt.Fprintf(w, "    <%s>%s</%s>\n", k, server[k], k)
		}
		fmt.Fprintln(w, "</clickhouse>")
	}

	if len(profile) > 0 {
		fmt.Fprintln(w, "\n<!-- users.xml (perfil default) -->")
		fmt.Fprintln(w, "<clickhouse>")
		fmt.Fprintln(w, "  <profiles>")
		fmt.Fprintln(w, "    <default>")
		for _, k := range sortedKeys(profile) {
			fmt.Fprintf(w, "      <%s>%s</%s>\n", k, profile[k], k)
		}
		fmt.Fprintln(w, "    </default>")
		fmt.Fprintln(w, "  </profiles>")
		fmt.Fprintln(w, "</clickhouse>")
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
