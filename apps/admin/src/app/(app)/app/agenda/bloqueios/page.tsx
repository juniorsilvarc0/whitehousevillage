import { ListaDeBloqueios } from "@/components/agenda/lista-de-bloqueios";
import { EstadoDeErro, SemAcesso } from "@/components/layout/estados";
import { CabecalhoDeTela, Nota, Tela } from "@/components/layout/tela";
import { carregarBloqueios, carregarHorarios } from "@/lib/agenda/api";
import { CAMINHO_DA_AGENDA } from "@/lib/agenda/mensagens";
import { permissoesDaAgenda } from "@/lib/agenda/permissoes";
import { requireSession } from "@/lib/auth/session";
import { somarDias } from "@/lib/datas";

export const metadata = { title: "Bloqueios de horário" };

/** A janela que a lista cobre: o teto do contrato (92 dias) a partir de hoje. */
const DIAS = 92;

/**
 * Os bloqueios de horário da operação (`/agenda/blocks`) — "equipe em
 * treinamento das 14:00 às 16:00", "operação fechada no dia da dedetização".
 *
 * Os próximos 92 dias a partir do **hoje da casa** (`settings.today`). Lidos
 * com `agenda:ver` em qualquer escopo; criar e remover, só em `all`.
 */
export default async function BloqueiosDeHorarioPage() {
  const { permissions } = await requireSession();
  const permissoes = permissoesDaAgenda(permissions);
  if (!permissoes.ver) return <SemAcesso recurso="agenda" />;

  const voltar = { href: CAMINHO_DA_AGENDA, rotulo: "Agenda" };
  const horarios = await carregarHorarios();
  if (!horarios.ok) {
    return (
      <Tela>
        <CabecalhoDeTela voltar={voltar} titulo="Bloqueios de horário" />
        <EstadoDeErro code={horarios.code} titulo="Não foi possível saber que dia é hoje na casa" />
      </Tela>
    );
  }

  const hoje = horarios.data.today;
  const bloqueios = await carregarBloqueios({ from: hoje, to: somarDias(hoje, DIAS) });

  return (
    <Tela>
      <CabecalhoDeTela
        voltar={voltar}
        titulo="Bloqueios de horário"
        descricao="Horários em que a operação não atende. Não tiram nenhuma unidade da venda e não recusam nada: quem marcar algo em cima deles é avisado."
      />
      {!permissoes.bloquearHorario && permissoes.criar ? (
        <Nota>
          O seu perfil marca os próprios compromissos; o bloqueio de horário vale para a operação inteira e é marcado pela
          gestão.
        </Nota>
      ) : null}
      {!bloqueios.ok ? (
        <EstadoDeErro
          code={bloqueios.code}
          titulo="Não foi possível carregar os bloqueios"
          detalhe={bloqueios.code === "NOT_FOUND" ? "Esta parte do sistema pode ainda não estar no ar." : undefined}
        />
      ) : (
        <>
          {bloqueios.data.incompleta ? (
            <Nota variante="atencao">Há mais bloqueios do que a tela carrega de uma vez; alguns podem não aparecer.</Nota>
          ) : null}
          <p className="text-xs text-muted-foreground">Os próximos {DIAS} dias.</p>
          <ListaDeBloqueios
            bloqueios={bloqueios.data.bloqueios}
            hoje={hoje}
            podeCriar={permissoes.bloquearHorario}
            podeRemover={permissoes.removerBloqueio}
          />
        </>
      )}
    </Tela>
  );
}
