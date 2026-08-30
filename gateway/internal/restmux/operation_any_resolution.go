// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

import (
	"google.golang.org/protobuf/proto"

	"github.com/PRO-Robotech/kacho/internal/operationany"
)

// operation_any_resolution.go — край обязан уметь РАЗРЕШИТЬ каждый тип, который
// владельцы кладут в `Operation.response`.
//
// ПРЕДМЕТ, МЕХАНИЗМ И ПРИЧИНА, ПО КОТОРОЙ ЭТО ВООБЩЕ ОБЪЯВЛЕНО, — в шапке
// `internal/operationany`. Здесь их пересказа нет намеренно: два места об одном
// предмете расходятся на первом же уточнении, а расходиться этим двум нельзя —
// от них зависит, ответит ли край телом или пятисоткой.
//
// ПОЧЕМУ ОБЪЯВЛЕНИЕ ЛЕЖИТ НЕ ЗДЕСЬ. У него два читателя по разные стороны
// границы `internal`: сам край и гейт дерева `internal/repohygiene`, который
// сверяет объявление с тем, что владельцы в дерево кладут. Пакет под
// `gateway/internal` из корневого `internal` не импортируется, а сверять
// объявление ЧТЕНИЕМ ИСХОДНИКА КАК ТЕКСТА в этом дереве запрещено. Поэтому
// объявление — одно, в общем пакете, и обе стороны читают его ЗНАЧЕНИЯ.
//
// ЧТО ЗДЕСЬ ОСТАЛОСЬ. Ссылка на общий пакет — и именно она вносит
// `google.protobuf.Empty` в реестр типов ЭТОГО бинаря: цепочка импортов
// restmux → operationany → emptypb. Снятие ссылки уносит регистрацию, и край
// начинает отвечать 500 на каждом завершённом удалении; это ловит
// `TestEdgeRendersOperationResponsePackedByOwner` в соседнем файле.

// operationResponseAnchors — МЕХАНИЗМ разрешения, взятый из общего объявления.
var operationResponseAnchors = operationany.Anchors()

// requiredOperationResponseTypeURLs — НАМЕРЕНИЕ: адреса, которые край обязан
// разрешать.
func requiredOperationResponseTypeURLs() []string {
	return operationany.RequiredResponseTypeURLs()
}

// anchoredTypeURLs — адреса, которые якоря фактически вносят в реестр.
func anchoredTypeURLs() []string { return operationany.AnchoredTypeURLs() }

// проверка формы: якоря остаются сообщениями proto, а не превращаются в строки.
var _ []proto.Message = operationResponseAnchors
