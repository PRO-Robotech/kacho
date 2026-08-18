// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// protodoclinktarget_test.go — ссылка на документацию из комментария контракта
// ведёт на страницу, которая в дереве ЕСТЬ.
//
// # Предмет
//
// Комментарий контракта писал «For more information, see [Networks](…)» и
// адресовал раздел `concepts/` сайта документации. Такого раздела нет ни у одного
// сайта дерева, а сами адреса начинались с `/docs/<домен>/` — префикса, которого
// нет ни в одной раскладке: сайт сервиса стоит в корне собственного origin
// (`url` + `baseUrl: '/'` + `routeBasePath: '/'`). Читатель контракта переходит и
// не попадает никуда.
//
// Хуже, чем битая ссылка в документе: комментарий контракта уезжает в
// сгенерированные стабы и в справочник API, то есть тиражируется в места, где
// его уже никто не сверяет с деревом.
//
// # Почему гейт, а не разовая правка
//
// Правка снимает восемь ссылок и уносит с собой того, кто их нашёл. Ссылка
// заводится вместе с каждым новым полем, а сайт документации переименовывает
// страницы своим темпом — расхождение возвращается молча и в обе стороны. Гейт
// остаётся в дереве и называет координату.
//
// # Что считается находкой
//
// Ссылка markdown в КОММЕНТАРИИ файла протокола, у которой в дереве нет цели:
//
//   - адрес от корня (`/docs/vpc/…`, `/api/network`) — у комментария контракта
//     нет базы, относительно которой корень разрешается: его читают в `.proto`, в
//     стабах и в справочнике, и ни одно из этих мест не является сайтом
//     документации. То же для относительного адреса;
//   - адрес сайта документации дерева, у которого нет страницы-цели;
//   - адрес сайта документации дерева С ФРАГМЕНТОМ: слug заголовка производит
//     генератор сайта, и из дерева контрактов его не проверить. Непроверяемое
//     утверждение в контракте — то же, что неверное, только тише.
//
// # Что находкой НЕ является
//
//   - чужой origin (`ietf.org`, `iana.org`): внешняя ссылка, дерево ей не
//     авторитет — считается в переписи, не судится;
//   - запись внутри код-спана: “ `[a-z]([-a-z0-9]{0,61}[a-z0-9])?` “ — это
//     регулярное выражение, а не ссылка, и markdown читает его так же;
//   - строковый литерал файла протокола (`(pattern) = "…"`): это код, а не
//     комментарий.
//
// # Граница гейта, названная явно
//
// Осматривается ИСТОЧНИК — дерево `proto/`. Сгенерированные стабы `pkg/api/…`
// несут те же комментарии копией, и молчание этого гейта об их содержимом НЕ
// означает, что копии свежи: свежесть выхода генератора — отдельное свойство
// («стабы перегенерированы после правки контракта»), и держать его обязан гейт
// генерации, а не этот. Здесь это сказано, чтобы следующий читатель не принял
// область молчания за область проверенного.
package repohygiene

