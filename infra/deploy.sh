#!/usr/bin/env bash
# Deploy na VPS. Rode NA VPS, da raiz do repositório clonado:
#   bash infra/deploy.sh                    # padrão: atrás do proxy que JÁ roda na VPS
#   bash infra/deploy.sh --proxy-proprio    # só numa VPS sem nada em 80/443
#
# A VPS já roda outros serviços, e este script não pode afetar nenhum deles.
# Por isso ele RECUSA seguir quando:
#   - uma porta que ele vai ocupar já está em uso por algo que não é deste stack;
#   - --proxy-proprio foi pedido e 80 ou 443 já estão ocupadas.
# E nunca usa `down`, `prune`, `--remove-orphans` nem toca em container fora do
# projeto `whv-gestao`.
# Volumes (`pgdata`, `midia`) atravessam todo redeploy: `up -d` recria
# containers, nunca volumes. `down -v` apagaria o banco e as fotos/vídeos do
# site, que não têm outra cópia além do backup — por isso não existe aqui.
#
# Ordem: build → postgres saudável → migrate → seed → resto → fumaça local.
set -euo pipefail
cd "$(dirname "$0")/.."

ENV_FILE=infra/.env.production
PROJETO=whv-gestao
PROXY_PROPRIO=0
[[ ${1:-} == --proxy-proprio ]] && PROXY_PROPRIO=1

[[ -f $ENV_FILE ]] || { echo "falta $ENV_FILE — copie de infra/.env.production.example e preencha." >&2; exit 1; }
if grep -q 'TROQUE' "$ENV_FILE"; then
  echo "$ENV_FILE ainda tem valor TROQUE: gere os segredos (openssl rand -base64 48)." >&2; exit 1
fi
set -a
# shellcheck disable=SC1090  # caminho em variável, de propósito
source "$ENV_FILE"
set +a
ADMIN_PORT=${WHV_ADMIN_PORT:-3110}
SITE_PORT=${WHV_SITE_PORT:-3210}

dc() {
  local perfis=()
  [[ $PROXY_PROPRIO == 1 ]] && perfis=(--profile proxy-proprio)
  docker compose -f infra/docker-compose.prod.yml --env-file "$ENV_FILE" "${perfis[@]}" "$@"
}

# Porta ocupada por QUEM? Se for container deste projeto (redeploy), tudo bem.
porta_livre_ou_nossa() {
  local porta=$1
  ss -Hltn "sport = :$porta" | grep -q . || return 0
  docker ps --filter "label=com.docker.compose.project=$PROJETO" --format '{{.Ports}}' \
    | grep -q ":$porta->" && return 0
  return 1
}

echo "==> conferindo que nada da VPS é afetado"
for p in "$ADMIN_PORT" "$SITE_PORT"; do
  porta_livre_ou_nossa "$p" || { echo "porta $p já está em uso por outro serviço da VPS. Escolha outra em $ENV_FILE." >&2; exit 1; }
done
if [[ $PROXY_PROPRIO == 1 ]]; then
  for p in 80 443; do
    porta_livre_ou_nossa "$p" || { echo "porta $p já é de outro serviço: não use --proxy-proprio; configure o proxy existente (ver docs/infra.md)." >&2; exit 1; }
  done
fi

echo "==> build";     dc build
echo "==> postgres";  dc up -d --wait postgres
echo "==> migrate";   dc run --rm migrate
echo "==> seed";      dc run --rm seed
servicos=(api worker admin site)
[[ $PROXY_PROPRIO == 1 ]] && servicos+=(traefik)
echo "==> stack";     dc up -d --wait "${servicos[@]}"

# Fotos e vídeos do site moram no volume `whv-gestao_midia`, montado na API. Se
# o mount sumir (compose editado errado), a API sobe, aceita upload e grava na
# camada descartável do container — que o próximo deploy apaga. Melhor parar aqui.
echo "==> volume de mídia"
api_id=$(dc ps -q api)
if ! docker inspect -f '{{range .Mounts}}{{.Name}}={{.Destination}} {{end}}' "$api_id" | grep -q "${PROJETO}_midia=/data/midia"; then
  echo "a API subiu SEM o volume ${PROJETO}_midia em /data/midia — uploads se perderiam no próximo deploy." >&2; exit 1
fi
echo "   ${PROJETO}_midia em /data/midia"

echo "==> fumaça local (sem depender do proxy)"
falhou=0
checar() {
  local status
  status=$(curl -s -o /dev/null -w '%{http_code}' --max-time 10 -H "Host: $2" "http://127.0.0.1:$1$3" || true)
  echo "   $status  $2$3 (127.0.0.1:$1)"
  [[ $status == "$4" ]] || falhou=1
}
checar "$SITE_PORT"  www.whitehousevillage.com.br      /        200
checar "$SITE_PORT"  www.whitehousevillage.com.br      /admin/  404
checar "$ADMIN_PORT" gestor.whitehousevillage.com.br   /login   200
checar "$ADMIN_PORT" corretor.whitehousevillage.com.br /login   200
# A vitrine pelo site: o preço do site sai daqui. E SÓ ela passa pelo site.
checar "$SITE_PORT"  www.whitehousevillage.com.br      /api/v1/public/products 200
checar "$SITE_PORT"  www.whitehousevillage.com.br      /api/v1/auth/me         404
if [[ $falhou == 1 ]]; then
  echo "fumaça REPROVOU — docker compose -p $PROJETO logs --tail=100" >&2; exit 1
fi
echo "deploy ok. Falta só o proxy da VPS apontar os três nomes para 127.0.0.1:$SITE_PORT (www) e 127.0.0.1:$ADMIN_PORT (gestor, corretor) — docs/infra.md."
