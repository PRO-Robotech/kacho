// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/platformmodules"
)

func clientDocsContractDriftOptions(t *testing.T) ClientDocsContractDriftOptions {
	t.Helper()
	return ClientDocsContractDriftOptions{
		Root:      repoRoot(t),
		ProtoRoot: "proto",
		// Балансировщик — единственный домен, чей каталог сайта и каталог
		// контракта расходятся: `services/nlb/docs` против
		// `proto/kacho/cloud/loadbalancer`. Псевдоним объявлен, а не выведен:
		// выводить его не из чего, расхождение историческое. Берётся у
		// ЕДИНСТВЕННОГО объявления, а не выписывается здесь: выписанная копия
		// разошлась бы с деревом молча, и гейт судил бы не тот каталог (#1885).
		DomainAliases: platformmodules.AliasesByService(),
	}
}

// TestClientDocsExamplesDoNotShowRetiredFields — вердикт о НАСТОЯЩЕМ дереве.
//
// Способность падать доказывает не этот прогон, а инъекция
// (`clientdocscontractdrift_injection_test.go`): здесь только вердикт.
func TestClientDocsExamplesDoNotShowRetiredFields(t *testing.T) {
	t.Parallel()
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
	// Третья половина: каждый объявленный ОБЩИЙ пакет обязан дать поля. Пакет,
	// выпавший из обхода, превращает поля конверта в «снятые» поля домена — и
	// гейт краснеет на верном тексте либо, хуже, молчит, пока совпадения нет.
	if len(census.SharedSilent) > 0 {
		t.Fatalf("общих пакетов объявлено %d, без полей в обходе %d (%s) — пакет снят либо "+
			"обход не видит его корня; снимите объявление вместе с пакетом или научите обход "+
			"его корню (contractDomainBases)",
			census.SharedDeclared, len(census.SharedSilent), strings.Join(census.SharedSilent, " "))
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
	t.Parallel()
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
	t.Parallel()
	var log strings.Builder
	findings, census, err := AuditClientDocsDeprecationParity(clientDocsContractDriftOptions(t), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	if census.ProtoFiles < 50 {
		t.Fatalf("файлов контракта %d — дерево контрактов не прочитано", census.ProtoFiles)
	}
	// ЗДЕСЬ СТОЯЛО «помеченных путей 0 ⇒ отказ: либо пометок не осталось, либо
	// распознаватель их не видит». Различить эти два исхода по числу было можно,
	// пока пометки лежали в дереве, которое читает ЭТОТ анализатор. Решением
	// владельца (kacho#2616, исход C, 2026-09-13) контракты службы доступа уехали
	// в её собственный репозиторий, и с ними уехали ВСЕ пометки: предикат
	// `grep -c '^[[:space:]]*//[[:space:]]*DEPRECATED'` даёт по этому дереву ноль
	// файлов, а по дереву контрактов её модуля — 13 пометок в трёх файлах.
	//
	// Популяция этого анализатора собирается из `<Root>/<ProtoRoot>` (два места:
	// `clientDocsProtoDomains` и `clientDocsDeprecatedPaths` в
	// clientdocscontractdrift.go), то есть КОРНЕМ В ДЕРЕВЕ; корень, приезжающий
	// модулем, она не резолвит, а состав берёт у индекса git, которого у кэша
	// модулей нет by construction. Расширить её отсюда нечем: единственная ручка входа —
	// `ProtoRoot`, и она относительна корню дерева.
	//
	// ПОЧЕМУ ЭТО НЕ ГАШЕНИЕ СУДЬИ. Способность гейта упасть держат ЧЕТЫРЕ пробы
	// инъекции, а не это число: TestDeprecationGateFallsOnADeprecatedVerbShownAsCurrent
	// (ровно одна находка при DeprecatedPaths=1 и BlocksJudged=1),
	// TestDeprecationGateStaysSilentOnLawfulTwins, TestDeprecationMarkIsNotCountedFromANeighbouringBlock,
	// TestDeprecationMarkIsReadFromTheDeclarationNotAnyComment — последняя зовёт
	// распознаватель напрямую и требует, чтобы он прочитал пометку. Отказ пустого
	// обхода держит TestBothGatesFallOnAnEmptyWalk. Прочитанное от непрочитанного
	// по-прежнему отличают отказы ниже: файлов контракта, сайтов, страниц и блоков
	// операции.
	//
	// Число САМО возвращает вердикт: первая пометка к снятию в контракте
	// платформенного домена даёт DeprecatedPaths > 0, страницы его сайта лежат
	// здесь, и гейт снова судит. Поэтому ноль печатается переписью, а вердиктом не
	// становится.
	if census.DeprecatedPaths == 0 {
		t.Logf("помеченных к снятию путей 0 при %d прочитанных файлах контракта: пометок в "+
			"контрактах ЭТОГО дерева не осталось — все уехали вместе с контрактом службы "+
			"доступа. Способность падать держит инъекция, а не это число", census.ProtoFiles)
	}
	if census.Sites < 5 || census.Pages < 50 {
		t.Fatalf("сайтов %d, страниц %d — обход пуст, вердикт беспредметен", census.Sites, census.Pages)
	}
	if census.Blocks == 0 {
		t.Fatalf("блоков операции 0 — распознаватель формы страницы не сработал")
	}
	// ЗДЕСЬ СТОЯЛО «блоков о помеченном пути 0 ⇒ вердикт не вынесен». Утверждение
	// было верным, пока ОБЕ половины предмета жили в одном дереве: пометки — в
	// контракте, страницы о них — на сайте документации той же службы.
	//
	// Половины разошлись по репозиториям, и это измерено, а не предположено: все
	// пометки к снятию стоят в контракте службы доступа, а её САЙТ документации
	// уехал вместе с самой службой (`git ls-tree -d --name-only HEAD
	// services/iam/docs` резолвился до выноса, сегодня каталога нет).
	//
	// ЗДЕСЬ СТОЯЛО «контракт остался здесь» с предикатом
	// `git grep -ln 'deprecated = true' -- 'proto/**/*.proto'` → три файла. В
	// ИНДЕКСЕ этого дерева тот же предикат даёт НОЛЬ: контракт уехал вслед за
	// страницами (kacho#2616, исход C, 2026-09-13).
	//
	// НО ПОПУЛЯЦИЯ ГЕЙТА НЕ ОПУСТЕЛА, и это правка того же дня: обход читает
	// доменные корни ОБОИХ домов (`contractDomainBases`), поэтому пометки
	// контракта службы он видит в дереве модуля — 4 помеченных пути при 122
	// прочитанных файлах контракта (перепись выше). Опустела бы она без второго
	// дома, и тогда «находок ноль» означало бы «ноль прочитанного».
	//
	// Число СВЕРЕННЫХ блоков от этого не изменилось — оно было и осталось нулём, —
	// но причина у нуля стала одна и названа: пометки есть, страниц о них в этом
	// дереве нет.
	//
	// Отказ на этом факте требовал бы либо вернуть чужой сайт, либо пометить к
	// снятию что-нибудь у платформенного домена ради зелёного — то есть чинить
	// дерево под проверку. Поэтому число печатается ПЕРЕПИСЬЮ (ниже, в вердикте
	// находок) и остаётся видимым, а вердиктом не становится.
	//
	// Способность гейта упасть от этого не потеряна и держится синтетикой:
	// TestDeprecationGateFallsOnADeprecatedVerbShownAsCurrent строит своё дерево,
	// где пометка и страница о ней стоят рядом, и требует ровно одной находки
	// при DeprecatedPaths=1 и BlocksJudged=1; два законных близнеца требуют
	// молчания. Пока эта пара зелена, ноль здесь — свойство дерева.
	if census.BlocksJudged == 0 {
		t.Logf("блоков о помеченном пути 0 при %d помеченных путях: пометки принадлежат "+
			"домену, чей сайт документации живёт в другом репозитории — сверять не с чем. "+
			"Способность падать держит инъекция, а не это число", census.DeprecatedPaths)
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