import (
	"fmt"
	"net/url"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// ── что осматривается ────────────────────────────────────────────────────────

// protoDocLinkJudged — домены контрактов, чьи ссылки гейт СУДИТ, и сайт, чьи
// страницы для них авторитет искать не надо: origin ссылка называет сама.
var protoDocLinkJudged = []string{"proto/kacho/cloud/vpc/v1"}

// protoDocLinkDeferred — домены контрактов, где тот же класс ИЗМЕРЕН и не закрыт:
// их файлы принадлежат другому набору правок (compute — 7 адресов от корня,
// замер на e1275446). Послабление самоистекающее: гейт требует, чтобы у каждой
// записи БЫЛ предмет, и краснеет, когда домен вычищен, — тогда его место в
// protoDocLinkJudged, а не здесь. Записи, которой нечего исключать, в списке не
// остаётся.
//
// Запись `proto/kacho/cloud/access` (5 адресов) снята вместе со своим предметом:
// домен удалён целиком — контракт не обслуживался ни одним сервисом (kacho#580).
// Это и есть предписанное самоистечение, а не послабление гейту.
var protoDocLinkDeferred = []string{"proto/kacho/cloud/compute/v1"}

// ── чтение комментариев файла протокола ──────────────────────────────────────

var (
	// Код-спан снимается ДО поиска ссылок: двойная кавычка первой, иначе
	// одинарный сопоставитель разрежет её пополам.
	protoCodeSpanRe = regexp.MustCompile("``[^`]*``|`[^`]*`")
	protoMdLinkRe   = regexp.MustCompile(`\[([^\]\n]*)\]\(([^)\s]+)\)`)
)

// protoCommentLine — одна строка комментария с уже снятыми код-спанами.
type protoCommentLine struct {
	line  int
	text  string
	spans int
}

// protoCommentLines отдаёт комментарии файла протокола. `//` внутри строкового
// литерала комментарий не открывает — иначе `(pattern) = "…//…"` читался бы как
// проза, а образцы значений попадали бы в вердикт гейта.
func protoCommentLines(body string) []protoCommentLine {
	var out []protoCommentLine
	for i, raw := range strings.Split(body, "\n") {
		text, ok := protoLineComment(raw)
		if !ok {
			continue
		}
		spans := len(protoCodeSpanRe.FindAllString(text, -1))
		out = append(out, protoCommentLine{
			line:  i + 1,
			text:  protoCodeSpanRe.ReplaceAllString(text, " "),
			spans: spans,
		})
	}
	return out
}

func protoLineComment(line string) (string, bool) {
	inStr := false
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '\\':
			if inStr {
				i++
			}
		case '"':
			inStr = !inStr
		case '/':
			if !inStr && i+1 < len(line) && line[i+1] == '/' {
				return line[i+2:], true
			}
		}
	}
	return "", false
}

// ── сайт документации: origin → страницы ─────────────────────────────────────

// docSite — сайт документации компонента: origin, который он себе объявил, и
// множество маршрутов, которые он отдаёт.
type docSite struct {
	config string
	origin string
	routes map[string]bool
}

var (
	docSiteURLRe       = regexp.MustCompile(`(?m)^\s*url:\s*'([^']+)'`)
	docSiteBaseURLRe   = regexp.MustCompile(`(?m)^\s*baseUrl:\s*'([^']+)'`)
	docSitePathRe      = regexp.MustCompile(`(?m)^\s*path:\s*'([^']+)'`)
	docSiteRouteBaseRe = regexp.MustCompile(`(?m)^\s*routeBasePath:\s*'([^']+)'`)
	docSiteSlugRe      = regexp.MustCompile(`(?m)^slug:\s*(\S+)\s*$`)
)

// readDocSite собирает сайт из ОБЪЯВЛЕНИЯ (`docusaurus.config.ts`), а не из
// догадки о раскладке. Каждая величина, на которой стоит отображение
// «маршрут → файл», читается и проверяется: разъедется объявление — гейт скажет
// это словами, а не начнёт судить по неверной карте.
func readDocSite(configRel string, pages []string, read func(rel string) ([]byte, error)) (docSite, error) {
	body, err := read(configRel)
	if err != nil {
		return docSite{}, fmt.Errorf("%s: %w — сайт не прочитан, а непрочитанный сайт "+
			"нельзя ни засчитать в перепись, ни молча пропустить", configRel, err)
	}
	pick := func(re *regexp.Regexp, what string) (string, error) {
		m := re.FindSubmatch(body)
		if m == nil {
			return "", fmt.Errorf("%s: не объявлен %s — отображение «маршрут → файл» "+
				"стоит на этой величине, и без неё гейт судил бы по догадке", configRel, what)
		}
		return string(m[1]), nil
	}
	origin, err := pick(docSiteURLRe, "url")
	if err != nil {
		return docSite{}, err
	}
	baseURL, err := pick(docSiteBaseURLRe, "baseUrl")
	if err != nil {
		return docSite{}, err
	}
	contentDir, err := pick(docSitePathRe, "path раздела docs")
	if err != nil {
		return docSite{}, err
	}
	routeBase, err := pick(docSiteRouteBaseRe, "routeBasePath")
	if err != nil {
		return docSite{}, err
	}
	if baseURL != "/" || routeBase != "/" {
		return docSite{}, fmt.Errorf("%s: baseUrl=%q routeBasePath=%q — гейт умеет "+
			"отображать маршрут в файл только при корневой посадке сайта. Посадка "+
			"сменилась: перечитайте отображение, а не вердикт", configRel, baseURL, routeBase)
	}

	home := path.Dir(configRel) + "/" + contentDir + "/"
	site := docSite{config: configRel, origin: strings.TrimSuffix(origin, "/"), routes: map[string]bool{}}
	for _, rel := range pages {
		if !strings.HasPrefix(rel, home) {
			continue
		}
		route := strings.TrimSuffix(strings.TrimSuffix(strings.TrimPrefix(rel, home), ".mdx"), ".md")
		route = strings.TrimSuffix(route, "/index")
		body, err := read(rel)
		if err != nil {
			return docSite{}, fmt.Errorf("%s: %w — страница не прочитана", rel, err)
		}
		// `slug:` во front-matter переопределяет маршрут файла (в дереве так
		// посажена корневая страница). Без него гейт объявил бы существующую
		// страницу отсутствующей.
		if m := docSiteSlugRe.FindSubmatch(body); m != nil {
			route = strings.Trim(string(m[1]), `'"`)
		}
		site.routes[normalizeDocRoute(route)] = true
	}
	return site, nil
}

