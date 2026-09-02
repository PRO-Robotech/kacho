// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// existence_probe_coverage_test.go — охват пробы существования против того, о
// чём её спросят (задача PRO-Robotech/kacho#1931).
//
// # Предмет
//
// Рантайм спрашивает пробу о ПООБЪЕКТНЫХ типах, выведенных из карты прав
// сервиса (`catalogderive.ObjectScopedTypes`, `pkg/servicehost/links.go`). Тип,
// попавший в этот набор и не попавший в таблицу пробы, получает от неё ошибку
// «неизвестный тип»; вызывающий отрабатывает fail-closed и оставляет отказ
// отказом. Наблюдаемо это так: соседние типы ОДНОГО сервиса отвечают на одном и
// том же входе разным кодом — один промахом владельца, другой отказом в правах.
//
// # Почему проба здесь, а не только в носителе
//
// Тree-wide свойство держит носитель на старте (`servicehost`, О5в): он
// сравнивает `ProbeableTypes()` с выведенным набором у КАЖДОГО сервиса,
// принёсшего порт, и отказывает в пуске. Эта проба — регрессия на compute,
// где расхождение и наблюдалось: перепись карты давала три типа, таблица знала
// один. Она исполняется обычным прогоном пакета, а не только подъёмом процесса.
//
// # Единица счёта названа
//
// Тип объекта модели прав, ВЫВЕДЕННЫЙ из карты прав compute, — не строка
// манифеста. Манифест объявляет типы шире: он не знает, какие из них
// адресуются пообъектно на пути отказа, и по нему счёт давал бы типы, о
// которых пробу не спросят никогда.
package repo_test

import (
	"sort"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/authz/catalogderive"
	"github.com/PRO-Robotech/kacho/services/compute/internal/check"
	"github.com/PRO-Robotech/kacho/services/compute/internal/repo"
)

// TestExistenceProbeCoversEveryObjectScopedType — охват пробы покрывает всё, о
// чём её спросят, и наоборот: запись, которой нечего покрывать, — находка.
//
// Пустой вывод любой из сторон — НАХОДКА, а не чистота: «ноль расхождений» и
// «ноль осмотренного» иначе неразличимы.
func TestExistenceProbeCoversEveryObjectScopedType(t *testing.T) {
	scoped := catalogderive.ObjectScopedTypes(check.PermissionMap())
	if len(scoped) == 0 {
		t.Fatal("из карты прав compute не вывелось ни одного пообъектного типа — обход пуст, и " +
			"молчание пробы было бы неотличимо от чистоты")
	}

	covered := map[string]struct{}{}
	for _, ot := range (&repo.ExistenceProbe{}).ProbeableTypes() {
		covered[ot] = struct{}{}
	}
	if len(covered) == 0 {
		t.Fatal("проба не объявила ни одного типа охвата: пустой перечень означает «ни о чём», " +
			"и каждый отказ приходил бы кодом отказа в правах")
	}

	missing := diffSorted(scoped, covered)
	for _, ot := range missing {
		t.Errorf("тип %q пообъектен по карте прав — пробу о нём спросят, — а в её таблице его нет: "+
			"сверка ответит ошибкой, вызывающий отработает fail-closed, и отказ придёт кодом отказа "+
			"в правах там, где соседний тип того же сервиса отвечает промахом владельца", ot)
	}
	stale := diffSorted(covered, scoped)
	for _, ot := range stale {
		t.Errorf("тип %q объявлен охватом пробы, а карта прав пообъектным его не называет: "+
			"спросить о нём некому, и запись утверждает про продукт то, чего в нём нет", ot)
	}

	t.Logf("перепись: пообъектных типов карты прав %d · типов в таблице пробы %d · "+
		"непокрытых %d · лишних %d", len(scoped), len(covered), len(missing), len(stale))
}

// diffSorted — элементы left, которых нет в right, в устойчивом порядке.
func diffSorted(left, right map[string]struct{}) []string {
	out := make([]string, 0, len(left))
	for k := range left {
		if _, ok := right[k]; !ok {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
