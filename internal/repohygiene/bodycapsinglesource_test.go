// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// bodycapsinglesource_test.go — потолок тела запроса РЕАЛИЗОВАН ровно один раз
// (приёмка F2, сценарий F2-12).
//
// # Предмет
//
// Потолок состоит из двух слоёв: немедленный отказ на объявленную длину сверх
// потолка и ограничитель чтения для того, кто длину не объявил. Реализация,
// поставившая только второй слой, выглядит исполненной — она и правда
// ограничивает память, — но платит за это чтением всего тела: отказ приходит
// после того, как экономить уже нечего.
//
// Пока реализаций несколько, различие между ними не является ничьей находкой:
// оно НЕ ВЫРАЖЕНО и потому не может покраснеть. Замер, из-за которого
// пакет-владелец заведён, а этот гейт написан: реализаций в дереве было ТРИ, и
// объявленную длину проверяла ОДНА.
//
// # Почему находкой является реализация, а НЕ употребление
//
// Гейт, считающий употребления, потребовал бы от дерева одного вызывающего —
// то есть запретил бы ровно то, ради чего пакет заведён. Потребителей должно
// быть много; реализация обязана быть одна.
//
// # Что здесь считается деревом
//
// Индекс git — то же множество, которое увидит свежий клон и CI.
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const (
	// bodyCapImplementation — то, чем потолок РЕАЛИЗУЕТСЯ. Пара «путь импорта +
	// имя функции», а не имя пакета: псевдоним задаёт вызывающий.
	bodyCapImplementation = "net/http.MaxBytesReader"
	// bodyCapConsumer — то, чем потолок УПОТРЕБЛЯЕТСЯ. Таких мест сколько
	// угодно.
	bodyCapConsumer = "github.com/PRO-Robotech/kacho/pkg/httpbody.Cap"
	// bodyCapOwner — каталог единственного владельца реализации.
	bodyCapOwner = "pkg/httpbody/"
	// bodyCapCensusFloor — порог переписи: ниже него «ноль находок» означало бы
	// «ноль прочитанного».
	bodyCapCensusFloor = 1000
)

// TestBodyCapIsDeclaredExactlyOnce — сам гейт.
func TestBodyCapIsDeclaredExactlyOnce(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	var rels []string
	for rel := range tt.files {
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		if skipPath(rel) {
			continue
		}
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	var (
		parsed, calls, imports, dotImports int
		impls, consumers                   []BodyCapSite
	)
	for _, rel := range rels {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		i, c, census, err := ScanBodyCapCalls(rel, src, bodyCapImplementation, bodyCapConsumer)
		if err != nil {
			t.Fatalf("разбор %s: %v", rel, err)
		}
		parsed++
		calls += census.Calls
		imports += census.Imports
		dotImports += census.DotImports
		impls = append(impls, i...)
		consumers = append(consumers, c...)
	}

	t.Logf("перепись: не-тестовых файлов Go разобрано %d, объявлений импорта прочитано %d, "+
		"вызовов осмотрено %d, точечных импортов встречено %d, реализаций потолка "+
		"(%s) найдено %d, потребителей (%s) найдено %d",
		parsed, imports, calls, dotImports,
		bodyCapImplementation, len(impls), bodyCapConsumer, len(consumers))

	if parsed < bodyCapCensusFloor {
		t.Fatalf("перепись обвалилась: разобрано %d файлов при пороге %d — на таком "+
			"объёме «ноль находок» означало бы «ноль прочитанного»",
			parsed, bodyCapCensusFloor)
	}
	if calls == 0 {
		t.Fatalf("осмотрено ноль вызовов на %d файлах — разбор перестал видеть предмет, "+
			"и его молчание сказано ни о чём", parsed)
	}

	// (1) Предпосылка: реализация вообще ЕСТЬ. Ноль означает, что потолка в
	// дереве нет, и всякий обработчик читает тело без границы — молчать гейт
	// права не имеет.
	if len(impls) == 0 {
		t.Fatalf("реализаций потолка тела (%s) в дереве НОЛЬ — потолка не существует, "+
			"и тело запроса читается без границы. Гейт беспредметен: он молчал бы и "+
			"тогда, когда предмет исчез, и тогда, когда сломался он сам.",
			bodyCapImplementation)
	}

	// (2) Предпосылка: у единственной реализации есть ПОТРЕБИТЕЛИ. Реализация
	// без вызывающих — потолок, который никого не ограничивает; при этом гейт
	// остаётся зелёным, потому что реализация ровно одна.
	if len(consumers) == 0 {
		t.Fatalf("потребителей потолка (%s) в дереве НОЛЬ при %d реализации(ях) — "+
			"единственная реализация никем не зовётся, то есть потолок не применяется "+
			"нигде. Различение «реализация против потребителя» на таком дереве ничего "+
			"не различает.", bodyCapConsumer, len(impls))
	}

	// (3) Находка: реализация ВНЕ единственного владельца.
	var findings []string
	for _, s := range impls {
		if strings.HasPrefix(s.File, bodyCapOwner) {
			continue
		}
		findings = append(findings, fmt.Sprintf("%s:%d  %s", s.File, s.Line, s.Callee))
	}
	sort.Strings(findings)
	if len(findings) > 0 {
		t.Fatalf("потолок тела запроса реализован ВНЕ %s — %d место(а):\n  %s\n\n"+
			"Каждое такое место есть вторая реализация потолка. Различие между двумя "+
			"реализациями НЕ ВЫРАЖЕНО: каждая по отдельности выглядит исполненной, а "+
			"правка одной до другой не доезжает и не доезжает молча. Реализация, "+
			"поставившая только ограничитель чтения, пропускает запрос, объявивший "+
			"гигантскую длину: отказ приходит после того, как тело прочитано.\n"+
			"Снятие: звать %s, а не ставить ограничитель своей рукой.",
			bodyCapOwner, len(findings), strings.Join(findings, "\n  "), bodyCapConsumer)
	}

	for _, s := range impls {
		t.Logf("единственная реализация потолка: %s:%d", s.File, s.Line)
	}
	var where []string
	for _, s := range consumers {
		where = append(where, fmt.Sprintf("%s:%d", s.File, s.Line))
	}
	t.Logf("законные потребители (их сколько угодно): %s", strings.Join(where, ", "))
}
