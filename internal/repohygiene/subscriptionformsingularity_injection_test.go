// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// Инъекция гейта единственности на стенде — обе стороны по каждой оси, которую
// этот гейт судит.

// ── ось «единственность»: доменное объявление против импорта общего типа ─────

// TestSubscriptionSingularity_CatchesDomainOwnRequest — ДЕФЕКТ: домен объявил
// свой запрос подписки, послабления на него нет.
func TestSubscriptionSingularity_CatchesDomainOwnRequest(t *testing.T) {
	root := subscriptionStand(t, map[string]string{
		"proto/kacho/cloud/subscription/subscription.proto": standCommonForm,
		"proto/kacho/cloud/demo/v1/demo_watch.proto":        standDomainOwnRequest,
		"proto/kacho/cloud/other/v1/other.proto":            standFiller,
	})

	findings, census, err := AuditSubscriptionFormSingularity(subscriptionStandOptions(root), nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 1 || findings[0].Kind != "undeclared-domain-request" {
		t.Fatalf("доменное объявление вне ведомости не поймано: находок %d (%v), объявлений прочитано %d",
			len(findings), findings, census.RequestDecls)
	}
	if !strings.Contains(findings[0].String(), "demo_watch.proto") {
		t.Errorf("находка не называет файл: %s", findings[0].String())
	}
	if !strings.Contains(findings[0].String(), "kacho.cloud.demo.v1.WatchRequest") {
		t.Errorf("находка не называет символ: %s", findings[0].String())
	}
}

// TestSubscriptionSingularity_SilentWhenDomainImportsCommonForm — ЗАКОННЫЙ
// БЛИЗНЕЦ: тот же домен, тот же путь файла, но общий тип импортируется.
func TestSubscriptionSingularity_SilentWhenDomainImportsCommonForm(t *testing.T) {
	root := subscriptionStand(t, map[string]string{
		"proto/kacho/cloud/subscription/subscription.proto": standCommonForm,
		"proto/kacho/cloud/demo/v1/demo_watch.proto":        standDomainImportsCommon,
		"proto/kacho/cloud/other/v1/other.proto":            standFiller,
	})

	findings, census, err := AuditSubscriptionFormSingularity(subscriptionStandOptions(root), nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("импорт общего типа принят за второе объявление — гейт ловит форму, а не существо: %s",
			findings[0].String())
	}
	// Премиса контроля: молчание получено на прочитанном дереве, а не на пустом,
	// и общая форма при этом ЗАСЧИТАНА — иначе молчание досталось бы даром.
	if census.ProtoFiles != 3 || census.CommonDecls != 1 || census.DomainDecls != 0 {
		t.Fatalf("файлов %d, общих объявлений %d, доменных %d — контроль молчал вхолостую",
			census.ProtoFiles, census.CommonDecls, census.DomainDecls)
	}
}

// TestSubscriptionSingularity_DomainRequestIsExcusedByLedger — та же сторона с
// другого конца: доменное объявление, СТОЯЩЕЕ в ведомости, прогон не роняет.
func TestSubscriptionSingularity_DomainRequestIsExcusedByLedger(t *testing.T) {
	root := subscriptionStand(t, map[string]string{
		"proto/kacho/cloud/subscription/subscription.proto": standCommonForm,
		"proto/kacho/cloud/demo/v1/demo_watch.proto":        standDomainOwnRequest,
		"proto/kacho/cloud/other/v1/other.proto":            standFiller,
	})
	excuse := SubscriptionRequestAllowance{
		Symbol: "kacho.cloud.demo.v1.WatchRequest", Issue: "kacho#0",
		Reason: "домен переводится позже", ExpiresWhen: "объявление снято вместе с переводом",
	}

	findings, census, err := AuditSubscriptionFormSingularity(subscriptionStandOptions(root, excuse), nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("послабление не сработало: %s", findings[0].String())
	}
	// Ожидание ВЫВЕДЕНО из ведомости, а не выписано константой.
	if census.Expected != 2 || census.RequestDecls != 2 {
		t.Errorf("ожидалось %d, объявлений %d — ожидание не выводится из ведомости",
			census.Expected, census.RequestDecls)
	}
}

// ── ось «послабление»: запись, которой нечего исключать ─────────────────────

// TestSubscriptionSingularity_CatchesStaleAllowance — ДЕФЕКТ: запись ведомости
// стоит, а объявления, которое она исключала, в дереве нет.
func TestSubscriptionSingularity_CatchesStaleAllowance(t *testing.T) {
	root := subscriptionStand(t, map[string]string{
		"proto/kacho/cloud/subscription/subscription.proto": standCommonForm,
		"proto/kacho/cloud/other/v1/other.proto":            standFiller,
	})
	stale := SubscriptionRequestAllowance{
		Symbol: "kacho.cloud.demo.v1.WatchRequest", Issue: "kacho#0",
		Reason: "домен переводится позже", ExpiresWhen: "объявление снято вместе с переводом",
	}

	findings, _, err := AuditSubscriptionFormSingularity(subscriptionStandOptions(root, stale), nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 1 || findings[0].Kind != "stale-allowance" {
		t.Fatalf("истёкшее послабление не поймано: находок %d (%v)", len(findings), findings)
	}
	if !strings.Contains(findings[0].String(), "kacho.cloud.demo.v1.WatchRequest") {
		t.Errorf("находка не называет запись: %s", findings[0].String())
	}
}

// TestSubscriptionSingularity_EmptyLedgerIsTheGoalNotAFailure — вторая сторона
// той же оси, и она про ЦЕЛЬ: ведомость, в которой не осталось записей, — то
// состояние, ради которого эпик заведён. Падение на нём толкало бы держать
// запись ради зелёного.
func TestSubscriptionSingularity_EmptyLedgerIsTheGoalNotAFailure(t *testing.T) {
	root := subscriptionStand(t, map[string]string{
		"proto/kacho/cloud/subscription/subscription.proto": standCommonForm,
		"proto/kacho/cloud/other/v1/other.proto":            standFiller,
	})

	findings, census, err := AuditSubscriptionFormSingularity(subscriptionStandOptions(root), nil)
	if err != nil {
		t.Fatalf("пустая ведомость обязана проходить, а не ронять прогон: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("пустая ведомость дала находку: %s", findings[0].String())
	}
	if census.Allowances != 0 || census.Expected != 1 || census.RequestDecls != 1 {
		t.Errorf("послаблений %d, ожидалось %d, объявлений %d — не то состояние, о котором проба",
			census.Allowances, census.Expected, census.RequestDecls)
	}
}

// ── премисы: то, на чём вердикт получен ─────────────────────────────────────

// TestSubscriptionSingularity_MissingCommonFormIsAFinding — состояние ДО Ф1:
// общей формы нет. Именно на нём гейт обязан быть красным, иначе он не различает
// «форма объявлена» и «форма не объявлена вовсе».
func TestSubscriptionSingularity_MissingCommonFormIsAFinding(t *testing.T) {
	root := subscriptionStand(t, map[string]string{
		"proto/kacho/cloud/other/v1/other.proto": standFiller,
	})

	findings, census, err := AuditSubscriptionFormSingularity(subscriptionStandOptions(root), nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 1 || findings[0].Kind != "missing-common-form" {
		t.Fatalf("отсутствие общей формы не поймано: находок %d (%v)", len(findings), findings)
	}
	if census.TopLevelMessages == 0 {
		t.Fatal("сообщений прочитано ноль — вердикт получен на пустом обходе")
	}
}

// TestSubscriptionSingularity_SecondFormInCommonPackageIsAFinding — второй язык
// фильтров рядом с общим: он не доменный, поэтому ведомостью не покрывается, и
// поймать его обязан отдельный исход.
func TestSubscriptionSingularity_SecondFormInCommonPackageIsAFinding(t *testing.T) {
	second := `syntax = "proto3";
package kacho.cloud.subscription;
message LegacySubscriptionRequest {
  string anything = 1;
}
`
	root := subscriptionStand(t, map[string]string{
		"proto/kacho/cloud/subscription/subscription.proto": standCommonForm,
		"proto/kacho/cloud/subscription/legacy.proto":       second,
		"proto/kacho/cloud/other/v1/other.proto":            standFiller,
	})

	findings, _, err := AuditSubscriptionFormSingularity(subscriptionStandOptions(root), nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 1 || findings[0].Kind != "second-form-in-common-package" {
		t.Fatalf("второй запрос в общем пакете не пойман: находок %d (%v)", len(findings), findings)
	}
}

// TestSubscriptionSingularity_NestedNameIsNotADeclaration — вложенное сообщение
// с подходящим именем формой подписки не является. Стенд несёт такое внутри
// общей формы: гейт, считающий по сырому тексту, засчитал бы его.
func TestSubscriptionSingularity_NestedNameIsNotADeclaration(t *testing.T) {
	root := subscriptionStand(t, map[string]string{
		"proto/kacho/cloud/subscription/subscription.proto": standCommonForm,
		"proto/kacho/cloud/other/v1/other.proto":            standFiller,
	})

	_, census, err := AuditSubscriptionFormSingularity(subscriptionStandOptions(root), nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if census.NestedMessages == 0 {
		t.Fatal("вложенных сообщений прочитано ноль — стенд не создал условия пробы")
	}
	if census.RequestDecls != 1 {
		t.Errorf("объявлений насчитано %d при одном настоящем — вложенное или комментарий засчитаны",
			census.RequestDecls)
	}
}

// TestSubscriptionSingularity_EmptyWalkIsAnError — премиса: пустое дерево не
// даёт «ноль находок».
func TestSubscriptionSingularity_EmptyWalkIsAnError(t *testing.T) {
	root := subscriptionStand(t, map[string]string{"proto/.keep": ""})
	if _, _, err := AuditSubscriptionFormSingularity(subscriptionStandOptions(root), nil); err == nil {
		t.Fatal("пустой обход прошёл как «ноль находок» — гейт инертен и об этом не сообщает")
	}
}

// TestSubscriptionSingularity_AllowanceWithoutIssueIsRejected — послабление без
// задачи не может истечь даже в принципе, поэтому не принимается вовсе.
func TestSubscriptionSingularity_AllowanceWithoutIssueIsRejected(t *testing.T) {
	root := subscriptionStand(t, map[string]string{
		"proto/kacho/cloud/subscription/subscription.proto": standCommonForm,
		"proto/kacho/cloud/other/v1/other.proto":            standFiller,
	})
	noIssue := SubscriptionRequestAllowance{Symbol: "kacho.cloud.demo.v1.WatchRequest", Reason: "потом"}
	if _, _, err := AuditSubscriptionFormSingularity(subscriptionStandOptions(root, noIssue), nil); err == nil {
		t.Fatal("послабление без задачи принято — истечь ему нечем")
	}
}

// ── ось «признак — СВОЙСТВО ОБЪЯВЛЕНИЯ, а не форма имени» (задача #1072) ─────
//
// Прогонов по этой оси ТРИ, и третий обязателен. Инъекция, которая попутно
// нарушает уже существующий контроль, доказательством не является: красное
// пришло бы от соседа, а новая ветвь могла бы оказаться вакуумной, не показав
// этого ничем. Поэтому: контроль (всё цело — молчат обе ветви) · инъекция
// НОВОГО свойства (краснеет только новая ветвь) · инъекция СТАРОГО (краснеет
// только прежняя ветвь имени).

// TestSubscriptionSingularity_ControlBothBranchesSilent — ПРОГОН 1, КОНТРОЛЬ:
// дерево несёт общую форму и оба законных близнеца сразу. Молчать обязаны обе
// ветви, и молчание получено на прочитанном дереве, а не на пустом.
func TestSubscriptionSingularity_ControlBothBranchesSilent(t *testing.T) {
	root := subscriptionStand(t, map[string]string{
		"proto/kacho/cloud/subscription/subscription.proto": standCommonForm,
		"proto/kacho/cloud/other/v1/paged_list.proto":       standPagedListTwin,
		"proto/kacho/cloud/other/v1/upload.proto":           standClientStreamingTwin,
		"proto/kacho/cloud/other/v1/nested.proto":           standNestedShapeIsNotADeclaration,
		"proto/kacho/cloud/other/v1/other.proto":            standFiller,
	})

	findings, census, err := AuditSubscriptionFormSingularity(subscriptionStandOptions(root), nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("контроль дал находку — красное на инъекции пришло бы не от инъекции: %s",
			findings[0].String())
	}
	// Премиса контроля: обе ветви ЧТО-ТО наблюдали. Ветвь без предмета молчит
	// даром, и её молчание неотличимо от слепоты.
	if census.MessagesWithFields == 0 {
		t.Fatal("сообщений с разобранными полями ноль — ветвь состава не наблюдала ничего")
	}
	if census.RPCs == 0 {
		t.Fatal("глаголов прочитано ноль — ветвь употребления не наблюдала ничего")
	}
	// И ни один близнец не опознан: страничный список несёт ось видов И позицию,
	// клиентский поток несёт слово `stream`, вложенное сообщение несёт весь состав.
	if census.ByShape != 1 || census.ByStreaming != 0 || census.ByName != 1 {
		t.Errorf("по составу опознано %d, по употреблению %d, по имени %d — ожидалось 1/0/1 "+
			"(одна общая форма ветвями состава и имени; потоковых глаголов на стенде нет): "+
			"близнец принят за подписку", census.ByShape, census.ByStreaming, census.ByName)
	}
}

// TestSubscriptionSingularity_CatchesForeignNamedSubscription — ПРОГОН 2,
// ИНЪЕКЦИЯ НОВОГО СВОЙСТВА: три имени вне семейства, каждое со своим словарём
// полей. Прежний гейт пропускал их молча — это и есть предмет задачи #1072.
//
// Существующего контроля инъекция не задевает: ни одно из трёх имён семейству не
// принадлежит, поэтому ветвь имени обязана остаться на нуле, и красное приходит
// ровно от новой ветви.
func TestSubscriptionSingularity_CatchesForeignNamedSubscription(t *testing.T) {
	for _, tc := range []struct {
		file, body, symbol, where string
	}{
		{"demo_watch.proto", standForeignNamedSubscription,
			"kacho.cloud.demo.v1.WatchNetworksRequest", "demo_watch.proto"},
		{"demo_tail.proto", standTailEventsRequest,
			"kacho.cloud.demo.v1.TailEventsRequest", "demo_tail.proto"},
		{"demo_feed.proto", standFeedRequest,
			"kacho.cloud.demo.v1.FeedRequest", "demo_feed.proto"},
	} {
		t.Run(tc.symbol, func(t *testing.T) {
			root := subscriptionStand(t, map[string]string{
				"proto/kacho/cloud/subscription/subscription.proto": standCommonForm,
				"proto/kacho/cloud/demo/v1/" + tc.file:              tc.body,
				"proto/kacho/cloud/other/v1/paged_list.proto":       standPagedListTwin,
				"proto/kacho/cloud/other/v1/other.proto":            standFiller,
			})

			findings, census, err := AuditSubscriptionFormSingularity(subscriptionStandOptions(root), nil)
			if err != nil {
				t.Fatalf("анализатор не отработал: %v", err)
			}
			if len(findings) != 1 || findings[0].Kind != "undeclared-domain-request" {
				t.Fatalf("запрос подписки под чужим именем не пойман: находок %d (%v); "+
					"опознано по составу %d, по имени %d",
					len(findings), findings, census.ByShape, census.ByName)
			}
			if !strings.Contains(findings[0].String(), tc.symbol) {
				t.Errorf("находка не называет символ: %s", findings[0].String())
			}
			if !strings.Contains(findings[0].String(), tc.where) {
				t.Errorf("находка не называет файл: %s", findings[0].String())
			}
			// Красное пришло от НОВОЙ ветви: прежняя эти имена не узнаёт вовсе.
			if census.ByName != 1 {
				t.Errorf("ветвью имени опознано %d при одной только общей форме — "+
					"инъекция задела существующий контроль, и вердикт о новой ветви недействителен",
					census.ByName)
			}
			if census.ByShape != 2 {
				t.Errorf("ветвью состава опознано %d, ожидалось 2 (общая форма + инъекция)",
					census.ByShape)
			}
		})
	}
}

// TestSubscriptionSingularity_CatchesSubscriptionByStreamingUse — ПРОГОН 2,
// вторая новая ветвь: имени семейства нет, состава подписки нет, но сообщение
// стоит входом СЕРВЕРНО-ПОТОКОВОГО глагола. Эта ветвь имён не читает вовсе и
// потому переименованием не снимается ни при каком имени.
func TestSubscriptionSingularity_CatchesSubscriptionByStreamingUse(t *testing.T) {
	root := subscriptionStand(t, map[string]string{
		"proto/kacho/cloud/subscription/subscription.proto": standCommonForm,
		"proto/kacho/cloud/demo/v1/demo_feed.proto":         standStreamingVerbOverPlainName,
		"proto/kacho/cloud/other/v1/upload.proto":           standClientStreamingTwin,
		"proto/kacho/cloud/other/v1/other.proto":            standFiller,
	})

	findings, census, err := AuditSubscriptionFormSingularity(subscriptionStandOptions(root), nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 1 || findings[0].Kind != "undeclared-domain-request" {
		t.Fatalf("вход потокового глагола не пойман: находок %d (%v)", len(findings), findings)
	}
	if !strings.Contains(findings[0].String(), "kacho.cloud.demo.v1.DemoFeedInput") {
		t.Errorf("находка не называет символ: %s", findings[0].String())
	}
	if !strings.Contains(findings[0].String(), "demo_feed.proto") {
		t.Errorf("находка не называет файл: %s", findings[0].String())
	}
	// Опознано именно УПОТРЕБЛЕНИЕМ, а не именем и не составом: у `DemoFeedInput`
	// ни того, ни другого нет.
	if census.ByStreaming != 1 {
		t.Errorf("ветвью употребления опознано %d, ожидалась одна", census.ByStreaming)
	}
	if census.ByName != 1 || census.ByShape != 1 {
		t.Errorf("по имени %d, по составу %d — ожидалось по одной (только общая форма): "+
			"инъекция задела соседние ветви, и вердикт о ветви употребления недействителен",
			census.ByName, census.ByShape)
	}
	// Клиентский поток остался невидим: слово `stream` в глаголе есть, но на
	// стороне запроса, и подпиской это не делает.
	if census.RPCs != 2 || census.StreamingRPCs != 1 {
		t.Errorf("глаголов %d, серверно-потоковых %d — ожидалось 2 и 1: "+
			"загрузка потоком принята за подписку либо глаголы не прочитаны",
			census.RPCs, census.StreamingRPCs)
	}
}

// TestSubscriptionSingularity_NameBranchStillCatchesShapelessRequest — ПРОГОН 3,
// ИНЪЕКЦИЯ СУЩЕСТВУЮЩЕГО: имя из семейства при полном отсутствии состава
// подписки. Краснеть обязана ТОЛЬКО прежняя ветвь.
//
// Без этого прогона молчание ветви имени в двух предыдущих неотличимо от её
// смерти: расширение распознавателя могло бы незаметно вытеснить то, что гейт
// умел с самого начала.
func TestSubscriptionSingularity_NameBranchStillCatchesShapelessRequest(t *testing.T) {
	root := subscriptionStand(t, map[string]string{
		"proto/kacho/cloud/subscription/subscription.proto": standCommonForm,
		"proto/kacho/cloud/demo/v1/demo_name_only.proto":    standNameOnlyRequest,
		"proto/kacho/cloud/other/v1/paged_list.proto":       standPagedListTwin,
		"proto/kacho/cloud/other/v1/other.proto":            standFiller,
	})

	findings, census, err := AuditSubscriptionFormSingularity(subscriptionStandOptions(root), nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 1 || findings[0].Kind != "undeclared-domain-request" {
		t.Fatalf("прежняя ветвь имени умерла при расширении: находок %d (%v)", len(findings), findings)
	}
	if !strings.Contains(findings[0].String(), "kacho.cloud.demo.v1.SubscribeRequest") {
		t.Errorf("находка не называет символ: %s", findings[0].String())
	}
	// Красное пришло от СТАРОЙ ветви: состава подписки у этого сообщения нет.
	if census.ByName != 2 {
		t.Errorf("ветвью имени опознано %d, ожидалось 2 (общая форма + инъекция)", census.ByName)
	}
	if census.ByShape != 1 {
		t.Errorf("ветвью состава опознано %d при одной только общей форме — "+
			"инъекция задела новую ветвь, и вердикт о существующей недействителен",
			census.ByShape)
	}
}

// TestSubscriptionSingularity_SilentOnPagedListTwin — ЗАКОННЫЙ БЛИЗНЕЦ ветви
// состава, предъявленный отдельно и в одиночку: страничный список несёт ось
// видов И поле позиции, и разделяет их только размер страницы.
func TestSubscriptionSingularity_SilentOnPagedListTwin(t *testing.T) {
	root := subscriptionStand(t, map[string]string{
		"proto/kacho/cloud/subscription/subscription.proto": standCommonForm,
		"proto/kacho/cloud/other/v1/paged_list.proto":       standPagedListTwin,
	})

	findings, census, err := AuditSubscriptionFormSingularity(subscriptionStandOptions(root), nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("страничный список принят за подписку — ветвь состава ловит форму, а не существо: %s",
			findings[0].String())
	}
	// Премиса: близнец ПРОЧИТАН и его поля разобраны, иначе молчание досталось даром.
	if census.MessagesWithFields < 3 {
		t.Fatalf("сообщений с разобранными полями %d — близнец не прочитан, молчание беспредметно",
			census.MessagesWithFields)
	}
}

// TestSubscriptionSingularity_SilentOnNestedShape — законный близнец РАЗБОРА
// ТЕЛА: весь состав подписки лежит во вложенном сообщении, владелец его не
// несёт. Гейт, читающий поля построчно, засчитал бы их владельцу.
func TestSubscriptionSingularity_SilentOnNestedShape(t *testing.T) {
	root := subscriptionStand(t, map[string]string{
		"proto/kacho/cloud/subscription/subscription.proto": standCommonForm,
		"proto/kacho/cloud/other/v1/nested.proto":           standNestedShapeIsNotADeclaration,
	})

	findings, census, err := AuditSubscriptionFormSingularity(subscriptionStandOptions(root), nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("вложенный состав засчитан владельцу: %s", findings[0].String())
	}
	if census.NestedMessages == 0 {
		t.Fatal("вложенных сообщений прочитано ноль — стенд не создал условия пробы")
	}
}
