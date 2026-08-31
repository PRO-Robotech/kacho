// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clienttruth_vpc_docsbodyfields.go — анализатор «ключ в примере тела запроса
// существует в контракте».
//
// # Предмет
//
// Пример `curl` на странице документации — это то, что клиент копирует ПЕРВЫМ.
// Ключ, которого у запроса нет, край отбрасывает молча (разбор тела неизвестные
// ключи не сохраняет), и вызывающий получает отказ по совершенно другому поводу —
// «обязательное поле не задано» — при том что он его задал, как учила страница.
// Стоит это круга «запрос → 400 → поиск вслепую», а искать нечего: верного имени
// на странице нет ни разу.
//
// Замер на день заведения (kacho#1620): быстрый старт vpc создавал подсеть полем
// `v4CidrBlocks`, снятым с запроса создания (его номер зарезервирован), тогда как
// действующий якорь — `ipv4CidrPrimary`. Перепись по имени механизма нашла ВТОРОЕ
// место того же класса, которого задача не называла: справочная страница подсети
// учит верной форме во вкладке ZONAL и неверной (`ipv4CidrBlocks`) во вкладке
// REGIONAL — то есть один документ расходится сам с собой.
//
// # Что судит анализатор
//
// Маршруты выводятся из ДЕРЕВА КОНТРАКТОВ: `rpc <Имя>(<Запрос>)` плюс его
// `option (google.api.http)` с глаголом, несущим тело (`post`/`patch`/`put`).
// Второй рукописной таблицы «путь → сообщение» не заводится: она разошлась бы с
// первой молча.
//
// На странице распознаётся ВЫЗОВ С ТЕЛОМ — строка `curl` с `-X POST|PATCH|PUT` и
// путём, и следующий за ней одинарно-закавыченный аргумент `-d '…'`. Каждый ключ
// ВЕРХНЕГО уровня этого тела, переведённый в змеиную запись, обязан быть
// объявленным полем сообщения запроса.
//
// # ЧЕГО ОН НЕ СУДИТ, и это названо числом, а не умолчанием
//
//  1. ОХВАТ ЧАСТИЧЕН, и перепись печатает обе величины — сколько деревьев
//     документации в репозитории и сколько судится. Класс живёт и у соседей;
//     чинит их владелец домена, а расширение [DocsBodyFieldOptions.DocRoots] —
//     правка одной строки. Молчаливо суженный обход был бы хуже: он выглядел бы
//     как «класс закрыт везде».
//
//  2. ВЛОЖЕННЫЕ КЛЮЧИ не судятся — только верхний уровень. Вложенное тело
//     принадлежит другому сообщению, и связать его с полем без разбора типов
//     нельзя; догадка здесь давала бы находки на верных примерах.
//
//  3. ПУТЬ БЕЗ МАРШРУТА не судится, и число таких печатается. Ноль здесь —
//     свойство дерева vpc, а не гарантия: у соседей встречаются пути с
//     сокращённым идентификатором в примере, которые сегментно не совпадают ни с
//     одним шаблоном.
//
//  4. ПОЛНОТА не судится: пример, назвавший два поля из десяти, верен в том, что
//     назвал.
//
// # Падает на ПУСТОМ ОБХОДЕ
//
// Ноль маршрутов, ноль прочитанных страниц, ноль распознанных тел либо ноль
// рассуженных ключей — «находок ноль» неотличимо от «прочитано ноль».
package repohygiene

import (
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
)

// DocsBodyFieldOptions — вход анализатора.
type DocsBodyFieldOptions struct {
	// Tree — СОСТАВ дерева, а не его корень: гейт берёт индекс git
	// (`treecorpus.NewTree`), инъекционная проба — синтетическое дерево
	// (`treecorpus.SyntheticTree`). Разбор — clienttruth_treefiles.go.
	Tree *treecorpus.Tree
	// ProtoRoot — каталог контрактов относительно корня дерева. Маршруты
	// выводятся отсюда и только отсюда.
	ProtoRoot string
	// DocRoots — каталоги клиентской документации, которые судятся.
	DocRoots []string
}

