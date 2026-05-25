# database-fining

CLI em Go que conecta a um ClickHouse, coleta o hardware do host e métricas de
runtime (parts, merges, mutations, settings) e **sugere** ajustes de tuning.

> **Somente leitura.** A ferramenta só executa `SELECT`/`ping` nas system
> tables. Ela nunca roda `INSERT`, `DELETE`, `ALTER`, `OPTIMIZE`, `KILL` nem
> qualquer comando de escrita — as recomendações são impressas para você aplicar
> manualmente.

## Instalação

Requer Go 1.26+.

```sh
git clone https://github.com/sipesistemas/database-fining.git
cd database-fining
go build -o bin/chfining ./cmd/chfining
```

O binário fica em `bin/chfining`.

## Uso rápido

```sh
./bin/chfining -addr localhost:9000 -user default -password "$CH_PASSWORD"
```

Isso imprime um relatório no terminal com os achados ordenados por severidade
(críticos primeiro). É preciso que o usuário tenha permissão de `SELECT` nas
tabelas `system.*`.

## Flags

| Flag         | Env            | Padrão                | Descrição                              |
|--------------|----------------|-----------------------|----------------------------------------|
| `-addr`      | `CH_ADDR`      | `localhost:9000`      | host:port (protocolo nativo, não HTTP) |
| `-database`  | `CH_DATABASE`  | `default`             | database para a conexão                |
| `-user`      | `CH_USER`      | `default`             | usuário                                |
| `-password`  | `CH_PASSWORD`  | *(vazio)*             | senha — prefira a env var              |
| `-data-path` | `CH_DATA_PATH` | `/var/lib/clickhouse` | caminho dos dados (stats de disco)     |
| `-format`    | —              | `cli`                 | `cli`, `json` ou `config`              |
| `-no-color`  | —              | `false`               | desliga cores no formato `cli`         |
| `-timeout`   | —              | `30s`                 | timeout total da coleta                |

> O `-data-path` deve apontar para onde o ClickHouse guarda os dados **na
> máquina onde você roda o `chfining`**, pois as estatísticas de disco são lidas
> do filesystem local. Rode a ferramenta no próprio host do ClickHouse para que
> CPU/RAM/disco reflitam o servidor real.

## Formatos de saída

### `cli` (padrão)

Relatório legível e colorido. Cada achado mostra severidade, categoria,
diagnóstico, sugestão e, quando aplicável, as chaves de config recomendadas.

```sh
./bin/chfining -addr localhost:9000
./bin/chfining -addr localhost:9000 -no-color   # sem ANSI (logs/arquivo)
```

### `json`

Snapshot completo + findings, para pipelines, dashboards ou histórico.

```sh
./bin/chfining -addr localhost:9000 -format json > snapshot.json
./bin/chfining -addr localhost:9000 -format json | jq '.findings[] | {severity, title}'
```

### `config`

Gera trechos de XML prontos, separados por escopo (`config.xml` para settings de
servidor e `users.xml` para o profile). **Revise antes de aplicar.**

```sh
./bin/chfining -addr localhost:9000 -format config
```

## Variáveis de ambiente

Útil para não expor a senha no histórico do shell:

```sh
export CH_ADDR=ch-prod.internal:9000
export CH_USER=monitor
export CH_PASSWORD='...'
./bin/chfining            # usa as envs acima
```

## Código de saída

| Código | Significado                                  |
|--------|----------------------------------------------|
| `0`    | Coleta ok, sem achados críticos              |
| `1`    | Erro de conexão ou coleta                    |
| `2`    | Pelo menos um achado **crítico** (útil em CI)|

Exemplo em CI:

```sh
./bin/chfining -addr "$CH_ADDR" -format json > report.json || \
  { [ $? -eq 2 ] && echo "Achados críticos de tuning!" && exit 1; }
```

## O que é analisado hoje

- **Hardware → pools/memória**: `background_pool_size` vs. núcleos,
  `max_server_memory_usage` vs. RAM (inclui alerta crítico se exceder a RAM
  física), `mark_cache_size`, espaço livre em disco.
- **Parts**: too-many-parts por partição (limiares de `parts_to_delay_insert` /
  `parts_to_throw_insert`) e over-partitioning.
- **Merges**: merges lentos em andamento e saturação do background pool.
- **Mutations**: mutations falhando, pendentes há muito tempo ou acumuladas.

## Testar localmente com Docker

```sh
docker run -d --rm --name ch -p 19000:9000 \
  -e CLICKHOUSE_PASSWORD=secret clickhouse/clickhouse-server:latest

CH_PASSWORD=secret ./bin/chfining -addr localhost:19000 -data-path "$(pwd)"

docker rm -f ch
```

## Desenvolvimento

```sh
go test ./...      # testes dos analisadores
go vet ./...
go build ./...
```

Estrutura:

```
cmd/chfining        entrypoint + flags
internal/model      tipos compartilhados (Snapshot, Finding, ...)
internal/collector  conexão ClickHouse (somente leitura) + hardware
internal/analyzer   regras de tuning por subsistema
internal/report     renderização cli / json / config
```

Veja `CLAUDE.md` para convenções de contribuição (como adicionar uma regra de
análise) e a regra inviolável de somente-leitura.

## Roadmap

- Suporte a outros bancos (a arquitetura já isola coletor/analisador por isso).
- Coleta em janela (vários snapshots) para capturar picos de merge.
- Mais regras: ZooKeeper/Keeper, replicação, async inserts, compressão.
