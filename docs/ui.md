# Design system — painel White House Village

> Porte do sistema visual do SanareVita ("Casca 3.0"), retonalizado para a marca White House. Referência de origem: `/Users/junior/DEV/SANAREVITA/sanarevita/src/app/globals.css` e `UI.md`.

## 1. Princípio

**Não existe sidebar.** O fundo é um *wash* de manchas radiais fixas; sobre ele **flutuam** uma barra superior em gradiente da marca, painéis brancos (`panel-float`) e barras de vidro (`glass`). A interface é feita de superfícies soltas sobre um fundo vivo, não de caixas encaixadas.

## 2. Marca

O símbolo é o monograma **WH** em verde-oliva com o ramo de folhas em sálvia, sobre a palavra WHITE HOUSE. A paleta sai dele: **oliva profundo + sálvia + areia**, com o branco como superfície.

## 3. Tokens (`globals.css`, Tailwind v4 CSS-first)

Sem `tailwind.config.ts`. Tudo em `@theme inline` + `:root` / `.dark`.

```css
:root {
  /* geometria — telas de altura cheia descontam TOKEN, nunca calc() escrito à mão */
  --app-bar-height: 3.5rem;
  --app-chrome-top: calc(var(--app-bar-height) + 1rem);
  --mobile-nav-height: 3.5rem;
  --radius: 1.1rem;

  /* marca: oliva (hue ~110) com apoio em sálvia (hue ~100) */
  --background: oklch(0.98 0.006 95);      /* areia clara */
  --foreground: oklch(0.24 0.018 130);
  --card:       oklch(1 0 0);
  --primary:    oklch(0.44 0.055 110);     /* oliva da logo */
  --primary-foreground: oklch(0.99 0.004 95);
  --secondary:  oklch(0.94 0.014 100);
  --muted:      oklch(0.96 0.008 100);
  --muted-foreground: oklch(0.48 0.018 120);
  --accent:     oklch(0.90 0.030 120);     /* sálvia */
  --accent-foreground: oklch(0.34 0.045 120);
  --destructive:oklch(0.55 0.19 27);
  --border:     oklch(0.90 0.010 100);
  --ring:       oklch(0.44 0.055 110);
  --chart-1: oklch(0.44 0.055 110);  --chart-2: oklch(0.62 0.055 135);
  --chart-3: oklch(0.55 0.05 95);    --chart-4: oklch(0.70 0.04 120);
  --chart-5: oklch(0.36 0.045 140);
}
```

Escala de raio derivada: `--radius-sm ×0.6`, `-md ×0.8`, `-lg ×1`, `-xl ×1.4`, `-2xl ×1.8`, `-3xl ×2.2`, `-4xl ×2.6`.

## 4. As cinco utilities de assinatura

```css
@utility bg-brand-gradient {   /* ação primária, item ativo, aba ativa */
  background-image: linear-gradient(135deg, oklch(0.36 0.05 140), oklch(0.42 0.055 120) 55%, oklch(0.47 0.06 100));
}
@utility bg-brand-bar {        /* a barra superior flutuante — o elemento de assinatura */
  background-image: linear-gradient(100deg, oklch(0.30 0.04 145), oklch(0.38 0.05 125) 34%,
                                            oklch(0.44 0.055 110) 66%, oklch(0.48 0.06 98));
}
@utility shadow-soft {         /* cartões e painéis — sombra tingida pela marca */
  box-shadow: 0 16px 40px -20px oklch(0.42 0.05 120 / 32%), 0 4px 12px -6px oklch(0.42 0.05 120 / 12%);
}
@utility panel-float {         /* a superfície branca que carrega cada tela */
  background-color: var(--card);
  border-radius: var(--radius-xl);
  box-shadow: 0 1px 0 0 oklch(1 0 0 / 60%) inset,
              0 20px 60px -28px oklch(0.40 0.05 120 / 40%),
              0 4px 16px -8px oklch(0.40 0.05 120 / 14%);
  border: 1px solid oklch(0.45 0.04 115 / 10%);
}
@utility glass {               /* barras translúcidas */
  background-color: oklch(1 0 0 / 72%);
  backdrop-filter: blur(16px) saturate(140%);
}
```

**Todos os stops dos gradientes têm `L ≤ 0.48`** — é o que garante contraste AA do texto branco por cima. Ao ajustar a paleta, essa é a regra que não pode cair.

## 5. O wash de fundo

```css
body {
  background-image:
    radial-gradient(60rem 44rem at -6% -10%, oklch(0.62 0.07 120 / 22%), transparent 60%),
    radial-gradient(52rem 40rem at 104% 4%,  oklch(0.72 0.06 100 / 20%), transparent 58%),
    radial-gradient(56rem 46rem at 88% 96%,  oklch(0.66 0.06 140 / 18%), transparent 60%),
    radial-gradient(44rem 38rem at 18% 104%, oklch(0.80 0.04 95  / 16%), transparent 58%);
  background-attachment: fixed;
}
```

