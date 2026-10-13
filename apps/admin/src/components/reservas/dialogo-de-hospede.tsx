"use client";

import * as React from "react";
import Link from "next/link";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { ArrowLeft, Loader2, Search, UserPlus, UserRoundCheck } from "lucide-react";

import { CamposDoContato, valoresDoContato } from "@/components/contatos/campos-do-contato";
import type { ControleDeModal } from "@/components/layout/controle-de-modal";
import { ModalShell } from "@/components/layout/modal-shell";
import { Nota } from "@/components/layout/tela";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Campo } from "@/components/ui/campo";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import { aplicarFalha } from "@/lib/acoes/formulario";
import { falha, mensagemDoErro, type Falha } from "@/lib/acoes/resultado";
import { documentoValido, formatarDocumento } from "@/lib/contatos/documento";
import { ContatoFormulario } from "@/lib/contatos/esquemas";
import { ROTULO_DO_DOCUMENTO, TIPOS_DE_DOCUMENTO, type TipoDeDocumento } from "@/lib/contatos/tipos";
import {
  acrescentarHospede,
  aumentarHospedes,
  cadastrarEAcrescentar,
  procurarPorDocumento,
  type DuplicataNaLista,
  type PessoaEncontrada,
  type PessoaNaLista,
} from "@/lib/reservas/acoes-de-hospedes";
import {
  cabeAumentar,
  ehTitular,
  lerListaCheia,
  mensagemDaLista,
  numeroParaMaisUm,
  type GestoNaLista,
} from "@/lib/reservas/hospedes";
import type { HospedeDaReserva, Reserva } from "@/lib/reservas/tipos";
import { cn } from "@/lib/utils";

/**
 * "Acrescentar hóspede" — o fluxo do balcão, do jeito que o contrato o desenha
 * (`POST /reservations/{id}/guests`):
 *
 * 1. **Procura pelo documento** (`GET /contacts?doc_number=`). A linha volta
 *    mascarada; a tela mostra a máscara e escolhe **pelo id**.
 * 2. **Achou**: acrescenta pelo `contact_id`.
 * 3. **Não achou**: "Cadastrar pessoa" com o formulário de contato de sempre
 *    (`POST /contacts`) e, na mesma ida ao servidor, o `POST` de hóspede.
 * 4. **O cadastro colidiu** (`409 CONTACT_DUPLICATE`): segue com o
 *    `details.contact_id` — depois de mostrar **quem** é. Telefone repetido é
 *    comum em família, e seguir sem perguntar acrescentaria a pessoa que já tem
 *    aquele número, não a que está no balcão.
 *
 * Cada recusa é lida pelo código (`mensagemDaLista`), dentro do diálogo, onde o
 * operador está olhando. `GUEST_LIST_FULL` com o vendido abaixo da capacidade
 * oferece o caminho do contrato: aumentar o número de hóspedes e acrescentar,
 * numa ida só.
 *
 * O documento digitado vive só no estado deste diálogo: não vai para a URL, para
 * `localStorage` nem para o console, e o campo não entra no histórico de
 * preenchimento do navegador.
 */

type Passo =
  | { passo: "busca" }
  | { passo: "resultado"; encontrados: PessoaEncontrada[] }
  | { passo: "cadastro" }
  | { passo: "duplicata"; duplicata: DuplicataNaLista }
  /** A ficha nasceu, a pessoa não entrou: tenta-se de novo com este id, sem
   *  cadastrar de novo. */
  | { passo: "pendente"; pessoa: PessoaNaLista };

/** A última tentativa de pôr alguém na lista que não deu certo. */
type Tentativa = {
  falha: Falha;
  gesto: GestoNaLista;
  pessoa: PessoaNaLista | null;
  /** O que já aconteceu antes da recusa ("a ficha foi criada", "o número
   *  subiu") — sem isso a recusa parece dizer que nada mudou. */
  contexto: string | null;
};

type Envio = null | "busca" | "acrescentar" | "aumentar";

const ID_DA_BUSCA = "busca-de-hospede";
const ID_DO_CADASTRO = "cadastro-de-hospede";

/** Recusas que tentar de novo pode resolver. As outras dizem o que mudar. */
const TRANSITORIAS: readonly Falha["code"][] = ["NETWORK_ERROR", "INTERNAL", "RATE_LIMITED"];

