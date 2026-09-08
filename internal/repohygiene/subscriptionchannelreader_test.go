// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// subscriptionchannelreader_test.go — чтение канала заведено ОДИН раз, и не у
// владельца (задача #1416, DoD эпика #1016 п.4).
//
// # Предмет
//
// Поток изменений устроен так, что канал Postgres читает СЕРВЕР ПОТОКА в
// `pkg/subscription`, а провязанный владелец (compute, nlb) не читает его вовсе:
// он отдаёт свой журнал общим глаголом и о канале не знает. Владелец, заведший
// своё чтение, получает второе соединение вне пула на каждую подписку и вторую
// раскладку пробуждений — и обе расходятся с общей молча, потому что каждая по
// отдельности работает.
//
// # Почему гейт, а не строка в описании эпика
//
// Строка в DoD судила подстроку и потому отвечала «нет» при любом дереве (разбор
// — в шапке subscriptionchannelreader.go). Предикат, который нельзя пройти, не
// проверяет ничего: его перестают читать, а вместе с ним перестают замечать
// настоящую находку.
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
	// channelReadOwner — каталог единственного законного чтения канала для
	// подписки.
	channelReadOwner = "pkg/subscription/"
	// channelReadServices — дерево владельцев доменов: здесь чтения канала для
	// подписки быть не должно.
	channelReadServices = "services/"
	// channelReadCensusFloor — порог переписи: ниже него «ноль находок»
	// означало бы «ноль прочитанного».
	channelReadCensusFloor = 1000
)

// channelReadExemptions — чтения канала В СЕРВИСАХ, законные по решению.
//
// Ведомость ИСТЕКАЕТ САМА: запись, которой больше нечего исключать, — находка.
// Без этого свойства снятое чтение оставило бы за собой прощение, и следующее,
// заведённое в том же файле, уехало бы под него незамеченным.
var channelReadExemptions = map[string]string{
	"services/iam/internal/repo/kaname/pg/reconcile_notify.go": "реконсиляция прав: " +
		"ДРУГАЯ подсистема, заведена до эпика подписки и к потоку изменений отношения " +
		"не имеет. Пробуждает свой обходчик прав, а не отдаёт журнал подписчику.",
}

// TestChannelIsReadByTheStreamServerAlone — сам гейт.
func TestChannelIsReadByTheStreamServerAlone(t *testing.T) {
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
		parsed, literals int
		sites            []ChannelReadSite
	)
	for _, rel := range rels {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		found, census, err := ScanChannelReads(rel, src)
		if err != nil {
			t.Fatalf("разбор %s: %v", rel, err)
		}
		parsed++
		literals += census.Literals
		sites = append(sites, found...)
	}

	var owner, inServices, elsewhere []ChannelReadSite
	for _, s := range sites {
		switch {
		case strings.HasPrefix(s.File, channelReadOwner):
			owner = append(owner, s)
		case strings.HasPrefix(s.File, channelReadServices):
			inServices = append(inServices, s)
		default:
			elsewhere = append(elsewhere, s)
		}
	}

	t.Logf("перепись: не-тестовых файлов Go разобрано %d, строковых литералов осмотрено %d, "+
		"чтений канала найдено %d (у сервера потока %s — %d, в %s — %d, в прочем общем "+
		"фундаменте — %d)",
		parsed, literals, len(sites),
		channelReadOwner, len(owner), channelReadServices, len(inServices), len(elsewhere))

	if parsed < channelReadCensusFloor {
		t.Fatalf("перепись обвалилась: разобрано %d файлов при пороге %d — на таком "+
			"объёме «ноль находок» означало бы «ноль прочитанного»",
			parsed, channelReadCensusFloor)
	}
	if literals == 0 {
		t.Fatalf("осмотрено ноль строковых литералов на %d файлах — разбор перестал "+
			"видеть предмет, и его молчание сказано ни о чём", parsed)
	}

	// (1) Предпосылка: сервер потока канал ЧИТАЕТ. Ноль означает, что предмета
	// у гейта нет вовсе — и молчал бы он тогда и при исчезнувшем предмете, и
	// при сломанном разборе.
	if len(owner) == 0 {
		t.Fatalf("чтений канала у сервера потока (%s) НОЛЬ — либо сервер перестал читать "+
			"канал, либо разбор перестал его видеть. Гейт беспредметен: отличить эти два "+
			"случая по его молчанию нельзя.", channelReadOwner)
	}
	if len(owner) != 1 {
		var where []string
		for _, s := range owner {
			where = append(where, fmt.Sprintf("%s:%d", s.File, s.Line))
		}
		sort.Strings(where)
		t.Errorf("сервер потока читает канал в %d местах (%s) — экземпляр обязан быть один, "+
			"иначе раскладка пробуждений расходится сама с собой",
			len(owner), strings.Join(where, ", "))
	}

	// (2) Находка: чтение канала у ВЛАДЕЛЬЦА домена вне ведомости.
	var findings []string
	seen := make(map[string]bool, len(channelReadExemptions))
	for _, s := range inServices {
		if _, ok := channelReadExemptions[s.File]; ok {
			seen[s.File] = true
			continue
		}
		findings = append(findings, fmt.Sprintf("%s:%d  %q", s.File, s.Line, s.Operator))
	}
	sort.Strings(findings)
	if len(findings) > 0 {
		t.Errorf("владелец домена читает канал сам — %d место(а):\n  %s\n\n"+
			"Поток изменений отдаётся общим глаголом: канал читает сервер потока в %s, а "+
			"владелец о канале не знает. Своё чтение даёт второе соединение вне пула на "+
			"каждую подписку и вторую раскладку пробуждений — обе расходятся с общей молча, "+
			"потому что каждая по отдельности работает.\n"+
			"Снятие: отдать журнал через pkg/subscription либо, если предмет иной подсистемы, "+
			"внести файл в channelReadExemptions с причиной.",
			len(findings), strings.Join(findings, "\n  "), channelReadOwner)
	}

	// (3) Ведомость истекает сама: прощать больше нечего — снимайте запись.
	var stale []string
	for file := range channelReadExemptions {
		if !seen[file] {
			stale = append(stale, file)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("исключение потеряло предмет — %d запись(и):\n  %s\n\n"+
			"Чтения канала в этом файле больше нет, а прощение осталось. Следующее чтение, "+
			"заведённое здесь, уехало бы под него незамеченным.\n"+
			"Снятие: убрать запись из channelReadExemptions.",
			len(stale), strings.Join(stale, "\n  "))
	}

	for _, s := range owner {
		t.Logf("сервер потока читает канал: %s:%d  %q", s.File, s.Line, s.Operator)
	}
	for _, s := range elsewhere {
		t.Logf("чтение канала в общем фундаменте (не предмет этого гейта): %s:%d", s.File, s.Line)
	}
}
