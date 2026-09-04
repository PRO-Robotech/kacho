// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Инъекция гейта «пример ответа несёт ровно то множество полей, которое называет
// таблица».
//
// Вход НАСТОЯЩИЙ по форме и синтетический по содержанию: страница собирается в
// t.TempDir(), поэтому вердикт не зависит ни от состояния репозитория, ни от
// порядка прогонов. Каждая ось проверяется В ОБЕ СТОРОНЫ, и законный близнец
// стоит ПЕРВЫМ — иначе отрицание зеленело бы на анализаторе, находящем
// нарушение в любом дереве.

// cdefPage собирает страницу ресурса: таблица полей, блок чтения по
// идентификатору с обещанием и примерами, блок списочного чтения.
//
// Параметры названы порознь, потому что каждая ось инъекции двигает РОВНО ОДИН
// факт против положительного близнеца.
type cdefPage struct {
	// tableRows — первые ячейки строк таблицы, как они стоят на странице.
	tableRows []string
	// promise — несёт ли страница обещание «ответ несёт все поля ресурса».
	promise bool
	// full — тело полного примера (форма компонента с шаблонной строкой).
	full string
	// fragment — тело фрагмента (форма ограды). Пусто — фрагмента нет.
	fragment string
	// listExample — тело примера в блоке СПИСОЧНОГО чтения. Пусто — примера нет.
	listExample string
}

func cdefRender(p cdefPage) string {
	var b strings.Builder
	b.WriteString("# Thing\n\n## Поля ресурса\n\n<table>\n")
	b.WriteString("  <thead><tr><th>Поле</th><th>Описание</th></tr></thead>\n  <tbody>\n")
	for _, row := range p.tableRows {
		b.WriteString("    <tr><td>" + row + "</td><td>проза</td></tr>\n")
	}
	b.WriteString("  </tbody>\n</table>\n\n## Методы\n\n")
	b.WriteString(`### Get — получить

<ApiOperation method="GET" endpoint="/demo/v1/things/{thing_id}">

`)
	if p.promise {
		b.WriteString("Ответ несёт **все** поля ресурса, в том числе пустые.\n\n")
	} else {
		b.WriteString("Ответ несёт часть полей ресурса — остальные смотрите в таблице выше.\n\n")
	}
	b.WriteString("<CodeBlock language=\"json\">\n  {dedent`\n" + p.full + "\n  `}\n</CodeBlock>\n\n")
	if p.fragment != "" {
		b.WriteString("```json\n" + p.fragment + "\n```\n\n")
	}
	b.WriteString("</ApiOperation>\n\n### List — каталог\n\n")
	b.WriteString("<ApiOperation method=\"GET\" endpoint=\"/demo/v1/things\">\n\n")
	if p.listExample != "" {
		b.WriteString("```json\n" + p.listExample + "\n```\n\n")
	}
	b.WriteString("</ApiOperation>\n")
	return b.String()
}

func cdefWriteTree(t *testing.T, page string) ClientDocsExampleFieldsOptions {
	t.Helper()
	root := t.TempDir()
	apiDir := filepath.Join(root, "services", "demo", "docs", "content", "api")
	if err := os.MkdirAll(apiDir, 0o750); err != nil {
		t.Fatalf("каталог страниц: %v", err)
	}
	if err := os.WriteFile(filepath.Join(apiDir, "thing.mdx"), []byte(page), 0o600); err != nil {
		t.Fatalf("страница: %v", err)
	}
	return ClientDocsExampleFieldsOptions{Root: root, DocRoots: []string{"services"}}
}

// cdefRun — вердикт анализатора о синтетическом дереве.
func cdefRun(t *testing.T, p cdefPage) ([]ClientDocsExampleFieldsFinding, ClientDocsExampleFieldsCensus, error) {
	t.Helper()
	var log strings.Builder
	f, c, err := AuditClientDocsExampleFields(cdefWriteTree(t, cdefRender(p)), &log)
	t.Log(strings.TrimSpace(log.String()))
	return f, c, err
}

