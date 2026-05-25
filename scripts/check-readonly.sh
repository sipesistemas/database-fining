#!/usr/bin/env bash
# check-readonly.sh — garante a regra inviolável do projeto: a ferramenta só lê.
# Falha (exit 1) se encontrar SQL de escrita ou métodos de escrita do driver.
#
# Uso: ./scripts/check-readonly.sh
set -euo pipefail

cd "$(dirname "$0")/.."

GO="${GO:-go}"
if ! command -v "$GO" >/dev/null 2>&1 && [ -x /usr/local/go/bin/go ]; then
  GO=/usr/local/go/bin/go
fi

echo "==> Verificação AST (go test) ..."
"$GO" test ./internal/collector -run TestCollectorIsReadOnly -count=1

# Reforço por grep: métodos de escrita do driver em qualquer .go (exceto testes).
echo "==> Grep de reforço (métodos de escrita do driver) ..."
hits=$(grep -rnE '\.(Exec|ExecContext|PrepareBatch|AsyncInsert)\(' \
  --include='*.go' cmd internal | grep -vE '_test\.go:' || true)
if [ -n "$hits" ]; then
  echo "ERRO: método de escrita do driver encontrado:"
  echo "$hits"
  exit 1
fi

echo "OK: nenhum comando de escrita no código (somente leitura)."
