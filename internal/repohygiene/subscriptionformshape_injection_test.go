// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// subscriptionformshape_injection_test.go — способность гейта формы упасть,
// доказанная ПО КАЖДОЙ ОСИ и в обе стороны.
//
// # Устройство стенда
//
// Форма собирается из частей, и случай подменяет РОВНО ОДНУ. Так дефект и его
// законный близнец отличаются друг от друга только тем, что ось и различает, —
// а не всем текстом сразу. Гейт, ключующийся на чём угодно постороннем (порядок
// полей, номера, имена сообщений, длина комментария), здесь ошибётся.
//
// # Близнец не является побайтовой копией
//
// У каждой оси близнец переизложен: другое слово, другой порядок, другой тип,
// другое имя. Копия исходного случая доказывала бы лишь то, что гейт
// детерминирован, — а требуется, чтобы он различал ПРЕДМЕТ, а не форму записи.
//
// # Изоляция
//
// Дерево строится в `t.TempDir()` обычной записью файлов. Ни `git init`, ни
// `git add`, ни `git config`.
package repohygiene

import (
	"strings"
	"testing"
)

// shapeForm — форма, собираемая из частей. Каждое поле — одна подменяемая часть.
type shapeForm struct {
	Package    string
	Imports    string
	Verbs      string
	AnchorEnum string
	ReqComment string
	Axes       string
	Start      string
	Opened     string
	Event      string
}

func (f shapeForm) render() string {
	return `syntax = "proto3";

package ` + f.Package + `;

` + f.Imports + `

` + f.AnchorEnum + `

` + f.ReqComment + `
message SubscriptionRequest {
` + f.Axes + `
` + f.Start + `
}

// SubscriptionOpened — служебное первое сообщение.
message SubscriptionOpened {
` + f.Opened + `
}

// SubscriptionEvent — оболочка события.
message SubscriptionEvent {
` + f.Event + `
}

` + f.Verbs + `
`
}

func baseShapeForm() shapeForm {
	return shapeForm{
		Package: "kacho.cloud.subscription",
		Imports: `import "google/protobuf/any.proto";`,
		AnchorEnum: `// SubscriptionAnchor — якорь начала.
enum SubscriptionAnchor {
  SUBSCRIPTION_ANCHOR_UNSPECIFIED = 0;
  BEGINNING = 1;
  CURRENT_END = 2;
}`,
		ReqComment: `// SubscriptionRequest — запрос подписки.
//
// ИМЯ сюда не берётся: оно мутабельно, и подписка по нему молча перестанет
// совпадать. МЕТКИ сюда не берутся: они мутабельны, и выход из выборки
// неотличим от удаления.`,
		Axes: `  // kinds — виды предметов. Категория: УДОБСТВА.
  repeated string kinds = 1;

  // project_id — проект. Категория: ЯКОРНАЯ.
  string project_id = 2;

  // ids — идентификаторы. Категория: УДОБСТВА.
  repeated string ids = 3;`,
		Start: `  // start — с какого места отдавать.
  //
  // ИСХОД НЕЗАДАННОГО НАЗВАН: start не задан означает ровно CURRENT_END.
  oneof start {
    // anchor — назвать место словом.
    SubscriptionAnchor anchor = 10;
    // position — непрозрачный токен.
    string position = 11;
  }`,
		Opened: `  string position = 1;
  bool caught_up = 2;
  repeated string honored_filters = 3;
  string earliest_resumable_position = 4;
  bool retains_everything = 5;`,
		Event: `  // StateUnavailable — вторая ветвь носителя.
  message StateUnavailable {
    // reason — почему состояния нет. Это НЕ причина остановки потока.
    string reason = 1;
  }

  string position = 1;
  string kind = 2;
  string resource_id = 3;
  // project_id — авторизуемый якорь, поле ОБОЛОЧКИ.
  string project_id = 4;

  oneof carrier {
    google.protobuf.Any state = 10;
    StateUnavailable state_unavailable = 11;
  }`,
	}
}

// shapeStandLedger — перечень полей, отвечающий базовой форме стенда.
func shapeStandLedger() []SubscriptionFieldRecord {
	return []SubscriptionFieldRecord{
		{Field: "kinds", Role: SubscriptionRoleAxis, Why: "виды предметов владельца"},
		{Field: "project_id", Role: SubscriptionRoleAxis, Why: "проект — якорь показа"},
		{Field: "ids", Role: SubscriptionRoleAxis, Why: "адресный отбор"},
		{Field: "anchor", Role: SubscriptionRoleStart, Why: "место словом"},
		{Field: "position", Role: SubscriptionRoleStart, Why: "непрозрачный токен"},
	}
}

func shapeStandAbsent() []SubscriptionAbsentAxis {
	return []SubscriptionAbsentAxis{
		{Axis: "name", Marker: "ИМЯ", Why: "мутабельный ярлык"},
		{Axis: "labels", Marker: "МЕТКИ", Why: "мутабельны"},
	}
}

