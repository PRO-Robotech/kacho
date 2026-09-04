// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clientdocsexamplefields.go — анализатор «пример ответа несёт РОВНО то
// множество полей, которое называет таблица той же страницы».
//
// # Предмет
//
// Страница ресурса говорит про ответ `Get` две вещи РАЗНЫМИ средствами: таблица
// «Поля ресурса» перечисляет поля прозой, пример показывает их телом. Вызывающий
// строит клиента по ПРИМЕРУ — он копируется целиком, — а таблицу читает выборочно.
// Поэтому поле, названное таблицей и выпавшее из примера, читается как
// случайность ровно так же, как поле, о котором страница молчит вовсе
// (`clientdocsfieldcoverage.go`): вызывающий не знает, приходит оно всегда или
// «иногда», и на всякий случай не опирается на него.
//
// Особенно это верно для ПРОИЗВОДНЫХ полей разбора: у роли выпадение
// `health`, счётчиков сегментов, ведомостей переселения и вырезания, состояний
// правил и жизненного состояния означало, что пример молчит ровно о том, ради
// чего эти поля заведены, — о разборе «почему у меня отобрали право».
//
// # Судится МНОЖЕСТВО, а не ЧИСЛО
//
// Сверка по числу пропускает двустороннее расхождение: пример, из которого одно
// поле выпало и в который одно лишнее добавлено, даёт то же число, что верный.
// Поэтому здесь сравниваются МНОЖЕСТВА и обе разности называются отдельно.
//
// Числом это уже мерили, и оно солгало дважды: заголовок задачи объявил «20 из
// 28», ревизия — «20 из 26» и лишнее `updatedAt`. Верно ни то ни другое:
// СТРОК таблицы 26, а ПОЛЕЙ в ней 27 — последняя строка называет `createdAt` и
// `updatedAt` вместе, — и разность односторонняя. Оба числа получены счётом
// строк (`<tr><td><code>`) и первого `<code>` в строке; предикат считал
// РАЗМЕТКУ, а не поля.
//
// # Кого он судит и почему круг именно такой
//
// Страницу, которая САМА обещает, что ответ несёт все поля ресурса. Обещание —
// предложение в блоке чтения по идентификатору; оно и есть утверждение, у
// которого обязан быть держатель. Страница, такого обещания не давшая, не
// судится: её пример вправе быть фрагментом.
//
// ОБЕЩАНИЕ СНЯТЬ РАДИ МОЛЧАНИЯ НЕЛЬЗЯ. Круг судимых страниц выведен из дерева, и
// пустой круг есть ОТКАЗ, а не успех (см. «Падает на пустом обходе»): снявший
// последнее обещание получает красное, а не тишину. Это не «строгость», а
// единственная форма, при которой послабление истекает само.
//
// # Как выбирается сам пример
//
// В блоке чтения по идентификатору примеров бывает несколько: рядом с полным
// ответом стоят фрагменты, показывающие одно-два поля крупным планом. Полным
// считается тот, у кого полей БОЛЬШЕ всех, — определение полного примера и есть
// «несёт все поля». Фрагменты не судятся; выпадение поля из полного примера его
// самым большим быть не перестаёт, поэтому подмена предмета этим выбором
// невозможна.
//
// Признаются ОБЕ формы записи примера, и это не мелочь: полный пример роли
// записан `<CodeBlock language="json">{dedent`…`}</CodeBlock>`, а фрагменты —
// оградой ```json. Распознаватель, знающий одну форму, полного примера не видел
// бы ВОВСЕ и молчал бы на любом расхождении.
//
// # ЧЕГО ОН НЕ СУДИТ
//
//  1. ЗНАЧЕНИЯ полей не судятся — только НАБОР имён. «Поле есть, но значение
//     несогласовано с соседним» есть другой предикат, и машинного признака у
//     него нет.
//  2. ПОЛЕ ВЛОЖЕННОГО СООБЩЕНИЯ, названное строкой таблицы через точку
//     (`referrer.type`), верхнеуровневым полем не является и из сверки выведено:
//     в примере оно лежит ВНУТРИ своего объекта, а не ключом верхнего уровня.
//  3. ГРАНИЦА ЭТОГО ВЫВЕДЕНИЯ НАЗВАНА ЧЕСТНО: строка, называющая поле вложенного
//     сообщения БЕЗ точки, от верхнеуровневой машинно неотличима, и такая
//     страница получит находку. Чинится она страницей — точкой в имени либо
//     отдельной таблицей вложенного сообщения, — а не послаблением здесь.
//  4. ПРИМЕР, КОТОРЫЙ НЕ РАЗОБРАЛСЯ как JSON, в сверку не идёт, и число таких
//     печатает перепись. Если у судимой страницы не разобрался НИ ОДИН — это
//     отказ анализатора, а не молчание: сверять было бы не с чем.
//
// # Ведомости исключений у гейта НЕТ
//
// По тому же доводу, что у соседа: страница, которой прощено расхождение,
// расходится ровно в том, ради чего гейт заведён, и снять запись некому.
//
// # Падает на ПУСТОМ ОБХОДЕ
//
// Ноль прочитанных страниц, ноль страниц с таблицей полей, ноль судимых страниц
// либо ноль рассуженных полей — «находок ноль» неотличимо от «прочитано ноль».
package repohygiene

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ClientDocsExampleFieldsOptions — вход анализатора.
type ClientDocsExampleFieldsOptions struct {
	// Root — корень дерева.
	Root string
	// DocRoots — каталоги сайтов документации относительно Root. Раздел API
	// ищется как <корень>/<компонент>/docs/content/api.
	DocRoots []string
}

