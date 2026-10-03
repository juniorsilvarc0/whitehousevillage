import * as React from "react";
import Link from "next/link";
import { AlertTriangle, Inbox, Lock } from "lucide-react";

import { buttonVariants } from "@/components/ui/button";
import type { CodigoDeErro } from "@/lib/api/codigos";
import { mensagemDoErro } from "@/lib/acoes/resultado";
import { cn } from "@/lib/utils";

/**
 * Os três estados que toda tela de dados precisa ter e quase nenhuma tem.
 *
 * Tabela vazia sem explicação, erro que vira página em branco e "some sem
 * dizer nada" quando falta permissão são os três jeitos de a tela mentir sobre
 * o próprio estado. Ficam num arquivo só para que nenhuma tela precise
 * reinventá-los — e para que todas errem junto, se errarem.
 */

export function EstadoVazio({
  titulo,
  descricao,
  acao,
  icone: Icone = Inbox,
  className,
}: {
  titulo: string;
  descricao: React.ReactNode;
  acao?: React.ReactNode;
  icone?: React.ComponentType<{ className?: string }>;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "flex flex-col items-center justify-center rounded-xl border border-dashed border-border/70 bg-muted/20 px-6 py-10 text-center",
        className,
      )}
    >
      <Icone className="size-6 text-muted-foreground" aria-hidden="true" />
      <h3 className="font-display mt-3 text-base">{titulo}</h3>
      <p className="mt-1 max-w-prose text-sm text-muted-foreground">{descricao}</p>
      {acao ? <div className="mt-4">{acao}</div> : null}
    </div>
  );
}

/**
 * Falha de carregamento, com o **código** à vista.
 *
 * O código aparece na tela de propósito: nesta fase o back-end dos módulos está
 * sendo escrito em paralelo, e a diferença entre "a rota ainda não existe"
 * (`NOT_FOUND`), "o seu perfil não alcança" (`FORBIDDEN`) e "a API caiu"
 * (`NETWORK_ERROR`) é a diferença entre esperar, pedir permissão e chamar
 * alguém. Esconder isso atrás de "algo deu errado" transforma três problemas
 * distintos num único chamado inútil.
 */
export function EstadoDeErro({
  code,
  titulo = "Não foi possível carregar",
  detalhe,
  className,
}: {
  code: CodigoDeErro;
  titulo?: string;
  detalhe?: React.ReactNode;
  className?: string;
}) {
  return (
    <div
      role="alert"
      data-codigo={code}
      title={`Código para o suporte: ${code}`}
      className={cn("rounded-xl border border-destructive/30 bg-destructive/8 px-5 py-4", className)}
    >
      <div className="flex items-start gap-3">
        <AlertTriangle className="mt-0.5 size-5 shrink-0 text-destructive" aria-hidden="true" />
        <div className="min-w-0">
          <h3 className="font-display text-base text-foreground">{titulo}</h3>
          <p className="mt-1 text-sm text-muted-foreground">{mensagemDoErro(code)}</p>
          {detalhe ? <p className="mt-2 text-sm text-muted-foreground">{detalhe}</p> : null}
        </div>
      </div>
    </div>
  );
}

/**
 * Falta de permissão, dita em voz alta.
 *
 * **Esconder não é autorizar** — o guard real é o middleware da API, que
 * responde 403 de qualquer forma. Esta tela existe para o usuário saber o que
 * pedir a quem, em vez de encontrar uma página quebrada e concluir que o
 * sistema está com defeito.
 */
const NOME_DA_AREA: Record<string, string> = {
  calendar: "Mapa de ocupação",
  contacts: "Contatos",
  "crm.leads": "Leads",
  "crm.opportunities": "Funil de vendas",
  inventory: "Inventário",
  quotes: "Orçamentos",
  reservations: "Reservas",
  settings: "Configurações",
};

const NOME_DA_ACAO: Record<string, string> = {
  ver: "ver",
  criar: "criar",
  editar: "editar",
  excluir: "excluir",
};

export function SemAcesso({ recurso, acao = "ver" }: { recurso: string; acao?: string }) {
  const area = NOME_DA_AREA[recurso] ?? recurso;
  const verbo = NOME_DA_ACAO[acao] ?? acao;
  return (
    <div className="flex flex-col gap-6 p-4 sm:p-6 lg:p-8">
      <div
        data-permissao={`${recurso}:${acao}`}
        className="mx-auto max-w-prose rounded-xl border border-border/60 bg-muted/25 px-6 py-8 text-center"
      >
        <Lock className="mx-auto size-6 text-muted-foreground" aria-hidden="true" />
        <h1 className="font-display mt-3 text-xl">Esta tela não está liberada para o seu perfil</h1>
        <p className="mt-2 text-sm text-muted-foreground">
          Para usar esta tela, seu perfil precisa da permissão de <strong>{verbo}</strong> em{" "}
          <strong>{area}</strong>. Peça à gestão para liberar em Configurações → Perfis; a mudança vale
          na hora, sem precisar entrar de novo.
        </p>
        <Link href="/app" className={cn(buttonVariants({ variant: "outline" }), "mt-5")}>
          Voltar ao painel
        </Link>
      </div>
    </div>
  );
}
