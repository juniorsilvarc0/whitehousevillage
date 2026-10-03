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
        descricao="As regras de venda e de cancelamento. Cada reserva segue as regras que valiam no dia em que foi feita."
      />

      <Nota variante="atencao">
        <strong>Salvar cria uma versão nova e não altera nenhuma reserva já feita.</strong> Não existe
        &ldquo;editar a política atual&rdquo;: cada mudança vira uma versão nova, com data para começar a
        valer, e não pode valer para trás. Para desfazer, publique de novo a regra antiga — fica
        registrado quem mudou o quê.
      </Nota>

      <Secao
        titulo="Política comercial"
        descricao="Sinal para confirmar, vencimento do saldo, prazo da pré-reserva, caução de evento e os limites de desconto."
      >
        {comercial.ok || comercial.code === "NOT_FOUND" ? (
          <FormularioComercial politica={comercial.ok ? comercial.data : null} podeEditar={podeEditar} />
        ) : (
          <EstadoDeErro
            code={comercial.code}
            titulo="Não foi possível carregar a política comercial atual"
            detalhe="Por segurança, não é possível publicar mudanças sem ver primeiro as regras atuais. Tente recarregar a página."
          />
        )}
      </Secao>

      <Secao
        titulo="Política de cancelamento"
        descricao="Quanto se devolve conforme a antecedência do cancelamento. Vale a primeira faixa que servir, de cima para baixo."
      >
        {cancelamento.ok || cancelamento.code === "NOT_FOUND" ? (
          <FormularioDeCancelamento
            politica={cancelamento.ok ? cancelamento.data : null}
            podeEditar={podeEditar}
          />
        ) : (
          <EstadoDeErro
            code={cancelamento.code}
            titulo="Não foi possível carregar a política de cancelamento atual"
          />
        )}
      </Secao>
    </Tela>
  );
}
