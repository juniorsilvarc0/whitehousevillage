# Infraestrutura

> **Leia antes: parte deste documento é ALVO, não estado.** Conferido em 02/10/2026.
> **Existe**: `infra/docker-compose.prod.yml` e `infra/deploy.sh` (§0 abaixo) —
> escritos e validados com `docker compose config`, mas **ainda não executados
> numa VPS**. **Não existem**: serviço de `backup` automático, restore verificado,
> `/metrics`, push de imagem para registry. O backup que existe é `make backup`,
> manual; `make restore` reprova de propósito. O worker **não** usa River: é um
> loop próprio com um job (`holds.expire`). As seções 2, 3, 5, 6 e 7 são plano.

## 0. Deploy na VPS (82.29.59.229) sem afetar o que já roda nela

A VPS já atende outros serviços. O stack de produção foi desenhado para **não
encostar em nenhum deles**:

| Garantia | Como |
|---|---|
| Nada fora do projeto é tocado | Nome de projeto `whv-gestao`: containers, redes, volumes e imagens com prefixo próprio. O `deploy.sh` nunca roda `down`, `prune` nem `--remove-orphans` |
| Nenhuma porta pública | Painel e site escutam só em `127.0.0.1` (`WHV_ADMIN_PORT`, padrão 3110; `WHV_SITE_PORT`, padrão 3210). Postgres e API não publicam porta nenhuma |
| Porta ocupada não é tomada | O `deploy.sh` confere com `ss` e **para** se a porta for de outro serviço |
| 80/443 continuam do proxy atual | O Traefik do compose só sobe com `--proxy-proprio`, e o script recusa essa opção se 80 ou 443 estiverem ocupadas |

DNS (já configurado): `www`, `gestor` e `corretor` `.whitehousevillage.com.br` → 82.29.59.229.
`gestor` e `corretor` são o **mesmo painel**: o que cada perfil vê sai da matriz de
permissões, não do endereço.

**Primeiro deploy**, na VPS:

```bash
git clone https://github.com/juniorsilvarc0/whitehousevillage.git /opt/whv-gestao && cd /opt/whv-gestao
cp infra/.env.production.example infra/.env.production && chmod 600 infra/.env.production
# preencha: senhas (openssl rand -base64 48), JWT_SECRET, portas livres.
# Para a apresentação, SEED_DEV_USERS=true cria admin/gestao/corretor @wh.local
# com a senha de desenvolvimento — troque as senhas logo depois e volte para false.
bash infra/deploy.sh
```

**O que já roda na VPS (medido em 02/10/2026 com `docker ps`)**: crmsup, escalakids,
rdguara, spinchat e supabase, com estas portas no loopback: 3001, 3010, 3020, 3201,
3203, 5433, 8001, 8080, 8090, 9000, 9001, 9010, 9011. Nenhum container publica 80/443,
então o HTTPS é de um proxy **no host** (nginx, presumivelmente). Daí os padrões
3110 e 3210 — e a `3201`, a primeira escolha, era justamente a do `crmsup-gateway`.
Nomes de container deste stack começam com `whv-gestao-`, que não colide com nenhum.

O script termina com uma fumaça em `127.0.0.1` (site 200, `/admin/` 404, login do
painel 200 nos dois nomes). Falta então **o proxy que já atende 80/443** encaminhar
os três nomes. Exemplos para os proxies mais comuns:

No nginx do host, **um arquivo novo, só nosso** — nada dos sites existentes é editado:

```bash
sudo nano /etc/nginx/sites-available/whv-gestao      # conteúdo abaixo
sudo ln -s /etc/nginx/sites-available/whv-gestao /etc/nginx/sites-enabled/
sudo nginx -t && sudo systemctl reload nginx         # reload, não restart: conexões vivas seguem
sudo certbot --nginx -d www.whitehousevillage.com.br -d gestor.whitehousevillage.com.br -d corretor.whitehousevillage.com.br
```

`nginx -t` reprova antes de qualquer coisa mudar se o arquivo tiver erro, e o
`certbot --nginx -d ...` só edita os blocos desses três nomes.