// DocsBodyFieldCensus — объём осмотренного. Печатается всегда: «ноль находок»
// обязано быть отличимо от «ноль прочитанного».
type DocsBodyFieldCensus struct {
	// ProtoFiles — прочитано файлов контракта.
	ProtoFiles int
	// Routes — маршрутов с телом, выведенных из контрактов.
	Routes int
	// DocFiles — прочитано страниц документации.
	DocFiles int
	// Bodies — распознано тел запроса.
	Bodies int
	// Routed — из них сопоставлено маршруту.
	Routed int
	// Unrouted — не сопоставлено (путь не совпал ни с одним шаблоном).
	Unrouted int
	// Keys — рассужено ключей верхнего уровня.
	Keys int
	// DocTreesTotal — деревьев документации в репозитории.
	DocTreesTotal int
	// DocTreesJudged — из них судится этим прогоном.
	DocTreesJudged int
}

// DocsBodyFieldFinding — одна находка.
type DocsBodyFieldFinding struct {
	// File, Line — координата примера.
	File string
	Line int
	// Method, Path — глагол и путь примера.
	Method string
	Path   string
	// Request — сообщение запроса, выведенное из контракта.
	Request string
	// Key — ключ примера, как он написан.
	Key string
	// Snake — он же в змеиной записи, которой искалось поле.
	Snake string
}

func (f DocsBodyFieldFinding) String() string {
	return fmt.Sprintf("%s:%d: %s %s — ключ %q (%s) не объявлен полем %s",
		f.File, f.Line, f.Method, f.Path, f.Key, f.Snake, f.Request)
}

var (
	// docsRPCRe — объявление RPC вместе с типом запроса.
	docsRPCRe = regexp.MustCompile(`\brpc\s+\w+\s*\(\s*([\w.]+)\s*\)`)

	// docsHTTPRe — глагол, несущий тело, и его путь.
	docsHTTPRe = regexp.MustCompile(`\b(post|patch|put)\s*:\s*"([^"]+)"`)

	// docsMessageRe — объявление сообщения.
	docsMessageRe = regexp.MustCompile(`^\s*message\s+([A-Za-z][A-Za-z0-9_]*)\s*\{`)

	// docsFieldRe — объявление поля, включая карту (`map<string, string> labels = 6;`).
	docsFieldRe = regexp.MustCompile(`^\s*(?:repeated\s+|optional\s+)?(?:map<[^>]+>|[A-Za-z0-9_.]+)\s+([a-z][a-z0-9_]*)\s*=\s*\d+\s*;`)

	// docsCurlVerbRe — глагол вызова.
	docsCurlVerbRe = regexp.MustCompile(`-X\s+(POST|PATCH|PUT)\b`)

	// docsCurlPathRe — путь вызова. Хост, переменные оболочки и подстановки вида
	// `{projectId}` отбрасываются сопоставлением по сегментам.
	docsCurlPathRe = regexp.MustCompile(`(/[a-z][a-zA-Z0-9]*/v1/[A-Za-z0-9{}$/:._-]*)`)

	// docsJSONTokenRe — фигурная скобка либо ключ. Глубина считается по скобкам,
	// поэтому ключи верхнего уровня отделимы от вложенных.
	docsJSONTokenRe = regexp.MustCompile(`[{}]|"([A-Za-z_][A-Za-z0-9_]*)"\s*:`)
)

