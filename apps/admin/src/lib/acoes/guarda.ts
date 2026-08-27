import type { Acao } from "@/lib/api/types";
import type { RecursoCodigo } from "@/lib/auth/recursos";
import { can } from "@/lib/auth/permissions";
import { getSession } from "@/lib/auth/session";
import { falha, type Falha } from "@/lib/acoes/resultado";

/**
 * Recusa cedo o que a API recusaria de qualquer jeito.
 *
 * **Isto não autoriza nada.** Server Action é endpoint público: qualquer um com
 * o id da action pode chamá-la, e é por isso que a checagem de verdade continua
 * do outro lado, no middleware da API, que responde 403 a cada requisição. O
 * que se ganha aqui é a mensagem certa sem uma ida ao servidor de dados — e um
 * registro explícito, no código da action, de qual par recurso × ação ela
 * exige, que é a mesma chave que o `x-rbac` da OpenAPI declara.
 */
export async function exigir(recurso: RecursoCodigo, acao: Acao): Promise<Falha | null> {
  const sessao = await getSession();
  if (!sessao) return falha("UNAUTHORIZED", "Sessão ausente ou expirada.");
  if (!can(sessao.permissions, recurso, acao)) {
    return falha("FORBIDDEN", "Seu perfil não alcança esta operação.", { resource: recurso, action: acao });
  }
  return null;
}
