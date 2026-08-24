// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_inherited_schemas_are_declared_test.go — НАША секция схем личности
// обязана ПОКРЫВАТЬ то, что объявил слой под нами.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Процесс службы личности получает ДВА файла настроек и сливает их по порядку;
// наш идёт ВТОРЫМ. Слияние идёт по ключам, а `identity.schemas` — СПИСОК: он не
// дополняется, он ЗАМЕЩАЕТСЯ целиком. Значит всякая схема, объявленная слоем под
// нами, из действующей конфигурации ИСЧЕЗАЕТ — молча, без диагностики при старте
// и без единого признака в рендере: обе секции по отдельности валидны.
//
// Платят за исчезновение не новые регистрации (им умолчанием остаётся наша
// схема), а УЖЕ СУЩЕСТВУЮЩИЕ строки: имя схемы записано в строке самой личности,
// и чтение личности, чья схема не объявлена, отвечает `500 invalid_configuration`.
// Арендатор, заведённый до смены схемы, перестаёт читаться вовсе.
//
// Перевести такие строки на новую схему нельзя: обе схемы строгие
// (`additionalProperties: false`) и различаются составом признаков — валидация
// по новой отвергла бы каждую. Значит унаследованная схема обязана оставаться
// объявленной ровно столько, сколько живут ссылающиеся на неё строки.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ИМЕННО УТВЕРЖДАЕТСЯ — И ЧЕГО ЭТО НЕ УТВЕРЖДАЕТ
//
// Гейт дерева НЕ ЗНАЕТ множества живых `schema_id` — оно лежит в хранилище
// кластера, а не в git. Поэтому проверяется ПРОКСИ, и он назван прямо:
//
//	личность могла быть заведена только по схеме, которая была ОБЪЯВЛЕНА;
//	единственное объявление в дереве, кроме нашего, — секция подчарта
//	поставщика; значит наша секция обязана покрывать ЕЁ.
//
// Прокси не покрывает ровно один случай, и он назван, а не закрыт: схема,
// объявлявшаяся В ПРОШЛОМ и уже снятая из дерева. Ссылающиеся на неё строки
// живы, а объявления нет нигде — это видно только на кластере, запросом к
// хранилищу личностей. Гейт об этом молчит by construction, и молчание здесь
// означает «вне охвата», а не «чисто».
//
// Оси четыре, и две из них ОБРАТНЫЕ друг другу:
//
//	(1) ПОКРЫТИЕ  — каждый id, объявленный поставщиком, объявлен и у нас
//	                (иначе живые строки перестают читаться);
//	(2) САМОИСТЕЧЕНИЕ — каждый id, объявленный у нас как унаследованный,
//	                объявлен и поставщиком (иначе объявление пережило свой
//	                предмет: связка «якорь ↔ ссылка» разорвана, и следующая
//	                правка тела доедет только до одной стороны);
//	(3) СОГЛАСИЕ ТЕЛА — у общего id тело обязано совпадать ДОСЛОВНО. При якоре
//	                YAML это выполняется by construction; ось существует затем,
//	                чтобы замена якоря на копию литерала не прошла молча;
//	(4) БЕЗ КОЛЛИЗИИ — унаследованный id не совпадает с нашим собственным: две
//	                схемы под одним именем разрешает порядок, а не решение.
//
// Ось (1) без (2) оставила бы объявление, которому нечего наследовать, — это тот
// самый «список исключений, которому нечего исключать». Ось (2) без (1) — сам
// дефект.
//
// ПОЧЕМУ БЕЗ helm в первой пробе. Она читает ОБЪЯВЛЕНИЯ: рендер умбреллы требует
// скачанных зависимостей, которых в свежем клоне нет, и пропущенная проверка не
// краснеет никогда. Вторая проба, наоборот, читает РЕНДЕР — потому что
// объявление, которое шаблон не читает, есть форма без содержания.
//
// Способность упасть и смолчать доказана инъекцией —
// identity_inherited_schemas_injection_test.go.
package deploy_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// schemaDecl — одно объявление схемы личности: имя и тело (адрес тела).
type schemaDecl struct {
	ID  string
	URL string
}

