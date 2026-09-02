// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func clientDocsContractDriftOptions(t *testing.T) ClientDocsContractDriftOptions {
	t.Helper()
	return ClientDocsContractDriftOptions{
		Root:      repoRoot(t),
		ProtoRoot: "proto",
		// Балансировщик — единственный домен, чей каталог сайта и каталог
		// контракта расходятся: `services/nlb/docs` против
		// `proto/kacho/cloud/loadbalancer`. Псевдоним объявлен, а не выведен:
		// выводить его не из чего, расхождение историческое.
		DomainAliases: map[string]string{"nlb": "loadbalancer"},
	}
}

// TestClientDocsExamplesDoNotShowRetiredFields — вердикт о НАСТОЯЩЕМ дереве.
//
// Способность падать доказывает не этот прогон, а инъекция
// (`clientdocscontractdrift_injection_test.go`): здесь только вердикт.
func TestClientDocsExamplesDoNotShowRetiredFields(t *testing.T) {
	var log strings.Builder
	findings, census, err := AuditClientDocsRetiredFieldInExample(clientDocsContractDriftOptions(t), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	// Премиса: прочитано то, что заведомо есть. Без неё «ноль находок»
	// достигалось бы пустым обходом.
	if census.ProtoFiles < 50 {
		t.Fatalf("файлов контракта %d — дерево контрактов не прочитано, забранные имена выводить не из чего",
			census.ProtoFiles)
	}
	if census.RetiredNames < 10 {
		t.Fatalf("забранных имён %d — множество, о котором выносится вердикт, пусто", census.RetiredNames)
	}
	if census.Sites < 5 {
		t.Fatalf("сайтов документации %d — обход пуст, вердикт беспредметен", census.Sites)
	}
	if census.Pages < 50 {
		t.Fatalf("страниц %d — обход пуст, вердикт беспредметен", census.Pages)
	}
	// Вторая половина: вердикт выносится только о КЛЮЧАХ примеров. Ноль примеров
	// либо ноль рассуженных ключей означал бы, что он не вынесен ни разу.
	if census.Examples == 0 || census.KeysJudged == 0 {
		t.Fatalf("примеров JSON %d, ключей рассужено %d — сверка не состоялась",
			census.Examples, census.KeysJudged)
	}

	if len(findings) == 0 {
		return
	}
	lines := make([]string, 0, len(findings))
	for _, f := range findings {
		lines = append(lines, "  "+f.String())
	}
	t.Errorf("пример на клиентской странице показывает поле, снятое с контракта "+
		"(примеров %d, ключей рассужено %d):\n%s\n\n"+
		"Резерв ИМЕНИ в контракте означает, что живым полем оно не станет никогда. "+
		"Пример читается как образец — по нему пишут разбор ответа и тело запроса, — "+
		"поэтому такой пример обещает поле, которого не придёт, и принимает значение, "+
		"которое сервер отвергнет. Правьте пример, а не этот список.",
		census.Examples, census.KeysJudged, strings.Join(lines, "\n"))
}

// TestClientDocsRetiredFieldLedgerHasSubject — запись, которой больше нечего
// прощать, есть находка.
//
// Без этой пробы ведомость переживала бы свой дефект: прощённое вхождение
// исчезает, гейт остаётся зелёным, а запись продолжает создавать впечатление
// покрытия, которого нет, — и унаследует следующую слепую зону.
func TestClientDocsRetiredFieldLedgerHasSubject(t *testing.T) {
	opts := clientDocsContractDriftOptions(t)
	if len(clientDocsRetiredFieldLedger) == 0 {
		t.Log("ведомость пуста — прощать нечего; это цель, а не поломка")
		return
	}
	live, reserved, _, err := clientDocsProtoDomains(opts)
	if err != nil {
		t.Fatalf("дерево контрактов не прочитано: %v", err)
	}
	sites, err := clientDocsSites(opts)
	if err != nil {
		t.Fatalf("сайты не найдены: %v", err)
	}
	domainOfPage := func(page string) (string, bool) {
		for _, s := range sites {
			if strings.HasPrefix(page, s.Dir+"/") {
				return s.Domain, true
			}
		}
		return "", false
	}

	checked := 0
	for key, why := range clientDocsRetiredFieldLedger {
		parts := strings.SplitN(key, "#", 2)
		if len(parts) != 2 {
			t.Errorf("запись ведомости %q не имеет формы «<страница>#<ключ>»", key)
			continue
		}
		page, field := parts[0], parts[1]
		if why == "" {
			t.Errorf("запись ведомости %q без причины: послабление без записанной причины "+
				"снимут как непонятное либо не снимут никогда", key)
		}
		raw, rerr := os.ReadFile(filepath.Join(opts.Root, filepath.FromSlash(page))) // #nosec G304 -- путь из ведомости этого же пакета
		if rerr != nil {
			t.Errorf("запись ведомости %q: страницы нет в дереве — прощать нечего", key)
			continue
		}
		domain, ok := domainOfPage(page)
		if !ok {
			t.Errorf("запись ведомости %q: страница не принадлежит ни одному сайту", key)
			continue
		}
		if !reserved[domain][field] {
			t.Errorf("запись ведомости %q: имя %q больше НЕ забрано контрактом домена %q — "+
				"прощать нечего, запись снимается", key, field, domain)
			continue
		}
		if live[domain][field] {
			t.Errorf("запись ведомости %q: имя %q снова живое поле домена %q — прощать нечего",
				key, field, domain)
			continue
		}
		if !strings.Contains(string(raw), `"`+field+`"`) {
			t.Errorf("запись ведомости %q: ключа %q на странице больше нет — прощать нечего, "+
				"запись снимается", key, field)
			continue
		}
		checked++
	}
	t.Logf("ведомость: записей %d · с живым предметом %d", len(clientDocsRetiredFieldLedger), checked)
}

// TestClientDocsDoNotPresentDeprecatedVerbsAsCurrent — вердикт о НАСТОЯЩЕМ дереве.
func TestClientDocsDoNotPresentDeprecatedVerbsAsCurrent(t *testing.T) {
	var log strings.Builder
	findings, census, err := AuditClientDocsDeprecationParity(clientDocsContractDriftOptions(t), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	if census.ProtoFiles < 50 {
		t.Fatalf("файлов контракта %d — дерево контрактов не прочитано", census.ProtoFiles)
	}
	if census.DeprecatedPaths == 0 {
		t.Fatalf("помеченных к снятию путей 0 — множество, о котором выносится вердикт, пусто; " +
			"либо пометок в контракте не осталось (тогда снимайте гейт вместе с предметом), " +
			"либо распознаватель их больше не видит")
	}
	if census.Sites < 5 || census.Pages < 50 {
		t.Fatalf("сайтов %d, страниц %d — обход пуст, вердикт беспредметен", census.Sites, census.Pages)
	}
	if census.Blocks == 0 {
		t.Fatalf("блоков операции 0 — распознаватель формы страницы не сработал")
	}
	// Ноль СВЕРЕННЫХ блоков означает, что ни один помеченный путь не документирован
	// вовсе: вердикт зелен, потому что о нём не высказывались. Это отличимо от
	// «высказались и всё в порядке», и различие обязано быть видно.
	if census.BlocksJudged == 0 {
		t.Fatalf("блоков о помеченном пути 0 — ни один помеченный к снятию глагол не "+
			"документирован ни на одной странице (помеченных путей %d). Вердикт не вынесен",
			census.DeprecatedPaths)
	}

	if len(findings) == 0 {
		return
	}
	lines := make([]string, 0, len(findings))
	for _, f := range findings {
		lines = append(lines, "  "+f.String())
	}
	t.Errorf("клиентская страница подаёт как действующее то, что контракт помечает к снятию "+
		"(помеченных путей %d, блоков сверено %d):\n%s\n\n"+
		"Депрекация, невидимая клиенту, — это обещание совместимости, которого продукт "+
		"не давал: клиент строит интеграцию на чтении, помеченном к снятию, и узнаёт об "+
		"этом в момент снятия. Поставьте пометку в блоке операции и назовите рядом "+
		"рекомендованную замену.",
		census.DeprecatedPaths, census.BlocksJudged, strings.Join(lines, "\n"))
}
