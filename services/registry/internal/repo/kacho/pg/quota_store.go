// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	corequota "github.com/PRO-Robotech/kacho/pkg/quota"
	"github.com/PRO-Robotech/kacho/pkg/quota/quotaread"
	"github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/quota"
)

// Адаптер порта `quota.Store` поверх пула.
//
// Приёмка `docs/specs/sub-phase-quota-v2-materialised-usage-acceptance.md`
// (APPROVED, раунд 2), DoD S4 п.1.
//
// ПОЧЕМУ ОТДЕЛЬНЫЙ ФАЙЛ, А НЕ МЕТОДЫ НА СУЩЕСТВУЮЩЕМ РЕПОЗИТОРИИ. Полоса не
// участвует ни в одной writer-транзакции ресурса: она стоит ПЕРЕД ней и после
// не участвует ни в чём. Повесив её на репозиторий реестра, мы бы намекнули, что
// её вызовы разделяют транзакцию с созданием реестра, — а они её не разделяют, и
// разделять не должны: материализация идёт своим оператором и обязана быть видна
// следующему запросу даже если сам create откатится.
//
// ПОЧЕМУ ТИПЫ ПЕРЕКЛАДЫВАЮТСЯ, А НЕ ПЕРЕИСПОЛЬЗУЮТСЯ. `quota.Row` живёт в
// use-case-слое и не знает про pgx; `QuotaRow` — контракт адаптера. Один тип на
// оба слоя означал бы, что use-case импортирует адаптер (запрещено
// dependency-rule), либо что адаптер диктует форму use-case'у.

// QuotaSchema — схема, в которой у этого владельца лежит таблица учёта.
//
// Экспортируется ради composition root: подъём синхронизатора величин работает
// над той же таблицей, и второе имя схемы разошлось бы с первым молча.
const QuotaSchema = schema

// QuotaStore — реализация порта совещательной полосы поверх пула.
type QuotaStore struct {
	pool *pgxpool.Pool
}

// Compile-time проверка: адаптер удовлетворяет порту use-case-слоя.
var _ quota.Store = (*QuotaStore)(nil)

// NewQuotaStore оборачивает пул.
//
// nil pool → nil адаптер: сборка без базы законна только в пробах, и «адаптера
// нет» обязано быть отличимо от «адаптер есть и молчит».
func NewQuotaStore(pool *pgxpool.Pool) *QuotaStore {
	if pool == nil {
		return nil
	}
	return &QuotaStore{pool: pool}
}

// QuotaAdmit — совещательный вопрос: есть ли место, НЕ занимая его.
func (s *QuotaStore) QuotaAdmit(ctx context.Context, carrierType, carrierID, kind string) error {
	return QuotaAdmit(ctx, s.pool, carrierType, carrierID, kind)
}

// ListStates отдаёт строки учёта носителя — то, что арендатор читает как свои
// квоты.
//
// Оператор ОБЩИЙ (`pkg/quota.ListStates`): таблица у всех владельцев одна и та
// же с точностью до имени схемы, и своя копия здесь разошлась бы с соседями на
// составе столбцов или на порядке — то есть там, где расхождение не ломает
// сборку и не видно глазом. Имя схемы берётся из той же константы, которой
// пользуются остальные операторы этого пакета.
func (s *QuotaStore) ListStates(
	ctx context.Context, carrierType, carrierID string,
) ([]quotaread.State, error) {
	return corequota.ListStates(ctx, s.pool, schema, carrierType, carrierID)
}

// MaterializeQuotas заводит отсутствующие строки учёта.
func (s *QuotaStore) MaterializeQuotas(ctx context.Context, rows []quota.Row) (int64, error) {
	return MaterializeQuotas(ctx, s.pool, toQuotaRows(rows))
}

// MaterializeNestedDefaults заводит проектный резолв вложенных видов.
func (s *QuotaStore) MaterializeNestedDefaults(ctx context.Context, rows []quota.Row) (int64, error) {
	return MaterializeNestedDefaults(ctx, s.pool, toQuotaRows(rows))
}

// toQuotaRows перекладывает строки use-case-слоя в контракт адаптера.
func toQuotaRows(rows []quota.Row) []QuotaRow {
	out := make([]QuotaRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, QuotaRow{
			CarrierType:   r.CarrierType,
			CarrierID:     r.CarrierID,
			Kind:          r.Kind,
			Limit:         r.Limit,
			SourceScope:   r.SourceScope,
			SourceScopeID: r.SourceScopeID,
			LimitRevision: r.LimitRevision,
			AccountID:     r.AccountID,
		})
	}
	return out
}
