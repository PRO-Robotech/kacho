// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// poolcloseunbounded_test.go — пул в пробе закрывается С ПРЕДЕЛОМ, а не «когда-нибудь».
//
// # Предмет
//
// `pgxpool.Pool.Close` ждёт возврата ВСЕХ выданных соединений. Проба, упавшая
// внутри открытой транзакции, соединение не вернёт: `require.NoError` →
// `t.FailNow` → `runtime.Goexit` завершает её горутину, а отложенное закрытие
// встаёт ждать писателя, которого уже нет.
//
// Дальше подменяется ВЕРДИКТ. Пакет упирается в `-timeout` (умолчание `go test` —
// 600 с) и печатает `FAIL`: «не выполнилось» приходит к читателю под видом
// красного. Вердикта при этом нет НИ У ОДНОЙ пробы пакета, включая прошедшие.
// Замер разбора, из которого выведён гейт: однострочная опечатка в фикстуре
// стоила десяти минут прогона вместо восьми секунд.
//
// # Что требуется вместо
//
//	pool, err := coredb.NewPool(ctx, dsn)
//	require.NoError(t, err)
//	pgtest.ClosePoolAtEnd(t, pool)   // было: defer pool.Close() / t.Cleanup(pool.Close)
//
// `internal/pgtest.ClosePoolAtEnd` даёт закрытию предел и на его исходе называет
// причину и снятие. Десять минут молчания меняются на секунды с именем пробы.
//
// # Почему остаток назван ЧИСЛОМ, а не молчанием
//
// Класс измерен по дереву, а не по диффу, в котором он замечен: находок было
// **813** в 2069 тестовых файлах. Закрыто **315** — всё, что не принадлежит
// деревьям, которые правят соседние сеансы. Оставшееся стоит в poolCloseDebt
// поимённо: не «мы это допускаем», а «столько ещё осталось, и меньше стать может,
// а больше — нет».
//
// Храповик ДВУСТОРОННИЙ. Число выросло — заведено новое место, находка. Число
// упало — послабление пережило свой предмет, и это тоже находка: запись, которой
// больше нечего исключать, унаследует следующая слепая зона. Так послабление
// истекает само, а не памятью дежурного.
//
// # Чего гейт НЕ видит — названо, а не спрятано
//
// Переменная считается пулом, только если заведена в ТОЙ ЖЕ функции вызовом из
// `poolProducers`. Пул, приехавший параметром, полем структуры или из чужого
// помощника, гейту невидим: закрыть это потребовало бы разрешения типов, чего
// обход не делает. Радиус ограничен намеренно — и по нему же выбран опознаватель:
// по ИМЕНИ переменной пул неотличим от репозитория, чей `r.Close` стоит рядом и
// закрывается законно.
//
// # Перепись
//
// «Ноль находок» обязано отличаться от «ноль прочитанного»: гейт печатает, сколько
// тестовых файлов прочитал и разобрал. Пустой обход — провал, а не чистота.
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// poolCloseDebt — остаток класса по деревьям, замер 2026-08-14 на
// release/tech-debt-sweep.
//
// Дерево, которого здесь нет, обязано иметь НОЛЬ находок. Правка этих чисел —
// правка утверждения о дереве: их получают прогоном гейта, а не памятью.
//
// Почему именно эти два дерева: их правят соседние сеансы того же свипа, и
// одновременная механическая переписка тех же файлов дала бы конфликт на ровном
// месте. Снятие — их заходом; предикат снятия — эта карта пустеет.
var poolCloseDebt = map[string]int{
	"services/iam": 472,
	"services/nlb": 26,
}

// TestPoolCloseInTestsIsBounded — гейт.
func TestPoolCloseInTestsIsBounded(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	found := map[string][]PoolCloseFinding{}
	var read, parsed int
	var rels []string
	for rel := range tt.files {
		if !strings.HasSuffix(rel, "_test.go") {
			continue
		}
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	for _, rel := range rels {
		src, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("чтение %s: %v — без переписи «ноль находок» неотличимо от «ноль прочитанного»", rel, err)
		}
		read++
		fs, err := ScanPoolCloses(rel, src)
		if err != nil {
			// Неразбираемый тестовый файл — отказ, а не пропуск: молча не
			// прочитанный файл и есть слепая зона, ради которой пишут перепись.
			t.Fatalf("разбор %s: %v", rel, err)
		}
		parsed++
		if len(fs) > 0 {
			b := poolCloseBucket(rel)
			found[b] = append(found[b], fs...)
		}
	}

	if read == 0 || parsed == 0 {
		t.Fatalf("обход не прочитал ни одного тестового файла (read=%d parsed=%d) — "+
			"пустой обход это провал, а не чистота", read, parsed)
	}

	buckets := map[string]bool{}
	for b := range found {
		buckets[b] = true
	}
	for b := range poolCloseDebt {
		buckets[b] = true
	}
	names := make([]string, 0, len(buckets))
	for b := range buckets {
		names = append(names, b)
	}
	sort.Strings(names)

	total := 0
	for _, b := range names {
		got, allowed := len(found[b]), poolCloseDebt[b]
		total += got
		switch {
		case got > allowed:
			t.Errorf("%s: закрытий пула без предела %d, разрешено %d — заведено новое место.\n%s\n"+
				"Снятие: `pgtest.ClosePoolAtEnd(t, pool)` вместо `defer pool.Close()` / `t.Cleanup(pool.Close)`.\n"+
				"Отложенное закрытие ждёт соединение, которое проба, упавшая внутри открытой транзакции, "+
				"не вернёт никогда, — и уносит с собой вердикт всего пакета.",
				b, got, allowed, coordinatesOf(found[b], allowed))
		case got < allowed:
			t.Errorf("%s: закрытий пула без предела %d, а в poolCloseDebt записано %d — "+
				"послабление пережило свой предмет.\n"+
				"Это находка, а не мелочь: запись, которой больше нечего исключать, "+
				"молча унаследует следующую слепую зону.\n"+
				"Снятие: поставить в poolCloseDebt число %d (или убрать запись, если оно ноль).",
				b, got, allowed, got)
		}
	}

	t.Logf("перепись: тестовых файлов прочитано %d, разобрано %d; закрытий без предела %d; "+
		"деревьев с остатком %d", read, parsed, total, len(poolCloseDebt))
}

// coordinatesOf — первые находки сверх разрешённого, поимённо: отказ обязан
// называть координату, иначе по нему нечего чинить.
func coordinatesOf(fs []PoolCloseFinding, allowed int) string {
	sort.Slice(fs, func(i, j int) bool {
		if fs[i].File != fs[j].File {
			return fs[i].File < fs[j].File
		}
		return fs[i].Line < fs[j].Line
	})
	var b strings.Builder
	shown := 0
	for _, f := range fs {
		if shown >= 12 {
			fmt.Fprintf(&b, "  … и ещё %d\n", len(fs)-shown)
			break
		}
		fmt.Fprintf(&b, "  %s:%d  %s  (%s %s.Close)\n", f.File, f.Line, f.Var, f.Form, f.Var)
		shown++
	}
	_ = allowed
	return strings.TrimRight(b.String(), "\n")
}

// poolCloseBucket — дерево, к которому относится файл: `services/<svc>` либо
// первый сегмент пути.
func poolCloseBucket(rel string) string {
	p := strings.Split(rel, "/")
	if p[0] == "services" && len(p) > 2 {
		return strings.Join(p[:2], "/")
	}
	return p[0]
}
