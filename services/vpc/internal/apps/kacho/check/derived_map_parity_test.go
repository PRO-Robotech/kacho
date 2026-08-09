// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check

import (
	"slices"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/api"
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/authz/catalogderive"
)

// adjudicatedDivergences — расхождение, по которому принято решение оставить
// сторону КАТАЛОГА, потому что доступ им не меняется вовсе.
//
// AddressService/GetByValue ищет адрес по значению IP, поэтому объекта решения у
// него до резолва нет; область берётся из необязательного `subnet_id`.
// Рукописный экстрактор возвращал ОШИБКУ, когда поле пусто; выведенный
// возвращает пустой идентификатор. Оба пути кончаются одним и тем же:
// интерсептор отказывает — в первом случае на извлечении, во втором на
// форматировании объекта (`FormatObject` отвергает пустой id). Различается
// только текст в журнале. Свойство закреплено пробой
// TestEmptyScopeIdIsRefusedNotAsked в pkg/authz/catalogderive.
var adjudicatedDivergences = []string{
	`/kacho.cloud.vpc.v1.AddressService/GetByValue: scope — one side declares a static object type and the other does not (hand-written map=false, annotations=true)`,
}

// TestDerivedMapEqualsTheHandWrittenOne — переходный гейт фазы migrate.
//
// Карта прав перестаёт писаться руками и начинает выводиться из аннотаций
// proto — тех самых, из которых генерируется каталог края. Переключение меняет
// то, что сервис спрашивает на КАЖДОМ вызове, поэтому оно предваряется полной
// сверкой: расхождение, не разобранное здесь, означало бы, что чьи-то права
// изменились молча.
//
// Сравниваются только те оси, которые РЕШАЮТ: полоса, отношение и тип объекта
// в полосе edge-checks, форма отказа. Строка permission не сравнивается — её не
// читает интерсептор ни в одной полосе.
//
// Гейт удаляется вместе с рукописной картой: сверка, у которой не осталось
// левого операнда, — проверка без предмета.
func TestDerivedMapEqualsTheHandWrittenOne(t *testing.T) {
	derived, err := catalogderive.Derive(protoPackages...)
	require.NoError(t, err, "вывод карты из аннотаций обязан состояться — иначе сверять нечего")

	manual := PermissionMap()
	require.NotEmpty(t, manual, "предпосылка: рукописная карта не пуста")
	require.NotEmpty(t, derived, "предпосылка: выведенная карта не пуста")

	diff := catalogderive.Diff(manual, derived)

	t.Logf("перепись: рукописных записей %d, выведенных %d, расхождений %d, разобрано %d",
		len(manual), len(derived), len(diff), len(adjudicatedDivergences))

	for _, d := range diff {
		if !slices.Contains(adjudicatedDivergences, d) {
			t.Errorf("карта сервиса и аннотации расходятся, и это НЕ разобрано:\n  %s\n\n"+
				"Исходы: править аннотацию, если права у сервиса; принять сторону каталога с "+
				"письменным разбором в adjudicatedDivergences; снять расхождение целиком.", d)
		}
	}
	pinned := slices.Clone(adjudicatedDivergences)
	sort.Strings(pinned)
	for _, d := range pinned {
		if !slices.Contains(diff, d) {
			t.Errorf("разбор пережил свой предмет — такого расхождения больше нет:\n  %s\n\n"+
				"Удали запись: перечень, которому нечего исключать, — находка.", d)
		}
	}
}
