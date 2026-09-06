// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clientdocscontractdrift.go — два анализатора класса «клиентская страница
// пережила свой контракт».
//
// # Предмет, общий для обоих
//
// Страница документации — утверждение о контракте, и устаревает оно МОЛЧА: снятие
// поля и пометка глагола к снятию не рождают ни конфликта при слиянии, ни красного
// в сборке сайта. Клиент строит на таком утверждении интеграцию и узнаёт о
// расхождении в проде.
//
// Замер дня заведения (kacho#1700, #1692): на одной странице iam пример ответа
// показывал поле, снятое с контракта, а разбор снятия стоял на ЭТОЙ ЖЕ странице
// 84 строками ниже; четыре чтения привязок были помечены в контракте к снятию, а
// страница подавала их действующим срезом, ни разу не назвав рекомендованную
// замену. Ни то, ни другое не видно в обзоре изменения: каждая половина защитима,
// расходится только смысл.
//
// # Анализатор 1 — снятое поле в примере (AuditClientDocsRetiredFieldInExample)
//
// Судится ОДНО, зато без суждения: имя, которое контракт домена ЗАБРАЛ
// (`reserved "<имя>"`), обратно живым полем не станет никогда — резерв имени для
// того и пишется. Пример, показывающий такое имя, обещает поле, которого не
// придёт, и принимает значение, которое сервер отвергнет.
//
// Ключи примера сверяются с ДВУМЯ множествами домена сайта:
//
//	забранные   — все `reserved "<имя>"` пакета контракта этого домена;
//	живые       — все объявленные поля этого домена ПЛЮС общих пакетов
//	              (`operation`, `api`, `reference`, `quota`, `subscription`).
//
// Общие пакеты в объединении обязательны: пример ответа мутации показывает
// конверт `Operation`, чьи поля живут не в домене. Без объединения гейт краснел бы
// на `metadata` у семи страниц вычислений — на верном тексте (замерено).
//
// ЧЕГО ОН НЕ СУДИТ, и это названо числом, а не умолчанием:
//
//  1. ключ, которого в контракте нет ВОВСЕ, не судится. Вариант «неизвестный ключ
//     — находка» проверен и ОТВЕРГНУТ замером: он краснеет на `code`/`message`/
//     `details` (конверт `google.rpc.Status`, чужая схема) и на всяком поле чужого
//     продукта, законно процитированном примером. Чтобы судить его, надо знать, о
//     КАКОМ сообщении пример, а пример этого не говорит;
//  2. сообщение примера не резолвится, поэтому живое поле СОСЕДНЕГО сообщения
//     проходит;
//  3. проза не судится — только тела примеров JSON.
//
// # Анализатор 2 — помеченное к снятию подаётся действующим
//
//	(AuditClientDocsDeprecationParity)
//
// Контракт помечает глагол к снятию комментарием `// DEPRECATED`. Клиентская
// страница, документирующая ЕГО REST-путь и не несущая ни одной пометки, даёт
// обещание совместимости, которого продукт не давал.
//
// Обе стороны читаются ОБЪЯВЛЕНИЯМИ, а не подстрокой: пометка ищется в блоке
// комментариев НЕПОСРЕДСТВЕННО перед `rpc`, путь — в аннотации `google.api.http`
// внутри тела этого же `rpc`, а на стороне страницы судится тело блока
// `<ApiOperation …>…</ApiOperation>`, объявляющего ровно этот путь. Предикат по
// подстроке краснел бы на собственном объяснении: слово «снят» стоит и в прозе
// про снятые поля.
//
// ЧЕГО ОН НЕ СУДИТ: пометка на странице засчитывается ЛЮБАЯ из словаря —
// анализатор не берётся судить, названа ли рядом замена. «Замена названа» —
// свойство смысла, у него нет машинного предиката; его держит обзор.
//
// # Оба падают на ПУСТОМ ОБХОДЕ
//
// Ноль сайтов, ноль страниц, ноль примеров, ноль забранных имён, ноль помеченных
// глаголов, ноль сверенных блоков — «находок ноль» неотличимо от «прочитано ноль».
package repohygiene

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// clientDocsSharedProtoDomains — пакеты контракта, чьи поля законно встречаются в
// примере ЛЮБОГО домена: конверт операции, общие опции, ссылки, пределы, подписка.
//
// Перечень объявлен, а не выведен, и это осознанно: «домен, у которого нет своего
// сайта документации» вывело бы сюда и `subscription`, и завтрашний домен без
// сайта — то есть предикат менялся бы от появления сайта, а не от смысла.
var clientDocsSharedProtoDomains = []string{
	"operation", "api", "reference", "quota", "subscription",
}

