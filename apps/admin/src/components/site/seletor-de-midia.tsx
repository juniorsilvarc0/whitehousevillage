"use client";

import * as React from "react";
import { ImageIcon, Loader2, Upload, Video } from "lucide-react";

import { Button } from "@/components/ui/button";
import { AVISO_DE_LIMITE, LIMITES, conferirArquivo, enderecoDePrevia } from "@/lib/site/edicao";
import { enviarArquivo } from "@/lib/site/envio";
import type { Midia } from "@/lib/site/tipos";
import { cn } from "@/lib/utils";

/**
 * Prévia + "Trocar foto"/"Trocar vídeo", com barra de progresso.
 *
 * Escolher o arquivo **envia** o arquivo, mas não publica: o site só passa a
 * mostrá-lo quando o gestor clicar em Salvar, como nos textos. O arquivo
 * enviado e não salvo fica guardado sem uso — não aparece em lugar nenhum.
 */
export function SeletorDeMidia({
  tipo,
  valor,
  aoMudar,
  desabilitado,
  compacto,
  rotulo,
}: {
  tipo: "imagem" | "video";
  valor: Midia | null;
  aoMudar: (m: Midia) => void;
  desabilitado?: boolean;
  compacto?: boolean;
  /** Nome do campo, para o leitor de tela distinguir botões iguais. */
  rotulo: string;
}) {
  const entrada = React.useRef<HTMLInputElement>(null);
  const [progresso, setProgresso] = React.useState<number | null>(null);
  const [erro, setErro] = React.useState<string | null>(null);
  const previa = enderecoDePrevia(valor);
  const enviando = progresso !== null;

  async function escolher(e: React.ChangeEvent<HTMLInputElement>) {
    const arquivo = e.target.files?.[0];
    e.target.value = "";
    if (!arquivo) return;
    const problema = conferirArquivo(arquivo, tipo);
    if (problema) {
      setErro(problema);
      return;
    }
    setErro(null);
    setProgresso(0);
    const r = await enviarArquivo(arquivo, setProgresso);
    setProgresso(null);
    if (!r.ok) {
      setErro(r.mensagem ?? "Não foi possível enviar o arquivo.");
      return;
    }
    aoMudar({ media_id: r.data.id, url: r.data.url, alt: valor?.alt ?? "" });
  }

  const Icone = tipo === "imagem" ? ImageIcon : Video;
  const porcento = Math.round((progresso ?? 0) * 100);

  return (
    <div className={cn("flex flex-col gap-2", !compacto && "sm:flex-row sm:items-start sm:gap-4")}>
      <div
        className={cn(
          "relative flex shrink-0 items-center justify-center overflow-hidden rounded-xl border border-border/60 bg-muted/40",
          compacto ? "h-28 w-full sm:w-44" : "aspect-video w-full sm:w-72",
        )}
      >
        {previa ? (
          tipo === "imagem" ? (
            // Prévia de arquivo enviado: tamanho e endereço variam, o otimizador do
            // Next não ajuda aqui.
            // eslint-disable-next-line @next/next/no-img-element
            <img src={previa} alt={valor?.alt ?? ""} className="size-full object-contain" />
          ) : (
            <video
              key={previa}
              src={previa}
              muted
              loop
              autoPlay
              playsInline
              preload="metadata"
              aria-label="Prévia do vídeo"
              className="size-full object-cover"
            />
          )
        ) : (
          <div className="flex flex-col items-center gap-1 px-3 text-center text-xs text-muted-foreground">
            <Icone className="size-5" aria-hidden="true" />
            {tipo === "imagem" ? "Sem foto: o site mostra um fundo desenhado." : "Sem vídeo."}
          </div>
        )}
      </div>

      <div className="flex min-w-0 flex-col gap-2">
        <div>
          <Button
            variant="outline"
            size="sm"
            disabled={desabilitado || enviando}
            onClick={() => entrada.current?.click()}
            aria-label={`${tipo === "imagem" ? "Trocar foto" : "Trocar vídeo"}: ${rotulo}`}
          >
            {enviando ? <Loader2 className="animate-spin" aria-hidden="true" /> : <Upload aria-hidden="true" />}
            {tipo === "imagem" ? "Trocar foto" : "Trocar vídeo"}
          </Button>
          <input
            ref={entrada}
            type="file"
            accept={LIMITES[tipo].aceitar}
            className="sr-only"
            tabIndex={-1}
            aria-hidden="true"
            onChange={escolher}
          />
        </div>
        <p className="text-xs text-muted-foreground">{AVISO_DE_LIMITE[tipo]}</p>
        {enviando ? (
          <div className="flex flex-col gap-1" aria-live="polite">
            <div
              role="progressbar"
              aria-valuemin={0}
              aria-valuemax={100}
              aria-valuenow={porcento}
              aria-label="Enviando o arquivo"
              className="h-2 w-full max-w-64 overflow-hidden rounded-full bg-muted"
            >
              <div className="h-full bg-brand-gradient transition-[width]" style={{ width: `${porcento}%` }} />
            </div>
            <span className="text-xs text-muted-foreground">
              {porcento < 100 ? `Enviando… ${porcento}%` : "Quase pronto…"}
            </span>
          </div>
        ) : null}
        {erro ? (
          <p role="alert" className="text-xs font-medium text-destructive">
            {erro}
          </p>
        ) : null}
      </div>
    </div>
  );
}
