import type { Alcada } from "@/lib/api/comercial";

/**
 * Alçada de desconto — o semáforo do orçamento.
 *
 * **Quem decide é o servidor**: `Orcamento.discount_authority` vem do motor
 * `internal/domain/booking`, sobre a política vigente, e um desconto acima do
 * teto nem chega a virar orçamento (`422 DISCOUNT_ABOVE_LIMIT`). Esta função
 * existe para o instante em que o dedo ainda está no slider e não houve
 * requisição nenhuma: sem ela, o vendedor arrastaria até 40% e só descobriria o
 * "não" depois de calcular, na frente do hóspede.
 *
 * Os limites são **dado versionado** (`commercial_policies.discount_auto_pct` e
 * `discount_approval_pct`), não constantes: 5 e 10 são o que a política vigente
 * diz hoje, e a tela de política pode publicar outros amanhã sem deploy.
 */
export type LimitesDeAlcada = {
  /** Até aqui a gestão fecha sozinha. */
  auto: number;
  /** Até aqui, com aprovação do proprietário. Acima, negado. */
  aprovacao: number;
};

export function alcadaDe(pct: number, limites: LimitesDeAlcada): Alcada {
  // Percentual que não é número é defeito de digitação, e a falha segura é
  // travar a venda — nunca liberá-la por omissão.
  if (!Number.isFinite(pct)) return "negado";
  if (pct <= limites.auto) return "gestao";
  if (pct <= limites.aprovacao) return "proprietario";
  return "negado";
}

export type DescricaoDaAlcada = {
  /** Uma palavra, para a etiqueta. */
  rotulo: string;
  /** A frase que a gestão lê antes de prometer o preço. */
  explicacao: string;
  /** `false` desabilita a emissão — é o vermelho do semáforo. */
  podeFechar: boolean;
  /** Classe do ponto/etiqueta. Tokens dedicados: um semáforo precisa de três
   *  matizes distintos, e improvisar `bg-[#f59e0b]` seria cor fora do sistema. */
  classe: string;
  ponto: string;
};

export const SEMAFORO: Record<Alcada, DescricaoDaAlcada> = {
  gestao: {
    rotulo: "Pode fechar",
    explicacao: "Dentro do limite da gestão. Pode fechar agora, sem consultar ninguém.",
    podeFechar: true,
    classe: "bg-alcada-livre/12 text-alcada-livre border-alcada-livre/30",
    ponto: "bg-alcada-livre",
  },
  proprietario: {
    rotulo: "Exige aprovação do proprietário",
    explicacao: "Acima do limite da gestão. O orçamento pode ser feito, mas só vira reserva com o aval do proprietário.",
    podeFechar: true,
    classe: "bg-alcada-atencao/14 text-alcada-atencao border-alcada-atencao/35",
    ponto: "bg-alcada-atencao",
  },
  negado: {
    rotulo: "Não autorizado",
    explicacao: "Acima do desconto máximo da política. O sistema não aceita — nem com aprovação do proprietário.",
    podeFechar: false,
    classe: "bg-destructive/12 text-destructive border-destructive/30",
    ponto: "bg-destructive",
  },
};

/** As três faixas, para desenhar a régua do semáforo com os números da política. */
export function faixasDaAlcada(limites: LimitesDeAlcada): { alcada: Alcada; de: number; ate: number | null }[] {
  return [
    { alcada: "gestao", de: 0, ate: limites.auto },
    { alcada: "proprietario", de: limites.auto, ate: limites.aprovacao },
    { alcada: "negado", de: limites.aprovacao, ate: null },
  ];
}
