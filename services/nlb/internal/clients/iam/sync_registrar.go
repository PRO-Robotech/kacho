// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// sync_registrar.go — перевод намерения nlb в ОБЩУЮ форму синхронной доставки
// регистрации (pkg/ownerregister). Своей формы у nlb больше нет.
package iam

import (
	"context"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/ownerregister"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// Registrar — порт синхронной регистрации, потребляемый create/update-use-case'ами
// (loadbalancer/listener/targetgroup). Реализация — [SyncRegistrar] поверх общей
// формы доставки [ownerregister.Registrar].
//
// `intentVersion` — штамп, который БД поставила durable-намерению ВНУТРИ
// writer-транзакции (его возвращает FGARegisterEmitter.Emit). Обе доставки одной
// регистрации обязаны нести ОДНО значение: принимающая сторона гасит повторную
// строгим монотонным сравнением версий, и на свежих часах гашение зависело бы от
// того, кто выиграл гонку.
//
// Семантика вызывающего — BEST-EFFORT: use-case зовёт Register ПОСЛЕ durable
// commit'а ресурса и его durable-намерения; отказ ЛОГИРУЕТ и ГЛОТАЕТ — не
// пробрасывает, не гейтит Operation.done. Гейт видимости на done = phantom-ресурс
// (ban #9) — запрещён.
type Registrar interface {
	Register(ctx context.Context, intent domain.FGARegisterIntent, intentVersion time.Time) error
}

// SyncRegistrar — перевод намерения nlb в общую форму доставки
// ([ownerregister.Registrar]) и ничего больше.
//
// ЧЕГО ЗДЕСЬ БОЛЬШЕ НЕТ И ПОЧЕМУ. Стояли: собственный цикл по tuple'ам,
// собственный срок вызова, собственная сборка запроса, собственный
// классификатор отказа и собственные часы для маркера версии. Всё это было
// копией, разошедшейся с четырьмя соседями — а комментарий рядом объявлял
// паритет с vpc, которого не было. Хуже того, тот же комментарий, разбирая
// первую ложь, вводил вторую: он ссылался на шагающую по tuple'ам версию «в
// NewRegisterApplier» nlb, тогда как такой функции у nlb нет — она живёт у
// registry (предикат: `git grep -n stepSourceVersion` даёт один файл, и это не
// nlb). Ложное заявление о паритете и есть то, из-за чего расхождение прожило
// незамеченным.
//
// Классификатор снят по существу, а не для краткости: его единственная
// снисходительная ветка считала успехом `AlreadyExists`, а такого кода контракт
// RegisterResource не производит вовсе — ветка молчала на том, ради чего
// написана. Отказ теперь сурфейсится целиком (общая форма), и лог остаётся
// единственным признаком поломки синхронного пути.
type SyncRegistrar struct {
	delivery *ownerregister.Registrar
}

// NewSyncRegistrar собирает registrar. Нулевой клиент — ОТКАЗ, а не пустая
// операция: пустая операция неотличима от исправно работающего ускорителя,
// который никогда ничего не ускорял (см. [ownerregister.ErrNoClient]).
func NewSyncRegistrar(cli RegisterResourceClient) (*SyncRegistrar, error) {
	d, err := ownerregister.New(cli)
	if err != nil {
		return nil, err
	}
	return &SyncRegistrar{delivery: d}, nil
}

// Register доставляет каждый tuple намерения, неся штамп writer-транзакции.
//
// Одна версия на все tuple'ы намерения здесь безвредна: намерение nlb несёт на
// объект РОВНО ОДИН tuple (containment `project`), а гейт редоставки у
// принимающей стороны ключуется на строке зеркала — то есть на объекте.
// Появится второй tuple на тот же объект — версию обязана нести каждая строка
// отдельно, см. godoc [ownerregister.Registration].
func (s *SyncRegistrar) Register(ctx context.Context, intent domain.FGARegisterIntent, intentVersion time.Time) error {
	regs := make([]ownerregister.Registration, 0, len(intent.Tuples))
	for _, t := range intent.Tuples {
		regs = append(regs, ownerregister.Registration{
			Tuple: ownerregister.Tuple{
				SubjectID: t.SubjectID,
				Relation:  t.Relation,
				Object:    t.Object,
			},
			TraceID:         intent.ResourceID,
			Labels:          intent.Labels,
			ParentProjectID: intent.ParentProjectID,
			ParentAccountID: intent.ParentAccountID,
			// Цепь предков — та же, что на пути очереди: обе доставки одного
			// намерения обязаны нести одно содержание, иначе повтор стирает
			// то, что записала первая.
			ParentChain:   ownerregister.ParentChain(nil, intent.ParentProjectID, intent.ParentAccountID),
			SourceVersion: intentVersion,
		})
	}
	return s.delivery.Register(ctx, regs)
}

// Compile-time check.
var _ Registrar = (*SyncRegistrar)(nil)
