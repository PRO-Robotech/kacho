// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package proxy

import (
	"log/slog"
	"sync/atomic"
)

// RouteRefusalObserver — наблюдаемое отказа по маршруту: своя величина и своя
// запись.
//
// # Зачем отдельная величина
//
// Отказ «этот слушатель такого не обслуживает» — не решение о правах: ни
// каталог, ни личность, ни модель не спрашивались. Слитый с отказом в правах, он
// записал бы перебор административной поверхности в решения модели, а он ими не
// является. Тот же довод и та же развязка, что у полосы `Unserved` на полосе
// HTTP; здесь — её нативный близнец.
//
// # Зачем запись, если есть журнал доступа
//
// Затем, что в журнале этот отказ выглядит как NotFound и НЕОТЛИЧИМ от NotFound
// любого другого происхождения — в том числе от честного «такого метода нет».
// Неотличимость снаружи и есть предмет отказа; изнутри она не нужна и вредна.
type RouteRefusalObserver struct {
	refused atomic.Uint64
	logger  *slog.Logger
}

// NewRouteRefusalObserver создаёт наблюдателя. Журнал обязателен: наблюдатель
// без него — половина предмета.
func NewRouteRefusalObserver(logger *slog.Logger) *RouteRefusalObserver {
	if logger == nil {
		logger = slog.Default()
	}
	return &RouteRefusalObserver{logger: logger}
}

// Refused — сколько отказов по маршруту произведено за жизнь процесса.
func (o *RouteRefusalObserver) Refused() uint64 {
	if o == nil {
		return 0
	}
	return o.refused.Load()
}

// record отмечает один отказ. Наблюдатель может быть нулевым — звено остаётся
// работоспособным, но тогда отказ снова становится молчаливым, и это видно в
// корне, а не прячется здесь.
func (o *RouteRefusalObserver) record(method string) {
	if o == nil {
		return
	}
	o.refused.Add(1)
	// Уровень — тот, который процесс печатает (порог корня — Info). Имя метода
	// приходит из таблицы маршрутов процесса, а не от запросчика, поэтому
	// усечения не требует: управлять его длиной снаружи нельзя.
	o.logger.Info("proxy: internal route refused on the external listener", "method", method)
}
