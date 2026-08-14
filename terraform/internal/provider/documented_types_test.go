// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

import (
	"context"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// providerPagePath — страница, на которую приходят с вопросом «что вообще умеет
// провайдер». Она одна на всю платформу и лежит в документации VPC по
// историческим причинам: провайдер начинался с этого домена.
const providerPagePath = "../../../services/vpc/docs/content/terraform/provider.mdx"

var typeNamePattern = regexp.MustCompile(`kacho_[a-z0-9_]+`)

// TestDocumentedTypesMatchTheRegistry — перечень на странице провайдера сверяется
// с реестром В ОБЕ СТОРОНЫ.
//
// Зачем именно в обе. Односторонняя проверка ловит только «завели тип, забыли
// написать» — и молчит о противоположном, которое опаснее: страница утверждала,
// что адрес, таблица маршрутизации и группа безопасности «ещё не заведены», при
// том что все три уже были в реестре. Арендатор читает такое как «возможности
// нет» и обходится без неё; никакая сборка на это не краснеет.
//
// Проверяется СТРАНИЦА, а не вся документация: перечень типов — её предмет, и
// расширять радиус на прозу значило бы ловить упоминание типа в примере как
// объявление о поддержке.
func TestDocumentedTypesMatchTheRegistry(t *testing.T) {
	raw, err := os.ReadFile(providerPagePath)
	if err != nil {
		t.Fatalf("страница провайдера не прочитана (%s): %v\n"+
			"Если она переехала — правьте координату здесь же: проба, потерявшая свой "+
			"предмет, обязана падать, а не молчать.", providerPagePath, err)
	}

	// Сканируется РАЗДЕЛ перечня, а не вся страница.
	//
	// Причина измерена, а не предположена: по всей странице предикат считал
	// `var.kacho_token` из примера настройки за имя типа. Имя ресурса и имя
	// переменной в примере неразличимы по форме, поэтому различает их МЕСТО —
	// раздел, чей предмет и есть перечень.
	section, ok := sectionBetween(string(raw), "## Ресурсы", "## Модули")
	if !ok {
		t.Fatalf("на странице нет раздела «## Ресурсы … ## Модули» — предикат поиска "+
			"устарел или страница переписана. Проба, потерявшая свой предмет, обязана "+
			"падать: правьте %s или координаты здесь", providerPagePath)
	}

	documented := map[string]bool{}
	for _, m := range typeNamePattern.FindAllString(section, -1) {
		documented[m] = true
	}

	registered := map[string]bool{}
	p := New().(*kachoProvider)
	for _, ctor := range p.Resources(context.Background()) {
		registered[typeNameOfResource(ctor())] = true
	}
	for _, ctor := range p.DataSources(context.Background()) {
		registered[typeNameOfDataSource(ctor())] = true
	}

	// Перепись обеих сторон: без неё «расхождений нет» неотличимо от «страница
	// пуста» или «реестр не собрался».
	t.Logf("на странице имён: %d, в реестре: %d", len(documented), len(registered))
	if len(documented) == 0 || len(registered) == 0 {
		t.Fatal("одна из сторон пуста — сверять нечего, и проба зеленела бы всегда")
	}

	var missing, phantom []string
	for name := range registered {
		if !documented[name] {
			missing = append(missing, name)
		}
	}
	for name := range documented {
		if !registered[name] {
			phantom = append(phantom, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(phantom)

	if len(missing) > 0 {
		t.Errorf("провайдер объявляет типы, которых нет на его странице: %s\n"+
			"Страница — единственное место, куда приходят с вопросом «что умеет провайдер»; "+
			"тип, о котором она молчит, для читателя не существует.\n"+
			"Правьте %s", strings.Join(missing, ", "), providerPagePath)
	}
	if len(phantom) > 0 {
		t.Errorf("страница называет типы, которых в реестре НЕТ: %s\n"+
			"Читатель напишет их в конфигурацию и получит «Invalid resource type».\n"+
			"Правьте %s", strings.Join(phantom, ", "), providerPagePath)
	}
}

// sectionBetween — кусок текста от заголовка `from` до заголовка `to`.
//
// Оба конца обязаны найтись, и об этом сообщает второе значение: раздел, который
// не нашёлся, дал бы пустую выборку — то есть «расхождений нет» на непрочитанной
// странице.
func sectionBetween(text, from, to string) (string, bool) {
	i := strings.Index(text, from)
	if i < 0 {
		return "", false
	}
	rest := text[i:]
	j := strings.Index(rest, to)
	if j < 0 {
		return "", false
	}
	return rest[:j], true
}
