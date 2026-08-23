// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// subscriptionRequestAllowances — ведомость послаблений: доменные запросы
// подписки, ещё не переведённые на общую форму.
//
// # ПЕРЕЧЕНЬ ПУСТ, и это нормальное состояние, а не недосмотр
//
// Приёмка фазы писалась, когда доменное объявление было одно —
// `kacho.cloud.compute.v1.WatchRequest`, — и предписывала завести под него
// запись. Ствол снял это объявление ДО начала фазы (kacho#813, имя лежит в
// надгробии `retiredRPCSurface`), поэтому запись под него была бы истёкшей в
// момент внесения: исключать ей нечего, и гейт назвал бы её находкой — ровно
// так, как и должен.
//
// Перемер на базе фазы: доменных объявлений запроса подписки в дереве НОЛЬ.
// Предикат, которым это повторяется:
//
//	git grep -hE '^\s*message ([A-Za-z0-9_]*SubscriptionRequest|WatchRequest|SubscribeRequest)\b' \
//	  origin/main -- proto | wc -l
//
// # Когда запись здесь появится
//
// Тогда и только тогда, когда домен заведёт свой запрос подписки раньше, чем
// возьмёт общую форму. Запись обязана назвать задачу перевода и предикат
// истечения; снятие объявления из дерева делает её находкой само.
var subscriptionRequestAllowances []SubscriptionRequestAllowance

func subscriptionSingularityOptions(t *testing.T) SubscriptionSingularityOptions {
	t.Helper()
	return SubscriptionSingularityOptions{
		Root:      repoRoot(t),
		ProtoRoot: "proto",
		Allow:     subscriptionRequestAllowances,
	}
}

// TestSubscriptionFormIsDeclaredOnce — WATCH-1-14…16 на НАСТОЯЩЕМ дереве.
//
// Гейт различает состояния, а не зеленеет на всех: до объявления общей формы он
// красен («формы нет»), после — зелен, а доменное объявление вне ведомости
// краснит его снова. Способность падать доказана не этим прогоном, а стендом
// (`subscriptionstand_test.go`): здесь только вердикт о дереве.
func TestSubscriptionFormIsDeclaredOnce(t *testing.T) {
	var log strings.Builder
	findings, census, err := AuditSubscriptionFormSingularity(subscriptionSingularityOptions(t), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	// Премиса: прочитано то, что заведомо есть.
	if census.ProtoFiles < 20 || census.TopLevelMessages < 100 {
		t.Fatalf("файлов контракта %d, сообщений верхнего уровня %d — обход пуст, вердикт беспредметен",
			census.ProtoFiles, census.TopLevelMessages)
	}

	if len(findings) == 0 {
		return
	}
	lines := make([]string, 0, len(findings))
	for _, f := range findings {
		lines = append(lines, "  "+f.String())
	}
	t.Errorf("объявлений запроса подписки %d при ожидаемых %d:\n%s",
		census.RequestDecls, census.Expected, strings.Join(lines, "\n"))
}
