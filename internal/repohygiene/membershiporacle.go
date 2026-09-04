// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// membershiporacle.go — разбор дерева для гейта «вопроса „в каких аккаунтах
// состоит этот человек“ на публичной поверхности iam НЕТ» (IAM-ID-2-15).
//
// # ПРЕДМЕТ
//
// Перечень аккаунтов человека — факт о ТРЕТЬИХ СТОРОНАХ: он раскрывает связи
// человека с организациями, к которым спрашивающий отношения не имеет. Права
// видеть его у распорядителя ОДНОГО аккаунта нет, и решением по задаче #1085
// такое чтение объявлено утечкой, а не удобством.
//
// Запрет держится не тем, что «никто не добавит», а тем, что вопрос НЕВОЗМОЖНО
// задать: у аккаунт-скоупного чтения аккаунт обязателен, а своего списка
// человек спрашивает только про себя. Гейт стережёт ровно это свойство
// поверхности.
//
// # ПОЧЕМУ ПОЛОС ТРИ, А НЕ ОДНА
//
// Полосы A и B ловят вопрос, заданный ПАРАМЕТРОМ (полем субъекта либо термом
// фильтра). Полоса C ловит его, заданный АРИФМЕТИКОЙ: идентификатор членства
// не чеканится, а вычисляется из пары «человек:аккаунт» неизменяемой функцией
// без соли, поэтому чтение по одному идентификатору — полный межаккаунтный
// оракул, у которого идентификатора ЧЕЛОВЕКА нет вовсе, и первые две полосы о
// нём молчат by construction.
//
// Предпосылка полосы C названа здесь, рядом с ней, и ПРОВЕРЯЕТСЯ: перестанет
// идентификатор быть вычислимым — запрет обязан быть ПЕРЕСМОТРЕН, а не
// унаследован молча.
//
// # ЧЕГО ГЕЙТ НЕ УТВЕРЖДАЕТ
//
// Он судит КОНТРАКТЫ и белые списки фильтра, а не рисунок консоли: столбец,
// собранный консолью из многих аккаунт-скоупных чтений, ему невидим. Это
// названо, а не умолчано.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
)

// oracleProtoDir — поверхность, которую судит гейт.
const oracleProtoDir = "proto/kacho/cloud/iam/v1"

// oracleFilterRoots — прод-код, где объявляются белые списки фильтра.
var oracleFilterRoots = []string{"services/iam/internal"}

// Предпосылка полосы C: идентификатор членства ВЫЧИСЛЯЕТСЯ из пары, а не
// чеканится.
//
// # Ищется по КОРПУСУ, а не по имени файла
//
// Здесь стояла координата одной миграции, и она умерла 2026-09-04: свод (171
// файл → один, написанный `pg_dump`) снял файл, в котором деривация была
// заведена. Само выражение при этом ЖИВО и переехало в свод байт-в-байт — оно
// стоит в теле функции, — то есть предпосылка была верна, а гейт объявлял её
// ложной. Так выглядит утверждение, привязанное к координате вместо предмета:
// оно переживает не факт, а раскладку файлов.
//
// Предмет предпосылки — «схема iam выводит идентификатор членства из пары», и
// принадлежит он КОРПУСУ миграций сервиса, а не какому-то его файлу.
// Применённую миграцию не правят (ban #5), поэтому деривация переезжает вместе
// со сводами и будет переезжать впредь; координата на неё указывать не вправе.
const (
	oracleMembershipIDCorpus = "services/iam/internal/migrations"
	oracleMembershipIDMark   = "substr(md5('membership:'"
)

// oracleAccountDictionary — по каким именам полей ответ считается «называющим
// аккаунт или членство» (условие «б» полосы A).
//
// Словарь печатается ЦЕЛИКОМ. Имя, которого в нём нет, делает поле невидимым
// для полосы, и отличить это от «полей нет» иначе нечем; пополнение словаря
// обязано менять ПЕРЕПИСЬ ОСМОТРЕННОГО, а не только число находок.
var oracleAccountDictionary = []string{"account_id", "scope_id", "membership"}