func normalizeDocRoute(route string) string {
	route = "/" + strings.Trim(route, "/")
	if route == "/" {
		return route
	}
	return route
}

// ── классификация и вердикт ──────────────────────────────────────────────────

const (
	linkClassSite     = "адрес сайта дерева"
	linkClassForeign  = "чужой origin"
	linkClassRooted   = "адрес от корня"
	linkClassRelative = "относительный адрес"
)

const (
	whyNoBase    = "у комментария контракта нет базы: его читают в .proto, в стабах и в справочнике — ни одно из этих мест не сайт документации"
	whyNoPage    = "сайт дерева такой страницы не отдаёт"
	whyFragment  = "фрагмент производит генератор сайта — из дерева контрактов он не проверяется"
	whyBadTarget = "адрес не разбирается"
)

type protoDocLinkFinding struct {
	file   string
	line   int
	text   string
	target string
	why    string
}

func (f protoDocLinkFinding) String() string {
	return fmt.Sprintf("%s:%d — [%s](%s): %s", f.file, f.line, f.text, f.target, f.why)
}

// protoDocLinkCensus — объём осмотренного. Печатается всегда: без него «ноль
// находок» неотличимо от «ноль прочитанного».
type protoDocLinkCensus struct {
	protoFiles   int
	commentLines int
	codeSpans    int
	links        map[string]int
	sites        int
	routes       int
}

func (c protoDocLinkCensus) String() string {
	return fmt.Sprintf("файлов протокола %d; строк комментария %d; код-спанов снято %d; "+
		"ссылок: %s %d, %s %d, %s %d, %s %d; сайтов %d, маршрутов %d",
		c.protoFiles, c.commentLines, c.codeSpans,
		linkClassSite, c.links[linkClassSite],
		linkClassForeign, c.links[linkClassForeign],
		linkClassRooted, c.links[linkClassRooted],
		linkClassRelative, c.links[linkClassRelative],
		c.sites, c.routes)
}

// protoSource — файл протокола вместе с телом: предикат не открывает файлы сам,
// поэтому тот же код судит и дерево, и фикстуру.
type protoSource struct {
	rel  string
	body []byte
}

// judgeProtoDocLinks судит каждую ссылку комментариев files против маршрутов
// сайтов sites (ключ — origin).
func judgeProtoDocLinks(files []protoSource, sites map[string]docSite) ([]protoDocLinkFinding, protoDocLinkCensus) {
	census := protoDocLinkCensus{protoFiles: len(files), links: map[string]int{}, sites: len(sites)}
	for _, s := range sites {
		census.routes += len(s.routes)
	}
	var findings []protoDocLinkFinding
	for _, f := range files {
		for _, c := range protoCommentLines(string(f.body)) {
			census.commentLines++
			census.codeSpans += c.spans
			for _, m := range protoMdLinkRe.FindAllStringSubmatch(c.text, -1) {
				class, why := classifyProtoDocLink(m[2], sites)
				census.links[class]++
				if why != "" {
					findings = append(findings, protoDocLinkFinding{f.rel, c.line, m[1], m[2], why})
				}
			}
		}
	}
	return findings, census
}

