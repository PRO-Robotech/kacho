// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// subscriptionchannelreader_injection_test.go — доказательство того, что
// TestChannelIsReadByTheStreamServerAlone СПОСОБЕН упасть, и падает он на
// существе, а не на слове.
//
// Инъекция гоняет ТУ ЖЕ функцию разбора (ScanChannelReads), что и гейт.
//
// Пара обязательна в обе стороны, и вторая сторона здесь ТЯЖЕЛЕЕ первой. Гейт
// заведён ВЗАМЕН предиката, который судил подстроку и потому находил 17 файлов
// при одном операторе. Инъекция, доказывающая только «умеет краснеть», оставила
// бы ровно ту болезнь, от которой гейт написан: законные близнецы собраны из
// НАСТОЯЩИХ форм дерева — ресурс `Listener` домена nlb, ручка
// `INTERNAL_LISTENER`, проза «`LISTEN` требует своей сессии», обёртка ошибки
// `fmt.Errorf("LISTEN: %w")` и сетевое `log.Fatalf("listen %s")`.
//
// Последние две — не выдумка для полноты: первая редакция разбора судила литерал
// целиком и обе принимала за оператор.
package repohygiene

import (
	"strings"
	"testing"
)

// channelReadInjectedOperators — чтения канала, заведённые владельцем домена.
//
// Четыре написания, и каждое обходит более наивный разбор:
//
//   - конкатенация с именем канала — каноничная форма дерева;
//   - имя каналом внутри литерала — литерал целиком, без конкатенации;
//   - НИЖНИЙ регистр — SQL его не различает, и `listen` исполнится наравне;
//   - оператор, вынесенный в КОНСТАНТУ, — литерал вне аргумента вызова, и
//     разбор, сужённый до вызова `Exec`, его не увидел бы.
const channelReadInjectedOperators = `package journal

const hoisted = "LISTEN " + channelName

func wake(conn *pgx.Conn) {
	_, _ = conn.Exec(ctx, "LISTEN "+channelName)
	_, _ = conn.Exec(ctx, "LISTEN compute_journal_changed")
	_, _ = conn.Exec(ctx, "listen "+channelName)
	_, _ = conn.Exec(ctx, hoisted)
}
`

// channelReadInjectedLegitimateTwins — тот же файл БЕЗ единого оператора, со
// всеми формами, на которых спотыкался прежний предикат.
const channelReadInjectedLegitimateTwins = `package journal

// Отдельное соединение вне пула: ` + "`LISTEN`" + ` требует своей сессии, а
// пул её переиспользует — пробуждения приходили бы в чужую.
//
// Пробуждение читает НЕ этот слой: LISTEN/NOTIFY живёт у сервера потока.

// Listener — РЕСУРС домена: имя типа, а не оператор.
type Listener struct {
	Name string
}

const (
	knobInternal = "INTERNAL_LISTENER"
	permission   = "nlb.listeners.update"
	caseName     = "T31-LBLREVOKE-NLB-LISTENER-04"
	longestWord  = "LISTENER"
)

func wake(conn *pgx.Conn) error {
	if _, err := conn.Exec(ctx, "UNLISTEN "+channelName); err != nil {
		return fmt.Errorf("LISTEN: %w", err)
	}
	log.Fatalf("listen %s: %v", addr, err)
	return nil
}
`

// TestChannelReadScanFindsOperators — первая сторона: оператор находится, и
// находка называет КООРДИНАТУ.
func TestChannelReadScanFindsOperators(t *testing.T) {
	sites, census, err := ScanChannelReads(
		"services/compute/internal/repo/kacho/pg/journal_notify.go",
		[]byte(channelReadInjectedOperators))
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if census.Literals == 0 {
		t.Fatal("осмотрено ноль литералов — разбор не читал синтетику вовсе")
	}

	const want = 4
	if len(sites) != want {
		var got []string
		for _, s := range sites {
			got = append(got, s.Operator)
		}
		t.Fatalf("найдено %d чтений канала, ожидалось %d — %v.\n"+
			"Каждое написание обходит более наивный разбор: конкатенация, имя внутри "+
			"литерала, нижний регистр, вынос в константу.", len(sites), want, got)
	}
	for _, s := range sites {
		if s.Line == 0 {
			t.Errorf("находка %q без строки — гейт, не называющий координаты, "+
				"посылает читателя искать руками", s.Operator)
		}
		if !strings.EqualFold(s.Operator[:6], "LISTEN") {
			t.Errorf("находка называет %q — она обязана показывать то, что нашла", s.Operator)
		}
	}
}

// TestChannelReadScanIsSilentOnLegitimateTwins — вторая сторона: ни одна
// законная форма находкой не становится.
//
// Без этой половины гейт ловил бы форму, а не существо, — и первый же ложный
// срабат его отключил бы. Ровно это и случилось с предикатом-предшественником:
// он отвечал «нет» независимо от состояния дерева, и оценивать по нему DoD было
// нельзя.
func TestChannelReadScanIsSilentOnLegitimateTwins(t *testing.T) {
	sites, census, err := ScanChannelReads(
		"services/nlb/internal/apps/kacho/api/listener/update.go",
		[]byte(channelReadInjectedLegitimateTwins))
	if err != nil {
		t.Fatalf("разбор синтетики: %v", err)
	}
	if census.Literals == 0 {
		t.Fatal("осмотрено ноль литералов — молчание сказано ни о чём: " +
			"разбор не читал синтетику вовсе")
	}
	if len(sites) != 0 {
		var got []string
		for _, s := range sites {
			got = append(got, s.File+": "+s.Operator)
		}
		t.Fatalf("законные формы приняты за оператор — %d: %v.\n"+
			"Ресурс `Listener`, ручка `INTERNAL_LISTENER`, проза о самом решении, "+
			"обёртка ошибки `LISTEN: %%w`, сетевое `listen %%s` и отписка `UNLISTEN` "+
			"операторами чтения канала не являются. Гейт, краснеющий на них, "+
			"повторяет предикат, взамен которого написан.", len(sites), got)
	}
}

// TestChannelReadGrammarSeparatesOperatorFromProse — граница, на которой первая
// редакция разбора ошиблась, закреплена отдельно.
//
// Проверяется ГРАММАТИКА: за глаголом обязано стоять начало имени канала.
// Таблица держит обе стороны рядом, чтобы правка правила не могла сдвинуть одну
// из них молча.
func TestChannelReadGrammarSeparatesOperatorFromProse(t *testing.T) {
	for _, tc := range []struct {
		literal  string
		operator bool
		why      string
	}{
		{"LISTEN ", true, "каноничная форма: имя приклеивается конкатенацией"},
		{"LISTEN chan_name", true, "имя внутри литерала"},
		{"listen chan_name", true, "SQL не различает регистр"},
		{`LISTEN "Mixed Case"`, true, "составное имя в кавычках"},
		{"LISTEN", true, "голый глагол: имя приклеивается следующим куском"},
		{"LISTEN: %w", false, "обёртка ошибки: за глаголом двоеточие"},
		{"listen %s: %v", false, "сетевой слушатель: за глаголом подстановка"},
		{"LISTENER", false, "имя ресурса: за глаголом буква"},
		{"INTERNAL_LISTENER", false, "имя ручки: глагол не в начале"},
		{"UNLISTEN ch", false, "отписка — не чтение канала"},
	} {
		if got := isChannelReadOperator(tc.literal); got != tc.operator {
			t.Errorf("%q признан оператором=%v, ожидалось %v — %s",
				tc.literal, got, tc.operator, tc.why)
		}
	}
}
