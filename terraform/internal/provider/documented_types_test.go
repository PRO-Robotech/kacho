// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// TestDocumentedTypesMatchTheRegistry — перечни типов на страницах сверяются с реестром
// В ОБЕ СТОРОНЫ.
//
// # Зачем именно в обе
//
// Односторонняя проверка ловит только «завели тип, забыли написать» — и молчит о
// противоположном, которое опаснее: страница утверждала, что адрес, таблица маршрутизации и
// группа безопасности «ещё не заведены», при том что все три уже были в реестре. Арендатор
// читает такое как «возможности нет» и обходится без неё; никакая сборка на это не краснеет.
//
// # Почему страницы ВЫВОДЯТСЯ, а не называются координатой
//
// Прежняя редакция сторожила ОДНУ страницу — платформенную, — и это было видно только тому,
// кто читал её исходник. Страниц с перечнем типов больше одной; самая подробная из
// несторожимых разбирает типы по одному и пережила бы переименование молча. Поэтому
// население выводится из дерева: страница с разделом перечня, называющая хоть один
// зарегистрированный тип, сторожится по построению — включая ту, которой ещё нет.
//
// # За какие типы отвечает страница
//
// Тоже выводится, а не назначается: страница отвечает за все зарегистрированные типы того
// ДОМЕНА, чей тип она называет. Домен читается из самого имени — `kacho_vpc_network`
// принадлежит домену `kacho_vpc`, `kaname_role` — домену `kaname`. Страница платформы
// называет типы всех доменов и отвечает за все; страница домена — только за свой.
// Назначенная руками карта разошлась бы с деревом молча, а деление по первому сегменту
// сделало бы каждую доменную страницу ответственной за всю платформу.
//
// # Почему сканируется РАЗДЕЛ, а не вся страница
//
// Причина измерена, а не предположена: по всей странице предикат считал `var.kacho_token`
// из примера настройки за имя типа. Имя ресурса и имя переменной в примере неразличимы по
// форме, поэтому различает их МЕСТО — раздел, чей предмет и есть перечень.
func TestDocumentedTypesMatchTheRegistry(t *testing.T) {
	root := repoTreeRoot(t)

	registered := map[string]bool{}
	p := New().(*kachoProvider)
	for _, ctor := range p.Resources(context.Background()) {
		registered[typeNameOfResource(ctor())] = true
	}
	for _, ctor := range p.DataSources(context.Background()) {
		registered[typeNameOfDataSource(ctor())] = true
	}
	if len(registered) == 0 {
		t.Fatal("реестр провайдера пуст — сверять не с чем")
	}

	// Образец имени выводится из реестра: семейство, заведённое завтра, попадёт под
	// наблюдение само. Рукописная альтернатива уже подводила — предикат, знавший одну
	// приставку, объявил девять существующих типов ненаписанными.
	families := map[string]bool{}
	for name := range registered {
		if i := strings.Index(name, "_"); i > 0 {
			families[name[:i]] = true
		}
	}
	if len(families) == 0 {
		t.Fatal("семейства имён из реестра не выведены — узнавать нечем")
	}
	// Хвост имени обязан кончаться знаком, а не подчёркиванием: `kacho_iam_` в прозе —
	// приставка, о которой идёт речь, а не имя типа. Без этого условия проба краснела бы
	// на собственном объяснении.
	typeNamePattern := regexp.MustCompile(
		`\b(?:` + strings.Join(sortedSet(families), "|") + `)_[a-z0-9]+(?:_[a-z0-9]+)*\b`)

	pages, err := treecorpus.UnderWithSuffix(root, ".mdx")
	if err != nil {
		t.Fatalf("перепись страниц: %v", err)
	}
	if len(pages) == 0 {
		t.Fatal("страниц не найдено — обход пуст, вердикт беспредметен")
	}

	guarded := 0
	compared := 0
	for _, path := range pages {
		raw, err := os.ReadFile(path) // #nosec G304 -- путь пришёл из индекса репозитория
		if err != nil {
			t.Fatalf("чтение %s: %v", path, err)
		}
		section, ok := sectionBetween(string(raw), "## Ресурсы", "\n## ")
		if !ok {
			continue
		}

		documented := map[string]bool{}
		for _, m := range typeNamePattern.FindAllString(section, -1) {
			documented[m] = true
		}
		// Раздел с таким заголовком, но без единого имени типа, перечнем не является:
		// у службы может быть свой раздел «Ресурсы» про предметную область.
		ownDomains := map[string]bool{}
		for name := range documented {
			if registered[name] {
				ownDomains[domainOfTypeName(name)] = true
			}
		}
		if len(ownDomains) == 0 {
			continue
		}
		guarded++

		rel, _ := filepath.Rel(root, path)
		var missing, phantom []string
		for name := range registered {
			if ownDomains[domainOfTypeName(name)] && !documented[name] {
				missing = append(missing, name)
			}
		}
		for name := range documented {
			if !registered[name] {
				phantom = append(phantom, name)
			}
		}
		compared += len(documented)
		sort.Strings(missing)
		sort.Strings(phantom)

		if len(missing) > 0 {
			t.Errorf("%s: провайдер объявляет типы, которых нет в перечне страницы: %s\n"+
				"Страница — единственное место, куда приходят с вопросом «что умеет "+
				"провайдер»; тип, о котором она молчит, для читателя не существует.",
				rel, strings.Join(missing, ", "))
		}
		if len(phantom) > 0 {
			t.Errorf("%s: перечень называет типы, которых в реестре НЕТ: %s\n"+
				"Читатель напишет их в конфигурацию и получит «Invalid resource type».",
				rel, strings.Join(phantom, ", "))
		}
	}

	if guarded == 0 {
		t.Fatal("страниц с перечнем типов не найдено — предикат поиска устарел либо " +
			"страницы переписаны. Проба, потерявшая свой предмет, обязана падать")
	}
	t.Logf("осмотрено: страниц %d, из них с перечнем типов %d, имён сверено %d, "+
		"типов в реестре %d, семейств имён %d",
		len(pages), guarded, compared, len(registered), len(families))
}

// sectionBetween — кусок текста от заголовка `from` до следующего заголовка `to`.
//
// Оба конца обязаны найтись, и об этом сообщает второе значение: раздел, который не
// нашёлся, дал бы пустую выборку — то есть «расхождений нет» на непрочитанной странице.
func sectionBetween(text, from, to string) (string, bool) {
	i := strings.Index(text, from)
	if i < 0 {
		return "", false
	}
	rest := text[i+len(from):]
	j := strings.Index(rest, to)
	if j < 0 {
		return "", false
	}
	return rest[:j], true
}

func sortedSet(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// domainOfTypeName — домен, которому принадлежит имя типа.
//
// Правило выведено из формы самих имён, а не назначено: у платформы имя несёт домен вторым
// сегментом (`kacho_vpc_network`), у службы, называющей себя своим именем, домен и есть
// первый сегмент (`kaname_role`). Деление по первому сегменту у всех сделало бы каждую
// доменную страницу ответственной за всю платформу.
func domainOfTypeName(name string) string {
	seg := strings.SplitN(name, "_", 3)
	if len(seg) >= 3 && seg[0] == providerTypeName {
		return seg[0] + "_" + seg[1]
	}
	return seg[0]
}