```nginx
# /etc/nginx/sites-available/whv-gestao
server { listen 80; server_name www.whitehousevillage.com.br;
  location / { proxy_pass http://127.0.0.1:3210; proxy_set_header Host $host;
               proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
               proxy_set_header X-Forwarded-Proto $scheme; } }
server { listen 80; server_name gestor.whitehousevillage.com.br corretor.whitehousevillage.com.br;
  location / { proxy_pass http://127.0.0.1:3110; proxy_set_header Host $host;
               proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
               proxy_set_header X-Forwarded-Proto $scheme;
               # o mapa usa SSE: sem buffer e com leitura longa
               proxy_buffering off; proxy_read_timeout 1h; } }
```

```caddy
# Caddy (emite o certificado sozinho)
www.whitehousevillage.com.br { reverse_proxy 127.0.0.1:3210 }
gestor.whitehousevillage.com.br, corretor.whitehousevillage.com.br { reverse_proxy 127.0.0.1:3110 }
```

Se o proxy da VPS for um Traefik em container, ele não alcança `127.0.0.1` do
host: use um roteador de arquivo apontando para o IP da bridge do Docker
(`172.17.0.1:3110` / `:3210`), ou mude as portas para escutar nessa bridge.

**Redeploy**: `git pull && bash infra/deploy.sh` — idempotente; migrations e seed
só aplicam o que falta.

## 1. Ambientes

| Ambiente | Onde | Domínio | Dados |
|---|---|---|---|
| **dev** | Máquina do desenvolvedor, Docker Compose | `localhost:3000` / `:8080` | Seed |
| **staging** *(opcional)* | VPS, mesmo compose com outro `.env` | `staging.dominio` | Cópia anonimizada |
| **prod** | VPS com Traefik | `app.dominio` (admin) e `api.dominio` | Real, com backup |

Configuração **100% por variável de ambiente** (`.env.example` é a fonte da verdade). Nenhum segredo no repositório.

## 2. Serviços

```
                         ┌─────────── Traefik ───────────┐
   Internet ── 443 ──►   │ SSL Let's Encrypt (HTTP-01)   │
                         │ app.dominio → admin:3000      │
                         │ api.dominio → api:8080        │
                         └───────────────┬───────────────┘
                                         │
             ┌───────────────┬───────────┴────────────┬──────────────┐
             ▼               ▼                        ▼              ▼
        admin (Next)     api (Go)                worker (Go)     postgres:16
        standalone       chi + pgx           loop próprio (*)    volume nomeado
                              │                       │              ▲
                              └───── LISTEN/NOTIFY ───┴──────────────┘
                                                              backup (cron pg_dump)
```

| Serviço | Imagem | Notas |
|---|---|---|
| `traefik` | `traefik:v3` | Entrypoints 80/443, redirect para HTTPS, `acme.json` em volume com `chmod 600` |
| `api` | multi-stage Go → `distroless/static` | Binário estático, usuário não-root, `/healthz` como healthcheck |
| `worker` | mesma imagem, entrypoint `worker` | (*) Loop próprio com um job, `holds.expire` (a expiração da pré-reserva), sem River. Escala independente da API. No dev, sobe no `make up` desde 02/10/2026 |
| `admin` | `node:22-alpine` → `next start` (output standalone) | Só o BFF fala com a API |
| `postgres` | `postgres:16-alpine` | Volume nomeado, `TZ=America/Fortaleza`, healthcheck `pg_isready` |
| `migrate` | mesma imagem da API, entrypoint `migrate` | Roda sob demanda (`make migrate`), **nunca** no boot da API |
| `seed` | idem, entrypoint `seed` | Idempotente |
| `backup` | `postgres:16-alpine` + cron | `pg_dump` diário comprimido e cifrado |

`depends_on` com `condition: service_healthy` — a API não sobe antes do banco responder.

## 3. Deploy

```bash
# primeira vez na VPS
git clone git@github.com:juniorsilvarc0/whitehousevillage.git /opt/whv
cd /opt/whv && cp .env.example .env && vim .env      # domínio, e-mail ACME, segredos
mkdir -p infra/traefik && touch infra/traefik/acme.json && chmod 600 infra/traefik/acme.json
make up && make migrate && make seed

# atualização
git pull && make build && make migrate && docker compose up -d
```

