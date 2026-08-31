// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package kachomock

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"github.com/PRO-Robotech/kacho/pkg/quota/quotadetail"
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
	// nested — проектный резолв вложенных величин. Отдельно от строк учёта, как
	// и в настоящем хранилище: у вложенного вида есть проектная ВЕЛИЧИНА и нет
	// проектного ПОТРЕБЛЕНИЯ.
	nested map[quotaKey]int64
}

type quotaKey struct {
	carrierType string
	carrierID   string
	kind        string
}

type quotaRecord struct {
	limit int64
	used  int64
	// Область, на которой величина победила, и её объект. Заполняются
	// материализацией из ответа владельца величин; `SeedQuota` их не назначает —
	// проба, которой они важны, заводит строку тем же путём, что продукт.
	sourceScope   string
	sourceScopeID string
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
		// Величины приклеиваются и здесь: дублёр, отдающий МЕНЬШЕ настоящего,
		// делает невидимым ровно то, ради чего его подставляют — проба на нём
		// зеленела бы при потерянных величинах (задача продукта #1605).
		return quotadetail.Attach(
			fmt.Errorf("%w: project %s has no ceiling stated for %s",
				helpers.ErrQuotaNotProvisioned, carrierID, kind),
			mockRefusalDetail(carrierType, carrierID, kind, nil))
	}
	if rec.used >= rec.limit {
		limit := rec.limit
		used := rec.used
		return quotadetail.Attach(
			fmt.Errorf("%w: project %s has reached its limit of %d %s",
				helpers.ErrQuotaExceeded, carrierID, rec.limit, kind),
			mockRefusalDetail(carrierType, carrierID, kind, []int64{limit, used}))
	}
	return nil
}

// mockRefusalDetail собирает `DETAIL` в той же форме, в какой её производит
// единственный производитель отказа (`kacho_quota_refuse`). Форма повторяется
// ДОСЛОВНО: дублёр, чей отказ беднее настоящего, скрывает дефект, который сам же
// и кормит. `amounts` — пара «предел, занятое» либо nil для полосы, где потолок
// не назван: величин у неё не существует, и ноль вместо них был бы числом,
// которого никто не считал.
func mockRefusalDetail(carrierType, carrierID, kind string, amounts []int64) string {
	obj := map[string]any{
		"carrier_type": carrierType,
		"carrier_id":   carrierID,
		"kind":         kind,
	}
	if len(amounts) == 2 {
		obj["limit"] = amounts[0]
		obj["used"] = amounts[1]
	}
	b, err := json.Marshal(obj)
	if err != nil {
		// Величин не будет — отказ от этого отказом быть не перестаёт.
		return ""
	}
	return string(b)
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
		q.store.rows[k] = &quotaRecord{
			limit:         r.Limit,
			sourceScope:   r.SourceScope,
			sourceScopeID: r.SourceScopeID,
		}
		n++
	}
	return n, nil
}

// ListStates отдаёт строки носителя, отсортированные ПО ВИДУ — тем же порядком,
// каким их отдаёт настоящий (`ORDER BY kind`).
//
// Порядок здесь не косметика и не совпадение: карта Go обходится в случайном
// порядке by design, поэтому дублёр без явной сортировки отдавал бы виды
// вперемешку и проба на порядок зеленела бы через раз — то есть проверяла бы
// не продукт, а удачу. Дублёр обязан выполнять контракт настоящего, включая
// объявленный порядок.
func (q *quotaMock) ListStates(_ context.Context, carrierType, carrierID string) ([]kacho.QuotaState, error) {
	q.store.mu.Lock()
	defer q.store.mu.Unlock()

	out := make([]kacho.QuotaState, 0, len(q.store.rows))
	for k, rec := range q.store.rows {
		if k.carrierType != carrierType || k.carrierID != carrierID {
			continue
		}
		out = append(out, kacho.QuotaState{
			Kind:          k.kind,
			Limit:         rec.limit,
			Used:          rec.used,
			SourceScope:   rec.sourceScope,
			SourceScopeID: rec.sourceScopeID,
			CarrierType:   k.carrierType,
			CarrierID:     k.carrierID,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	return out, nil
}

// MaterializeNestedDefaults запоминает проектный резолв вложенных видов.
//
// Догоняющих строк учёта родителей здесь НЕТ, и это не упрощение: у подставного
// хранилища нет таблицы родителей, поэтому «догнать» ему нечего. Дублёр обязан
// не быть СНИСХОДИТЕЛЬНЕЕ настоящего — а он и не бывает: догоняющая запись
// только ДОБАВЛЯЕТ строки учёта, поэтому её отсутствие здесь делает подставное
// хранилище строже, а не мягче. Свойство догона проверяется интеграционной
// пробой на настоящей базе, и другого места у него нет.
func (q *quotaMock) MaterializeNestedDefaults(
	_ context.Context, rows []kacho.QuotaRow,
) (int64, error) {
	q.store.mu.Lock()
	defer q.store.mu.Unlock()

	var n int64
	for _, r := range rows {
		if q.store.nested == nil {
			q.store.nested = map[quotaKey]int64{}
		}
		q.store.nested[quotaKey{r.CarrierType, r.CarrierID, r.Kind}] = r.Limit
		n++
	}
	return n, nil
}
