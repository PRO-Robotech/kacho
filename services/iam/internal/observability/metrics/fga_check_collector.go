// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package metrics

import (
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

// FGACheckOutcomesMetric — имя семейства исходов вопроса к хранилищу прав.
const FGACheckOutcomesMetric = "kacho_iam_fga_check_outcomes_total"

// FGACheckCounts — снимок закрытого набора взаимоисключающих клеток.
//
// Взаимоисключающих намеренно: сумма равна числу заданных вопросов, поэтому
// «отказов не было» отличимо от «сюда никто не приходил».
type FGACheckCounts struct {
	// Answered — ответ с первой попытки (обычная жизнь).
	Answered uint64
	// Recovered — ответ НЕ с первой попытки: перебой поглощён повтором.
	Recovered uint64
	// Deadline — хранилище не ответило в срок (лечится временем).
	Deadline uint64
	// Connect — до хранилища не дозвонились (реплик не осталось).
	Connect uint64
	// Reset — связь оборвалась после установления, ответ обрезан.
	Reset uint64
	// ServerError — хранилище ответило 5xx.
	ServerError uint64
	// Decode — тело ответа не разобралось: по адресу отвечает не то.
	Decode uint64
	// Rejected — хранилище отвергло сам запрос (401/403/404): настройка.
	Rejected uint64
	// Canceled — ушёл вызывающий; отказ не хранилища.
	Canceled uint64
	// Other — форма, не попавшая ни в одну названную. Отличное от нуля —
	// НАХОДКА: набор форм отстал от действительности.
	Other uint64
}

type fgaCheckCollector struct {
	read func() FGACheckCounts
	desc *prometheus.Desc
}

// NewFGACheckCollector регистрирует читателя исходов вопроса к хранилищу прав.
//
// ЗАЧЕМ ЭТО СЕМЕЙСТВО (выведено из #720). Одиночный перебой хранилища прав
// приезжает арендатору кодом недоступности, и по одному этому коду «хранилище
// моргнуло», «до хранилища не дозвонились» и «связь оборвалась» НЕРАЗЛИЧИМЫ:
// установить, что именно было, удавалось только чтением журнала построчно — а
// путь чтения прав, на котором это случилось, не пишет в журнал вовсе. Клетки
// ниже отвечают на тот же вопрос числом, поэтому:
//
//   - `recovered` отличное от нуля означает, что хранилище потряхивает, —
//     и это видно ДО того, как перебой станет отказом арендатору, без единой
//     красной пробы;
//   - `deadline` против `connect` против `reset` разводят три источника,
//     которые прежде выглядели одинаково;
//   - `rejected` и `decode` держат настройку ОТДЕЛЬНО от сбоя: первая временем
//     не лечится, и прятать её под недоступностью запрещено
//     (security.md §Hardening-инвариант 8).
//
// nil-источник — ОТКАЗ по той же причине, что у соседних коллекторов: вечный
// ноль выглядит как работающее наблюдение и утверждает неправду о хранилище,
// которое просто забыли подключить.
func (r *Registry) NewFGACheckCollector(read func() FGACheckCounts) {
	if read == nil {
		panic("metrics: NewFGACheckCollector без источника величин — " +
			"вечный ноль неотличим от хранилища, которого никто не спрашивал")
	}
	c := &fgaCheckCollector{
		read: read,
		desc: prometheus.NewDesc(
			FGACheckOutcomesMetric,
			"Outcomes of the per-RPC authorization Check against the relation store, by "+
				"outcome ("+strings.Join(FGACheckOutcomeCells(), "|")+"). Cells are mutually "+
				"exclusive, so their sum is the number of questions asked and \"no failures\" "+
				"is distinguishable from \"never asked\". A non-zero \"recovered\" means a "+
				"transient blip was absorbed by the retry — the store is flapping, visible "+
				"before it becomes a tenant-facing refusal. Outage shapes are kept apart "+
				"(deadline|connect|reset) because they have different causes and different "+
				"remedies, and configuration (rejected|decode) is kept apart from outage "+
				"because it never heals on its own.",
			[]string{"outcome"}, nil,
		),
	}
	r.reg.MustRegister(c)
}

// FGACheckOutcomeCells — порядок клеток для описания семейства.
func FGACheckOutcomeCells() []string {
	return []string{
		"answered", "recovered",
		"deadline", "connect", "reset",
		"server_error", "decode", "rejected", "canceled", "other",
	}
}

// Describe — семейство видно и до первого сбора.
func (c *fgaCheckCollector) Describe(ch chan<- *prometheus.Desc) { ch <- c.desc }

// Collect отдаёт все клетки закрытого набора, читая живые счётчики.
func (c *fgaCheckCollector) Collect(ch chan<- prometheus.Metric) {
	n := c.read()
	for outcome, value := range map[string]uint64{
		"answered":     n.Answered,
		"recovered":    n.Recovered,
		"deadline":     n.Deadline,
		"connect":      n.Connect,
		"reset":        n.Reset,
		"server_error": n.ServerError,
		"decode":       n.Decode,
		"rejected":     n.Rejected,
		"canceled":     n.Canceled,
		"other":        n.Other,
	} {
		ch <- prometheus.MustNewConstMetric(c.desc, prometheus.CounterValue, float64(value), outcome)
	}
}