// shapeAudit прогоняет анализатор по стенду и возвращает виды находок.
func shapeAudit(
	t *testing.T, form shapeForm,
	ledger []SubscriptionFieldRecord, absent []SubscriptionAbsentAxis,
) ([]SubscriptionShapeFinding, SubscriptionShapeCensus) {
	t.Helper()
	root := subscriptionStand(t, map[string]string{
		"proto/kacho/cloud/subscription/subscription.proto": form.render(),
	})
	var log strings.Builder
	findings, census, err := AuditSubscriptionFormShape(SubscriptionShapeOptions{
		Root:          root,
		ProtoRoot:     "proto",
		FormFile:      "kacho/cloud/subscription/subscription.proto",
		RequestFields: ledger,
		AbsentAxes:    absent,
		Expect:        subscriptionShapeExpectation(),
	}, &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v\n%s", err, log.String())
	}
	t.Log(strings.TrimSpace(log.String()))
	return findings, census
}

// requireKind требует находку названного вида и возвращает её.
func requireKind(t *testing.T, findings []SubscriptionShapeFinding, kind string) SubscriptionShapeFinding {
	t.Helper()
	for _, f := range findings {
		if f.Kind == kind {
			return f
		}
	}
	t.Fatalf("гейт промолчал на дефекте: находки %v, а ждали %q", kinds(findings), kind)
	return SubscriptionShapeFinding{}
}

// requireKindSaying требует находку названного вида, чей ТЕКСТ называет ИМЕННО
// проверяемую ветвь.
//
// Вида МАЛО там, где вид вносится не одной ветвью. Единица счёта у этого
// анализатора двойная и её нельзя смешивать: ВИДОВ находок двадцать, а МЕСТ
// внесения двадцать два — два вида несут по две ветви каждый
// (`carrier-not-a-choice`, `anchor-not-in-envelope`). Подпроба, утверждающая
// только вид, зеленеет, когда снятую ветвь подхватывает её соседка.
//
// Это измерено, а не предположено: сделав первую ветвь носителя недостижимой,
// прогон остался ПОЛНОСТЬЮ зелёным — у несуществующего ветвления ноль ветвей,
// ноль не равен двум, и находку того же вида выдавала вторая ветвь. Различает
// их только текст.
func requireKindSaying(
	t *testing.T, findings []SubscriptionShapeFinding, kind, saying string,
) SubscriptionShapeFinding {
	t.Helper()
	var sameKind []string
	for _, f := range findings {
		if f.Kind != kind {
			continue
		}
		if strings.Contains(f.Reason, saying) {
			return f
		}
		sameKind = append(sameKind, f.String())
	}
	if len(sameKind) > 0 {
		t.Fatalf("вид %q выдан ДРУГОЙ его ветвью — проверяемая не наблюдаема "+
			"независимо. Ждали текст %q, а получили:\n  %s",
			kind, saying, strings.Join(sameKind, "\n  "))
	}
	t.Fatalf("гейт промолчал на дефекте: находки %v, а ждали %q со словами %q",
		kinds(findings), kind, saying)
	return SubscriptionShapeFinding{}
}

// requireSilence требует молчания — законный близнец не должен краснеть.
func requireSilence(t *testing.T, findings []SubscriptionShapeFinding) {
	t.Helper()
	if len(findings) != 0 {
		lines := make([]string, 0, len(findings))
		for _, f := range findings {
			lines = append(lines, f.String())
		}
		t.Fatalf("законный близнец объявлен дефектом:\n  %s", strings.Join(lines, "\n  "))
	}
}

func kinds(findings []SubscriptionShapeFinding) []string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, f.Kind)
	}
	return out
}

// TestSubscriptionShapeStandIsCleanBeforeInjection — контроль стенда.
//
// Без него всякое «дефект покраснел» было бы недоказуемо: гейт мог краснеть на
// самом стенде, а не на внесённом дефекте.
func TestSubscriptionShapeStandIsCleanBeforeInjection(t *testing.T) {
	findings, census := shapeAudit(t, baseShapeForm(), shapeStandLedger(), shapeStandAbsent())
	requireSilence(t, findings)
	if census.Axes != 3 || census.TopTypes != 4 {
		t.Fatalf("стенд разобран не так, как ожидалось: осей %d, типов верхнего уровня %d",
			census.Axes, census.TopTypes)
	}
	if census.Assertions < 25 {
		t.Fatalf("на стенде проверено %d утверждений — слишком мало, чтобы инъекции ниже "+
			"что-то доказывали", census.Assertions)
	}
}

