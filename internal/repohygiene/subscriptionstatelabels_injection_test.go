// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// subscriptionstatelabels_injection_test.go — доказательство, что гейт меток
// СПОСОБЕН упасть и СПОСОБЕН смолчать.
//
// Прогонов ТРИ, и третий обязателен: контроль (всё цело — молчат обе стороны) ·
// инъекция нового свойства (краснеет только оно) · инъекция уже действовавшей
// сверки полноты (краснеет только она). Без третьего молчание сверки чисел
// неотличимо от её отсутствия.
//
// Вход синтетический: вердикт, снятый правкой настоящих контрактов, был бы
// свойством рабочего каталога, а не коммита. Судят здесь ТЕ ЖЕ функции, что и
// гейт, — своя копия разошлась бы с ним молча.
package repohygiene

import (
	"strings"
	"testing"
)

// protoWithLabels — контракт, чьё сообщение несёт метки.
const protoWithLabels = `syntax = "proto3";

package kacho.cloud.probe.v1;

message Widget {
  string id = 1;
  string project_id = 2;
  map<string, string> labels = 6;
}

message Gadget {
  string id = 1;
  map<string,string> labels = 4;
}
`

// protoWithoutLabels — тот же контракт, у которого метки СНЯТЫ. Это инъекция
// нового свойства: всё остальное на месте.
const protoWithoutLabels = `syntax = "proto3";

package kacho.cloud.probe.v1;

message Widget {
  string id = 1;
  string project_id = 2;
  string description = 6;
}
`

// journalSource — объявление журнала владельца с n видами.
func journalSource(n int) []byte {
	var b strings.Builder
	b.WriteString("package subscriptionjournal\n\nfunc build() interface{} {\n")
	b.WriteString("\treturn subscription.Journal{Mapping: subscription.Mapping{\n")
	b.WriteString("\t\tKinds: map[string]subscription.Kind{\n")
	for i := 0; i < n; i++ {
		b.WriteString("\t\t\tkindWord" + string(rune('A'+i)) + ": {ObjectType: t, Action: a},\n")
	}
	b.WriteString("\t\t},\n\t}}\n}\n")
	return []byte(b.String())
}

// TestLabelsGateFiresOnlyOnTheMissingLabels — три прогона.
func TestLabelsGateFiresOnlyOnTheMissingLabels(t *testing.T) {
	// ── ПРОГОН 1: контроль. Метки на месте, числа сходятся.
	t.Run("контроль: метки на месте — молчание", func(t *testing.T) {
		labels, found := MessageCarriesLabels(protoWithLabels, "Widget")
		if !found {
			t.Fatal("контроль беспредметен: сообщение не найдено в собственном контракте")
		}
		if !labels {
			t.Fatal("гейт объявил бы недостачу меток там, где поле объявлено")
		}
		// И вторая форма записи отображения, без пробела: разбор обязан видеть обе,
		// иначе он меряет пробел, а не поле.
		if l, ok := MessageCarriesLabels(protoWithLabels, "Gadget"); !ok || !l {
			t.Fatalf("форма `map<string,string>` без пробела не опознана (найдено=%v, метки=%v)", ok, l)
		}
	})

	// ── ПРОГОН 2: инъекция НОВОГО свойства — меток нет.
	t.Run("инъекция: меток нет — сообщение найдено, метки нет", func(t *testing.T) {
		labels, found := MessageCarriesLabels(protoWithoutLabels, "Widget")
		if !found {
			t.Fatal("инъекция не состоялась: сообщение перестало находиться вовсе, и " +
				"гейт сказал бы про мёртвую координату, а не про метки")
		}
		if labels {
			t.Fatal("гейт промолчал на сообщении БЕЗ поля меток — он не способен упасть")
		}
	})

	// ── ПРОГОН 3: инъекция УЖЕ ДЕЙСТВОВАВШЕЙ сверки полноты — числа двух
	// источников разошлись. Без этого прогона молчание сверки неотличимо от её
	// отсутствия.
	t.Run("инъекция: числа двух источников разошлись", func(t *testing.T) {
		kinds, err := ScanJournalKinds("services/probe/internal/subscriptionjournal/journal.go", journalSource(3))
		if err != nil {
			t.Fatalf("разбор объявления журнала: %v", err)
		}
		if kinds.Count != 3 {
			t.Fatalf("разбор насчитал %d видов вместо трёх — инъекция не состоялась", kinds.Count)
		}
		page := "Тип по виду: `kacho.cloud.probe.v1.Widget` у одного вида, " +
			"`kacho.cloud.probe.v1.Gadget` у другого."
		types, mentions := ScanStateTypesOnPage(page)
		if mentions != 2 || len(types) != 2 {
			t.Fatalf("со страницы прочитано упоминаний %d, различных типов %d — ожидалось 2 и 2",
				mentions, len(types))
		}
		if len(types) == kinds.Count {
			t.Fatal("сверка полноты промолчала бы: числа сошлись там, где страница " +
				"называет на один тип меньше, чем видов у владельца")
		}
	})
}

