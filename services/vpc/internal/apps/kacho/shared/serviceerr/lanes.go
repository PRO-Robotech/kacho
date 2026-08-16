// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package serviceerr

import (
	kerrors "github.com/PRO-Robotech/kacho/pkg/errors"
)

// Полосы СОБСТВЕННОГО резолва — в отличие от peer.go, где полосы про чужой
// ресурс. Клиент различает полосы машинно, по `ErrorInfo.reason`, а не разбором
// прозы: тон сообщения стабилен, но не парсибелен.
//
// Обе функции живут здесь, а не у вызывающего, по той же причине, по которой
// здесь живёт `serviceDomain`: литерал источника отказа, повторённый по местам
// вызова, разъезжается молча — и разъедется он именно там, где деталь читают
// машиной, а не глазом.

// NotFoundLane — полоса ПРЯМОГО ЧТЕНИЯ: own-owned id корректен по форме, строки
// в своей БД нет. Код — NOT_FOUND: это «я не нашёл СВОЙ ресурс», а не
// «предусловие на чужой не выполнено».
//
// Тон — контрактный `"<Resource> <id> not found"` (`api-conventions.md`).
// Общий классификатор отдаёт на этой полосе голый текст sentinel'а («not
// found»), потому что id ему неизвестен; вызывающий его знает, поэтому тон
// собирается там, где есть чем его собрать.
func NotFoundLane(resourceType, display, id string) error {
	return kerrors.ReasonResourceNotFound.Errf(
		kerrors.PeerRef{Service: serviceDomain, ResourceType: resourceType, ResourceID: id},
		"%s %s not found", display, id)
}

// InvalidIDLane — полоса SYNC-ФОРМАТА: own-id негоден по форме, отвергнут первым
// стейтментом RPC, до любого обращения к репозиторию.
//
// Без этой полосы негодный по форме id уезжает в `repo.Get` и возвращается
// кодом ОТСУТСТВИЯ — то есть утверждением, что такой ресурс мог бы
// существовать. Это не косметика: «исправь ввод» и «его нет» — разные ответы,
// и второй заставляет вызывающего искать несуществующее.
func InvalidIDLane(resourceType, display, id string) error {
	return kerrors.ReasonInvalidResourceID.Errf(
		kerrors.PeerRef{Service: serviceDomain, ResourceType: resourceType, ResourceID: id},
		"invalid %s id '%s'", display, id)
}
