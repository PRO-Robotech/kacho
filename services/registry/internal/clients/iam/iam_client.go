// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package iam — adapter-клиент к kacho-registry-консумируемым RPC kaname.
// Реализует порт registry.IAMClient: cross-domain валидация project'а на Create
// (ProjectService.Get). ProjectService живёт ТОЛЬКО на iam PUBLIC-листенере (:9090),
// поэтому conn сюда подаётся именно на :9090 (отдельный от authz/register conn'а на
// :9091). Owner-tuple lifecycle (RegisterResource/UnregisterResource) живёт в
// register_applier.go (drainer-half, iam internal :9091), а per-RPC authz-Check — в
// internal/check (iam internal :9091) — это разные консумируемые поверхности kaname.
package iam

import (
	"context"
	"log/slog"
	"time"

	"google.golang.org/grpc"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/auth"
	"github.com/PRO-Robotech/kacho/pkg/peer"
	"github.com/PRO-Robotech/kacho/pkg/retry"

	regerrors "github.com/PRO-Robotech/kacho/services/registry/internal/errors"
)

// iamCallTimeout — per-call deadline на ProjectExists (зеркалит
// check.checkTimeout, internal/check/check_client.go, 2s). ProjectService.Get —
// resource-read (не authz-Check) → более щедрый бюджет (5s). retry.OnUnavailable
// сам по себе НЕ ограничивает время одного зависшего Get — bounds только backoff
// МЕЖДУ попытками; без собственного дедлайна зависший-но-подключённый iam пинил
// бы Create-горутину навсегда (architecture.md "Per-call deadline на КАЖДОМ
// внешнем вызове"). Никогда не полагаемся на inbound ctx deadline вызывающего.
const iamCallTimeout = 5 * time.Second

// Client — adapter к kaname ProjectService поверх grpc-conn к PUBLIC-листенеру (:9090).
type Client struct {
	conn grpc.ClientConnInterface
}

// New оборачивает grpc-conn к kaname PUBLIC-листенеру (:9090 — ProjectService.Get).
// nil conn → методы отвечают Unavailable (мутация fail-closed).
func New(conn grpc.ClientConnInterface) *Client { return &Client{conn: conn} }

// ready — conn к kaname обязан быть подан (иначе fail-closed Unavailable).
func (c *Client) ready() error {
	if c.conn == nil {
		return regerrors.ErrUnavailable
	}
	return nil
}

// ProjectExists валидирует project-владельца на Create через ProjectService.Get.
// Семантика ошибок (existence-hiding: tenant не различает "нет" и "нет доступа"):
//
//	NotFound / PermissionDenied / InvalidArgument → ErrInvalidArg ("project not found")
//	Unavailable / DeadlineExceeded                → ErrUnavailable (мутация fail-closed)
//
// Исходящий ctx оборачивается auth.PropagateOutgoing — iam-side ProjectService.Get
// проходит per-RPC Check от реального вызывающего (не SystemPrincipal-fallback).
func (c *Client) ProjectExists(ctx context.Context, projectID string) error {
	if err := c.ready(); err != nil {
		return err
	}
	if projectID == "" {
		return regerrors.ErrInvalidArg
	}
	ctx, cancel := context.WithTimeout(ctx, iamCallTimeout)
	defer cancel()
	cli := iamv1.NewProjectServiceClient(c.conn)
	err := retry.OnUnavailable(ctx, func(ctx context.Context) error {
		_, gerr := cli.Get(auth.PropagateOutgoing(ctx), &iamv1.GetProjectRequest{ProjectId: projectID})
		return gerr
	})
	if err == nil {
		return nil
	}
	// Полосу выбирает носитель (pkg/peer): «владелец установил, что ссылка не
	// годится» — нет проекта / нет доступа / негодный по его мнению id.
	switch o := peer.Classify(err); {
	case o.RefusedReference():
		return regerrors.ErrInvalidArg
	case o.Transient():
		return regerrors.ErrUnavailable
	}
	// Непонятый ответ (в частности Unimplemented, если conn ошибочно указывает на
	// iam internal :9091, где ProjectService не зарегистрирован) наружу — фикс.
	// INTERNAL, но причина уходит в журнал: иначе ошибка маршрутизации теряется
	// немо (урок этого бага). Проза соседа наружу НЕ идёт — она несёт host/port.
	slog.Default().Error("registry: iam ProjectService.Get unexpected",
		"project_id", projectID, "outcome", peer.Classify(err).String(),
		"grpc_code", peer.PeerCode(err).String(), "grpc_msg", err.Error())
	return regerrors.ErrInternal
}

// AccountOf возвращает аккаунт проекта — зеркало, без которого строка учёта
// числа ресурсов невидима аккаунтной дельте (приёмка квот, V2-4).
//
// Идёт тем же `ProjectService.Get`, что и `ProjectExists`, — нового ребра работа
// не заводит. Полосы отказа те же: «владелец установил, что ссылка не годится»
// против «сосед не отвечает», и мутация на второй fail-closed.
//
// Пустой аккаунт при успешном ответе — НЕ «нет аккаунта», а нарушение контракта
// соседа: проект без аккаунта невыразим (`account_id` immutable и обязателен).
// Поэтому он отвергается здесь, а не уезжает в строку учёта, где ограничение
// схемы отвергло бы его позже и без имени предмета.
func (c *Client) AccountOf(ctx context.Context, projectID string) (string, error) {
	if err := c.ready(); err != nil {
		return "", err
	}
	if projectID == "" {
		return "", regerrors.ErrInvalidArg
	}
	ctx, cancel := context.WithTimeout(ctx, iamCallTimeout)
	defer cancel()
	cli := iamv1.NewProjectServiceClient(c.conn)

	var accountID string
	err := retry.OnUnavailable(ctx, func(ctx context.Context) error {
		p, gerr := cli.Get(auth.PropagateOutgoing(ctx), &iamv1.GetProjectRequest{ProjectId: projectID})
		if gerr != nil {
			return gerr
		}
		accountID = p.GetAccountId()
		return nil
	})
	if err != nil {
		switch o := peer.Classify(err); {
		case o.RefusedReference():
			return "", regerrors.ErrInvalidArg
		case o.Transient():
			return "", regerrors.ErrUnavailable
		}
		slog.Default().Error("registry: iam ProjectService.Get unexpected (account lookup)",
			"project_id", projectID, "outcome", peer.Classify(err).String(),
			"grpc_code", peer.PeerCode(err).String())
		return "", regerrors.ErrInternal
	}
	if accountID == "" {
		return "", regerrors.ErrFailedPrecondition
	}
	return accountID, nil
}