// cdefSound — ЗАКОННЫЙ близнец: таблица и полный пример называют одно множество,
// рядом стоят фрагмент и списочный пример, которые судиться не должны.
func cdefSound() cdefPage {
	return cdefPage{
		tableRows: []string{
			"<code>id</code>",
			"<code>name</code>",
			"<code>health</code>",
			// Одна строка — ДВА поля. Счёт по строкам потерял бы второе.
			"<code>createdAt</code> / <code>updatedAt</code>",
			// Поле вложенного сообщения: ключом верхнего уровня не приходит.
			"<code>referrer.type</code>",
		},
		promise: true,
		full: `    {
      "id": "thg1",
      "name": "demo",
      "health": "HEALTHY",
      "createdAt": "2026-09-04T00:00:00Z",
      "updatedAt": "2026-09-04T00:00:00Z"
    }`,
		fragment:    `{ "health": "DEGRADED" }`,
		listExample: `{ "things": [] }`,
	}
}

func cdefAssertSilent(t *testing.T, name string, p cdefPage) ClientDocsExampleFieldsCensus {
	t.Helper()
	f, c, err := cdefRun(t, p)
	if err != nil {
		t.Fatalf("%s: анализатор не отработал: %v", name, err)
	}
	if len(f) != 0 {
		t.Fatalf("%s: законный вход дал %d находок: %v", name, len(f), f)
	}
	return c
}

func cdefAssertFinds(t *testing.T, name string, p cdefPage, want ...string) {
	t.Helper()
	f, _, err := cdefRun(t, p)
	if err != nil {
		t.Fatalf("%s: анализатор не отработал: %v", name, err)
	}
	if len(f) != len(want) {
		t.Fatalf("%s: находок %d, ожидалось %d: %v", name, len(f), len(want), f)
	}
	for i, w := range want {
		if !strings.Contains(f[i].String(), w) {
			t.Fatalf("%s: находка %d не называет %q: %s", name, i, w, f[i].String())
		}
	}
}