// clientDocsRetiredFieldLedger — прощённые вхождения снятого поля в примере.
//
// ЗАПИСЬ ОБЯЗАНА ИМЕТЬ ПРЕДМЕТ. Запись, которой больше нечего прощать, —
// находка (TestClientDocsRetiredFieldLedgerHasSubject): пустая запись создаёт
// впечатление покрытия, которого нет, и переживает свой дефект.
//
// Ключ — «<путь страницы>#<ключ примера>».
var clientDocsRetiredFieldLedger = map[string]string{
	// Записей нет: предмет прежней записи снят пересборкой страницы в этой же
	// линии — пример приведён к контракту. Пустая ведомость есть ЦЕЛЬ, а не
	// поломка: гейт на ней проходит и печатает «записей 0».
}

// ── общее ───────────────────────────────────────────────────────────────────────

// ClientDocsContractDriftOptions — вход обоих анализаторов.
type ClientDocsContractDriftOptions struct {
	// Root — корень дерева.
	Root string
	// ProtoRoot — каталог контрактов относительно Root.
	ProtoRoot string
	// DomainAliases — как каталог сайта называется по отношению к каталогу
	// контракта, если имена расходятся. Единственный сегодня — балансировщик:
	// каталог сайта `nlb`, каталог контракта `loadbalancer`.
	DomainAliases map[string]string
}

var (
	// clientDocsSiteConfigName — признак сайта. Опознаётся КОНФИГОМ, а не именем
	// каталога: переименование каталога документации вывело бы все сайты из-под
	// гейта, и он отчитался бы «сайтов ноль» — отказом лишь потому, что пустой
	// обход объявлен отказом.
	clientDocsSiteConfigName = "docusaurus.config.ts"

	// clientDocsJSONFenceRe / clientDocsJSONBlockRe — два способа записать пример
	// JSON в этом дереве. Форма, о которой распознаватель не знает, — не край и
	// не редкость: всё записанное в ней оказалось бы ВНЕ НАБЛЮДЕНИЯ.
	clientDocsJSONFenceRe = regexp.MustCompile("(?s)```json\\s*\n(.*?)```")
	clientDocsJSONBlockRe = regexp.MustCompile(`(?s)<CodeBlock language="json">(.*?)</CodeBlock>`)
	clientDocsJSONKeyRe   = regexp.MustCompile(`"([A-Za-z_][A-Za-z0-9_]*)"\s*:`)

	// clientDocsProtoFieldRe — ОБЪЯВЛЕНИЕ поля сообщения.
	clientDocsProtoFieldRe = regexp.MustCompile(
		`(?m)^\s*(?:repeated\s+|optional\s+)?(?:map<[^>]+>|[\w.]+)\s+([a-z][A-Za-z0-9_]*)\s*=\s*\d+\s*(?:\[[^\]]*\])?;`)
	// clientDocsProtoReservedRe — резерв ИМЕНИ (не номера).
	clientDocsProtoReservedRe = regexp.MustCompile(`reserved\s+((?:"[A-Za-z_][A-Za-z0-9_]*"\s*,?\s*)+);`)
	clientDocsQuotedNameRe    = regexp.MustCompile(`"([A-Za-z_][A-Za-z0-9_]*)"`)

	// clientDocsAPIOperationRe — объявление блока операции на странице.
	clientDocsAPIOperationRe = regexp.MustCompile(`<ApiOperation\b[^>]*?endpoint="([^"]+)"[^>]*?>`)
	clientDocsAPIOperEndRe   = regexp.MustCompile(`</ApiOperation>`)

	// clientDocsDeprecationMarkRe — словарь пометки на клиентской поверхности.
	// Русская и англоязычная формы обе: корпус двуязычный, и предикат на одном
	// языке недобирает молча.
	clientDocsDeprecationMarkRe = regexp.MustCompile(
		`(?i)(к снятию|снят с контракта|устарел|deprecated)`)

	// clientDocsHTTPRuleRe — путь из аннотации `google.api.http`.
	clientDocsHTTPRuleRe = regexp.MustCompile(`(?:get|post|delete|patch|put):\s*"([^"]+)"`)
	// clientDocsRPCDeclRe — объявление глагола.
	clientDocsRPCDeclRe = regexp.MustCompile(`^rpc\s+([A-Za-z][A-Za-z0-9]*)\s*\(`)
	// clientDocsDeprecatedCommentRe — пометка в блоке комментариев ПЕРЕД глаголом.
	clientDocsDeprecatedCommentRe = regexp.MustCompile(`^//\s*DEPRECATED\b`)
)

