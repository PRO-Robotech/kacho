// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

// Package narrowiam — перевод порта сужателя в контракт владельца модели.
//
// # Почему адаптер живёт ЗДЕСЬ, а не рядом с портом
//
// Порт (`pkg/listnarrow`) принадлежит фундаменту, а контракт `AuthorizeService`
// — службе доступа. Пока адаптер лежал в самом порту, фундамент импортировал
// контракт службы: после разъезда на три модуля `corelib` потребовал бы
// `kaname`, который уже требует `corelib`, — цикл, который Go не собирает
// (приёмка K3-1 §7.2, задача #2131).
//
// Правило, из которого выведено размещение: КОНТРАКТ ОСТАЁТСЯ У ТОГО, КТО ЕГО
// РЕАЛИЗУЕТ. Каталог объявлен классом `kaname` (карта расщеплений гейта
// границы), поэтому его рёбра законны в обе стороны: к порту фундамента —
// `kaname → corelib`, к контракту доступа — внутри `kaname`. Зовут его службы
// платформы, и это `kacho → kaname` — тоже разрешённое направление.
//
// Эталон того же приёма лежит рядом: `pkg/authz` принимает решателя интерфейсом
// и контракта не импортирует вовсе.
package narrowiam

import (
	"context"

	"google.golang.org/grpc"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/auth"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow"
)

// New оборачивает соединение с владельцем модели в порт сужателя. conn
// указывает на службу доступа, где живёт `AuthorizeService` (`BatchCheck` —
// публичный read-RPC, зарегистрирован и на внутреннем листенере).
func New(conn grpc.ClientConnInterface) listnarrow.AuthorizeClient {
	return &grpcAuthorizeClient{cli: iamv1.NewAuthorizeServiceClient(conn)}
}

type grpcAuthorizeClient struct {
	cli iamv1.AuthorizeServiceClient
}

// BatchCheck переводит вопросы фундамента в контракт владельца и возвращает
// вердикты В ПОРЯДКЕ ВОПРОСОВ — так, как объявил порт.
//
// Длина ответа ЗДЕСЬ не сверяется намеренно: сверяет её вызывающий, и он же
// знает, о какой партии и о каком отношении шла речь. Вторая сверка в адаптере
// была бы вторым местом об одном предмете и разошлась бы с первым молча.
//
// Исходящий контекст обёрнут auth.PropagateOutgoing, чтобы извлекатель личности
// на стороне владельца увидел РЕАЛЬНОГО вызывающего, а не запасное значение
// уровня начальной загрузки. Без обёртки внутренний страж владельца видит
// служебную личность и отбивает запрос: сужатель возвращал бы отказ каждому
// пользователю — страж пропускает вопрос субъекта О СЕБЕ, поэтому настоящий
// принципал обязателен.
func (g *grpcAuthorizeClient) BatchCheck(ctx context.Context, checks []listnarrow.Check) ([]bool, error) {
	in := make([]*iamv1.AuthorizeCheckRequest, 0, len(checks))
	for _, c := range checks {
		in = append(in, &iamv1.AuthorizeCheckRequest{
			Subject:          c.Subject,
			Resource:         &iamv1.ResourceRef{Type: c.ResourceType, Id: c.ResourceID},
			Action:           c.Action,
			RequiredRelation: c.RequiredRelation,
		})
	}
	resp, err := g.cli.BatchCheck(auth.PropagateOutgoing(ctx),
		&iamv1.BatchAuthorizeCheckRequest{Checks: in})
	if err != nil {
		return nil, err
	}
	out := make([]bool, 0, len(resp.GetResponses()))
	for _, r := range resp.GetResponses() {
		out = append(out, r.GetAllowed())
	}
	return out, nil
}

var _ listnarrow.AuthorizeClient = (*grpcAuthorizeClient)(nil)
