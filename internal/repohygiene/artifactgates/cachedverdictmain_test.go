// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// cachedverdictmain_test.go — пакет проверок ДЕРЕВА отказывается работать на прогоне,
// результат которого `go test` положит в кеш.
//
// # Почему страж стоит здесь, а не в каждой проверке
//
// Проверок дерева в этом пакете сотни, и часть из них берёт состав НЕ через
// [treecorpus], а вызовом git напрямую. Страж внутри конструкторов treecorpus
// такую проверку не накрывает: отбор `-run` по одному имени — самый частый
// способ отладки гейта, и именно он давал бы `ok (cached)` над красным деревом.
// TestMain накрывает пакет by construction, включая проверки, которых ещё нет.
//
// # Цена названа, а не спрятана
//
// Под стражем оказываются и пробы этого пакета, дерева НЕ судящие: их вердикт
// кешировался бы законно. Плата — их повторное исполнение. Она принята
// сознательно: разделить пробы по этому признаку внутри одного пакета нечем —
// TestMain у тестового бинаря один, — а сортировать их вручную значило бы
// держать перечень, который разойдётся с деревом молча.
//
// Разбор класса, замеры и дискриминатор — [treecorpus] (cachedverdict.go).

package artifactgates

import (
	"fmt"
	"os"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

func TestMain(m *testing.M) {
	if msg := treecorpus.CachedVerdictRefusal(); msg != "" {
		fmt.Fprintln(os.Stderr, "internal/repohygiene/artifactgates: "+msg)
		os.Exit(1)
	}
	os.Exit(m.Run())
}
