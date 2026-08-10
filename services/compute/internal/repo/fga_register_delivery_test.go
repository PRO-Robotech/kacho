// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo

import (
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/ownerregister"
)

// TestUnmappableIntentProducesNoDelivery — намерение, которого нет (не
// отображаемый kind, пустой id, пустой проект), НЕ порождает строки доставки.
//
// # Откуда эта проба здесь
//
// Свойство проверялось у синхронного регистратора compute
// (`TestSyncRegistrar_Register_UnknownKindOrEmpty_NoOp`), пока регистратор был
// свой. Регистратор снят — форма доставки теперь одна на все сервисы
// (pkg/ownerregister), — но САМО СВОЙСТВО никуда не делось, оно лишь переехало
// на слой ниже: решение «регистрировать нечего» принимает эмиттер writer-
// транзакции, а не доставка. Проба переехала вместе с ним, а не была удалена
// вместе с файлом: удаление проверки вместе с её прежним домом и есть то, как
// свойство теряется молча.
//
// # Почему это важнее, чем выглядит
//
// Общий регистратор регистрацию с нулевой версией ОТВЕРГАЕТ. Если бы «нечего
// регистрировать» доезжало до него строкой с нулевым штампом, каждое создание
// ресурса неотображаемого вида давало бы отказ на пути, где отказывать не за
// что: ресурс закоммичен, регистрировать его действительно нечем. Поэтому
// пустота выражается ОТСУТСТВИЕМ строки, а не строкой с пустым значением.
func TestUnmappableIntentProducesNoDelivery(t *testing.T) {
	if got := registrationsOf(ownerregister.Registration{}); got != nil {
		t.Fatalf("нулевая строка превратилась в доставку %+v — общий регистратор отверг бы её "+
			"как регистрацию без версии, хотя регистрировать здесь нечего", got)
	}
}

// TestStampedIntentProducesExactlyOneDelivery — положительный контроль к пробе
// выше. Без него отрицание зеленело бы и на функции, которая не отдаёт НИЧЕГО
// никогда: «доставки нет» и «доставок не бывает» — разные утверждения.
func TestStampedIntentProducesExactlyOneDelivery(t *testing.T) {
	stamp := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	got := registrationsOf(ownerregister.Registration{
		Tuple:         ownerregister.Tuple{SubjectID: "project:prj-1", Relation: "project", Object: "compute_instance:ins-1"},
		SourceVersion: stamp,
	})
	if len(got) != 1 {
		t.Fatalf("проштампованное намерение дало %d строк доставки, ждали 1", len(got))
	}
	if !got[0].SourceVersion.Equal(stamp) {
		t.Fatalf("штамп writer-транзакции изменён по пути: %s вместо %s", got[0].SourceVersion, stamp)
	}
}