// oracleAccountTypes — типы сообщений, само присутствие которых в ответе
// означает «называет членство».
var oracleAccountTypes = []string{"Membership"}

// oracleSubjectFields — как запрос называет ЧЕЛОВЕКА (условие «а»).
//
// Форм ДВЕ, и обе читаются. Единица счёта «форма пути» дала бы три чтения из
// пяти: два, называющих человека ПОЛЕМ, оказались бы вне наблюдения — не
// находкой, а невидимостью.
var oracleSubjectFields = []string{"user_id", "subject_id"}

// oracleFilterSubjectTerms — термы фильтра, называющие субъекта (полоса B).
var oracleFilterSubjectTerms = []string{"userId", "user_id", "subjectId", "subject_id"}

// oracleTraversalDepth — предел обхода вложенных сообщений условия «б».
//
// Это РЕШЕНИЕ, а не измерение: «транзитивно по сообщениям» конечно не само по
// себе, его обрывает выбранный предел. Он печатается числом, и рядом
// печатается, сколько сообщений на нём УСЕЧЕНО: ненулевое усечение означает,
// что часть ответов осмотрена не до конца, и это обязано быть видно.
const oracleTraversalDepth = 6

// OracleRPC — публичное чтение поверхности iam.
type OracleRPC struct {
	Service  string
	Method   string
	File     string
	HTTPPath string
	Request  string
	Response string
	// ReqFields — поля запроса (имя → тип).
	ReqFields map[string]string
}

// FQN — имя, которым находка называет координату.
func (r OracleRPC) FQN() string { return r.Service + "/" + r.Method }

// OracleFinding — одна находка с координатой и полосой.
type OracleFinding struct {
	Lane string
	FQN  string
	File string
	Why  string
}

// OracleFilterWhitelist — один объявленный белый список фильтра.
type OracleFilterWhitelist struct {
	File  string
	Line  int
	Terms []string
	// Bound — чтение, которому список принадлежит; пусто, если связать нечем.
	Bound string
}

// OracleCensus — исход обхода. Объём осмотренного ВХОДИТ в исход, а не в лог.
type OracleCensus struct {
	ProtoFiles   int
	Messages     int
	RPCs         int
	PublicReads  int
	LaneASeen    int
	LaneBSeen    int
	LaneCSeen    int
	Depth        int
	TruncatedAt  []string
	Dictionary   []string
	Whitelists   []OracleFilterWhitelist
	IDComputable bool
	// IDCorpusFiles — файлов корпуса миграций прочитано при проверке
	// предпосылки полосы C. Ноль означает «читать было нечего», а не «признака
	// нет», и путать эти два ответа нельзя.
	IDCorpusFiles int

	Findings []OracleFinding
	// Allowed — объявленные послабления, СРАБОТАВШИЕ на этом дереве. Запись,
	// которой нечего исключать, — находка (см. гейт).
	Allowed []string
}

