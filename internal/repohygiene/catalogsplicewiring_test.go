// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// catalogsplicewiring_test.go — гейт «гейт склейки обязан звать конвейер»
// (задача #1110, вторая половина действующего предиката).
//
// # Предмет: судья, чей вердикт не доходит ни до кого
//
// `gateway/scripts/check-catalog-splice.sh` утверждает то, чего не утверждает
// сосед (`check-domain-generation.sh`, гейт разреза, kacho#1110 первая
// половина): доля службы, СЛОЖЕННАЯ с долей платформы, восстанавливает
// вшитый каталог прав и вшитую таблицу маршрутов ПОБАЙТОВО, без пропуска и
// без лишнего. Сосед доказывает, что доля службы порождается в одиночку; он
// не обязан доказывать другое, и не доказывает.
//
// Класс «цель существует, объявлена CI-гейтом и не вызывается ни одним
// процессом» в этом же job'е (`permission-catalog · rest-route-table`) уже
// найден ЧЕТЫРЕЖДЫ до этого гейта — сама сверка копий каталога (#2041),
// таблица маршрутов, listauthz в четырёх сервисах и гейт разреза сам
// (kacho#1110, первая половина). Заводить пятый экземпляр молча — то есть
// написать судью и не провязать его — значило бы повторить его собственный
// урок в его же изменении.
//
// # Механизм ОБЩИЙ
//
// Обход живёт в gatetargetwiring.go и принимает КАТАЛОГ и ИМЯ ЦЕЛИ
// параметрами; тем же механизмом уже держатся `module-manifest-check`,
// `model-canon-check`, `permission-catalog-check` и `domain-generation-check`.
// Вторая копия обхода разошлась бы с первой молча — и разошлась бы там, где
// обе зелены.
//
// # Чем этот гейт НЕ является
//
// Он не порождает каталог, не сверяет доли и не дублирует утверждений судьи
// склейки (S1–S7 в самом скрипте). Его предмет — ПРОВЯЗКА: существует ли у
// цели вызывающий среди того, что исполняется само.
package repohygiene

import (
	"testing"
)

// catalogSpliceMakefileDir — каталог, чей Makefile объявляет судью склейки.
const catalogSpliceMakefileDir = "gateway"

// catalogSpliceTarget — цель, чей вердикт обязан кого-то достигать.
const catalogSpliceTarget = "catalog-splice-check"

// TestCatalogSpliceGateIsCalledByThePipeline — у гейта склейки обязан быть
// вызывающий среди того, что исполняется само.
//
// Что делать, если гейт сработал: провязать судью шагом конвейера — шагом с
// `working-directory: gateway` и телом `make catalog-splice-check` либо
// `make -C gateway catalog-splice-check`. Заводить второй судья склейки
// взамен провязки нельзя: утверждения судьи и его инъекция уже написаны, и
// вторая их копия разошлась бы с первой молча.
func TestCatalogSpliceGateIsCalledByThePipeline(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	w, err := readMakeTargetWiring(root, catalogSpliceMakefileDir, catalogSpliceTarget)
	if err != nil {
		t.Fatalf("провязка не прочитана: %v — вердикт беспредметен", err)
	}

	// ── ПРЕДПОСЫЛКИ ОБХОДА ──────────────────────────────────────────────────
	if w.Reach.RecipeLines == 0 {
		t.Fatalf("в %s/Makefile не прочитано ни одной строки рецепта — разбор сломан, "+
			"и вердикт о провязке беспредметен", catalogSpliceMakefileDir)
	}
	if w.WorkflowsRead == 0 {
		t.Fatal("не прочитано ни одного файла .github/workflows — гейт смотрит не туда")
	}
	if w.WorkflowStepsRead == 0 {
		t.Fatal("ни в одном файле конвейера не найдено шага с телом `run:` — разбор сломан, " +
			"и «вызова нет» означало бы «не прочитано ничего»")
	}
	// Положительный контроль: цель обязана быть НАЙДЕНА объявленной.
	if !w.Reach.Declared {
		t.Fatalf("цель %s не объявлена в %s/Makefile (прочитано %d строк рецепта) — судить склейку "+
			"некому, и молчание про провязку ничего не значит",
			catalogSpliceTarget, catalogSpliceMakefileDir, w.Reach.RecipeLines)
	}
	// Второй положительный контроль: достигающих обязано быть НЕ МЕНЬШЕ одной.
	if len(w.Reach.Reaching) == 0 {
		t.Fatalf("множество целей, достигающих %s, пусто — разбор достижимости сломан, "+
			"и «никто не зовёт» было бы верно ни на чём", catalogSpliceTarget)
	}

	for _, f := range findMakeTargetWiringFaults(w) {
		t.Errorf("%s", f)
	}

	t.Log(makeTargetWiringCensus(w))
}