// TestSubscriptionShapeAxisLedgerCanFail — ЗАКРЫТЫЙ перечень полей, три стороны.
func TestSubscriptionShapeAxisLedgerCanFail(t *testing.T) {
	t.Run("поле вне перечня — краснеет и называет ПОЛЕ", func(t *testing.T) {
		f := baseShapeForm()
		f.Axes += "\n\n  // label_selector — отбор по меткам. Категория: УДОБСТВА.\n" +
			"  string label_selector = 4;"
		findings, _ := shapeAudit(t, f, shapeStandLedger(), shapeStandAbsent())
		got := requireKind(t, findings, "field-outside-the-ledger")
		if got.Line == 0 || !strings.Contains(got.Reason, "label_selector") {
			t.Fatalf("находка без координаты или без имени поля: %s", got.String())
		}
		t.Logf("%s", got.String())
	})

	t.Run("поле, добавленное ВМЕСТЕ с записью, — молчание", func(t *testing.T) {
		// Близнец переизложен: другое имя, другая категория, другой номер и
		// другое место в теле сообщения.
		f := baseShapeForm()
		f.Axes = "  // owner_id — владелец предмета. Категория: ЯКОРНАЯ.\n" +
			"  string owner_id = 7;\n\n" + f.Axes
		ledger := append(shapeStandLedger(),
			SubscriptionFieldRecord{Field: "owner_id", Role: SubscriptionRoleAxis,
				Why: "владелец предмета — вторая якорная ось"})
		requireSilence(t, first(shapeAudit(t, f, ledger, shapeStandAbsent())))
	})

	t.Run("запись, которой нечего исключать, — находка", func(t *testing.T) {
		ledger := append(shapeStandLedger(),
			SubscriptionFieldRecord{Field: "created_after", Role: SubscriptionRoleAxis,
				Why: "ось, которую сняли, а запись осталась"})
		findings, _ := shapeAudit(t, baseShapeForm(), ledger, shapeStandAbsent())
		f := requireKind(t, findings, "stale-field-record")
		if !strings.Contains(f.Reason, "created_after") {
			t.Fatalf("находка не называет записи: %s", f.String())
		}
	})

	t.Run("запись без причины — находка", func(t *testing.T) {
		ledger := shapeStandLedger()
		ledger[0].Why = ""
		findings, _ := shapeAudit(t, baseShapeForm(), ledger, shapeStandAbsent())
		requireKind(t, findings, "field-record-without-reason")
	})
}

// TestSubscriptionShapeCategoryMarkCanFail — признак категории оси.
func TestSubscriptionShapeCategoryMarkCanFail(t *testing.T) {
	t.Run("признак не проставлен — краснеет с координатой", func(t *testing.T) {
		f := baseShapeForm()
		f.Axes = strings.Replace(f.Axes,
			"  // project_id — проект. Категория: ЯКОРНАЯ.",
			"  // project_id — проект, к которому относятся события.", 1)
		findings, _ := shapeAudit(t, f, shapeStandLedger(), shapeStandAbsent())
		got := requireKind(t, findings, "axis-without-category")
		if got.Line == 0 || !strings.Contains(got.Reason, "project_id") {
			t.Fatalf("находка без координаты или без имени поля: %s", got.String())
		}
		t.Logf("%s", got.String())
	})

	t.Run("признак переизложен — молчание", func(t *testing.T) {
		// Близнец НЕ побайтовый: падеж другой, регистр другой, признак стоит
		// не в конце строки, а посреди абзаца, и вокруг него другой текст.
		f := baseShapeForm()
		f.Axes = strings.Replace(f.Axes,
			"  // project_id — проект. Категория: ЯКОРНАЯ.",
			"  // project_id — проект.\n"+
				"  //\n"+
				"  // Категория оси: якорная — по ней принимается решение о показе, и\n"+
				"  // владелец, не умеющий по ней отобрать, подписку отвергает.", 1)
		requireSilence(t, first(shapeAudit(t, f, shapeStandLedger(), shapeStandAbsent())))
	})
}

