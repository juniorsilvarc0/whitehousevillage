"use client";

import * as React from "react";
import { ArrowDown, ArrowUp, Loader2, Plus, RotateCcw, Trash2 } from "lucide-react";
import { toast } from "sonner";

import { restaurarCampoDoSite, salvarCampoDoSite } from "@/app/(app)/app/site/acoes";
import { ModalDeConfirmacao } from "@/components/layout/modal-de-confirmacao";
import { SeletorDeMidia } from "@/components/site/seletor-de-midia";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import type { Resultado } from "@/lib/acoes/resultado";
import {
  asteriscoSemPar,
  itemVazio,
  limiteDe,
  listaComFotoFaltando,
  mover,
  mudou,
  remover,
  trechosDoTitulo,
  valorParaGravar,
} from "@/lib/site/edicao";
import { mensagemDoSite } from "@/lib/site/mensagens";
import type { CampoDoSite, ItemDeLista, Midia, Subcampo, ValorDeCampo } from "@/lib/site/tipos";

/** As duas ações, injetáveis para o teste não precisar do servidor. */
export type AcoesDoSite = {
  salvar: (chave: string, valor: unknown) => Promise<Resultado<CampoDoSite>>;
  restaurar: (chave: string) => Promise<Resultado<null>>;
};

const ACOES_PADRAO: AcoesDoSite = { salvar: salvarCampoDoSite, restaurar: restaurarCampoDoSite };

export const DICA_DO_TITULO = "Coloque uma palavra entre *asteriscos* para destacá-la em itálico.";
export const DICA_DO_TEXTO_LONGO = "Deixe uma linha em branco para começar outro parágrafo.";

function valorAtual(campo: CampoDoSite): ValorDeCampo {
  return campo.value ?? campo.default_value;
}

function comoMidia(v: unknown): Midia | null {
  return v && typeof v === "object" && !Array.isArray(v) ? (v as Midia) : null;
}

/**
 * Um campo do site: rótulo, ajuda, o editor do tipo, "Texto original"/"Editado",
 * Salvar (só com mudança) e Restaurar original (só se editado, com confirmação).
 */
