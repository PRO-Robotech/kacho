// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package domain_test

// reserved_prefixes_test.go — перечень диапазонов, которые посадка держит за
// собой, и предикат пересечения с ним.
//
// # Что здесь проверяется в первую очередь
//
// Пересечение считается как пересечение ДИАПАЗОНОВ, а не как равенство строк, и
// ловится в ОБЕ стороны вложенности. Вторая сторона — классический промах:
// «арендаторский блок внутри зарезервированного» замечают все, а
// «зарезервированный внутри арендаторского» (`10.1.2.0/24` объявлен служебным,
// арендатор просит `10.0.0.0/8`) пропускают, потому что проверку пишут как
// «содержится ли ввод в перечне».
//
// Каждое отрицание идёт В ПАРЕ с положительным контролем: без него «отвергнуто»
// неотличимо от предиката, отвергающего всё.

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
)

// mustReservedPrefix — разбор ожидаемо-годного значения внутри случая.
func mustReservedPrefix(t *testing.T, s string) netip.Prefix {
	t.Helper()
	p, err := netip.ParsePrefix(s)
	require.NoError(t, err, "значение случая обязано быть разбираемым префиксом")
	return p
}

// RP-01 (положительный контроль конструктора): годные записи объявляются.
func TestReservedPrefixes_ValidEntriesAreDeclared(t *testing.T) {
	r := domain.NewReservedPrefixes("169.254.0.0/16", "fe80::/10")

	assert.True(t, r.IsDeclared(), "две годные записи обязаны читаться как объявление")
	assert.Equal(t, 2, r.Len())
	assert.Empty(t, r.Rejected(), "годные записи не могут попасть в отвергнутые")
}

// RP-02: негодная запись НЕ отбрасывается молча — она названа.
//
// Молча отброшенная запись оставила бы диапазон, который оператор считает
// зарезервированным, а контур — нет: подсеть арендатора поверх него была бы
// принята, и оператор искал бы причину в сети, а не в своём файле настроек.
func TestReservedPrefixes_UnparsableEntryIsNamed(t *testing.T) {
	r := domain.NewReservedPrefixes("169.254.0.0/16", "10.0.0.0/33", "не-префикс")

	rejected := r.Rejected()
	require.Len(t, rejected, 2, "обе негодные записи обязаны быть названы")
	assert.Equal(t, "10.0.0.0/33", rejected[0].Entry)
	assert.NotEmpty(t, rejected[0].Reason, "у отказа обязана быть причина: оператор чинит по ней")
	assert.Equal(t, "не-префикс", rejected[1].Entry)

	// Парный положительный контроль: годная запись из того же ввода объявлена, то
	// есть негодная не утащила за собой весь перечень.
	assert.Equal(t, 1, r.Len())
	assert.True(t, r.Overlaps(mustReservedPrefix(t, "169.254.1.0/24")))
}

// RP-03: адрес с непустыми host-битами — негодная запись, а не «почти годная».
//
// Продукт требует того же от арендатора (`validateCIDRPrefix`: host-bits=0), и
// молчаливая нормализация здесь означала бы, что объявление значит НЕ то, что
// написано: `10.0.0.1/24` расширился бы до всей `/24` без ведома автора.
func TestReservedPrefixes_HostBitsSetIsRejected(t *testing.T) {
	r := domain.NewReservedPrefixes("10.0.0.1/24")

	require.Len(t, r.Rejected(), 1)
	assert.Contains(t, r.Rejected()[0].Reason, "10.0.0.0/24",
		"причина обязана назвать сетевой адрес, которым запись надо переписать")
	assert.False(t, r.IsDeclared(), "негодная запись объявлением не становится")

	// Положительный контроль: та же запись в канонической форме — годна.
	assert.True(t, domain.NewReservedPrefixes("10.0.0.0/24").IsDeclared())
}

