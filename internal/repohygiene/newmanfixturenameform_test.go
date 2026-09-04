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
//  2. Сервис, НЕ мигрировавший под канон (registry), — у него своя форма, и судить
//     его этой мерой значило бы требовать свойства, которого контракт не обещает.
//     Перечень мигрировавших ВЫВОДИТСЯ из дерева по наличию миграции, а не
//     выписывается: выписанный разошёлся бы молча в тот день, когда мигрирует
//     следующий. Шестым мигрировал iam (#1279).
//  3. Ресурс, чьё имя — ДРУГОЙ РЕФЕРЕНТ, а не косметическая метка. Такой ресурс
//     живёт в мигрировавшем сервисе и формой имени не судится: идентификатор роли
//     (`roles/vpc.admin`) — то, на что ссылаются привязки, и подчёркивание в нём
//     законно по его собственной форме (записанное решение владельца, #715; та же
//     граница проведена миграцией формы имени в iam и пробой её ограничения).
//     Освобождение даётся по ЦЕЛИ ЗАПРОСА, а не по имени шага: имя шага — проза, и
//     предикат по нему стал бы маской. Перечень проверяется в обе стороны: запись,
//     которой больше нечего освобождать, — находка.
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

// nameFormOtherReferents — цели запроса, чьё поле `name` формой имени НЕ судится:
// значение — причина. Ключ сравнивается как подстрока пути, потому что адрес шага
// несёт подстановку базы (`{{baseUrl}}`) и хвост идентификатора.
//
// Перечень намеренно КРОШЕЧНЫЙ и обязан таким остаться: каждая запись — слепая
// зона, выданная вперёд. Заводя новую, назови, ЧЕМ имя этого ресурса судится
// вместо канона, — иначе это не другой референт, а забытая фикстура.
var nameFormOtherReferents = map[string]string{
	"/iam/v1/roles": "идентификатор роли, а не косметическая метка: на него ссылаются " +
		"привязки, и он судится своей формой (`^[a-z][a-z0-9_]{0,40}$` либо `roles/<модуль>.<роль>`)",
}

// reMustache — подстановка окружения. Её значение гейту неизвестно, поэтому вместо
// неё подставляется образец, отвечающий канону: судим ФОРМУ обрамления, а не значение.
var reMustache = regexp.MustCompile(`\{\{[^}]*\}\}`)

// canonMigratedServices — сервисы, чья схема приведена к канону.
//
// Выводится из СОДЕРЖИМОГО миграций общей функцией `nameFormCanonAdoptions`, а не
// из имени файла. Прежде здесь стоял предикат по имени — `strings.Contains(f.Name(),
// "resource_name_single_form")`, — и он был ПРОКСИ: настоящий признак это форма
// имени, объявленная в схеме, а имя файла лишь место, откуда она однажды туда
// попала.
//
// Прокси пережил свой предмет ровно так, как проксям и положено. Цепь iam сведена
// в одну первичную миграцию: форма на месте, файла нет — и iam молча выпал из
// перечня. Наблюдалось это не как «сервис не проверен», а с другого конца и
// непохоже: освобождение `/iam/v1/roles` перестало освобождать хоть один шаг,
// потому что коллекции iam вообще не читались, и гейт объявил находкой ЕГО —
// запись, у которой предмет был на месте всё это время.
//
// Тот же вывод читает гейт суффикса ограничения (`checkviolationtone_test.go`).
// Два места об одном предмете разошлись бы молча — и разошлись бы они именно так,
// как разошлись здесь.
func canonMigratedServices(t *testing.T, root string) map[string]bool {
	t.Helper()
	adoptions, err := nameFormCanonAdoptions(root)
	if err != nil {
		t.Fatalf("состав дерева не читается (%v) — предпосылка гейта не установлена", err)
	}
	out := map[string]bool{}
	for _, a := range adoptions {
		out[a.Service] = true
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
// Возвращает: находки, число шагов с именем в теле, число судимых из них и
// сколько шагов освобождено по каждому другому референту.
//
// Последнее — не украшение отчёта: перечень освобождений обязан истекать сам, а
// «сколько раз он сработал» есть единственный способ отличить запись, у которой
// есть предмет, от записи, пережившей его.
func judgeFixtureNames(rel string, c pmCollection) (
	findings []string, withName, judged int, spared map[string]int,
) {
	spared = map[string]int{}
	walkItems(c.Item, func(it pmItem) {
		names := bodyNames(it)
		if len(names) == 0 {
			return
		}
		withName++
		if expectsRefusal(it) {
			return // негодное имя здесь и есть предмет кейса
		}
		if ref := otherReferent(it); ref != "" {
			spared[ref]++
			return // имя этого ресурса судится не каноном
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
	return findings, withName, judged, spared
}

// otherReferent — цель шага, чьё имя формой имени не судится; пусто, если шаг
// обычный. Судится АДРЕС запроса: имя шага — проза, и предикат по ней стал бы
// маской для любой забытой фикстуры, которую назвали «create-role-…».
func otherReferent(it pmItem) string {
	if it.Request == nil {
		return ""
	}
	url := strings.TrimSpace(rawURL(it.Request.URL))
	for path := range nameFormOtherReferents {
		if strings.Contains(url, path) {
			return path
		}
	}
	return ""
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
	spared := map[string]int{}

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
			f, withName, j, sp := judgeFixtureNames(filepath.ToSlash(rel), c)
			findings = append(findings, f...)
			stepsWithName += withName
			judged += j
			for k, v := range sp {
				spared[k] += v
			}
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

	sparedWhy := make([]string, 0, len(nameFormOtherReferents))
	for path, why := range nameFormOtherReferents {
		sparedWhy = append(sparedWhy, fmt.Sprintf("%s ×%d — %s", path, spared[path], why))
	}
	sort.Strings(sparedWhy)

	t.Logf("осмотрено: сервисов под каноном %d (%s), коллекций %d, шагов с именем в теле %d, "+
		"из них судимых (не утверждающих отказ) %d; освобождено по другому референту %d [%s]",
		len(migrated), strings.Join(svcNames, " "), collections, stepsWithName, judged,
		len(spared), strings.Join(sparedWhy, "; "))

	if collections == 0 || stepsWithName == 0 {
		t.Fatal("прочитано ноль коллекций либо ноль шагов с именем — гейт беспредметен, " +
			"его зелёное ничего не означает")
	}

	// Освобождение живёт, пока у него есть предмет. Запись, не освободившая НИ
	// ОДНОГО шага, — находка: она переживает свой предмет и достанется в
	// наследство следующей слепой зоне.
	for path := range nameFormOtherReferents {
		if spared[path] == 0 {
			t.Errorf("освобождение %q не освободило ни одного шага — оно потеряло предмет "+
				"и подлежит снятию, иначе станет слепой зоной для следующей фикстуры", path)
		}
	}

	if len(findings) > 0 {
		sort.Strings(findings)
		t.Errorf("фикстур с именем вне канона: %d\n  %s",
			len(findings), strings.Join(findings, "\n  "))
	}
}
