"use client";

import * as React from "react";
import { ArrowLeft, ArrowRight, Loader2, Star, Trash2, Upload } from "lucide-react";

import { notificarFalha } from "@/components/bens/avisos";
import { FotoDoBem } from "@/components/bens/foto";
import { ConfirmacaoDoInventario } from "@/components/bens/confirmacao";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { salvarGaleria } from "@/lib/bens/acoes";
import { enviarFoto } from "@/lib/bens/envio";
import { ACEITAR, AVISO_DE_LIMITE, conferirArquivo, enderecoDaFoto } from "@/lib/bens/midia";
import type { FotoDoBem as Foto, MidiaDeBem } from "@/lib/bens/tipos";

const MAXIMO_DE_FOTOS = 24;

/** A ordem de exibição: `sort_order`, desempatado pelo id da mídia — a mesma
 *  regra da API, senão duas fotos empatadas trocariam de capa a cada abertura. */
function emOrdem(fotos: readonly Foto[]): MidiaDeBem[] {
  return [...fotos].sort((a, b) => a.sort_order - b.sort_order || a.media.id.localeCompare(b.media.id)).map((f) => f.media);
}

/**
 * A galeria do bem: enviar, reordenar, escolher a capa, tirar.
 *
 * ## Cada gesto grava na hora, otimista
 *
 * `PUT /inventory/items/{id}/photos` **substitui** a galeria inteira com a
 * ordem do array — o primeiro é a capa. A tela manda o estado completo a cada
 * gesto e mostra o resultado antes da resposta; se a API recusar, a galeria
 * volta ao que era e o toast diz por quê. Não há botão "Salvar galeria" para
 * esquecer de apertar depois de subir 15 MB pelo sinal do apartamento.
 *
 * Tirar uma foto da galeria **não apaga o arquivo** (a mídia é imutável e pode
 * estar ligada a outros itens da mesma cena) — e é por isso que a confirmação
 * fala em "tirar", não em "apagar".
 */