// RP-04: форма IPv4-в-IPv6 отвергается.
//
// Это не придирка к записи, а защита от резервирования, которое НЕ РЕЗЕРВИРУЕТ
// НИЧЕГО: `::ffff:10.0.0.0/104` — адрес семейства IPv6, поэтому с арендаторским
// `10.0.0.0/8` он не пересекается ни по одному предикату, и объявление выглядит
// исполненным, ничего не запрещая.
func TestReservedPrefixes_IPv4MappedFormIsRejected(t *testing.T) {
	r := domain.NewReservedPrefixes("::ffff:10.0.0.0/104")

	require.Len(t, r.Rejected(), 1)
	assert.False(t, r.IsDeclared())

	// Парный контроль, называющий цену пропуска: если бы такую запись приняли,
	// пересечения с арендаторским v4-блоком не было бы.
	mapped := mustReservedPrefix(t, "::ffff:10.0.0.0/104")
	tenant := mustReservedPrefix(t, "10.0.0.0/8")
	assert.False(t, mapped.Overlaps(tenant),
		"предпосылка случая: в этой форме пересечения с v4 нет вовсе")

	// Тот же диапазон, написанный семейством IPv4, — годен и пересекается.
	assert.True(t, domain.NewReservedPrefixes("10.0.0.0/8").Overlaps(tenant))
}

// RP-05: запись, покрывающая ВСЁ семейство, отвергается как самопротиворечие.
//
// `0.0.0.0/0` разбирается, канонична и при этом не оставляет контуру ни одного
// адреса, который он мог бы выдать: процесс поднимется, а КАЖДОЕ создание подсети
// этого семейства будет отвергнуто. Такой отказ виден только в трафике
// арендатора, поэтому объявление отвергается на старте.
func TestReservedPrefixes_WholeFamilyIsRejected(t *testing.T) {
	for _, entry := range []string{"0.0.0.0/0", "::/0"} {
		r := domain.NewReservedPrefixes(entry)
		require.Len(t, r.Rejected(), 1, "запись %q обязана быть отвергнута", entry)
		assert.False(t, r.IsDeclared())
	}

	// Положительный контроль: широкая, но не всеобъемлющая запись законна —
	// стенд вправе держать за собой целый /8.
	assert.True(t, domain.NewReservedPrefixes("10.0.0.0/8").IsDeclared())
}

// RP-06 — ВЫРОЖДЕННЫЙ ВХОД: одинокая запятая.
//
// Сырая настройка непуста (разбор по запятой даёт две пустые записи), а
// объявленных диапазонов ноль. Предикат по длине сырой настройки прочитал бы её
// как заполненную — и разошёлся бы со читателем ровно там, где расхождение
// опасно.
func TestReservedPrefixes_LoneCommaIsNotADeclaration(t *testing.T) {
	r := domain.NewReservedPrefixes("", "")

	assert.False(t, r.IsDeclared(), "две пустые записи объявлением не являются")
	assert.Zero(t, r.Len())
	assert.Empty(t, r.Rejected(), "пустая запись — не негодная запись, а её отсутствие")
}

// RP-07: пробелы срезаются, повторы схлопываются, регистр IPv6 не значим.
//
// Парный положительный контроль к RP-06: нормализация не делает пустым то, что
// оператор действительно написал.
func TestReservedPrefixes_NormalisesWhitespaceAndDuplicates(t *testing.T) {
	r := domain.NewReservedPrefixes("  10.0.0.0/8 ", "10.0.0.0/8", "FE80::/10", "fe80::/10")

	assert.True(t, r.IsDeclared())
	assert.Equal(t, 2, r.Len(), "повторы обязаны схлопнуться: два написания одного диапазона — один диапазон")
	assert.True(t, r.Overlaps(mustReservedPrefix(t, "10.1.0.0/16")))
	assert.True(t, r.Overlaps(mustReservedPrefix(t, "fe80::/64")))
}

// RP-08 — ЯДРО: вложенность ловится в ОБЕ стороны.
func TestReservedPrefixes_OverlapCatchesBothNestingDirections(t *testing.T) {
	// (а) арендаторский блок ВНУТРИ зарезервированного.
	wide := domain.NewReservedPrefixes("10.0.0.0/8")
	assert.True(t, wide.Overlaps(mustReservedPrefix(t, "10.1.2.0/24")),
		"узкий блок арендатора внутри широкого служебного диапазона обязан ловиться")

	// (б) зарезервированный ВНУТРИ арендаторского — та сторона, которую пропускают.
	narrow := domain.NewReservedPrefixes("10.1.2.0/24")
	assert.True(t, narrow.Overlaps(mustReservedPrefix(t, "10.0.0.0/8")),
		"служебный диапазон внутри широкого блока арендатора обязан ловиться: иначе "+
			"арендатор забирает служебные адреса, попросив диапазон пошире")

	// (в) равные диапазоны.
	assert.True(t, wide.Overlaps(mustReservedPrefix(t, "10.0.0.0/8")))
}

