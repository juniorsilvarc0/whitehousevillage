"use server";

import { revalidatePath } from "next/cache";
import { z } from "zod";

import { apiFetch } from "@/lib/api/client";
import { tentar } from "@/lib/acoes/executar";
import { exigir } from "@/lib/acoes/guarda";
import { falha, type Resultado } from "@/lib/acoes/resultado";
import type { CampoDoSite } from "@/lib/site/tipos";

const CAMINHO = "/app/site";

/**
 * Salvar = publicar (docs/site-cms.md §1). Não há rascunho: o `PUT` grava e o
 * site mostra a mudança na próxima leitura.
 *
 * O zod aqui confere só a forma — chave do catálogo, texto ou objeto de mídia.
 * Tamanho máximo, tipo do campo e existência da mídia são da API, que conhece o
 * catálogo; repetir as regras aqui seria ter duas verdades.
 */
const Chave = z.string().regex(/^[a-z0-9][a-z0-9.\-_]{0,99}$/);

const Midia = z.object({ media_id: z.uuid(), alt: z.string().max(500).optional() }).strict();

const Item = z.record(z.string(), z.union([z.string().max(2000), Midia, z.null()]));

const Valor = z.union([z.string().max(2000), Midia, z.array(Item).max(100)]);

export async function salvarCampoDoSite(chave: string, valor: unknown): Promise<Resultado<CampoDoSite>> {
  const recusa = await exigir("site", "editar");
  if (recusa) return recusa;

  const k = Chave.safeParse(chave);
  const v = Valor.safeParse(valor);
  if (!k.success || !v.success) return falha("VALIDATION_ERROR", "Valor fora do formato.", {});

  const resultado = await tentar(() =>
    apiFetch<CampoDoSite>(`/site/content/${encodeURIComponent(k.data)}`, { method: "PUT", body: { value: v.data } }),
  );
  if (resultado.ok) revalidatePath(CAMINHO);
  return resultado;
}

export async function restaurarCampoDoSite(chave: string): Promise<Resultado<null>> {
  const recusa = await exigir("site", "editar");
  if (recusa) return recusa;

  const k = Chave.safeParse(chave);
  if (!k.success) return falha("VALIDATION_ERROR", "Chave fora do formato.", {});

  const resultado = await tentar(async () => {
    await apiFetch<void>(`/site/content/${encodeURIComponent(k.data)}`, { method: "DELETE" });
    return null;
  });
  if (resultado.ok) revalidatePath(CAMINHO);
  return resultado;
}
