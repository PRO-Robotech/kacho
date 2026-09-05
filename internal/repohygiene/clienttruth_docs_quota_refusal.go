// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// Отказ по исчерпанию предела описан у КАЖДОГО владельца учёта — и только у него.
//
// ПРЕДМЕТ. Клиент, упёршийся в потолок, получает `429` и идёт читать, что
// произошло. Если сайт домена об этом отказе молчит, единственный способ узнать —
// спросить у нас; поверхность, по которой предел читается, при этом существует и
// не названа. Обратная сторона того же предмета сильнее: домен, учёта НЕ ведущий,
// описывать отказ не вправе — страница, обещающая отказ, которого домен не
// производит, лжёт ровно так же, как молчание о живом, и обходится дороже
// (клиент строит обработку исхода, который не наступит никогда).
//
// ПЕРЕЧЕНЬ ВЛАДЕЛЬЦЕВ ВЫВОДИТСЯ, А НЕ ВЫПИСЫВАЕТСЯ. Он приходит из
// `pkg/quota.RefusalOwners()` — того же объявления, из которого рендерятся файлы
// миграций отказа. Рукописная копия разошлась бы с деревом молча: в этом дереве
// такое уже случалось с перечнем репозиториев, и разошлись сразу три копии.
//
// ЛОВУШКА, РАДИ КОТОРОЙ ГЕЙТ И НАПИСАН. Таблицу кодов сайты подают в ДВУХ
// формах, и наивный распознаватель слеп ко второй by construction:
//
//	литералом      <tr><td><code>RESOURCE_EXHAUSTED</code></td>…  — iam, nlb, registry
//	компонентом    <Codes codes={['…', 'resourceExhausted', …]} /> — vpc, compute, storage
//	               поверх словаря services/<домен>/docs/src/constants/codes.ts
//
// У второй формы литерала `RESOURCE_EXHAUSTED` на странице НЕТ и быть не может.
// Гейт, ищущий литерал в `.mdx`, напечатал бы по трём сайтам «ноль находок», и
// это означало бы «ноль прочитанного» (п.7 §«Гейт на класс»: форма, о которой
// распознаватель не знает, — не редкость, а невидимость).
//
// И этого мало: ключ бывает объявлен в словаре, но НЕ включён в массив
// `codes={[…]}` — тогда таблица строку не рендерит, клиент отказа не видит, а
// поиск по словарю его находит. Поэтому от компонентной формы требуются ОБА:
// ключ в словаре И его вхождение в массив.
//
// ПОЧЕМУ ПРЕДМЕТ — ТАБЛИЦА, А НЕ ПРОЗА РЯДОМ. Абзац про `RESOURCE_EXHAUSTED`
// стоит на всех шести сайтах и остался бы на месте, выпади ключ из массива
// `codes={[…]}`: исчезает СТРОКА ТАБЛИЦЫ, а не абзац. Гейт по прозе был бы
// зелёным ровно на том дефекте, ради которого написан, — поэтому он судит
// таблицу.
//
// ПОВЕРХНОСТЬ ЧТЕНИЯ выводится из контракта: путь `google.api.http` контракта
// квот домена обязан дословно встречаться на его страницах. Соответствие
// «домен → его контракт» тоже НЕ выписано — оно берётся из первого сегмента
// самого пути (`/vpc/v1/quotas` → `vpc`), поэтому каталог контракта может
// называться как угодно (`loadbalancer` у nlb, `quota` у iam). Домен, чьего пути
// в контрактах не нашлось, даёт находку предпосылки, а не молчание.
//
// ЧЕМ ЭТОТ ГЕЙТ НЕ ЯВЛЯЕТСЯ. Он не судит, ВЕРНО ли описан отказ, не читает
// текст описания и не проверяет, что страница пределов вообще существует, —
// только то, что таблица кодов несёт строку отказа, что путь чтения назван, и
// что домен без учёта отказа не обещает. Он не смотрит на `gateway/docs`: край
// не домен, учёта не ведёт и ресурсов не владеет, а требование к нему было бы
// требованием к другому предмету.

