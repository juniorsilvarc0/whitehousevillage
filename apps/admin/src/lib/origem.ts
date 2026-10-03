/**
 * O nome de apresentação da origem (`source`) de lead e reserva.
 *
 * O contrato declara a origem como vocabulário aberto (`whatsapp`, `site`,
 * `indicacao`, `ota`…): o que não está aqui aparece como veio, e isso é melhor
 * do que esconder uma origem nova.
 */
const ROTULO: Record<string, string> = {
  whatsapp: "WhatsApp",
  site: "site",
  indicacao: "indicação",
  ota: "site de reservas",
  telefone: "telefone",
  instagram: "Instagram",
  email: "e-mail",
  manual: "cadastro manual",
  balcao: "balcão",
};

export function rotuloDaOrigem(origem: string): string {
  return ROTULO[origem.trim().toLowerCase()] ?? origem;
}
