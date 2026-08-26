package auth

// Códigos de recurso referenciados pelo código Go.
//
// O CATÁLOGO é dado: vive na tabela `resources` e é o seed que o popula. Estas
// constantes existem só para a tabela de rotas e as regras que precisam citar um
// recurso específico sem digitar a string em cinco lugares. Acrescentar recurso
// novo é INSERT no seed; a constante só aparece aqui quando o Go precisa
// mencioná-lo.
const (
	RecursoUsuarios = "users"
	RecursoPerfis   = "roles"
)
