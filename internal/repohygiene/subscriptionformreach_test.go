// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// subscriptionformreach_test.go — ГЕЙТ: у объявленной формы подписки есть, кому
// её взять, и послабление истекает ОТ ЗАКРЫТИЯ ПРЕДМЕТА.
//
// Механика анализатора и перечень того, что считается ссылкой, — в
// `subscriptionformreach.go`; здесь вердикт о дереве и сверка с трекером.
//
// # Почему решение отделено от измерения
//
// Состояние задачи-берущего — измерение СЕТЕВОЕ. Вердикт гейта не вправе быть
// функцией доступности GitHub, а `go test ./...` не вправе ходить в сеть. Сверка
// идёт по явной ручке; решение («ноль ссылок при закрытой задаче — находка»)
// живёт чистой функцией, чью способность упасть доказывает инъекция БЕЗ сети
// (`subscriptionformreach_injection_test.go`).
//
// Состояние измерения печатается ВСЕГДА: «сверено 0» не должно выглядеть как
// «сверено и в порядке».
package repohygiene

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// subscriptionReachTrackerKnob — та же ручка, что у прочих сетевых измерений
// дерева. Одна ручка на все: иначе их пришлось бы включать по одной и забывать
// по одной.
const subscriptionReachTrackerKnob = "KACHO_ISSUE_TRACKER_CHECK"

// subscriptionReachMissingMarker — ответ трекера, доказывающий, что номера НЕТ.
//
// Задача, которой в трекере не существует, закрыться не может НИКОГДА, то есть
// послабление под ней вечно, — поэтому такой ответ приравнивается к закрытой
// задаче, а не к неизвестному состоянию.
const subscriptionReachMissingMarker = "Could not resolve to an issue or pull request"

func subscriptionReachOptions(t *testing.T) SubscriptionReachOptions {
	t.Helper()
	return SubscriptionReachOptions{Root: repoRoot(t), ProtoRoot: "proto"}
}

// TestSubscriptionFormHasSomeoneToTakeIt — §7 п.1б приёмки WATCH-1, второй из
// двух механизмов DoD п.5.
//
// Первый — ведомость транспортных сообщений — судит по СУФФИКСУ ИМЕНИ и потому
// видит один тип из трёх. Этот судит по ПАКЕТУ и покрывает все три. Подмена
// одного другим названа приёмкой ошибкой: она сузила бы наблюдение втрое.
func TestSubscriptionFormHasSomeoneToTakeIt(t *testing.T) {
	var log strings.Builder
	types, census, err := AuditSubscriptionFormReach(subscriptionReachOptions(t), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	// Премиса: прочитано то, что заведомо есть. Каталог контракта мог переехать,
	// пакет — смениться; в обоих случаях зелёный вердикт был бы получен даром.
	if census.ProtoFiles < 20 {
		t.Fatalf("файлов контракта прочитано %d — обход пуст, вердикт беспредметен", census.ProtoFiles)
	}
	// Дискриминатор ключуется на пакете. Ноль типов означает, что он не нашёл
	// НИЧЕГО, — и тогда молчание гейта есть поломка разбора, а не чистота.
	if census.CommonTypes == 0 {
		t.Fatalf("типов пакета %s не найдено ни одного — разбор сломан, "+
			"и всякий тип был бы объявлен несуществующим, а не безссылочным",
			SubscriptionCommonPackage)
	}

	// Сверка с трекером — по явной просьбе. Состояние измерения называется всегда.
	state, tracker := resolveSubscriptionTakerState()
	for _, f := range SubscriptionReachFindings(types, state) {
		t.Errorf("%s", f)
	}
	t.Logf("задача-берущий #%d: %s; при состоянии %q находок %d",
		SubscriptionFormTaker, tracker, state, len(SubscriptionReachFindings(types, state)))
}

// resolveSubscriptionTakerState спрашивает трекер про состояние задачи-берущего.
// Возвращает состояние ("OPEN" | "CLOSED" | "") и человекочитаемую строку
// переписи.
func resolveSubscriptionTakerState() (string, string) {
	if os.Getenv(subscriptionReachTrackerKnob) != "1" {
		return "", "НЕ ЗАПРАШИВАЛАСЬ (" + subscriptionReachTrackerKnob + "=1), сверено 0. " +
			"Решение про закрытую задачу проверено инъекцией без сети — " +
			"TestSubscriptionReachDecisionCanFail"
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return "", "сверка запрошена ручкой " + subscriptionReachTrackerKnob +
			"=1, но `gh` в PATH нет — это НАСТРОЙКА, а не сбой: измерение объявлено " +
			"включённым и не выполняется"
	}
	out, err := exec.Command("gh", "issue", "view",
		strconv.Itoa(SubscriptionFormTaker), "--json", "state", "--jq", ".state").CombinedOutput()
	switch {
	case err == nil:
		state := strings.ToUpper(strings.TrimSpace(string(out)))
		if state != "OPEN" && state != "CLOSED" {
			return "", "трекер ответил неразборчиво (" + strconv.Quote(state) + "), сверено 0"
		}
		return state, "сверено, состояние " + state
	case strings.Contains(string(out), subscriptionReachMissingMarker):
		return "CLOSED", "задачи в трекере НЕТ — послабление под несуществующим номером " +
			"истечь не может никогда, поэтому считается закрытым"
	default:
		return "", "сверить не удалось: " + strings.TrimSpace(string(out)) +
			". «Не сверено» не засчитывается в «сверено и в порядке»"
	}
}