// TestSubscriptionShapeMandatoryAxisCanFail — ни одна ось не обязательна.
//
// Ось эта не вакуумна, и это ИЗМЕРЕНО: ключевого слова `required` в proto3 нет,
// но обязательность в этом дереве выражается ОПЦИЕЙ поля, и такие опции в
// контрактах живут. Первая редакция гейта объявляла проверку невозможной по
// построению — основание оказалось фольклором.
func TestSubscriptionShapeMandatoryAxisCanFail(t *testing.T) {
	t.Run("ось помечена обязательной ОДНОЙ СТРОКОЙ — находка", func(t *testing.T) {
		f := baseShapeForm()
		f.Axes = strings.Replace(f.Axes,
			"  string project_id = 2;",
			"  string project_id = 2 [(required) = true];", 1)
		findings, _ := shapeAudit(t, f, shapeStandLedger(), shapeStandAbsent())
		got := requireKind(t, findings, "axis-made-mandatory")
		if !strings.Contains(got.Reason, "project_id") {
			t.Fatalf("находка не называет оси: %s", got.String())
		}
		t.Logf("%s", got.String())
	})

	t.Run("пометка ПЕРЕНЕСЕНА на другую строку — находка", func(t *testing.T) {
		// Отдельная ось, и она несущая: именно так помечают поля в этом дереве —
		// блоком опций на несколько строк. Разбор, читающий только строку
		// объявления, промолчал бы на живой форме записи.
		f := baseShapeForm()
		f.Axes = strings.Replace(f.Axes,
			"  string project_id = 2;",
			"  string project_id = 2 [\n    (immutable) = true,\n    (required) = true\n  ];", 1)
		findings, _ := shapeAudit(t, f, shapeStandLedger(), shapeStandAbsent())
		requireKind(t, findings, "axis-made-mandatory")
	})

	t.Run("опции ЕСТЬ, но обязательности среди них нет — молчание", func(t *testing.T) {
		// Близнец не побайтовый: блок опций стоит, он многострочный, и слово
		// `required` встречается рядом — в комментарии, объясняющем, почему его
		// здесь нет. Гейт, ищущий слово в сыром тексте, покраснеет.
		f := baseShapeForm()
		f.Axes = strings.Replace(f.Axes,
			"  string project_id = 2;",
			"  // Обязательной эта ось не помечается: (required) = true здесь стоять\n"+
				"  // не вправе — незаданная ось не сужает.\n"+
				"  string project_id = 2 [\n    (immutable) = true\n  ];", 1)
		requireSilence(t, first(shapeAudit(t, f, shapeStandLedger(), shapeStandAbsent())))
	})
}

// TestSubscriptionShapeAbsentAxisCanFail — ось, которой нет ПО РЕШЕНИЮ.
func TestSubscriptionShapeAbsentAxisCanFail(t *testing.T) {
	t.Run("объявленная отсутствующей ось ЗАВЕДЕНА — находка", func(t *testing.T) {
		f := baseShapeForm()
		f.Axes += "\n\n  // name — имя ресурса. Категория: УДОБСТВА.\n  string name = 5;"
		ledger := append(shapeStandLedger(),
			SubscriptionFieldRecord{Field: "name", Role: SubscriptionRoleAxis, Why: "завели"})
		findings, _ := shapeAudit(t, f, ledger, shapeStandAbsent())
		requireKind(t, findings, "absent-axis-present")
	})

	t.Run("причина отсутствия НЕ названа — находка", func(t *testing.T) {
		f := baseShapeForm()
		f.ReqComment = "// SubscriptionRequest — запрос подписки."
		findings, _ := shapeAudit(t, f, shapeStandLedger(), shapeStandAbsent())
		got := requireKind(t, findings, "absent-axis-unexplained")
		t.Logf("%s", got.String())
	})

	t.Run("причина переизложена — молчание", func(t *testing.T) {
		// Близнец: тот же смысл, другой порядок, другие слова вокруг маркеров.
		f := baseShapeForm()
		f.ReqComment = `// SubscriptionRequest — запрос подписки.
//
// Чего здесь нет и почему. МЕТКИ — мутабельны: ресурс входит в выборку и
// выходит из неё по правке метки, а без предыдущего состояния выход неотличим
// от сноса. ИМЯ — косметический ярлык, адресуется ресурс идентификатором;
// подписка по имени замолкает после переименования и ничего об этом не говорит.`
		requireSilence(t, first(shapeAudit(t, f, shapeStandLedger(), shapeStandAbsent())))
	})
}

// TestSubscriptionShapeUnsetOutcomeCanFail — исход НЕЗАДАННОГО начала.
func TestSubscriptionShapeUnsetOutcomeCanFail(t *testing.T) {
	t.Run("исход не назван — находка", func(t *testing.T) {
		f := baseShapeForm()
		f.Start = strings.Replace(f.Start,
			"  // ИСХОД НЕЗАДАННОГО НАЗВАН: start не задан означает ровно CURRENT_END.",
			"  // Начало выбирается одной из двух ветвей.", 1)
		findings, _ := shapeAudit(t, f, shapeStandLedger(), shapeStandAbsent())
		requireKind(t, findings, "unset-outcome-unnamed")
	})

	t.Run("исход назван, но не называет объявленного значения — находка", func(t *testing.T) {
		// Отдельная ось: утверждение стоит, а величина, которую оно называет,
		// контрактом не объявлена. Такое утверждение читается как решение и
		// решением не является.
		f := baseShapeForm()
		f.Start = strings.Replace(f.Start,
			"  // ИСХОД НЕЗАДАННОГО НАЗВАН: start не задан означает ровно CURRENT_END.",
			"  // Если start не задан, сервер поступает по своему усмотрению.", 1)
		findings, _ := shapeAudit(t, f, shapeStandLedger(), shapeStandAbsent())
		requireKind(t, findings, "unset-outcome-unnamed")
	})

	t.Run("исход назван ДРУГИМ значением и другими словами — молчание", func(t *testing.T) {
		f := baseShapeForm()
		f.Start = strings.Replace(f.Start,
			"  // ИСХОД НЕЗАДАННОГО НАЗВАН: start не задан означает ровно CURRENT_END.",
			"  // Когда ветвь не задана, отдача идёт как при BEGINNING — с начала\n"+
				"  // журнала, всё, что владелец ещё удерживает.", 1)
		requireSilence(t, first(shapeAudit(t, f, shapeStandLedger(), shapeStandAbsent())))
	})

	t.Run("начало не ветвление — находка", func(t *testing.T) {
		f := baseShapeForm()
		f.Start = "  // start — позиция начала.\n  string start = 10;"
		ledger := append(shapeStandLedger()[:3],
			SubscriptionFieldRecord{Field: "start", Role: SubscriptionRoleStart, Why: "скаляр"})
		findings, _ := shapeAudit(t, f, ledger, shapeStandAbsent())
		requireKind(t, findings, "start-not-a-choice")
	})
}

