// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
	"github.com/PRO-Robotech/kacho/pkg/validate/nameform"
)

// Форма имени, показанная клиенту, совпадает с формой, которую держит код.
//
// ПРЕДМЕТ. Регулярка на странице — не пояснение, а ПРАВИЛО, по которому клиент
// сочиняет имя. Разойдясь с деревом, она отправляет чинить имя по правилу,
// которого нет: годное имя объявляется негодным (страница запрещает цифру первым
// знаком, а сервис её принимает) либо негодное объявляется годным (страница
// разрешает подчёркивание, а сервис отвергает — и узнаётся это только отказом
// после отправки).
//
// ФОРМА ОДНА И ВЫВОДИТСЯ, А НЕ ВЫПИСЫВАЕТСЯ. Эталон приходит из
// `pkg/validate/nameform.Form` — из ЕДИНСТВЕННОГО объявления, которое читают и
// домены, и валидаторы. Копия эталона здесь завела бы второе место об одном
// предмете, то есть ровно тот класс, от которого этот гейт и защищает: копия
// пережила бы смену канона и краснела бы на верных страницах.
//
// ИСКАТЬ НАДО В ДВУХ МЕСТАХ, И ВТОРОЕ ВАЖНЕЕ ПЕРВОГО. Страницы сайта берут
// описание поля из словаря `docs/src/constants/dictionary.ts`, поэтому ОДНА
// запись словаря питает восемь страниц сразу. Гейт, читающий только
// `content/**`, увидел бы страницы исправленными и не заметил бы, что источник
// их текста разошёлся с деревом.
//
// ТРИ ЗАКОННЫХ ФОРМЫ ЗАПИСИ ПРЕДМЕТА — и форма, о которой распознаватель не
// знает, есть НЕВИДИМОСТЬ, а не редкость (п.7 §«Гейт на класс»):
//
//	ряд таблицы  <tr><td><code>name</code></td>…<code>^…$</code>…</tr>
//	словарь      name: { short: '… ^…$ …' }   ·   const NAME_RULES = ['regex ^…$ …']
//	проза        «Имя Gateway подчиняется общей форме — `^…$`: строчные латинские…»
//
// И ДВЕ ЗАКОННЫХ КОДИРОВКИ. В MDX фигурные и квадратные скобки внутри разметки
// экранируются HTML-сущностями, поэтому та же самая регулярка встречается и как
// `^[a-z0-9]…$`, и как `^&#91;a-z0-9&#93;…$`. Живое расхождение дерева записано
// ИМЕННО сущностями — распознаватель, читающий только обычную запись, объявил бы
// «ноль находок», и это означало бы «ноль прочитанного».
//
// ОБЛАСТЬ МАРКЕРА РАЗНАЯ У РАЗНЫХ ФОРМ, и это решение, а не недосмотр:
//
//   - ряд таблицы судится ТОЛЬКО по своей первой ячейке. Абзацная область
//     сделала бы предметом соседний ряд про MAC-адрес — он стоит через три
//     строки после ряда имени и несёт свою, законно другую регулярку;
//   - запись словаря — по объемлющему идентификатору (`name`, `nameGateway`,
//     `NAME_RULES`), потому что регулярка стоит внутри значения, а не рядом с
//     полем. Ключ `namespaceId` под это НЕ подпадает: за `name` обязан идти
//     разделитель или заглавная;
//   - проза — по АБЗАЦУ (до пустой строки, не далее пяти строк назад). Маркер,
//     взятый по файлу, объявил бы предметом любую регулярку страницы, где слово
//     «имя» вообще встречается.
//
// ЧЕМ ЭТОТ ГЕЙТ НЕ ЯВЛЯЕТСЯ. Он судит РЕГУЛЯРКУ, а не прозу вокруг неё: длина
// словами («3–63 символа»), область уникальности и поведение пустой строки —
// другие утверждения, у них другой источник истины и им нужен свой предикат. Он
// не читает инженерную часть сайтов (`docs/engineering/**`): сборка её не
// публикует, клиент её не видит, и требование к ней было бы требованием к
// другому предмету. Он не читает `docs/src/utils/**`: там код сайта, а разбор
// разметки регулярками — не утверждение о продукте.

