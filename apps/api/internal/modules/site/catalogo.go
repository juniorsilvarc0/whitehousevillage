package site

import (
	"encoding/json"
	"fmt"
)

// O catálogo de campos do site (docs/site-cms.md §4) — a fonte ÚNICA do que a
// gestão pode editar. O painel desenha o formulário a partir dele, o site sabe
// onde pôr cada chave (`data-cms*`) e o banco só guarda o que foi editado.
//
// O valor original de cada campo é o texto que estava no HTML/JS do site no
// dia em que o catálogo nasceu (index.html, disponibilidade.html, 404.html e o
// CATEGORIAS de scripts/vitrine.js). Nos títulos, `<em>x</em>` virou `*x*` e
// `<br />` virou quebra de linha — o mesmo formato que a gestão digita.
//
// Chave é contrato: renomear uma quebra o site (que procura a chave antiga) e
// órfã a linha gravada no banco. Campo novo entra no fim da seção dele.

// Tipo é o tipo de um campo (docs/site-cms.md §3).
type Tipo string

const (
	TipoTexto      Tipo = "texto"
	TipoTextoLongo Tipo = "texto_longo"
	TipoTitulo     Tipo = "titulo"
	TipoImagem     Tipo = "imagem"
	TipoVideo      Tipo = "video"
	TipoLista      Tipo = "lista"
)

// Limites por tipo. `max` do campo pode apertar, nunca afrouxar.
const (
	maxTexto        = 200
	maxTextoLongo   = 2000
	maxTitulo       = 200
	maxAlt          = 200
	maxItensDeLista = 24
)

// formatoEmail marca o campo de texto que precisa ser um endereço de e-mail.
const formatoEmail = "email"

// Subcampo é uma coluna de um item de lista. Subtipos: texto, texto_longo e
// imagem.
type Subcampo struct {
	Chave  string
	Rotulo string
	Tipo   Tipo
	Max    int
}

// Campo é uma linha do catálogo.
type Campo struct {
	Chave  string
	Rotulo string
	Tipo   Tipo
	Ajuda  string
	// Max: caracteres (texto, texto_longo, titulo), itens (lista) ou
	// caracteres do texto alternativo (imagem). Vídeo: 0.
	Max     int
	Formato string
	Itens   []Subcampo
	// Original é o valor de hoje do site, já em JSON: string, lista, mídia
	// original ({"url", "alt"}) ou null (bloco de cena sem foto).
	Original json.RawMessage
}

// Secao agrupa os campos na ordem em que o painel mostra.
type Secao struct {
	Chave  string
	Rotulo string
	Campos []Campo
}

// ─────────────────────────── Construtores ───────────────────────────────────

const (
	ajudaTitulo = "Use *asteriscos* em volta da palavra que deve aparecer em destaque (itálico dourado). " +
		"Uma quebra de linha no texto vira quebra de linha no site."
	ajudaLongo = "Deixe uma linha em branco para começar um novo parágrafo."
	ajudaFoto  = "Prefira foto na horizontal, com boa luz. JPG, PNG ou WebP, até 15 MB. " +
		"O texto alternativo descreve a foto para quem não enxerga e para o Google."
	ajudaRotulo = "A palavrinha em letras pequenas acima do título."
)

func jsonDe(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		// Só acontece com valor não serializável — erro de programação no
		// próprio catálogo, que o teste de unidade pega antes do deploy.
		panic(fmt.Sprintf("site: original não serializável: %v", err))
	}
	return b
}

func texto(chave, rotulo, ajuda, original string) Campo {
	return Campo{Chave: chave, Rotulo: rotulo, Tipo: TipoTexto, Ajuda: ajuda, Max: maxTexto, Original: jsonDe(original)}
}

func titulo(chave, rotulo, original string) Campo {
	return Campo{Chave: chave, Rotulo: rotulo, Tipo: TipoTitulo, Ajuda: ajudaTitulo, Max: maxTitulo, Original: jsonDe(original)}
}