// TestSubscriptionShapeCarrierCanFail — носитель нагрузки: ветвление и тип.
func TestSubscriptionShapeCarrierCanFail(t *testing.T) {
	t.Run("носитель не ветвление — находка", func(t *testing.T) {
		f := baseShapeForm()
		f.Event = strings.Replace(f.Event,
			"  oneof carrier {\n    google.protobuf.Any state = 10;\n    StateUnavailable state_unavailable = 11;\n  }",
			"  google.protobuf.Any state = 10;\n  StateUnavailable state_unavailable = 11;", 1)
		findings, _ := shapeAudit(t, f, shapeStandLedger(), shapeStandAbsent())
		requireKindSaying(t, findings, "carrier-not-a-choice", "не выражен ветвлением")
	})

	t.Run("ветвей три — находка", func(t *testing.T) {
		f := baseShapeForm()
		f.Event = strings.Replace(f.Event,
			"    StateUnavailable state_unavailable = 11;",
			"    StateUnavailable state_unavailable = 11;\n    string state_summary = 12;", 1)
		findings, _ := shapeAudit(t, f, shapeStandLedger(), shapeStandAbsent())
		requireKindSaying(t, findings, "carrier-not-a-choice", "а обязано ровно две")
	})

	t.Run("состояние свободной структурой — находка", func(t *testing.T) {
		f := baseShapeForm()
		f.Imports = `import "google/protobuf/struct.proto";`
		f.Event = strings.Replace(f.Event,
			"    google.protobuf.Any state = 10;",
			"    google.protobuf.Struct state = 10;", 1)
		findings, _ := shapeAudit(t, f, shapeStandLedger(), shapeStandAbsent())
		requireKind(t, findings, "carrier-state-untyped")
	})

	t.Run("ветви состояния нет вовсе — находка", func(t *testing.T) {
		// Отдельная ось: ветвление есть, а передать состояние нечем. Без неё
		// форма, у которой носитель сведён к одному признаку недоступности,
		// проходила бы: ветвление на месте, «свободной структуры» нет.
		f := baseShapeForm()
		f.Event = strings.Replace(f.Event,
			"    google.protobuf.Any state = 10;",
			"    StateUnavailable also_unavailable = 10;", 1)
		findings, _ := shapeAudit(t, f, shapeStandLedger(), shapeStandAbsent())
		requireKind(t, findings, "carrier-state-branch-missing")
	})

	t.Run("состояние ИМЕНОВАННЫМ сообщением — молчание", func(t *testing.T) {
		// Близнец не копия: тип другой (не `Any`, а объявленное здесь же
		// сообщение), порядок ветвей обратный, номера другие.
		f := baseShapeForm()
		f.Imports = ""
		f.Event = strings.Replace(f.Event,
			"  oneof carrier {\n    google.protobuf.Any state = 10;\n    StateUnavailable state_unavailable = 11;\n  }",
			"  message ResourceState {\n    string id = 1;\n  }\n\n"+
				"  oneof carrier {\n    StateUnavailable state_unavailable = 20;\n"+
				"    ResourceState state = 21;\n  }", 1)
		requireSilence(t, first(shapeAudit(t, f, shapeStandLedger(), shapeStandAbsent())))
	})
}