func classifyProtoDocLink(target string, sites map[string]docSite) (class, why string) {
	switch {
	case strings.HasPrefix(target, "http://"), strings.HasPrefix(target, "https://"):
		u, err := url.Parse(target)
		if err != nil {
			return linkClassForeign, whyBadTarget
		}
		site, ok := sites[u.Scheme+"://"+u.Host]
		if !ok {
			return linkClassForeign, ""
		}
		if u.Fragment != "" {
			return linkClassSite, whyFragment
		}
		if !site.routes[normalizeDocRoute(u.Path)] {
			return linkClassSite, whyNoPage
		}
		return linkClassSite, ""
	case strings.HasPrefix(target, "/"):
		return linkClassRooted, whyNoBase
	default:
		return linkClassRelative, whyNoBase
	}
}

// ── гейт на дереве ───────────────────────────────────────────────────────────

func TestProtoDocLinksResolveToPagesTheTreeHas(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	osRoot, err := os.OpenRoot(root)
	if err != nil {
		t.Fatalf("открыть корень %s: %v", root, err)
	}
	defer func() { _ = osRoot.Close() }()

	var configs, pages []string
	judged := map[string][]protoSource{}
	deferred := map[string][]protoSource{}
	for rel := range tt.files {
		switch {
		case strings.HasSuffix(rel, "/docs/docusaurus.config.ts"):
			configs = append(configs, rel)
		case strings.Contains(rel, "/docs/content/") &&
			(strings.HasSuffix(rel, ".md") || strings.HasSuffix(rel, ".mdx")):
			pages = append(pages, rel)
		case strings.HasSuffix(rel, ".proto"):
			dir := path.Dir(rel)
			body, err := osRoot.ReadFile(rel)
			if err != nil {
				t.Fatalf("%s: %v — файл контракта не прочитан", rel, err)
			}
			for _, d := range protoDocLinkJudged {
				if dir == d {
					judged[d] = append(judged[d], protoSource{rel, body})
				}
			}
			for _, d := range protoDocLinkDeferred {
				if dir == d {
					deferred[d] = append(deferred[d], protoSource{rel, body})
				}
			}
		}
	}
	sort.Strings(configs)
	sort.Strings(pages)

	sites := map[string]docSite{}
	for _, cfg := range configs {
		site, err := readDocSite(cfg, pages, osRoot.ReadFile)
		if err != nil {
			t.Fatalf("сайт документации: %v", err)
		}
		if len(site.routes) == 0 {
			t.Fatalf("%s: сайт объявлен, страниц ноль — гейт молчал бы не потому, что "+
				"ссылки целы, а потому, что цель искать негде", cfg)
		}
		sites[site.origin] = site
	}
	if len(sites) == 0 {
		t.Fatal("сайтов документации ноль — судить ссылки не против чего")
	}

	var files []protoSource
	for _, d := range protoDocLinkJudged {
		if len(judged[d]) == 0 {
			t.Fatalf("%s: файлов протокола ноль — домен объявлен судимым, а осматривать "+
				"в нём нечего. Каталог переехал или переименован: перечитайте перечень, "+
				"а не вердикт", d)
		}
		files = append(files, judged[d]...)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].rel < files[j].rel })

	findings, census := judgeProtoDocLinks(files, sites)
	t.Logf("перепись: %s; файлов в индексе %d", census, tt.count())

	if len(findings) > 0 {
		var b strings.Builder
		for _, f := range findings {
			b.WriteString("\n  " + f.String())
		}
		t.Fatalf("%d ссылок(и) из комментариев контрактов не имеют цели в дереве:%s\n\n"+
			"Сайт документации сервиса стоит в корне собственного origin (`url` в "+
			"docusaurus.config.ts), раздела concepts/ нет ни у одного: цель называется "+
			"полным адресом объявленной страницы. Нет подходящей страницы — заведите её "+
			"по регламенту документации сервиса либо объясните предмет текстом "+
			"комментария; ссылка в никуда не остаётся.\nПерепись: %s", len(findings), b.String(), census)
	}

	// Предпосылки проверяются ПОСЛЕ находок, и порядок здесь не косметика:
	// предпосылка объясняет МОЛЧАНИЕ гейта, поэтому спрашивать её, когда находки
	// есть, значит подменять вердикт. Первая редакция стояла до находок и на
	// красном дереве печатала «нет ни одной ссылки на сайт» вместо восьми
	// координат — то есть маскировала ровно тот дефект, который нашла.

	// Предпосылка первая: ссылка на сайт дерева в корпусе ЕСТЬ. Иначе судить
	// нечего, и молчание гейта означает «ссылок не осталось», а не «ссылки целы».
	if census.links[linkClassSite] == 0 {
		t.Fatalf("предпосылка не выполняется: в %d строках комментария нет ни одной "+
			"ссылки на сайт документации дерева. Либо ссылки убрали из контрактов "+
			"(тогда гейту нечего держать), либо сопоставитель ссылок сломан (тогда он "+
			"молча не читает ни одной) — перепись: %s", census.commentLines, census)
	}
	// Предпосылка вторая: код-спаны в комментариях есть, значит их снятие имеет
	// предмет. Регулярные выражения в комментариях контрактов — ровно та форма,
	// которую наивный сопоставитель читает как ссылку.
	if census.codeSpans == 0 {
		t.Fatalf("предпосылка не выполняется: код-спанов в комментариях ноль — их "+
			"снятие ничего не снимает, и ложное срабатывание на регулярном выражении "+
			"больше ничем не закрыто: %s", census)
	}

	// Послабление живёт, пока у него есть предмет: домен, вычищенный от адресов
	// от корня, обязан переехать в судимые, а не остаться в отложенных.
	for _, d := range protoDocLinkDeferred {
		if len(deferred[d]) == 0 {
			t.Fatalf("%s: домен отложен, а файлов протокола в нём ноль — исключать "+
				"нечего. Снимите запись из protoDocLinkDeferred", d)
		}
		rooted, _ := judgeProtoDocLinks(deferred[d], sites)
		if len(rooted) == 0 {
			t.Fatalf("%s: домен отложен, а ссылок без цели в нём больше нет — "+
				"послаблению нечего исключать. Перенесите запись в protoDocLinkJudged, "+
				"иначе следующее расхождение в этом домене никто не поймает", d)
		}
	}
}

