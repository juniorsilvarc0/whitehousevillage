"use client";

import { Printer } from "lucide-react";

import { Button } from "@/components/ui/button";

/** "Imprimir / salvar como PDF" — o PDF é o do navegador, não do servidor. */
export function BotaoImprimir() {
  return (
    <Button onClick={() => window.print()}>
      <Printer aria-hidden="true" />
      Imprimir ou salvar como PDF
    </Button>
  );
}
