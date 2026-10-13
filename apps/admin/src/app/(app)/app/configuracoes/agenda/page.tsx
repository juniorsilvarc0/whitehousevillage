import { FormularioDeHorarios } from "@/components/agenda/formulario-de-horarios";
import { EstadoDeErro, SemAcesso } from "@/components/layout/estados";
import { CabecalhoDeTela, Tela } from "@/components/layout/tela";
import { carregarHorarios } from "@/lib/agenda/api";
import { permissoesDaAgenda } from "@/lib/agenda/permissoes";
import { requireSession } from "@/lib/auth/session";

export const metadata = { title: "Horários da agenda" };

/**
 * Configurações → Agenda: check-in, check-out, janela de limpeza,
 * funcionamento por dia, passo da grade e folga.
 *
 * Lida com `agenda:ver` em qualquer escopo — é o `x-rbac` de
 * `GET /agenda/settings`: quem opera a agenda precisa saber onde começa o
 * expediente. Gravar pede `settings:editar` (`PUT /agenda/settings`), e o botão
 * só aparece para quem tem.
 */
export default async function HorariosDaAgendaPage() {
  const { permissions } = await requireSession();
  const permissoes = permissoesDaAgenda(permissions);
  if (!permissoes.ver) return <SemAcesso recurso="agenda" />;

  const horarios = await carregarHorarios();
  const voltar = { href: "/app/configuracoes", rotulo: "Configurações" };

  return (
    <Tela>
      <CabecalhoDeTela
        voltar={voltar}
        titulo="Horários da agenda"
        descricao="Os horários que a casa promete ao hóspede e o expediente da operação. Os check-ins, check-outs e limpezas de cada reserva confirmada nascem com eles."
      />
      {horarios.ok ? (
        <FormularioDeHorarios configuracao={horarios.data} podeEditar={permissoes.gravarHorarios} />
      ) : (
        <EstadoDeErro
          code={horarios.code}
          titulo="Não foi possível carregar os horários"
          detalhe={
            horarios.code === "NOT_FOUND"
              ? "Esta parte do sistema pode ainda não estar no ar."
              : "Por segurança, não é possível gravar sem ver primeiro os horários atuais. Tente recarregar a página."
          }
        />
      )}
    </Tela>
  );
}
