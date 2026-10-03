import { normalizarCodigo } from "@/lib/api/codigos";
import type { Resultado } from "@/lib/acoes/resultado";
import { mensagemDoSite } from "@/lib/site/mensagens";
import type { MidiaEnviada } from "@/lib/site/tipos";

/**
 * Envio de foto ou vídeo pelo navegador, para `POST /api/site/midia` (a rota do
 * painel que repassa à API).
 *
 * `XMLHttpRequest` e não `fetch`: só ele avisa o progresso do **envio**, e um
 * vídeo de 200 MB sem barra andando parece travado.
 *
 * Nunca lança. Erro vem com `mensagem` já pronta para o gestor.
 */
export type ResultadoDoEnvio = Resultado<MidiaEnviada> & { mensagem?: string };

export function enviarArquivo(
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
      resolve({ ok: false, code: codigo, message: "", details, mensagem: mensagemDoSite({ code: codigo, details }, status) });
    };

    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable && e.total > 0) aoProgredir?.(Math.min(1, e.loaded / e.total));
    };
    xhr.onerror = () => recusa(0, "NETWORK_ERROR");
    xhr.onabort = () => recusa(0, "NETWORK_ERROR");
    xhr.onload = () => {
      let envelope: { data?: MidiaEnviada; error?: { code?: string; details?: Record<string, unknown> } } = {};
      try {
        envelope = xhr.responseText ? JSON.parse(xhr.responseText) : {};
      } catch {
        /* corpo que não é do contrato: decide pelo status */
      }
      if (xhr.status >= 200 && xhr.status < 300 && envelope.data?.id) {
        aoProgredir?.(1);
        resolve({ ok: true, data: envelope.data });
        return;
      }
      recusa(xhr.status, envelope.error?.code ?? "", envelope.error?.details ?? {});
    };

    sinal?.addEventListener("abort", () => xhr.abort(), { once: true });
    xhr.open("POST", "/api/site/midia");
    xhr.setRequestHeader("Accept", "application/json");
    xhr.send(corpo);
  });
}