func longo(chave, rotulo, ajuda, original string) Campo {
	if ajuda == "" {
		ajuda = ajudaLongo
	}
	return Campo{Chave: chave, Rotulo: rotulo, Tipo: TipoTextoLongo, Ajuda: ajuda, Max: maxTextoLongo, Original: jsonDe(original)}
}

func rotulo(secao, original string) Campo {
	return texto(secao+".rotulo", "Rótulo da seção", ajudaRotulo, original)
}

// midiaOriginal é a mídia que o site usa hoje, servida pelo próprio nginx.
type midiaOriginal struct {
	URL string  `json:"url"`
	Alt *string `json:"alt,omitempty"`
}

// imagem com url vazia = bloco de cena sem foto hoje (original null).
func imagem(chave, rotulo, ajuda, url, alt string) Campo {
	original := json.RawMessage("null")
	if url != "" {
		original = jsonDe(midiaOriginal{URL: url, Alt: &alt})
	}
	if ajuda == "" {
		ajuda = ajudaFoto
	}
	return Campo{Chave: chave, Rotulo: rotulo, Tipo: TipoImagem, Ajuda: ajuda, Max: maxAlt, Original: original}
}

// video com url vazia = sem vídeo próprio hoje (original null), como imagem.
func video(chave, rotulo, ajuda, url string) Campo {
	original := json.RawMessage("null")
	if url != "" {
		original = jsonDe(midiaOriginal{URL: url})
	}
	return Campo{Chave: chave, Rotulo: rotulo, Tipo: TipoVideo, Ajuda: ajuda, Original: original}
}

func sub(chave, rotulo string, tipo Tipo) Subcampo {
	m := maxTexto
	switch tipo {
	case TipoTextoLongo:
		m = maxTextoLongo
	case TipoImagem:
		m = maxAlt
	}
	return Subcampo{Chave: chave, Rotulo: rotulo, Tipo: tipo, Max: m}
}

// lista monta o original a partir de linhas posicionais: a coluna i de cada
// linha é o subcampo i. Subcampo imagem com "" vira null (sem foto hoje).
func lista(chave, rotulo, ajuda string, itens []Subcampo, linhas ...[]string) Campo {
	original := make([]map[string]json.RawMessage, 0, len(linhas))
	for _, l := range linhas {
		if len(l) != len(itens) {
			panic(fmt.Sprintf("site: %s: linha original com %d colunas, esperadas %d", chave, len(l), len(itens)))
		}
		item := make(map[string]json.RawMessage, len(itens))
		for i, s := range itens {
			if s.Tipo == TipoImagem {
				item[s.Chave] = json.RawMessage("null")
				continue
			}
			item[s.Chave] = jsonDe(l[i])
		}
		original = append(original, item)
	}
	return Campo{Chave: chave, Rotulo: rotulo, Tipo: TipoLista, Ajuda: ajuda, Max: maxItensDeLista, Itens: itens, Original: jsonDe(original)}
}

func numeros(chave, rotulo string, linhas ...[]string) Campo {
	return lista(chave, rotulo,
		"Cada número aparece grande, com a legenda embaixo. Ex.: “24” e “Hóspedes na casa completa”.",
		[]Subcampo{sub("valor", "Número", TipoTexto), sub("rotulo", "Legenda", TipoTexto)}, linhas...)
}

// categoria monta os cinco campos de uma categoria de acomodação. Preço,
// lotação e disponibilidade NÃO são campos: vêm do tarifário.
func categoria(c, nome, selo, descricao string, specs ...string) []Campo {
	linhas := make([][]string, 0, len(specs))
	for _, s := range specs {
		linhas = append(linhas, []string{s})
	}
	p := "categoria." + c
	return []Campo{
		texto(p+".titulo", nome+" — nome", "O nome da acomodação no card.", nome),
		texto(p+".selo", nome+" — selo", "A etiqueta pequena sobre a foto do card.", selo),
		longo(p+".descricao", nome+" — descrição", "", descricao),
		lista(p+".itens", nome+" — destaques",
			"Os destaques curtos do card (ex.: “2 suítes”). Preço e lotação vêm da tabela de tarifas, não daqui.",
			[]Subcampo{sub("texto", "Destaque", TipoTexto)}, linhas...),
		imagem(p+".foto", nome+" — foto", "", "", ""),
	}
}

