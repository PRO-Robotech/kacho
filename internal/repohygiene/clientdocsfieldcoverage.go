// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clientdocsfieldcoverage.go — анализатор «страница ресурса называет каждое
// поле, которое несёт его контракт».
//
// # Предмет
//
// Поле, которое приходит в ответе края и не названо страницей, вызывающий
// читает как случайность: он не знает ни что оно значит, ни на что опираться.
// Это близнец класса «принято-и-проигнорировано» с другой стороны шва: там
// продукт обещает возможность, которой нет, здесь — отдаёт величину, о которой
// молчит. Оба расхождения между контрактом и его описанием, и оба тихие: ни
// сборка сайта, ни обзор изменения их не видят.
//
// Особенно это верно для пары производных наборов роли: `effectiveVerbs`
// ОТЛИЧАЕТСЯ от `authoredVerbs` (роль-редактор несёт производный `delete*`), и
// страница, не объяснившая различия, оставляет вызывающего гадать, какой из
// двух наборов исполняется.
//
// # Что судится и как выводится пара «страница ↔ сообщение»
//
// Страница `services/<сервис>/docs/content/api/<имя>.mdx` сопоставляется
// сообщению `<Имя>` (дефисное имя файла к верхнему регистру по сегментам),
// найденному ВЕРХНИМ уровнем контрактов ЭТОГО домена
// (`proto/kacho/cloud/<домен>/v1/*.proto`). Ограничение доменом намеренное:
// без него `Registry` края и `Registry` реестра были бы одним предметом.
//
// Поле сообщения — поле верхнего уровня ЛИБО ветвь `oneof` верхнего уровня:
// ветвь такое же поле контракта, и вызывающий получает её тем же чтением.
// Поля вложенных сообщений (`Rule`, `Target`) судятся вместе со своим
// сообщением, а не с объемлющим, — иначе `Instance` отвечал бы за поля
// четырёх чужих форм.
//
// Названным считается поле, чьё имя встречено на странице в виде кода — в
// camelCase (форма JSON края, которую и видит вызывающий) либо в исходной
// форме контракта. Требование «код, а не проза» намеренно: слово `status` в
// предложении о состоянии ресурса не является упоминанием ПОЛЯ, и гейт,
// принимавший бы прозу, зеленел бы на любой странице.
//
// # ЧЕГО ОН НЕ СУДИТ
//
//  1. СТРАНИЦА БЕЗ СООБЩЕНИЯ не судится вовсе: обзор, операции, пределы,
//     токены, внутренняя поверхность. Их число печатает перепись, поэтому
//     слепая зона видна, а не подразумевается.
//  2. ПОЛНОТА ОПИСАНИЯ не судится — только НАЗВАННОСТЬ. «Поле названо, но
//     объяснено неверно» есть другой предикат, и машинного признака у него нет.
//  3. СНЯТОЕ поле не судится by construction: `reserved` полем не является.
//  4. ПОЛЕ, ПОМЕЧЕННОЕ `[deprecated = true]`, не судится, и число таких печатает
//     перепись. Граница машинная, а не прощение по имени: пометка есть свойство
//     самого контракта, и продукт ею просит вызывающего полем НЕ пользоваться.
//     Требовать строку таблицы о том, что не производится, значило бы
//     предлагать снятую поверхность наравне с живой. Обратная сторона названа
//     честно: пометка молчание покупает — но покупается она правкой контракта,
//     видимой в его же диффе и в проверке совместимости, а не строкой в списке
//     прощённых. Сегодня такое поле в дереве ОДНО (`Role.permissions`:
//     внутренняя скомпилированная форма, в ответе пуста, на входе отвергается
//     `INVALID_ARGUMENT`).
//
// # Ведомости исключений у гейта НЕТ, и это решение
//
// Ведомость означала бы отложенную часть: страница, которой прощено молчание,
// молчит ровно о том, ради чего гейт заведён, и снять запись некому. На день
// заведения класс закрыт целиком — семнадцать полей на восьми страницах, — и
// потому у послабления нет ни одного предмета. Появится поле, которое назвать
// нечем, — это находка о контракте, а не повод завести список прощённых.
//
// # Падает на ПУСТОМ ОБХОДЕ
//
// Ноль прочитанных контрактов, ноль страниц, ноль сопоставленных сообщений либо
// ноль рассуженных полей — «находок ноль» неотличимо от «прочитано ноль».
package repohygiene

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ClientDocsFieldCoverageOptions — вход анализатора.
type ClientDocsFieldCoverageOptions struct {
	// Root — корень дерева.
	Root string
	// ProtoRoot — каталог контрактов относительно Root.
	ProtoRoot string
	// ServicesRoot — каталог сервисов относительно Root.
	ServicesRoot string
	// DomainAliases — домен контракта для сервиса, чей каталог назван иначе.
	// Единственный сегодня — балансировщик: каталог сервиса `nlb`, каталог
	// контракта `loadbalancer`.
	DomainAliases map[string]string
}