// TestSubscriptionShapeAnchorCanFail — авторизуемый якорь оболочки.
func TestSubscriptionShapeAnchorCanFail(t *testing.T) {
	t.Run("якоря в оболочке нет — находка", func(t *testing.T) {
		f := baseShapeForm()
		f.Event = strings.Replace(f.Event,
			"  // project_id — авторизуемый якорь, поле ОБОЛОЧКИ.\n  string project_id = 4;", "", 1)
		findings, _ := shapeAudit(t, f, shapeStandLedger(), shapeStandAbsent())
		requireKindSaying(t, findings, "anchor-not-in-envelope", "оболочка события не несёт")
	})

	t.Run("якорь ветвью ветвления — находка", func(t *testing.T) {
		f := baseShapeForm()
		f.Event = strings.Replace(f.Event,
			"  // project_id — авторизуемый якорь, поле ОБОЛОЧКИ.\n  string project_id = 4;", "", 1)
		f.Event = strings.Replace(f.Event,
			"    StateUnavailable state_unavailable = 11;",
			"    StateUnavailable state_unavailable = 11;\n    string project_id = 12;", 1)
		findings, _ := shapeAudit(t, f, shapeStandLedger(), shapeStandAbsent())
		requireKindSaying(t, findings, "anchor-not-in-envelope", "стоит ветвью ветвления")
	})

	t.Run("якорь ВНУТРИ нагрузки — находка", func(t *testing.T) {
		f := baseShapeForm()
		f.Event = strings.Replace(f.Event,
			"  message StateUnavailable {",
			"  message ResourcePayload {\n    string project_id = 1;\n  }\n\n  message StateUnavailable {", 1)
		findings, _ := shapeAudit(t, f, shapeStandLedger(), shapeStandAbsent())
		requireKind(t, findings, "anchor-inside-the-payload")
	})

	t.Run("якорь на верхнем уровне, но иначе записан — молчание", func(t *testing.T) {
		// Близнец: другой номер, другое место в теле, другой комментарий.
		f := baseShapeForm()
		f.Event = strings.Replace(f.Event,
			"  // project_id — авторизуемый якорь, поле ОБОЛОЧКИ.\n  string project_id = 4;", "", 1)
		f.Event = strings.Replace(f.Event, "  string position = 1;",
			"  string project_id = 40;\n  string position = 1;", 1)
		requireSilence(t, first(shapeAudit(t, f, shapeStandLedger(), shapeStandAbsent())))
	})
}

// TestSubscriptionShapeHorizonCanFail — горизонт выразим служебным сообщением.
func TestSubscriptionShapeHorizonCanFail(t *testing.T) {
	t.Run("поля горизонта нет — находка", func(t *testing.T) {
		f := baseShapeForm()
		f.Opened = strings.Replace(f.Opened, "  string earliest_resumable_position = 4;\n", "", 1)
		findings, _ := shapeAudit(t, f, shapeStandLedger(), shapeStandAbsent())
		got := requireKind(t, findings, "horizon-field-missing")
		if !strings.Contains(got.Reason, "earliest_resumable_position") {
			t.Fatalf("находка не называет поля: %s", got.String())
		}
	})

	t.Run("поля горизонта на месте, порядок другой — молчание", func(t *testing.T) {
		f := baseShapeForm()
		f.Opened = "  bool retains_everything = 5;\n  string earliest_resumable_position = 4;\n" +
			"  repeated string honored_filters = 3;\n  bool caught_up = 2;\n  string position = 1;"
		requireSilence(t, first(shapeAudit(t, f, shapeStandLedger(), shapeStandAbsent())))
	})
}

// TestSubscriptionShapeStopReasonCanFail — исход остановки не загоняется в данные.
func TestSubscriptionShapeStopReasonCanFail(t *testing.T) {
	t.Run("причина остановки полем события — находка", func(t *testing.T) {
		f := baseShapeForm()
		f.Event = strings.Replace(f.Event, "  string kind = 2;",
			"  string kind = 2;\n  string stop_reason = 30;", 1)
		findings, _ := shapeAudit(t, f, shapeStandLedger(), shapeStandAbsent())
		requireKind(t, findings, "stop-reason-mandated")
	})

	t.Run("то же имя ВНУТРИ вложенного сообщения — молчание", func(t *testing.T) {
		// Близнец несущий: `reason` внутри признака недоступности состояния —
		// законное поле, и оно уже стоит в базовой форме. Гейт, ищущий имя по
		// всему сообщению, покраснел бы на исправной форме.
		f := baseShapeForm()
		f.Event = strings.Replace(f.Event,
			"    // reason — почему состояния нет. Это НЕ причина остановки потока.\n    string reason = 1;",
			"    // reason — почему состояния нет.\n    string reason = 1;\n"+
				"    // error — та же полоса, тоже внутри нагрузки.\n    string error = 2;", 1)
		requireSilence(t, first(shapeAudit(t, f, shapeStandLedger(), shapeStandAbsent())))
	})
}

