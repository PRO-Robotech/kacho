// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// foundationledgerissue_test.go — вторая ось самоистечения ведомости пропусков:
// запись, чья задача ЗАКРЫТА.
//
// # Почему это отдельный гейт, а не часть переписи
//
// Перепись самоистекает по ДЕРЕВУ: пропуск, чью возможность усыновили, — находка.
// Но запись держится ещё и ЗАДАЧЕЙ, а задачу закрывают отдельно от кода. Закрыли
// задачу, не провязав возможность, — и запись извиняет пропуск дальше, вечно и
// молча. Дерево при этом не изменилось, поэтому перепись остаётся зелёной: у неё
// нет способа узнать про такое.
//
// Класс не гипотетический. Прежняя редакция набора несла две записи на задачи,
// закрытые за неделю до; нашлись они перемером руками, а не гейтом — то есть
// ровно тем способом, который правило и запрещает считать механизмом.
//
// # Почему решение отделено от измерения
//
// Состояние задачи — измерение СЕТЕВОЕ. Вердикт гейта не вправе быть функцией
// доступности GitHub, поэтому сверка идёт по явной ручке, а решение («закрытая
// задача под живой записью — находка») живёт отдельной чистой функцией, чью
// способность упасть доказывает инъекция БЕЗ сети.
package repohygiene

import (
	"encoding/json"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// foundationTrackerKnob — та же ручка, что у сверки ссылок сквозных проб: одна
// ручка на все сетевые измерения дерева, иначе их пришлось бы включать по одной
// и забывать по одной.
const foundationTrackerKnob = "KACHO_ISSUE_TRACKER_CHECK"

// foundationIssueMissing — ответ трекера, доказывающий, что номера НЕТ. Всё
// прочее (сеть, 5xx, отсутствие прав) — не отказ трекера, а несостоявшееся
// измерение: оно считается отдельно и печатается, а не проглатывается
// (`security.md` §Hardening-инварианты, п. 8).
const foundationIssueMissing = "Could not resolve to an issue or pull request"

// TestFoundationLedgerIssuesAreStillOpen — сверка ведомости с трекером.
//
// При выключенной ручке проба не «пропускается»: она печатает перепись и прямо
// говорит, что сверено НОЛЬ. Молчаливый пропуск сделал бы «сверено ноль»
// неотличимым от «сверено и всё в порядке» — то есть завёл бы ровно тот класс,
// против которого гейт и написан.
func TestFoundationLedgerIssuesAreStillOpen(t *testing.T) {
	r := foundationRoster()
	issues := r.LedgerIssues()
	if len(issues) == 0 {
		t.Fatalf("ведомость пропусков не называет ни одной задачи: либо пропусков не осталось " +
			"вовсе (тогда эту пробу снимают вместе с ведомостью), либо записи перестали нести " +
			"номер — и тогда красной обязана быть сама перепись")
	}

	if os.Getenv(foundationTrackerKnob) != "1" {
		t.Logf("сверка с трекером ВЫКЛЮЧЕНА (ручка %s=1): задач в ведомости %d (%v), сверено 0. "+
			"Решение про закрытую задачу проверено инъекцией без сети — "+
			"TestFoundationClosedIssueUnderALiveRecordIsAFinding",
			foundationTrackerKnob, len(issues), issues)
		return
	}

	states, checked, unresolved, cfgErr := resolveFoundationIssueStates(issues)
	if cfgErr != "" {
		t.Fatalf("%s", cfgErr)
	}
	t.Logf("сверка с трекером: задач %d, сверено %d, не удалось %d", len(issues), checked, unresolved)
	if unresolved > 0 {
		t.Errorf("состояние %d задач из %d выяснить не удалось: измерение объявлено включённым и "+
			"не выполнено. «Не сверено» не засчитывается в «сверено и в порядке»", unresolved, len(issues))
	}
	for _, b := range r.LedgerRecordsWhoseIssueIsClosed(states) {
		t.Errorf("%s", b)
	}
}

// resolveFoundationIssueStates спрашивает трекер про состояние каждой задачи.
func resolveFoundationIssueStates(issues []int) (map[int]string, int, int, string) {
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, 0, len(issues),
			"сверка запрошена ручкой " + foundationTrackerKnob + "=1, но `gh` в PATH нет — " +
				"это НАСТРОЙКА, а не сбой: измерение объявлено включённым и не выполняется"
	}
	states := map[int]string{}
	checked, unresolved := 0, 0
	for _, n := range issues {
		out, err := exec.Command("gh", "issue", "view", strconv.Itoa(n), "--json", "state").CombinedOutput()
		switch {
		case err == nil:
			var got struct {
				State string `json:"state"`
			}
			if json.Unmarshal(out, &got) != nil || got.State == "" {
				unresolved++
				continue
			}
			checked++
			states[n] = got.State
		case strings.Contains(string(out), foundationIssueMissing):
			// Номера нет вовсе — это НЕ «состояние неизвестно», а запись,
			// ссылающаяся в пустоту: она не может истечь никогда.
			checked++
			states[n] = "CLOSED"
		default:
			unresolved++
		}
	}
	return states, checked, unresolved, ""
}

// TestFoundationClosedIssueUnderALiveRecordIsAFinding — способность упасть,
// доказанная БЕЗ сети, в обе стороны.
func TestFoundationClosedIssueUnderALiveRecordIsAFinding(t *testing.T) {
	r := FoundationRoster{Ledger: []FoundationLedgerEntry{
		{Capability: "звено", Listener: "services/a", Issue: 11, Why: "работа впереди"},
		{Capability: "звено", Listener: "services/b", Issue: 22, Why: "работа впереди"},
	}}

	t.Run("все задачи открыты — молчит", func(t *testing.T) {
		bad := r.LedgerRecordsWhoseIssueIsClosed(map[int]string{11: "OPEN", 22: "OPEN"})
		if len(bad) != 0 {
			t.Fatalf("живая запись объявлена истёкшей: %v", bad)
		}
	})

	t.Run("одна задача закрыта — краснеет и называет запись", func(t *testing.T) {
		bad := r.LedgerRecordsWhoseIssueIsClosed(map[int]string{11: "OPEN", 22: "CLOSED"})
		if len(bad) != 1 {
			t.Fatalf("закрытая задача под живой записью дала %d находок, а ждали одну: %v", len(bad), bad)
		}
		if !strings.Contains(bad[0], "#22") || !strings.Contains(bad[0], "services/b") {
			t.Fatalf("находка не называет ни задачи, ни записи: %q", bad[0])
		}
		t.Logf("%s", bad[0])
	})

	t.Run("состояние неизвестно — НЕ находка", func(t *testing.T) {
		// Несостоявшееся измерение не превращается в вердикт: иначе временная
		// недоступность трекера роняла бы прогон и его научились бы обходить.
		// «Не сверено» считается отдельно — см. пробу выше.
		bad := r.LedgerRecordsWhoseIssueIsClosed(map[int]string{})
		if len(bad) != 0 {
			t.Fatalf("отсутствие ответа трекера засчитано закрытой задачей: %v", bad)
		}
	})
}
