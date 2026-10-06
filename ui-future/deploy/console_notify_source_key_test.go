// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// console_notify_source_key_test.go — КЛЮЧ МОДУЛЯ КАТАЛОГА `notify`, НАЗВАННЫЙ
// КОНСОЛЬЮ ДЛЯ ВЛАДЕЛЬЦА ЖУРНАЛА, ОБЯЗАН БЫТЬ КАТАЛОГОМ СЛУЖБЫ, ЧЕЙ ЖУРНАЛ
// ОБЪЯВЛЯЕТ ВИДЫ ЭТОГО ВЛАДЕЛЬЦА (NTF-6 Р10а, CX6-01).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Владелец журнала (`STREAM_SUBJECTS[spec].owner`) — домен контракта; ключ модуля
// каталога `notify` (`GET /notify/v1/catalog`, `modules[].key`) — имя источника
// уведомлений. Это РАЗНЫЕ словари: у балансировщика владелец `loadbalancer`, а
// модуль `nlb`; у службы доступа владелец `iam`, а модуль `kaname`. Переход
// объявлен одним местом — `NOTIFY_SOURCE_BY_OWNER` в `subjects.ts`, — и
// исчерпывающим его делает проверка типов. Но проверка типов судит только
// ПОЛНОТУ: запись `loadbalancer: "loadbalancer"` она примет, а действие подписки
// на карточках балансировщика молча исчезнет — модуль с таким ключом каталог не
// отдаёт, и поиск ячейки промахнётся без единой ошибки в журнале.
//
// ─────────────────────────────────────────────────────────────────────────────
// ОЖИДАЕМЫЙ КЛЮЧ ВЫВОДИТСЯ ИЗ ДЕРЕВА, А НЕ ВЫПИСЫВАЕТСЯ
//
// Для владельца, чей журнал в этом дереве: журнал какой службы
// `services/<каталог>` объявляет виды, названные консолью при этом владельце, —
// тот же вывод, что у гейта написания вида (`console_stream_kind_dictionary_test.go`,
// общий помощник `ownerJournalsOf`, не копия разбора). Ключ — `<каталог>`.
//
// Для владельца, чей журнал ведёт другой репозиторий: последний сегмент пути
// модуля из ведомости `journalsOutsideThisTree` того же гейта словаря
// (`iam` → `github.com/PRO-Robotech/kaname` → `kaname`). Второй ведомости здесь
// нет: она была бы вторым местом об одном предмете.
//
// Выписанная здесь таблица «владелец → ключ» была бы третьим местом об одном
// предмете и сверяла бы консоль с самой собой.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ГЕЙТ ПЕЧАТАЕТ И НА ЧЁМ ПАДАЕТ
//
// Печатает перепись («владельцев N, записей M») отдельно от находок: «ноль
// находок» обязан отличаться от «ноль прочитанного». Падает на пустом обходе
// (ни одной записи карты, ни одного журнала, ни одной записи перехода,
// объявления перехода нет вовсе) и на записи перехода, записанной формой,
// которой разбор не знает, — такая запись выпала бы из осмотренного молча.
//
// Способность падать и молчать доказывает соседний
// `console_notify_source_key_injection_test.go` — на настоящем тексте карты.
package deploy_test

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// notifySourceDecl — объявление перехода. Ищется по имени, а не по позиции.
var notifySourceDecl = regexp.MustCompile(`export\s+const\s+NOTIFY_SOURCE_BY_OWNER\b`)

// notifySourceEntry — одна запись перехода: владелец (голым именем либо строкой)
// и ключ модуля строкой. Сверяется ЦЕЛЫЙ сегмент между запятыми верхнего уровня,
// поэтому любая иная форма (ключ константой, расширение `...`, вычисляемое имя)
// не выпадает молча, а называется находкой «форма не распознана».
var notifySourceEntry = regexp.MustCompile(`^\s*(?:([A-Za-z_$][A-Za-z0-9_$]*)|"([^"\\]*)")\s*:\s*"([^"\\]*)"\s*$`)

// journalServicePattern — форма каталога журнала своего дерева, та же, что у
// образца обхода словаря (`journalKindsGlob`).
var journalServicePattern = regexp.MustCompile(`^services/([^/]+)/internal/subscriptionjournal$`)

// notifySourceEntryDecl — одна разобранная запись перехода.
type notifySourceEntryDecl struct {
	Owner string
	Key   string
}

