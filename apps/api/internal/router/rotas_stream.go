package router

import "net/http"

// rotasStream declara as rotas do módulo de stream.
//
// Este arquivo pertence a quem implementa o módulo: mexer só aqui evita disputa
// com os outros módulos escritos em paralelo.
func rotasStream(d Deps) []Rota {
	return []Rota{
		{
			Metodo: http.MethodGet, Path: "/stream", Acesso: AcessoAutenticado,
			// A rota não é um recurso.
			//
			// `AcessoPermissao` obrigaria a escolher UMA célula (recurso, ação)
			// para a rota inteira, e não existe uma que descreva `/stream`:
			// quem assina `calendar` precisa de `calendar:ver`, quem assina
			// `crm` precisa de `crm.opportunities:ver`, e é rotina alguém
			// alcançar um e não o outro. Uma célula única mentiria nos dois
			// sentidos — barraria o corretor que só quer o calendário, ou
			// deixaria passar quem não pode ver o funil.
			//
			// A conferência real acontece por TÓPICO, dentro do handler, contra
			// o mesmo `auth.Conjunto` que o middleware usaria; o mapa está no
			// contrato como `x-rbac-topicos`. Tópico fora da matriz é descartado
			// da assinatura, e conexão que sobra sem tópico nenhum é 403.
			Motivo: "a rota não é um recurso: cada tópico assinado é conferido " +
				"contra a matriz no handshake (x-rbac-topicos do contrato), e a " +
				"conexão abre só com os tópicos alcançáveis",
			Handler: d.Stream.Assinar,
		},
	}
}
