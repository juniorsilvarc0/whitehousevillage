import { cookies } from "next/headers";

import { SESSION_COOKIE_NAME } from "@/lib/auth/cookies";
import type { Lista, Meta } from "@/lib/api/types";

/**
 * Cliente da API Go — **exclusivamente servidor**.
 *
 * O navegador nunca fala com a API: o JWT vive em cookie `httpOnly`, então quem
 * o injeta é o RSC ou a Route Handler. Um `apiFetch` que vazasse para o bundle
 * do cliente não teria como ler o cookie e, pior, deixaria a URL interna da API
 * exposta.
 */

/** Códigos do contrato (`components/responses/Erro`), na ordem do openapi. */
export type CodigoDeErro =
  | "VALIDATION_ERROR"
  | "UNAUTHORIZED"
  | "FORBIDDEN"
  | "NOT_FOUND"
  | "DATE_CONFLICT"
  | "MIN_STAY_NOT_MET"
  | "CAPACITY_EXCEEDED"
  | "DISCOUNT_ABOVE_LIMIT"
  | "HOLD_EXPIRED"
  | "IDEMPOTENCY_MISMATCH"
  | "RATE_LIMITED"
  | "INVALID_CREDENTIALS"
  | "TOKEN_INVALID"
  | "TOKEN_REUSED"
  | "EMAIL_IN_USE"
  | "ROLE_IMMUTABLE"
  | "ROLE_IN_USE"
  | "INTERNAL"
  // Fora do contrato: a API não respondeu (caiu, DNS, timeout). É do BFF, não
  // do servidor, e por isso não pode se disfarçar de INTERNAL — a tela precisa
  // saber que o problema é de conexão para oferecer "tentar de novo".
  | "NETWORK_ERROR";

export class ApiError extends Error {
  readonly code: CodigoDeErro;
  readonly status: number;
  readonly details: Record<string, unknown>;

  constructor(code: CodigoDeErro, message: string, status: number, details: Record<string, unknown> = {}) {
    super(message);
    this.name = "ApiError";
    this.code = code;
    this.status = status;
    this.details = details;
  }

  /** Erro por campo, como o contrato entrega em `details` — a tela repassa
   *  direto para o `setError` do react-hook-form. */
  campo(nome: string): string | undefined {
    const valor = this.details[nome];
    return typeof valor === "string" ? valor : undefined;
  }
}

export function isApiError(erro: unknown): erro is ApiError {
  return erro instanceof ApiError;
}

/**
 * A base pode vir com ou sem `/api/v1`. O `next.config.ts` define só o host, o
 * openapi declara o servidor com o prefixo — normalizar aqui evita a classe de
 * bug em que metade das chamadas cai em 404 dependendo de quem exportou a env.
 */
export function apiBaseUrl(): string {
  const bruto = (process.env.API_INTERNAL_URL ?? "http://localhost:8080").replace(/\/+$/, "");
  return bruto.endsWith("/api/v1") ? bruto : `${bruto}/api/v1`;
}

export type Query = Record<string, string | number | boolean | null | undefined>;

export type ApiFetchInit = {
  method?: "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
  /** Serializado como JSON. `undefined` não manda corpo. */
  body?: unknown;
  /** Vira querystring; chave com `null`/`undefined` é omitida. */
  query?: Query;
  headers?: Record<string, string>;
  /** Tags do cache do Next — `revalidateTag()` invalida depois da mutação. */
  tags?: string[];
  revalidate?: number | false;
  /** Não injeta `Authorization` (login, refresh, forgot/reset). */
  anonymous?: boolean;
  /** Token explícito, para quando ainda não existe cookie (fluxo de login). */
  accessToken?: string;
  signal?: AbortSignal;
  /** Ganho de acesso à resposta crua antes do parse. Existe por um motivo só:
   *  as rotas de auth precisam ler o `Set-Cookie` que a API Go devolve com o
   *  refresh rotacionado — e ele não aparece no corpo. */
  onResponse?: (resposta: Response) => void;
};

type Envelope<T> = { data?: T; meta?: Meta; error?: { code?: string; message?: string; details?: Record<string, unknown> } };

function montarUrl(path: string, query?: Query): string {
  const url = new URL(`${apiBaseUrl()}${path.startsWith("/") ? path : `/${path}`}`);
  for (const [chave, valor] of Object.entries(query ?? {})) {
    if (valor === null || valor === undefined || valor === "") continue;
    url.searchParams.set(chave, String(valor));
  }
  return url.toString();
}

async function tokenDoCookie(): Promise<string | undefined> {
  // `cookies()` só existe em contexto de requisição. Numa geração estática ele
  // lança — e é o comportamento certo: melhor a página virar dinâmica do que
  // servir HTML com dados de outra pessoa.
  const jar = await cookies();
  return jar.get(SESSION_COOKIE_NAME)?.value;
}