func (d schemaDecl) String() string { return fmt.Sprintf("%s→%s", d.ID, shortURL(d.URL)) }

// shortURL укорачивает адрес тела для сообщений: тело унаследованной схемы —
// это base64 в семьсот с лишним знаков, и целиком оно делает вывод нечитаемым.
func shortURL(u string) string {
	if len(u) <= 48 {
		return u
	}
	return u[:44] + "…"
}

// inheritedSchemaFinding — одна находка адъюдикатора. Строка причины, а не код:
// стороны расхождения чинятся по-разному, и вердикт обязан это различать.
type inheritedSchemaFinding struct {
	Stack  string
	ID     string
	Reason string
}

// Причины — поимённо, чтобы инъекция сверялась с ними, а не с текстом.
const (
	inheritedNotCovered = "поставщик объявляет схему, наша секция её не объявляет"
	inheritedNoSubject  = "унаследованная схема объявлена у нас, но поставщик её больше не объявляет"
	inheritedBodyDrift  = "у общего имени схемы РАЗНЫЕ тела"
	inheritedCollides   = "унаследованное имя совпадает с собственной схемой продукта"
)

// adjudicateInheritedSchemas — ЕДИНСТВЕННЫЙ дискриминатор. Зовётся и переписью по
// дереву, и инъекцией: копия предиката разошлась бы с оригиналом молча и
// доказывала бы саму себя.
//
// `provider` — что объявляет слой под нами, `ours` — что объявляем мы как
// унаследованное, `ownID` — имя собственной схемы продукта.
func adjudicateInheritedSchemas(stack string, provider, ours []schemaDecl, ownID string) []inheritedSchemaFinding {
	var out []inheritedSchemaFinding

	byOurs := map[string]schemaDecl{}
	for _, d := range ours {
		byOurs[d.ID] = d
	}
	byProv := map[string]schemaDecl{}
	for _, d := range provider {
		byProv[d.ID] = d
	}

	// (1) покрытие
	for _, p := range provider {
		if p.ID == ownID {
			continue // поставщик назвал ровно нашу схему — покрыта собой
		}
		o, ok := byOurs[p.ID]
		if !ok {
			out = append(out, inheritedSchemaFinding{stack, p.ID, inheritedNotCovered})
			continue
		}
		// (3) согласие тела
		if o.URL != p.URL {
			out = append(out, inheritedSchemaFinding{stack, p.ID, inheritedBodyDrift})
		}
	}

	// (2) самоистечение + (4) коллизия
	for _, o := range ours {
		if o.ID == ownID {
			out = append(out, inheritedSchemaFinding{stack, o.ID, inheritedCollides})
			continue
		}
		if _, ok := byProv[o.ID]; !ok {
			out = append(out, inheritedSchemaFinding{stack, o.ID, inheritedNoSubject})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		return out[i].Reason < out[j].Reason
	})
	return out
}

// schemaDeclsOf вынимает список объявлений схем из произвольного узла значений.
func schemaDeclsOf(node any) []schemaDecl {
	list, ok := node.([]any)
	if !ok {
		return nil
	}
	out := make([]schemaDecl, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id, _ := m["id"].(string)
		url, _ := m["url"].(string)
		if strings.TrimSpace(id) == "" {
			continue
		}
		out = append(out, schemaDecl{ID: id, URL: url})
	}
	return out
}

