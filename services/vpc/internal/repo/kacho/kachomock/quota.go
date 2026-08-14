// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package kachomock

import (
	"context"
	"fmt"
	"sync"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/helpers"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// Подставной учёт числа ресурсов для unit-проб use-case'ов.
//
// Приёмка `docs/specs/sub-phase-quota-v2-materialised-usage-acceptance.md`
// (APPROVED, раунд 2), DoD S2 п.3 и п.5.
//
// ЧТО ЭТОТ ДУБЛЁР ВОСПРОИЗВОДИТ ТОЧНО. Совещательную полосу: пустой набор строк
// означает ОТКАЗ `ErrQuotaNotProvisioned`, полная строка — `ErrQuotaExceeded`,
// и тексты собираются теми же форматами, что у единственного производителя в
// базе (`kacho_quota_refuse`, миграция 0041). Это несущее свойство: дублёр,
// молча пропускающий там, где настоящий отказывает, сделал бы невидимым ровно
// тот дефект, ради которого его подставляют — use-case, забывший спросить про
// место.
//
// ЧЕГО ОН НЕ ВОСПРОИЗВОДИТ, И ЭТО СКАЗАНО ВСЛУХ. Списания на вставке строки
// ресурса здесь НЕТ: оно живёт в триггере базы, а этот дублёр — не база, он не
// исполняет ни триггеров, ни внешних ключей, ни исключающих ограничений. Тем
// самым «use-case вставил ресурс, не спросив про место» на unit-уровне
// НЕОТЛИЧИМО от исправного пути, и закрывает это НЕ он, а интеграционные пробы
// пакета `repo/kacho/pg` (списание, возврат, конкуренция) плюс гейт дерева
// `TestQuotaUsedIsWrittenOnlyByItsTrigger`. Написано затем, чтобы следующий
// читатель не принял зелёный unit-прогон за доказательство, которого тот не даёт.

// quotaStore — строки учёта дублёра.
type quotaStore struct {
	mu   sync.Mutex
	rows map[quotaKey]*quotaRecord
}

type quotaKey struct {
	carrierType string
	carrierID   string
	kind        string
}

type quotaRecord struct {
	limit int64
	used  int64
}

func newQuotaStore() *quotaStore {
	return &quotaStore{rows: make(map[quotaKey]*quotaRecord)}
}

// SeedQuota заводит строку учёта напрямую — то, что на живом пути делает
// материализация. Проба, которой нужен «проект с местом», объявляет это ЯВНО:
// умолчания «место есть» у дублёра нет, потому что его нет и у продукта.
func (r *Repository) SeedQuota(carrierType, carrierID, kind string, limit, used int64) {
	st := r.quotaStore()
	st.mu.Lock()
	defer st.mu.Unlock()
	st.rows[quotaKey{carrierType, carrierID, kind}] = &quotaRecord{limit: limit, used: used}
}

// QuotaRows возвращает снимок заведённых строк — чтобы проба материализации
// утверждала ИСХОД (что заведено), а не факт вызова.
func (r *Repository) QuotaRows() map[string]int64 {
	st := r.quotaStore()
	st.mu.Lock()
	defer st.mu.Unlock()
	out := make(map[string]int64, len(st.rows))
	for k, v := range st.rows {
		out[k.carrierType+"/"+k.carrierID+"/"+k.kind] = v.limit
	}
	return out
}

// quotaMock реализует обе проекции контракта учёта.
type quotaMock struct {
	store *quotaStore
}

var (
	_ kacho.QuotaReaderIface = (*quotaMock)(nil)
	_ kacho.QuotaWriterIface = (*quotaMock)(nil)
)

// Admit — совещательная полоса; тексты те же, что у производителя в базе.
func (q *quotaMock) Admit(_ context.Context, carrierType, carrierID, kind string) error {
	q.store.mu.Lock()
	defer q.store.mu.Unlock()

	rec, ok := q.store.rows[quotaKey{carrierType, carrierID, kind}]
	if !ok {
		return fmt.Errorf("%w: project %s has no ceiling stated for %s",
			helpers.ErrQuotaNotProvisioned, carrierID, kind)
	}
	if rec.used >= rec.limit {
		return fmt.Errorf("%w: project %s has reached its limit of %d %s",
			helpers.ErrQuotaExceeded, carrierID, rec.limit, kind)
	}
	return nil
}

// Materialize заводит отсутствующие строки и не трогает имеющиеся — та же
// семантика `ON CONFLICT DO NOTHING`, что у настоящего.
func (q *quotaMock) Materialize(_ context.Context, rows []kacho.QuotaRow) (int64, error) {
	q.store.mu.Lock()
	defer q.store.mu.Unlock()

	var n int64
	for _, r := range rows {
		k := quotaKey{r.CarrierType, r.CarrierID, r.Kind}
		if _, exists := q.store.rows[k]; exists {
			continue
		}
		q.store.rows[k] = &quotaRecord{limit: r.Limit}
		n++
	}
	return n, nil
}