/** Tom de aviso, não de falha: a pessoa já está lá, ou falta abrir lugar. */
const INFORMATIVAS: readonly Falha["code"][] = ["CODE_IN_USE", "GUEST_LIST_FULL"];

/** A action não respondeu (rede, servidor do painel reiniciando): é recusa
 *  de conexão, e "tentar de novo" é o gesto certo. */
async function seguro<T>(chamada: () => Promise<T>): Promise<T | Falha> {
  try {
    return await chamada();
  } catch {
    return falha("NETWORK_ERROR", "");
  }
}

function primeiroNome(nome: string): string {
  return nome.trim().split(/\s+/)[0] || nome;
}

function erroDoDocumento(tipo: TipoDeDocumento): string {
  return tipo === "passaporte"
    ? "Passaporte tem de 5 a 20 letras ou números."
    : `${ROTULO_DO_DOCUMENTO[tipo]} inválido — confira os dígitos.`;
}

export type ReservaDoDialogo = Pick<Reserva, "id" | "code" | "guests_count" | "contact_id">;

export function DialogoDeHospede({
  controle,
  reserva,
  hospedes,
  podeProcurar,
  podeCadastrar,
  aoConcluir,
}: {
  controle: React.RefObject<ControleDeModal<null> | null>;
  reserva: ReservaDoDialogo;
  /** A lista como está na tela — para marcar quem a busca acha e já está nela. */
  hospedes: HospedeDaReserva[];
  /** `contacts:ver`: sem ela não há como achar a pessoa. */
  podeProcurar: boolean;
  /** `contacts:criar`: sem ela, "Cadastrar pessoa" não é oferecido. */
  podeCadastrar: boolean;
  /** O que a seção anuncia quando a pessoa entra. */
  aoConcluir: (aviso: string) => void;
}) {
  const [aberto, setAberto] = React.useState(false);
  const [passo, setPasso] = React.useState<Passo>({ passo: "busca" });
  const [tipo, setTipo] = React.useState<TipoDeDocumento>("cpf");
  const [numero, setNumero] = React.useState("");
  const [erroDoNumero, setErroDoNumero] = React.useState<string | null>(null);
  const [falhaDaBusca, setFalhaDaBusca] = React.useState<Falha | null>(null);
  const [tentativa, setTentativa] = React.useState<Tentativa | null>(null);
  const [erroGeral, setErroGeral] = React.useState<string | null>(null);
  const [enviando, setEnviando] = React.useState<Envio>(null);
  // Número da abertura em curso. Resposta que chega depois de o diálogo fechar
  // (ou reabrir) é de outra abertura e não pode mexer nesta.
  const abertura = React.useRef(0);

  const form = useForm<ContatoFormulario>({
    resolver: zodResolver(ContatoFormulario),
    defaultValues: valoresDoContato(null),
  });

  React.useImperativeHandle(
    controle,
    () => ({
      abrir() {
        abertura.current += 1;
        setPasso({ passo: "busca" });
        setTipo("cpf");
        setNumero("");
        setErroDoNumero(null);
        setFalhaDaBusca(null);
        setTentativa(null);
        setErroGeral(null);
        setEnviando(null);
        form.reset(valoresDoContato(null));
        setAberto(true);
      },
    }),
    [form],
  );

  function mudarAberto(valor: boolean) {
    if (!valor) abertura.current += 1;
    setAberto(valor);
  }

  function concluir(aviso: string) {
    abertura.current += 1;
    setAberto(false);
    aoConcluir(aviso);
  }

  /** Roda um passo de rede marcando o botão ocupado, e descarta a resposta de
   *  uma abertura que já passou. */
  async function enviar<T>(qual: Exclude<Envio, null>, chamada: () => Promise<T>): Promise<T | Falha | null> {
    const minha = abertura.current;
    setEnviando(qual);
    try {
      const resposta = await seguro(chamada);
      return minha === abertura.current ? resposta : null;
    } finally {
      if (minha === abertura.current) setEnviando(null);
    }
  }

  const contatosNaLista = React.useMemo(() => new Set(hospedes.map((h) => h.contact_id)), [hospedes]);

  // ── 1. Procurar ──────────────────────────────────────────────────────────

  async function procurar(evento: React.FormEvent) {
    evento.preventDefault();
    setFalhaDaBusca(null);
    if (numero.trim() === "") {
      setErroDoNumero("Digite o número do documento.");
      return;
    }
    // A mesma conferência do cadastro, antes de gastar a busca: o CPF com um
    // dígito trocado não acharia ninguém e levaria a um cadastro recusado.
    if (!documentoValido(tipo, numero)) {
      setErroDoNumero(erroDoDocumento(tipo));
      return;
    }
    setErroDoNumero(null);

    const r = await enviar("busca", () => procurarPorDocumento(tipo, numero));
    if (r === null) return;
    if (!r.ok) {
      const doCampo = typeof r.details.doc_number === "string" ? r.details.doc_number : null;
      if (doCampo) setErroDoNumero(doCampo);
      else setFalhaDaBusca(r);
      return;
    }
    setTentativa(null);
    setPasso({ passo: "resultado", encontrados: r.data });
  }

  // ── 2. Acrescentar quem tem ficha ────────────────────────────────────────

  async function acrescentar(pessoa: PessoaNaLista) {
    setTentativa(null);
    const r = await enviar("acrescentar", () => acrescentarHospede(reserva.id, pessoa.contact_id));
    if (r === null) return;
    if (r.ok) {
      concluir(`${r.data.name || pessoa.name} entrou na lista.`);
      return;
    }
    setTentativa({ falha: r, gesto: "acrescentar", pessoa, contexto: null });
  }

  /** `GUEST_LIST_FULL` com lugar no produto: o número sobe e a pessoa entra,
   *  numa ida ao servidor. */
  async function aumentarEAcrescentar(novoNumero: number, pessoa: PessoaNaLista) {
    setTentativa(null);
    const r = await enviar("aumentar", () => aumentarHospedes(reserva.id, novoNumero, pessoa.contact_id));
    if (r === null) return;
    if (r.ok) {
      const quem = r.data.acrescentada?.name || pessoa.name;
      concluir(`A reserva agora é para ${r.data.guests_count} hóspedes, e ${quem} entrou na lista.`);
      return;
    }
    const subiu = "etapa" in r && r.etapa === "lista";
    setTentativa({
      falha: r,
      gesto: subiu ? "acrescentar" : "aumentar",
      pessoa,
      contexto: subiu ? `A reserva agora é para ${novoNumero} hóspedes, mas ${pessoa.name} ainda não está na lista.` : null,
    });
  }

  // ── 3. Cadastrar e acrescentar ───────────────────────────────────────────

  function irParaCadastro() {
    // Só o documento é reposto: quem volta da busca para o cadastro não perde
    // o nome e o telefone que já tinha digitado.
    form.setValue("doc_type", tipo);
    form.setValue("doc_number", numero.trim());
    form.clearErrors();
    setErroGeral(null);
    setTentativa(null);
    setPasso({ passo: "cadastro" });
  }

  async function cadastrar(valores: ContatoFormulario) {
    setErroGeral(null);
    setTentativa(null);
    const minha = abertura.current;
    const r = await seguro(() => cadastrarEAcrescentar(reserva.id, valores));
    if (minha !== abertura.current) return;

    if (r.ok) {
      concluir(`Ficha criada, e ${r.data.name || valores.name.trim()} entrou na lista.`);
      return;
    }
    if ("duplicata" in r) {
      setPasso({ passo: "duplicata", duplicata: r.duplicata });
      return;
    }
    if ("cadastrada" in r) {
      setPasso({ passo: "pendente", pessoa: r.cadastrada });
      setTentativa({
        falha: r.falha,
        gesto: "acrescentar",
        pessoa: r.cadastrada,
        contexto: `A ficha de ${r.cadastrada.name} foi criada, mas a pessoa ainda não está na lista.`,
      });
      return;
    }
    setErroGeral(aplicarFalha(r, form));
  }

  function corrigirDuplicata(duplicata: DuplicataNaLista) {
    const campo = duplicata.campo === "doc_number" ? "doc_number" : "phone_e164";
    setTentativa(null);
    setPasso({ passo: "cadastro" });
    form.setError(campo, { type: "server", message: "Já pertence a outro cadastro." });
    // O campo volta a existir no próximo quadro; o foco espera por ele.
    setTimeout(() => form.setFocus(campo), 0);
  }

  // ── Desenho ──────────────────────────────────────────────────────────────

  const listados = hospedes.length;
  const ocupado = enviando !== null || form.formState.isSubmitting;

  const rodape =
    passo.passo === "busca" ? (
      <>
        <Button variant="ghost" onClick={() => mudarAberto(false)} disabled={ocupado}>
          Cancelar
        </Button>
        {podeProcurar ? (
          <Button type="submit" form={ID_DA_BUSCA} disabled={ocupado}>
            {enviando === "busca" ? <Loader2 className="animate-spin" aria-hidden="true" /> : <Search aria-hidden="true" />}
            Procurar
          </Button>
        ) : null}
      </>
    ) : passo.passo === "cadastro" ? (
      <>
        <Button variant="ghost" onClick={() => setPasso({ passo: "busca" })} disabled={ocupado}>
          <ArrowLeft aria-hidden="true" />
          Voltar
        </Button>
        <Button type="submit" form={ID_DO_CADASTRO} disabled={ocupado}>
          {form.formState.isSubmitting ? (
            <Loader2 className="animate-spin" aria-hidden="true" />
          ) : (
            <UserPlus aria-hidden="true" />
          )}
          Cadastrar e acrescentar
        </Button>
      </>
    ) : passo.passo === "pendente" ? (
      <Button variant="ghost" onClick={() => mudarAberto(false)} disabled={ocupado}>
        Fechar
      </Button>
    ) : (
      <Button variant="ghost" onClick={() => setPasso({ passo: "busca" })} disabled={ocupado}>
        <ArrowLeft aria-hidden="true" />
        Procurar outro documento
      </Button>
    );

  return (
    <ModalShell
      open={aberto}
      onOpenChange={mudarAberto}
      title="Acrescentar hóspede"
      description={`${reserva.code} · ${listados} de ${reserva.guests_count} com nome na lista`}
      footer={rodape}
    >
      {passo.passo === "busca" ? (
        <PassoDeBusca
          podeProcurar={podeProcurar}
          tipo={tipo}
          numero={numero}
          erro={erroDoNumero}
          falha={falhaDaBusca}
          aoMudarTipo={(t) => {
            setTipo(t);
            setErroDoNumero(null);
          }}
          aoMudarNumero={(n) => {
            setNumero(n);
            setErroDoNumero(null);
          }}
          aoProcurar={procurar}
        />
      ) : null}

      {passo.passo === "resultado" ? (
        <div className="flex flex-col gap-4">
          {passo.encontrados.length > 0 ? (
            <>
              <p className="text-sm text-muted-foreground">
                Encontrado no cadastro. Confira o nome antes de acrescentar — o documento aparece mascarado, e
                a lista mostra o número completo depois que a pessoa entra.
              </p>
              <ul aria-label="Pessoas encontradas" className="flex flex-col gap-2">
                {passo.encontrados.map((pessoa) => {
                  // O titular é o contato da reserva, esteja ou não a linha dele
                  // na lista: acrescentá-lo é `409 CODE_IN_USE` de qualquer
                  // jeito (`CheckAdd` compara com `reservations.contact_id`).
                  const titular = ehTitular({ contact_id: pessoa.id }, reserva.contact_id);
                  const naLista = contatosNaLista.has(pessoa.id);
                  return (
                    <li
                      key={pessoa.id}
                      className="flex flex-col gap-2 rounded-lg border border-border/60 bg-card px-3 py-2.5 sm:flex-row sm:items-center sm:justify-between"
                    >
                      <div className="min-w-0">
                        <p className="font-medium">{pessoa.name}</p>
                        {pessoa.doc_number ? (
                          <p className="font-mono text-xs text-muted-foreground">
                            {pessoa.doc_type ? `${ROTULO_DO_DOCUMENTO[pessoa.doc_type]} ` : ""}
                            {pessoa.doc_number}
                          </p>
                        ) : null}
                      </div>
                      {titular || naLista ? (
                        <Badge variant="accent">{titular ? "Titular desta reserva" : "Já está na lista"}</Badge>
                      ) : (
                        <Button
                          size="sm"
                          className="max-sm:h-10 max-sm:w-full max-sm:text-sm"
                          disabled={ocupado}
                          onClick={() => void acrescentar({ contact_id: pessoa.id, name: pessoa.name })}
                        >
                          {enviando === "acrescentar" ? (
                            <Loader2 className="animate-spin" aria-hidden="true" />
                          ) : (
                            <UserPlus aria-hidden="true" />
                          )}
                          Acrescentar {primeiroNome(pessoa.name)}
                        </Button>
                      )}
                    </li>
                  );
                })}
              </ul>
            </>
          ) : (
            <NaoAchou tipo={tipo} numero={numero} podeCadastrar={podeCadastrar} aoCadastrar={irParaCadastro} />
          )}

          {tentativa ? (
            <AvisoDaTentativa
              tentativa={tentativa}
              ocupado={ocupado}
              aoAumentar={aumentarEAcrescentar}
              aoTentarDeNovo={acrescentar}
            />
          ) : null}
        </div>
      ) : null}

      {passo.passo === "cadastro" ? (
        <div className="flex flex-col gap-4">
          <Nota>
            A ficha nasce no cadastro de contatos — a mesma de qualquer outra estadia — e a pessoa entra na lista
            em seguida.
          </Nota>
          {erroGeral ? (
            <p role="alert" className="rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive">
              {erroGeral}
            </p>
          ) : null}
          <CamposDoContato form={form} idDoForm={ID_DO_CADASTRO} aoEnviar={cadastrar} prefixo="hospede" />
        </div>
      ) : null}

      {passo.passo === "duplicata" ? (
        <div className="flex flex-col gap-4">
          <CaminhoDaDuplicata
            duplicata={passo.duplicata}
            ocupado={ocupado}
            aoAcrescentar={acrescentar}
            aoCorrigir={() => corrigirDuplicata(passo.duplicata)}
            aoProcurar={() => setPasso({ passo: "busca" })}
          />
          {tentativa ? (
            <AvisoDaTentativa
              tentativa={tentativa}
              ocupado={ocupado}
              aoAumentar={aumentarEAcrescentar}
              aoTentarDeNovo={acrescentar}
            />
          ) : null}
        </div>
      ) : null}

      {passo.passo === "pendente" ? (
        <div className="flex flex-col gap-4">
          {tentativa ? (
            <AvisoDaTentativa
              tentativa={tentativa}
              ocupado={ocupado}
              aoAumentar={aumentarEAcrescentar}
              aoTentarDeNovo={acrescentar}
            />
          ) : null}
          <p className="text-sm text-muted-foreground">
            A ficha de {passo.pessoa.name} continua no cadastro, sem ligação com esta reserva.{" "}
            <Link
              href={`/app/contatos/${passo.pessoa.contact_id}`}
              className="text-primary underline-offset-4 hover:underline"
            >
              Abrir a ficha
            </Link>
          </p>
        </div>
      ) : null}
    </ModalShell>
  );
}

