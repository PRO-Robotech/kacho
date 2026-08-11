// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package clients

import (
	"context"
	"time"

	"google.golang.org/grpc"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/auth"
	"github.com/PRO-Robotech/kacho/pkg/peer"
	"github.com/PRO-Robotech/kacho/pkg/retry"
)

// Публичный ProjectService.Get в kacho-iam несет tenant scope-filter: он
// возвращает NOT_FOUND, если caller — не owner владеющего Account'а, и сразу
// NOT_FOUND для анонимного caller'а. kacho-vpc валидирует project Network'а через
// ProjectService.Get на hot-path Network.Create; вызов идет внутри Operation
// worker'а, чей ctx (через operations baggage) все еще несет исходный request
// Principal — но vpc обязан пробросить его в исходящую gRPC-metadata через
// auth.PropagateOutgoing, иначе peer увидит анонимный/системный вызов, вернет
// NOT_FOUND, и Network.Create провалит свою project-exists-проверку.

// ProjectClient реализует service.ProjectClient через gRPC к kacho-iam.
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

// Exists проверяет существование Project через kacho-iam.ProjectService.Get.
// Кеш — в CachedProjectClient (bounded LRU); тут только gRPC + retry.
func (c *ProjectClient) Exists(ctx context.Context, projectID string) (bool, error) {
	var exists bool
	err := retry.OnUnavailable(ctx, func(ctx context.Context) error {
		cctx, cancel := peerCallCtx(ctx, c.timeout)
		defer cancel()
		_, rerr := c.cli.Get(auth.PropagateOutgoing(cctx), &iamv1.GetProjectRequest{ProjectId: projectID})
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
		return nil
	})
	if err != nil {
		return false, err
	}
	return exists, nil
}
