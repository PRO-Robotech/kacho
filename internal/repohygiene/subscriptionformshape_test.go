// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// subscriptionformshape_test.go — ГЕЙТ формы единой подписки на НАСТОЯЩЕМ дереве.
//
// Механика и полный перечень утверждений — в `subscriptionformshape.go`; здесь
// закрытый перечень полей, оси, которых нет по решению, и вердикт о дереве.
//
// Способность гейта падать доказывается не этим прогоном (он зелен по
// построению: гейт написан после контракта), а инъекцией —
// `subscriptionformshape_injection_test.go`, где каждая ось предъявляется
// дефектом и законным близнецом.
package repohygiene

import (
	"strings"
	"testing"
)

// subscriptionRequestFieldLedger — ЗАКРЫТЫЙ перечень полей запроса подписки.
//
// Он закрыт не ради строгости. Ось заводится «для удобства», и каждое отдельное
// добавление защитимо: домену нужно фильтровать по имени, по метке, по времени.
// Через три таких добавления форма перестаёт быть общей, а решение о каждом
// нигде не записано — возразить следующему будет нечем. Перечень делает
// добавление ВИДИМЫМ: поле заводится вместе с записью, запись называет роль и
// причину, причина читается на обзоре.
//
// Признак категории живёт НЕ ЗДЕСЬ, а в самом контракте, рядом с полем, — и это
// осознанно: два места об одном предмете разошлись бы молча, а гейт требует
// признака там, где его прочтёт всякий, кто читает форму.
var subscriptionRequestFieldLedger = []SubscriptionFieldRecord{
	{
		Field: "kinds", Role: SubscriptionRoleAxis,
		Why: "виды предметов владельца. Словарь принадлежит владельцу и им закрыт: " +
			"перечисление, объявленное контрактом, не вместило бы владельца, чьи события " +
			"не про ресурс, а про субъект прав",
	},
	{
		Field: "project_id", Role: SubscriptionRoleAxis,
		Why: "проект, к которому относятся события. Ось якорная: по ней принимается " +
			"решение о показе, поэтому владелец, не умеющий по ней отобрать, отвергает " +
			"подписку, а не открывает её с оговоркой",
	},
	{
		Field: "ids", Role: SubscriptionRoleAxis,
		Why: "неизменяемые идентификаторы предметов — адресный отбор. Единственная ось, " +
			"принимающая произвольную строку, и потому единственный путь протащить " +
			"мутабельную адресацию через иммутабельную",
	},
	{
		Field: "anchor", Role: SubscriptionRoleStart,
		Why: "назвать место начала словом: с начала журнала либо с текущего конца",
	},
	{
		Field: "position", Role: SubscriptionRoleStart,
		Why: "непрозрачный токен, выданный сервером. Возвращается дословно; клиент его " +
			"не разбирает, не сравнивает и не конструирует",
	},
}

// subscriptionAbsentAxes — оси, которых в форме нет ПО РЕШЕНИЮ.
//
// Отсутствие поля само по себе ничего не сообщает: от пропуска оно неотличимо.
// Запись здесь плюс требование назвать причину в самом контракте превращают
// пропуск в решение — и делают его самоистекающим: заведут поле, и запись
// станет находкой.
var subscriptionAbsentAxes = []SubscriptionAbsentAxis{
	{
		Axis: "name", Marker: "ИМЯ",
		Why: "имя — мутабельный косметический ярлык, а адресуется ресурс неизменяемым id. " +
			"Подписка по имени МОЛЧА перестаёт совпадать после переименования: ни события, " +
			"ни ошибки — поток просто замолкает навсегда",
	},
	{
		Axis: "labels", Marker: "МЕТКИ",
		Why: "метки мутабельны, и ресурс входит в выборку и выходит из неё по их правке. " +
			"Без предыдущего состояния выход из выборки неотличим от удаления — подписчик " +
			"принял бы правку метки за снос",
	},
}

// subscriptionShapeExpectation — что форма обязана нести структурно.
func subscriptionShapeExpectation() SubscriptionShapeExpectation {
	return SubscriptionShapeExpectation{
		RequestMessage:      "SubscriptionRequest",
		OpenedMessage:       "SubscriptionOpened",
		EventMessage:        "SubscriptionEvent",
		StartOneof:          "start",
		StartAnchorBranch:   "anchor",
		CarrierOneof:        "carrier",
		CarrierStateBranch:  "state",
		UntypedCarriers:     []string{"google.protobuf.Struct", "bytes", "string"},
		AuthorizationAnchor: "project_id",
		HorizonFields:       []string{"earliest_resumable_position", "retains_everything"},
		// Закрытый список имён исхода остановки. Он не претендует на полноту
		// языка — он называет формы, которыми этот исход заводят чаще всего, и
		// потому ловит его в момент внесения, а не через фазу.
		StopReasonNames:     []string{"stop_reason", "termination_reason", "close_reason", "reason", "error", "status"},
		OwnerVocabularyAxis: "kinds",
		OwnerVocabularyType: "string",
		// Обязательность в этом дереве выражается ОПЦИЕЙ поля, а не ключевым
		// словом: `required` из proto3 убрано, а опция жива и употребляется.
		MandatoryOption: "(required) = true",
	}
}

func subscriptionShapeOptions(t *testing.T) SubscriptionShapeOptions {
	t.Helper()
	return SubscriptionShapeOptions{
		Root:          repoRoot(t),
		ProtoRoot:     "proto",
		FormFile:      "kacho/cloud/subscription/subscription.proto",
		RequestFields: subscriptionRequestFieldLedger,
		AbsentAxes:    subscriptionAbsentAxes,
		Expect:        subscriptionShapeExpectation(),
	}
}

// TestSubscriptionFormShape — WATCH-1-06/07/10/11/22/27/30/33/35 на дереве.
func TestSubscriptionFormShape(t *testing.T) {
	var log strings.Builder
	findings, census, err := AuditSubscriptionFormShape(subscriptionShapeOptions(t), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	// Премиса разбора. Форма могла переехать, а разбор — перестать её видеть;
	// в обоих случаях «находок ноль» означало бы «прочитано ноль».
	if census.TopTypes < 4 || census.Fields < 10 {
		t.Fatalf("разобрано типов верхнего уровня %d, полей %d — разбор сломан, "+
			"и всякое утверждение о форме было бы утверждением ни о чём",
			census.TopTypes, census.Fields)
	}
	// Дискриминатор осей ключуется на перечне. Ноль осей означает, что перечень
	// с формой разошёлся целиком, и половина утверждений не проверялась.
	if census.Axes != len(subscriptionRequestFieldLedger)-2 {
		t.Fatalf("осей распознано %d — перечень и форма разошлись, и признак категории "+
			"не спрашивался ни у одной", census.Axes)
	}
	if census.Assertions < 25 {
		t.Fatalf("утверждений проверено %d — слишком мало, чтобы вердикт относился ко "+
			"всей форме", census.Assertions)
	}

	for _, f := range findings {
		t.Errorf("%s", f.String())
	}
}