var (
	// Регулярка-литерал: якорь, тело без пробелов и разметки, якорь.
	// Нижняя граница в три знака отсекает `^…$` — многоточие-подстановку из
	// прозы, которая регуляркой не является.
	docsNameFormRegexRe = regexp.MustCompile(`\^[^\s` + "`" + `"'|<>]{3,}\$`)

	// Первая ячейка ряда таблицы — с оборачивающим <code> и без него.
	docsNameFormFirstCellRe = regexp.MustCompile(`<td[^>]*>\s*(?:<code>)?\s*([^<]*?)\s*(?:</code>)?\s*</td>`)

	// Объявление идентификатора в словаре: `name:`, `const NAME_RULES =`.
	docsNameFormIdentRe = regexp.MustCompile(`^\s*(?:export\s+)?(?:const\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*[:=]`)

	// Маркер абзаца: страница называет предметом ИМЯ.
	docsNameFormProseMarkerRe = regexp.MustCompile(`(?i)(имя|имени|имена|имён|именем|именован|<code>name</code>|` + "`name`" + `)`)

	// Числовые и именованные сущности MDX.
	docsNameFormNumEntityRe = regexp.MustCompile(`&#(\d{1,5});`)
)

// docsNameFormProseWindow — сколько строк абзаца читается назад от регулярки.
//
// Пять, а не «до начала файла»: маркер, взятый по файлу, объявил бы предметом
// любую регулярку страницы, где слово «имя» вообще встречается. Обход всё равно
// останавливается на пустой строке — граница абзаца сильнее числа.
const docsNameFormProseWindow = 5

// docsNameFormExemption — законное расхождение формы.
//
// Ведомость обязана ИСТЕКАТЬ САМА: запись, которой больше нечего исключать, —
// находка, а не безобидный остаток (п.5 §«Гейт на класс»). Ключ — пара
// «файл + раскодированная регулярка», а не файл: запись по файлу сняла бы
// наблюдение со всей страницы, включая расхождения, о которых никто не решал.
//
// СЕГОДНЯ ЗАПИСЕЙ НОЛЬ, и это замер, а не забывчивость. Registry, чья форма
// имени репозитория действительно иная (OCI-грамматика, точка допустима),
// регулярки на клиентских страницах НЕ публикует вовсе — он описывает имя
// прозой. Заводить ему запись значило бы завести исключение без предмета, то
// есть находку. Опубликует свою форму — запись заводится тогда и вместе с
// предметом.
type docsNameFormExemption struct {
	File  string // путь от корня дерева
	Regex string // регулярка в РАСКОДИРОВАННОМ виде
	Why   string // почему расхождение законно
}

var docsNameFormExemptions []docsNameFormExemption

// docsNameFormShape — в какой из трёх форм записано утверждение.
type docsNameFormShape string

const (
	docsNameFormShapeTable docsNameFormShape = "таблица"
	docsNameFormShapeDict  docsNameFormShape = "словарь"
	docsNameFormShapeProse docsNameFormShape = "проза"
)

// docsNameFormClaim — одно показанное клиенту утверждение о форме имени.
type docsNameFormClaim struct {
	File   string
	Line   int
	Raw    string // как записано на странице
	Regex  string // раскодированное
	Shape  docsNameFormShape
	Entity bool // записано HTML-сущностями
}

// docsNameFormCensus — объём осмотренного.
type docsNameFormCensus struct {
	Files       int
	ContentDocs int
	DictFiles   int
	Regexes     int // всего регулярок прочитано
	RegexEntity int // из них записанных сущностями
	Claims      []docsNameFormClaim
}

// ShapeCount — сколько утверждений в каждой форме записи.
func (c docsNameFormCensus) ShapeCount(s docsNameFormShape) int {
	var n int
	for _, cl := range c.Claims {
		if cl.Shape == s {
			n++
		}
	}
	return n
}