// AuditDocsBodyFields читает контракты и страницы и возвращает находки и перепись.
func AuditDocsBodyFields(
	opts DocsBodyFieldOptions, log io.Writer,
) ([]DocsBodyFieldFinding, DocsBodyFieldCensus, error) {
	var census DocsBodyFieldCensus

	type route struct {
		method  string
		segs    []string
		request string
	}
	var routes []route
	fields := map[string]map[string]bool{}

	for _, rel := range clientTruthTreeFiles(opts.Tree, opts.ProtoRoot, true, ".proto") {
		body, rerr := clientTruthReadTreeFile(opts.Tree, rel)
		if rerr != nil {
			return nil, census, rerr
		}
		census.ProtoFiles++
		var (
			curMsg  string
			depth   int
			pending string
		)
		for _, raw := range strings.Split(string(body), "\n") {
			// Комментарий снимается ДО подсчёта скобок и разбора полей: скобка в
			// прозе сдвигала бы глубину, и всё, что за ней, приписывалось бы не
			// тому сообщению.
			line := docsStripComment(raw)
			if curMsg == "" {
				if m := docsMessageRe.FindStringSubmatch(line); m != nil {
					curMsg = m[1]
					if fields[curMsg] == nil {
						fields[curMsg] = map[string]bool{}
					}
					depth = strings.Count(line, "{") - strings.Count(line, "}")
					docsCollectFields(fields[curMsg], line)
					if depth <= 0 {
						// Сообщение в ОДНУ строку (`message R { string id = 1; }`).
						// Не закрыв его здесь, разбор считал бы своими все
						// последующие строки файла — и поля настоящих сообщений
						// терялись бы молча. В дереве таких сообщений двадцать с
						// лишним, и на них анализатор давал шесть ложных находок.
						curMsg = ""
					}
					continue
				}
			} else {
				depth += strings.Count(line, "{") - strings.Count(line, "}")
				docsCollectFields(fields[curMsg], line)
				if depth <= 0 {
					curMsg = ""
				}
				continue
			}
			if m := docsRPCRe.FindStringSubmatch(line); m != nil {
				pending = m[1]
			}
			if pending == "" {
				continue
			}
			if m := docsHTTPRe.FindStringSubmatch(line); m != nil {
				routes = append(routes, route{
					method:  strings.ToUpper(m[1]),
					segs:    strings.Split(strings.Trim(m[2], "/"), "/"),
					request: pending,
				})
				pending = ""
			}
		}
	}
	census.Routes = len(routes)

	// Сколько деревьев документации в репозитории вообще — чтобы частичность
	// охвата была видна в каждом прогоне, а не только в этом комментарии.
	census.DocTreesTotal = countDocTrees(opts.Tree)
	census.DocTreesJudged = len(opts.DocRoots)

	matchRoute := func(method string, path string) string {
		want := strings.Split(strings.Trim(path, "/"), "/")
		for _, r := range routes {
			if r.method != method || len(r.segs) != len(want) {
				continue
			}
			ok := true
			for i, seg := range r.segs {
				if strings.Contains(seg, "{") {
					// Сегмент-подстановка может нести суффикс-глагол
					// (`{subnet_id}:add-cidr-blocks`) — он обязан совпасть.
					if idx := strings.Index(seg, ":"); idx >= 0 {
						if !strings.HasSuffix(want[i], seg[idx:]) {
							ok = false
							break
						}
					}
					continue
				}
				if seg != want[i] {
					ok = false
					break
				}
			}
			if ok {
				return r.request
			}
		}
		return ""
	}

	var findings []DocsBodyFieldFinding
	for _, docRoot := range opts.DocRoots {
		for _, rel := range clientTruthTreeFiles(opts.Tree, docRoot, true, ".mdx") {
			raw, rerr := clientTruthReadTreeFile(opts.Tree, rel)
			if rerr != nil {
				return nil, census, rerr
			}
			census.DocFiles++
			lines := strings.Split(string(raw), "\n")
			for i, line := range lines {
				if !strings.Contains(line, "curl") {
					continue
				}
				verb := docsCurlVerbRe.FindStringSubmatch(line)
				if verb == nil {
					continue
				}
				p := docsCurlPathRe.FindStringSubmatch(line)
				if p == nil {
					continue
				}
				blob, ok := docsQuotedBody(lines, i)
				if !ok {
					continue
				}
				census.Bodies++
				req := matchRoute(verb[1], p[1])
				if req == "" {
					census.Unrouted++
					continue
				}
				census.Routed++
				for _, key := range docsTopLevelKeys(blob) {
					census.Keys++
					snake := docsSnake(key)
					if fields[req][snake] {
						continue
					}
					findings = append(findings, DocsBodyFieldFinding{
						File: rel, Line: i + 1, Method: verb[1], Path: p[1],
						Request: req, Key: key, Snake: snake,
					})
				}
			}
		}
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		if findings[i].Line != findings[j].Line {
			return findings[i].Line < findings[j].Line
		}
		return findings[i].Key < findings[j].Key
	})

	if log != nil {
		_, _ = fmt.Fprintf(log,
			"перепись: файлов контракта %d · маршрутов с телом %d · страниц %d · "+
				"тел запроса %d (сопоставлено %d, без маршрута %d) · ключей рассужено %d · "+
				"деревьев документации %d, судится %d\n",
			census.ProtoFiles, census.Routes, census.DocFiles,
			census.Bodies, census.Routed, census.Unrouted, census.Keys,
			census.DocTreesTotal, census.DocTreesJudged)
	}
	return findings, census, nil
}

// docsStripComment снимает строчный комментарий. Скобка в прозе, посчитанная как
// код, сдвигает глубину разбора, и дальше поля приписываются не тому сообщению.
func docsStripComment(line string) string {
	if i := strings.Index(line, "//"); i >= 0 {
		return line[:i]
	}
	return line
}

