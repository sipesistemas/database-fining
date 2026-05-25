# CLAUDE.md

Orientações para o Claude Code trabalhar neste repositório.

## O que é

CLI em Go (`chfining`) que conecta a um ClickHouse, coleta hardware do host +
métricas de runtime (parts, merges, mutations, settings) e emite sugestões de
tuning. A arquitetura é isolada por camada para permitir suportar outros bancos
no futuro.

## Comandos

```sh
go build -o bin/chfining ./cmd/chfining   # compila o binário
go test ./...                             # roda os testes (analisadores)
go vet ./...                              # análise estática
gofmt -w ./cmd ./internal                 # formata
```

`go` não está no PATH padrão de algumas sessões; use `/usr/local/go/bin/go` se
`go` não for encontrado.

### Teste end-to-end com ClickHouse real

```sh
docker run -d --rm --name ch -p 19000:9000 -e CLICKHOUSE_PASSWORD=secret clickhouse/clickhouse-server:latest
CH_PASSWORD=secret ./bin/chfining -addr localhost:19000 -data-path "$(pwd)"
docker rm -f ch
```

## Arquitetura

Fluxo: **collector → model.Snapshot → analyzer → model.Report → report**.

- `internal/model` — tipos compartilhados (`Snapshot`, `Finding`, `Severity`,
  `Category`). Nenhuma dependência das outras camadas; tudo aponta para cá.
- `internal/collector` — conexão via `clickhouse-go/v2` (protocolo nativo) lendo
  `system.{server_settings,settings,parts,merges,mutations,asynchronous_metrics}`;
  `hardware.go` lê CPU/RAM/disco/load via `gopsutil`.
- `internal/analyzer` — uma `rule` por subsistema (`hardware.go`, `parts.go`,
  `merges.go`, `mutations.go`). `analyzer.go` orquestra e ordena por severidade.
  São funções puras sobre `*model.Snapshot` — testáveis sem rede.
- `internal/report` — renderiza `cli.go` / `json.go` / `config.go` a partir do
  `*model.Report`.
- `cmd/chfining` — flags, conexão e dispatch de formato. Sai com código `2` se
  houver finding crítico (útil em CI).

## Regra inviolável: somente leitura

A ferramenta **NUNCA** pode executar comandos que alterem dados ou estado do
servidor: nada de `INSERT`, `DELETE`, `UPDATE`, `ALTER`, `DROP`, `TRUNCATE`,
`OPTIMIZE`, `KILL`, `CREATE`/`ATTACH`/`DETACH` ou `SYSTEM`. Apenas `SELECT` (e
`Ping`) nas system tables. Recomendações de mudança são **sugeridas** ao usuário
(texto/config), nunca aplicadas pelo programa. Qualquer PR que introduza um
comando de escrita deve ser rejeitado.

## Convenções

- **Adicionar uma regra de análise**: escreva uma função
  `func(s *model.Snapshot) []model.Finding` no pacote `analyzer`, registre-a no
  slice `rules` em `analyzer.go`, e adicione um teste de mesa em
  `analyzer_test.go` (use `baseSnapshot()` como base saudável e mute o campo que
  a regra inspeciona).
- **Sugestões aplicáveis**: preencha `Finding.ConfigKeys` + `ConfigScope`
  (`"server"` → config.xml, `"profile"` → users.xml) para que o formato `config`
  gere o XML automaticamente. Sem isso, o finding é só diagnóstico.
- **Severidade**: `critical` para risco imediato (OOM, disco cheio, mutation
  falhando), `warning` para degradação, `info` para observações/ajustes opcionais.
- Mensagens voltadas ao usuário (títulos/detalhes/sugestões) ficam em
  **português**; comentários de código em inglês, como já está.
- Não comitar `bin/` (já no `.gitignore`).

## Estado

- Suporta apenas ClickHouse. A divisão collector/analyzer existe para abrir
  espaço a outros bancos.
- Coleta é um snapshot instantâneo; merges/mutations refletem só o momento da
  execução.