## 6. Tipografia

| Uso | Fonte |
|---|---|
| Corpo, tabelas, dados densos | **Geist** (`--font-sans`) |
| Títulos, navegação, números de destaque (KPI, totais) | **Outfit** (`--font-display` / `--font-heading`) |
| Código, identificadores, valores monetários em tabela | **Geist Mono** |

## 7. A casca

| Elemento | Como é |
|---|---|
| **Barra superior** | Flutuante, `bg-brand-bar`, `rounded-2xl`, `ring-1 ring-white/15`, sombra difusa. No desktop carrega a navegação inteira + busca ⌘K + perfil |
| **Barra inferior (mobile)** | `glass`, `rounded-t-3xl`, cinco destinos escolhidos a dedo — **não** um `slice` dos primeiros itens. WhatsApp fica nela de propósito |
| **Gaveta de navegação** | Tela cheia (`88dvh`), agrupada por seção — o que não cabe na barra vive aqui, não num popover apertado |
| **Painel de conteúdo** | `panel-float`, ocupa a tela, rola por dentro |
| **Command palette** | ⌘K / Ctrl+K — busca reserva, contato, oportunidade e navega |
| **Modal** | `Dialog` no desktop **vira `Drawer` no mobile**, automaticamente. `ModalShell` é a casca canônica: header fixo, corpo rolável, footer com `env(safe-area-inset-bottom)` |
| **Botões** | Pílula (`rounded-full`) em todos os tamanhos; primário em `bg-brand-gradient` |
| **Toast** | `sonner`, `top-center`, com offsets de safe-area |

Navegação declarativa em `src/config/navigation.ts`, com `allowedRoles` por item — o `corretor` enxerga quatro itens. **O guard real é no servidor**; a lista só esconde.

## 8. Padrões de tela

- **Lista de dados**: cada linha é um cartão (`rounded-xl border bg-muted/20 shadow-soft`); no mobile vira `<article>`. Toolbar canônica: busca → filtros → filtros ativos → ações.
- **Formulário**: sempre dentro de `ModalShell`, validação com zod espelhando o DTO do Go, erro por campo vindo de `error.details`.
- **Vazio**: nunca tabela vazia sem explicação — título, uma frase e a ação primária.
- **Carregando**: skeleton com a forma do conteúdo, não spinner centralizado.
- **Números**: `tabular-nums` em toda coluna de valor; dinheiro sempre alinhado à direita.

## 9. Componentes-chave

| Componente | Nota de implementação |
|---|---|
| **`OccupancyMap`** | Linhas = 8 unidades agrupadas por produto + linha sintética "White House Completa". Colunas = dias (~90, virtualizados). Célula com cor de fundo pelo tipo de data e barra por cima: `hold` tracejado com contador de expiração, `confirmed` sólido, `maintenance` hachurado, `ota` com selo do canal. Drag cria bloqueio; hover mostra a tarifa. Atualiza por SSE. **É a tela mais cara do projeto — prototipar cedo.** |
| **`QuoteBuilder`** | Noite a noite, agrupado por tipo, slider de desconto com semáforo de alçada (verde ≤5%, âmbar 6–10% "exige aprovação", vermelho >10% desabilita), limpeza fora do desconto, total e sinal |
| **`PipelineKanban`** | dnd-kit, movimento otimista com rollback, cor da etapa tinge a coluna (card fica neutro), menu "Mover para" como alternativa ao arrasto, faixa de SLA estourado no card |
| **`OpportunityPage`** | Header + trilha de etapas + faixa de SLA + abas + rail lateral (alertas, próxima ação, linha do tempo) + cards de domínio com edição inline (salva no `blur`, sem botão Salvar) |
| **`ChatShell`** | Escopo visual próprio (`.wa-surface`): o chat imita o WhatsApp, não o resto do app. Lista + thread + composer, ticks, takeover |
| **`AgendaGrid`** | Grade própria, sem biblioteca de calendário. Quatro visões; layout de colisão em colunas |
| **`RateEditor`** | Grade produto × tipo de data com edição inline e salvamento em lote |

## 10. Anti-padrões

- Sombra dentro de sombra: sub-superfície aninhada em `panel-float` **não** leva `shadow-soft`.
- Altura de tela escrita à mão em vez do token de geometria.
- `datetime-local` — usar campos separados de data e hora.
- Cor fora dos tokens (`bg-[#hex]` no meio de um componente).
- Ícone escolhido pelo genérico do setor em vez do que a tela faz.
- Tabela que estoura horizontalmente a página: conteúdo largo rola **dentro** do próprio container.