// docsNameFormDecodeEntities — раскодирование числовых и именованных сущностей MDX.
//
// Без него `^&#91;a-z&#93;…$` не сравнить с `^[a-z]…$`: это ОДНА регулярка,
// записанная двумя законными способами, и распознаватель, знающий один, выводит
// из-под наблюдения всё написанное вторым.
func docsNameFormDecodeEntities(s string) string {
	if !strings.Contains(s, "&") {
		return s
	}
	s = docsNameFormNumEntityRe.ReplaceAllStringFunc(s, func(m string) string {
		var code int
		if _, err := fmt.Sscanf(m, "&#%d;", &code); err != nil || code <= 0 || code > 0x10FFFF {
			return m
		}
		return string(rune(code))
	})
	r := strings.NewReplacer(
		"&lt;", "<", "&gt;", ">", "&quot;", `"`, "&apos;", "'", "&amp;", "&",
	)
	return r.Replace(s)
}

// docsNameFormUnescapeMarkdown — снятие экранирования разметки в имени поля
// (`mac\_address` — это `mac_address`, а не другое поле).
func docsNameFormUnescapeMarkdown(s string) string {
	return strings.NewReplacer(`\_`, "_", `\-`, "-", `\.`, ".", `\*`, "*").Replace(s)
}

// docsNameFormIsNameKey — идентификатор словаря именует ПОЛЕ ИМЕНИ.
//
// `name`, `nameGateway`, `NAME_RULES` — да; `namespaceId` — нет: за `name`
// обязан идти разделитель или заглавная, иначе предикат по префиксу забирал бы
// в предмет чужие ключи и первый же ложный срабат снял бы гейт.
func docsNameFormIsNameKey(id string) bool {
	if strings.EqualFold(id, "name") {
		return true
	}
	if len(id) > 4 && strings.EqualFold(id[:4], "name") {
		c := id[4]
		return c == '_' || (c >= 'A' && c <= 'Z')
	}
	return false
}

// docsNameFormTableFieldOf — поле, которому посвящён ряд таблицы; "" — строка
// рядом таблицы не является.
func docsNameFormTableFieldOf(line string) (string, bool) {
	if !strings.Contains(line, "<tr") {
		return "", false
	}
	m := docsNameFormFirstCellRe.FindStringSubmatch(line)
	if m == nil {
		return "", true
	}
	return docsNameFormUnescapeMarkdown(strings.TrimSpace(m[1])), true
}

// docsNameFormProseNamesTheField — абзац вокруг строки называет предметом имя.
func docsNameFormProseNamesTheField(lines []string, idx int) bool {
	for i := idx; i >= 0 && i > idx-docsNameFormProseWindow; i-- {
		if i != idx && strings.TrimSpace(lines[i]) == "" {
			return false // граница абзаца сильнее окна
		}
		if docsNameFormProseMarkerRe.MatchString(lines[i]) {
			return true
		}
	}
	return false
}

