// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package kacho

import "context"

// Контракт доступа к строкам учёта числа ресурсов.
//
// Приёмка `docs/specs/sub-phase-quota-v2-materialised-usage-acceptance.md`
// (APPROVED, раунд 2), DoD S2 п.3 и п.5.

// QuotaRow — одна строка учёта, какой её ЗАВОДИТ материализация.
//
// Потребления здесь нет НАМЕРЕННО: `used` принадлежит триггеру и никакому
// другому писателю (гейт дерева `TestQuotaUsedIsWrittenOnlyByItsTrigger`).
// Структура, несущая `Used`, немедленно сделала бы «записать потребление»
// выразимым на Go-стороне — а невыразимость этого и есть то, чем счётчик
// защищён от расхождения с реальностью.
type QuotaRow struct {
	// CarrierType — тип носителя учёта (`project`; в S3 к нему добавляются
	// родительские типы вроде `vpc.network`).
	CarrierType string
	// CarrierID — идентификатор носителя.
	CarrierID string
	// Kind — вид ресурса точечным токеном платформы (`vpc.network`).
	Kind string
	// Limit — разрешённая величина, как её РАЗРЕШИЛ владелец величин. Снимок, а
	// не второй авторитет.
	Limit int64
	// SourceScope — область, на которой величина победила (`DEFAULT` / `ACCOUNT`
	// / `PROJECT`). Ради неё арендатор отличает личное перекрытие от общего
	// правила, а синхронизатор — строки, которых касается дельта умолчания.
	SourceScope string
	// SourceScopeID — объект победившей области; пуст, когда победило умолчание.
	SourceScopeID string
	// LimitRevision — ревизия величины на момент снимка.
	LimitRevision int64
	// AccountID — зеркало аккаунта проекта. Пустым быть не может: строка без
	// зеркала невидима аккаунтной дельте, а снаружи это неотличимо от исправной
	// работы. Схема отвергает пустое значение.
	AccountID string
}

// QuotaReaderIface — совещательная полоса.
type QuotaReaderIface interface {
	// Admit сообщает, есть ли место под ещё одну строку этого вида у этого
	// носителя, НЕ занимая его.
	//
	// nil — место было в момент чтения. Отказ — `repo.ErrQuotaExceeded` либо
	// `repo.ErrQuotaNotProvisioned`, теми же текстом и признаком, что и отказ
	// авторитетной полосы: у обеих один производитель.
	//
	// Решением НЕ является: между этим ответом и вставкой помещается чужая
	// запись. Решает атомарное списание триггера.
	Admit(ctx context.Context, carrierType, carrierID, kind string) error
}

// QuotaWriterIface — материализация строк учёта.
type QuotaWriterIface interface {
	QuotaReaderIface
	// Materialize заводит отсутствующие строки учёта и не трогает имеющиеся.
	// Возвращает число заведённых — чтобы «ноль заведённых» было отличимо от
	// «не звали».
	Materialize(ctx context.Context, rows []QuotaRow) (int64, error)
}