// clientDocsCamel переводит имя поля контракта в форму провода (camelCase).
func clientDocsCamel(s string) string {
	parts := strings.Split(s, "_")
	var b strings.Builder
	for i, p := range parts {
		if p == "" {
			continue
		}
		if i == 0 || b.Len() == 0 {
			b.WriteString(p)
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]))
		b.WriteString(p[1:])
	}
	return b.String()
}

// clientDocsProtoDomains читает дерево контрактов и возвращает по каждому домену
// множество живых имён полей и множество забранных имён — оба в форме провода.
func clientDocsProtoDomains(opts ClientDocsContractDriftOptions) (
	live map[string]map[string]bool, reserved map[string]map[string]bool, files int, err error,
) {
	live = map[string]map[string]bool{}
	reserved = map[string]map[string]bool{}
	base := filepath.Join(opts.Root, opts.ProtoRoot, "kacho", "cloud")
	// Состав берётся ИЗ ИНДЕКСА, а не обходом диска: чтение внутри колбэка обхода
	// подвержено подмене пути символической ссылкой между шагом обхода и открытием
	// файла (G122). Индекс отдаёт готовый перечень, и файл открывается вне обхода.
	paths, err := treecorpus.UnderWithSuffix(base, ".proto")
	if err != nil {
		return nil, nil, 0, err
	}
	for _, p := range paths {
		rel, rerr := filepath.Rel(base, p)
		if rerr != nil {
			return nil, nil, files, rerr
		}
		domain := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]
		raw, rerr := os.ReadFile(p) // #nosec G304 -- путь из индекса собственного дерева
		if rerr != nil {
			return nil, nil, files, rerr
		}
		files++
		src := string(raw)
		if live[domain] == nil {
			live[domain] = map[string]bool{}
			reserved[domain] = map[string]bool{}
		}
		for _, m := range clientDocsProtoFieldRe.FindAllStringSubmatch(src, -1) {
			live[domain][clientDocsCamel(m[1])] = true
		}
		for _, m := range clientDocsProtoReservedRe.FindAllStringSubmatch(src, -1) {
			for _, n := range clientDocsQuotedNameRe.FindAllStringSubmatch(m[1], -1) {
				reserved[domain][clientDocsCamel(n[1])] = true
			}
		}
	}
	return live, reserved, files, nil
}

// clientDocsSite — один сайт документации и домен, о котором он говорит.
type clientDocsSite struct {
	Dir    string // относительно корня
	Domain string // имя каталога контракта
}

// clientDocsSites выводит перечень сайтов ИЗ ДЕРЕВА: рукописный список разошёлся
// бы с ним молча, и новый сайт остался бы вне гейта, ничем себя не выдав.
func clientDocsSites(opts ClientDocsContractDriftOptions) ([]clientDocsSite, error) {
	var out []clientDocsSite
	err := filepath.WalkDir(opts.Root, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "build", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != clientDocsSiteConfigName {
			return nil
		}
		rel, rerr := filepath.Rel(opts.Root, filepath.Dir(p))
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		seg := strings.Split(rel, "/")
		// Каталог компонента — на глубине 1 (`gateway/docs`) либо 2
		// (`services/<svc>/docs`); сайт лежит сразу под ним.
		var component string
		switch {
		case len(seg) >= 3 && seg[0] == "services":
			component = seg[1]
		case len(seg) >= 2:
			component = seg[0]
		default:
			return nil
		}
		domain := component
		if alias, ok := opts.DomainAliases[component]; ok {
			domain = alias
		}
		out = append(out, clientDocsSite{Dir: rel, Domain: domain})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dir < out[j].Dir })
	return out, nil
}