var (
	oracleMessageRe = regexp.MustCompile(`(?m)^message\s+(\w+)\s*\{`)
	oracleFieldRe   = regexp.MustCompile(`(?m)^\s*(?:repeated\s+)?(?:map<[^>]+>\s+)?([\w.]+)\s+(\w+)\s*=\s*\d+`)
	oracleRPCRe     = regexp.MustCompile(`(?s)rpc\s+(\w+)\s*\(\s*([\w.]+)\s*\)\s*returns\s*\(\s*([\w.]+)\s*\)\s*\{(.*?)\n\s*\}`)
	oracleServiceRe = regexp.MustCompile(`(?m)^service\s+(\w+)\s*\{`)
	oracleGetRe     = regexp.MustCompile(`get:\s*"([^"]+)"`)
	oracleParseRe   = regexp.MustCompile(`(?s)(?:filter\.Parse|parseListFilter)\s*\(\s*[^,]+,\s*(?:\[\]string\{)?([^)}]*)`)
	oracleTermRe    = regexp.MustCompile(`"([A-Za-z_][A-Za-z0-9_]*)"`)
	// oracleTermIdentRe — терм, записанный ИМЕНОВАННОЙ КОНСТАНТОЙ, а не литералом.
	//
	// Форма законная и в этом дереве обычная: единый источник истины лучше
	// литерала, продублированного у двух читателей. Распознаватель, знающий одну
	// форму, оставил бы всё записанное второй ВНЕ НАБЛЮДЕНИЯ — не находкой, а
	// невидимостью, и молчал бы об этом. Ровно так он и молчал о белом списке
	// членства, пока сюда не добавили вторую форму.
	oracleTermIdentRe = regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*)\b`)
	// oracleConstRe — объявление строковой константы, к которому идентификатор
	// разрешается.
	oracleConstRe = regexp.MustCompile(`(?m)^\s*(?:[A-Za-z_][A-Za-z0-9_]*\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*"([^"]*)"`)
)

// SurveyMembershipOracle обходит дерево и сводит три полосы.
//
// Дерево приходит СОСТАВЛЕННЫМ: вердикт обязан быть свойством коммита, а не
// рабочего каталога.
func SurveyMembershipOracle(tree *treecorpus.Tree) (OracleCensus, error) {
	c := OracleCensus{Depth: oracleTraversalDepth, Dictionary: append([]string{}, oracleAccountDictionary...)}

	msgs := map[string]string{}
	var rpcs []OracleRPC

	for _, rel := range tree.SortedFiles() {
		if !strings.HasPrefix(rel, oracleProtoDir+"/") || !strings.HasSuffix(rel, ".proto") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(tree.Root(), filepath.FromSlash(rel)))
		if err != nil {
			return c, fmt.Errorf("чтение %s: %w", rel, err)
		}
		c.ProtoFiles++
		s := string(body)
		for name, block := range oracleMessageBlocks(s) {
			msgs[name] = block
		}
		rpcs = append(rpcs, oracleRPCsIn(rel, s)...)
	}
	c.Messages = len(msgs)
	c.RPCs = len(rpcs)

	for i := range rpcs {
		rpcs[i].ReqFields = oracleFieldsOf(msgs, rpcs[i].Request)
	}
	for _, r := range rpcs {
		if r.HTTPPath != "" {
			c.PublicReads++
		}
	}

	trunc := map[string]bool{}
	for _, r := range rpcs {
		if r.HTTPPath == "" {
			continue
		}
		c.LaneASeen++
		if f, ok := oracleLaneA(r, msgs, trunc); ok {
			c.Findings = append(c.Findings, f)
		}
		c.LaneCSeen++
		if f, ok := oracleLaneC(r); ok {
			c.Findings = append(c.Findings, f)
		}
	}
	for t := range trunc {
		c.TruncatedAt = append(c.TruncatedAt, t)
	}
	sort.Strings(c.TruncatedAt)

	wl, err := oracleWhitelists(tree, rpcs)
	if err != nil {
		return c, err
	}
	c.Whitelists = wl
	for _, w := range wl {
		c.LaneBSeen++
		if f, ok := oracleLaneB(w, rpcs); ok {
			c.Findings = append(c.Findings, f)
		}
	}

	c.IDComputable, c.IDCorpusFiles = oracleIDIsComputable(tree)

	// Послабления разводятся с находками ПОСЛЕ обхода, а не вычитаются из него:
	// вычет перед решением скрыл бы, что послабление сработало.
	kept := c.Findings[:0]
	for _, f := range c.Findings {
		if oracleIsAllowed(f.FQN) {
			c.Allowed = append(c.Allowed, f.Lane+" "+f.FQN)
			continue
		}
		kept = append(kept, f)
	}
	c.Findings = kept
	sort.Strings(c.Allowed)

	sort.Slice(c.Findings, func(i, j int) bool {
		if c.Findings[i].Lane != c.Findings[j].Lane {
			return c.Findings[i].Lane < c.Findings[j].Lane
		}
		return c.Findings[i].FQN < c.Findings[j].FQN
	})
	return c, nil
}

