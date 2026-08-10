// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package kacho

import (
	"context"
	"time"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// FGARegisterEmitter — emit одного FGA-register-intent в
// `kacho_nlb.fga_register_outbox`. Использует pgx.Tx writer'а, поэтому
// INSERT/DELETE ресурса + register-intent commit'ятся атомарно одной writer-tx
// (Вариант A — no dual-write, в отличие от прежнего best-effort
// fgawrite после Commit).
//
// eventType ∈ {domain.FGAEventRegister, domain.FGAEventUnregister}. CHECK
// constraint в `fga_register_outbox` (миграция 0002) защищает от typo →
// SQLSTATE 23514 → ErrInvalidArg в mapPgErr.
//
// Пустой набор tuple (intent.Tuples == 0) — no-op (строка не пишется): нечего
// регистрировать (напр. system-initiated Create без creator-tuple, но с
// project-hierarchy — набор непуст; полностью пустой набор не возникает в
// нормальном флоу, но guard защищает от записи пустой строки).
//
// ВОЗВРАЩАЕТСЯ ШТАМП, КОТОРЫЙ БД ПОСТАВИЛА ЭТОЙ СТРОКЕ внутри writer-транзакции
// (`now() + <порядковый номер> µs`, см. реализацию). Его обязана нести и
// СИНХРОННАЯ доставка того же намерения: обе доставки приходят к владельцу прав,
// и он гасит повторную строгим монотонным сравнением версий — при одном значении
// гашение срабатывает, какая бы ни пришла первой. Синхронный путь здесь штамповал
// собственные часы момента доставки, отчего гашение работало только в одном
// порядке. Пустой набор tuple → нулевое время (строки не было).
type FGARegisterEmitter interface {
	Emit(ctx context.Context, eventType string, intent domain.FGARegisterIntent) (time.Time, error)
}
