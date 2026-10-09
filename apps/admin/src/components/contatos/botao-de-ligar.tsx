"use client";

import * as React from "react";
import { Loader2, Phone, PhoneOff } from "lucide-react";

import { Button } from "@/components/ui/button";
import type { FalhaDeContato } from "@/lib/contatos/api";
import { discar, linkDeLigacao } from "@/lib/contatos/ligacao";
import { formatarTelefone } from "@/lib/contatos/telefone";
import { cn } from "@/lib/utils";

import { lerFichaDoContato } from "@/app/(app)/app/contatos/acoes";
import { notificar } from "@/components/contatos/avisos";

type Estado =
  | { fase: "ocioso" }
  | { fase: "buscando" }
  | { fase: "discou"; telefone: string }
  | { fase: "sem-telefone" };

/**
 * Ligar para quem a coleção só mostra mascarado — **pela ficha**.
 *
 * O lead (e a linha da lista de contatos) traz o telefone como `+*********0000`:
 * a API mascara toda coleção que embute contato, e mascara o lead até no
 * detalhe, porque o telefone é do contato e a leitura dele mora num lugar só.
 * Até a D11 o card do lead fazia `tel:` com essa máscara, e o botão discava
 * asteriscos.
 *
 * O caminho que o contrato prevê é este: o clique lê a ficha
 * (`GET /contacts/{id}`, que grava `pii_access_log`) e disca o número **dela**.
 * A leitura acontece no gesto, para uma pessoa — nunca para desenhar a lista,
 * que seriam 25 leituras de dado pessoal sem ninguém ter pedido nenhuma.
 *
 * Depois de discar, o número fica à vista como link `tel:` comum: se o
 * navegador não abriu o discador (alguns só aceitam o toque direto num link),
 * o operador toca nele sem uma segunda leitura.
 *
 * **Quem decide se o botão existe é quem o desenha**: o lead sem telefone (a
 * máscara de `null` é `null`) não o recebe. Se a ficha, lida agora, não tiver
 * mais telefone, o botão vira o aviso — não um `tel:` vazio.
 */
export function BotaoDeLigar({
  contatoId,
  nome,
  className,
}: {
  contatoId: string;
  nome: string;
  className?: string;
}) {
  const [estado, setEstado] = React.useState<Estado>({ fase: "ocioso" });

  async function ligar() {
    setEstado({ fase: "buscando" });

    let resultado: Awaited<ReturnType<typeof lerFichaDoContato>>;
    try {
      resultado = await lerFichaDoContato(contatoId);
    } catch {
      const falha: FalhaDeContato = { ok: false, code: "NETWORK_ERROR", message: "", details: {} };
      resultado = falha;
    }

    if (!resultado.ok) {
      notificar(resultado);
      setEstado({ fase: "ocioso" });
      return;
    }

    const telefone = resultado.data.phone_e164;
    if (!telefone || !linkDeLigacao(telefone)) {
      setEstado({ fase: "sem-telefone" });
      return;
    }

    setEstado({ fase: "discou", telefone });
    discar(telefone);
  }

  if (estado.fase === "discou") {
    return (
      <a
        href={linkDeLigacao(estado.telefone) ?? undefined}
        className={cn("inline-flex items-center gap-1.5 font-mono text-xs hover:text-foreground", className)}
      >
        <Phone className="size-3.5" aria-hidden="true" />
        {formatarTelefone(estado.telefone)}
      </a>
    );
  }

  if (estado.fase === "sem-telefone") {
    return (
      <span className={cn("inline-flex items-center gap-1.5 text-xs text-muted-foreground", className)}>
        <PhoneOff className="size-3.5" aria-hidden="true" />
        Sem telefone na ficha
      </span>
    );
  }

  const buscando = estado.fase === "buscando";
  return (
    <Button
      size="sm"
      variant="outline"
      onClick={ligar}
      disabled={buscando}
      aria-label={`Ligar para ${nome}`}
      title="Abre a ficha do contato para pegar o número — a leitura fica registrada, como pede a LGPD."
      className={className}
    >
      {buscando ? <Loader2 className="animate-spin" aria-hidden="true" /> : <Phone aria-hidden="true" />}
      Ligar
    </Button>
  );
}