// oracleMessageBlocks — тела сообщений файла, со скобочным балансом (вложенные
// сообщения и enum'ы внутрь тела попадают, и это верно: поле вложенного
// сообщения тоже часть ответа).
func oracleMessageBlocks(s string) map[string]string {
	out := map[string]string{}
	for _, m := range oracleMessageRe.FindAllStringSubmatchIndex(s, -1) {
		name := s[m[2]:m[3]]
		i := m[1]
		depth, j := 1, i
		for depth > 0 && j < len(s) {
			switch s[j] {
			case '{':
				depth++
			case '}':
				depth--
			}
			j++
		}
		out[name] = s[i : j-1]
	}
	return out
}

// oracleRPCsIn — публичные RPC файла. `Internal*`-сервисы предметом не
// являются: их поверхность не тенант-фейсинг.
func oracleRPCsIn(rel, s string) []OracleRPC {
	svcAt := oracleServiceRe.FindAllStringSubmatchIndex(s, -1)
	svcOf := func(pos int) string {
		name := ""
		for _, m := range svcAt {
			if m[0] < pos {
				name = s[m[2]:m[3]]
			}
		}
		return name
	}
	var out []OracleRPC
	for _, m := range oracleRPCRe.FindAllStringSubmatchIndex(s, -1) {
		svc := svcOf(m[0])
		if svc == "" || strings.HasPrefix(svc, "Internal") {
			continue
		}
		body := s[m[8]:m[9]]
		path := ""
		if g := oracleGetRe.FindStringSubmatch(body); g != nil {
			path = g[1]
		}
		out = append(out, OracleRPC{
			Service:  svc,
			Method:   s[m[2]:m[3]],
			File:     rel,
			HTTPPath: path,
			Request:  oracleBare(s[m[4]:m[5]]),
			Response: oracleBare(s[m[6]:m[7]]),
		})
	}
	return out
}

func oracleBare(t string) string {
	if i := strings.LastIndex(t, "."); i >= 0 {
		return t[i+1:]
	}
	return t
}

func oracleFieldsOf(msgs map[string]string, name string) map[string]string {
	out := map[string]string{}
	for _, f := range oracleFieldRe.FindAllStringSubmatch(msgs[name], -1) {
		out[f[2]] = oracleBare(f[1])
	}
	return out
}

// oracleLaneA — все ЧЕТЫРЕ условия сразу.
func oracleLaneA(r OracleRPC, msgs map[string]string, trunc map[string]bool) (OracleFinding, bool) {
	if !oracleNamesPerson(r) {
		return OracleFinding{}, false
	}
	if !oracleResponseNamesAccount(r.Response, msgs, 0, map[string]bool{}, trunc) {
		return OracleFinding{}, false
	}
	if oracleAccountIsMandatory(r) {
		return OracleFinding{}, false
	}
	if oracleNarrowedByCallerRights(r.FQN()) {
		return OracleFinding{}, false
	}
	return OracleFinding{
		Lane: "A",
		FQN:  r.FQN(),
		File: r.File,
		Why: "запрос называет человека, ответ называет аккаунт или членство, аккаунт " +
			"в запросе НЕ обязателен, и ответ не сужен пообъектно правами вызывающего — " +
			"то есть чтение отвечает на вопрос «в каких аккаунтах состоит этот человек»",
	}, true
}