// ── контроль в обе стороны ───────────────────────────────────────────────────

// docSiteFixtureConfig — объявление сайта той же ФОРМЫ, что в дереве: каждая
// величина на своей строке, вложенность presets сохранена. Форма здесь не
// украшение — читатель объявления привязан к началу строки, и фикстура, где
// величины стоят в одну строку, доказывала бы работу гейта на файле, которого в
// дереве не бывает.
func docSiteFixtureConfig(baseURL, routeBase string) string {
	return "import type { Config } from '@docusaurus/types'\n\n" +
		"const config: Config = {\n" +
		"  title: 'Kachō VPC',\n" +
		"  url: 'https://vpc.kacho.cloud',\n" +
		"  baseUrl: '" + baseURL + "',\n\n" +
		"  presets: [\n    [\n      'classic',\n      {\n        docs: {\n" +
		"          path: 'content',\n" +
		"          sidebarPath: './sidebars.ts',\n" +
		"          routeBasePath: '" + routeBase + "',\n" +
		"        },\n      },\n    ],\n  ],\n}\n\nexport default config\n"
}

// protoDocLinkFixture — дерево-близнец боевого: сайт документации сервиса с
// корневой посадкой и одной страницей раздела api, плюс один файл протокола, в
// комментарии которого стоит ссылка.
func protoDocLinkFixture(t *testing.T, comment string) ([]protoSource, map[string]docSite) {
	t.Helper()
	files := map[string]string{
		"services/vpc/docs/docusaurus.config.ts":    docSiteFixtureConfig("/", "/"),
		"services/vpc/docs/content/api/network.mdx": "---\nid: network\n---\n\n# Network\n",
		"services/vpc/docs/content/intro.mdx":       "---\nid: intro\nslug: /\n---\n\n# Введение\n",
	}
	read := func(rel string) ([]byte, error) {
		body, ok := files[rel]
		if !ok {
			return nil, fmt.Errorf("нет такого файла: %s", rel)
		}
		return []byte(body), nil
	}
	site, err := readDocSite("services/vpc/docs/docusaurus.config.ts",
		[]string{"services/vpc/docs/content/api/network.mdx", "services/vpc/docs/content/intro.mdx"}, read)
	if err != nil {
		t.Fatalf("фикстура сайта: %v", err)
	}
	// Фикстура не снисходительнее продукта: маршруты те же две формы, что в
	// дереве, — файловая и переопределённая `slug:`.
	if !site.routes["/api/network"] || !site.routes["/"] {
		t.Fatalf("фикстура сайта собрана неверно, маршруты: %v", site.routes)
	}
	body := "syntax = \"proto3\";\n\nmessage Network {\n  // " + comment + "\n  string id = 1;\n}\n"
	return []protoSource{{"proto/kacho/cloud/vpc/v1/network.proto", []byte(body)}},
		map[string]docSite{site.origin: site}
}

