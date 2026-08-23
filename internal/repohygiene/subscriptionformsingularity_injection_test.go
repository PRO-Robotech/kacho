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
