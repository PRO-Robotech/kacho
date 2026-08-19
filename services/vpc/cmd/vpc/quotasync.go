// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	corequota "github.com/PRO-Robotech/kacho/pkg/quota"
)

// Синхронизатор величин: то, чем снимок предела догоняет авторитет.
//
// Сборка ОБЩАЯ (`pkg/quota.StartLimitSyncer`) — доменного в подъёме тянущего
// только имя схемы. Прежде тело стояло здесь целиком, и это было верно ровно до
// появления второго владельца с той же нуждой: пять копий подъёма разошлись бы
// на обработке отказа первого прохода, то есть там, где расхождение видно только
// на недоступном соседе.

// limitDeltaSource — то единственное, что синхронизатору нужно от соседа.
//
// Объявлен здесь, у потребителя: composition root не обязан знать, чем именно
// клиент отличается от порта, а порт, объявленный у поставщика, связывал бы
// владельца типа с формой чужого клиента.
type limitDeltaSource interface {
	ListChangedSince(ctx context.Context, cursor string, pageSize int32) ([]corequota.Change, string, error)
}

// startQuotaLimitSyncer поднимает тянущего в фоне и возвращает его останов.
func startQuotaLimitSyncer(
	ctx context.Context,
	pool *pgxpool.Pool,
	src limitDeltaSource,
	schema string,
	rec corequota.Recorder,
	logger *slog.Logger,
) (func(), error) {
	return corequota.StartLimitSyncer(ctx, pool, src, schema, rec, corequota.Config{}, logger)
}