var (
	// Строка таблицы кодов, поданная литералом.
	docsQuotaLiteralRowRe = regexp.MustCompile(`<tr>.*<code>RESOURCE_EXHAUSTED</code>`)

	// Вызов компонента таблицы кодов. Массив читается целиком, включая перенос
	// строки: форма записи принадлежит автору страницы, а не гейту.
	docsQuotaCodesCallRe = regexp.MustCompile(`(?s)<Codes\b[^>]*?codes=\{\[(.*?)\]\}`)

	// Элемент массива: 'invalidArgument' | "notFound" | `internal`.
	docsQuotaCodesItemRe = regexp.MustCompile("['\"`]([A-Za-z][A-Za-z0-9_]*)['\"`]")

	// Ключ словаря кодов: две ведущих позиции отступа, затем имя и открывающая скобка.
	docsQuotaDictKeyRe = regexp.MustCompile(`(?m)^\s{2}([A-Za-z][A-Za-z0-9_]*)\s*:\s*\{`)

	// gRPC-код внутри записи словаря.
	docsQuotaDictGrpcRe = regexp.MustCompile(`(?m)^\s*grpc\s*:\s*['"]([A-Z][A-Z0-9_]*)['"]`)

	// Путь google.api.http в контракте — и в одну строку, и блоком.
	docsQuotaHTTPRuleRe = regexp.MustCompile(`(?m)(get|post|put|patch|delete)\s*:\s*"([^"]+)"`)

	// Первый сегмент пути: он же имя каталога домена под services/.
	docsQuotaPathHeadRe = regexp.MustCompile(`^/([a-z0-9][a-z0-9-]*)/`)
)

// docsQuotaRefusalCode — код, чья строка обязана стоять в таблице кодов владельца.
const docsQuotaRefusalCode = "RESOURCE_EXHAUSTED"

// docsQuotaTableForm — как сайт подаёт таблицу кодов.
type docsQuotaTableForm int

const (
	docsQuotaFormAbsent    docsQuotaTableForm = iota // строки отказа в таблице нет
	docsQuotaFormLiteral                             // литерал в overview.mdx
	docsQuotaFormComponent                           // <Codes codes={…}/> поверх словаря
)

func (f docsQuotaTableForm) String() string {
	switch f {
	case docsQuotaFormLiteral:
		return "литералом"
	case docsQuotaFormComponent:
		return "компонентом"
	default:
		return "нет"
	}
}

// docsQuotaSite — что установлено про один сайт документации.
type docsQuotaSite struct {
	Service string
	Owner   bool

	OverviewPath string // "" — страницы обзора у сайта нет
	Form         docsQuotaTableForm
	FormAt       string // координата строки таблицы либо вызова компонента

	// Компонентная форма: обе половины, потому что порознь каждая зеленеет.
	HasCodesCall bool     // на странице обзора стоит вызов <Codes codes={…}/>
	DictKey      string   // ключ словаря, чей grpc == RESOURCE_EXHAUSTED
	DictAt       string   // координата этого ключа
	CodesArray   []string // элементы массива codes={…} на странице обзора
	InCodesArray bool     // ключ словаря входит в массив

	// Поверхность чтения пределов.
	ContractPaths []string          // конкретные пути (без подстановок)
	ParamPaths    []string          // пути с подстановкой — в требование не идут
	PathShownAt   map[string]string // путь → координата, где он встречен дословно

	// Зеркало: чем домен БЕЗ учёта обещает отказ.
	RefusalMentions []string
}

// docsQuotaCensus — объём осмотренного.
type docsQuotaCensus struct {
	ProtoFiles    int
	DocFiles      int
	DictFiles     int
	Sites         []docsQuotaSite
	Owners        int
	NonOwners     int
	FormLiteral   int
	FormComponent int
	Described     int
}

