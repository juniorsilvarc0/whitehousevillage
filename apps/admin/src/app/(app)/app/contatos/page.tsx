import { Suspense } from "react";

import { AvisoDeErro } from "@/components/contatos/avisos";
import { ListaDeContatos } from "@/components/contatos/lista";
import { SemAcesso } from "@/components/layout/estados";
import { CabecalhoDeTela, Nota, Tela } from "@/components/layout/tela";
import { can } from "@/lib/auth/permissions";
import { requireSession } from "@/lib/auth/session";
import { paginarContatos } from "@/lib/contatos/api";
import { consultaDeBusca } from "@/lib/contatos/busca";
import type { Contato } from "@/lib/contatos/tipos";

import { BarraDeContatos } from "./barra";

export const metadata = { title: "Contatos" };

type Filtros = {
  busca?: string;
  marketing_opt_in?: string;
  include_anonymized?: string;
  page?: string;
};

const POR_PAGINA = 25;

/**
 * Contatos — **quem é essa pessoa**.
 *
 * Até esta rodada essa pergunta só tinha resposta por SQL: a tabela existia
 * desde a Fase 1, `ReservaCriar` exigia `contact_id`, e não havia tela nenhuma
 * — nem rota de API, medido em 27/08/2026 (`GET /api/v1/contacts` → 404). O
 * cadastro é a base de tudo que tem gente: lead, hóspede, corretor e
 * proprietário apontam todos para cá, **um registro por ser humano** (spec §6).
 *
 * ## O escopo `own` não se aplica aqui, e é decisão do catálogo
 *
 * `contacts.supports_own = false` no seed: a agenda é da casa, não do corretor.
 * O que é `own` é o negócio (lead, oportunidade, comissão), não a pessoa — se
 * cada corretor visse só os "seus" contatos, o segundo a atender a mesma família
 * abriria o segundo cadastro, e a deduplicação por telefone perderia o sentido.
 */
export default async function ContatosPage({ searchParams }: { searchParams: Promise<Filtros> }) {
  const { permissions } = await requireSession();
  if (!can(permissions, "contacts", "ver")) return <SemAcesso recurso="contacts" />;

  const filtros = await searchParams;
  const pagina = Number.parseInt(filtros.page ?? "1", 10);
  const termo = filtros.busca ?? "";

  const contatos = await paginarContatos<Contato>("/contacts", {
    // Uma caixa, três filtros: o formato do termo escolhe entre `phone`
    // (igualdade E.164), `doc_number` (igualdade) e `q` (trigram por nome).
    ...consultaDeBusca(termo),
    marketing_opt_in: filtros.marketing_opt_in || undefined,
    include_anonymized: filtros.include_anonymized === "true" ? "true" : undefined,
    page: Number.isFinite(pagina) && pagina > 0 ? pagina : 1,
    per_page: POR_PAGINA,
    sort: "name",
  });

  const permissoes = {
    criar: can(permissions, "contacts", "criar"),
    editar: can(permissions, "contacts", "editar"),
  };

  return (
    <Tela>
      <CabecalhoDeTela
        titulo="Contatos"
        descricao={
          <>
            <strong>Uma pessoa, um registro.</strong> É esta ficha que a reserva exige, que o lead
            aponta e que o WhatsApp usa para reconhecer quem já escreveu — a deduplicação é pelo
            telefone em E.164, garantida por índice único no banco, nunca por uma busca antes de
            gravar.
          </>
        }
      />

      <Suspense fallback={<div className="h-16" />}>
        <BarraDeContatos />
      </Suspense>

      {contatos.ok ? (
        <>
          <ListaDeContatos
            contatos={contatos.data.data}
            permissoes={permissoes}
            temFiltro={termo !== ""}
          />

          {contatos.data.meta.total_pages > 1 ? (
            <p className="text-xs text-muted-foreground">
              Página {contatos.data.meta.page} de {contatos.data.meta.total_pages} ·{" "}
              <span className="tabular-nums">{contatos.data.meta.total}</span> no recorte.
            </p>
          ) : null}

          <Nota>
            A ficha completa (documento, nascimento) e a exportação registram quem olhou em
            <code className="mx-1 font-mono text-xs">pii_access_log</code> — é a lista que serve à
            operação do dia, e a ficha que é leitura de dado pessoal identificável.
          </Nota>
        </>
      ) : (
        <AvisoDeErro
          code={contatos.code}
          titulo="Não foi possível carregar os contatos"
          detalhe={
            contatos.code === "NOT_FOUND" ? (
              <>
                A rota <code className="font-mono text-xs">/contacts</code> ainda não está registrada
                na API — o módulo está sendo escrito nesta mesma rodada. A tela já fala o contrato
                inteiro e passa a funcionar assim que ele subir.
              </>
            ) : null
          }
        />
      )}
    </Tela>
  );
}
