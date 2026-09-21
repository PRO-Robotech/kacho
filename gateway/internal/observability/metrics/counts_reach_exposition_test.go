// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// counts_reach_exposition_test.go — КАЖДАЯ величина накопителя доезжает до
// экспозиции.
//
// # Предмет
//
// Связь поля структуры со строкой карты собирателя не держит ничто: карта
// литеральная, и поле, которому в ней не завели строки, молча не выходит
// наружу. Счётчик при этом растёт, и в процессе всё выглядит исправным — не
// существует он ровно там, где на него смотрят.
//
// Так и случилось бы с полосой `Unserved`: её завели, провели через накопитель
// и вписали в карту, но ни одна проба не связывала одно с другим. Соседний
// держатель перечисляет полосы литералами и потому растёт только вместе с тем,
// кто про него вспомнил.
//
// # Почему отражением, а не перечнем
//
// Перечень полос здесь разошёлся бы с типом ровно тогда, когда в тип добавят
// восьмую: проба осталась бы зелёной, потому что о восьмой не знает. Предмет
// берётся ИЗ ТИПА: каждому полю-счётчику присваивается своя величина, и она
// обязана найтись в экспозиции. Новое поле без строки в карте краснит гейт и
// называет поле по имени.
//
// # ГРАНИЦА ГЕЙТА — измерена, не предполагается
//
// Отражение идёт по ОДНОЙ структуре — `middleware.AuthzCounts`. Собиратель же
// читает снимок `AuthzSnapshot`, у которого есть и СОБСТВЕННЫЕ поля помимо
// вложенных величин: сегодня это `ClientCalls`, и он доезжает до экспозиции
// своим семейством. Поле-счётчик, заведённое на снимке РЯДОМ с `Counts`, а не
// внутри него, этот гейт не увидит: оно не попадёт в обход и его пропажа из
// карты пройдёт молча.
//
// Граница сужена ровно настолько, насколько дёшево: обход берёт ту структуру, в
// которой полосы и заводят. Расширять его на снимок целиком имеет смысл тогда,
// когда на снимке появится ВТОРОЕ собственное поле-счётчик, — одно сегодняшнее
// держится соседней пробой поимённо.
package metrics_test

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	gwmetrics "github.com/PRO-Robotech/kacho/gateway/internal/observability/metrics"
)

// TestEveryCounterFieldReachesTheExposition — ни одно поле-счётчик не теряется
// по дороге на провод.
func TestEveryCounterFieldReachesTheExposition(t *testing.T) {
	var counts middleware.AuthzCounts
	v := reflect.ValueOf(&counts).Elem()
	typ := v.Type()

	// Каждому полю — СВОЯ величина. Одинаковые не годятся: поле, потерянное по
	// дороге, было бы прикрыто соседним с тем же числом.
	want := map[string]uint64{}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.Type.Kind() != reflect.Uint64 || !v.Field(i).CanSet() {
			continue
		}
		val := uint64(700001 + i*7)
		v.Field(i).SetUint(val)
		want[f.Name] = val
	}

	// Премиса: полей-счётчиков найдено не ноль. Пустой обход дал бы «ноль
	// потерянных» так же убедительно, как исправная карта.
	if len(want) == 0 {
		t.Fatal("в накопителе не найдено ни одного поля-счётчика — отражение сломано, " +
			"и молчание гейта пусто")
	}

	m := gwmetrics.New("test", "deadbeef")
	m.RegisterAuthz(func() gwmetrics.AuthzSnapshot {
		return gwmetrics.AuthzSnapshot{Counts: counts}
	})
	body := expose(t, m)

	var lost []string
	for name, val := range want {
		if !strings.Contains(body, strconv.FormatUint(val, 10)) {
			lost = append(lost, "  "+name+" (величина "+strconv.FormatUint(val, 10)+")")
		}
	}
	t.Logf("перепись: полей-счётчиков %d · не доехало до экспозиции %d", len(want), len(lost))

	if len(lost) > 0 {
		t.Errorf("%d из %d величин накопителя НЕ доезжают до экспозиции — в процессе они растут, "+
			"а снаружи их не существует:\n%s\nЗавести строку в карте собирателя (metrics.go).",
			len(lost), len(want), strings.Join(lost, "\n"))
	}
}

// TestRouteRefusalSeriesIsDeclaredAndReadable — нативный близнец полосы
// `unserved` доезжает до экспозиции, и нулевая величина объявляется наравне с
// ненулевой.
//
// Отсутствие серии и нулевая серия обязаны быть различимы: «отказов по маршруту
// не было» и «счётчик отвалился» — противоположные состояния, и по пустой
// поверхности они неотличимы.
func TestRouteRefusalSeriesIsDeclaredAndReadable(t *testing.T) {
	const name = "kacho_api_gateway_route_refusals_total"

	zero := gwmetrics.New("test", "deadbeef")
	zero.RegisterRouteRefusal(func() uint64 { return 0 })
	if body := expose(t, zero); !strings.Contains(body, name+" 0") {
		t.Errorf("нулевая величина отказов по маршруту не объявлена на поверхности — "+
			"«отказов не было» неотличимо от «счётчик отвалился»:\n%s", body)
	}

	grown := gwmetrics.New("test", "deadbeef")
	grown.RegisterRouteRefusal(func() uint64 { return 700123 })
	if body := expose(t, grown); !strings.Contains(body, "700123") {
		t.Errorf("величина отказов по маршруту не доехала до экспозиции:\n%s", body)
	}
}