// notifySourceBlockOf вырезает тело объекта перехода: от первой `{` после знака
// присваивания до парной ей `}`. Комментарии сняты заранее — рядом с
// объявлением стоит проза, называющая и владельцев, и ключи.
func notifySourceBlockOf(src string) (string, bool) {
	code := stripSubjectComments(src)
	loc := notifySourceDecl.FindStringIndex(code)
	if loc == nil {
		return "", false
	}
	// Аннотация типа `Readonly<Record<JournalOwner, string>>` стоит ДО знака
	// присваивания; скобка ищется после него.
	assign := -1
	for i := loc[1]; i < len(code); i++ {
		if code[i] == '=' && i+1 < len(code) && code[i+1] != '=' && code[i+1] != '>' {
			assign = i
			break
		}
	}
	if assign < 0 {
		return "", false
	}
	open := strings.IndexByte(code[assign:], '{')
	if open < 0 {
		return "", false
	}
	start := assign + open
	depth := 0
	for i := start; i < len(code); i++ {
		switch code[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return code[start+1 : i], true
			}
		}
	}
	return "", false
}

// notifySourceEntriesOf разбирает переход. Сегменты — по запятым ВЕРХНЕГО
// уровня тела; строки уважаются. Возвращает разобранные записи, сегменты,
// которых разбор не знает, и признак найденного объявления.
func notifySourceEntriesOf(src string) (entries []notifySourceEntryDecl, unknown []string, found bool) {
	block, ok := notifySourceBlockOf(src)
	if !ok {
		return nil, nil, false
	}
	var segments []string
	depth, last := 0, 0
	for i := 0; i < len(block); i++ {
		switch c := block[i]; c {
		case '"', '\'', '`':
			for i++; i < len(block) && block[i] != c; i++ {
				if block[i] == '\\' {
					i++
				}
			}
		case '{', '[', '(':
			depth++
		case '}', ']', ')':
			depth--
		case ',':
			if depth == 0 {
				segments = append(segments, block[last:i])
				last = i + 1
			}
		}
	}
	segments = append(segments, block[last:])
	for _, seg := range segments {
		if strings.TrimSpace(seg) == "" {
			continue
		}
		m := notifySourceEntry.FindStringSubmatch(seg)
		if m == nil {
			unknown = append(unknown, strings.Join(strings.Fields(seg), " "))
			continue
		}
		owner := m[1]
		if owner == "" {
			owner = m[2]
		}
		entries = append(entries, notifySourceEntryDecl{Owner: owner, Key: m[3]})
	}
	return entries, unknown, true
}

// notifySourceVerdict — находки по владельцам плюс перепись.
type notifySourceVerdict struct {
	// Owners — владельцев, названных картой предметов.
	Owners int
	// Findings — по одной на нарушение; каждая называет владельца.
	Findings []string
}

// expectedNotifySourceKey — ключ модуля каталога для владельца, выведенный из
// дерева: каталог службы, чей журнал объявляет его виды, либо последний сегмент
// пути модуля из ведомости внешних журналов.
func expectedNotifySourceKey(owner string, ownerJournals map[string]map[string]bool,
	external map[string]string) (key, basis string, err error) {
	if mp, outside := external[owner]; outside {
		return path.Base(mp), "ведомость journalsOutsideThisTree → " + mp, nil
	}
	journals := sortedSetKeys(ownerJournals[owner])
	if len(journals) != 1 {
		return "", "", fmt.Errorf("виды владельца %q объявляют журналов %d (%s) — "+
			"ключ модуля каталога из дерева не выводится: ожидается ровно один журнал "+
			"службы, чей каталог и есть ключ", owner, len(journals), strings.Join(journals, ", "))
	}
	m := journalServicePattern.FindStringSubmatch(journals[0])
	if m == nil {
		return "", "", fmt.Errorf("журнал владельца %q лежит вне формы "+
			"services/<каталог>/internal/subscriptionjournal (%s) — каталог службы "+
			"не выводится", owner, journals[0])
	}
	return m[1], "журнал " + journals[0], nil
}

