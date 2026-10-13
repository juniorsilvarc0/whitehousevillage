/**
 * Vocabulário de recursos do RBAC — espelho do catálogo do banco.
 *
 * **Fonte da verdade:** a tabela `resources`, populada por
 * `apps/api/cmd/seed/acesso.go` (`catalogoSeed`) e servida por
 * `GET /roles/resources`. É esse vocabulário que a spec §7 descreve e é ele que
 * a tela de perfis edita — o painel não tem um segundo dicionário.
 *
 * Este arquivo existe por um motivo só: transformar erro de digitação em erro de
 * compilação. Recurso inexistente é pior que recurso proibido — ninguém tem, e
 * ninguém *pode* ter, permissão num código que não está no catálogo, então a
 * tela some silenciosamente para todo mundo, inclusive o admin, sem 403 nenhum
 * para denunciar. Com `RecursoCodigo`, `"finance"` nem chega a buildar.
 *
 * Espelho não é sincronia automática: `permissions.test.ts` lê o `acesso.go` e
 * falha se as duas listas divergirem. Recurso novo entra no seed primeiro, aqui
 * depois.
 *
 * Os dois códigos citados pelo Go em `internal/auth/recursos.go`
 * (`users`, `roles`) também moram no catálogo — as constantes de lá são atalho
 * de digitação, não um catálogo paralelo.
 */
export const RECURSOS_DO_CATALOGO = [
  "dashboard",
  "reports",

  "reservations",
  "calendar",
  "agenda",
  "inventory",
  "inventory.goods",
  // Ordens de manutenção (spec §12). Recurso próprio, e não uma ação a mais em
  // `inventory.goods`: a ordem pode tirar a unidade da venda (bloqueio em
  // `stay_blocks`), e quem conta taça não precisa poder fazer isso.
  "maintenance",
  "channels",

  "quotes",
  "contacts",
  "brokers",

  "crm.pipelines",
  "crm.leads",
  "crm.opportunities",
  "crm.activities",

  "chat",

  "finance.receivables",
  "finance.payables",
  "finance.commissions",

  "users",
  "roles",
  "settings",
  "integrations",
  "audit",

  "site",
] as const;

/** Todo código de recurso citado pelo painel tem que ser um destes. */
export type RecursoCodigo = (typeof RECURSOS_DO_CATALOGO)[number];