// oracleNamesPerson — условие «а»: ОБЕ формы, путь и поле.
func oracleNamesPerson(r OracleRPC) bool {
	for _, f := range oracleSubjectFields {
		if strings.Contains(r.HTTPPath, "{"+f+"}") {
			return true
		}
		if _, ok := r.ReqFields[f]; ok {
			return true
		}
	}
	return false
}

// oracleResponseNamesAccount — условие «б», транзитивно по сообщениям до
// объявленной глубины. Усечение НАЗЫВАЕТСЯ, а не проглатывается.
func oracleResponseNamesAccount(msg string, msgs map[string]string, depth int, seen, trunc map[string]bool) bool {
	if depth > oracleTraversalDepth {
		trunc[msg] = true
		return false
	}
	if seen[msg] {
		return false
	}
	seen[msg] = true
	for _, f := range oracleFieldRe.FindAllStringSubmatch(msgs[msg], -1) {
		typ, name := oracleBare(f[1]), strings.ToLower(f[2])
		for _, k := range oracleAccountDictionary {
			if strings.Contains(name, k) {
				return true
			}
		}
		for _, t := range oracleAccountTypes {
			if typ == t {
				return true
			}
		}
		if _, ok := msgs[typ]; ok && oracleResponseNamesAccount(typ, msgs, depth+1, seen, trunc) {
			return true
		}
	}
	return false
}

// oracleAccountIsMandatory — условие «в». Обязательным аккаунт делает ПУТЬ:
// поле запроса можно не заполнить, сегмент пути не заполнить нельзя.
func oracleAccountIsMandatory(r OracleRPC) bool {
	return strings.Contains(r.HTTPPath, "{account_id}")
}

// oracleLaneC — чтение, резолвящее ИДЕНТИФИКАТОР ЧЛЕНСТВА без обязательного
// аккаунта. Идентификатора ЧЕЛОВЕКА в таком запросе нет вовсе, поэтому полосы A
// и B о нём молчат.
func oracleLaneC(r OracleRPC) (OracleFinding, bool) {
	named := strings.Contains(r.HTTPPath, "{membership_id}")
	if !named {
		if _, ok := r.ReqFields["membership_id"]; ok {
			named = true
		}
	}
	if !named || oracleAccountIsMandatory(r) {
		return OracleFinding{}, false
	}
	return OracleFinding{
		Lane: "C",
		FQN:  r.FQN(),
		File: r.File,
		Why: "чтение резолвит идентификатор членства без обязательного аккаунта, а " +
			"идентификатор вычислим офлайн из пары «человек:аккаунт» — вопрос задаётся " +
			"арифметикой вместо параметра",
	}, true
}

// oracleLaneB — терм фильтра, называющий субъекта, на чтении без обязательного
// аккаунта.
func oracleLaneB(w OracleFilterWhitelist, rpcs []OracleRPC) (OracleFinding, bool) {
	subject := ""
	for _, t := range w.Terms {
		for _, s := range oracleFilterSubjectTerms {
			if t == s {
				subject = t
			}
		}
	}
	if subject == "" {
		return OracleFinding{}, false
	}
	for _, r := range rpcs {
		if r.FQN() == w.Bound && oracleAccountIsMandatory(r) {
			return OracleFinding{}, false
		}
	}
	return OracleFinding{
		Lane: "B",
		FQN:  w.Bound,
		File: fmt.Sprintf("%s:%d", w.File, w.Line),
		Why: "белый список фильтра несёт терм «" + subject + "», называющий субъекта, а " +
			"аккаунт у этого чтения не обязателен — та же форма вопроса, спрятанная в filter",
	}, true
}