// collectDocsQuotaRefusal — состав фактов по каждому сайту документации.
//
// Состав дерева приходит СОСТАВЛЕННЫМ (`treecorpus.Tree`): гейт берёт индекс
// git, инъекционная проба — синтетическое дерево. Перечень владельцев подаёт
// вызывающий: в гейте это `quota.RefusalOwners()`, в инъекции — синтетический
// перечень, иначе полосу «домен без учёта» нечем было бы проверить.
func collectDocsQuotaRefusal(tree *treecorpus.Tree, owners []string) (docsQuotaCensus, error) {
	var c docsQuotaCensus

	isOwner := map[string]bool{}
	for _, o := range owners {
		isOwner[o] = true
	}

	// Пути чтения пределов — из контрактов квот, сгруппированные по первому
	// сегменту пути.
	byHead := map[string][]string{}
	for _, rel := range clientTruthTreeFiles(tree, "proto/kacho/cloud", true, ".proto") {
		if !strings.Contains(strings.ToLower(rel), "quota") {
			continue
		}
		body, err := clientTruthReadTreeFile(tree, rel)
		if err != nil {
			return c, err
		}
		c.ProtoFiles++
		for _, m := range docsQuotaHTTPRuleRe.FindAllStringSubmatch(string(body), -1) {
			p := m[2]
			h := docsQuotaPathHeadRe.FindStringSubmatch(p)
			if h == nil {
				continue
			}
			byHead[h[1]] = append(byHead[h[1]], p)
		}
	}

	// Какие сайты вообще есть в дереве.
	services := map[string]bool{}
	for rel := range tree.Files() {
		if !strings.HasPrefix(rel, "services/") {
			continue
		}
		seg := strings.Split(rel, "/")
		if len(seg) < 4 || seg[2] != "docs" || seg[3] != "content" {
			continue
		}
		services[seg[1]] = true
	}
	for _, o := range owners {
		// Владелец без сайта тоже обязан быть увиден: молчание о нём было бы
		// тем же «ноль прочитанного».
		services[o] = true
	}

	names := make([]string, 0, len(services))
	for s := range services {
		names = append(names, s)
	}
	sort.Strings(names)

	for _, svc := range names {
		site := docsQuotaSite{
			Service:     svc,
			Owner:       isOwner[svc],
			PathShownAt: map[string]string{},
		}
		for _, p := range byHead[svc] {
			if strings.ContainsAny(p, "{}") {
				site.ParamPaths = append(site.ParamPaths, p)
				continue
			}
			site.ContractPaths = append(site.ContractPaths, p)
		}
		sort.Strings(site.ContractPaths)
		sort.Strings(site.ParamPaths)

		// Словарь кодов сайта: ключ выводится по объявленному gRPC-коду, а не
		// выписывается — имя ключа принадлежит автору словаря.
		dictRel := "services/" + svc + "/docs/src/constants/codes.ts"
		if tree.HasFile(dictRel) {
			body, err := clientTruthReadTreeFile(tree, dictRel)
			if err != nil {
				return c, err
			}
			c.DictFiles++
			if key, line := docsQuotaDictKeyForCode(string(body), docsQuotaRefusalCode); key != "" {
				site.DictKey, site.DictAt = key, fmt.Sprintf("%s:%d", dictRel, line)
			}
		}

		// Страница обзора: обе формы подачи таблицы кодов.
		overviewRel := "services/" + svc + "/docs/content/api/overview.mdx"
		if tree.HasFile(overviewRel) {
			site.OverviewPath = overviewRel
			body, err := clientTruthReadTreeFile(tree, overviewRel)
			if err != nil {
				return c, err
			}
			for i, ln := range strings.Split(string(body), "\n") {
				if docsQuotaLiteralRowRe.MatchString(ln) {
					site.Form = docsQuotaFormLiteral
					site.FormAt = fmt.Sprintf("%s:%d", overviewRel, i+1)
					break
				}
			}
			if m := docsQuotaCodesCallRe.FindStringSubmatchIndex(string(body)); m != nil {
				site.HasCodesCall = true
				inner := string(body)[m[2]:m[3]]
				for _, it := range docsQuotaCodesItemRe.FindAllStringSubmatch(inner, -1) {
					site.CodesArray = append(site.CodesArray, it[1])
				}
				if site.DictKey != "" {
					for _, k := range site.CodesArray {
						if k == site.DictKey {
							site.InCodesArray = true
						}
					}
				}
				// Форма подачи определяется НАЛИЧИЕМ вызова компонента, а не
				// успехом сверки: иначе сломанная компонентная форма приходила бы
				// в отчёт как «формы нет вовсе», и находка называла бы не тот
				// предмет — а на непонятную находку тратят прогон и снимают гейт.
				if site.Form == docsQuotaFormAbsent {
					site.Form = docsQuotaFormComponent
					site.FormAt = fmt.Sprintf("%s:%d", overviewRel,
						1+strings.Count(string(body)[:m[0]], "\n"))
				}
			}
		}

		// Страницы сайта: где путь чтения назван и чем домен без учёта обещает отказ.
		for _, rel := range clientTruthTreeFiles(tree, "services/"+svc+"/docs/content", true, ".mdx", ".md") {
			body, err := clientTruthReadTreeFile(tree, rel)
			if err != nil {
				return c, err
			}
			c.DocFiles++
			for i, ln := range strings.Split(string(body), "\n") {
				at := fmt.Sprintf("%s:%d", rel, i+1)
				for _, p := range site.ContractPaths {
					if _, seen := site.PathShownAt[p]; !seen && strings.Contains(ln, p) {
						site.PathShownAt[p] = at
					}
				}
				if !site.Owner && strings.Contains(ln, docsQuotaRefusalCode) {
					site.RefusalMentions = append(site.RefusalMentions, at)
				}
			}
		}
		if !site.Owner && site.DictKey != "" {
			site.RefusalMentions = append(site.RefusalMentions, site.DictAt)
		}

		switch {
		case site.Owner:
			c.Owners++
		default:
			c.NonOwners++
		}
		switch site.Form {
		case docsQuotaFormLiteral:
			c.FormLiteral++
		case docsQuotaFormComponent:
			c.FormComponent++
		}
		if site.Owner && docsQuotaTableCarriesRefusal(site) {
			c.Described++
		}
		c.Sites = append(c.Sites, site)
	}
	return c, nil
}

