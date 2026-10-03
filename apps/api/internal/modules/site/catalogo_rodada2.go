package site

// Rodada 2 do catálogo (docs/site-cms.md §4b, 03/10/2026): "tudo no site" —
// menu, botões, legendas, avisos do calendário, orçamento, formulário de
// pré-reserva e mensagens prontas do WhatsApp. 118 chaves.
//
// Gerado a partir das tabelas do §4b e revisado à mão: rótulo, tipo, max,
// original e ajuda são os do contrato, sem retoque. Marcadores como {horas}
// são texto comum para a API (o site os troca pelos números da política).
//
// camposDaRodada2 são os campos novos de seções que já existiam; entram no FIM
// da seção (campo novo nunca muda a posição dos antigos). secoesDaRodada2 são
// as seções novas, posicionadas por montarCatalogo.

var camposDaRodada2 = map[string][]Campo{
	"marca": {
		r2("marca.nome", "Nome no topo (linha grande)", TipoTexto, 40, "WHITE HOUSE", "O nome ao lado do logotipo, no topo de todas as páginas."),
		r2("marca.subtitulo", "Nome no topo (linha pequena)", TipoTexto, 40, "Village", "A palavra menor embaixo do nome."),
		imagem("marca.icone", "Ícone da aba do navegador", "A figurinha que aparece na aba do navegador. Prefira imagem quadrada.", "/assets/logo.png", ""),
	},
	"inicio": {
		r2("inicio.botao-disponibilidade", "Botão principal da capa", TipoTexto, 40, "Ver disponibilidade", ""),
		r2("inicio.botao-acomodacoes", "Segundo botão da capa", TipoTexto, 40, "Acomodações", ""),
		r2("inicio.rolar", "Convite para rolar a página", TipoTexto, 40, "Role para descobrir", "A frase pequena no pé da capa."),
	},
	"casa": {
		r2("casa.foto-legenda", "Legenda do fundo desenhado", TipoTexto, 80, "Pôr do sol · Praia do Coqueiro", "Só aparece enquanto não houver foto."),
	},
	"acomodacoes": {
		r2("acomodacoes.botao", "Botão ao lado do título", TipoTexto, 60, "Ver calendário e tarifas →", ""),
		r2("acomodacoes.botao-card", "Botão de cada card", TipoTexto, 40, "Ver disponibilidade", ""),
		r2("acomodacoes.preco-sufixo", "Texto ao lado do preço", TipoTexto, 60, "/ diária · a partir de", "O preço vem da tabela de tarifas."),
		r2("acomodacoes.sob-consulta", "Preço sob consulta", TipoTexto, 40, "Sob consulta", "No lugar do preço, quando a acomodação não tem preço de tabela."),
		r2("acomodacoes.sob-consulta-nota", "Nota do preço sob consulta", TipoTexto, 60, "fale com a gente", ""),
		r2("acomodacoes.opcoes", "Quantidade de opções", TipoTexto, 40, "{n} opções", "{n} vira o número de unidades da categoria."),
		r2("acomodacoes.falha-titulo", "Aviso quando as acomodações não carregam: título", TipoTexto, 60, "Acomodações", ""),
		r2("acomodacoes.falha-texto", "Aviso quando as acomodações não carregam: texto", TipoTextoLongo, 400, "Não conseguimos carregar as acomodações agora. Consulte datas e valores na central de reservas.", ""),
	},
	"local": {
		r2("local.foto-legenda", "Legenda do fundo desenhado", TipoTexto, 80, "Área da piscina", "Só aparece enquanto não houver foto."),
	},
	"chamada": {
		r2("chamada.botao-datas", "Botão principal", TipoTexto, 40, "Consultar datas", ""),
		r2("chamada.botao-whatsapp", "Botão de WhatsApp", TipoTexto, 40, "WhatsApp", ""),
	},
	"rodape": {
		r2("rodape.titulo-navegue", "Título da coluna de links", TipoTexto, 40, "Navegue", ""),
		r2("rodape.titulo-reservas", "Título da coluna de contato", TipoTexto, 40, "Reservas", ""),
		r2("rodape.titulo-endereco", "Título da coluna de endereço", TipoTexto, 40, "Endereço", ""),
		r2("rodape.whatsapp", "Texto do link de WhatsApp", TipoTexto, 60, "WhatsApp oficial", "O número continua sendo configurado à parte."),
	},
	"disp": {
		r2("disp.campo-produto", "Nome do campo de acomodação", TipoTexto, 40, "Produto", ""),
		r2("disp.campo-hospedes", "Nome do campo de hóspedes", TipoTexto, 40, "Hóspedes", ""),
		r2("disp.legenda-livre", "Legenda: livre", TipoTexto, 40, "Livre", ""),
		r2("disp.legenda-indisponivel", "Legenda: indisponível", TipoTexto, 40, "Indisponível", ""),
		r2("disp.legenda-consulta", "Legenda: sob consulta", TipoTexto, 40, "Sob consulta", ""),
		r2("disp.legenda-especial", "Legenda: data especial", TipoTexto, 40, "Data especial", ""),
		r2("disp.tarifas.texto", "Texto da tabela de tarifas", TipoTexto, 200, "Valores por diária, por produto, da tabela vigente.", ""),
		r2("disp.tarifas.minimos", "Aviso de estadia mínima", TipoTextoLongo, 400, "A estadia mínima varia por acomodação e tipo de data — passe o mouse sobre o dia no calendário para ver a de cada data.", "Aparece depois do texto da tabela."),
		r2("disp.tabela-produto", "Tabela: coluna do nome", TipoTexto, 40, "Produto", "As colunas dos tipos de data (Normal, Feriado…) vêm do sistema de tarifas."),
		r2("disp.tabela-capacidade", "Tabela: coluna da lotação", TipoTexto, 40, "Capacidade", ""),
		r2("disp.tabela-consulta", "Tabela: preço sob consulta", TipoTexto, 40, "consulta", "Aparece na célula sem preço de tabela."),
		r2("disp.tabela-pacotes", "Tabela: começo da linha de pacotes", TipoTexto, 40, "Pacotes:", ""),
		r2("disp.chamada.texto", "Texto da chamada final", TipoTextoLongo, 600, "A pré-reserva bloqueia o calendário por {horas} horas. A confirmação acontece com o sinal de {sinal}%; o saldo vence {dias} dias antes do check-in.", "{horas}, {sinal} e {dias} viram os números da política em vigor. Se o sistema não responder, o site mostra uma frase sem números."),
		r2("disp.chamada.botao-inicio", "Botão \"voltar\" da chamada final", TipoTexto, 40, "Voltar ao início", ""),
	},
	"seo": {
		r2("seo.erro.titulo", "Título da página não encontrada", TipoTexto, 200, "Página não encontrada | White House · Praia do Coqueiro", "Aparece na aba do navegador."),
	},
	"erro": {
		r2("erro.rotulo", "Rótulo acima do título", TipoTexto, 40, "Erro 404", ""),
		r2("erro.botao-disponibilidade", "Botão principal", TipoTexto, 40, "Ver disponibilidade", ""),
		r2("erro.botao-inicio", "Botão \"voltar\"", TipoTexto, 40, "Voltar ao início", ""),
	},
}

