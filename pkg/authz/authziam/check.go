// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

// Package authziam — перевод порта проверки доступа в контракт владельца модели.
//
// # Почему адаптер живёт ЗДЕСЬ, а не в носителе
//
// Порт (`pkg/authz.CheckClient`) принадлежит фундаменту, а контракт
// `InternalIAMService` — службе доступа. Пока адаптер лежал в носителе
// (`pkg/servicehost`), фундамент импортировал контракт службы: после разъезда на
// три модуля `corelib` потребовал бы `kaname`, который уже требует `corelib`, —
// цикл, который Go не собирает (приёмка K3-1 §7.2, задача #2131).
//
// Правило то же, что у соседнего `pkg/listnarrow/narrowiam`: КОНТРАКТ ОСТАЁТСЯ
// У ТОГО, КТО ЕГО РЕАЛИЗУЕТ. Каталог объявлен классом `kaname` в карте
// расщеплений гейта границы, поэтому его рёбра законны: к порту фундамента —
// `kaname → corelib`, к контракту доступа — внутри `kaname`, а зовущие его
// службы платформы дают `kacho → kaname`.
package authziam

import (
	"context"

	"google.golang.org/grpc"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/auth"
	"github.com/PRO-Robotech/kacho/pkg/authz"
)

// NewCheckClient оборачивает соединение с владельцем модели в порт проверки.
//
// Годится прямо как значение `servicecontract.Spec.PeerCheck`: носитель набирает
// соседа по объявленному ребру и зовёт этот сборщик, оставаясь без знания о
// чужом контракте.
func NewCheckClient(conn grpc.ClientConnInterface) authz.CheckClient {
	return &checkClient{cli: iamv1.NewInternalIAMServiceClient(conn)}
}

// checkClient — адаптер клиента владельца модели под порт проверки.
//
// Исходящий контекст оборачивается [auth.PropagateOutgoing], чтобы на стороне
// владельца извлечение личности видело РЕАЛЬНОГО вызывающего, а не сервис,
// задающий вопрос.
type checkClient struct {
	cli iamv1.InternalIAMServiceClient
}

// Check спрашивает владельца модели и НЕ разбирает прозу его ответа.
//
// # Почему причина отказа не читается, хотя соблазн есть
//
// Владелец различает в тексте причины «пути к объекту нет» и «не хватает
// отношения», и два сервиса из семи этот текст разбирали, превращая первое в
// пропуск к обработчику. Адаптер так не делает, и это осознанный выбор, а не
// потеря:
//
//   - пропуск к обработчику на отказе — решение о ДОСТУПЕ, и принимать его по
//     подстроке чужого сообщения нельзя: тон сообщения стабилен, но не
//     предназначен для разбора машиной (машинная полоса — token в `details`);
//   - «пути нет» означает «намерение регистрации ещё не доехало» ровно так же
//     часто, как «объекта нет»: платформа eventually-consistent. Пропуск в этом
//     окне отдал бы существующий объект тому, кому он не принадлежит;
//   - у вопроса есть авторитетный ответчик — СВОЯ БАЗА. Его даёт порт
//     существования носителя, и он же отличает «нет объекта» от «есть, но не
//     твой» без единого допущения о чужом тексте.
//
// Перепись, из-за которой ветка не была введена на все семь: разбор причины
// живёт у vpc и nlb, у compute, storage, registry, geo и iam его нет
// (`grep -rl ErrNoPath services/*/... | grep -v _test`). Ввести его разом
// значило бы завести пропуск там, где сегодня отказ, — то есть ослабить пятерых
// ради единообразия.
func (c *checkClient) Check(ctx context.Context, subjectID, relation, object string) (bool, error) {
	resp, err := c.cli.Check(auth.PropagateOutgoing(ctx), &iamv1.CheckRequest{
		SubjectId: subjectID,
		Relation:  relation,
		Object:    object,
	})
	if err != nil {
		return false, err
	}
	return resp.GetAllowed(), nil
}

var _ authz.CheckClient = (*checkClient)(nil)
