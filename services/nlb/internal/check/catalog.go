// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check

// catalogOnlyOperationPermissions — имена каталога, за которыми не стоит своего
// RPC этого домена.
//
// `OperationService.Get/Cancel` объявлены `<exempt>` (per-RPC Check снят,
// авторизация — предикат владельца в SQL), поэтому в выведенной карте они
// permission не несут; `loadbalancer.operations.list` — каталожное имя без
// отдельного RPC вовсе (пообъектный `ListOperations` несёт своё).
var catalogOnlyOperationPermissions = []string{
	"loadbalancer.operations.get",
	"loadbalancer.operations.cancel",
	"loadbalancer.operations.list",
}

// Catalog — имена прав домена loadbalancer: те, что несут RPC карты, плюс
// каталожные без RPC.
//
// Имена берутся из выведенной карты, то есть из аннотаций proto — из того же
// источника, что и каталог края. Прежде их держал собственный перечень констант
// рядом с рукописной картой, и это был третий список об одном предмете.
//
// Порядок не гарантируется; сверяющая проба сравнивает как множество.
func Catalog() []string {
	m := PermissionMap()
	out := make([]string, 0, len(m)+len(catalogOnlyOperationPermissions))
	for _, e := range m {
		if e.Permission != "" {
			out = append(out, e.Permission)
		}
	}
	return append(out, catalogOnlyOperationPermissions...)
}
