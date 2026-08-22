// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package idempotencypg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// dpopReplaySQL — допуск ОДНИМ оператором.
//
// Вставилось ⇒ это доказательство предъявлено впервые. Не вставилось ⇒ его уже
// предъявляли, и повтор отвергается. Проверки перед вставкой здесь нет и быть
// не может: пара «посмотреть — записать» под конкуренцией двух реплик пропустит
// оба предъявления (ban #10), а именно эта конкуренция и есть предмет.
//
// Просроченная запись перезаписывается, а не мешает: за пределами окна свежести
// повтор отвергается уже проверкой времени, и держать её как запрет значило бы
// запрещать значение навсегда.
const dpopReplaySQL = `
INSERT INTO kacho_gateway.dpop_replay (jti, expires_at)
VALUES ($1, now() + make_interval(secs => $2))
ON CONFLICT (jti) DO UPDATE
    SET expires_at = EXCLUDED.expires_at
    WHERE kacho_gateway.dpop_replay.expires_at <= now()
RETURNING TRUE`

// ErrDPoPReplay — доказательство уже предъявляли.
//
// Отдельная величина, а не общая ошибка: вызывающий обязан отличить «повтор» от
// «хранилище не ответило». Первое — отказ клиенту, второе — отказ края, и
// смешать их значило бы отдать клиенту 401 на нашу же недоступность.
var ErrDPoPReplay = errors.New("dpop: proof already presented")

// AddDPoPProof допускает доказательство ровно один раз за окно свежести.
//
// Возвращает nil при первом предъявлении, ErrDPoPReplay при повторном и ошибку
// хранилища во всех прочих случаях — fail-closed остаётся за вызывающим, у
// которого есть контекст запроса.
func (s *Store) AddDPoPProof(ctx context.Context, jti string, ttl time.Duration) error {
	if jti == "" {
		// Пустой `jti` — не «нет значения», а доказательство без опознавателя:
		// допустить его значило бы разрешить неограниченный повтор одного и
		// того же пустого ключа.
		return fmt.Errorf("dpop replay store: jti пуст — доказательство без опознавателя не допускается")
	}
	if ttl <= 0 {
		return fmt.Errorf("dpop replay store: окно свежести %v не положительно — запись истекла бы раньше, чем появилась", ttl)
	}

	var inserted bool
	err := s.pool.QueryRow(ctx, dpopReplaySQL, jti, ttl.Seconds()).Scan(&inserted)
	switch {
	case err == nil && inserted:
		return nil
	case errors.Is(err, pgx.ErrNoRows):
		// Ни вставки, ни перезаписи: живая строка с этим `jti` уже есть.
		return ErrDPoPReplay
	case err != nil:
		return fmt.Errorf("dpop replay store: %w", err)
	default:
		return ErrDPoPReplay
	}
}

// PurgeExpiredDPoPProofs убирает просроченные записи.
//
// Без уборки таблица растёт без границы; сборщик хранилища зовёт это вместе со
// своей уборкой, а не отдельным расписанием — два расписания об одном предмете
// разошлись бы молча.
func (s *Store) PurgeExpiredDPoPProofs(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM kacho_gateway.dpop_replay WHERE expires_at <= now()`)
	if err != nil {
		return 0, fmt.Errorf("dpop replay store: уборка: %w", err)
	}
	return tag.RowsAffected(), nil
}