export function GaleriaDoBem({
  itemId,
  nome,
  fotos,
  podeEnviar,
  podeOrganizar,
}: {
  itemId: string;
  nome: string;
  fotos: Foto[];
  /** Enviar exige `criar` (a mídia) **e** `editar` (a galeria): sem o segundo,
   *  o arquivo subiria e ficaria solto, sem aparecer em lugar nenhum. */
  podeEnviar: boolean;
  podeOrganizar: boolean;
}) {
  const inicial = React.useMemo(() => emOrdem(fotos), [fotos]);
  const [lista, setLista] = React.useState<MidiaDeBem[]>(inicial);
  const [base, setBase] = React.useState(inicial);
  // Novas fotos vindas do servidor (revalidação) repõem a lista — ajuste no
  // render, não em efeito, como em `ModalDeConfirmacao`.
  if (inicial !== base) {
    setBase(inicial);
    setLista(inicial);
  }

  const [progresso, setProgresso] = React.useState<number | null>(null);
  const [erroDoEnvio, setErroDoEnvio] = React.useState<string | null>(null);
  const [salvando, setSalvando] = React.useState(false);
  const [aTirar, setATirar] = React.useState<MidiaDeBem | null>(null);
  const entrada = React.useRef<HTMLInputElement>(null);

  async function gravar(proxima: MidiaDeBem[], anterior: MidiaDeBem[]): Promise<boolean> {
    setLista(proxima);
    setSalvando(true);
    const r = await salvarGaleria(
      itemId,
      proxima.map((m) => m.id),
    );
    setSalvando(false);
    if (!r.ok) {
      setLista(anterior);
      notificarFalha("A galeria não foi salva", r, "foto");
      return false;
    }
    setLista(emOrdem(r.data));
    return true;
  }

  async function escolher(e: React.ChangeEvent<HTMLInputElement>) {
    const arquivos = [...(e.target.files ?? [])];
    e.target.value = "";
    if (arquivos.length === 0) return;
    setErroDoEnvio(null);

    let atual = lista;
    for (const arquivo of arquivos) {
      if (atual.length >= MAXIMO_DE_FOTOS) {
        setErroDoEnvio(`Cada bem aceita até ${MAXIMO_DE_FOTOS} fotos.`);
        break;
      }
      const problema = conferirArquivo(arquivo);
      if (problema) {
        setErroDoEnvio(`${arquivo.name}: ${problema}`);
        continue;
      }
      setProgresso(0);
      const r = await enviarFoto(arquivo, setProgresso);
      setProgresso(null);
      if (!r.ok) {
        setErroDoEnvio(r.mensagem ?? "Não foi possível enviar a foto.");
        continue;
      }
      const proxima = [...atual, r.data];
      if (await gravar(proxima, atual)) atual = proxima;
    }
  }

  function mover(indice: number, passo: -1 | 1) {
    const destino = indice + passo;
    if (destino < 0 || destino >= lista.length) return;
    const proxima = [...lista];
    const [m] = proxima.splice(indice, 1);
    proxima.splice(destino, 0, m);
    void gravar(proxima, lista);
  }

  function tornarCapa(indice: number) {
    if (indice === 0) return;
    const proxima = [lista[indice], ...lista.filter((_, i) => i !== indice)];
    void gravar(proxima, lista);
  }

  const porcento = Math.round((progresso ?? 0) * 100);
  const enviando = progresso !== null;

  return (
    <div className="flex flex-col gap-4">
      {lista.length === 0 ? (
        <p className="rounded-xl border border-dashed border-border/70 bg-muted/20 px-4 py-6 text-center text-sm text-muted-foreground">
          Este bem ainda não tem foto. É pela foto que quem confere reconhece a peça — a taça certa, e não “taça de vinho
          nº 2”.
        </p>
      ) : (
        <ol className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4">
          {lista.map((midia, i) => (
            <li key={midia.id} className="flex flex-col gap-2 rounded-xl border border-border/60 bg-card/70 p-2">
              <a href={enderecoDaFoto(midia, "original")} target="_blank" rel="noreferrer" className="block rounded-lg outline-none focus-visible:ring-2 focus-visible:ring-ring">
                <FotoDoBem midia={midia} alt={`${nome} — foto ${i + 1}`} className="aspect-square w-full" />
              </a>
              <div className="flex items-center justify-between gap-1">
                {i === 0 ? (
                  <Badge variant="brand">
                    <Star aria-hidden="true" />
                    capa
                  </Badge>
                ) : (
                  <span className="text-xs tabular-nums text-muted-foreground">{i + 1}ª</span>
                )}
                {podeOrganizar ? (
                  <div className="flex gap-0.5">
                    <Button size="iconSm" variant="ghost" aria-label={`Mover a foto ${i + 1} para antes`} disabled={i === 0 || salvando} onClick={() => mover(i, -1)}>
                      <ArrowLeft aria-hidden="true" />
                    </Button>
                    <Button
                      size="iconSm"
                      variant="ghost"
                      aria-label={`Mover a foto ${i + 1} para depois`}
                      disabled={i === lista.length - 1 || salvando}
                      onClick={() => mover(i, 1)}
                    >
                      <ArrowRight aria-hidden="true" />
                    </Button>
                    {i > 0 ? (
                      <Button size="iconSm" variant="ghost" aria-label={`Usar a foto ${i + 1} como capa`} disabled={salvando} onClick={() => tornarCapa(i)}>
                        <Star aria-hidden="true" />
                      </Button>
                    ) : null}
                    <Button size="iconSm" variant="ghost" aria-label={`Tirar a foto ${i + 1} da galeria`} disabled={salvando} onClick={() => setATirar(midia)}>
                      <Trash2 aria-hidden="true" />
                    </Button>
                  </div>
                ) : null}
              </div>
            </li>
          ))}
        </ol>
      )}

      {podeEnviar ? (
        <div className="flex flex-col gap-2">
          <div className="flex flex-wrap items-center gap-3">
            <Button variant="outline" size="sm" disabled={enviando || salvando || lista.length >= MAXIMO_DE_FOTOS} onClick={() => entrada.current?.click()}>
              {enviando ? <Loader2 className="animate-spin" aria-hidden="true" /> : <Upload aria-hidden="true" />}
              {lista.length === 0 ? "Enviar foto" : "Enviar mais fotos"}
            </Button>
            <span className="text-xs text-muted-foreground">{AVISO_DE_LIMITE} No celular, abre a câmera.</span>
            <input
              ref={entrada}
              type="file"
              accept={ACEITAR}
              multiple
              className="sr-only"
              tabIndex={-1}
              aria-hidden="true"
              onChange={escolher}
            />
          </div>
          {enviando ? (
            <div className="flex flex-col gap-1" aria-live="polite">
              <div
                role="progressbar"
                aria-valuemin={0}
                aria-valuemax={100}
                aria-valuenow={porcento}
                aria-label="Enviando a foto"
                className="h-2 w-full max-w-72 overflow-hidden rounded-full bg-muted"
              >
                <div className="h-full bg-brand-gradient transition-[width]" style={{ width: `${porcento}%` }} />
              </div>
              <span className="text-xs text-muted-foreground">{porcento < 100 ? `Enviando… ${porcento}%` : "Gerando a miniatura…"}</span>
            </div>
          ) : null}
          {erroDoEnvio ? (
            <p role="alert" className="text-xs font-medium text-destructive">
              {erroDoEnvio}
            </p>
          ) : null}
        </div>
      ) : null}

      <ConfirmacaoDoInventario
        contexto="foto"
        aberto={aTirar !== null}
        aoMudar={(aberto) => !aberto && setATirar(null)}
        titulo="Tirar esta foto da galeria?"
        rotuloConfirmar="Tirar da galeria"
        destrutivo
        descricao={<>A foto deixa de aparecer neste bem. O arquivo continua guardado — ele pode mostrar outros itens da mesma cena.</>}
        aoConfirmar={async () => {
          const alvo = aTirar;
          if (!alvo) return { ok: true as const, data: null };
          const anterior = lista;
          const proxima = lista.filter((m) => m.id !== alvo.id);
          setLista(proxima);
          const r = await salvarGaleria(
            itemId,
            proxima.map((m) => m.id),
          );
          if (!r.ok) setLista(anterior);
          else setLista(emOrdem(r.data));
          return r;
        }}
      />
    </div>
  );
}
