// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/idempotencypg"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// dpopReplayStore — хранилище однократности предъявления ВНЕ процесса,
// приведённое к порту стража.
//
// # Почему адаптер, а не прямая реализация в хранилище
//
// Порт `middleware.DPoPReplayGuard` синхронный и без контекста: он живёт на
// пути проверки доказательства, где контекст запроса уже израсходован на
// разбор. Хранилищу контекст нужен, и оно получает СВОЙ — со сроком, чтобы
// неотвечающая база вешала не горутину запроса, а один вызов.
//
// # Почему срок именно здесь
//
// Проверка однократности стоит на пути КАЖДОГО запроса с доказательством
// владения. Без своего срока неотвечающее хранилище держало бы запрос до
// таймаута клиента, то есть превращало бы недоступность базы в недоступность
// края целиком.
type dpopReplayStore struct {
	store   *idempotencypg.Store
	ttl     time.Duration
	timeout time.Duration
}

func newDPoPReplayStore(store *idempotencypg.Store, ttl, timeout time.Duration) *dpopReplayStore {
	return &dpopReplayStore{store: store, ttl: ttl, timeout: timeout}
}

// Add допускает доказательство ровно один раз за окно свежести.
//
// Отказ хранилища — НЕ «повтор»: он возвращается своей ошибкой, и вызывающий
// отвергает запрос, называя причиной недоступность, а не подлог. Смешать их
// значило бы отдать клиенту отказ в доступе на нашу собственную неисправность
// — и скрыть от нас, что хранилище лежит.
func (s *dpopReplayStore) Add(jti string) error {
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	switch err := s.store.AddDPoPProof(ctx, jti, s.ttl); {
	case err == nil:
		return nil
	case errors.Is(err, idempotencypg.ErrDPoPReplay):
		return middleware.ErrDPoPReplay
	default:
		return fmt.Errorf("dpop replay store unavailable: %w", err)
	}
}

var _ middleware.DPoPReplayGuard = (*dpopReplayStore)(nil)

// dpopReplayStoreTimeout — срок одного обращения к общему хранилищу.
//
// Величина названа здесь, а не выведена из настроек: она про то, сколько край
// готов ждать СВОЮ базу на пути каждого запроса с доказательством владения, и
// подниматься вместе с окном свежести доказательства ей незачем. Полсекунды —
// на порядок больше обычного ответа и на порядок меньше терпения клиента.
const dpopReplayStoreTimeout = 500 * time.Millisecond