// RP-09: касание границ пересечением НЕ является (положительный контроль).
//
// Соседние диапазоны — законный ввод, и проверка, отвергающая их, отвергала бы
// каждую подсеть рядом со служебной.
func TestReservedPrefixes_AdjacentRangesDoNotOverlap(t *testing.T) {
	r := domain.NewReservedPrefixes("10.0.1.0/24")

	assert.False(t, r.Overlaps(mustReservedPrefix(t, "10.0.0.0/24")), "диапазон ниже вплотную")
	assert.False(t, r.Overlaps(mustReservedPrefix(t, "10.0.2.0/24")), "диапазон выше вплотную")
	// И граничные адреса самого диапазона — внутри.
	assert.True(t, r.Overlaps(mustReservedPrefix(t, "10.0.1.0/32")))
	assert.True(t, r.Overlaps(mustReservedPrefix(t, "10.0.1.255/32")))
}

// RP-10: семейства между собой не пересекаются.
func TestReservedPrefixes_FamiliesDoNotCross(t *testing.T) {
	v4only := domain.NewReservedPrefixes("10.0.0.0/8")
	assert.False(t, v4only.Overlaps(mustReservedPrefix(t, "2001:db8::/32")),
		"v6-блок не может пересечься с v4-резервом")

	v6only := domain.NewReservedPrefixes("fe80::/10")
	assert.False(t, v6only.Overlaps(mustReservedPrefix(t, "10.0.0.0/8")),
		"v4-блок не может пересечься с v6-резервом")

	// Положительный контроль: у каждого семейства своё пересечение работает.
	assert.True(t, v4only.Overlaps(mustReservedPrefix(t, "10.0.0.0/24")))
	assert.True(t, v6only.Overlaps(mustReservedPrefix(t, "fe80::/64")))
}

// RP-11: пустой перечень не пересекается НИ С ЧЕМ — и это названо прямо.
//
// Это и есть вакуумность, ради которой существует страж старта: пустой перечень
// означает «не сужаем», а не «нечего сужать». Случай закрепляет именно такое
// поведение, чтобы никто не «починил» его молчаливым отказом-по-умолчанию: тогда
// dev-фикстуры, у которых перечня нет, начали бы отвергать всё.
func TestReservedPrefixes_EmptySetNarrowsNothing(t *testing.T) {
	var zero domain.ReservedPrefixes

	assert.False(t, zero.IsDeclared())
	assert.False(t, zero.Overlaps(mustReservedPrefix(t, "10.0.0.0/24")),
		"пустой перечень ничего не запрещает — старт с ним отвергает страж, а не этот предикат")
	assert.Equal(t, domain.NewReservedPrefixes(), zero,
		"нулевое значение и пустой конструктор обязаны совпадать: два представления одного "+
			"состояния однажды разошлись бы")
}

// RP-12: перечень нельзя получить обратно из значения.
//
// У типа нет ни одного метода, отдающего сами диапазоны, — и это by
// construction, а не по забывчивости: арендатору сообщается про ЕГО ввод, а не
// выдаётся карта служебных диапазонов. `String()` печатает счёт, потому что его
// читает журнал процесса, а не ответ на запрос.
func TestReservedPrefixes_ValueDoesNotPrintTheRanges(t *testing.T) {
	r := domain.NewReservedPrefixes("10.0.0.0/8", "fe80::/10")

	s := r.String()
	assert.NotContains(t, s, "10.0.0.0", "печать значения не вправе раскрывать служебные диапазоны")
	assert.NotContains(t, s, "fe80", "печать значения не вправе раскрывать служебные диапазоны")
	assert.Contains(t, s, "2", "печать обязана оставаться полезной журналу: счёт объявленного")

	var zero domain.ReservedPrefixes
	assert.Contains(t, zero.String(), "not declared",
		"необъявленный перечень обязан читаться в журнале как необъявленный, а не как «0»")
}
