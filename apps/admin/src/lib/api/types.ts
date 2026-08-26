/**
 * Tipos do contrato — espelho de `apps/api/openapi/openapi.yaml`.
 *
 * Escritos à mão de propósito: o contrato é a fonte da verdade e um gerador
 * automático entraria no meio do caminho sem acrescentar nada enquanto a
 * superfície é esta. Quando o openapi crescer, isto vira `openapi-typescript`.
 */

export type Acao = "ver" | "criar" | "editar" | "excluir";

/** `own` vira `AND owner_id = <usuário>` no SQL do repositório — nunca filtro
 *  em memória, que faria a paginação mentir. Aqui do lado do painel o escopo
 *  serve só para rotular o que o usuário enxerga. */
export type Escopo = "all" | "own";

export type Permissao = {
  resource: string;
  action: Acao;
  scope: Escopo;
};

export type Usuario = {
  id: string;
  name: string;
  email: string;
  phone: string | null;
  role_id: string;
  /** `code` do perfil. **Não é enum fechado**: perfil é dado, criável em
   *  `/roles`. Nada de `switch (user.role)` — quem decide o que aparece é
   *  `permissions`. */
  role: string;
  role_name: string;
  broker_id: string | null;
  active: boolean;
  last_login_at: string | null;
  created_at: string;
};

/** Resposta de `GET /auth/me`: o usuário e a matriz do perfil já achatada. */
export type Sessao = {
  user: Usuario;
  permissions: Permissao[];
};

/** Resposta de `POST /auth/login` e `POST /auth/refresh` — mesmo shape. */
export type ParDeTokens = {
  access_token: string;
  expires_in: number;
  user: Usuario;
};

export type Perfil = {
  id: string;
  code: string;
  name: string;
  is_system: boolean;
};

export type PerfilComMatriz = Perfil & { permissions: Permissao[] };

/** Item de `GET /roles/resources` — o catálogo que a tela de perfis desenha. */
export type RecursoRBAC = {
  code: string;
  label: string;
  group: string;
  actions: Acao[];
  scopes: Escopo[];
};

export type Meta = {
  page: number;
  per_page: number;
  total: number;
  total_pages: number;
};

export type Lista<T> = { data: T[]; meta: Meta };
