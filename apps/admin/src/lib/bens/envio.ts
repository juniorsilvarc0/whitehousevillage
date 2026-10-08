import { normalizarCodigo } from "@/lib/api/codigos";
import type { Resultado } from "@/lib/acoes/resultado";
import { mensagemDeBens } from "@/lib/bens/mensagens";
import type { MidiaDeBem } from "@/lib/bens/tipos";

/**
 * Envio da foto de um bem pelo navegador, para `POST /api/inventario/midia` (a
 * rota do painel que repassa a `POST /inventory/media` em fluxo).
 *
 * `XMLHttpRequest` e não `fetch`: só ele avisa o progresso do **envio**, e 15 MB
 * subindo pelo sinal de dentro de um apartamento sem barra andando parecem
 * travados. Mesmo desenho de `lib/site/envio.ts`; ficou separado porque o
 * destino, a permissão e o tipo da resposta são outros.
 *
 * Nunca lança. Erro vem com `mensagem` pronta para a tela.
 */
export type ResultadoDoEnvio = Resultado<MidiaDeBem> & { mensagem?: string };

export function enviarFoto(
  arquivo: File,
  aoProgredir?: (fracao: number) => void,
  sinal?: AbortSignal,
): Promise<ResultadoDoEnvio> {
  return new Promise((resolve) => {
    const xhr = new XMLHttpRequest();
    const corpo = new FormData();
    corpo.append("file", arquivo, arquivo.name);

    const recusa = (status: number, code: string, details: Record<string, unknown> = {}) => {
      const codigo = normalizarCodigo(code, status);
      const mensagem =
        status === 413 || details.reason === "too_large"
          ? "A foto passa de 15 MB. Reduza a resolução e tente de novo."
          : codigo === "NETWORK_ERROR"
            ? "Sem conexão agora. Confira o sinal e tente de novo."
            : mensagemDeBens({ code: codigo, details }, "foto");
      resolve({ ok: false, code: codigo, message: "", details, mensagem });
    };

    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable && e.total > 0) aoProgredir?.(Math.min(1, e.loaded / e.total));
    };
    xhr.onerror = () => recusa(0, "NETWORK_ERROR");
    xhr.onabort = () => recusa(0, "NETWORK_ERROR");
    xhr.onload = () => {
      let envelope: { data?: MidiaDeBem; error?: { code?: string; details?: Record<string, unknown> } } = {};
      try {
        envelope = xhr.responseText ? JSON.parse(xhr.responseText) : {};
      } catch {
        /* corpo fora do contrato: decide pelo status */
      }
      if (xhr.status >= 200 && xhr.status < 300 && envelope.data?.id) {
        aoProgredir?.(1);
        resolve({ ok: true, data: envelope.data });
        return;
      }
      recusa(xhr.status, envelope.error?.code ?? "", envelope.error?.details ?? {});
    };

    sinal?.addEventListener("abort", () => xhr.abort(), { once: true });
    xhr.open("POST", "/api/inventario/midia");
    xhr.setRequestHeader("Accept", "application/json");
    xhr.send(corpo);
  });
}
