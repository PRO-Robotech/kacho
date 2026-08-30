// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// loadbalancer_journal_vocabulary_test.go — СЛОВАРЬ НАГРУЗКИ ЖУРНАЛА РАВЕН
// СПИСКУ КОЛОНОК, КОТОРЫЙ ЧИТАЕТ РЕПОЗИТОРИЙ.
//
// # Предмет
//
// У вида `nlb_load_balancer` ДВА производителя строки журнала: семь точек на Go
// и ТРИГГЕР базы, пишущий `to_jsonb(строки)`. Форма на проводе принята одна —
// форма триггера, то есть ИМЕНА КОЛОНОК; сторона Go приведена к ней тегами
// `kacho.LoadBalancerJournalRow`.
//
// Расхождение тега с именем колонки НЕ ДАЁТ НИ ОТКАЗА, НИ ПУСТОТЫ: строка
// триггера разберётся, потеряв ровно это поле, и подписчик получит ПОЛНОЕ
// состояние без него — контракт формы разрешает читать непустое состояние как
// полное. Со стороны Go это невидимо by construction: строитель и читатель берут
// один и тот же тег, поэтому круг замыкается при ЛЮБОМ его значении.
//
// # Почему сверка ИМЕННО с `loadBalancerCols`, а не со списком, выписанным здесь
//
// Выписанный список был бы ТРЕТЬИМ местом об одном предмете и разошёлся бы с
// деревом молча. `loadBalancerCols` — тот самый список, которым репозиторий
// читает строку и из которого собирается запись: колонка, попавшая в запись, но
// не попавшая на провод, — ровно та ошибка, которую здесь ловят.
//
// # Чего эта проба НЕ утверждает — названо, чтобы её не приняли шире
//
// Она не утверждает, что имена колонок в `loadBalancerCols` совпадают с именами
// колонок в БАЗЕ: это утверждает сам путь чтения (запрос с неверным именем
// колонки отвергается первым же обращением, и на нём падают интеграционные пробы
// репозитория). И она ничего не говорит о живом теле триггера — его знает только
// база, поэтому согласие двух производителей держит сквозная проба
// `TestLoadBalancerStateIsTheSameFromTheTriggerAndFromGo` над настоящей схемой.

// journalVocabularyOf — ключи, которыми строитель кладёт состояние на провод.
//
// Читаются они ЧЕРЕЗ НАСТОЯЩИЙ JSON, а не через отражение по тегам: тег — то,
// что проверяется, и брать его же в качестве ожидания значило бы замкнуть круг.
func journalVocabularyOf(t *testing.T) []string {
	t.Helper()
	rec := &kacho.LoadBalancerRecord{CreatedAt: time.Now(), UpdatedAt: time.Now()}
	rec.ID = domain.ResourceID("nlb-1234567890abcdef")
	raw, err := json.Marshal(kacho.LoadBalancerStatePayload(rec))
	if err != nil {
		t.Fatalf("нагрузка не собралась: %v", err)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("нагрузка не разобралась: %v", err)
	}
	body, ok := envelope[kacho.PayloadKeyState]
	if !ok {
		t.Fatalf("в нагрузке нет конверта %q — полноту объявляет он, и без него строка "+
			"неотличима от прежней, минимальной формы", kacho.PayloadKeyState)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatalf("состояние под конвертом не разобралось: %v", err)
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// readColumns — колонки, которыми репозиторий читает строку балансировщика.
//
// `xmin::text` исключён НАЗВАННО: это снимок оптимистичной блокировки, а не
// состояние. `to_jsonb(строки)` системных колонок не отдаёт вовсе, поэтому у
// триггера его нет by construction, и класть его со стороны Go значило бы
// объявить полем то, чем следующий читатель попробует воспользоваться.
func readColumns() []string {
	var out []string
	for _, raw := range strings.Split(loadBalancerCols, ",") {
		col := strings.TrimSpace(raw)
		if col == "" || strings.Contains(col, "xmin") {
			continue
		}
		out = append(out, col)
	}
	sort.Strings(out)
	return out
}

// TestJournalPayloadSpeaksTheColumnVocabulary — провод и список чтения сходятся
// в ОБЕ стороны.
func TestJournalPayloadSpeaksTheColumnVocabulary(t *testing.T) {
	t.Parallel()

	onWire := journalVocabularyOf(t)
	inRead := readColumns()

	t.Logf("перепись: ключей на проводе %d · колонок чтения %d", len(onWire), len(inRead))
	if len(onWire) == 0 || len(inRead) == 0 {
		t.Fatal("одна из сторон пуста — сверять нечего, и проба беспредметна, а не пройдена")
	}

	wire := map[string]bool{}
	for _, k := range onWire {
		wire[k] = true
	}
	read := map[string]bool{}
	for _, c := range inRead {
		read[c] = true
	}

	for _, c := range inRead {
		if !wire[c] {
			t.Errorf("колонка %q читается репозиторием, но на провод НЕ попадает.\n"+
				"Строка триггера несёт её (`to_jsonb` берёт всю строку), а строка Go — нет: "+
				"два производителя одного вида скажут разное, и расхождение будет тихим — "+
				"подписчик получит ПОЛНОЕ состояние без этого поля", c)
		}
	}
	for _, k := range onWire {
		if !read[k] {
			t.Errorf("ключ %q уходит на провод, но такой колонки репозиторий НЕ читает.\n"+
				"Значит имя придумано на стороне Go: строка триггера этого ключа не пишет, "+
				"и поле будет теряться на каждом событии пересчёта статуса", k)
		}
	}
}