// ── As peças ──────────────────────────────────────────────────────────────

function PassoDeBusca({
  podeProcurar,
  tipo,
  numero,
  erro,
  falha: falhaDaBusca,
  aoMudarTipo,
  aoMudarNumero,
  aoProcurar,
}: {
  podeProcurar: boolean;
  tipo: TipoDeDocumento;
  numero: string;
  erro: string | null;
  falha: Falha | null;
  aoMudarTipo: (tipo: TipoDeDocumento) => void;
  aoMudarNumero: (numero: string) => void;
  aoProcurar: (evento: React.FormEvent) => void;
}) {
  if (!podeProcurar) {
    return (
      <Nota variante="atencao">
        Para acrescentar alguém é preciso achar a pessoa no cadastro de contatos, e o seu perfil não tem acesso a{" "}
        <strong>Contatos</strong>. Peça a liberação em Configurações → Perfis.
      </Nota>
    );
  }

  return (
    <form id={ID_DA_BUSCA} onSubmit={aoProcurar} className="flex flex-col gap-4">
      <p className="text-sm text-muted-foreground">
        Procure pelo documento primeiro: quem já se hospedou ou falou com a casa tem ficha, e uma pessoa é um
        cadastro só.
      </p>
      <div className="grid gap-4 sm:grid-cols-[10rem_1fr]">
        <Campo id="hospede-busca-tipo" label="Documento">
          {(p) => (
            <Select {...p} value={tipo} onChange={(e) => aoMudarTipo(e.target.value as TipoDeDocumento)}>
              {TIPOS_DE_DOCUMENTO.map((t) => (
                <option key={t} value={t}>
                  {ROTULO_DO_DOCUMENTO[t]}
                </option>
              ))}
            </Select>
          )}
        </Campo>
        <Campo id="hospede-busca-numero" label="Número" erro={erro ?? undefined}>
          {(p) => (
            <Input
              {...p}
              value={numero}
              onChange={(e) => aoMudarNumero(e.target.value)}
              inputMode={tipo === "passaporte" ? "text" : "numeric"}
              autoCapitalize={tipo === "passaporte" ? "characters" : "off"}
              // O documento não entra no histórico de preenchimento do
              // navegador — o balcão é um aparelho de muita gente.
              autoComplete="off"
              spellCheck={false}
              className="font-mono"
              placeholder={tipo === "cpf" ? "000.000.000-00" : tipo === "cnpj" ? "00.000.000/0000-00" : undefined}
            />
          )}
        </Campo>
      </div>
      {falhaDaBusca ? (
        <p
          role="alert"
          data-codigo={falhaDaBusca.code}
          className="rounded-lg bg-destructive/10 px-3 py-2 text-sm text-destructive"
        >
          {/* A busca é de contatos, não da reserva: o texto geral serve, e o da
              lista diria "a reserva não foi encontrada" num 404 da coleção. */}
          {mensagemDoErro(falhaDaBusca.code)}
        </p>
      ) : null}
    </form>
  );
}