// mergedValuesOfStack — действующие значения стека: умолчания чарта, затем
// профили слева направо, ровно как их получает helm. Возвращает ещё и объём
// прочитанного: «ноль находок» обязано быть отличимо от «ноль прочитанного».
func mergedValuesOfStack(t *testing.T, chain []string) (map[string]any, int, int) {
	t.Helper()
	files, bytesRead := 0, 0
	read := func(p string) map[string]any {
		raw, err := os.ReadFile(filepath.Clean(p)) // #nosec G304 -- путь из таблицы стеков собственного дерева
		if err != nil {
			t.Fatalf("профиль %s не читается: %v — предпосылка исчезла, а не дерево стало чистым", p, err)
		}
		files++
		bytesRead += len(raw)
		var tree map[string]any
		if err := yaml.Unmarshal(raw, &tree); err != nil {
			t.Fatalf("профиль %s не разбирается как YAML: %v", p, err)
		}
		return tree
	}
	merged := read(filepath.Join(umbrellaDir, "values.yaml"))
	for _, p := range chain {
		merged = mergeValues(merged, read(filepath.Join(umbrellaDir, p)))
	}
	return merged, files, bytesRead
}

// ownIdentitySchemaID — имя собственной схемы продукта, прочитанное ИЗ ШАБЛОНА,
// а не выписанное здесь: выписанное разошлось бы с шаблоном молча.
func ownIdentitySchemaID(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(identityConfigTemplate)) // #nosec G304 -- путь — константа собственного дерева
	if err != nil {
		t.Fatalf("объявление настроек личности не прочитано (%s): %v", identityConfigTemplate, err)
	}
	m := defaultSchemaIDDecl.FindStringSubmatch(string(raw))
	if m == nil {
		t.Fatalf("в %s не найдено объявление `default_schema_id` — имя собственной схемы "+
			"взять неоткуда, и любой вердикт о покрытии был бы вынесен неизвестно о чём",
			filepath.Base(identityConfigTemplate))
	}
	return strings.Trim(m[1], `"'`)
}

// ─────────────────────────────────────────────────────────────────────────────
// (I) ОБЪЯВЛЕНИЯ.