// ClientDocsExampleFieldsCensus — объём осмотренного.
type ClientDocsExampleFieldsCensus struct {
	Pages          int
	PagesWithTable int
	PagesWithRead  int
	PagesJudged    int
	PagesUnjudged  []string
	TableFields    int
	ExampleFields  int
	ExamplesParsed int
	ExamplesBroken int
	Findings       int
}

func (c ClientDocsExampleFieldsCensus) String() string {
	return fmt.Sprintf(
		"страниц раздела API прочитано %d · с таблицей «Поля ресурса» %d · из них с примером "+
			"чтения по идентификатору %d · ОБЕЩАЮЩИХ «ответ несёт все поля» (судимых) %d · "+
			"примеров разобрано %d (не разобрано %d) · полей таблицы рассужено %d · полей "+
			"примера %d · находок %d",
		c.Pages, c.PagesWithTable, c.PagesWithRead, c.PagesJudged, c.ExamplesParsed,
		c.ExamplesBroken, c.TableFields, c.ExampleFields, c.Findings)
}

// ClientDocsExampleFieldsFinding — одна сторона расхождения по одному полю.
type ClientDocsExampleFieldsFinding struct {
	Page string
	// Field — имя поля в форме JSON края.
	Field string
	// MissingFromExample — поле названо таблицей и отсутствует в примере.
	// Иначе поле стоит в примере и не названо таблицей.
	MissingFromExample bool
}

func (f ClientDocsExampleFieldsFinding) String() string {
	if f.MissingFromExample {
		return fmt.Sprintf("%s: таблица называет поле %s, пример его не несёт", f.Page, f.Field)
	}
	return fmt.Sprintf("%s: пример несёт поле %s, таблица его не называет", f.Page, f.Field)
}