function NaoAchou({
  tipo,
  numero,
  podeCadastrar,
  aoCadastrar,
}: {
  tipo: TipoDeDocumento;
  numero: string;
  podeCadastrar: boolean;
  aoCadastrar: () => void;
}) {
  return (
    <div className="rounded-xl border border-dashed border-border/70 bg-muted/20 px-4 py-4">
      <p className="text-sm">
        Ninguém com o {ROTULO_DO_DOCUMENTO[tipo]}{" "}
        <span className="font-mono">{formatarDocumento(tipo, numero.trim()) || numero.trim()}</span> no cadastro.
      </p>
      {podeCadastrar ? (
        <>
          <p className="mt-1 text-sm text-muted-foreground">
            Cadastre a pessoa: a ficha nasce com este documento e ela entra na lista em seguida.
          </p>
          <Button className="mt-3 max-sm:w-full" onClick={aoCadastrar}>
            <UserPlus aria-hidden="true" />
            Cadastrar pessoa
          </Button>
        </>
      ) : (
        <p className="mt-1 text-sm text-muted-foreground">
          O seu perfil não cadastra pessoas. Peça a quem tem acesso a Contatos para cadastrar e acrescente aqui
          depois, procurando pelo documento.
        </p>
      )}
    </div>
  );
}