func TestIdentityInheritedSchemasAreDeclared(t *testing.T) {
	stacks := deployStacks(t)
	ownID := ownIdentitySchemaID(t)

	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)

	var (
		filesRead, bytesRead        int
		providerDecls, inheritedDcl int
		stacksWithProviderNode      int
		findings                    int
	)
	for _, name := range names {
		merged, f, b := mergedValuesOfStack(t, stacks[name])
		filesRead += f
		bytesRead += b

		// Предпосылка охвата: узел настроек поставщика обязан РЕЗОЛВИТЬСЯ хотя бы
		// у одного стека. Переименуют его — гейт перестанет читать и позеленеет
		// на вернувшемся дефекте; поэтому резолв считается отдельно от находок.
		if _, ok := lookup(merged, "kratos", "kratos", "config"); ok {
			stacksWithProviderNode++
		}

		provNode, _ := lookup(merged, "kratos", "kratos", "config", "identity", "schemas")
		oursNode, _ := lookup(merged, "global", "kacho", "identity", "inheritedSchemas")
		prov := schemaDeclsOf(provNode)
		ours := schemaDeclsOf(oursNode)
		providerDecls += len(prov)
		inheritedDcl += len(ours)

		for _, fnd := range adjudicateInheritedSchemas(name, prov, ours, ownID) {
			findings++
			switch fnd.Reason {
			case inheritedNotCovered:
				t.Errorf("стек %q: %s — схема %q. Наша секция `identity.schemas` ЗАМЕЩАЕТ "+
					"объявление поставщика целиком, поэтому строки личностей со схемой %q "+
					"перестают читаться (`500 invalid_configuration`). Объявите её "+
					"ссылкой на тот же узел YAML в global.kacho.identity.inheritedSchemas",
					name, fnd.Reason, fnd.ID, fnd.ID)
			case inheritedNoSubject:
				t.Errorf("стек %q: %s — схема %q. Объявление пережило свой предмет: "+
					"связка «якорь ↔ ссылка» разорвана, и следующая правка тела доедет "+
					"только до одной стороны. Снимите запись либо верните ссылку",
					name, fnd.Reason, fnd.ID)
			case inheritedBodyDrift:
				t.Errorf("стек %q: %s — схема %q. Два объявления одного имени разошлись; "+
					"процесс возьмёт то, что стоит ПОЗЖЕ, то есть наше. Тело обязано "+
					"приезжать ОДНИМ узлом YAML (якорь + ссылка), а не копией литерала",
					name, fnd.Reason, fnd.ID)
			case inheritedCollides:
				t.Errorf("стек %q: %s — имя %q. Двух схем под одним именем не бывает: "+
					"какое тело применится к живым строкам, решит порядок, а не решение",
					name, fnd.Reason, fnd.ID)
			}
		}
	}

	if len(names) == 0 || filesRead == 0 || bytesRead == 0 {
		t.Fatalf("перепись пуста (стеков %d, профилей %d, байт %d) — вердикта НЕТ: "+
			"«нарушений не найдено» здесь неотличимо от «ничего не прочитано»",
			len(names), filesRead, bytesRead)
	}
	if stacksWithProviderNode == 0 {
		t.Fatalf("узел настроек подчарта поставщика (`kratos.kratos.config`) не резолвится "+
			"НИ У ОДНОГО из %d стеков — гейт больше не читает ту сторону, которую сверяет, "+
			"и позеленел бы на вернувшемся дефекте", len(names))
	}

	t.Logf("перепись: стеков %d · профилей прочитано %d · байт %d · узел поставщика "+
		"резолвится у %d стеков · объявлений поставщика %d · унаследованных объявлений %d · "+
		"собственная схема %q · находок %d",
		len(names), filesRead, bytesRead, stacksWithProviderNode, providerDecls, inheritedDcl, ownID, findings)
	if providerDecls == 0 && inheritedDcl == 0 {
		t.Logf("ВНЕ ОХВАТА: ни один стек не объявляет унаследованных схем — свойство " +
			"выполняется пусто. Это НЕ поломка (пустая ведомость и есть цель), но и не " +
			"свидетельство: живые `schema_id` лежат в хранилище кластера, гейт дерева их " +
			"не видит by construction")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (II) РЕНДЕР. Объявление, которого шаблон не читает, — форма без содержания.

func TestIdentityInheritedSchemasReachTheRenderedConfig(t *testing.T) {
	stacks := deployStacks(t)
	ownID := ownIdentitySchemaID(t)

	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)

	var rendered, idsChecked, carried int
	for _, name := range names {
		merged, _, _ := mergedValuesOfStack(t, stacks[name])
		provNode, _ := lookup(merged, "kratos", "kratos", "config", "identity", "schemas")
		want := schemaDeclsOf(provNode)

		out, err := renderIdentitySubchart(t, stacks[name])
		if err != nil {
			t.Fatalf("стек %q: рендер отказал: %v\n%s", name, err, out)
		}
		rendered++
		cfg, _ := identityConfigOf(t, out)
		got := map[string]string{}
		if idNode, ok := cfg["identity"].(map[string]any); ok {
			for _, d := range schemaDeclsOf(idNode["schemas"]) {
				got[d.ID] = d.URL
			}
		}
		if len(got) == 0 {
			t.Fatalf("стек %q: в отрендеренных настройках нет НИ ОДНОЙ схемы — вердикта нет: "+
				"«унаследованной не найдено» неотличимо от «секции не найдено вовсе»", name)
		}
		if _, ok := got[ownID]; !ok {
			t.Errorf("стек %q: собственная схема %q в рендере отсутствует", name, ownID)
		}
		for _, w := range want {
			idsChecked++
			url, ok := got[w.ID]
			if !ok {
				t.Errorf("стек %q: схема %q объявлена слоем под нами, но в отрендеренных "+
					"настройках её НЕТ — значит объявление профиля шаблон не читает",
					name, w.ID)
				continue
			}
			if url != w.URL {
				t.Errorf("стек %q: у схемы %q рендер несёт другое тело (%s против %s)",
					name, w.ID, shortURL(url), shortURL(w.URL))
				continue
			}
			carried++
		}
	}

	if rendered == 0 {
		t.Fatalf("не отрендерено НИ ОДНОГО стека из %d — вердикта нет", len(names))
	}
	t.Logf("перепись рендера: стеков отрендерено %d · чужих схем сверено %d · доехало %d",
		rendered, idsChecked, carried)
}