// collectDocsNameForm — что показано клиенту про форму имени.
//
// Состав дерева приходит СОСТАВЛЕННЫМ (`treecorpus.Tree`): гейт берёт индекс
// git, инъекционная проба — синтетическое дерево.
func collectDocsNameForm(tree *treecorpus.Tree) (docsNameFormCensus, error) {
	var c docsNameFormCensus

	// Корни клиентской поверхности: страницы сайтов и словари, из которых
	// страницы берут текст. Инженерная часть и код сайта сюда не входят —
	// граница названа в шапке.
	var roots []struct {
		dir   string
		suffs []string
		dict  bool
	}
	seen := map[string]bool{}
	for rel := range tree.Files() {
		var base string
		switch {
		case strings.HasPrefix(rel, "services/"):
			seg := strings.Split(rel, "/")
			if len(seg) < 3 || seg[2] != "docs" {
				continue
			}
			base = seg[0] + "/" + seg[1] + "/docs"
		case strings.HasPrefix(rel, "gateway/docs/"):
			base = "gateway/docs"
		default:
			continue
		}
		if seen[base] {
			continue
		}
		seen[base] = true
		roots = append(roots,
			struct {
				dir   string
				suffs []string
				dict  bool
			}{base + "/content", []string{".mdx", ".md"}, false},
			struct {
				dir   string
				suffs []string
				dict  bool
			}{base + "/src/constants", []string{".ts"}, true},
		)
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].dir < roots[j].dir })

	for _, r := range roots {
		for _, rel := range clientTruthTreeFiles(tree, r.dir, true, r.suffs...) {
			body, err := clientTruthReadTreeFile(tree, rel)
			if err != nil {
				return c, err
			}
			c.Files++
			if r.dict {
				c.DictFiles++
			} else {
				c.ContentDocs++
			}
			lines := strings.Split(string(body), "\n")
			ident := ""
			for i, ln := range lines {
				lineIdent := ident
				if m := docsNameFormIdentRe.FindStringSubmatch(ln); m != nil {
					ident, lineIdent = m[1], m[1]
				}

				decoded := docsNameFormDecodeEntities(ln)
				raws := docsNameFormRegexRe.FindAllString(ln, -1)
				decs := docsNameFormRegexRe.FindAllString(decoded, -1)

				// Регулярки читаются В ОБЕИХ кодировках. Авторитетен
				// РАСКОДИРОВАННЫЙ набор: он содержит и записанное обычно, и
				// записанное сущностями. Сырой текст ищется сопоставлением, а не
				// по номеру совпадения: нумерация разошлась бы на первой же
				// строке, где кодировки смешаны.
				for _, dec := range decs {
					c.Regexes++
					entity := !strings.Contains(ln, dec)
					raw := dec
					if entity {
						c.RegexEntity++
						for _, r0 := range raws {
							if docsNameFormDecodeEntities(r0) == dec {
								raw = r0
								break
							}
						}
					}

					shape, ok := docsNameFormClassify(lines, i, lineIdent, r.dict)
					if !ok {
						continue
					}
					c.Claims = append(c.Claims, docsNameFormClaim{
						File: rel, Line: i + 1, Raw: raw, Regex: dec,
						Shape: shape, Entity: entity,
					})
				}
			}
		}
	}
	sort.Slice(c.Claims, func(i, j int) bool {
		if c.Claims[i].File != c.Claims[j].File {
			return c.Claims[i].File < c.Claims[j].File
		}
		return c.Claims[i].Line < c.Claims[j].Line
	})
	return c, nil
}

// docsNameFormClassify — является ли регулярка на этой строке утверждением о
// ФОРМЕ ИМЕНИ, и в какой из трёх форм записи оно сделано.
func docsNameFormClassify(lines []string, idx int, ident string, dict bool) (docsNameFormShape, bool) {
	if dict {
		if docsNameFormIsNameKey(ident) {
			return docsNameFormShapeDict, true
		}
		return "", false
	}
	if field, isRow := docsNameFormTableFieldOf(lines[idx]); isRow {
		// Ряд таблицы судится ТОЛЬКО по своей первой ячейке: абзацная область
		// забрала бы в предмет соседний ряд с законно другой регуляркой.
		return docsNameFormShapeTable, field == "name"
	}
	if docsNameFormProseNamesTheField(lines, idx) {
		return docsNameFormShapeProse, true
	}
	return "", false
}

// docsNameFormFindings — что не сходится.
func docsNameFormFindings(c docsNameFormCensus) []string {
	exempt := map[string]docsNameFormExemption{}
	used := map[string]bool{}
	for _, e := range docsNameFormExemptions {
		exempt[e.File+"\x00"+e.Regex] = e
	}

	var out []string
	for _, cl := range c.Claims {
		key := cl.File + "\x00" + cl.Regex
		if _, ok := exempt[key]; ok {
			used[key] = true
			continue
		}
		if cl.Regex == nameform.Form {
			continue
		}
		out = append(out, fmt.Sprintf(
			"%s:%d (%s) показывает форму имени %q, а дерево держит %q "+
				"(pkg/validate/nameform.Form) — клиент чинит имя по правилу, которого нет",
			cl.File, cl.Line, cl.Shape, cl.Raw, nameform.Form))
	}

	// Самоистечение: запись, которой больше нечего исключать, — находка.
	for _, e := range docsNameFormExemptions {
		if used[e.File+"\x00"+e.Regex] {
			continue
		}
		out = append(out, fmt.Sprintf(
			"ведомость исключений: записи %s → %q больше нечего исключать "+
				"(такого утверждения в дереве нет) — снимите её, иначе она унаследует "+
				"следующую слепую зону; заведена была так: %s",
			e.File, e.Regex, e.Why))
	}
	sort.Strings(out)
	return out
}
