// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionjournal

import (
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"testing"

	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
	"github.com/PRO-Robotech/kacho/services/registry/internal/migrations"
)

// Производитель слов журнала у реестра — МИГРАЦИЯ, а не код Go: строку пишет
// триггер базы, и словарь родов события закрыт ограничением той же таблицы.
// Поэтому перепись читает SQL, а не синтаксическое дерево Go.
//
// Файл миграции ищется ПО ПРЕДМЕТУ (упоминание имени таблицы), а не по имени:
// имя несёт метку времени заведения, и выписанное сюда оно устарело бы при
// первой же следующей миграции журнала.
var (
	// checkWordsRe — перечень слов, разрешённых ограничением базы.
	checkWordsRe = regexp.MustCompile(`(?s)registry_resource_journal_event_type_check\s*\n?\s*CHECK\s*\(\s*event_type\s*=\s*ANY\s*\(\s*ARRAY\[(.*?)\]\s*\)\s*\)`)
	// sqlWordRe — строковый литерал SQL.
	sqlWordRe = regexp.MustCompile(`'([A-Za-z_.]+)'`)
	// emitWordRe — слово, которое ПИШЕТ триггер (присваивание рода события).
	emitWordRe = regexp.MustCompile(`v_event\s*:=\s*'([A-Z]+)'`)
	// emitKindRe — слово вида, которым триггер заполняет колонку `resource_kind`.
	emitKindRe = regexp.MustCompile(`VALUES\s*\(\s*'([A-Za-z_]+)'`)
)

// journalMigration возвращает текст миграции, заводящей ресурсный журнал.
//
// Отказ, а не пустая строка: разбор, судящий пустоту, отвечает «расхождений нет»
// даром.
func journalMigration(t *testing.T) string {
	t.Helper()
	names, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatalf("состав миграций не прочитался: %v", err)
	}
	if len(names) == 0 {
		t.Fatal("миграций ноль — встроенная файловая система пуста, и перепись беспредметна")
	}
	var found []string
	var body string
	for _, name := range names {
		b, rerr := fs.ReadFile(migrations.FS, name)
		if rerr != nil {
			t.Fatalf("миграция %s не прочиталась: %v", name, rerr)
		}
		if strings.Contains(string(b), "CREATE TABLE kacho_registry.registry_resource_journal") {
			found = append(found, name)
			body = string(b)
		}
	}
	if len(found) != 1 {
		t.Fatalf("миграций, заводящих ресурсный журнал, найдено %d %v среди %d осмотренных, ожидалась ровно одна: "+
			"ноль означает, что разбор сломан либо таблица переименована; больше одной — что журнал заводится дважды",
			len(found), found, len(names))
	}
	t.Logf("осмотрено миграций %d; журнал заводит %s", len(names), found[0])
	return body
}

// TestChangeDictionaryIsDerivedFromTheMigration — словарь родов изменения
// сверяется с ПРОИЗВОДИТЕЛЕМ, а не со вторым рукописным перечнем.
//
// # Почему перепись, а не список
//
// Производителей у слова два, и они обязаны сходиться: ограничение базы решает,
// какое слово вообще может лечь в таблицу, триггер решает, какое ложится
// сегодня. Проба, выписывающая слова третий раз, закрепила бы ОТВЕТ словаря, а
// не его согласие с деревом: слово, заменённое в триггере на необъявленное,
// такой пробой не ловится ничем — строка просто перестаёт доставляться, тихо.
//
// # Что именно утверждается — ТРИ стороны
//
//	каждое слово ОГРАНИЧЕНИЯ названо словарём  — иначе строка, законно лежащая в
//	                                             таблице, недоставляема;
//	каждое слово словаря разрешено ОГРАНИЧЕНИЕМ — иначе запись пережила свой
//	                                             предмет: такой строки не бывает;
//	каждое слово ТРИГГЕРА разрешено ограничением — иначе вставка отказывает, и
//	                                             мутация ресурса падает целиком.
//
// Пустой обход — отказ: ноль найденных слов означает, что разбор сломан, и
// «расхождений нет» получено даром.
func TestChangeDictionaryIsDerivedFromTheMigration(t *testing.T) {
	body := journalMigration(t)

	m := checkWordsRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatal("ограничение словаря родов события не найдено в миграции: разбор сломан, " +
			"и согласие объявления с базой не установлено ни в одну сторону")
	}
	allowed := map[string]bool{}
	for _, w := range sqlWordRe.FindAllStringSubmatch(m[1], -1) {
		allowed[w[1]] = true
	}
	if len(allowed) == 0 {
		t.Fatalf("ограничение найдено, а слов в нём ноль (%q): разбор перечня сломан", m[1])
	}

	emitted := map[string]bool{}
	for _, w := range emitWordRe.FindAllStringSubmatch(body, -1) {
		emitted[w[1]] = true
	}
	if len(emitted) == 0 {
		t.Fatal("триггер не пишет ни одного рода события: разбор сломан либо эмиссии нет вовсе")
	}

	declared := Journal(probeEndpointBase).Mapping.Changes

	for word := range allowed {
		if declared[word] == subscriptionv1.SubscriptionEvent_CHANGE_UNSPECIFIED {
			t.Errorf("ограничение базы разрешает род %q, а словарь его НЕ называет: строка с ним "+
				"недоставляема, и потеря эта тихая — ни отказа, ни пропуска в нумерации", word)
		}
	}
	for word := range declared {
		if !allowed[word] {
			t.Errorf("словарь называет род %q, которого ограничение базы НЕ разрешает: "+
				"такой строки в журнале не бывает, и запись пережила свой предмет", word)
		}
	}
	for word := range emitted {
		if !allowed[word] {
			t.Errorf("триггер пишет род %q, которого ограничение базы НЕ разрешает: "+
				"вставка отказала бы, и мутация реестра упала бы целиком", word)
		}
	}

	words := make([]string, 0, len(allowed))
	for w := range allowed {
		words = append(words, w)
	}
	sort.Strings(words)
	t.Logf("родов разрешено ограничением %d %v; пишет триггер %d; объявлено словарём %d",
		len(allowed), words, len(emitted), len(declared))
}

// TestJournalWordIsDerivedFromTheTrigger — ключ словаря видов сверяется с тем,
// что триггер РЕАЛЬНО кладёт в колонку `resource_kind`.
//
// Слово выписано в двух местах — литералом в триггере и константой здесь, — и
// расхождение между ними ТИХОЕ: строка с неназванным словом перестаёт
// доставляться без отказа и без пропуска в нумерации.
func TestJournalWordIsDerivedFromTheTrigger(t *testing.T) {
	body := journalMigration(t)

	produced := map[string]int{}
	for _, w := range emitKindRe.FindAllStringSubmatch(body, -1) {
		produced[w[1]]++
	}
	if len(produced) == 0 {
		t.Fatal("в миграции не найдено ни одной вставки со словом вида: разбор сломан, " +
			"и «расхождений нет» получено даром")
	}

	declared := Journal(probeEndpointBase).Mapping.Kinds
	for word := range produced {
		if _, ok := declared[word]; !ok {
			t.Errorf("триггер пишет вид %q, а словарь его НЕ называет: строка недоставляема, "+
				"и вопрос о её видимости задать нечем", word)
		}
	}
	for word := range declared {
		if produced[word] == 0 {
			t.Errorf("словарь называет вид %q, которого не пишет НИ ОДИН триггер: "+
				"запись пережила свой предмет и читается как способность журнала", word)
		}
	}
	t.Logf("видов пишет триггер %d %v; объявлено словарём %d", len(produced), produced, len(declared))
}