func TestProtoDocLinkGate_ProvenByInjection(t *testing.T) {
	// Первые две подпробы — пара «дефект вернулся / законный близнец той же формы».
	// Близнец обязателен: без него гейт нельзя отличить от такого, который
	// краснеет на любой ссылке вообще.
	t.Run("дефект возвращён — гейт краснеет и называет координату", func(t *testing.T) {
		files, sites := protoDocLinkFixture(t,
			"A Network resource. For more information, see [Networks](/docs/vpc/concepts/network).")
		findings, census := judgeProtoDocLinks(files, sites)
		if census.links[linkClassRooted] != 1 {
			t.Fatalf("ссылка не прочитана (%s) — гейт молчал бы не потому, что дерево "+
				"чистое", census)
		}
		if len(findings) != 1 {
			t.Fatalf("ожидалась 1 находка, получено %d: %v", len(findings), findings)
		}
		got := findings[0]
		if got.file != "proto/kacho/cloud/vpc/v1/network.proto" || got.line != 4 ||
			got.target != "/docs/vpc/concepts/network" || got.why != whyNoBase {
			t.Fatalf("находка не называет координату: %s", got)
		}
	})

	t.Run("законный близнец той же формы — гейт молчит", func(t *testing.T) {
		// Та же строка, тот же глагол, тот же текст ссылки; меняется только
		// существо: адрес полон и страница у сайта есть.
		files, sites := protoDocLinkFixture(t,
			"A Network resource. For more information, see [Networks](https://vpc.kacho.cloud/api/network).")
		findings, census := judgeProtoDocLinks(files, sites)
		if census.links[linkClassSite] != 1 {
			t.Fatalf("близнец должен быть ПРОЧИТАН, иначе молчание ничего не значит: %s", census)
		}
		if len(findings) != 0 {
			t.Fatalf("ложное срабатывание на законной ссылке: %v", findings)
		}
	})

	t.Run("страницы у сайта нет — находка", func(t *testing.T) {
		files, sites := protoDocLinkFixture(t,
			"See [Concepts](https://vpc.kacho.cloud/concepts/network).")
		findings, census := judgeProtoDocLinks(files, sites)
		if census.links[linkClassSite] != 1 {
			t.Fatalf("ссылка не прочитана: %s", census)
		}
		if len(findings) != 1 || findings[0].why != whyNoPage {
			t.Fatalf("ожидалась находка «нет страницы», получено %v", findings)
		}
	})

	t.Run("фрагмент не проверяется из дерева — находка", func(t *testing.T) {
		files, sites := protoDocLinkFixture(t,
			"See [Subnets](https://vpc.kacho.cloud/api/network#subnet).")
		findings, _ := judgeProtoDocLinks(files, sites)
		if len(findings) != 1 || findings[0].why != whyFragment {
			t.Fatalf("ожидалась находка «фрагмент», получено %v", findings)
		}
	})

	t.Run("чужой origin не судится, но считается", func(t *testing.T) {
		files, sites := protoDocLinkFixture(t,
			"Creation timestamp in [RFC3339](https://www.ietf.org/rfc/rfc3339.txt) text format.")
		findings, census := judgeProtoDocLinks(files, sites)
		if census.links[linkClassForeign] != 1 {
			t.Fatalf("чужая ссылка обязана попасть в перепись: %s", census)
		}
		if len(findings) != 0 {
			t.Fatalf("внешняя ссылка судиться не должна — дерево ей не авторитет: %v", findings)
		}
	})

	t.Run("регулярное выражение в код-спане ссылкой не является", func(t *testing.T) {
		// Ровно та форма, что стоит в комментариях контрактов дерева. Наивный
		// сопоставитель читает `[a-z](…)` как ссылку и роняет гейт на законном
		// комментарии — тогда его снимут как шумный, вместе со всем предметом.
		files, sites := protoDocLinkFixture(t,
			"Value must match the regular expression ``\\|[a-z]([-a-z0-9]{0,61}[a-z0-9])?``.")
		findings, census := judgeProtoDocLinks(files, sites)
		if census.codeSpans != 1 {
			t.Fatalf("код-спан не снят (%d) — ложное срабатывание закрыто не тем, чем думаем: %s",
				census.codeSpans, census)
		}
		total := 0
		for _, n := range census.links {
			total += n
		}
		if total != 0 || len(findings) != 0 {
			t.Fatalf("регулярное выражение прочитано как ссылка: ссылок %d, находки %v",
				total, findings)
		}
	})

	t.Run("строковый литерал — код, а не комментарий", func(t *testing.T) {
		// Образец значения поля несёт ту же форму в кавычках. Комментария в строке
		// нет вовсе, значит и ссылки в ней нет.
		files := []protoSource{{"proto/kacho/cloud/vpc/v1/gateway_service.proto",
			[]byte("message M {\n  string name = 2 [(pattern) = \"|[a-z]([-a-z0-9]{0,61}[a-z0-9])?\"];\n}\n")}}
		_, sites := protoDocLinkFixture(t, "no link here")
		findings, census := judgeProtoDocLinks(files, sites)
		if census.commentLines != 0 {
			t.Fatalf("строка без комментария прочитана как комментарий: %s", census)
		}
		if len(findings) != 0 {
			t.Fatalf("образец значения прочитан как ссылка: %v", findings)
		}
	})

	t.Run("ссылка рядом с код-спаном всё ещё читается", func(t *testing.T) {
		// Обратная сторона снятия код-спанов: снимать надо спан, а не остаток
		// строки. Без этой подпробы жадный сопоставитель проглотил бы ссылку
		// вместе со спаном, и гейт молчал бы на настоящей находке.
		files, sites := protoDocLinkFixture(t,
			"Matches ``[a-z]([-a-z0-9]{0,61})?`` — see [Networks](/docs/vpc/concepts/network).")
		findings, census := judgeProtoDocLinks(files, sites)
		if census.codeSpans != 1 {
			t.Fatalf("код-спан не снят: %s", census)
		}
		if len(findings) != 1 || findings[0].target != "/docs/vpc/concepts/network" {
			t.Fatalf("ссылка рядом со спаном потеряна: находки %v, перепись %s", findings, census)
		}
	})

	t.Run("посадка сайта сменилась — гейт отказывает, а не судит по догадке", func(t *testing.T) {
		read := func(string) ([]byte, error) {
			return []byte(docSiteFixtureConfig("/docs/vpc/", "/")), nil
		}
		if _, err := readDocSite("services/vpc/docs/docusaurus.config.ts", nil, read); err == nil {
			t.Fatal("непорневая посадка принята молча — отображение «маршрут → файл» " +
				"стало бы неверным, а вердикт остался бы уверенным")
		}
	})
}