// ClientDocsFieldCoverageCensus — объём осмотренного.
type ClientDocsFieldCoverageCensus struct {
	ProtoFiles    int
	DocPages      int
	PagesJudged   int
	PagesOutside  int
	FieldsJudged  int
	FieldsNamed   int
	FieldsRetired int
	OutsidePages  []string
}

func (c ClientDocsFieldCoverageCensus) String() string {
	return fmt.Sprintf(
		"контрактов прочитано %d · страниц документации %d · из них сопоставлено сообщению %d "+
			"(вне охвата %d) · полей рассужено %d · названо %d · снятых с поверхности "+
			"(deprecated) %d — не судятся",
		c.ProtoFiles, c.DocPages, c.PagesJudged, c.PagesOutside, c.FieldsJudged, c.FieldsNamed,
		c.FieldsRetired)
}

// ClientDocsFieldCoverageFinding — одна находка: поле контракта, которого
// страница не называет.
type ClientDocsFieldCoverageFinding struct {
	Page    string
	Message string
	Field   string
}

func (f ClientDocsFieldCoverageFinding) String() string {
	return fmt.Sprintf("%s: сообщение %s несёт поле %s, страница его не называет",
		f.Page, f.Message, f.Field)
}

var (
	// clientDocsFieldMessageRe — ОБЪЯВЛЕНИЕ сообщения верхнего уровня. Читается
	// объявление, а не упоминание: имя сообщения встречается и в комментариях.
	clientDocsFieldMessageRe = regexp.MustCompile(`^message ([A-Za-z][A-Za-z0-9_]*) \{`)
	// clientDocsFieldOneofRe — объявление ветвления. Его ветви — такие же поля
	// контракта, и вызывающий получает их тем же чтением.
	clientDocsFieldOneofRe = regexp.MustCompile(`^oneof\s+[a-z][a-z0-9_]*\s*\{`)
	// clientDocsFieldRe — объявление поля. Хвостовые опции (`[deprecated = true]`)
	// допускаются: снятым поле от них не становится.
	clientDocsFieldRe = regexp.MustCompile(
		`^(?:repeated\s+|optional\s+)?(?:map<[^>]+>|[A-Za-z0-9_.]+)\s+([a-z][a-z0-9_]*)\s*=\s*\d+\s*(\[[^\]]*\])?\s*;`)
	// clientDocsFieldDeprecatedRe — пометка снятия с поверхности в опциях поля.
	clientDocsFieldDeprecatedRe = regexp.MustCompile(`\bdeprecated\s*=\s*true\b`)
)

