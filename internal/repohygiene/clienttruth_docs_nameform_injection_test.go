// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/validate/nameform"
)

// Инъекция для гейта формы имени — В ОБЕ СТОРОНЫ.
//
// Дефекты возвращаются НАСТОЯЩИЕ — те самые, что закрыл коммит «форма имени и
// написание ресурса приведены к дереву»: `permissive`-запись словаря, питавшая
// восемь страниц сразу, и форма, показанная рядом таблицы записью HTML-сущностей
// (именно так расхождение выглядит в MDX и именно её не видит наивный поиск по
// обычной записи).
//
// Каждая проба меняет ОДНО против контроля (п.2в §«Гейт на класс»).

// docsNameFormFixture — синтетическое дерево клиентской поверхности: страница с
// таблицей полей, страница с прозой и словарь описаний.
type docsNameFormFixture struct {
	dictName  string // регулярка в записи `name` словаря
	tableName string // регулярка в ряду таблицы, ЗАПИСЬЮ СУЩНОСТЯМИ
	proseName string // регулярка в абзаце про имя
	// Законные соседи, на которых гейт обязан молчать: их регулярки другие и
	// таковыми и должны остаться.
	tableLabels string // ряд таблицы про labels
	proseNoName string // абзац БЕЗ упоминания имени
	dictOther   string // запись словаря с ключом namespaceId
}

// docsNameFormEntityEncode — запись регулярки HTML-сущностями, как её пишут в MDX.
func docsNameFormEntityEncode(s string) string {
	return strings.NewReplacer(
		"[", "&#91;", "]", "&#93;", "{", "&#123;", "}", "&#125;",
	).Replace(s)
}

// docsNameFormControlFixture — исправное дерево: все три формы записи несут канон.
func docsNameFormControlFixture() docsNameFormFixture {
	return docsNameFormFixture{
		dictName:    nameform.Form,
		tableName:   nameform.Form,
		proseName:   nameform.Form,
		tableLabels: `^[a-z][-_./@a-z0-9]{0,62}$`,
		proseNoName: `^[0-9a-f]{2}(:[0-9a-f]{2}){5}$`,
		dictOther:   `^ns-[0-9a-hjkmnp-tv-z]{17}$`,
	}
}

func writeDocsNameFormTree(t *testing.T, f docsNameFormFixture) string {
	t.Helper()
	root := t.TempDir()
	mk := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	// Словарь: одна запись питает все страницы сайта — поэтому она и есть
	// главное место расхождения.
	mk("services/vpc/docs/src/constants/dictionary.ts",
		"export const DICTIONARY = {\n"+
			"  id: { short: 'Идентификатор ресурса' },\n"+
			"  name: { short: 'Имя ресурса: DNS label по RFC 1123 — "+f.dictName+" (1..63)' },\n"+
			"  namespaceId: { short: 'Идентификатор пространства имён "+f.dictOther+"' },\n"+
			"} as const\n")

	// Ряд таблицы — записью СУЩНОСТЯМИ. Соседний ряд про labels несёт свою,
	// законно другую регулярку и стоит через строку: он и есть проба того, что
	// область маркера у ряда — строка, а не абзац.
	mk("services/vpc/docs/content/api/network.mdx",
		"# Network\n\n<table>\n  <tbody>\n"+
			"    <tr><td><code>name</code></td><td>string</td><td>regex <code>"+
			docsNameFormEntityEncode(f.tableName)+"</code> — DNS label</td></tr>\n"+
			"    <tr><td><code>labels</code></td><td>map</td><td>key <code>"+
			docsNameFormEntityEncode(f.tableLabels)+"</code></td></tr>\n"+
			"  </tbody>\n</table>\n")

	// Проза: маркер абзаца стоит строкой выше регулярки. Ниже — абзац БЕЗ
	// упоминания имени со своей регуляркой: он обязан остаться вне предмета.
	mk("services/vpc/docs/content/api/gateway.mdx",
		"# Gateway\n\n"+
			"Имя шлюза подчиняется общей форме платформы — DNS label по RFC 1123,\n"+
			"`"+f.proseName+"`: строчные латинские буквы, цифры и дефис.\n\n"+
			"MAC-адрес интерфейса выдаётся сервером в форме\n"+
			"`"+f.proseNoName+"` (lowercase, через двоеточие).\n")

	return root
}

func docsNameFormInjectionCensus(t *testing.T, f docsNameFormFixture) docsNameFormCensus {
	t.Helper()
	c, err := collectDocsNameForm(mustSyntheticTree(t, writeDocsNameFormTree(t, f)))
	if err != nil {
		t.Fatalf("обход: %v", err)
	}
	// Инъекция беспредметна, если фикстура не прочитана: молчание непрочитанного
	// гейта неотличимо от молчания исправного.
	if c.ContentDocs != 2 || c.DictFiles != 1 {
		t.Fatalf("фикстура не прочитана: страниц %d, словарей %d", c.ContentDocs, c.DictFiles)
	}
	if len(c.Claims) != 3 {
		t.Fatalf("признано утверждениями о форме имени %d вместо 3: %+v", len(c.Claims), c.Claims)
	}
	return c
}

