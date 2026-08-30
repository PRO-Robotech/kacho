// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package serviceerr

import (
	kerrors "github.com/PRO-Robotech/kacho/pkg/errors"
)

// serviceDomain — источник отказа, из которого собирается `ErrorInfo.domain`
// вида "<service>.kacho.cloud". Литерал живёт ЗДЕСЬ, а не по местам вызова:
// повторённый, он разъезжается молча — и разъедется именно там, где деталь
// читают машиной, а не глазом.
const serviceDomain = "geo"

// Типы ресурсов каталога размещения, как они уезжают в `ErrorInfo.metadata`.
// Специфика ресурса живёт ЗДЕСЬ, а не в токене: токен называет ПОЛОСУ
// (`RESOURCE_NOT_FOUND`), одну на все ресурсы, — иначе словарь полос разросся бы
// по числу ресурсов, и клиенту пришлось бы знать их все, чтобы понять одно.
const (
	RegionKind = "geo.region"
	ZoneKind   = "geo.zone"
)

// NotFoundLane — полоса ПРЯМОГО ЧТЕНИЯ: own-owned id корректен по форме, строки
// в своей БД нет. Код — NOT_FOUND: это «я не нашёл СВОЙ ресурс», а не
// «предусловие на чужой не выполнено».
//
// # Почему полоса нужна ИМЕННО leaf-владельцу каталога
//
// Consumer'ы (vpc/compute/nlb/registry) классифицируют ответ соседа ПО КОДУ
// (`pkg/peer.Classify`), и токен geo им не нужен by construction. Читатель здесь
// другой — АРЕНДАТОР НАПРЯМУЮ: `Region/Zone.Get` и `List` публичны
// (project-scope EXEMPT, `security.md`), и каталог размещения обязан прочитать
// всякий, кто создаёт размещаемый ресурс. Он получает 404 и без токена
// вынужден разбирать прозу — то есть ровно то, что контракт запрещает.
//
// # Проза не выводится из полосы
//
// Тон `"<Resource> <id> not found"` — часть контракта Kachō и принадлежит
// вызывающему; полоса добавляет к сказанному ТОЛЬКО машинный признак. Деталь не
// влияет на HTTP-статус края (grpc-gateway отображает по КОДУ), поэтому
// постановка признака ничего не ломает у REST-клиента.
func NotFoundLane(resourceType, display, id string) error {
	return kerrors.ReasonResourceNotFound.Errf(
		kerrors.PeerRef{Service: serviceDomain, ResourceType: resourceType, ResourceID: id},
		"%s %s not found", display, id)
}