// AuditClientDocsFieldCoverage выносит вердикт о дереве.
func AuditClientDocsFieldCoverage(
	opts ClientDocsFieldCoverageOptions,
	log io.Writer,
) ([]ClientDocsFieldCoverageFinding, ClientDocsFieldCoverageCensus, error) {
	var census ClientDocsFieldCoverageCensus

	pages, err := clientDocsAPIPages(opts)
	if err != nil {
		return nil, census, err
	}
	census.DocPages = len(pages)

	byDomain := map[string]map[string][]clientDocsField{}
	var findings []ClientDocsFieldCoverageFinding

	for _, p := range pages {
		domain := opts.DomainAliases[p.service]
		if domain == "" {
			domain = p.service
		}
		msgs, ok := byDomain[domain]
		if !ok {
			read, files, merr := clientDocsDomainMessages(opts, domain)
			if merr != nil {
				return nil, census, merr
			}
			byDomain[domain] = read
			msgs = read
			census.ProtoFiles += files
		}

		fields, known := msgs[p.message]
		if !known {
			census.PagesOutside++
			census.OutsidePages = append(census.OutsidePages, p.rel)
			continue
		}
		census.PagesJudged++

		// #nosec G304 -- путь получен обходом дерева документации ЭТОГО репозитория
		raw, rerr := os.ReadFile(filepath.Join(opts.Root, filepath.FromSlash(p.rel)))
		if rerr != nil {
			return nil, census, fmt.Errorf("%s: %w", p.rel, rerr)
		}
		text := string(raw)
		for _, f := range fields {
			if f.retired {
				census.FieldsRetired++
				continue
			}
			census.FieldsJudged++
			if clientDocsPageNamesField(text, f.name) {
				census.FieldsNamed++
				continue
			}
			findings = append(findings, ClientDocsFieldCoverageFinding{
				Page: p.rel, Message: p.message, Field: clientDocsCamel(f.name),
			})
		}
	}

	sort.Strings(census.OutsidePages)
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Page != findings[j].Page {
			return findings[i].Page < findings[j].Page
		}
		return findings[i].Field < findings[j].Field
	})

	if log != nil {
		_, _ = fmt.Fprintf(log, "перепись: %s\n", census)
		if len(census.OutsidePages) > 0 {
			_, _ = fmt.Fprintf(log, "  вне охвата (сообщения с таким именем в домене нет): %s\n",
				strings.Join(census.OutsidePages, " "))
		}
	}

	switch {
	case census.ProtoFiles == 0:
		return findings, census, fmt.Errorf("прочитано ноль контрактов в %s: сверять не с чем", opts.ProtoRoot)
	case census.DocPages == 0:
		return findings, census, fmt.Errorf("прочитано ноль страниц документации в %s: сверять нечего", opts.ServicesRoot)
	case census.PagesJudged == 0:
		return findings, census, fmt.Errorf("ни одна страница не сопоставлена сообщению: правило имени страницы разошлось с деревом")
	case census.FieldsJudged == 0:
		return findings, census, fmt.Errorf("рассужено ноль полей: разбор контракта перестал видеть объявления полей")
	}
	return findings, census, nil
}

// clientDocsAPIPage — страница документации и то, чему она сопоставляется.
type clientDocsAPIPage struct {
	rel     string
	service string
	message string
}

// clientDocsAPIPages обходит страницы разделов API сайтов сервисов.
func clientDocsAPIPages(opts ClientDocsFieldCoverageOptions) ([]clientDocsAPIPage, error) {
	servicesDir := filepath.Join(opts.Root, filepath.FromSlash(opts.ServicesRoot))
	entries, err := os.ReadDir(servicesDir)
	if err != nil {
		return nil, fmt.Errorf("каталог сервисов %s: %w", opts.ServicesRoot, err)
	}
	var out []clientDocsAPIPage
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		svc := e.Name()
		apiDir := filepath.Join(servicesDir, svc, "docs", "content", "api")
		files, rerr := os.ReadDir(apiDir)
		if rerr != nil {
			continue // у сервиса нет сайта документации — не находка
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".mdx") {
				continue
			}
			base := strings.TrimSuffix(f.Name(), ".mdx")
			out = append(out, clientDocsAPIPage{
				rel:     path4(opts.ServicesRoot, svc, "docs/content/api", f.Name()),
				service: svc,
				message: clientDocsPascal(base),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].rel < out[j].rel })
	return out, nil
}