// docsCollectFields собирает объявления полей строки. Разбор идёт по фрагментам
// между разделителями, а не по строке целиком: у сообщения в одну строку поле
// стоит после открывающей скобки, и предикат, привязанный к началу строки, его
// не увидел бы.
func docsCollectFields(into map[string]bool, line string) {
	for _, frag := range strings.FieldsFunc(line, func(r rune) bool {
		return r == '{' || r == '}' || r == ';'
	}) {
		if m := docsFieldRe.FindStringSubmatch(frag + ";"); m != nil {
			into[m[1]] = true
		}
	}
}

// docsQuotedBody — одинарно-закавыченный аргумент `-d '…'`, начинающийся в
// пределах ПРОДОЛЖЕНИЯ команды.
//
// Окно узкое намеренно. Широкий поиск уезжает за конец команды и подхватывает
// следующий блок страницы — обычно это пример ОТВЕТА, и тогда анализатор судит
// ключи ответа против полей запроса. Прототип на этом и споткнулся: три ложные
// находки (`id`, `done`, `metadata`) на верном примере операции.
func docsQuotedBody(lines []string, start int) (string, bool) {
	const window = 8
	for j := start; j < len(lines) && j < start+window; j++ {
		idx := strings.Index(lines[j], "-d '")
		if idx < 0 {
			continue
		}
		seg := lines[j][idx+len("-d '"):]
		// Тело в одну строку — закрывающая кавычка уже здесь. Проверять это надо
		// ДО чтения следующей строки, иначе однострочное тело склеивается с тем,
		// что за ним.
		if strings.HasSuffix(strings.TrimRight(seg, " \\"), "'") {
			return seg, true
		}
		var b strings.Builder
		b.WriteString(seg)
		for k := j + 1; k < len(lines) && k < j+80; k++ {
			b.WriteString("\n")
			b.WriteString(lines[k])
			if strings.HasSuffix(strings.TrimRight(lines[k], " \\"), "'") {
				return b.String(), true
			}
		}
		return b.String(), true
	}
	return "", false
}

// docsTopLevelKeys — ключи ВЕРХНЕГО уровня тела. Глубина считается по фигурным
// скобкам: вложенное тело принадлежит другому сообщению, и судить его ключи
// против полей внешнего значило бы краснеть на верных примерах.
func docsTopLevelKeys(blob string) []string {
	var (
		keys  []string
		depth int
	)
	for _, m := range docsJSONTokenRe.FindAllStringSubmatch(blob, -1) {
		switch m[0] {
		case "{":
			depth++
		case "}":
			depth--
		default:
			if depth == 1 {
				keys = append(keys, m[1])
			}
		}
	}
	return keys
}

// docsSnake — camelCase ключа JSON в snake_case поля контракта. Соответствие
// однозначно: JSON-имена производятся из имён полей самим транскодером края.
func docsSnake(k string) string {
	var b strings.Builder
	for i, c := range k {
		if c >= 'A' && c <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(c - 'A' + 'a')
			continue
		}
		b.WriteRune(c)
	}
	return b.String()
}

// countDocTrees — сколько деревьев клиентской документации есть в репозитории.
// Нужен ровно для того, чтобы частичность охвата печаталась числом: суженный
// обход, о котором молчат, читается как «класс закрыт везде».
//
// Считает по СОСТАВУ, а не по каталогам на диске: `os.ReadDir`+`os.Stat` учли бы
// сборку сайта документации и распаковку чарта наравне с деревом, и знаменатель
// охвата стал бы свойством рабочего каталога. Каталог, о котором состав не
// знает, деревом документации не является — [treecorpus.Tree.HasDir] отвечает
// «есть ли хоть один отслеживаемый файл на этом пути или ниже», а это и есть
// искомое.
func countDocTrees(tree *treecorpus.Tree) int {
	n := 0
	if tree.HasDir("gateway/docs/content") {
		n++
	}
	// Имена сервисов ВЫВОДЯТСЯ из состава, а не выписываются: рукописный
	// перечень разошёлся бы с деревом молча — ровно тот класс, ради которого
	// это число и печатается.
	seen := map[string]bool{}
	for _, rel := range tree.SortedFiles() {
		parts := strings.SplitN(rel, "/", 3)
		if len(parts) < 3 || parts[0] != "services" || seen[parts[1]] {
			continue
		}
		seen[parts[1]] = true
		if tree.HasDir("services/" + parts[1] + "/docs/content") {
			n++
		}
	}
	return n
}
