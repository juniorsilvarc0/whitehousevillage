import { AvisoDeErro } from "@/components/contatos/avisos";
import { FichaDoContato } from "@/components/contatos/ficha";
import { HistoricoDoContato } from "@/components/contatos/historico";
import { SemAcesso } from "@/components/layout/estados";
import { CabecalhoDeTela, Nota, Secao, Tela } from "@/components/layout/tela";
import { can } from "@/lib/auth/permissions";
import { requireSession } from "@/lib/auth/session";
import { chamarContatos, paginarContatos } from "@/lib/contatos/api";
import type { ContatoCompleto } from "@/lib/contatos/tipos";
import type { Reserva } from "@/lib/reservas/tipos";

export const metadata = { title: "Contato" };

/**
 * A ficha: **quem é essa pessoa**.
 *
 * ## Abrir esta tela deixa rastro, e é de propósito
 *
 * `GET /contacts/{id}` grava `pii_access_log` (ator, contato, instante) — é a
 * diferença entre esta rota e a lista. A lista serve à operação do dia; a ficha
 * é leitura de dado pessoal identificável (documento, nascimento), e a LGPD pede
 * que ela deixe registro de quem olhou. Por isso o cadastro completo **não** é
 * embutido no card do CRM: lá vai o `ContatoResumo`, que não registra.
 *
 * ## As duas buscas falham em separado
 *
 * A ficha e o histórico saem em paralelo e uma não derruba a outra. Se a lista
 * de reservas falhar, ainda há cadastro; se o cadastro falhar, não há tela — e é
 * o único caso em que a página inteira vira erro, porque não existe "ficha de
 * ninguém".
 */
export default async function ContatoPage({ params }: { params: Promise<{ id: string }> }) {
  const { permissions } = await requireSession();
  if (!can(permissions, "contacts", "ver")) return <SemAcesso recurso="contacts" />;

  const { id } = await params;

  const [contato, reservas] = await Promise.all([
    chamarContatos<ContatoCompleto>(`/contacts/${id}`),
    paginarContatos<Reserva>("/reservations", {
      contact_id: id,
      per_page: 100,
      sort: "-check_in",
    }),
  ]);

  if (!contato.ok) {
    return (
      <Tela>
        <CabecalhoDeTela voltar={{ href: "/app/contatos", rotulo: "Contatos" }} titulo="Contato" />
        <AvisoDeErro
          code={contato.code}
          titulo="Não foi possível abrir a ficha"
          detalhe={
            contato.code === "NOT_FOUND" ? (
              <>Este contato pode ter sido removido, ou esta parte do sistema ainda não está disponível.</>
            ) : null
          }
        />
      </Tela>
    );
  }

  const anonimizado = Boolean(contato.data.anonymized_at);

  return (
    <Tela>
      <CabecalhoDeTela
        voltar={{ href: "/app/contatos", rotulo: "Contatos" }}
        titulo={contato.data.name}
        descricao={
          anonimizado ? (
            <>
              Os dados pessoais desta ficha foram <strong>apagados a pedido da pessoa</strong>. A
              ficha continua existindo porque as reservas abaixo dependem dela — apagar os dados
              pessoais não apaga o histórico de vendas.
            </>
          ) : undefined
        }
      />

      <FichaDoContato
        contato={contato.data}
        permissoes={{
          editar: can(permissions, "contacts", "editar"),
          excluir: can(permissions, "contacts", "excluir"),
        }}
      />

      <Secao
        titulo="Estadias"
        descricao="O que essa pessoa já é para a casa — quantas vezes veio, quantas noites, quanto deixou."
      >
        {reservas.ok ? (
          <HistoricoDoContato reservas={reservas.data.data} />
        ) : (
          <AvisoDeErro code={reservas.code} titulo="Não foi possível carregar as estadias" />
        )}
      </Secao>

      <Nota>
        Abrir esta ficha fica registrado: o sistema guarda quem olhou os dados pessoais e quando, como
        pede a LGPD.
      </Nota>
    </Tela>
  );
}
