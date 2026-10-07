// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package authzwiring — сборка сужателя затронутых ресурсов notify-api
// (замысел issue-2924 З14): единственное место вызова `listnarrow.New` у
// развёртывания notify-api. Его зовёт корень notify-api с часами процесса —
// в пробах это управляемые часы обвязки; второй сборки сужателя нет. Пакет
// живёт под корнем процесса, которому принадлежит сужатель (правило Д74).
package authzwiring

import (
	"errors"
	"time"

	"github.com/PRO-Robotech/corelib/listnarrow"

	"github.com/PRO-Robotech/kacho/services/notify/cmd/notify-api/internal/config"
	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/notify/api/publicnotice"
	"github.com/PRO-Robotech/kacho/services/notify/internal/notice/rules"
)

// cacheMaxEntries — ёмкость окна положительных вердиктов: умолчание
// `listnarrow`, записанное явно (З14 п.2) — величина выбрана, а не унаследована.
const cacheMaxEntries = 10000

// NewListNarrower собирает сужатель над клиентом пакетной проверки владельца
// модели. Окно — значение ручки без преобразования; ноль и отрицательное —
// ошибка: страж старта их не пропускает, и умолчание фундамента вместо
// выбранного окна здесь не подставляется (З14 п.2). Мягкого пропуска и
// аварийного обхода нет (З13 п.3): недоступный владелец — отказ чтения.
func NewListNarrower(cli listnarrow.AuthorizeClient, cfg config.ListFilter, now func() time.Time) (*listnarrow.Narrower, error) {
	if cli == nil {
		return nil, errors.New("authzwiring: клиент пакетной проверки не задан")
	}
	if cfg.CacheTTL <= 0 {
		return nil, errors.New("authzwiring: окно сужателя неположительно — ручка KACHO_NOTIFY_LIST_FILTER_CACHE_TTL не прошла стража")
	}
	if cfg.CheckTimeout <= 0 {
		return nil, errors.New("authzwiring: срок пакетного вопроса неположителен")
	}
	if now == nil {
		return nil, errors.New("authzwiring: часы сужателя не заданы")
	}
	relations := map[string][]string{}
	for _, t := range rules.TenantResourceTypes() {
		relations[t] = []string{publicnotice.RelationRead}
	}
	n := listnarrow.New(cli, listnarrow.Config{
		Relations:             relations,
		Timeout:               cfg.CheckTimeout,
		CacheTTL:              cfg.CacheTTL,
		CacheMaxEntries:       cacheMaxEntries,
		SoftPassOnPeerFailure: false,
		Breakglass:            false,
	})
	return n.WithClock(now), nil
}
