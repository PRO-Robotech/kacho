// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// newmanfixturenameform_test.go — имя, которое ФИКСТУРА даёт создаваемому ресурсу,
// обязано отвечать канону формы (RFC 1123 DNS label), если сервис под канон мигрировал.
//
// # Предмет
//
// Канон имени (#715) сузил форму до `^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`. Сервер
// теперь отвергает то, что прежде принимал: заглавные, пробелы, подчёркивание. Пробы
// при этом продолжают слать человекочитаемые имена вида «QA Region CRUD …», и отказ
// приходит НЕ туда, где его читают: мутация возвращает Operation (200), операция
// падает асинхронно, ресурс не появляется, а красное вылезает через два-три шага —
// на подтверждении, на поллере по незахваченному идентификатору, на уборке.
//
// Цена измерена на прогоне #712: два набора из пяти, 273 упавших утверждения, из них
// 155 в одной коллекции — и ВСЕ от двух фикстурных имён. Ни одно падение не называло
// предмет: верхнее сообщение говорило «Region … not found» и «expected 403 to equal 200».
//
// # Почему гейт, а не внимание
//
// Класс уже дважды пережил собственный поиск. Перепись по литералам `"name": "…"`
// нашла geo и НЕ нашла storage: там имя собрано f-строкой, то есть предикат оказался
// уже своего предмета. Гейт читает СГЕНЕРИРОВАННУЮ коллекцию — то, что исполняется, —
// и потому не зависит от того, как имя записано в исходнике.
//
// # Что гейт НЕ судит, и почему это не послабление
//
//  1. Шаг, УТВЕРЖДАЮЩИЙ отказ (`expectsRefusal`), — негодное имя там и есть предмет
//     кейса: `NLB-CR-VAL-NAME-UPPERCASE` шлёт `EdgePublic-…` именно затем, чтобы
//     получить отказ. Различитель полярности — общая деталь, его собственная
//     способность ошибаться доказана отдельно (`newmanrefusalpolarity_injection_test.go`).
//  2. Сервис, НЕ мигрировавший под канон (iam, registry), — у него своя форма, и
//     судить его этой мерой значило бы требовать свойства, которого контракт не
//     обещает. Перечень мигрировавших ВЫВОДИТСЯ из дерева по наличию миграции, а не
//     выписывается: выписанный разошёлся бы молча в тот день, когда мигрирует шестой.
//
// # Предпосылка проверяется, объём печатается
//
// Ноль мигрировавших сервисов, ноль коллекций либо ноль шагов с именем — это «ноль
// прочитанного», а не «ноль находок»: гейт в таком случае падает, а не молчит.
package repohygiene

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// nameFormCanon — форма имени из `pkg/validate/nameform`. Здесь она воспроизведена
// намеренно: гейт судит ДАННЫЕ проб, а не код, и не должен зеленеть оттого, что
// продуктовая форма ослабла. Расхождение между этими двумя формами — само по себе
// находка, и его ловит `nameform`-проба на стороне продукта.
var nameFormCanon = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)

// reMustache — подстановка окружения. Её значение гейту неизвестно, поэтому вместо
// неё подставляется образец, отвечающий канону: судим ФОРМУ обрамления, а не значение.
var reMustache = regexp.MustCompile(`\{\{[^}]*\}\}`)

// canonMigratedServices — сервисы, чья схема приведена к канону. Выводится из дерева
// по имени миграции, а не выписывается.
func canonMigratedServices(t *testing.T, root string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	entries, err := os.ReadDir(filepath.Join(root, "services"))
	if err != nil {
		t.Fatalf("каталог сервисов не читается (%v) — предпосылка гейта не установлена", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		migDir := filepath.Join(root, "services", e.Name(), "internal", "migrations")
		files, err := os.ReadDir(migDir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if strings.Contains(f.Name(), "resource_name_single_form") {
				out[e.Name()] = true
				break
			}
		}
	}
	return out
}

// bodyNames — значения поля `name` в теле запроса шага. Тело разбирается как JSON;
// шаг с неразбираемым телом пропускается молча — форму имени в нём не установить,
// и выдумывать её гейт не вправе.
func bodyNames(it pmItem) []string {
	if it.Request == nil || it.Request.Body == nil || it.Request.Body.Raw == "" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(it.Request.Body.Raw), &m); err != nil {
		return nil
	}
	v, ok := m["name"]
	if !ok {
		return nil
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return nil
	}
	return []string{s}
}