// judgeNotifySourceKeys сверяет переход с деревом. Вход подаётся явно — инъекция
// зовёт ТУ ЖЕ функцию на подменённом тексте, не трогая дерева.
func judgeNotifySourceKeys(subjects []consoleStreamSubject, dict map[string][]string,
	external map[string]string, entries []notifySourceEntryDecl) notifySourceVerdict {
	ownerJournals, _ := ownerJournalsOf(subjects, dict)

	owners := map[string]bool{}
	for _, s := range subjects {
		owners[s.Owner] = true
	}
	verdict := notifySourceVerdict{Owners: len(owners)}

	declared := map[string]string{}
	for _, e := range entries {
		if prev, dup := declared[e.Owner]; dup {
			verdict.Findings = append(verdict.Findings, fmt.Sprintf(
				"владелец %q назван в NOTIFY_SOURCE_BY_OWNER дважды (%q и %q) — действует "+
					"последняя запись, первая молча не значит ничего", e.Owner, prev, e.Key))
		}
		declared[e.Owner] = e.Key
	}

	for _, owner := range sortedSetKeys(owners) {
		want, basis, err := expectedNotifySourceKey(owner, ownerJournals, external)
		if err != nil {
			verdict.Findings = append(verdict.Findings, err.Error())
			continue
		}
		got, ok := declared[owner]
		if !ok {
			verdict.Findings = append(verdict.Findings, fmt.Sprintf(
				"владелец %q назван картой предметов, а в NOTIFY_SOURCE_BY_OWNER записи нет — "+
					"`notifyModuleOf` вернёт на его спеках undefined, и действие подписки на "+
					"их карточках молча исчезнет; ожидается %q (%s)", owner, want, basis))
			continue
		}
		if got != want {
			verdict.Findings = append(verdict.Findings, fmt.Sprintf(
				"владелец %q: NOTIFY_SOURCE_BY_OWNER называет ключ модуля каталога %q, а "+
					"дерево выводит %q (%s) — модуль с таким ключом каталог не отдаёт, и "+
					"действие подписки на карточках этого владельца молча исчезнет. Ключ "+
					"модуля не выводится из имени владельца, сегмента пути или приставки вида",
				owner, got, want, basis))
		}
	}

	extra := make([]string, 0)
	for owner := range declared {
		if !owners[owner] {
			extra = append(extra, owner)
		}
	}
	sort.Strings(extra)
	for _, owner := range extra {
		verdict.Findings = append(verdict.Findings, fmt.Sprintf(
			"NOTIFY_SOURCE_BY_OWNER называет владельца %q, которого карта предметов НЕ "+
				"НАЗЫВАЕТ ни одной записью — читателя у записи нет, и она не сверена ни с "+
				"чем", owner))
	}
	return verdict
}

// TestConsoleNotifySourceKeyIsTheServiceWhoseJournalDeclaresTheOwner — переход
// «владелец → ключ модуля каталога» сходится с деревом.
func TestConsoleNotifySourceKeyIsTheServiceWhoseJournalDeclaresTheOwner(t *testing.T) {
	root := repoRootFromTest(t)

	raw, err := os.ReadFile(filepath.Join(root, consoleSubjectsRel)) // #nosec G304 -- корень этого дерева
	if err != nil {
		t.Fatalf("карта предметов консоли %s не читается (%v) — сверять нечего", consoleSubjectsRel, err)
	}
	src := string(raw)

	subjects := consoleStreamSubjectsOf(src)
	parsed, declaredOwnerFields := consoleSubjectCounts(src)
	entries, unknown, found := notifySourceEntriesOf(src)
	dict, _ := journalDictionaries(t, root)

	verdict := judgeNotifySourceKeys(subjects, dict, journalsOutsideThisTree, entries)
	t.Logf("осмотрено: владельцев %d, записей %d (карта предметов %s); записей "+
		"NOTIFY_SOURCE_BY_OWNER %d; журналов дерева и пина %d",
		verdict.Owners, len(subjects), consoleSubjectsRel, len(entries), len(dict))

	// Премисы: ноль прочитанного с любой стороны делает молчание неотличимым
	// от «нарушений нет».
	if !found {
		t.Fatalf("%s не объявляет NOTIFY_SOURCE_BY_OWNER — перехода «владелец → ключ "+
			"модуля каталога notify» нет, и действию подписки искать модуль нечем "+
			"(NTF-6 Р10а)", consoleSubjectsRel)
	}
	if len(subjects) == 0 {
		t.Fatalf("%s не называет ни одной записи STREAM_SUBJECTS — владельцев 0, "+
			"вердикт беспредметен", consoleSubjectsRel)
	}
	if parsed != declaredOwnerFields {
		t.Fatalf("%s объявляет владельца %d раз, а разобрано записей %d — часть карты "+
			"записана формой, которой разбор не знает", consoleSubjectsRel,
			declaredOwnerFields, parsed)
	}
	if len(dict) == 0 {
		t.Fatalf("журналов найдено 0 по образцу %s — ключ модуля выводить не из чего",
			filepath.ToSlash(journalKindsGlob))
	}
	if len(entries) == 0 && len(unknown) == 0 {
		t.Fatalf("NOTIFY_SOURCE_BY_OWNER в %s объявлен, но пуст — сверять нечего",
			consoleSubjectsRel)
	}
	for _, seg := range unknown {
		t.Errorf("%s: запись NOTIFY_SOURCE_BY_OWNER записана формой, которой разбор не "+
			"знает (%s) — её ключ не судит никто; почини распознаватель либо запиши "+
			"владельца строкой ключа", consoleSubjectsRel, seg)
	}
	for _, text := range verdict.Findings {
		t.Errorf("%s: %s", consoleSubjectsRel, text)
	}
}
