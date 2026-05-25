# database-fining

CLI em Go que se conecta a um ClickHouse, coleta hardware do host e métricas de
runtime (parts, merges, mutations, settings) e sugere ajustes de tuning.

## Como funciona

1. **Coleta** (`internal/collector`): conecta no protocolo nativo e lê
   `system.server_settings`, `system.settings`, `system.parts`, `system.merges`,
   `system.mutations` e `system.asynchronous_metrics`. O hardware do host (CPU,
   RAM, disco do `--data-path`) é lido localmente via gopsutil.
2. **Análise** (`internal/analyzer`): regras por subsistema geram *findings* com
   severidade (`info`/`warning`/`critical`), explicação e sugestão. Quando
   aplicável, cada finding carrega chaves de config prontas.
3. **Saída** (`internal/report`): `cli` (padrão, colorido), `json` (snapshot +
   findings) ou `config` (trechos de `config.xml`/`users.xml`).

## Build

```sh
go build -o bin/chfining ./cmd/chfining
```

## Uso

```sh
# relatório CLI
./bin/chfining -addr localhost:9000 -user default -password "$CH_PASSWORD"

# JSON para pipelines/dashboards
./bin/chfining -addr localhost:9000 -format json

# trechos de config prontos para aplicar
./bin/chfining -addr localhost:9000 -format config
```

Flags (todas têm equivalente via env): `-addr` (`CH_ADDR`), `-database`
(`CH_DATABASE`), `-user` (`CH_USER`), `-password` (`CH_PASSWORD`), `-data-path`
(`CH_DATA_PATH`), `-format`, `-no-color`, `-timeout`.

O processo sai com código `2` quando há ao menos um finding crítico (útil em CI).

## O que é analisado hoje

- **Hardware → pools/memória**: `background_pool_size` vs. núcleos,
  `max_server_memory_usage` vs. RAM, `mark_cache_size`, espaço em disco.
- **Parts**: too-many-parts por partição (limiares de `parts_to_delay_insert` /
  `parts_to_throw_insert`) e over-partitioning.
- **Merges**: merges lentos em andamento e saturação do background pool.
- **Mutations**: mutations falhando, pendentes há muito tempo ou acumuladas.

## Estrutura

```
cmd/chfining        entrypoint + flags
internal/model      tipos compartilhados (Snapshot, Finding, ...)
internal/collector  conexão ClickHouse + leitura de hardware
internal/analyzer    regras de tuning por subsistema
internal/report      renderização cli / json / config
```

## Roadmap

- Suporte a outros bancos (a arquitetura já isola coletor/analisador por isso).
- Coleta em janela (vários snapshots) para capturar picos de merge.
- Mais regras: ZooKeeper/Keeper, replicação, async inserts, compressão.