/**
 * Faz a chamada e devolve o miolo do envelope (`data`). Erro do contrato vira
 * `ApiError` com o `code` preservado — a tela reage ao código, nunca à mensagem.
 */
export async function apiFetch<T>(path: string, init: ApiFetchInit = {}): Promise<T> {
  const { data } = await apiRequest<T>(path, init);
  return data;
}

/** Igual ao `apiFetch`, mas devolve `meta` junto — para lista paginada. */
export async function apiList<T>(path: string, init: ApiFetchInit = {}): Promise<Lista<T>> {
  const { data, meta } = await apiRequest<T[]>(path, init);
  return {
    data: data ?? [],
    meta: meta ?? { page: 1, per_page: (data ?? []).length, total: (data ?? []).length, total_pages: 1 },
  };
}

async function apiRequest<T>(path: string, init: ApiFetchInit): Promise<{ data: T; meta?: Meta }> {
  if (typeof window !== "undefined") {
    throw new Error("apiFetch é servidor-only: o token vive em cookie httpOnly e não existe no navegador.");
  }

  const headers: Record<string, string> = {
    Accept: "application/json",
    ...init.headers,
  };

  if (init.body !== undefined) headers["Content-Type"] = "application/json";

  if (!init.anonymous) {
    const token = init.accessToken ?? (await tokenDoCookie());
    // Sem token não adianta bater na API para colher um 401: o `getSession()`
    // trata a ausência como "não logado" e o middleware manda para o /login.
    if (!token) throw new ApiError("UNAUTHORIZED", "Sessão ausente ou expirada.", 401);
    headers.Authorization = `Bearer ${token}`;
  }

  const temTags = Boolean(init.tags?.length) || init.revalidate !== undefined;

  let resposta: Response;
  try {
    resposta = await fetch(montarUrl(path, init.query), {
      method: init.method ?? "GET",
      headers,
      body: init.body === undefined ? undefined : JSON.stringify(init.body),
      signal: init.signal,
      // Dado de gestão é vivo: o padrão é não guardar. Só entra em cache quem
      // pedir tag explicitamente, e aí a invalidação é responsabilidade da
      // mutação que mexeu no recurso.
      ...(temTags
        ? { next: { tags: init.tags ?? [], ...(init.revalidate === undefined ? {} : { revalidate: init.revalidate }) } }
        : { cache: "no-store" as const }),
    });
  } catch (causa) {
    throw new ApiError("NETWORK_ERROR", "Não foi possível falar com o servidor.", 0, {
      cause: causa instanceof Error ? causa.message : String(causa),
    });
  }

  init.onResponse?.(resposta);

  if (resposta.status === 204) return { data: undefined as T };

  const texto = await resposta.text();
  let envelope: Envelope<T> = {};
  if (texto) {
    try {
      envelope = JSON.parse(texto) as Envelope<T>;
    } catch {
      // Corpo que não é JSON só acontece quando algo no caminho respondeu no
      // lugar da API (proxy, gateway). Tratar como erro de infraestrutura.
      if (resposta.ok) throw new ApiError("INTERNAL", "Resposta ilegível da API.", resposta.status);
    }
  }

  if (!resposta.ok) {
    const erro = envelope.error ?? {};
    throw new ApiError(
      normalizarCodigo(erro.code, resposta.status),
      erro.message ?? "Não foi possível concluir a operação.",
      resposta.status,
      erro.details ?? {},
    );
  }

  return { data: envelope.data as T, meta: envelope.meta };
}

/** Código desconhecido não pode virar `undefined` no meio da tela: mapeia pelo
 *  status, que é o que sobra de estável quando o corpo foge do contrato. */
function normalizarCodigo(code: string | undefined, status: number): CodigoDeErro {
  const conhecidos: readonly string[] = [
    "VALIDATION_ERROR", "UNAUTHORIZED", "FORBIDDEN", "NOT_FOUND", "DATE_CONFLICT",
    "MIN_STAY_NOT_MET", "CAPACITY_EXCEEDED", "DISCOUNT_ABOVE_LIMIT", "HOLD_EXPIRED",
    "IDEMPOTENCY_MISMATCH", "RATE_LIMITED", "INVALID_CREDENTIALS", "TOKEN_INVALID",
    "TOKEN_REUSED", "EMAIL_IN_USE", "ROLE_IMMUTABLE", "ROLE_IN_USE", "INTERNAL",
  ];
  if (code && conhecidos.includes(code)) return code as CodigoDeErro;
  if (status === 401) return "UNAUTHORIZED";
  if (status === 403) return "FORBIDDEN";
  if (status === 404) return "NOT_FOUND";
  if (status === 422) return "VALIDATION_ERROR";
  if (status === 429) return "RATE_LIMITED";
  return "INTERNAL";
}
