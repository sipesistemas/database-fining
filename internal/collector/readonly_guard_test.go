package collector

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// This guard enforces the inviolable "read-only" rule from CLAUDE.md: the
// collector package may only Ping and run SELECT queries. It parses every
// non-test source file in the package and fails the build if it finds either a
// write-capable driver call or a SQL string literal that is not a SELECT.
//
// If a future change legitimately needs a new driver method, update
// forbiddenDriverMethods deliberately — do not weaken this test casually.

// Driver methods on clickhouse-go/v2 that can mutate server state. Read-only
// access is limited to Query, QueryRow, Select and Ping.
var forbiddenDriverMethods = map[string]bool{
	"Exec":         true,
	"AsyncInsert":  true,
	"PrepareBatch": true,
}

// SQL verbs that write or change state. SELECT (and the WITH that precedes a
// SELECT) are the only statements allowed. The "system." database prefix is
// allowed (e.g. FROM system.parts); only the SYSTEM *command* is forbidden, so
// matches immediately followed by a dot are ignored.
var forbiddenSQLKeyword = regexp.MustCompile(`(?i)\b(INSERT|UPDATE|DELETE|ALTER|DROP|TRUNCATE|OPTIMIZE|KILL|RENAME|CREATE|ATTACH|DETACH|SYSTEM|GRANT|REVOKE)\b`)

// forbiddenSQLVerb returns the first write verb found in s (upper-cased), or ""
// if none. A "system." table-qualifier is not treated as the SYSTEM command.
func forbiddenSQLVerb(s string) string {
	for _, loc := range forbiddenSQLKeyword.FindAllStringIndex(s, -1) {
		word := s[loc[0]:loc[1]]
		if strings.EqualFold(word, "SYSTEM") && loc[1] < len(s) && s[loc[1]] == '.' {
			continue // "system." database prefix, not the SYSTEM command
		}
		return strings.ToUpper(word)
	}
	return ""
}

func TestCollectorIsReadOnly(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob sources: %v", err)
	}

	fset := token.NewFileSet()
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}

		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}

		ast.Inspect(f, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.CallExpr:
				if sel, ok := node.Fun.(*ast.SelectorExpr); ok {
					if forbiddenDriverMethods[sel.Sel.Name] {
						pos := fset.Position(sel.Pos())
						t.Errorf("%s: chamada de escrita proibida no driver: .%s(...) — o collector é somente leitura",
							pos, sel.Sel.Name)
					}
				}
			case *ast.BasicLit:
				if node.Kind == token.STRING {
					if kw := forbiddenSQLVerb(node.Value); kw != "" {
						pos := fset.Position(node.Pos())
						t.Errorf("%s: SQL de escrita proibido (%q) em string literal — apenas SELECT é permitido",
							pos, kw)
					}
				}
			}
			return true
		})
	}
}
