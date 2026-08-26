import { redirect } from "next/navigation";

/**
 * A raiz não tem conteúdo próprio: este repositório é só gestão administrativa
 * (o site público mora em outro projeto). Quem chega em `/` quer o painel — e
 * quem não tiver sessão será devolvido ao login pelo middleware de `/app`.
 */
export default function Raiz() {
  redirect("/app");
}
