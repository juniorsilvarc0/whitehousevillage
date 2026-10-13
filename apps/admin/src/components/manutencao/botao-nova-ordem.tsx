"use client";

import { Plus } from "lucide-react";

import { ModalDeOrdem, type AlvoDaOrdem, type UnidadeDoSeletor } from "@/components/manutencao/modal-de-ordem";
import { useControleDeModal } from "@/components/layout/controle-de-modal";
import { Button } from "@/components/ui/button";

/**
 * "Nova ordem" da lista — só renderizado com `maintenance:criar` (quem decide
 * é a página, que lê a matriz). Abre já na unidade do filtro, quando há uma.
 */
export function BotaoNovaOrdem({
  unidades,
  unidadeInicial,
  rotulo = "Nova ordem",
}: {
  unidades: readonly UnidadeDoSeletor[];
  unidadeInicial?: string;
  rotulo?: string;
}) {
  const modal = useControleDeModal<AlvoDaOrdem>();
  return (
    <>
      <Button onClick={() => modal.abrir({ unitId: unidadeInicial })}>
        <Plus aria-hidden="true" />
        {rotulo}
      </Button>
      <ModalDeOrdem controle={modal.ref} unidades={unidades} />
    </>
  );
}
