# PRD — White House Village Manager

> Documento de produto. O "o quê" e o "porquê". O "como" está em [`spec.md`](spec.md), [`db.md`](db.md) e [`api.md`](api.md).

## 1. Contexto

A White House Village é um condomínio de alto padrão na Praia do Coqueiro (Luís Correia, PI), alugado por temporada e para eventos — casamentos, mini weddings, aniversários, chá revelação, confraternizações, retiros corporativos, ensaios e produções.

O diagnóstico comercial feito com os proprietários revelou um ativo com potencial e uma operação sem estrutura:

- controle de disponibilidade em **WhatsApp + agenda física**;
- **sem histórico organizado** de reservas ou faturamento;
- preços que "precisam de ajuste", sem tabela consolidada;
- **sem limite de negociação nem desconto máximo** definidos;
- várias regras resolvidas "dependendo da situação";
- sem canal comercial exclusivo, sem CRM, sem indicadores.

O MVP de apresentação (site + painel com dados mocados) validou a proposta com os proprietários. Este projeto constrói o sistema real.

## 2. Problema

Cada consulta de cliente hoje percorre o caminho: cliente pergunta → gestão pergunta ao proprietário → proprietário demora → gestão responde → **o cliente já reservou em outro lugar**.

Três consequências mensuráveis:

1. **Perda por lentidão** — sem tabela, alçada e disponibilidade confiável, responder um orçamento leva horas.
2. **Risco de overbooking** — duas pessoas diferentes prometendo a mesma data em canais diferentes.
3. **Cegueira gerencial** — não se sabe ocupação, ticket médio, origem do lead nem motivo de perda; sem dado, não há decisão de preço.

## 3. Objetivo

> Colocar ordem na operação comercial e transformar cada atendimento em dado, para que a gestão responda em minutos sem consultar os proprietários — e para que, em 90 dias, exista base gerencial suficiente para decidir preço e posicionamento.

## 4. Personas

| Persona | Quem é | O que precisa | Dor principal |
|---|---|---|---|
| **Proprietário** | Dono do imóvel, aprova exceções | Ver ocupação, receita e repasse; ser consultado só no que importa | Ser interrompido por cada desconto de 3% |
| **Gestão comercial** (usuário) | Quem atende, orça e fecha | Disponibilidade confiável, orçamento em segundos, alçada clara, histórico do cliente | Caçar informação em três lugares |
| **Corretor / parceiro** | Traz cliente, recebe comissão | Consultar datas, gerar orçamento, criar pré-reserva e acompanhar a própria comissão | Depender da gestão para saber se a data está livre |
| **Operação local** | Limpeza, manutenção, check-in | Saber o que entra e sai hoje, e o que preparar | Descobrir a chegada de hóspede em cima da hora |
| **Hóspede** | Cliente final | Resposta rápida, orçamento claro, confirmação sem ruído | Ficar sem resposta e procurar outro lugar |

O hóspede **não é usuário do sistema** nesta fase — ele é atendido por WhatsApp, dentro do sistema.

## 5. Jornadas que o produto precisa cobrir

### 5.1 Do WhatsApp à reserva confirmada (jornada principal)

```
Mensagem no WhatsApp
  → conversa criada, contato identificado ou criado
  → lead entra no funil
  → gestão consulta disponibilidade (segundos)
  → orçamento gerado diária a diária, com desconto dentro da alçada
  → pré-reserva de 48h trava a data no calendário
  → sinal de 50% recebido → reserva confirmada
  → recebível do saldo agendado para 7 dias antes do check-in
  → agenda operacional recebe check-in, limpeza e check-out
  → pós-estadia: avaliação e reativação
```

Nenhum passo sai do sistema. Se sair, o dado se perde — que é exatamente o problema de hoje.

### 5.2 Exclusividade da casa completa

Uma consulta de "White House Completa" para um casamento tem que ver, num só lugar, se **qualquer** das 8 unidades está ocupada — e vender a casa inteira tem que travar todas elas. Hoje isso depende da memória de quem atende.

### 5.3 Corretor trabalhando sozinho

Corretor consulta datas, gera orçamento dentro da alçada, cria pré-reserva e acompanha a comissão — sem ver o financeiro global, os leads dos colegas ou as configurações.

### 5.4 Fechamento do mês

Gestão fecha o mês: receita realizada, comissões a pagar, repasse ao proprietário, ocupação, ADR, RevPAR e motivos de perda. Hoje isso não existe.

## 6. Escopo por fase