// oracleWhitelists собирает объявленные белые списки и СВЯЗЫВАЕТ каждый с
// чтением.
//
// Связывание идёт по каталогу файла — имя ресурса выводится из него и
// превращается в имя службы. Связывание ПЕЧАТАЕТСЯ переписью: читатель обязан
// видеть, что именно сопоставлено, а не верить, что сопоставлено верно.
func oracleWhitelists(tree *treecorpus.Tree, rpcs []OracleRPC) ([]OracleFilterWhitelist, error) {
	consts, err := oracleStringConsts(tree)
	if err != nil {
		return nil, err
	}
	var out []OracleFilterWhitelist
	for _, rel := range tree.SortedFiles() {
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		under := false
		for _, root := range oracleFilterRoots {
			if strings.HasPrefix(rel, root+"/") {
				under = true
			}
		}
		if !under {
			continue
		}
		body, err := os.ReadFile(filepath.Join(tree.Root(), filepath.FromSlash(rel)))
		if err != nil {
			return nil, fmt.Errorf("чтение %s: %w", rel, err)
		}
		s := string(body)
		for _, m := range oracleParseRe.FindAllStringSubmatchIndex(s, -1) {
			arg := s[m[2]:m[3]]
			var terms []string
			for _, t := range oracleTermRe.FindAllStringSubmatch(arg, -1) {
				terms = append(terms, t[1])
			}
			// Вторая законная форма: терм, названный КОНСТАНТОЙ. Разрешается к
			// её объявлению; неразрешимое имя термом не считается.
			for _, id := range oracleTermIdentRe.FindAllStringSubmatch(oracleStripLiterals(arg), -1) {
				if v, ok := consts[id[1]]; ok {
					terms = append(terms, v)
				}
			}
			if len(terms) == 0 {
				continue
			}
			sort.Strings(terms)
			out = append(out, OracleFilterWhitelist{
				File:  rel,
				Line:  1 + strings.Count(s[:m[0]], "\n"),
				Terms: terms,
				Bound: oracleBindWhitelist(rel, rpcs),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out, nil
}

// oracleStringConsts — строковые константы прод-кода сервиса, к которым
// разрешаются термы, записанные именем.
//
// Область та же, что у белых списков: константа, объявленная вне её, термом
// этого сервиса не является.
func oracleStringConsts(tree *treecorpus.Tree) (map[string]string, error) {
	out := map[string]string{}
	for _, rel := range tree.SortedFiles() {
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		under := false
		for _, root := range oracleFilterRoots {
			if strings.HasPrefix(rel, root+"/") {
				under = true
			}
		}
		if !under {
			continue
		}
		body, err := os.ReadFile(filepath.Join(tree.Root(), filepath.FromSlash(rel)))
		if err != nil {
			return nil, fmt.Errorf("чтение %s: %w", rel, err)
		}
		for _, m := range oracleConstRe.FindAllStringSubmatch(string(body), -1) {
			if _, seen := out[m[1]]; !seen {
				out[m[1]] = m[2]
			}
		}
	}
	return out, nil
}

// oracleStripLiterals убирает строковые литералы, чтобы разбор ИМЁН не считал
// содержимое литерала именем.
func oracleStripLiterals(s string) string {
	return oracleTermRe.ReplaceAllString(s, " ")
}

// oracleBindWhitelist — какому списочному чтению принадлежит белый список.
func oracleBindWhitelist(rel string, rpcs []OracleRPC) string {
	base := strings.TrimSuffix(filepath.Base(rel), ".go")
	base = strings.TrimSuffix(base, "_repo")
	candidates := []string{base, filepath.Base(filepath.Dir(rel))}
	for _, cand := range candidates {
		svc := oracleCamel(cand) + "Service"
		for _, r := range rpcs {
			if r.Service == svc && strings.HasPrefix(r.Method, "List") {
				return r.FQN()
			}
		}
	}
	return ""
}

func oracleCamel(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "")
}

// oracleQuench — чтение, гасящее условие «г» ДОКАЗАННО.
//
// Гасит его не звание, а СУЖЕНИЕ: страница такого чтения проходит пообъектный
// вопрос к модели прав, поэтому ответ не «аккаунты этого человека», а «те из них,
// что вам и так видны». Доказательство — координата в дереве и ВЫЗОВ в ней; гейт
// его проверяет, а не принимает на слово, и запись САМОИСТЕКАЕТ: пропал вызов —
// пропало и основание гасить.
//
// # Почему ВЫЗОВ, а не подстрока
//
// Оба файла, на которые указывают записи ниже, несут развёрнутый разбор сужения
// прозой — и называют в нём то же имя. Подстрочный поиск находил бы КОММЕНТАРИЙ,
// объясняющий сужение, и оставался бы зелёным при снятом сужении: гейт
// удостоверял бы собственное объяснение (`testing.md` §«Гейт на класс», п.4).
// Поэтому доказательство читается разбором и судится по узлу вызова.
type oracleQuench struct {
	FQN  string
	File string
	// Marker — ИМЯ ФУНКЦИИ, чей вызов доказывает сужение. Совпадение идёт по
	// последнему сегменту (`pkg.Fn` и `Fn` равнозначны): предмет — вызвана ли
	// функция, а не как записан её путь.
	Marker string
	Why    string
}

// oracleNarrowingCall — имя функции, чей вызов доказывает сужение страницы.
//
// Одно написание на обе записи: две копии имени разошлись бы при первом же
// переименовании, и разошлись бы молча — запись, потерявшая доказательство,
// краснеет, а запись, доказанная НЕ ТЕМ вызовом, нет.
const oracleNarrowingCall = "visibleOnNarrowedPage"

var oracleQuenchedByNarrowing = []oracleQuench{
	{
		FQN:    "AccessBindingService/ListBySubject",
		File:   "services/iam/internal/apps/kacho/api/access_binding/list_by_subject.go",
		Marker: oracleNarrowingCall,
		Why: "страница полосы распорядителя аккаунта проходит пообъектный вопрос к модели " +
			"прав, а полосы собственного чтения и надзора облака шире принадлежащего им не " +
			"бывают — ответ поэтому не называет областей, к которым вызывающий отношения " +
			"не имеет (#1352)",
	},
	{
		FQN:    "AccessBindingService/ListSubjectPrivileges",
		File:   "services/iam/internal/apps/kacho/api/access_binding/list_subject_privileges.go",
		Marker: oracleNarrowingCall,
		Why: "то же сужение и ТЕМ ЖЕ вызовом, что у соседнего чтения: допуск решается по " +
			"домашнему аккаунту субъекта, а строки ответа проходят пообъектный вопрос по " +
			"идентификатору выдачи, поэтому области в чужих аккаунтах на страницу не " +
			"попадают (#1354)",
	},
}

// oracleAllowance — послабление, НАЗВАННОЕ вслух и со ссылкой на задачу.
//
// Молчаливое исключение из полосы запрещено: оно было бы маской, а не близнецом.
// Запись самоистекает в обе стороны — послабление, которому нечего прощать,
// само становится находкой.
type oracleAllowance struct {
	FQN   string
	Issue int
	Why   string
}

// Ведомость ПУСТА, и это её цель, а не поломка. Единственная запись стояла на
// AccessBindingService/ListSubjectPrivileges (#1354): чтение допускало по
// авторитету над домашним аккаунтом субъекта и после допуска перечень не сужало.
// Предмет снят — страница сужается построчно, — поэтому запись снята вместе с
// ним: послабление без предмета переживает то, ради чего заведено, и следующий
// читатель принимает его за действующее основание.
//
// Разведение послаблений с находками ниже на пустой ведомости остаётся
// исполнимым и НЕ вырождается: перепись печатает «объявленных послаблений 0»,
// а гейт падает на записи, которой больше нечего прощать.
var oracleDeclaredAllowances = []oracleAllowance{}

// oracleNarrowedByCallerRights — гасит ли условие «г» это чтение.
func oracleNarrowedByCallerRights(fqn string) bool {
	for _, q := range oracleQuenchedByNarrowing {
		if q.FQN == fqn {
			return true
		}
	}
	return false
}

// OracleQuenchProof — доказательство одной гасящей записи, прочитанное в дереве.
type OracleQuenchProof struct {
	FQN   string
	File  string
	Found bool
}

// SurveyOracleQuenchProofs проверяет предпосылку КАЖДОЙ гасящей записи.
//
// Доказательство читается РАЗБОРОМ: судится узел вызова, а не текст файла.
// Прозу о сужении оба файла несут развёрнутую, и подстрочный поиск находил бы
// её, оставаясь зелёным при снятом сужении.
func SurveyOracleQuenchProofs(tree *treecorpus.Tree) []OracleQuenchProof {
	out := make([]OracleQuenchProof, 0, len(oracleQuenchedByNarrowing))
	for _, q := range oracleQuenchedByNarrowing {
		out = append(out, OracleQuenchProof{
			FQN:   q.FQN,
			File:  q.File,
			Found: oracleFileCalls(filepath.Join(tree.Root(), filepath.FromSlash(q.File)), q.Marker),
		})
	}
	return out
}

// oracleFileCalls — зовёт ли файл функцию с таким именем.
//
// Нечитаемый и неразбираемый файл отвечают «нет»: доказательство, которое нельзя
// прочитать, доказательством не является.
func oracleFileCalls(path, name string) bool {
	body, err := os.ReadFile(path) // #nosec G304 -- путь собран из ведомости гейта, не из ввода
	if err != nil {
		return false
	}
	file, err := parser.ParseFile(token.NewFileSet(), path, body, 0)
	if err != nil {
		return false
	}
	want := name
	if i := strings.LastIndex(want, "."); i >= 0 {
		want = want[i+1:]
	}
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fn := call.Fun.(type) {
		case *ast.Ident:
			if fn.Name == want {
				found = true
			}
		case *ast.SelectorExpr:
			if fn.Sel != nil && fn.Sel.Name == want {
				found = true
			}
		}
		return !found
	})
	return found
}

// OracleAllowanceNames — имена объявленных послаблений, для переписи.
func OracleAllowanceNames() []string {
	out := make([]string, 0, len(oracleDeclaredAllowances))
	for _, a := range oracleDeclaredAllowances {
		out = append(out, fmt.Sprintf("%s (#%d)", a.FQN, a.Issue))
	}
	sort.Strings(out)
	return out
}

// oracleIsAllowed — объявлено ли послабление на это чтение.
func oracleIsAllowed(fqn string) bool {
	for _, a := range oracleDeclaredAllowances {
		if a.FQN == fqn {
			return true
		}
	}
	return false
}

// oracleIDIsComputable — ПРЕДПОСЫЛКА полосы C, и она проверяется, а не
// предполагается.
//
// Обходится ВЕСЬ корпус миграций сервиса: деривация переезжает между файлами при
// каждом своде, и координата на неё указывать не вправе (см. комментарий у
// [oracleMembershipIDCorpus]). Второе возвращаемое — сколько файлов прочитано:
// «признака нет» обязано быть отличимо от «читать было нечего».
//
// Состав берётся у УЖЕ СОСТАВЛЕННОГО дерева, а не отдельным обходом: дерево
// приходит сюда из одного источника со всеми полосами, и второй обход дал бы
// вердикт о другом множестве файлов, чем тот, который гейт объявляет переписью.
func oracleIDIsComputable(tree *treecorpus.Tree) (bool, int) {
	prefix := oracleMembershipIDCorpus + "/"
	read := 0
	found := false
	for _, rel := range tree.SortedFiles() {
		if !strings.HasPrefix(rel, prefix) || !strings.HasSuffix(rel, ".sql") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(tree.Root(), filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		read++
		if strings.Contains(string(body), oracleMembershipIDMark) {
			found = true
		}
	}
	return found, read
}
