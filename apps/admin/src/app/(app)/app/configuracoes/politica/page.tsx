import { EstadoDeErro, SemAcesso } from "@/components/layout/estados";
import { CabecalhoDeTela, Nota, Secao, Tela } from "@/components/layout/tela";
import type { PoliticaComercial, PoliticaDeCancelamento } from "@/lib/api/comercial";
import { carregar } from "@/lib/api/carregar";
import { can } from "@/lib/auth/permissions";
import { requireSession } from "@/lib/auth/session";

import { FormularioComercial } from "./formulario-comercial";
import { FormularioDeCancelamento } from "./formulario-cancelamento";

export const metadata = { title: "Política comercial" };

/**
 * Sinal, prazos, pré-reserva, alçadas e faixas de cancelamento.
 *
 * As duas políticas são **versionadas**, e é essa a ideia que a tela precisa
 * transmitir antes de qualquer campo: publicar não é editar. A reserva grava o
 * número da versão que valia na criação e é julgada por ela até o fim.
 */
export default async function PoliticaPage() {
  const { permissions } = await requireSession();
  if (!can(permissions, "settings", "ver")) return <SemAcesso recurso="settings" />;

  const [comercial, cancelamento] = await Promise.all([
    carregar<PoliticaComercial>("/policies/commercial"),
    carregar<PoliticaDeCancelamento>("/policies/cancellation"),
  ]);

  const podeEditar = can(permissions, "settings", "editar");

  return (
    <Tela>
      <CabecalhoDeTela
        voltar={{ href: "/app/configuracoes", rotulo: "Configurações" }}
        titulo="Política comercial e de cancelamento"
        descricao="Tudo aqui é dado versionado, não constante em código. Cada reserva congela a versão que valia quando nasceu."
      />

      <Nota variante="atencao">
        <strong>Salvar cria uma versão nova e não altera nenhuma reserva já feita.</strong> Não existe
        &ldquo;editar a política vigente&rdquo;: o servidor numera a versão nova como a seguinte e
        recusa reescrever ou antedatar uma publicada. Voltar atrás é publicar de novo a regra antiga —
        que entra como versão nova e deixa rastro de quem mudou o quê.
      </Nota>

      <Secao
        titulo="Política comercial"
        descricao="Sinal para confirmar, vencimento do saldo, validade da pré-reserva, caução de evento e as alçadas de desconto."
      >
        {comercial.ok || comercial.code === "NOT_FOUND" ? (
          <FormularioComercial politica={comercial.ok ? comercial.data : null} podeEditar={podeEditar} />
        ) : (
          <EstadoDeErro
            code={comercial.code}
            titulo="Não foi possível carregar a política comercial vigente"
            detalhe="Publicar sem conhecer a versão atual criaria uma versão nova em cima de valores que ninguém conferiu."
          />
        )}
      </Secao>

      <Secao
        titulo="Política de cancelamento"
        descricao="Faixas por antecedência até o check-in. O motor aplica a primeira faixa aplicável, de cima para baixo."
      >
        {cancelamento.ok || cancelamento.code === "NOT_FOUND" ? (
          <FormularioDeCancelamento
            politica={cancelamento.ok ? cancelamento.data : null}
            podeEditar={podeEditar}
          />
        ) : (
          <EstadoDeErro
            code={cancelamento.code}
            titulo="Não foi possível carregar a política de cancelamento vigente"
          />
        )}
      </Secao>
    </Tela>
  );
}
