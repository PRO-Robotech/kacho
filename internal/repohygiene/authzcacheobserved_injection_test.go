// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// authzcacheobserved_injection_test.go — ИНЪЕКЦИЯ: обе стороны гоняют ТУ ЖЕ
// функцию, что и гейт по дереву.
//
// Дефект и законный случай отличаются РОВНО одним файлом — тем, что строит
// коллектор доли попаданий. Без стороны «молчит» гейт был бы неотличим от
// проверки «сервис зовёт носитель» и краснел бы на каждом сервисе сразу; без
// стороны «краснеет» он утверждал бы свойство, которого не проверяет.
//
// Третья проба — отрицательный контроль РАСПОЗНАВАНИЯ: обращение к пакету
// наблюдения, не строящее коллектора, наблюдателем не делает. Без неё гейт
// зеленел бы на импорте ради константы.

import (
	"strings"
	"testing"
)

const observedProdSrc = `package main

import "github.com/PRO-Robotech/kacho/pkg/servicehost"

func main() { _ = servicehost.Serve(nil, nil) }
`

const observerSrc = `package metrics

import "github.com/PRO-Robotech/kacho/pkg/authz/authzmetrics"

func wire() { _ = authzmetrics.New("x", nil) }
`

// Сторона дефекта: сервис у носителя, коллектора нет — гейт называет сервис.
func TestVerdictCacheGateRedOnACarrierWithoutACollector(t *testing.T) {
	root := synthCarrierTree(t, map[string]string{
		"services/x/cmd/x/main.go": observedProdSrc,
	})
	carriers, observers, files, err := scanVerdictCacheObservers(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if files == 0 {
		t.Fatal("синтетическое дерево не прочитано")
	}
	if len(carriers) != 1 || carriers["x"] == "" {
		t.Fatalf("сервис у носителя не найден: %v", carriers)
	}
	if observers["x"] != "" {
		t.Fatalf("коллектор найден там, где его нет: %q", observers["x"])
	}
}

// Законный близнец той же формы: тот же сервис у носителя — плюс файл,
// строящий коллектор.
func TestVerdictCacheGateSilentWhenTheCollectorIsBuilt(t *testing.T) {
	root := synthCarrierTree(t, map[string]string{
		"services/x/cmd/x/main.go":                             observedProdSrc,
		"services/x/internal/observability/metrics/metrics.go": observerSrc,
	})
	carriers, observers, files, err := scanVerdictCacheObservers(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if files == 0 {
		t.Fatal("синтетическое дерево не прочитано")
	}
	if len(carriers) != 1 {
		t.Fatalf("сервисов у носителя %d — ожидался один", len(carriers))
	}
	if observers["x"] == "" {
		t.Fatal("гейт нашёл дефект в законном дереве: коллектор построен, а не засчитан")
	}
	if !strings.Contains(observers["x"], "metrics.go") {
		t.Fatalf("координата наблюдателя не названа: %q", observers["x"])
	}
}

// Отрицательный контроль распознавания: обращение к пакету наблюдения, не
// строящее коллектора, наблюдателем не делает.
func TestVerdictCacheGateIgnoresANonConstructorCall(t *testing.T) {
	root := synthCarrierTree(t, map[string]string{
		"services/x/cmd/x/main.go": observedProdSrc,
		"services/x/internal/observability/metrics/metrics.go": `package metrics

import "github.com/PRO-Robotech/kacho/pkg/authz/authzmetrics"

func name() string { return authzmetrics.MetricName("x") }
`,
	})
	_, observers, files, err := scanVerdictCacheObservers(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if files == 0 {
		t.Fatal("синтетическое дерево не прочитано")
	}
	if observers["x"] != "" {
		t.Fatalf("наблюдателем засчитано обращение к пакету без сборки коллектора (%q) — "+
			"тогда наблюдателем станет всякий, кто прочтёт оттуда имя серии", observers["x"])
	}
}

// Отрицательный контроль ТЕКСТА: имя в комментарии коллектором не является.
//
// Оба имени встречаются в комментариях, объясняющих ровно эту провязку, и
// текстовый поиск принял бы объяснение за исполнение — тот самый класс, ради
// которого гейт разбирает синтаксическое дерево.
func TestVerdictCacheGateIgnoresTheNameInAComment(t *testing.T) {
	root := synthCarrierTree(t, map[string]string{
		"services/x/cmd/x/main.go": observedProdSrc,
		"services/x/internal/observability/metrics/metrics.go": `package metrics

// Здесь надо бы позвать authzmetrics.New — но никто не позвал.
const why = "authzmetrics.New"
`,
	})
	_, observers, _, err := scanVerdictCacheObservers(root)
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	if observers["x"] != "" {
		t.Fatalf("наблюдателем засчитано УПОМИНАНИЕ имени (%q): гейт читает текст, а не код",
			observers["x"])
	}
}