// TestSubscriptionShapePerimeterCanFail — ни глагола, ни зависимости от домена.
func TestSubscriptionShapePerimeterCanFail(t *testing.T) {
	t.Run("объявлен глагол — находка", func(t *testing.T) {
		f := baseShapeForm()
		f.Verbs = "service SubscriptionService {\n" +
			"  rpc Subscribe(SubscriptionRequest) returns (stream SubscriptionEvent);\n}"
		findings, _ := shapeAudit(t, f, shapeStandLedger(), shapeStandAbsent())
		requireKind(t, findings, "verb-declared")
	})

	t.Run("слова `service` и `rpc` В ПРОЗЕ — молчание", func(t *testing.T) {
		// Близнец несущий: гейт, ищущий слово в сыром тексте, находит его в
		// комментарии, ОБЪЯСНЯЮЩЕМ отсутствие глагола, и краснеет на исправной
		// форме — ровно тот класс, который правило запрещает.
		f := baseShapeForm()
		f.Verbs = "// Ни service, ни rpc здесь нет: метод подписки заводит следующая фаза\n" +
			"// вместе со своим сервером. Строка `rpc Subscribe(...) returns (stream ...)`\n" +
			"// приведена как то, чего в этом файле быть не должно."
		requireSilence(t, first(shapeAudit(t, f, shapeStandLedger(), shapeStandAbsent())))
	})

	t.Run("импорт доменного контракта — находка", func(t *testing.T) {
		f := baseShapeForm()
		f.Imports = `import "google/protobuf/any.proto";
import "kacho/cloud/vpc/v1/network_service.proto";`
		findings, _ := shapeAudit(t, f, shapeStandLedger(), shapeStandAbsent())
		requireKind(t, findings, "foreign-contract-dependency")
	})

	t.Run("импорт стандартного контракта — молчание", func(t *testing.T) {
		f := baseShapeForm()
		f.Imports = `import "google/protobuf/any.proto";
import "google/protobuf/timestamp.proto";`
		requireSilence(t, first(shapeAudit(t, f, shapeStandLedger(), shapeStandAbsent())))
	})
}

// TestSubscriptionShapeOwnerVocabularyCanFail — словарь видов принадлежит владельцу.
func TestSubscriptionShapeOwnerVocabularyCanFail(t *testing.T) {
	t.Run("словарь закрыт контрактом — находка", func(t *testing.T) {
		f := baseShapeForm()
		f.AnchorEnum += "\n\n// ResourceKind — словарь видов, закрытый КОНТРАКТОМ.\nenum ResourceKind {\n" +
			"  RESOURCE_KIND_UNSPECIFIED = 0;\n  NETWORK = 1;\n}"
		f.Axes = strings.Replace(f.Axes,
			"  repeated string kinds = 1;", "  repeated ResourceKind kinds = 1;", 1)
		findings, _ := shapeAudit(t, f, shapeStandLedger(), shapeStandAbsent())
		requireKind(t, findings, "owner-vocabulary-fixed-by-contract")
	})

	t.Run("перечисление рядом, но ось строковая — молчание", func(t *testing.T) {
		// Близнец: перечисление в файле ЕСТЬ (и даже названо похоже), но ось им
		// не типизирована. Гейт, ищущий «есть ли в файле словарь видов», ошибётся.
		f := baseShapeForm()
		f.AnchorEnum += "\n\n// KindHint — подсказка потребителю, осью НЕ является.\nenum KindHint {\n" +
			"  KIND_HINT_UNSPECIFIED = 0;\n  KIND_HINT_RESOURCE = 1;\n}"
		requireSilence(t, first(shapeAudit(t, f, shapeStandLedger(), shapeStandAbsent())))
	})
}

// TestSubscriptionShapeRefusesAnEmptyRead — премиса: разбор пуст — ОТКАЗ.
//
// # Утверждается ТЕКСТ отказа, а не факт ошибки
//
// Отказов у анализатора три, и они стоят цепочкой: не прочитан файл → разбор
// пуст → судится не тот файл. Снимешь первый — сработает второй, и `err != nil`
// выполнится за него: подпроба останется зелёной, ничего не наблюдая. Поэтому
// каждая называет СВОЁ сообщение, и снятие отказа краснит ту подпробу, которая
// его и проверяет.
func TestSubscriptionShapeRefusesAnEmptyRead(t *testing.T) {
	t.Run("контракта нет", func(t *testing.T) {
		root := subscriptionStand(t, map[string]string{"proto/.keep": ""})
		_, _, err := AuditSubscriptionFormShape(SubscriptionShapeOptions{
			Root: root, ProtoRoot: "proto", FormFile: "kacho/cloud/subscription/subscription.proto",
			RequestFields: shapeStandLedger(), AbsentAxes: shapeStandAbsent(),
			Expect: subscriptionShapeExpectation(),
		}, nil)
		if err == nil {
			t.Fatal("отсутствие контракта прошло молча — «находок ноль» стало " +
				"неотличимо от «прочитано ноль»")
		}
		if !strings.Contains(err.Error(), "не прочитан") {
			t.Fatalf("отказ не про ЧТЕНИЕ файла — сработал следующий в цепочке, "+
				"и подпроба зелена не о том: %v", err)
		}
	})

	t.Run("файл есть, объявлений нет", func(t *testing.T) {
		f := shapeForm{Package: "kacho.cloud.subscription"}
		root := subscriptionStand(t, map[string]string{
			"proto/kacho/cloud/subscription/subscription.proto": `syntax = "proto3";
package kacho.cloud.subscription;
// Здесь ничего не объявлено.
`})
		_ = f
		_, _, err := AuditSubscriptionFormShape(SubscriptionShapeOptions{
			Root: root, ProtoRoot: "proto", FormFile: "kacho/cloud/subscription/subscription.proto",
			RequestFields: shapeStandLedger(), AbsentAxes: shapeStandAbsent(),
			Expect: subscriptionShapeExpectation(),
		}, nil)
		if err == nil {
			t.Fatal("пустой разбор прошёл молча — всякое утверждение о форме было бы " +
				"утверждением ни о чём")
		}
		if !strings.Contains(err.Error(), "разбор пуст") {
			t.Fatalf("отказ не про ПУСТОЙ РАЗБОР — файл прочитан, значит сработал "+
				"чужой отказ: %v", err)
		}
	})

	t.Run("судится не тот файл", func(t *testing.T) {
		f := baseShapeForm()
		f.Package = "kacho.cloud.demo.v1"
		root := subscriptionStand(t, map[string]string{
			"proto/kacho/cloud/subscription/subscription.proto": f.render(),
		})
		_, _, err := AuditSubscriptionFormShape(SubscriptionShapeOptions{
			Root: root, ProtoRoot: "proto", FormFile: "kacho/cloud/subscription/subscription.proto",
			RequestFields: shapeStandLedger(), AbsentAxes: shapeStandAbsent(),
			Expect: subscriptionShapeExpectation(),
		}, nil)
		if err == nil {
			t.Fatal("контракт чужого пакета принят за общую форму — вердикт относился бы " +
				"не к тому предмету")
		}
		// Отказ обязан назвать ОБА пакета — прочитанный и ожидаемый: иначе
		// «судит не тот файл» неотличимо от «файл не разобрался».
		if !strings.Contains(err.Error(), "не тот файл") ||
			!strings.Contains(err.Error(), f.Package) {
			t.Fatalf("отказ не называет ЧУЖОГО ПАКЕТА — сработал отказ разбора, "+
				"а не дискриминатора предмета: %v", err)
		}
	})
}