func walkItems(items []pmItem, fn func(pmItem)) {
	for _, it := range items {
		if len(it.Item) > 0 {
			walkItems(it.Item, fn)
			continue
		}
		fn(it)
	}
}

// judgeFixtureNames — ядро гейта, вынесенное ради инъекции: обход дерева и суд над
// коллекцией — разные ответственности, и способность судьи ошибаться проверяется на
// входе, СОБРАННОМ пробой, а не снятом с дерева (дерево движется, проба истекла бы
// вместе с ним).
//
// Возвращает: находки, число шагов с именем в теле, число судимых из них.
func judgeFixtureNames(rel string, c pmCollection) (findings []string, withName, judged int) {
	walkItems(c.Item, func(it pmItem) {
		names := bodyNames(it)
		if len(names) == 0 {
			return
		}
		withName++
		if expectsRefusal(it) {
			return // негодное имя здесь и есть предмет кейса
		}
		judged++
		for _, n := range names {
			probe := reMustache.ReplaceAllString(n, "r1")
			if !nameFormCanon.MatchString(probe) {
				findings = append(findings, fmt.Sprintf(
					"%s → шаг %q даёт имя %q, которое сервер отвергнет "+
						"(канон %s); отказ придёт асинхронно и красное вылезет у соседа",
					rel, it.Name, n, nameFormCanon.String()))
			}
		}
	})
	return findings, withName, judged
}

func TestFixtureNamesObeyTheCanonWhereTheServiceMigrated(t *testing.T) {
	root := repoRoot(t)
	migrated := canonMigratedServices(t, root)
	if len(migrated) == 0 {
		t.Fatal("под канон не мигрировал НИ ОДИН сервис — предпосылка гейта отсутствует, " +
			"и его молчание означало бы «не читал», а не «находок нет»")
	}

	var (
		collections, stepsWithName, judged int
		findings                           []string
	)

	for svc := range migrated {
		base := filepath.Join(root, "services", svc, "tests", "newman", "collections")
		if _, err := os.Stat(base); err != nil {
			continue // у сервиса нет сквозных проб — это не находка
		}
		err := rootedWalk(base, func(rel string) bool {
			return strings.HasSuffix(rel, ".json")
		}, func(abs string, body []byte) error {
			collections++
			var c pmCollection
			if err := json.Unmarshal(body, &c); err != nil {
				return fmt.Errorf("коллекция %s не разбирается: %w", abs, err)
			}
			rel, _ := filepath.Rel(root, abs)
			f, withName, j := judgeFixtureNames(filepath.ToSlash(rel), c)
			findings = append(findings, f...)
			stepsWithName += withName
			judged += j
			return nil
		})
		if err != nil {
			t.Fatalf("обход коллекций %s: %v", svc, err)
		}
	}

	svcNames := make([]string, 0, len(migrated))
	for s := range migrated {
		svcNames = append(svcNames, s)
	}
	sort.Strings(svcNames)

	t.Logf("осмотрено: сервисов под каноном %d (%s), коллекций %d, шагов с именем в теле %d, "+
		"из них судимых (не утверждающих отказ) %d",
		len(migrated), strings.Join(svcNames, " "), collections, stepsWithName, judged)

	if collections == 0 || stepsWithName == 0 {
		t.Fatal("прочитано ноль коллекций либо ноль шагов с именем — гейт беспредметен, " +
			"его зелёное ничего не означает")
	}

	if len(findings) > 0 {
		sort.Strings(findings)
		t.Errorf("фикстур с именем вне канона: %d\n  %s",
			len(findings), strings.Join(findings, "\n  "))
	}
}