// clientDocsSitePages возвращает страницы сайта.
func clientDocsSitePages(root, siteDir string) ([]string, error) {
	var out []string
	base := filepath.Join(root, filepath.FromSlash(siteDir), "content")
	if _, err := os.Stat(base); err != nil {
		return nil, nil //nolint:nilerr // сайт без каталога страниц — не ошибка, а ноль страниц
	}
	err := filepath.WalkDir(base, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		switch filepath.Ext(p) {
		case ".mdx", ".md":
			rel, rerr := filepath.Rel(root, p)
			if rerr != nil {
				return rerr
			}
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

func clientDocsLineOf(src string, off int) int {
	return strings.Count(src[:off], "\n") + 1
}

// ── анализатор 1: снятое поле в примере ─────────────────────────────────────────

// ClientDocsRetiredFieldCensus — объём осмотренного.
type ClientDocsRetiredFieldCensus struct {
	ProtoFiles    int
	Sites         int
	Pages         int
	Examples      int
	KeysJudged    int
	RetiredNames  int
	LedgerEntries int
	LedgerUsed    int
}

// ClientDocsRetiredFieldFinding — одна находка.
type ClientDocsRetiredFieldFinding struct {
	File   string
	Line   int
	Key    string
	Domain string
}

func (f ClientDocsRetiredFieldFinding) String() string {
	return fmt.Sprintf("%s:%d: пример показывает %q — контракт домена %q это имя ЗАБРАЛ (reserved)",
		f.File, f.Line, f.Key, f.Domain)
}

// AuditClientDocsRetiredFieldInExample выносит вердикт о дереве.
func AuditClientDocsRetiredFieldInExample(
	opts ClientDocsContractDriftOptions,
	log io.Writer,
) ([]ClientDocsRetiredFieldFinding, ClientDocsRetiredFieldCensus, error) {
	var census ClientDocsRetiredFieldCensus
	census.LedgerEntries = len(clientDocsRetiredFieldLedger)

	live, reserved, protoFiles, err := clientDocsProtoDomains(opts)
	if err != nil {
		return nil, census, err
	}
	census.ProtoFiles = protoFiles
	for _, names := range reserved {
		census.RetiredNames += len(names)
	}

	sites, err := clientDocsSites(opts)
	if err != nil {
		return nil, census, err
	}
	census.Sites = len(sites)

	var findings []ClientDocsRetiredFieldFinding
	for _, site := range sites {
		shared := map[string]bool{}
		for _, d := range clientDocsSharedProtoDomains {
			for k := range live[d] {
				shared[k] = true
			}
		}
		for k := range live[site.Domain] {
			shared[k] = true
		}
		burned := reserved[site.Domain]

		pages, perr := clientDocsSitePages(opts.Root, site.Dir)
		if perr != nil {
			return nil, census, perr
		}
		for _, page := range pages {
			census.Pages++
			raw, rerr := os.ReadFile(filepath.Join(opts.Root, filepath.FromSlash(page))) // путь получен обходом собственного дерева
			if rerr != nil {
				return nil, census, rerr
			}
			src := string(raw)
			type block struct {
				off  int
				body string
			}
			var blocks []block
			for _, m := range clientDocsJSONFenceRe.FindAllStringSubmatchIndex(src, -1) {
				blocks = append(blocks, block{off: m[0], body: src[m[2]:m[3]]})
			}
			for _, m := range clientDocsJSONBlockRe.FindAllStringSubmatchIndex(src, -1) {
				blocks = append(blocks, block{off: m[0], body: src[m[2]:m[3]]})
			}
			for _, b := range blocks {
				census.Examples++
				seen := map[string]bool{}
				for _, km := range clientDocsJSONKeyRe.FindAllStringSubmatch(b.body, -1) {
					key := km[1]
					if seen[key] {
						continue
					}
					seen[key] = true
					census.KeysJudged++
					if !burned[key] || shared[key] {
						continue
					}
					if _, forgiven := clientDocsRetiredFieldLedger[page+"#"+key]; forgiven {
						census.LedgerUsed++
						continue
					}
					findings = append(findings, ClientDocsRetiredFieldFinding{
						File: page, Line: clientDocsLineOf(src, b.off), Key: key, Domain: site.Domain,
					})
				}
			}
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})

	if log != nil {
		_, _ = fmt.Fprintf(log, "снятое поле в примере: файлов контракта %d · забранных имён %d · "+
			"сайтов %d · страниц %d · примеров JSON %d · ключей рассужено %d · "+
			"записей ведомости %d (использовано %d) · находок %d\n",
			census.ProtoFiles, census.RetiredNames, census.Sites, census.Pages,
			census.Examples, census.KeysJudged, census.LedgerEntries, census.LedgerUsed,
			len(findings))
	}
	return findings, census, nil
}

// ── анализатор 2: помеченное к снятию подаётся действующим ──────────────────────

// ClientDocsDeprecationCensus — объём осмотренного.
type ClientDocsDeprecationCensus struct {
	ProtoFiles      int
	DeprecatedPaths int
	Sites           int
	Pages           int
	Blocks          int
	BlocksJudged    int
}

// ClientDocsDeprecationFinding — одна находка.
type ClientDocsDeprecationFinding struct {
	File     string
	Line     int
	Endpoint string
	RPC      string
}

func (f ClientDocsDeprecationFinding) String() string {
	return fmt.Sprintf("%s:%d: %s документируется как действующий — контракт помечает %s к снятию",
		f.File, f.Line, f.Endpoint, f.RPC)
}

// clientDocsDeprecatedPaths — REST-пути глаголов, помеченных контрактом к снятию.
//
// Читается ОБЪЯВЛЕНИЕ: пометка засчитывается только из блока комментариев,
// непосредственно предшествующего `rpc`, а путь — только из тела этого же `rpc`.
func clientDocsDeprecatedPaths(opts ClientDocsContractDriftOptions) (map[string]string, int, error) {
	out := map[string]string{}
	files := 0
	base := filepath.Join(opts.Root, opts.ProtoRoot, "kacho", "cloud")
	// Состав из индекса, а не обходом диска — та же причина, что у соседней функции:
	// чтение внутри колбэка обхода подвержено подмене пути символической ссылкой.
	paths, err := treecorpus.UnderWithSuffix(base, ".proto")
	if err != nil {
		return nil, 0, err
	}
	for _, p := range paths {
		raw, rerr := os.ReadFile(p) // #nosec G304 -- путь из индекса собственного дерева
		if rerr != nil {
			return nil, files, rerr
		}
		files++
		deprecated, inRPC := false, false
		name := ""
		for _, ln := range strings.Split(string(raw), "\n") {
			st := strings.TrimSpace(ln)
			switch {
			case strings.HasPrefix(st, "//"):
				if clientDocsDeprecatedCommentRe.MatchString(st) {
					deprecated = true
				}
			case clientDocsRPCDeclRe.MatchString(st):
				name = clientDocsRPCDeclRe.FindStringSubmatch(st)[1]
				inRPC = true
			case inRPC:
				if m := clientDocsHTTPRuleRe.FindStringSubmatch(st); m != nil && deprecated {
					if _, ok := out[m[1]]; !ok {
						out[m[1]] = name
					}
				}
				if st == "}" {
					inRPC, deprecated, name = false, false, ""
				}
			case st == "":
				// пустая строка блок комментариев не рвёт
			default:
				deprecated = false
			}
		}
	}
	return out, files, nil
}

// AuditClientDocsDeprecationParity выносит вердикт о дереве.
func AuditClientDocsDeprecationParity(
	opts ClientDocsContractDriftOptions,
	log io.Writer,
) ([]ClientDocsDeprecationFinding, ClientDocsDeprecationCensus, error) {
	var census ClientDocsDeprecationCensus

	paths, protoFiles, err := clientDocsDeprecatedPaths(opts)
	if err != nil {
		return nil, census, err
	}
	census.ProtoFiles = protoFiles
	census.DeprecatedPaths = len(paths)

	sites, err := clientDocsSites(opts)
	if err != nil {
		return nil, census, err
	}
	census.Sites = len(sites)

	var findings []ClientDocsDeprecationFinding
	for _, site := range sites {
		pages, perr := clientDocsSitePages(opts.Root, site.Dir)
		if perr != nil {
			return nil, census, perr
		}
		for _, page := range pages {
			census.Pages++
			raw, rerr := os.ReadFile(filepath.Join(opts.Root, filepath.FromSlash(page))) // путь получен обходом собственного дерева
			if rerr != nil {
				return nil, census, rerr
			}
			src := string(raw)
			ends := clientDocsAPIOperEndRe.FindAllStringIndex(src, -1)
			for _, m := range clientDocsAPIOperationRe.FindAllStringSubmatchIndex(src, -1) {
				census.Blocks++
				endpoint := src[m[2]:m[3]]
				rpcName, marked := paths[endpoint]
				if !marked {
					continue
				}
				census.BlocksJudged++
				body := src[m[0]:]
				for _, e := range ends {
					if e[0] > m[1] {
						body = src[m[0]:e[1]]
						break
					}
				}
				if clientDocsDeprecationMarkRe.MatchString(body) {
					continue
				}
				findings = append(findings, ClientDocsDeprecationFinding{
					File: page, Line: clientDocsLineOf(src, m[0]), Endpoint: endpoint, RPC: rpcName,
				})
			}
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})

	if log != nil {
		_, _ = fmt.Fprintf(log, "пометка к снятию: файлов контракта %d · помеченных путей %d · "+
			"сайтов %d · страниц %d · блоков операции %d · из них о помеченном пути %d · находок %d\n",
			census.ProtoFiles, census.DeprecatedPaths, census.Sites, census.Pages,
			census.Blocks, census.BlocksJudged, len(findings))
	}
	return findings, census, nil
}