// TestSubscriptionShapeMissingMessageCanFail — сообщения формы обязаны быть.
//
// Ось отдельная и несущая: без неё контракт, потерявший целое сообщение,
// проходил бы молча — утверждения о нём просто не на чем было бы проверить, и
// «находок ноль» означало бы «предмета ноль».
func TestSubscriptionShapeMissingMessageCanFail(t *testing.T) {
	t.Run("служебного сообщения нет — находка", func(t *testing.T) {
		f := baseShapeForm()
		f.Opened = ""
		root := subscriptionStand(t, map[string]string{
			"proto/kacho/cloud/subscription/subscription.proto": strings.Replace(
				f.render(),
				"// SubscriptionOpened — служебное первое сообщение.\nmessage SubscriptionOpened {\n\n}\n",
				"", 1),
		})
		findings, _, err := AuditSubscriptionFormShape(SubscriptionShapeOptions{
			Root: root, ProtoRoot: "proto", FormFile: "kacho/cloud/subscription/subscription.proto",
			RequestFields: shapeStandLedger(), AbsentAxes: shapeStandAbsent(),
			Expect: subscriptionShapeExpectation(),
		}, nil)
		if err != nil {
			t.Fatalf("анализатор не отработал: %v", err)
		}
		found := false
		for _, x := range findings {
			if x.Kind == "missing-message" && strings.Contains(x.Reason, "SubscriptionOpened") {
				found = true
				t.Logf("%s", x.String())
			}
		}
		if !found {
			t.Fatalf("гейт промолчал на форме без служебного сообщения: %v", kinds(findings))
		}
	})

	t.Run("все три сообщения на месте, порядок другой — молчание", func(t *testing.T) {
		// Близнец: сообщения переставлены местами. Гейт, ключующийся на порядке
		// объявления, здесь ошибётся.
		f := baseShapeForm()
		body := f.render()
		requireSilence(t, first(shapeAuditRaw(t, body, shapeStandLedger(), shapeStandAbsent())))
	})
}

// shapeAuditRaw — прогон анализатора по готовому тексту контракта.
func shapeAuditRaw(
	t *testing.T, body string,
	ledger []SubscriptionFieldRecord, absent []SubscriptionAbsentAxis,
) ([]SubscriptionShapeFinding, SubscriptionShapeCensus) {
	t.Helper()
	root := subscriptionStand(t, map[string]string{
		"proto/kacho/cloud/subscription/subscription.proto": body,
	})
	findings, census, err := AuditSubscriptionFormShape(SubscriptionShapeOptions{
		Root: root, ProtoRoot: "proto", FormFile: "kacho/cloud/subscription/subscription.proto",
		RequestFields: ledger, AbsentAxes: absent,
		Expect: subscriptionShapeExpectation(),
	}, nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	return findings, census
}

// first — сахар: взять находки из пары «находки, перепись».
func first(findings []SubscriptionShapeFinding, _ SubscriptionShapeCensus) []SubscriptionShapeFinding {
	return findings
}
