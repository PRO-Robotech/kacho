// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pbconv

// operation_timestamp_truncation_test.go — метки времени Operation на проводе
// усечены до секунды.
//
// # Предмет
//
// Конвенция продукта: «в proto-ответе truncate до СЕКУНД; БД хранит
// микросекунды» — и это относится к каждому ресурсу И каждой под-записи, а не
// только к «главным» ресурсам. Operation — ответ КАЖДОЙ мутации всех сервисов,
// то есть самая частая поверхность, на которой микросекунды могут утечь.
//
// # Почему проба смотрит на остаток, а не на равенство
//
// Утверждение «значение усечено» проверяется остатком от секунды, а не
// сравнением с заранее усечённым образцом: сравнение с образцом зеленеет и
// тогда, когда усечения нет, но вход случайно оказался круглым. Вход здесь
// намеренно НЕ круглый — микросекунды выставлены явно, — и положительный
// контроль рядом утверждает, что секундная часть при этом сохранена (иначе
// «усечено» было бы неотличимо от «обнулено»).

import (
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/operations"
)

func TestOperationToProto_TimestampsAreTruncatedToSecond(t *testing.T) {
	// Вход с микросекундами — ровно то, что отдаёт Postgres.
	created := time.Date(2026, 8, 11, 10, 20, 30, 123456000, time.UTC)
	modified := time.Date(2026, 8, 11, 10, 20, 31, 987654000, time.UTC)

	got := OperationToProto(&operations.Operation{
		ID:         "op-1",
		CreatedAt:  created,
		ModifiedAt: modified,
	})
	if got == nil {
		t.Fatal("конвертер вернул nil на непустой операции")
	}

	for _, c := range []struct {
		what string
		ts   interface{ AsTime() time.Time }
		want time.Time
	}{
		{"created_at", got.GetCreatedAt(), created},
		{"modified_at", got.GetModifiedAt(), modified},
	} {
		if c.ts == nil {
			t.Fatalf("%s не заполнен — проверять усечение не на чем", c.what)
		}
		v := c.ts.AsTime()
		if v.Nanosecond() != 0 {
			t.Errorf("%s = %v: доли секунды уехали на провод (%d нс). Конвенция продукта "+
				"требует усечения до секунды в proto-ответе — БД хранит микросекунды, клиент "+
				"их не видит", c.what, v, v.Nanosecond())
		}
		// Положительный контроль: усечено, а не обнулено.
		if !v.Equal(c.want.Truncate(time.Second)) {
			t.Errorf("%s = %v, ожидалось %v — значение не усечено, а потеряно",
				c.what, v, c.want.Truncate(time.Second))
		}
	}
}