func TestDocsNameFormGateInjection(t *testing.T) {
	t.Run("КОНТРОЛЬ: все три формы записи несут канон — гейт молчит", func(t *testing.T) {
		c := docsNameFormInjectionCensus(t, docsNameFormControlFixture())
		for _, s := range []docsNameFormShape{
			docsNameFormShapeTable, docsNameFormShapeDict, docsNameFormShapeProse,
		} {
			if c.ShapeCount(s) != 1 {
				t.Fatalf("форма записи %q прочитана %d раз вместо 1", s, c.ShapeCount(s))
			}
		}
		if c.RegexEntity == 0 {
			t.Fatal("запись сущностями не прочитана — контроль не покрывает вторую кодировку")
		}
		if f := docsNameFormFindings(c); len(f) != 0 {
			t.Errorf("гейт краснеет на исправном дереве: %v", f)
		}
	})

	t.Run("ДЕФЕКТ в СЛОВАРЕ: одна запись питает все страницы", func(t *testing.T) {
		fx := docsNameFormControlFixture()
		fx.dictName = `^[a-z]([-a-z0-9]{0,61}[a-z0-9])?$` // цифра первым знаком запрещена
		c := docsNameFormInjectionCensus(t, fx)
		f := docsNameFormFindings(c)
		if len(f) != 1 {
			t.Fatalf("ожидалась одна находка, получено %d: %v", len(f), f)
		}
		t.Logf("находка: %s", f[0])
		if !strings.Contains(f[0], "dictionary.ts") || !strings.Contains(f[0], "словарь") {
			t.Errorf("находка не называет ни координаты, ни формы записи: %s", f[0])
		}
		if !strings.Contains(f[0], nameform.Form) {
			t.Errorf("находка не называет действующую форму — читателю нечем чинить: %s", f[0])
		}
	})

	t.Run("ЛОВУШКА КОДИРОВКИ: расхождение записано HTML-сущностями", func(t *testing.T) {
		// Живое расхождение дерева выглядит именно так. Наивный распознаватель,
		// читающий только обычную запись, объявил бы «ноль находок».
		fx := docsNameFormControlFixture()
		fx.tableName = `^[a-z][-a-z0-9]{2,62}$`
		c := docsNameFormInjectionCensus(t, fx)
		f := docsNameFormFindings(c)
		if len(f) != 1 {
			t.Fatalf("ожидалась одна находка, получено %d: %v", len(f), f)
		}
		t.Logf("находка: %s", f[0])
		if !strings.Contains(f[0], "network.mdx:5") {
			t.Errorf("находка не называет координату: %s", f[0])
		}
		if !strings.Contains(f[0], "&#91;") {
			t.Errorf("находка показывает не то, что написано на странице — "+
				"читатель не найдёт это поиском: %s", f[0])
		}
	})

	t.Run("ДЕФЕКТ в ПРОЗЕ: абзац про имя показывает чужую форму", func(t *testing.T) {
		fx := docsNameFormControlFixture()
		fx.proseName = `^([a-zA-Z]([-_a-zA-Z0-9]{0,61}[a-zA-Z0-9])?)?$`
		c := docsNameFormInjectionCensus(t, fx)
		f := docsNameFormFindings(c)
		if len(f) != 1 {
			t.Fatalf("ожидалась одна находка, получено %d: %v", len(f), f)
		}
		t.Logf("находка: %s", f[0])
		if !strings.Contains(f[0], "gateway.mdx") || !strings.Contains(f[0], "проза") {
			t.Errorf("находка не называет ни координаты, ни формы записи: %s", f[0])
		}
	})

	t.Run("ЗАКОННЫЙ БЛИЗНЕЦ: соседний ряд таблицы про labels", func(t *testing.T) {
		// Ряд про метки стоит СТРОКОЙ НИЖЕ ряда про имя и несёт законно другую
		// регулярку. Абзацная область маркера объявила бы его находкой.
		fx := docsNameFormControlFixture()
		fx.tableLabels = `^[a-z][-_./@a-z0-9]{0,30}$` // другая, и это законно
		c := docsNameFormInjectionCensus(t, fx)
		if f := docsNameFormFindings(c); len(f) != 0 {
			t.Errorf("форма ключа метки объявлена формой имени: %v", f)
		}
	})

	t.Run("ЗАКОННЫЙ БЛИЗНЕЦ: абзац, не называющий имя", func(t *testing.T) {
		fx := docsNameFormControlFixture()
		fx.proseNoName = `^[0-9a-f]{2}(:[0-9a-f]{2}){7}$` // другая форма MAC
		c := docsNameFormInjectionCensus(t, fx)
		if f := docsNameFormFindings(c); len(f) != 0 {
			t.Errorf("регулярка абзаца, не называющего имя, объявлена формой имени: %v", f)
		}
	})

	t.Run("ЗАКОННЫЙ БЛИЗНЕЦ: ключ словаря namespaceId — не поле имени", func(t *testing.T) {
		// Предикат по префиксу («ключ начинается на name») забрал бы этот ключ в
		// предмет и дал бы ложную находку. За `name` обязан идти разделитель или
		// заглавная.
		fx := docsNameFormControlFixture()
		fx.dictOther = `^ns-[0-9A-Z]{17}$`
		c := docsNameFormInjectionCensus(t, fx)
		if f := docsNameFormFindings(c); len(f) != 0 {
			t.Errorf("ключ namespaceId прочитан как поле имени: %v", f)
		}
		if !docsNameFormIsNameKey("name") || !docsNameFormIsNameKey("nameGateway") ||
			!docsNameFormIsNameKey("NAME_RULES") {
			t.Error("предикат ключа не узнаёт законные формы имени ключа")
		}
		if docsNameFormIsNameKey("namespaceId") || docsNameFormIsNameKey("names") ||
			docsNameFormIsNameKey("") {
			t.Error("предикат ключа забирает в предмет чужие ключи")
		}
	})

	t.Run("ВЕДОМОСТЬ: запись без предмета — находка", func(t *testing.T) {
		// Ведомость обязана истекать сама. Проба подаёт запись, которой нечего
		// исключать, и требует, чтобы гейт её назвал.
		saved := docsNameFormExemptions
		t.Cleanup(func() { docsNameFormExemptions = saved })
		docsNameFormExemptions = []docsNameFormExemption{{
			File:  "services/registry/docs/content/api/repository.mdx",
			Regex: `^[a-z0-9]+([._-][a-z0-9]+)*$`,
			Why:   "OCI-грамматика имени репозитория",
		}}
		c := docsNameFormInjectionCensus(t, docsNameFormControlFixture())
		f := docsNameFormFindings(c)
		if len(f) != 1 {
			t.Fatalf("ожидалась одна находка, получено %d: %v", len(f), f)
		}
		t.Logf("находка: %s", f[0])
		if !strings.Contains(f[0], "ведомость исключений") ||
			!strings.Contains(f[0], "repository.mdx") {
			t.Errorf("находка не называет ни ведомости, ни записи: %s", f[0])
		}
	})

	t.Run("ВЕДОМОСТЬ: запись с предметом молчит и снимает находку", func(t *testing.T) {
		// Обратная половина: пока предмет есть, запись работает и лишнего не
		// прощает. Без этой пробы «ведомость молчит» было бы неотличимо от
		// «ведомость не читается».
		fx := docsNameFormControlFixture()
		fx.proseName = `^[a-z0-9]+([._-][a-z0-9]+)*$`
		saved := docsNameFormExemptions
		t.Cleanup(func() { docsNameFormExemptions = saved })
		docsNameFormExemptions = []docsNameFormExemption{{
			File:  "services/vpc/docs/content/api/gateway.mdx",
			Regex: `^[a-z0-9]+([._-][a-z0-9]+)*$`,
			Why:   "синтетический предмет пробы",
		}}
		c := docsNameFormInjectionCensus(t, fx)
		if f := docsNameFormFindings(c); len(f) != 0 {
			t.Errorf("запись ведомости с живым предметом не сняла находку: %v", f)
		}

		// И тут же — что ведомость не прощает лишнего: другое расхождение в том
		// же файле остаётся находкой.
		fx2 := fx
		fx2.dictName = `^[a-z]([-a-z0-9]{0,61}[a-z0-9])?$`
		c2 := docsNameFormInjectionCensus(t, fx2)
		f2 := docsNameFormFindings(c2)
		if len(f2) != 1 || !strings.Contains(f2[0], "dictionary.ts") {
			t.Errorf("ведомость простила расхождение, которого не называла: %v", f2)
		}
	})

	t.Run("ПУСТОЙ ОБХОД отличим от «нарушений нет»", func(t *testing.T) {
		c, err := collectDocsNameForm(mustSyntheticTree(t, t.TempDir()))
		if err != nil {
			t.Fatalf("обход: %v", err)
		}
		if c.Files != 0 || c.Regexes != 0 || len(c.Claims) != 0 {
			t.Fatalf("пустое дерево дало непустую перепись: %+v", c)
		}
	})
}