func path4(a, b, c, d string) string { return strings.Join([]string{a, b, c, d}, "/") }

// clientDocsDomainMessages читает сообщения верхнего уровня домена контрактов.
func clientDocsDomainMessages(
	opts ClientDocsFieldCoverageOptions, domain string,
) (map[string][]clientDocsField, int, error) {
	dir := filepath.Join(opts.Root, filepath.FromSlash(opts.ProtoRoot), domain, "v1")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return map[string][]clientDocsField{}, 0, nil // домена контрактов нет — страницы просто вне охвата
	}
	out := map[string][]clientDocsField{}
	files := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".proto") {
			continue
		}
		// #nosec G304 -- путь получен обходом дерева контрактов ЭТОГО репозитория
		raw, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil {
			return nil, files, fmt.Errorf("%s: %w", e.Name(), rerr)
		}
		files++
		for name, fields := range clientDocsTopLevelMessages(string(raw)) {
			if _, seen := out[name]; !seen {
				out[name] = fields
			}
		}
	}
	return out, files, nil
}

// clientDocsTopLevelMessages разбирает сообщения верхнего уровня одного файла
// контракта и их поля — включая ветви `oneof` верхнего уровня.
func clientDocsTopLevelMessages(src string) map[string][]clientDocsField {
	out := map[string][]clientDocsField{}
	lines := strings.Split(src, "\n")
	for i := 0; i < len(lines); i++ {
		m := clientDocsFieldMessageRe.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		name := m[1]
		var (
			fields     []clientDocsField
			depth      = 1
			oneofDepth = 0
			j          = i + 1
		)
		for ; j < len(lines) && depth > 0; j++ {
			s := strings.TrimSpace(lines[j])
			if strings.HasPrefix(s, "//") {
				continue
			}
			if depth == 1 && clientDocsFieldOneofRe.MatchString(s) {
				oneofDepth = 2
			}
			if depth == 1 || (oneofDepth != 0 && depth == oneofDepth) {
				if fm := clientDocsFieldRe.FindStringSubmatch(s); fm != nil {
					fields = append(fields, clientDocsField{
						name:    fm[1],
						retired: clientDocsFieldDeprecatedRe.MatchString(fm[2]),
					})
				}
			}
			depth += strings.Count(lines[j], "{") - strings.Count(lines[j], "}")
			if oneofDepth != 0 && depth < oneofDepth {
				oneofDepth = 0
			}
		}
		out[name] = fields
		i = j - 1
	}
	return out
}

// clientDocsField — поле сообщения и то, снято ли оно с поверхности.
type clientDocsField struct {
	name    string
	retired bool
}

// clientDocsPageNamesField — названо ли поле страницей. Признаётся КОД, а не
// проза: слово в предложении упоминанием поля не является.
func clientDocsPageNamesField(page, field string) bool {
	for _, form := range []string{clientDocsCamel(field), field} {
		if strings.Contains(page, "<code>"+form+"</code>") ||
			strings.Contains(page, "`"+form+"`") {
			return true
		}
	}
	return false
}

// clientDocsCamel — форма имени поля в JSON края: её и видит вызывающий.
func clientDocsCamel(field string) string {
	parts := strings.Split(field, "_")
	out := parts[0]
	for _, p := range parts[1:] {
		if p == "" {
			continue
		}
		out += strings.ToUpper(p[:1]) + p[1:]
	}
	return out
}

// clientDocsPascal — имя сообщения из дефисного имени файла страницы.
func clientDocsPascal(base string) string {
	var out string
	for _, p := range strings.Split(base, "-") {
		if p == "" {
			continue
		}
		out += strings.ToUpper(p[:1]) + p[1:]
	}
	return out
}