export function CampoDoSiteEditor({
  campo: inicial,
  podeEditar,
  acoes = ACOES_PADRAO,
  aoMudarEstado,
}: {
  campo: CampoDoSite;
  podeEditar: boolean;
  acoes?: AcoesDoSite;
  /** Avisa a seção quando o campo passa a ser (ou deixa de ser) editado. */
  aoMudarEstado?: (chave: string, editado: boolean) => void;
}) {
  const [campo, setCampo] = React.useState(inicial);
  const [rascunho, setRascunho] = React.useState<ValorDeCampo>(valorAtual(inicial));
  // Lista: cada linha tem uma identidade estável para o React não trocar o
  // conteúdo de lugar ao subir/descer.
  const [ids, setIds] = React.useState<number[]>(() =>
    Array.isArray(valorAtual(inicial)) ? (valorAtual(inicial) as ItemDeLista[]).map((_, i) => i) : [],
  );
  const proximoId = React.useRef(1000);
  const [salvando, setSalvando] = React.useState(false);
  const [erro, setErro] = React.useState<string | null>(null);
  const [confirmando, setConfirmando] = React.useState(false);

  const subcampos = campo.item_fields ?? [];
  const alterado = mudou(campo.kind, valorAtual(campo), rascunho, subcampos);
  const limite = limiteDe(campo);
  const texto = typeof rascunho === "string" ? rascunho : "";
  const passouDoLimite = limite !== null && (campo.kind === "texto" || campo.kind === "titulo" || campo.kind === "texto_longo") && texto.length > limite;
  const idBase = `campo-${campo.key.replace(/[^a-z0-9]/gi, "-")}`;

  function trocar(v: ValorDeCampo) {
    setRascunho(v);
    setErro(null);
  }

  function aplicarSalvo(novo: CampoDoSite) {
    const junto = { ...campo, ...novo, item_fields: novo.item_fields ?? campo.item_fields };
    setCampo(junto);
    const v = valorAtual(junto);
    setRascunho(v);
    if (Array.isArray(v)) setIds(v.map(() => proximoId.current++));
    aoMudarEstado?.(junto.key, !junto.is_default);
  }

  async function salvar() {
    setSalvando(true);
    setErro(null);
    try {
      const r = await acoes.salvar(campo.key, valorParaGravar(campo.kind, rascunho, subcampos));
      if (!r.ok) {
        setErro(mensagemDoSite(r));
        return;
      }
      aplicarSalvo(r.data ?? { ...campo, value: rascunho, is_default: false });
      toast.success("Salvo", { description: "O site mostra a mudança em até 1 minuto." });
    } finally {
      setSalvando(false);
    }
  }

  async function restaurar(): Promise<Resultado<unknown>> {
    const r = await acoes.restaurar(campo.key);
    if (r.ok) {
      aplicarSalvo({ ...campo, value: campo.default_value, is_default: true });
      toast.success("Original restaurado", { description: "O site volta ao texto original em até 1 minuto." });
    }
    return r;
  }

  function desfazer() {
    const v = valorAtual(campo);
    setRascunho(v);
    if (Array.isArray(v)) setIds(v.map(() => proximoId.current++));
    setErro(null);
  }

  // ── Lista ──
  const itens: ItemDeLista[] = Array.isArray(rascunho) ? rascunho : [];
  function mudarItem(i: number, chave: string, v: ItemDeLista[string]) {
    trocar(itens.map((item, j) => (j === i ? { ...item, [chave]: v } : item)));
  }

  return (
    <div className="flex flex-col gap-3 border-t border-border/50 py-5 first:border-t-0 first:pt-1" data-campo={campo.key}>
      <div className="flex flex-wrap items-center gap-2">
        <h3 className="text-sm font-semibold text-foreground">{campo.label}</h3>
        {campo.is_default ? (
          <Badge variant="outline">Texto original</Badge>
        ) : (
          <Badge variant="accent">Editado</Badge>
        )}
      </div>
      {campo.help ? <p className="-mt-2 text-xs text-muted-foreground">{campo.help}</p> : null}

      {campo.kind === "texto" ? (
        <Input
          id={idBase}
          aria-label={campo.label}
          value={texto}
          readOnly={!podeEditar}
          invalid={passouDoLimite}
          onChange={(e) => trocar(e.target.value)}
        />
      ) : null}

      {/* Título aceita quebra de linha: área de texto baixa, não campo de uma linha. */}
      {campo.kind === "titulo" ? (
        <div className="flex flex-col gap-1.5">
          <Textarea
            id={idBase}
            aria-label={campo.label}
            rows={Math.min(4, Math.max(2, texto.split("\n").length))}
            value={texto}
            readOnly={!podeEditar}
            invalid={passouDoLimite}
            onChange={(e) => trocar(e.target.value)}
          />
          <PreviaDoTitulo texto={texto} />
        </div>
      ) : null}

      {campo.kind === "texto_longo" ? (
        <Textarea
          id={idBase}
          aria-label={campo.label}
          rows={Math.min(10, Math.max(3, texto.split("\n").length + 1))}
          value={texto}
          readOnly={!podeEditar}
          invalid={passouDoLimite}
          onChange={(e) => trocar(e.target.value)}
        />
      ) : null}

      {campo.kind === "texto" || campo.kind === "titulo" || campo.kind === "texto_longo" ? (
        <div className="flex flex-wrap items-start justify-between gap-2 text-xs text-muted-foreground">
          <span>
            {campo.kind === "titulo" ? DICA_DO_TITULO : campo.kind === "texto_longo" ? DICA_DO_TEXTO_LONGO : null}
          </span>
          {limite !== null ? (
            <span className={passouDoLimite ? "font-medium text-destructive" : undefined} aria-live="polite">
              {texto.length} de {limite} letras
            </span>
          ) : null}
        </div>
      ) : null}

      {campo.kind === "imagem" ? (
        <div className="flex flex-col gap-3">
          <SeletorDeMidia
            tipo="imagem"
            rotulo={campo.label}
            valor={comoMidia(rascunho)}
            desabilitado={!podeEditar}
            aoMudar={(m) => trocar(m)}
          />
          <div className="flex flex-col gap-1.5">
            <label htmlFor={`${idBase}-alt`} className="text-xs font-medium text-foreground">
              Descrição da foto, para quem não enxerga e para o Google
            </label>
            <Input
              id={`${idBase}-alt`}
              value={comoMidia(rascunho)?.alt ?? ""}
              readOnly={!podeEditar}
              disabled={!comoMidia(rascunho)?.media_id}
              placeholder="Ex.: Piscina com espreguiçadeiras ao entardecer"
              onChange={(e) => trocar({ ...(comoMidia(rascunho) ?? {}), alt: e.target.value })}
            />
            {!comoMidia(rascunho)?.media_id ? (
              <p className="text-xs text-muted-foreground">Troque a foto para poder escrever a descrição.</p>
            ) : null}
          </div>
        </div>
      ) : null}

      {campo.kind === "video" ? (
        <SeletorDeMidia
          tipo="video"
          rotulo={campo.label}
          valor={comoMidia(rascunho)}
          desabilitado={!podeEditar}
          aoMudar={(m) => trocar({ media_id: m.media_id, url: m.url })}
        />
      ) : null}

      {campo.kind === "lista" ? (
        <div className="flex flex-col gap-3">
          {itens.length === 0 ? (
            <p className="rounded-xl border border-dashed border-border px-4 py-3 text-sm text-muted-foreground">
              Lista vazia: esta parte não aparece no site.
            </p>
          ) : null}
          <ol className="flex flex-col gap-3">
            {itens.map((item, i) => (
              <li key={ids[i] ?? `n${i}`} className="rounded-xl border border-border/60 bg-card p-3 sm:p-4">
                <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
                  <span className="text-xs font-semibold text-muted-foreground">Item {i + 1}</span>
                  {podeEditar ? (
                    <div className="flex flex-wrap gap-1">
                      <Button
                        variant="ghost"
                        size="sm"
                        disabled={i === 0}
                        aria-label={`Subir o item ${i + 1}`}
                        onClick={() => {
                          trocar(mover(itens, i, -1));
                          setIds(mover(ids, i, -1));
                        }}
                      >
                        <ArrowUp aria-hidden="true" /> Subir
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        disabled={i === itens.length - 1}
                        aria-label={`Descer o item ${i + 1}`}
                        onClick={() => {
                          trocar(mover(itens, i, 1));
                          setIds(mover(ids, i, 1));
                        }}
                      >
                        <ArrowDown aria-hidden="true" /> Descer
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        className="text-destructive"
                        aria-label={`Remover o item ${i + 1}`}
                        onClick={() => {
                          trocar(remover(itens, i));
                          setIds(remover(ids, i));
                        }}
                      >
                        <Trash2 aria-hidden="true" /> Remover
                      </Button>
                    </div>
                  ) : null}
                </div>
                <div className="grid gap-3 sm:grid-cols-2">
                  {subcampos.map((s) => (
                    <Subcampo
                      key={s.key}
                      sub={s}
                      id={`${idBase}-${ids[i] ?? i}-${s.key}`}
                      valor={item[s.key]}
                      podeEditar={podeEditar}
                      aoMudar={(v) => mudarItem(i, s.key, v)}
                    />
                  ))}
                </div>
              </li>
            ))}
          </ol>
          {podeEditar ? (
            <div>
              <Button
                variant="outline"
                size="sm"
                onClick={() => {
                  trocar([...itens, itemVazio(subcampos)]);
                  setIds([...ids, proximoId.current++]);
                }}
              >
                <Plus aria-hidden="true" /> Adicionar
              </Button>
            </div>
          ) : null}
          {listaComFotoFaltando(itens, subcampos) ? (
            <p className="text-xs text-muted-foreground">Há item sem foto: no site ele aparece com o fundo desenhado.</p>
          ) : null}
        </div>
      ) : null}

      {erro ? (
        <p role="alert" className="rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {erro}
        </p>
      ) : null}

      {podeEditar ? (
        <div className="flex flex-wrap items-center gap-2">
          <Button size="sm" onClick={salvar} disabled={!alterado || salvando || passouDoLimite}>
            {salvando ? <Loader2 className="animate-spin" aria-hidden="true" /> : null}
            Salvar
          </Button>
          {alterado && !salvando ? (
            <Button size="sm" variant="ghost" onClick={desfazer}>
              Desfazer
            </Button>
          ) : null}
          {!campo.is_default ? (
            <Button size="sm" variant="ghost" onClick={() => setConfirmando(true)} disabled={salvando}>
              <RotateCcw aria-hidden="true" /> Restaurar original
            </Button>
          ) : null}
        </div>
      ) : null}

      <ModalDeConfirmacao
        aberto={confirmando}
        aoMudar={setConfirmando}
        titulo="Restaurar o original?"
        descricao={
          <>
            <strong>{campo.label}</strong> volta a ser como era antes da primeira mudança. O que foi
            escrito ou enviado aqui deixa de aparecer no site.
          </>
        }
        rotuloConfirmar="Restaurar original"
        aoConfirmar={restaurar}
      />
    </div>
  );
}

