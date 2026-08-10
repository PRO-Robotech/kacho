// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// sync_registrar.go — перевод намерений registry в ОБЩУЮ форму синхронной
// доставки регистрации (pkg/ownerregister). Своей формы у registry больше нет.
package iam

import (
	"context"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/ownerregister"

	registry "github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/api/registry"
	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
)

// SyncRegistrar — перевод намерений registry в общую форму доставки.
//
// # Водяного знака здесь больше нет — у него был предмет, и предмет исчез
//
// Прежде этот путь версию НЕ СЛАЛ ВОВСЕ и потому не гейтился НИКОГДА: владелец
// прав требует ПОЛОЖИТЕЛЬНОГО доказательства редоставки — версии, которую можно
// сравнить, — и без неё открывается в сторону работы. registry платил за обе
// доставки на каждом создании.
//
// Заменой служил ВОДЯНОЙ ЗНАК: часы момента доставки, проставляемые РОВНО ОДИН
// РАЗ НА ОБЪЕКТ, на последнем его tuple'е, а всем предыдущим версия не
// доставалась. Конструкция была вынужденной и решала настоящую задачу: гейт
// редоставки ключуется на строке зеркала, а она ПО ОБЪЕКТУ, тогда как Create
// реестра шлёт на ОДИН объект два tuple'а (указатель принадлежности, затем
// владелец-создатель). Дай версию каждому — второй вызов зеркала не меняет,
// неотличим от редоставки и проглатывается вместе с постановкой владельца.
//
// Предмет исчез, потому что появилась НАСТОЯЩАЯ версия: триггер очереди штампует
// `clock_timestamp()` КАЖДОЙ строке внутри writer-транзакции, и эмиттер теперь
// возвращает этот штамп вызывающему. Различать tuple'ы одного объекта умеет
// [stepSourceVersion] — ТОТ ЖЕ шаг, которым их различает дренаж этой же
// очереди. Значит обе доставки одного tuple'а несут ОДНО значение (гашение
// срабатывает, какая бы ни пришла первой), а два tuple'а одного объекта
// различимы между собой (второй не проглатывается).
//
// Это и есть причина, по которой знак снят, а не «упрощён»: он был обходом
// отсутствующего маркера. Обход, переживший свой предмет, — не решение, а второе
// место об одном предмете, из которых верно одно.
type SyncRegistrar struct {
	delivery *ownerregister.Registrar
}

// NewSyncRegistrar собирает registrar. Нулевой клиент — ОТКАЗ (см.
// [ownerregister.ErrNoClient]): пустая операция неотличима от исправно
// работающего ускорителя, который никогда ничего не ускорял.
func NewSyncRegistrar(cli RegisterResourceClient) (*SyncRegistrar, error) {
	d, err := ownerregister.New(cli)
	if err != nil {
		return nil, err
	}
	return &SyncRegistrar{delivery: d}, nil
}

// Register доставляет каждый tuple каждого намерения, неся версию, которую БД
// проштамповала строке очереди ВНУТРИ writer-транзакции, шагнутую по tuple'ам
// тем же шагом, что и на пути дренажа.
//
// Намерение без версии (строка, поставленная в очередь до появления маркера)
// ПРОПУСКАЕТСЯ, а не отправляется без версии: общая форма доставки регистрацию
// без маркера отвергает, и молча превращать «версии нет» в отказ по каждому
// tuple'у значило бы шуметь там, где дренаж и так доведёт дело.
func (s *SyncRegistrar) Register(ctx context.Context, intents []domain.RegisterIntent) error {
	var regs []ownerregister.Registration
	for _, intent := range intents {
		if intent.SourceVersion.IsZero() {
			continue
		}
		for seq, t := range intent.Tuples {
			regs = append(regs, ownerregister.Registration{
				Tuple: ownerregister.Tuple{
					SubjectID: t.SubjectID,
					Relation:  t.Relation,
					Object:    t.Object,
				},
				TraceID:         intent.ResourceID,
				Labels:          intent.Labels,
				ParentProjectID: intent.ParentProjectID,
				SourceVersion:   intent.SourceVersion.Time.Add(time.Duration(seq) * sourceVersionStep),
			})
		}
	}
	return s.delivery.Register(ctx, regs)
}

// Compile-time check: adapter удовлетворяет use-case-порту.
var _ registry.SyncRegistrar = (*SyncRegistrar)(nil)
