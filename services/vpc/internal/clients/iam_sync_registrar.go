// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package clients

import (
	"context"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/ownerregister"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/fgaregister"
)

// SyncRegistrar — перевод намерения vpc в общую форму доставки
// ([ownerregister.Registrar]) и ничего больше.
//
// ПОЧЕМУ ЗДЕСЬ БОЛЬШЕ НЕТ НИ ЦИКЛА, НИ СРОКА, НИ СБОРКИ ЗАПРОСА. Всё это стояло
// копией у пяти сервисов и разошлось по пяти осям сразу — включая ту, ради
// которой доставка вообще несёт версию. Общая форма и её обоснования живут в
// godoc пакета ownerregister; здесь остаётся только то, что у vpc своё:
// раскладка Item'ов в строки доставки.
//
// Версия — параметр, а не часы: её проштамповала БД внутри writer-транзакции
// (`FGARegisterEmitter.EmitRegister` возвращает `now()` через RETURNING). vpc был
// ЕДИНСТВЕННЫМ из пяти, кто её протаскивал; остальные четыре читали часы в момент
// доставки, отчего гашение повторной доставки у принимающей стороны срабатывало
// только в одном порядке. Свойство держит гейт
// `internal/repohygiene.TestOwnerRegistrationCarriesWriterTxVersion`.
type SyncRegistrar struct {
	delivery *ownerregister.Registrar
}

// NewSyncRegistrar собирает registrar поверх IAMRegisterRPC (InternalIAMServiceClient
// или его узкое подмножество). Нулевой клиент — отказ, а не пустая операция
// (см. [ownerregister.ErrNoClient]).
func NewSyncRegistrar(cli IAMRegisterRPC) (*SyncRegistrar, error) {
	d, err := ownerregister.New(cli)
	if err != nil {
		return nil, err
	}
	return &SyncRegistrar{delivery: d}, nil
}

// Register доставляет каждый Item набора, неся ТУ ЖЕ версию, которой БД
// проштамповала durable-намерение внутри writer-транзакции.
//
// Одна версия на все Item'ы здесь безвредна: Item'ы одного вызова vpc адресуют
// РАЗНЫЕ объекты (сеть, её системная группа безопасности, её таблица
// маршрутизации), а гейт редоставки у принимающей стороны ключуется на строке
// зеркала — то есть на объекте. Совпадение версий у разных объектов ничего не
// схлопывает. Сервису, который шлёт на ОДИН объект несколько отношений, версию
// обязана нести каждая строка отдельно — см. godoc
// [ownerregister.Registration].
func (s *SyncRegistrar) Register(ctx context.Context, items []fgaregister.Item, sourceVersion time.Time) error {
	regs := make([]ownerregister.Registration, 0, len(items))
	for _, it := range items {
		regs = append(regs, ownerregister.Registration{
			Tuple: ownerregister.Tuple{
				SubjectID: it.Tuple.SubjectID,
				Relation:  it.Tuple.Relation,
				Object:    it.Tuple.Object,
			},
			Labels:          it.Labels,
			ParentProjectID: it.ParentProjectID,
			// Цепь предков — та же, что на пути очереди: обе доставки одного
			// намерения обязаны нести одно содержание, иначе повтор стирает
			// то, что записала первая.
			ParentChain:   ownerregister.ParentChain(nil, it.ParentProjectID, ""),
			SourceVersion: sourceVersion,
		})
	}
	return s.delivery.Register(ctx, regs)
}

// Compile-time check.
var _ fgaregister.Registrar = (*SyncRegistrar)(nil)