// TestClientDocsExampleFieldsInjection — способность падать и молчать.
func TestClientDocsExampleFieldsInjection(t *testing.T) {
	// ОСЬ 0 (положительный контроль, первым). Согласованная страница молчит, и
	// перепись доказывает, что обход не был пуст: без неё молчание было бы
	// достижимо анализатором, который ничего не читает.
	c := cdefAssertSilent(t, "законный близнец", cdefSound())
	if c.PagesJudged != 1 {
		t.Fatalf("судимых страниц %d, ожидалась 1 — обход пуст", c.PagesJudged)
	}
	if c.TableFields != 5 {
		t.Fatalf("полей таблицы %d, ожидалось 5 (пять строк: одна называет ДВА имени, "+
			"одна — вложенное, которое ключом верхнего уровня не приходит) — разбор "+
			"таблицы неверен", c.TableFields)
	}
	if c.ExampleFields != 5 {
		t.Fatalf("полей примера %d, ожидалось 5 — полным взят не тот пример", c.ExampleFields)
	}
	if c.ExamplesParsed != 2 {
		t.Fatalf("примеров блока чтения разобрано %d, ожидалось 2 — распознаватель знает "+
			"не все формы записи примера", c.ExamplesParsed)
	}

	// ОСЬ 1. Поле выпало из примера — сторона, ради которой гейт заведён.
	p := cdefSound()
	p.full = strings.ReplaceAll(p.full, "      \"health\": \"HEALTHY\",\n", "")
	cdefAssertFinds(t, "поле выпало из примера", p, "таблица называет поле health, пример его не несёт")

	// ОСЬ 2. Поле в примере есть, таблица его не называет — ОБРАТНАЯ сторона.
	// Сверка по ЧИСЛУ её не видит, если одновременно выпало другое поле, —
	// поэтому ось 3 ниже двигает оба факта сразу.
	p = cdefSound()
	p.tableRows = p.tableRows[:len(p.tableRows)-4] // остаётся только id
	cdefAssertFinds(t, "поле не названо таблицей", p,
		"пример несёт поле createdAt, таблица его не называет",
		"пример несёт поле health, таблица его не называет",
		"пример несёт поле name, таблица его не называет",
		"пример несёт поле updatedAt, таблица его не называет")

	// ОСЬ 3. ЧИСЛА СОВПАДАЮТ, множества — нет. Ровно этот вход проходит сверку
	// по числу и обязан быть находкой здесь.
	p = cdefSound()
	p.full = strings.ReplaceAll(p.full, `"health": "HEALTHY"`, `"purpose": ""`)
	f, _, err := cdefRun(t, p)
	if err != nil {
		t.Fatalf("совпадающие числа: анализатор не отработал: %v", err)
	}
	if len(f) != 2 {
		t.Fatalf("совпадающие числа: находок %d, ожидалось 2 (обе стороны): %v", len(f), f)
	}
	if !strings.Contains(f[0].String(), "таблица называет поле health, пример его не несёт") ||
		!strings.Contains(f[1].String(), "пример несёт поле purpose, таблица его не называет") {
		t.Fatalf("совпадающие числа: названы не обе стороны: %v", f)
	}

	// ОСЬ 4. Строка таблицы с ДВУМЯ именами. Разбор, берущий первое имя строки,
	// потеряет `updatedAt` — и объявит его лишним в примере. Такой предикат уже
	// применялся вживую и дал ложное «двустороннее расхождение».
	p = cdefSound()
	p.tableRows[3] = "<code>createdAt</code>"
	cdefAssertFinds(t, "второе имя строки", p,
		"пример несёт поле updatedAt, таблица его не называет")

	// ОСЬ 5. Имя вложенного сообщения ключом верхнего уровня не приходит и
	// находкой быть не должно (близнец оси — в контроле выше, где `referrer.type`
	// стоит и молчит). Здесь двигаем ОДИН факт: снимаем точку.
	p = cdefSound()
	p.tableRows[4] = "<code>referrer</code>"
	cdefAssertFinds(t, "имя без точки", p,
		"таблица называет поле referrer, пример его не несёт")

	// ОСЬ 6. Обещания нет — страница не судится (её пример вправе быть
	// фрагментом). Но круг судимых при этом ПУСТ, и пустой круг есть ОТКАЗ:
	// снявший обещание получает красное, а не тишину.
	p = cdefSound()
	p.promise = false
	p.full = strings.ReplaceAll(p.full, "      \"health\": \"HEALTHY\",\n", "")
	f, c, err = cdefRun(t, p)
	if err == nil {
		t.Fatalf("страница без обещания: анализатор отработал успехом — пустой круг судимых "+
			"прочитан как «нарушений нет» (находок %d)", len(f))
	}
	if !strings.Contains(err.Error(), "ни одна страница не обещает") {
		t.Fatalf("страница без обещания: отказ не называет предмет: %v", err)
	}
	if len(f) != 0 {
		t.Fatalf("страница без обещания: находок %d, ожидалось 0 — страница судилась", len(f))
	}
	if c.PagesWithRead != 1 {
		t.Fatalf("страница без обещания: страниц с примером %d, ожидалась 1 — обход пуст "+
			"по другой причине, и отказ выше беспредметен", c.PagesWithRead)
	}
	if len(c.PagesUnjudged) != 1 {
		t.Fatalf("страница без обещания: перепись не назвала её в несудимых: %v", c.PagesUnjudged)
	}

	// ОСЬ 7. ФОРМА ЗАПИСИ примера. Полный пример роли записан компонентом, а
	// фрагменты — оградой. Распознаватель, знающий одну форму, взял бы полным
	// фрагмент и молчал бы на любом расхождении. Здесь полный пример уходит в
	// форму ограды, а фрагмент — в форму компонента: множество полей то же,
	// значит вердикт обязан остаться прежним.
	p = cdefSound()
	p.full, p.fragment = `    { "health": "DEGRADED" }`,
		`{
  "id": "thg1",
  "name": "demo",
  "health": "HEALTHY",
  "createdAt": "2026-09-04T00:00:00Z",
  "updatedAt": "2026-09-04T00:00:00Z"
}`
	c = cdefAssertSilent(t, "формы записи переставлены", p)
	if c.ExampleFields != 5 {
		t.Fatalf("формы записи переставлены: полей примера %d, ожидалось 5 — распознаватель "+
			"знает не обе формы", c.ExampleFields)
	}

	// ОСЬ 8. Пустой обход. Дерево без страниц обязано дать ОТКАЗ, а не «находок
	// ноль»: иначе «ноль находок» неотличимо от «прочитано ноль».
	empty := ClientDocsExampleFieldsOptions{Root: t.TempDir(), DocRoots: []string{"services"}}
	if _, _, err = AuditClientDocsExampleFields(empty, nil); err == nil {
		t.Fatal("пустое дерево: анализатор отработал успехом — обход пуст, вердикт беспредметен")
	}
}