/**
 * O documento ou o telefone já são de alguém. A recusa vira porta: quem é, e
 * um botão para seguir com **essa** ficha — ou voltar e corrigir, se é outra
 * pessoa.
 */
function CaminhoDaDuplicata({
  duplicata,
  ocupado,
  aoAcrescentar,
  aoCorrigir,
  aoProcurar,
}: {
  duplicata: DuplicataNaLista;
  ocupado: boolean;
  aoAcrescentar: (pessoa: PessoaNaLista) => void;
  aoCorrigir: () => void;
  aoProcurar: () => void;
}) {
  const chave = duplicata.campo === "phone_e164" ? "telefone" : duplicata.campo === "doc_number" ? "documento" : "cadastro";
  const pessoa = duplicata.pessoa;

  if (!duplicata.contactId) {
    // O contrato promete o `contact_id`; sem ele não há com quem seguir.
    return (
      <div role="alert" className="rounded-xl border border-primary/30 bg-accent/50 px-4 py-3.5 text-sm">
        <p>Este {chave} já pertence a um cadastro, mas não foi possível saber qual.</p>
        <Button size="sm" variant="outline" className="mt-3" onClick={aoProcurar}>
          <Search aria-hidden="true" />
          Procurar pelo documento
        </Button>
      </div>
    );
  }

  const contactId = duplicata.contactId;
  const nome = pessoa?.name ?? null;

  return (
    <div role="alert" className="rounded-xl border border-primary/30 bg-accent/50 px-4 py-3.5">
      <div className="flex items-start gap-3">
        <UserRoundCheck className="mt-0.5 size-5 shrink-0 text-primary" aria-hidden="true" />
        <div className="min-w-0 flex-1">
          <h3 className="font-display text-base leading-tight">
            {nome ? `Este ${chave} já é de ${nome}` : `Este ${chave} já está cadastrado`}
          </h3>
          {pessoa?.doc_number ? (
            <p className="mt-0.5 font-mono text-xs text-muted-foreground">
              {pessoa.doc_type ? `${ROTULO_DO_DOCUMENTO[pessoa.doc_type]} ` : ""}
              {pessoa.doc_number}
            </p>
          ) : null}
          <p className="mt-1 text-sm text-muted-foreground">
            Nada foi cadastrado. Se é a mesma pessoa, acrescente a ficha que já existe — uma pessoa, um cadastro.
            Se não é (um telefone de família, por exemplo), volte e corrija o {chave}.
          </p>
          <div className="mt-3 flex flex-wrap items-center gap-2">
            <Button
              size="sm"
              className="max-sm:h-10 max-sm:w-full max-sm:text-sm"
              disabled={ocupado}
              onClick={() => aoAcrescentar({ contact_id: contactId, name: nome ?? "A pessoa" })}
            >
              <UserPlus aria-hidden="true" />
              {nome ? `Acrescentar ${primeiroNome(nome)}` : "Acrescentar essa pessoa"}
            </Button>
            <Button size="sm" variant="ghost" className="max-sm:h-10 max-sm:w-full" onClick={aoCorrigir} disabled={ocupado}>
              Não é essa pessoa — corrigir o {chave}
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}

/**
 * A recusa de pôr alguém na lista, com o caminho que ela tiver. Fica no
 * diálogo, não num toast: o operador precisa ler, decidir e tocar.
 */
function AvisoDaTentativa({
  tentativa,
  ocupado,
  aoAumentar,
  aoTentarDeNovo,
}: {
  tentativa: Tentativa;
  ocupado: boolean;
  aoAumentar: (novoNumero: number, pessoa: PessoaNaLista) => void;
  aoTentarDeNovo: (pessoa: PessoaNaLista) => void;
}) {
  const { falha: recusa, gesto, pessoa, contexto } = tentativa;
  const lista = recusa.code === "GUEST_LIST_FULL" ? lerListaCheia(recusa.details) : null;
  const aumento = lista && cabeAumentar(lista) && pessoa ? numeroParaMaisUm(lista.listed) : null;
  const transitoria = TRANSITORIAS.includes(recusa.code);
  const informativa = INFORMATIVAS.includes(recusa.code);

  return (
    <div
      role="alert"
      data-codigo={recusa.code}
      title={`Código para o suporte: ${recusa.code}`}
      className={cn(
        "flex flex-col gap-2 rounded-lg border px-3 py-2.5 text-sm",
        informativa ? "border-alcada-atencao/40 bg-alcada-atencao/10" : "border-destructive/30 bg-destructive/8",
      )}
    >
      {contexto ? <p className="font-medium">{contexto}</p> : null}
      <p>{mensagemDaLista(recusa, gesto, pessoa?.name)}</p>
      {aumento !== null && pessoa ? (
        <div>
          <Button
            size="sm"
            className="max-sm:h-10 max-sm:w-full max-sm:text-sm"
            disabled={ocupado}
            onClick={() => aoAumentar(aumento, pessoa)}
          >
            {ocupado ? <Loader2 className="animate-spin" aria-hidden="true" /> : <UserPlus aria-hidden="true" />}
            Aumentar para {aumento} hóspedes e acrescentar {primeiroNome(pessoa.name)}
          </Button>
          <p className="mt-1.5 text-xs text-muted-foreground">
            O valor da reserva não muda: muda quantas pessoas ela acomoda.
          </p>
        </div>
      ) : null}
      {transitoria && pessoa ? (
        <div>
          <Button size="sm" variant="outline" disabled={ocupado} onClick={() => aoTentarDeNovo(pessoa)}>
            Tentar de novo
          </Button>
        </div>
      ) : null}
    </div>
  );
}