// TestLabelsGateStaysSilentOnLegitimateTwins — законные близнецы. Без них гейт
// ловит форму, а не существо.
func TestLabelsGateStaysSilentOnLegitimateTwins(t *testing.T) {
	t.Run("мёртвая координата отличима от недостачи меток", func(t *testing.T) {
		labels, found := MessageCarriesLabels(protoWithLabels, "NoSuchMessage")
		if found {
			t.Fatal("несуществующее сообщение объявлено найденным")
		}
		if labels {
			t.Fatal("у несуществующего сообщения объявлены метки")
		}
		// Два разных замечания: «сообщения нет» и «метки нет» ведут к разной работе.
		present, _ := MessageCarriesLabels(protoWithoutLabels, "Widget")
		if present {
			t.Fatal("близнец подобран неверно")
		}
	})

	t.Run("одноимённый префикс не считается сообщением", func(t *testing.T) {
		// `WidgetSpec` не есть `Widget`: разбор ищет объявление целиком, вместе с
		// открывающей скобкой, иначе перепись считала бы одно сообщение дважды.
		const src = `syntax = "proto3";

message WidgetSpec {
  map<string, string> labels = 1;
}
`
		if _, found := MessageCarriesLabels(src, "Widget"); found {
			t.Fatal("сообщение опознано по префиксу имени — гейт судил бы чужой контракт")
		}
	})

	t.Run("поле меток внутри СОСЕДНЕГО сообщения не засчитывается", func(t *testing.T) {
		const src = `syntax = "proto3";

message Widget {
  string id = 1;
}

message Gadget {
  map<string, string> labels = 1;
}
`
		labels, found := MessageCarriesLabels(src, "Widget")
		if !found {
			t.Fatal("сообщение не найдено")
		}
		if labels {
			t.Fatal("метки соседнего сообщения засчитаны этому — разбор не удерживает " +
				"границу тела, и гейт молчал бы на всяком контракте, где метки есть " +
				"хоть у кого-нибудь")
		}
	})

	t.Run("страница без упоминаний типов читается как ноль, а не как ошибка", func(t *testing.T) {
		types, mentions := ScanStateTypesOnPage("Здесь про типы состояния не сказано ничего.")
		if mentions != 0 || len(types) != 0 {
			t.Fatalf("на тексте без типов прочитано %d упоминаний и %d типов", mentions, len(types))
		}
	})

	t.Run("повторное упоминание одного типа не удваивает перепись различных", func(t *testing.T) {
		page := "`kacho.cloud.probe.v1.Widget` и ещё раз `kacho.cloud.probe.v1.Widget`."
		types, mentions := ScanStateTypesOnPage(page)
		if mentions != 2 {
			t.Fatalf("упоминаний прочитано %d вместо двух", mentions)
		}
		if len(types) != 1 {
			t.Fatalf("различных типов %d вместо одного — сверка чисел стала бы функцией "+
				"того, сколько раз тип назвали", len(types))
		}
	})
}