var (
	// cdefTableRe — раздел «Поля ресурса» до следующего заголовка того же уровня.
	cdefTableRe = regexp.MustCompile(`(?ms)^## Поля ресурса\s*$(.*?)(?:^## |\z)`)
	// cdefRowRe — ПЕРВАЯ ячейка строки таблицы: в ней стоит имя поля (иногда не
	// одно — `<code>createdAt</code> / <code>updatedAt</code>`).
	cdefRowRe = regexp.MustCompile(`<tr><td>(.*?)</td>`)
	// cdefCodeRe — имя поля в виде кода.
	cdefCodeRe = regexp.MustCompile(`<code>([^<]+)</code>`)
	// cdefReadBlockRe — блок операции ЧТЕНИЯ ПО ИДЕНТИФИКАТОРУ: метод GET и
	// параметр пути в адресе. Списочное чтение сюда не попадает by construction —
	// у него параметра пути нет.
	cdefReadBlockRe = regexp.MustCompile(
		`(?s)<ApiOperation method="GET" endpoint="[^"]*\{[^"}]+\}"[^>]*>(.*?)</ApiOperation>`)
	// cdefPromiseRe — ОБЕЩАНИЕ страницы: ответ несёт все поля ресурса. Это
	// утверждение, ради держателя которого гейт и заведён.
	cdefPromiseRe = regexp.MustCompile(`Ответ несёт (?:\*\*)?все(?:\*\*)? поля ресурса`)
	// cdefCodeBlockRe — пример в форме компонента с шаблонной строкой.
	cdefCodeBlockRe = regexp.MustCompile(
		"(?s)<CodeBlock language=\"json\">\\s*\\{dedent`(.*?)`\\}\\s*</CodeBlock>")
	// cdefFenceRe — пример в форме ограды.
	cdefFenceRe = regexp.MustCompile("(?s)```json\\n(.*?)```")
)

// AuditClientDocsExampleFields выносит вердикт о дереве.
func AuditClientDocsExampleFields(
	opts ClientDocsExampleFieldsOptions,
	log io.Writer,
) ([]ClientDocsExampleFieldsFinding, ClientDocsExampleFieldsCensus, error) {
	var census ClientDocsExampleFieldsCensus

	pages, err := clientDocsExamplePages(opts)
	if err != nil {
		return nil, census, err
	}
	census.Pages = len(pages)

	var findings []ClientDocsExampleFieldsFinding
	for _, rel := range pages {
		// #nosec G304 -- путь получен обходом дерева документации ЭТОГО репозитория
		raw, rerr := os.ReadFile(filepath.Join(opts.Root, filepath.FromSlash(rel)))
		if rerr != nil {
			return nil, census, fmt.Errorf("%s: %w", rel, rerr)
		}
		text := string(raw)

		table, hasTable := clientDocsExampleTableFields(text)
		if !hasTable {
			continue
		}
		census.PagesWithTable++

		block, hasBlock := clientDocsExampleReadBlock(text)
		if !hasBlock {
			continue
		}
		full, parsed, broken := clientDocsExampleFullExample(block)
		census.ExamplesParsed += parsed
		census.ExamplesBroken += broken
		if full == nil {
			continue
		}
		census.PagesWithRead++

		if !cdefPromiseRe.MatchString(block) {
			census.PagesUnjudged = append(census.PagesUnjudged, rel)
			continue
		}
		census.PagesJudged++
		census.TableFields += len(table)
		census.ExampleFields += len(full)

		for _, f := range cdefSortedKeys(table) {
			if !full[f] {
				findings = append(findings, ClientDocsExampleFieldsFinding{
					Page: rel, Field: f, MissingFromExample: true,
				})
			}
		}
		for _, f := range cdefSortedKeys(full) {
			if !table[f] {
				findings = append(findings, ClientDocsExampleFieldsFinding{
					Page: rel, Field: f, MissingFromExample: false,
				})
			}
		}
	}

	sort.Strings(census.PagesUnjudged)
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Page != findings[j].Page {
			return findings[i].Page < findings[j].Page
		}
		if findings[i].MissingFromExample != findings[j].MissingFromExample {
			return findings[i].MissingFromExample
		}
		return findings[i].Field < findings[j].Field
	})
	census.Findings = len(findings)

	if log != nil {
		_, _ = fmt.Fprintf(log, "перепись: %s\n", census)
		if len(census.PagesUnjudged) > 0 {
			_, _ = fmt.Fprintf(log,
				"  не судятся (таблица и полный пример есть, обещания «все поля» страница не даёт): %s\n",
				strings.Join(census.PagesUnjudged, " "))
		}
	}

	switch {
	case census.Pages == 0:
		return findings, census, fmt.Errorf(
			"прочитано ноль страниц раздела API в %s: сверять нечего",
			strings.Join(opts.DocRoots, " "))
	case census.PagesWithTable == 0:
		return findings, census, fmt.Errorf(
			"ни одна страница не несёт таблицы «Поля ресурса»: разбор разошёлся с деревом")
	case census.PagesJudged == 0:
		return findings, census, fmt.Errorf(
			"ни одна страница не обещает «ответ несёт все поля ресурса»: обещание снято " +
				"вместе с его держателем, и пустой круг судимых есть отказ, а не успех")
	case census.TableFields == 0:
		return findings, census, fmt.Errorf(
			"у судимых страниц рассужено ноль полей таблицы: разбор перестал видеть строки")
	}
	return findings, census, nil
}