| Fase | Escopo | Resultado para o negócio |
|---|---|---|
| **0** | Fundação técnica, autenticação, perfis | Time consegue trabalhar |
| **1** | Inventário, tarifário, política, disponibilidade, orçamento, reservas, mapa em tempo real, CRM, WhatsApp | **A operação comercial inteira sai do caderno** |
| **2** | Financeiro, comissões, agenda operacional, hóspedes/LGPD | O dinheiro fecha e a operação local enxerga o dia |
| **3** | Portal do corretor, contratos, BI e KPIs | Escala comercial e decisão por dado |
| **4** | Canais/OTA (iCal), painel de conflitos | Airbnb e Booking sem overbooking |
| **5** | Inventário operacional, webhooks, tokens, agente de IA | Automação e atendimento assistido |
| **6** | Hardening, carga, restore testado, treinamento | Go-live seguro |

## 7. Perfis de acesso

| | admin | usuario | corretor |
|---|:--:|:--:|:--:|
| Reservas e calendário | total | total | próprias |
| CRM (leads, oportunidades) | total | total | próprios |
| Chat WhatsApp | total | total | próprias conversas |
| Agenda | total | total | leitura |
| Financeiro | total | lançamento | ❌ |
| Comissões | total | total | próprias |
| Inventário | total | total | ❌ |
| Canais/OTA | total | leitura | ❌ |
| BI | total | total | próprio desempenho |
| Configurações, usuários, tarifário, política, tokens | total | ❌ | ❌ |

Implementado como **dado** (papel × recurso × ação × escopo `all|own`), nunca como `if` no código — novos perfis (proprietário, operação local) entram por configuração.

## 8. Métricas de sucesso

| Métrica | Hoje | Meta em 90 dias |
|---|---|---|
| Tempo até o primeiro orçamento | horas | **< 10 min** |
| Reservas registradas fora do sistema | 100% | **0** |
| Overbooking | risco não medido | **0 casos** |
| Consultas ao proprietário por desconto | toda negociação | **só acima da alçada** |
| Leads com origem registrada | 0% | **> 95%** |
| Perdas com motivo registrado | 0% | **> 90%** |
| Base gerencial (ocupação, ADR, RevPAR, ticket) | inexistente | **fechamento mensal automático** |

## 9. Requisitos não-funcionais

- **Corretude sobre velocidade**: disponibilidade errada custa mais caro que tela lenta. Overbooking é impossibilidade estrutural, não boa intenção.
- **Desempenho**: mapa de 90 dias × 8 unidades em < 300 ms (p95); orçamento em < 150 ms.
- **Realtime**: alteração de calendário visível em outra sessão em < 2 s.
- **Disponibilidade**: alvo 99,5%; RTO < 4 h, RPO < 24 h (backup diário verificado por restore).
- **Auditoria**: toda escrita registra ator, antes e depois; leitura de dado pessoal também é registrada.
- **LGPD**: base legal por finalidade, minimização, exportação e anonimização do titular preservando o razão fiscal.
- **Português do Brasil** em toda a interface; moeda BRL; fuso `America/Fortaleza`.
- **Mobile de verdade**: a gestão atende do celular — o painel é responsivo por primitivo, não por adaptação.

## 10. Não-objetivos (explícitos)

- Motor de reservas público com pagamento online pelo hóspede (fase futura).
- Aplicativo nativo — o painel é PWA responsivo.
- Multi-tenant comercial (SaaS para outras pousadas). O schema já carrega `property_id` para não impedir, mas o produto não persegue isso agora.
- Contabilidade fiscal completa e emissão de nota — o sistema entrega o dado para o contador.
- Substituir o site público de marketing.
- Rodar o modelo de IA internamente — o sistema é o **hub**; o agente é externo e consome a API.

## 11. Premissas e riscos de produto

- **Airbnb e Booking não são integráveis por API hoje.** Airbnb exige aprovação como Software Partner (NDA, avaliação de segurança, recursos obrigatórios em 6 meses) e a Booking exige o programa de Connectivity/Supply — a *Demand API* citada no briefing é a de quem vende inventário da Booking, não a de quem publica o próprio. Por isso a Fase 4 entrega **iCal bidirecional**, que funciona hoje, com os adapters de API prontos por trás da mesma interface.
- **Várias regras comerciais ainda estão em aberto** (composição exata da Completa, remarcação, no-show, visitantes, limites de evento, responsáveis nomeados). O sistema as trata como **configuração versionada**, não como código — decidir depois não custa reescrita, e o que não estiver decidido aparece como pendência na tela de política.
- A adoção depende de a gestão parar de usar o caderno. O produto compete com o hábito: por isso a Fase 1 prioriza velocidade de orçamento e o WhatsApp dentro do sistema.