var secoesDaRodada2 = map[string]Secao{
	"menu": {Chave: "menu", Rotulo: "Menu e botões", Campos: []Campo{
		r2("menu.inicio", "Menu: Início", TipoTexto, 30, "Início", "Item do menu no topo e no menu do celular."),
		r2("menu.acomodacoes", "Menu: Acomodações", TipoTexto, 30, "Acomodações", "Item do menu no topo, no celular e no rodapé."),
		r2("menu.eventos", "Menu: Eventos", TipoTexto, 30, "Eventos", "Item do menu no topo, no celular e no rodapé."),
		r2("menu.estrutura", "Menu: Estrutura", TipoTexto, 30, "Estrutura", "Item do menu no topo, no celular e no rodapé."),
		r2("menu.disponibilidade", "Menu: Disponibilidade", TipoTexto, 30, "Disponibilidade", "Item do menu no topo, no celular e no rodapé."),
		r2("menu.reservar", "Botão do topo (início)", TipoTexto, 30, "Reservar", "O botão dourado no canto do topo, na página inicial e na página não encontrada."),
		r2("menu.falar-com-reservas", "Botão de WhatsApp (disponibilidade)", TipoTexto, 40, "Falar com reservas", "O botão do topo e o da chamada final na página de disponibilidade."),
		r2("menu.whatsapp-flutuante", "Botão flutuante de WhatsApp", TipoTexto, 40, "💬 Falar com reservas", "O botão verde que fica no canto da tela na página inicial."),
		r2("menu.pular", "Atalho \"pular para o conteúdo\"", TipoTexto, 60, "Pular para o conteúdo", "Só aparece para quem navega pelo teclado ou usa leitor de tela."),
	}},
	"calendario": {Chave: "calendario", Rotulo: "Calendário de disponibilidade", Campos: []Campo{
		r2("calendario.carregando", "Enquanto o calendário carrega", TipoTexto, 80, "consultando a central…", ""),
		r2("calendario.noite-livre", "Resumo do mês (uma noite)", TipoTexto, 120, "{n} noite livre para {acomodacao}", "{n} vira o número de noites; {acomodacao}, o nome escolhido."),
		r2("calendario.noites-livres", "Resumo do mês (várias noites)", TipoTexto, 120, "{n} noites livres para {acomodacao}", "{n} vira o número de noites; {acomodacao}, o nome escolhido."),
		r2("calendario.falha", "Aviso quando o calendário não carrega", TipoTextoLongo, 400, "A central de reservas está indisponível agora. Tente em instantes ou fale com a gente pelo WhatsApp.", ""),
		r2("calendario.falha-mes", "Aviso quando um mês não carrega", TipoTexto, 120, "Não foi possível carregar o calendário.", ""),
		r2("calendario.botao-whatsapp", "Botão de WhatsApp nos avisos", TipoTexto, 40, "Falar no WhatsApp", ""),
		r2("calendario.dia-minimo", "Dica do dia: estadia mínima", TipoTexto, 60, "mínimo {noites} noites", "Aparece ao passar o mouse no dia. {noites} vira o mínimo da tabela."),
		r2("calendario.dia-consulta", "Dica do dia: sob consulta", TipoTexto, 40, "sob consulta", ""),
		r2("calendario.dia-indisponivel", "Dica do dia: indisponível", TipoTexto, 40, "Indisponível", ""),
		r2("calendario.aviso-passou", "Aviso: data que já passou", TipoTexto, 120, "Data já passou.", ""),
		r2("calendario.aviso-consulta", "Aviso: data sob consulta", TipoTexto, 160, "{data} é sob consulta — fale com a gente pelo WhatsApp.", "{data} vira a data clicada."),
		r2("calendario.aviso-indisponivel", "Aviso: data indisponível", TipoTexto, 120, "{data} indisponível.", "{data} vira a data clicada."),
		r2("calendario.aviso-longa", "Aviso: estadia muito longa", TipoTexto, 160, "Para estadias acima de 90 noites, fale com a gente pelo WhatsApp.", ""),
		r2("calendario.aviso-intervalo", "Aviso: datas ocupadas no meio", TipoTexto, 160, "Há datas ocupadas no intervalo. Escolha um novo check-in.", ""),
		r2("calendario.kpi-livres", "Quadro: noites livres", TipoTexto, 60, "Noites livres em {mes}", "{mes} vira o nome do mês."),
		r2("calendario.kpi-diaria", "Quadro: menor diária", TipoTexto, 60, "Diária a partir de", ""),
		r2("calendario.kpi-diaria-nota", "Quadro: nota da menor diária", TipoTexto, 60, "no mês selecionado", ""),
		r2("calendario.kpi-sinal", "Quadro: sinal", TipoTexto, 60, "Sinal para confirmar", ""),
		r2("calendario.kpi-sinal-nota", "Quadro: nota do sinal", TipoTexto, 60, "saldo até {dias} dias antes", "{dias} vira o prazo do saldo."),
		r2("calendario.kpi-pre-reserva", "Quadro: pré-reserva", TipoTexto, 60, "Pré-reserva sem pagamento", ""),
		r2("calendario.kpi-pre-reserva-nota", "Quadro: nota da pré-reserva", TipoTexto, 60, "a data fica segura", ""),
	}},
	"orcamento": {Chave: "orcamento", Rotulo: "Orçamento", Campos: []Campo{
		r2("orcamento.titulo", "Título do quadro", TipoTexto, 60, "Seu orçamento", ""),
		r2("orcamento.selo", "Etiqueta do quadro", TipoTexto, 40, "Tabela vigente", ""),
		r2("orcamento.detalhes", "Linha de lotação e horários", TipoTexto, 160, "até {hospedes} hóspedes · check-in 14h · check-out 11h", "{hospedes} vira a lotação da acomodação. Os horários de entrada e saída estão escritos aqui."),
		r2("orcamento.check-in", "Rótulo: entrada", TipoTexto, 30, "Check-in", "Também usado na mensagem pronta do WhatsApp."),
		r2("orcamento.check-out", "Rótulo: saída", TipoTexto, 30, "Check-out", "Também usado na mensagem pronta do WhatsApp."),
		r2("orcamento.escolha-entrada", "Instrução: escolher a entrada", TipoTexto, 160, "Selecione a data de check-in no calendário para ver o valor da estadia.", ""),
		r2("orcamento.escolha-saida", "Instrução: escolher a saída", TipoTexto, 160, "Agora selecione a data de check-out no calendário.", ""),
		r2("orcamento.calculando", "Enquanto calcula", TipoTexto, 80, "Calculando com a tabela vigente…", ""),
		r2("orcamento.sinal", "Resumo: sinal", TipoTexto, 60, "Sinal para confirmar", ""),
		r2("orcamento.saldo", "Resumo: saldo", TipoTexto, 40, "Saldo", ""),
		r2("orcamento.saldo-prazo", "Resumo: prazo do saldo", TipoTexto, 60, "até {dias} dias antes", "{dias} vira o prazo do saldo."),
		r2("orcamento.pre-reserva", "Resumo: pré-reserva", TipoTexto, 40, "Pré-reserva", ""),
		r2("orcamento.pre-reserva-prazo", "Resumo: prazo da pré-reserva", TipoTexto, 40, "segura {horas}h", "{horas} vira o prazo da pré-reserva."),
		r2("orcamento.consulta-produto", "Acomodação sob consulta", TipoTextoLongo, 300, "{acomodacao} tem valores sob consulta: cada pedido é montado com a nossa equipe.", "{acomodacao} vira o nome escolhido."),
		r2("orcamento.consulta-data", "Data sob consulta", TipoTextoLongo, 300, "{data} tem valores sob consulta para {acomodacao}.", "{data} vira a data; {acomodacao}, o nome."),
		r2("orcamento.consulta-datas", "Período sob consulta", TipoTextoLongo, 300, "Essas datas têm valores sob consulta. Fale com a gente e montamos o seu pedido.", ""),
		r2("orcamento.botao-consultar", "Botão: consultar no WhatsApp", TipoTexto, 40, "Consultar no WhatsApp", ""),
		r2("orcamento.limpeza", "Linha da taxa de limpeza", TipoTexto, 60, "Taxa de limpeza", "O valor vem da tabela."),
		r2("orcamento.total", "Rótulo do total", TipoTexto, 30, "Total", ""),
		r2("orcamento.sinal-valor", "Linha do sinal", TipoTexto, 60, "Sinal ({sinal}%) para confirmar", "{sinal} vira a porcentagem do sinal."),
		r2("orcamento.saldo-valor", "Linha do saldo", TipoTexto, 60, "Saldo até {dias} dias antes", "{dias} vira o prazo do saldo."),
		r2("orcamento.diaria-media", "Linha da diária média", TipoTexto, 40, "Diária média", ""),
		r2("orcamento.botao-whatsapp", "Botão: preferir o WhatsApp", TipoTexto, 40, "Prefiro falar no WhatsApp", ""),
		r2("orcamento.nota", "Nota do orçamento", TipoTextoLongo, 400, "Valores da tabela vigente. A pré-reserva segura a data por {horas}h; sem o sinal, a data é liberada automaticamente.", "{horas} vira o prazo da pré-reserva."),
	}},
	"pre-reserva": {Chave: "pre-reserva", Rotulo: "Formulário de pré-reserva", Campos: []Campo{
		r2("pre-reserva.titulo", "Título do formulário", TipoTexto, 60, "Garanta a data agora", ""),
		r2("pre-reserva.nome", "Campo: nome", TipoTexto, 40, "Nome completo", ""),
		r2("pre-reserva.whatsapp", "Campo: WhatsApp", TipoTexto, 40, "WhatsApp com DDD", ""),
		r2("pre-reserva.whatsapp-exemplo", "Exemplo dentro do campo de WhatsApp", TipoTexto, 30, "(86) 99999-9999", "O texto cinza que some quando a pessoa começa a digitar."),
		r2("pre-reserva.email", "Campo: e-mail", TipoTexto, 40, "E-mail", ""),
		r2("pre-reserva.opcional", "Aviso de campo opcional", TipoTexto, 30, "(opcional)", ""),
		r2("pre-reserva.consentimento", "Autorização de uso dos dados", TipoTextoLongo, 500, "Autorizo a White House a usar meus dados para esta reserva e para falar comigo sobre ela.", "Texto com valor legal (Lei Geral de Proteção de Dados): a pessoa marca esta caixa para autorizar. Mude só com orientação jurídica; a alteração fica registrada no histórico."),
		r2("pre-reserva.consentimento-falta", "Aviso: autorização não marcada", TipoTexto, 120, "é preciso aceitar para reservar.", ""),
		r2("pre-reserva.botao", "Botão de enviar", TipoTexto, 40, "Fazer pré-reserva", ""),
		r2("pre-reserva.enviando", "Botão enquanto envia", TipoTexto, 40, "Reservando…", ""),
		r2("pre-reserva.nota", "Nota embaixo do botão", TipoTextoLongo, 300, "Sem pagamento agora. A data fica segura por {horas}h; para confirmar, paga-se o sinal de {sinal}%.", "{horas} e {sinal} viram os números da política em vigor."),
		r2("pre-reserva.erro-conflito", "Aviso: data tomada por outra pessoa", TipoTextoLongo, 300, "Essas datas acabaram de ser reservadas por outra pessoa. Escolha outras no calendário.", ""),
		r2("pre-reserva.erro-tentativas", "Aviso: tentativas demais", TipoTextoLongo, 300, "Muitas tentativas seguidas. Tente de novo mais tarde ou fale com a gente pelo WhatsApp.", ""),
		r2("pre-reserva.erro-conexao", "Aviso: sem conexão", TipoTextoLongo, 300, "Sem conexão. Tente de novo — se a primeira tentativa chegou, a mesma pré-reserva é devolvida, sem duplicar.", ""),
		r2("pre-reserva.erro-geral", "Aviso: outro problema", TipoTexto, 160, "Não foi possível concluir agora.", ""),
		r2("pre-reserva.ok-rotulo", "Confirmação: rótulo", TipoTexto, 40, "Pré-reserva feita", "Só aparece depois que a data foi de fato segurada."),
		r2("pre-reserva.ok-texto", "Confirmação: frase", TipoTexto, 200, "{acomodacao} está segura para você até {prazo}.", "{acomodacao} vira o nome; {prazo}, a data e hora limite (em negrito)."),
		r2("pre-reserva.ok-proximo", "Confirmação: próximo passo", TipoTextoLongo, 500, "Próximo passo: pagar o sinal até o prazo. Envie o código pelo WhatsApp e a nossa equipe passa os dados do pagamento. Sem o sinal, a data é liberada automaticamente.", ""),
		r2("pre-reserva.ok-botao-whatsapp", "Confirmação: botão de WhatsApp", TipoTexto, 60, "Enviar o código no WhatsApp", ""),
		r2("pre-reserva.ok-botao-nova", "Confirmação: botão de nova consulta", TipoTexto, 40, "Fazer outra consulta", ""),
	}},
	"whatsapp": {Chave: "whatsapp", Rotulo: "Mensagens prontas do WhatsApp", Campos: []Campo{
		r2("whatsapp.mensagem-consulta", "Primeira linha: pedido de valores", TipoTexto, 200, "Olá! Quero consultar valores na White House:", "A mensagem que já vem escrita quando a pessoa toca em \"Consultar no WhatsApp\". As datas e a acomodação entram embaixo."),
		r2("whatsapp.mensagem-orcamento", "Primeira linha: orçamento pronto", TipoTexto, 200, "Olá! Quero pré-reservar na White House:", "Em \"Prefiro falar no WhatsApp\". O orçamento entra embaixo."),
		r2("whatsapp.mensagem-pre-reserva", "Primeira linha: depois da pré-reserva", TipoTexto, 200, "Olá! Fiz a pré-reserva *{codigo}* no site da White House.", "{codigo} vira o código da pré-reserva. No WhatsApp, *texto* fica em negrito."),
		r2("whatsapp.pergunta-sinal", "Última linha: pergunta do sinal", TipoTexto, 200, "Como faço o pagamento do sinal?", ""),
	}},
}

// r2 é um campo de texto da rodada 2 com o max do contrato. Ajuda vazia no
// contrato fica vazia aqui.
func r2(chave, rotulo string, tipo Tipo, max int, original, ajuda string) Campo {
	return Campo{Chave: chave, Rotulo: rotulo, Tipo: tipo, Ajuda: ajuda, Max: max, Original: jsonDe(original)}
}