// clientDocsExamplePages обходит страницы разделов API всех сайтов документации.
func clientDocsExamplePages(opts ClientDocsExampleFieldsOptions) ([]string, error) {
	var out []string
	for _, docRoot := range opts.DocRoots {
		dir := filepath.Join(opts.Root, filepath.FromSlash(docRoot))
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // корня сайтов документации нет — не находка, её назовёт перепись
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			apiRel := docRoot + "/" + e.Name() + "/docs/content/api"
			files, rerr := os.ReadDir(filepath.Join(opts.Root, filepath.FromSlash(apiRel)))
			if rerr != nil {
				continue // у компонента нет сайта документации — не находка
			}
			for _, f := range files {
				if f.IsDir() || !strings.HasSuffix(f.Name(), ".mdx") {
					continue
				}
				out = append(out, apiRel+"/"+f.Name())
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

// clientDocsExampleTableFields — множество ВЕРХНЕУРОВНЕВЫХ полей, названных
// таблицей «Поля ресурса».
//
// Читаются ВСЕ имена первой ячейки строки, а не первое: строка `createdAt /
// updatedAt` называет ДВА поля, и счёт по строкам потерял бы второе — ровно так
// «лишнее updatedAt» и получилось у прежних замеров.
//
// Имя с точкой — поле вложенного сообщения, ключом верхнего уровня оно не
// приходит; см. шапку, п. 2 и 3.
func clientDocsExampleTableFields(page string) (map[string]bool, bool) {
	m := cdefTableRe.FindStringSubmatch(page)
	if m == nil {
		return nil, false
	}
	out := map[string]bool{}
	for _, row := range cdefRowRe.FindAllStringSubmatch(m[1], -1) {
		for _, c := range cdefCodeRe.FindAllStringSubmatch(row[1], -1) {
			name := strings.TrimSpace(c[1])
			if name == "" || strings.Contains(name, ".") {
				continue
			}
			out[name] = true
		}
	}
	return out, true
}

// clientDocsExampleReadBlock — тело блока операции чтения по идентификатору.
func clientDocsExampleReadBlock(page string) (string, bool) {
	m := cdefReadBlockRe.FindStringSubmatch(page)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// clientDocsExampleFullExample — множество ключей ПОЛНОГО примера блока и то,
// сколько примеров разобрано и сколько не разобралось.
//
// Полным считается пример с наибольшим числом ключей; довод — в шапке.
func clientDocsExampleFullExample(block string) (full map[string]bool, parsed, broken int) {
	var bodies []string
	for _, m := range cdefCodeBlockRe.FindAllStringSubmatch(block, -1) {
		bodies = append(bodies, m[1])
	}
	for _, m := range cdefFenceRe.FindAllStringSubmatch(block, -1) {
		bodies = append(bodies, m[1])
	}
	for _, b := range bodies {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal([]byte(b), &obj); err != nil {
			broken++
			continue
		}
		parsed++
		if full != nil && len(obj) <= len(full) {
			continue
		}
		keys := make(map[string]bool, len(obj))
		for k := range obj {
			keys[k] = true
		}
		full = keys
	}
	return full, parsed, broken
}

func cdefSortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
