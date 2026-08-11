// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package clients

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/auth"
	"github.com/PRO-Robotech/kacho/pkg/peer"

	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/image"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/snapshot"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/volume"
	storageerr "github.com/PRO-Robotech/kacho/services/storage/internal/errors"
)

// IAMClient — клиент ребра storage→iam (валидация project_id через
// ProjectService.Get, fail-closed). Реализует и volume.IAMClient, и
// snapshot.IAMClient (идентичная сигнатура EnsureProjectExists).
type IAMClient struct {
	cli iamv1.ProjectServiceClient
}

// NewIAMClient создаёт IAMClient поверх готового *grpc.ClientConn к kacho-iam.
// conn может быть nil в dev-скелете — тогда fail-closed Unavailable.
func NewIAMClient(conn *grpc.ClientConn) *IAMClient {
	c := &IAMClient{}
	if conn != nil {
		c.cli = iamv1.NewProjectServiceClient(conn)
	}
	return c
}

// EnsureProjectExists валидирует project_id через kacho-iam (ProjectService.Get)
// на request-path Create.
//
// Полосу ответа выбирает носитель (pkg/peer), а не рукописный разбор кодов.
// Прежде здесь стояло «NotFound либо InvalidArgument → предусловие не выполнено,
// ВСЁ ОСТАЛЬНОЕ → недоступность»: отказ владельца В ПРАВАХ попадал во «всё
// остальное» и уезжал арендатору как ВРЕМЕННЫЙ отказ. Повтор его не лечит —
// решение о правах есть функция от (вызывающий, отношение, объект), и ни одного
// из трёх повтор не меняет. Арендатор получал «повтори позже» на нашу неверную
// настройку, а сама настройка выглядела перебоем у соседа.
//
// Форма ответа не меняется: sentinel + контракт-текст «Project <id> not found»,
// который разворачивает сервисный маппер. Меняется РАСКЛАДКА кодов по полосам.
func (c *IAMClient) EnsureProjectExists(ctx context.Context, projectID string) error {
	if c.cli == nil {
		return status.Error(codes.Unavailable, "storage→iam ProjectService not configured")
	}
	cctx, cancel := context.WithTimeout(ctx, peerCallTimeout)
	defer cancel()
	_, err := c.cli.Get(auth.PropagateOutgoing(cctx), &iamv1.GetProjectRequest{ProjectId: projectID})
	if err == nil {
		return nil
	}
	if peer.Classify(err).RefusedReference() {
		return fmt.Errorf("%w: Project %s not found", storageerr.ErrFailedPrecondition, projectID)
	}
	// Владелец ничего не установил — недоступность либо непонятый ответ.
	// Fail-closed для мутации, и НИКОГДА не проза соседа: она несёт host/port.
	return status.Error(codes.Unavailable, "iam project validation unavailable")
}

var (
	_ volume.IAMClient   = (*IAMClient)(nil)
	_ snapshot.IAMClient = (*IAMClient)(nil)
	_ image.IAMClient    = (*IAMClient)(nil)
)