function PreviaDoTitulo({ texto }: { texto: string }) {
  if (!texto.trim()) return null;
  return (
    <div className="flex flex-col gap-1">
      <p className="text-xs text-muted-foreground">
        Como fica no site:{" "}
        <span className="font-display text-sm text-foreground" data-previa>
          {trechosDoTitulo(texto).map((t, i) =>
            "quebra" in t ? (
              <br key={i} />
            ) : t.destaque ? (
              <em key={i} className="font-medium text-primary">
                {t.texto}
              </em>
            ) : (
              <React.Fragment key={i}>{t.texto}</React.Fragment>
            ),
          )}
        </span>
      </p>
      {asteriscoSemPar(texto) ? (
        <p className="text-xs text-destructive">Falta fechar o asterisco: o destaque precisa de um * antes e outro depois.</p>
      ) : null}
    </div>
  );
}

function Subcampo({
  sub,
  id,
  valor,
  podeEditar,
  aoMudar,
}: {
  sub: Subcampo;
  id: string;
  valor: ItemDeLista[string];
  podeEditar: boolean;
  aoMudar: (v: ItemDeLista[string]) => void;
}) {
  if (sub.kind === "imagem") {
    const midia = comoMidia(valor);
    return (
      <div className="flex flex-col gap-2 sm:col-span-2">
        <span className="text-xs font-medium text-foreground">{sub.label}</span>
        <SeletorDeMidia tipo="imagem" compacto rotulo={sub.label} valor={midia} desabilitado={!podeEditar} aoMudar={(m) => aoMudar(m)} />
        {midia?.media_id ? (
          <Input
            aria-label={`Descrição da foto: ${sub.label}`}
            placeholder="Descrição da foto, para quem não enxerga e para o Google"
            value={midia.alt ?? ""}
            readOnly={!podeEditar}
            onChange={(e) => aoMudar({ ...midia, alt: e.target.value })}
          />
        ) : null}
      </div>
    );
  }
  const texto = typeof valor === "string" ? valor : "";
  return (
    <div className={sub.kind === "texto_longo" ? "flex flex-col gap-1.5 sm:col-span-2" : "flex flex-col gap-1.5"}>
      <label htmlFor={id} className="text-xs font-medium text-foreground">
        {sub.label}
      </label>
      {sub.kind === "texto_longo" ? (
        <Textarea id={id} rows={3} value={texto} readOnly={!podeEditar} onChange={(e) => aoMudar(e.target.value)} />
      ) : (
        <Input id={id} value={texto} readOnly={!podeEditar} onChange={(e) => aoMudar(e.target.value)} />
      )}
    </div>
  );
}