// docsQuotaTableCarriesRefusal — рендерит ли таблица кодов строку отказа.
//
// У компонентной формы требуются ОБЕ половины: ключ в словаре и его вхождение в
// массив. Ключ без вхождения — ровно тот случай, который наивный гейт пропускает:
// поиск по словарю его находит, а клиент строки не видит.
func docsQuotaTableCarriesRefusal(s docsQuotaSite) bool {
	switch s.Form {
	case docsQuotaFormLiteral:
		return true
	case docsQuotaFormComponent:
		return s.DictKey != "" && s.InCodesArray
	default:
		return false
	}
}

// docsQuotaDictKeyForCode — ключ словаря, чья запись объявляет заданный gRPC-код.
//
// Возвращается ключ и номер строки. Имя ключа не выписано здесь намеренно: оно
// принадлежит автору словаря, а гейт спрашивает про КОД, который обязан быть
// показан. Совпадение имён `resourceExhausted` ↔ `RESOURCE_EXHAUSTED` есть
// соглашение сайта, а не свойство дерева.
func docsQuotaDictKeyForCode(body, grpcCode string) (string, int) {
	lines := strings.Split(body, "\n")
	cur, curLine := "", 0
	for i, ln := range lines {
		if m := docsQuotaDictKeyRe.FindStringSubmatch(ln + "\n"); m != nil {
			cur, curLine = m[1], i+1
			continue
		}
		if m := docsQuotaDictGrpcRe.FindStringSubmatch(ln + "\n"); m != nil && cur != "" {
			if m[1] == grpcCode {
				return cur, curLine
			}
		}
	}
	return "", 0
}

// docsQuotaRefusalFindings — что не сходится.
func docsQuotaRefusalFindings(c docsQuotaCensus) []string {
	var out []string
	for _, s := range c.Sites {
		if !s.Owner {
			// Зеркало: домен без учёта отказа не производит и обещать его не вправе.
			for _, at := range s.RefusalMentions {
				out = append(out, fmt.Sprintf(
					"%s: домен учёта НЕ ведёт (его нет в quota.RefusalOwners), а %s обещает "+
						"отказ %s — клиент строит обработку исхода, которого не будет",
					s.Service, at, docsQuotaRefusalCode))
			}
			continue
		}

		if s.OverviewPath == "" {
			out = append(out, fmt.Sprintf(
				"%s: владелец учёта, а страницы обзора API на сайте нет — таблице кодов негде стоять",
				s.Service))
		} else if !docsQuotaTableCarriesRefusal(s) {
			switch {
			case s.Form == docsQuotaFormComponent && s.DictKey == "":
				out = append(out, fmt.Sprintf(
					"%s: таблица кодов подана компонентом, но словарь codes.ts не объявляет "+
						"записи с grpc %s — компоненту нечего показать",
					s.Service, docsQuotaRefusalCode))
			case s.Form == docsQuotaFormComponent && !s.InCodesArray:
				out = append(out, fmt.Sprintf(
					"%s: %s — ключ %q объявлен в словаре (%s), но в массив codes={…} не включён; "+
						"таблица строку %s не отрендерит, и клиент отказа не увидит",
					s.Service, s.FormAt, s.DictKey, s.DictAt, docsQuotaRefusalCode))
			default:
				out = append(out, fmt.Sprintf(
					"%s: %s не описывает отказ %s — таблица кодов его строки не несёт "+
						"(ни литералом, ни компонентом)",
					s.Service, s.OverviewPath, docsQuotaRefusalCode))
			}
		}

		if len(s.ContractPaths) == 0 {
			out = append(out, fmt.Sprintf(
				"%s: владелец учёта, а контракт квот не производит ни одного пути чтения — "+
					"предпосылка гейта неверна (соответствие «домен → путь» берётся из первого "+
					"сегмента пути, и для этого домена сегмента не нашлось)",
				s.Service))
			continue
		}
		for _, p := range s.ContractPaths {
			if _, ok := s.PathShownAt[p]; !ok {
				out = append(out, fmt.Sprintf(
					"%s: поверхность чтения пределов не названа — путь %q из контракта не "+
						"встречается дословно ни на одной странице services/%s/docs/content",
					s.Service, p, s.Service))
			}
		}
	}
	sort.Strings(out)
	return out
}