Rollback: as imagens são versionadas por tag de commit; `docker compose up -d` com a tag anterior volta a aplicação. **Migration com `down` testada** é o que permite rollback de schema — por isso toda `up` tem `down`.

## 4. CI/CD (GitHub Actions)

O que existe hoje em `.github/workflows/ci.yml` — a tabela é o arquivo, não a intenção:

| Job | O que roda |
|---|---|
| `api` | `gofmt -l` (falha se houver arquivo fora de forma), `go vet ./...` e `go vet -tags=integration ./...`, `go test ./... -race -count=1` — o teste de contrato (toda rota na OpenAPI, seis verbos, permissão) roda aqui |
| `lint-go` | `golangci-lint` na versão de `Makefile:GOLANGCI_LINT_VERSION` (v2.5.0, lida com `make -s golangci-versao`), instalado pela action e rodado por `make lint-golangci`, sem teto de repetição. Ainda **sem** a tag `integration` (6 apontamentos em `internal/router` de teste). Entrou em 02/10/2026 e não rodou num runner até o commit da Rodada 5 |
| `migrations` | Postgres de serviço: `migrate up` → `version` → `down -all` → `up` → `version`, com o **nosso** `cmd/migrate` |
| `integration` | Postgres de serviço: **schema → seed → suíte `-tags=integration` → concorrência repetida**. Detalhado abaixo |
| `admin` | `pnpm install --frozen-lockfile`, `pnpm lint` (eslint), `tsc --noEmit`, `pnpm test --run`, `pnpm build` |
| `smoke` | `make smoke-stack`: sobe o stack com imagem reconstruída, migra, semeia, roda a fumaça do painel e a do site e confere o worker (`worker-vivo`) |
| `site` | Constrói a imagem do site, sobe um contêiner dela **sem** o volume de desenvolvimento e roda a fumaça do site (`make smoke-site-imagem`) |
| `build-images` | Só em push para `main`: builda as imagens da API, do painel e do site (sem push para registry enquanto não houver VPS); `needs` inclui `lint-go` e `site` |

Pendências conhecidas do CI, para não parecerem entregues: o `e2e` de jornada com escrita (Playwright) não existe; o lint não enxerga os arquivos `//go:build integration`; não há push de imagem nem deploy.

Regras-alvo: `main` protegida, PR obrigatório, CI verde para merge, sem push direto. **Ainda não são o estado**: quatro commits de 31/08 e 02/10 entraram por push direto (ver `roadmap.md`, Fase 2).

### 4.1 Job de integração — por que tem seed e por que repete

O job roda os **alvos do Makefile**, não comandos soltos, para que `make test-integration` na máquina do desenvolvedor e o CI executem exatamente a mesma coisa. Os alvos `it-schema`, `it-seed`, `it-suite` e `it-concorrencia` assumem `DATABASE_URL` apontando para um banco descartável; `make test-integration` sobe o Postgres efêmero (`whv-it-postgres`, porta `55432`) e chama os quatro na ordem.

| Etapa | Comando | Por que existe |
|---|---|---|
| Schema | `make it-schema` | Aplica as migrations. Passo separado, como em produção |
| **Seed** | `make it-seed` | Sem ele a tabela `resources` fica vazia: todo teste que concede permissão bate na FK `role_permissions_resource_code_fkey` (**23503**), e `TestPerfilCorretorSemeadoSalvaSemAlteracao` se **pula** — teste pulado conta como verde, que é cobertura perdida disfarçada de sucesso |
| Suíte | `make it-suite` | `go test -tags=integration ./... -race -count=1` |
| **Concorrência** | `make it-concorrencia` | Repete os testes de disputa `-count=10`. O defeito de datas sob contenção é probabilístico: medido em Postgres real, `-count=1` passou **verde** com o defeito presente e `-count=10` reprovou — foi assim que ele atravessou duas revisões |

Quais testes são "de concorrência" é decidido por **nome**, no regex `TESTES_CONCORRENCIA` do Makefile (`Overbooking|Concorren|Simultane|Corrida|Disputa`), porque Go não tem categoria de teste. Duas consequências práticas:

