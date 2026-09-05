// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

// Package quotaiam — тяга дельты величин у владельца, одна на всех владельцев
// ресурсов.
//
// # Почему это общий пакет
//
// Дельта — прямой перевод одного RPC соседа в нейтральную форму, которую понимает
// синхронизатор. Доменного в нём нет НИЧЕГО: ни имени схемы, ни токена каталога,
// ни носителя. Написанный пять раз порознь, он разошёлся бы на переводе области —
// то есть там, где расхождение молча меняет, какие строки снимка правит
// администратор.
//
// Пакет отделён от `pkg/quota` намеренно: тот нейтрален к транспорту и не
// импортирует сгенерённые стабы, поэтому синхронизатор проверяем без соседа и без
// сети. Здесь стабы нужны by construction — это и есть граница с соседом.
package quotaiam

import (
	"context"
	"time"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/auth"
	corequota "github.com/PRO-Robotech/kacho/pkg/quota"
	"github.com/PRO-Robotech/kacho/pkg/retry"
)

// defaultCallTimeout — предел времени одной попытки тяги.
//
// Собственный per-call deadline на КАЖДОЙ попытке: сосед, который жив, но не
// отвечает, обязан биться таймаутом, а не висеть до входящего контекста.
const defaultCallTimeout = 5 * time.Second

// Delta — тянущий дельту величин.
type Delta struct {
	cli     iamv1.InternalLimitServiceClient
	timeout time.Duration
}

// NewDelta собирает тянущего поверх клиента ВНУТРЕННЕГО слушателя владельца
// величин. Нулевой срок означает умолчание.
func NewDelta(cli iamv1.InternalLimitServiceClient, timeout time.Duration) *Delta {
	if timeout <= 0 {
		timeout = defaultCallTimeout
	}
	return &Delta{cli: cli, timeout: timeout}
}

// ListChangedSince тянет ДЕЛЬТУ изменений величин — то, чем снимок догоняет
// авторитет.
//
// Курсор непрозрачен и приезжает от владельца величин; пустая строка означает
// «с начала времён», то есть ровно то, что нужно проекции, ни разу не тянувшей
// дельту. Следующий курсор владелец возвращает ВСЕГДА, включая пустую страницу:
// тянущий, продвигавший курсор только на непустых страницах, перечитывал бы один
// и тот же хвост вечно, как только догнал.
//
// Неизвестная область НЕ приводится к умолчанию: она означает, что сосед сказал
// то, чего мы не понимаем, и синхронизатор обязан отказаться, а не догадаться.
// Пустая строка невалидна для `quota.Change`, поэтому такой ответ остановит
// проход, не сдвинув курсор, — и это громче, чем применить догадку.
func (d *Delta) ListChangedSince(
	ctx context.Context, cursor string, pageSize int32,
) ([]corequota.Change, string, error) {
	var (
		out  []corequota.Change
		next string
	)
	err := retry.OnUnavailable(ctx, func(ctx context.Context) error {
		callCtx, cancel := context.WithTimeout(ctx, d.timeout)
		defer cancel()
		resp, rerr := d.cli.ListChangedSince(auth.PropagateOutgoing(callCtx),
			&iamv1.ListChangedLimitsRequest{Cursor: cursor, PageSize: int64(pageSize)})
		if rerr != nil {
			return rerr
		}
		changes := resp.GetChanges()
		out = make([]corequota.Change, 0, len(changes))
		for _, ch := range changes {
			l := ch.GetLimit()
			out = append(out, corequota.Change{
				Kind:      l.GetKind(),
				Scope:     corequota.Scope(ScopeName(l.GetScope())),
				ScopeID:   l.GetScopeId(),
				Value:     l.GetValue(),
				Revision:  l.GetRevision(),
				Withdrawn: ch.GetWithdrawn(),
			})
		}
		next = resp.GetNextCursor()
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	return out, next, nil
}

// ScopeName переводит область контракта в строку, которую хранит строка учёта.
//
// Перевод, а не приведение числа к тексту: столбец `source_scope` ограничен
// схемой тремя значениями, и попади туда имя перечисления или число, строка не
// вставилась бы вовсе. Необъявленная область поэтому переводится в ПУСТУЮ
// строку, а не в выдуманное значение: пусть отвергает тот, кто умеет назвать
// предмет, а не принимает молча несуществующую область.
func ScopeName(s iamv1.Limit_Scope) string {
	switch s {
	case iamv1.Limit_DEFAULT:
		return string(corequota.ScopeDefault)
	case iamv1.Limit_ACCOUNT:
		return string(corequota.ScopeAccount)
	case iamv1.Limit_PROJECT:
		return string(corequota.ScopeProject)
	}
	return ""
}