func juntar(grupos ...[]Campo) []Campo {
	var out []Campo
	for _, g := range grupos {
		out = append(out, g...)
	}
	return out
}

// ─────────────────────────── O catálogo ─────────────────────────────────────

// catalogo é o catálogo completo: a rodada 1 (§4, abaixo) com a rodada 2
// (§4b, catalogo_rodada2.go) costurada por montarCatalogo.
var catalogo = montarCatalogo()

// ordemDasSecoesNovas diz depois de qual seção da rodada 1 entra cada seção
// nova da rodada 2 (docs/site-cms.md §4b, "Seções novas, na ordem do painel").
var ordemDasSecoesNovas = map[string][]string{
	"marca": {"menu"},
	"disp":  {"calendario", "orcamento", "pre-reserva", "whatsapp"},
}

func montarCatalogo() []Secao {
	out := make([]Secao, 0, len(catalogoRodada1)+len(secoesDaRodada2))
	for _, s := range catalogoRodada1 {
		s.Campos = append(append([]Campo{}, s.Campos...), camposDaRodada2[s.Chave]...)
		out = append(out, s)
		for _, nova := range ordemDasSecoesNovas[s.Chave] {
			out = append(out, secoesDaRodada2[nova])
		}
	}
	return out
}

var catalogoRodada1 = []Secao{
	{Chave: "seo", Rotulo: "Google e redes", Campos: []Campo{
		texto("seo.inicio.titulo", "Título da página inicial (aba do navegador e Google)",
			"Aparece na aba do navegador e no resultado do Google. Ideal: até 60 letras.",
			"White House · Praia do Coqueiro | Aluguel por temporada e eventos"),
		withMax(longo("seo.inicio.descricao", "Descrição da página inicial no Google",
			"O resumo que o Google e o WhatsApp mostram embaixo do título. Ideal: até 160 letras.",
			"Condomínio de luxo na Praia do Coqueiro, Luís Correia (PI). Hospedagem por temporada, casamentos, aniversários, retiros e produções. Consulte disponibilidade."), 300),
		texto("seo.disponibilidade.titulo", "Título da página de disponibilidade",
			"Aparece na aba do navegador e no resultado do Google. Ideal: até 60 letras.",
			"Disponibilidade e tarifas | White House · Praia do Coqueiro"),
		withMax(longo("seo.disponibilidade.descricao", "Descrição da página de disponibilidade no Google",
			"O resumo que o Google mostra embaixo do título. Ideal: até 160 letras.",
			"Calendário central de reservas da White House: disponibilidade por produto, tarifas por período e orçamento imediato pela tabela vigente."), 300),
	}},
	{Chave: "marca", Rotulo: "Marca", Campos: []Campo{
		imagem("marca.logo", "Logotipo",
			"Aparece no topo e no rodapé de todas as páginas. Prefira PNG com fundo transparente.",
			"/assets/logo.png", "White House"),
	}},
	{Chave: "inicio", Rotulo: "Topo (capa)", Campos: []Campo{
		texto("inicio.local", "Linha de localização", "A linha pequena acima do título da capa.",
			"Praia do Coqueiro · Luís Correia · Piauí"),
		titulo("inicio.titulo", "Título da capa", "O litoral do Piauí em *exclusividade*"),
		longo("inicio.texto", "Texto da capa", "",
			"Um condomínio inteiro à beira-mar para temporadas, celebrações e produções. Suítes, piscina, rooftop e espaço gourmet — reservados só para você e os seus."),
		video("inicio.video", "Vídeo de fundo da capa",
			"Toca sem som, em repetição, atrás do título. MP4 ou WebM, até 300 MB — vídeos curtos (15 a 40 segundos) carregam mais rápido.",
			"/videos/hero.mp4"),
		// Pedido do dono em 03/10/2026: um vídeo deitado (16:9) para o
		// computador e outro em pé (9:16) para o celular. Opcional — vazio, o
		// celular continua com o vídeo de cima, recortado no meio.
		video("inicio.video-celular", "Vídeo de fundo da capa — celular",
			"Opcional. Versão EM PÉ (9:16, como Stories e Reels — 1080 × 1920) para quem abre o site com o celular na vertical. Sem este vídeo, o celular mostra o de cima, cortado nas laterais.",
			""),
		numeros("inicio.numeros", "Números da capa",
			[]string{"24", "Hóspedes na casa completa"},
			[]string{"12", "Acomodações"},
			[]string{"300m", "Da faixa de areia"}),
	}},
	{Chave: "faixa", Rotulo: "Faixa de temas", Campos: []Campo{
		lista("faixa.itens", "Temas da faixa", "As palavras que passam na faixa logo abaixo da capa.",
			[]Subcampo{sub("texto", "Tema", TipoTexto)},
			[]string{"Temporada"},
			[]string{"Casamentos & mini weddings"},
			[]string{"Aniversários"},
			[]string{"Retiros corporativos"},
			[]string{"Ensaios & produções"},
			[]string{"Réveillon & Carnaval"}),
	}},
	{Chave: "casa", Rotulo: "A casa", Campos: []Campo{
		rotulo("casa", "A casa"),
		titulo("casa.titulo", "Título", "Não é uma diária.\nÉ a casa *inteira* à sua disposição."),
		longo("casa.texto", "Texto", "",
			"A White House nasceu como residência de veraneio e virou o endereço onde as famílias do Piauí celebram o que importa. Cada unidade pode ser alugada isoladamente — ou o complexo inteiro pode ser reservado sob exclusividade."+
				"\n\n"+
				"Das Pool Suítes com saída para a piscina aos apartamentos duplex, da Classic Villa à Grand Villa com rooftop e piscina privativa, tudo foi pensado para receber: gente reunida, mesa posta, música ao entardecer e o mar a três minutos de caminhada."),
		imagem("casa.foto", "Foto", "Hoje o site mostra uma ilustração de pôr do sol. "+ajudaFoto, "", ""),
		numeros("casa.numeros", "Números",
			[]string{"6", "Apartamentos duplex"},
			[]string{"4", "Pool Suítes"},
			[]string{"2", "Villas"}),
	}},
	{Chave: "acomodacoes", Rotulo: "Acomodações", Campos: juntar(
		[]Campo{
			rotulo("acomodacoes", "Acomodações"),
			titulo("acomodacoes.titulo", "Título", "Escolha o seu *formato* de estadia"),
			longo("acomodacoes.texto", "Texto", "",
				"Seis duplex, quatro Pool Suítes e duas villas — você escolhe exatamente onde ficar. Valores a partir da diária de baixa temporada; fins de semana, feriados, alta temporada, Réveillon e Carnaval têm tarifa própria."),
		},
		categoria("duplex", "Apartamentos Duplex", "Hospedagem",
			"Seis apartamentos duplex com duas suítes e acesso à piscina — Aurora, Brisa, Duna, Maré, Âmbar e Horizonte. Você escolhe o seu.",
			"2 suítes", "até 7 pessoas", "acesso à piscina"),
		categoria("suites", "Pool Suítes", "Pé na piscina",
			"Quatro suítes com saída para a piscina — Coral, Pérola, Concha e Oceano. Reserve uma, ou as quatro juntas para até 8 pessoas.",
			"cama de casal", "frigobar", "acesso direto à piscina"),
		categoria("grand-villa", "White House Grand Villa", "Casa principal",
			"A casa principal triplex: amplos ambientes, elevador e rooftop com piscina privativa e vista para o mar. Pacotes de 2 e 4 diárias com valor especial.",
			"4 suítes", "elevador", "rooftop com piscina privativa", "vista para o mar"),
		categoria("classic-villa", "White House Classic Villa", "Casa rústica",
			"A casa rústica da White House, para quem quer a casa inteira com aconchego e privacidade.",
			"casa inteira", "charme rústico", "mínimo de 2 diárias"),
		categoria("completa", "White House Completa", "Exclusividade total",
			"O complexo inteiro sob exclusividade: duplex, suítes e villas. Base para casamentos, retiros e eventos — valores sob consulta.",
			"todas as unidades", "eventos", "exclusividade"),
	)},
	{Chave: "eventos", Rotulo: "Eventos", Campos: []Campo{
		rotulo("eventos", "Celebrações"),
		titulo("eventos.titulo", "Título", "A casa também recebe *o seu grande dia*"),
		longo("eventos.texto", "Texto", "",
			"Áreas independentes permitem receber convidados sem abrir mão do conforto de quem está hospedado. Produções, ensaios e gravações comerciais também são bem-vindos."),
		lista("eventos.cards", "Tipos de evento",
			"Cada cartão tem título, texto, uma lista curta (um item por linha) e, se quiser, uma foto de fundo.",
			[]Subcampo{
				sub("titulo", "Título", TipoTexto),
				sub("texto", "Texto", TipoTextoLongo),
				sub("itens", "Itens (um por linha)", TipoTextoLongo),
				sub("foto", "Foto", TipoImagem),
			},
			[]string{"Casamentos & mini weddings",
				"Cerimônia ao entardecer no rooftop, jantar no espaço gourmet e a noite inteira em casa — com a hospedagem dos noivos e padrinhos inclusa no pacote.",
				"Renovação de votos\nNoivado\nCerimônia + festa", ""},
			[]string{"Aniversários & confraternizações",
				"Piscina, churrasqueira e som liberado até o horário combinado. Da mesa de 20 à festa de 120 convidados.",
				"Chá revelação\nFormaturas", ""},
			[]string{"Corporativo & retiros",
				"Treinamentos, imersões e retiros de equipe com hospedagem, sala de trabalho e refeições no mesmo endereço.",
				"Off-sites\nEnsaios & produções", ""}),
	}},
	{Chave: "estrutura", Rotulo: "Estrutura", Campos: []Campo{
		rotulo("estrutura", "Estrutura"),
		titulo("estrutura.titulo", "Título", "Tudo dentro dos *muros*"),
		longo("estrutura.texto", "Texto", "", "Você chega, estaciona e não precisa sair para nada."),
		lista("estrutura.itens", "Itens da estrutura",
			"Cada item tem um ícone (um emoji, como 🏊), um nome e uma frase curta.",
			[]Subcampo{
				{Chave: "icone", Rotulo: "Ícone (emoji)", Tipo: TipoTexto, Max: 8},
				sub("titulo", "Nome", TipoTexto),
				sub("texto", "Frase", TipoTexto),
			},
			[]string{"🏊", "Piscina", "Área molhada com deck, espreguiçadeiras e suítes com acesso direto."},
			[]string{"🌇", "Rooftop", "Vista aberta para o pôr do sol — o melhor lugar da casa para cerimônias."},
			[]string{"🔥", "Churrasqueira", "Churrasqueira e espaço gourmet integrados à área externa."},
			[]string{"🍽️", "Cozinha completa", "Cozinha equipada para receber, com apoio para buffet e equipe."},
			[]string{"🛏️", "Suítes", "Todas com ar-condicionado, roupa de cama e banho inclusas."},
			[]string{"🚗", "Estacionamento", "Vagas internas para hóspedes e convidados de eventos."},
			[]string{"🛜", "Wi-Fi & trabalho", "Internet de alta velocidade e área para reuniões e treinamentos."},
			[]string{"🏖️", "Praia a 300m", "Acesso rápido à faixa de areia da Praia do Coqueiro."}),
	}},
	{Chave: "local", Rotulo: "Localização", Campos: []Campo{
		rotulo("local", "Localização"),
		titulo("local.titulo", "Título", "Praia do Coqueiro, *Luís Correia*"),
		longo("local.texto", "Texto", "",
			"No trecho mais tranquilo do litoral piauiense, entre Parnaíba e as praias de Atalaia e Macapá."),
		lista("local.itens", "Distâncias",
			"Cada linha tem um destaque em negrito (ex.: “300 m”) e o complemento.",
			[]Subcampo{sub("destaque", "Destaque", TipoTexto), sub("texto", "Complemento", TipoTexto)},
			[]string{"300 m", "da faixa de areia da Praia do Coqueiro"},
			[]string{"18 km", "do centro de Parnaíba"},
			[]string{"25 min", "do Aeroporto de Parnaíba (PHB)"},
			[]string{"1 h", "do Delta do Parnaíba e Pedra do Sal"}),
		imagem("local.foto", "Foto", "Hoje o site mostra uma ilustração da área da piscina. "+ajudaFoto, "", ""),
	}},
	{Chave: "chamada", Rotulo: "Chamada final", Campos: []Campo{
		titulo("chamada.titulo", "Título", "Sua data ainda está *livre*?"),
		// §4b: o original passa a ser a frase que o visitante vê hoje, com o
		// prazo da política no lugar de {horas}.
		longo("chamada.texto", "Texto",
			"{horas} vira o prazo da pré-reserva da política em vigor. Se o sistema não responder, o site mostra a frase sem o número.",
			"Consulte o calendário em tempo real, monte o orçamento da sua estadia e garanta a data com uma pré-reserva de {horas} horas."),
	}},
	{Chave: "rodape", Rotulo: "Rodapé e contato", Campos: []Campo{
		longo("rodape.texto", "Frase do rodapé", "Aparece embaixo do logotipo, no rodapé de todas as páginas.",
			"Condomínio de temporada e eventos na Praia do Coqueiro — Luís Correia, Piauí."),
		withFormato(texto("rodape.email", "E-mail de reservas", "O endereço que aparece no rodapé; clicar nele abre o e-mail.",
			"reservas@whitehouse.com.br"), formatoEmail),
		texto("rodape.horario", "Horário de atendimento", "", "Atendimento 8h — 20h"),
		longo("rodape.endereco", "Endereço", "Uma linha por linha do endereço.",
			"Praia do Coqueiro\nLuís Correia — PI"),
		texto("rodape.copyright", "Linha de direitos", "A última linha do rodapé.",
			"© 2026 White House — Todos os direitos reservados"),
	}},
	{Chave: "disp", Rotulo: "Página de disponibilidade", Campos: []Campo{
		rotulo("disp", "Central única de reservas"),
		titulo("disp.titulo", "Título", "Disponibilidade & *tarifas*"),
		longo("disp.texto", "Texto", "",
			"Um só calendário para todos os produtos. Selecione a acomodação, escolha as datas e o orçamento é calculado na hora — com tarifa de fim de semana, feriado, alta temporada, Réveillon e Carnaval já aplicadas."),
		texto("disp.tarifas.rotulo", "Rótulo da tabela de tarifas", ajudaRotulo, "Tabela comercial V1"),
		titulo("disp.tarifas.titulo", "Título da tabela de tarifas", "Tarifas por *tipo de data*"),
		titulo("disp.chamada.titulo", "Título da chamada final", "Segure a data com uma *pré-reserva*"),
	}},
	{Chave: "erro", Rotulo: "Página não encontrada", Campos: []Campo{
		titulo("erro.titulo", "Título", "Esta página não *existe*"),
		longo("erro.texto", "Texto", "",
			"O endereço pode ter mudado ou ter sido digitado com algum erro. As acomodações e o calendário continuam a um clique."),
	}},
}

func withMax(c Campo, m int) Campo        { c.Max = m; return c }
func withFormato(c Campo, f string) Campo { c.Formato = f; return c }

// indice é chave → campo, montado uma vez.
var indice = func() map[string]*Campo {
	m := map[string]*Campo{}
	for i := range catalogo {
		for j := range catalogo[i].Campos {
			c := &catalogo[i].Campos[j]
			m[c.Chave] = c
		}
	}
	return m
}()

// CampoDoCatalogo devolve o campo pela chave.
func CampoDoCatalogo(chave string) (Campo, bool) {
	c, ok := indice[chave]
	if !ok {
		return Campo{}, false
	}
	return *c, true
}

// Catalogo devolve as seções na ordem do painel.
func Catalogo() []Secao { return catalogo }