- quem escrever uma disputa nova **batiza com uma dessas palavras**, senão o teste fica fora da repetição;
- se o regex não casar com nenhum teste, o alvo **falha de propósito** — renomear um teste vira erro visível, nunca uma etapa vazia e verde.

`REPETICOES_CONCORRENCIA` (10) e `TESTES_CONCORRENCIA` são variáveis do Makefile e podem ser sobrescritas na linha de comando (`make test-integration REPETICOES_CONCORRENCIA=30`) para caçar uma intermitência mais rara. Com ~1/3 de chance de detecção por execução, 10 repetições deixam ~2% de chance de a falha atravessar o CI, contra 67% de uma execução só. Custo medido: ~75 s para os 6 testes de concorrência × 10; o job tem `timeout-minutes: 25` para que um impasse não queime hora de runner até o teto de 6 h da plataforma.

> `hashFiles()` **não** é permitido em `if:` de job — já derrubou o CI aqui. Condição de job usa só `github.*` e expressões de contexto estático.

## 5. Backup e restore

- `pg_dump` diário comprimido, cifrado (`age`/`gpg`), retenção 30 dias local + cópia remota opcional (`rclone`).
- **Restore é testado automaticamente**: job diário restaura o dump em base descartável e roda um `SELECT count(*)` sanity — backup que nunca foi restaurado não é backup.
- Alvos: **RPO < 24 h**, **RTO < 4 h**. Procedimento de restore documentado no runbook abaixo.

```bash
# restore
gunzip -c backup-20260820.sql.gz | docker compose exec -T postgres psql -U $POSTGRES_USER -d $POSTGRES_DB
```

## 6. Observabilidade

- **Logs** JSON estruturados (`slog`) com `request_id`, `user_id`, rota, status e latência. `docker compose logs` em dev; rotação por driver em prod.
- **Health**: `/healthz` (processo vivo) e `/readyz` (pool do banco + **versão da migration esperada**). A API se recusa a servir se o schema estiver defasado.
- **Métricas** Prometheus em `/metrics`: latência por rota, erro por código, fila de jobs, jobs falhos, conexões — e métricas de negócio (`whv_holds_expired_total`, `whv_ical_conflicts_total`, `whv_reservations_confirmed_total`).
- **Alertas** mínimos: API fora do ar, fila de jobs crescendo, job falhando repetidamente, conflito de canal detectado, backup do dia ausente.

## 7. Segurança

- TLS obrigatório; HSTS; cookies `httpOnly`, `Secure`, `SameSite=Lax`.
- Senha com argon2id. Refresh rotativo com detecção de reuso.
- Rate limit por IP no login e por token na API pública. **Hoje o contador vive na memória do processo** (`httpx.Limitador`): enquanto for assim, roda-se **uma** instância da API — com duas réplicas o limite multiplica e todo deploy zera a janela. Redis entra na Fase 6 (dívida **D1** em `docs/roadmap.md`).
- Postgres **não** exposto para fora do compose. Segredos por env, nunca no git.
- Imagens sem shell (`distroless`) e usuário não-root.
- `acme.json` com permissão 600 — Traefik recusa subir de outro jeito.
- Backups cifrados (contêm dado pessoal de hóspede).

## 8. Runbook (o mínimo para operar)

| Situação | O que fazer |
|---|---|
| API não sobe | `docker compose logs api` → geralmente migration pendente: `make migrate` |
| `/readyz` vermelho | Checar Postgres (`make psql`) e a versão da migration |
| Fila de jobs crescendo | `docker compose logs worker`; job travado libera com o advisory lock ao reiniciar o worker |
| WhatsApp mudo | `/chat/connection/state`; se desconectado, reconectar por QR e confirmar o webhook registrado |
| Conflito de canal | Painel `/canais/conflitos` → realocar unidade ou cancelar com compensação |
| Overbooking reportado | **Não deveria existir por reserva direta** (constraint). Se vier de OTA, é conflito de janela de risco — tratar pelo painel |
| Restaurar backup | Ver §5; sempre em base nova, nunca por cima da produção |
