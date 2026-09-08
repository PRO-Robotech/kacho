// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package clients

import (
	"context"
	"time"

	"google.golang.org/grpc"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/auth"
	"github.com/PRO-Robotech/kacho/pkg/peer"
	"github.com/PRO-Robotech/kacho/pkg/retry"
)

// Публичный ProjectService.Get в kaname несет tenant scope-filter: он
// возвращает NOT_FOUND, если caller — не owner владеющего Account'а, и сразу
// NOT_FOUND для анонимного caller'а. kacho-vpc валидирует project Network'а через
// ProjectService.Get на hot-path Network.Create; вызов идет внутри Operation
// worker'а, чей ctx (через operations baggage) все еще несет исходный request
// Principal — но vpc обязан пробросить его в исходящую gRPC-metadata через
// auth.PropagateOutgoing, иначе peer увидит анонимный/системный вызов, вернет
// NOT_FOUND, и Network.Create провалит свою project-exists-проверку.

// ProjectClient реализует service.ProjectClient через gRPC к kaname.
//
// Кеширование живет в декораторе CachedProjectClient (project_cache.go) —
// bounded TTL+LRU поверх Exists, которым оборачивается raw-клиент в composition
// root. Здесь — чистый pass-through к gRPC без локального кеша.
type ProjectClient struct {
	cli     iamv1.ProjectServiceClient
	timeout time.Duration // per-call deadline на каждый iam-вызов (см. defaultPeerCallTimeout)
}

// NewProjectClient создает ProjectClient. conn — обычно `clients.Build(...)`
// (см. builder.go), принимается как grpc.ClientConnInterface — что подходит и
// для corlib `ClientConn`, и для `*grpc.ClientConn`.
func NewProjectClient(conn grpc.ClientConnInterface) *ProjectClient {
	return &ProjectClient{cli: iamv1.NewProjectServiceClient(conn), timeout: defaultPeerCallTimeout}
}

// Exists проверяет существование Project через kaname.ProjectService.Get.
// Кеш — в CachedProjectClient (bounded LRU); тут только gRPC + retry.
func (c *ProjectClient) Exists(ctx context.Context, projectID string) (bool, error) {
	exists, _, err := c.describe(ctx, projectID)
	return exists, err
}

// AccountOf возвращает аккаунт проекта — зеркало, без которого строка учёта
// невидима аккаунтной дельте (приёмка квот, V2-4).
//
// ТОТ ЖЕ вызов, что и Exists, а не второй: `ProjectService.Get` возвращает
// проект целиком, и прежде ответ выбрасывался. «Нового ребра работа не заводит»
// держится ЗДЕСЬ — тем, что оба глагола идут одним `describe`, а кэш поверх них
// (`CachedProjectClient`) хранит одну запись на проект. Заведи AccountOf
// собственный вызов — утверждение осталось бы верным на бумаге и ложным в
// нагрузке: путь создания платил бы двумя обращениями вместо одного.
func (c *ProjectClient) AccountOf(ctx context.Context, projectID string) (string, error) {
	exists, accountID, err := c.describe(ctx, projectID)
	if err != nil {
		return "", err
	}
	if !exists {
		// Отсутствие проекта — не «пустой аккаунт»: пустая строка уехала бы в
		// материализацию и была бы отвергнута ограничением схемы, назвав
		// предметом зеркало вместо проекта. Наружу идёт то же, что и у Exists:
		// отказ ссылки, неразличимый для арендатора между промахом и отказом в
		// правах (анти-оракул).
		return "", nil
	}
	return accountID, nil
}

// Describe возвращает ОБА факта о проекте одним обращением: существует ли он и
// какому аккаунту принадлежит.
//
// Экспортирован ради кэша-декоратора: тот обязан класть в одну запись оба факта,
// а не выводить один из другого. Вывод «аккаунт непуст ⇒ проект есть» выглядит
// безобидно и неверен по построению: пустой аккаунт у существующего проекта —
// состояние, которого владелец проектов нам не обещал, и в тот день, когда оно
// случится, существующий проект стал бы «несуществующим» — то есть отказ пришёл
// бы не оттуда и не про то.
func (c *ProjectClient) Describe(ctx context.Context, projectID string) (bool, string, error) {
	return c.describe(ctx, projectID)
}

// describe — единственное место, где vpc зовёт ProjectService.Get.
func (c *ProjectClient) describe(ctx context.Context, projectID string) (bool, string, error) {
	var (
		exists    bool
		accountID string
	)
	err := retry.OnUnavailable(ctx, func(ctx context.Context) error {
		cctx, cancel := peerCallCtx(ctx, c.timeout)
		defer cancel()
		resp, rerr := c.cli.Get(auth.PropagateOutgoing(cctx), &iamv1.GetProjectRequest{ProjectId: projectID})
		if rerr != nil {
			// Полосу выбирает носитель (pkg/peer). «Владелец установил, что ссылка
			// не годится» — это промах, негодный по его мнению id И ОТКАЗ В ПРАВАХ:
			// последний прежде проваливался наружу сырым ответом соседа и приезжал
			// арендатору недоступностью, то есть «повтори позже» на отказ, который
			// повтором не лечится.
			//
			// Каноничная async-ошибка «Project <id> not found» собирается выше по
			// exists=false; сырой текст соседа («project check: rpc error: code =
			// Inval…») наружу не уходит.
			if peer.Classify(rerr).RefusedReference() {
				exists = false
				return nil
			}
			return rerr
		}
		exists = true
		accountID = resp.GetAccountId()
		return nil
	})
	if err != nil {
		return false, "", err
	}
	return exists, accountID, nil
}
