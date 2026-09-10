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
// # ПОЧЕМУ ПОЛОС ДВЕ, А НЕ ОДНА
//
// Полоса A ловит вопрос, заданный ПАРАМЕТРОМ (полем субъекта). Полоса C ловит
// его, заданный АРИФМЕТИКОЙ: идентификатор членства не чеканится, а вычисляется
// из пары «человек:аккаунт» неизменяемой функцией без соли, поэтому чтение по
// одному идентификатору — полный межаккаунтный оракул, у которого идентификатора
// ЧЕЛОВЕКА нет вовсе, и первая полоса о нём молчит by construction.
//
// Полос было ТРИ: третья (B) ловила вопрос, заданный термом фильтра. Она снята
// вместе со своим предметом — разбор ниже говорит, почему именно так, а не
// расширением корня.
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
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// oracleProtoDir — поверхность, которую судит гейт.
const oracleProtoDir = "proto/kaname/cloud/iam/v1"

// ЗДЕСЬ БЫЛА ПОЛОСА B — терм фильтра, называющий субъекта, на чтении без
// обязательного аккаунта. Она снята вместе со своим предметом.
//
// Предметом полосы были БЕЛЫЕ СПИСКИ ФИЛЬТРА, объявленные в прод-коде службы
// доступа: её единственный корень так и назывался — `services/iam/internal`.
// Служба вынесена отдельным продуктом, и в этом дереве таких списков нет ни
// одного.
//
// ШИРЕ КОРЕНЬ ВЗЯТЬ БЫЛО НЕЛЬЗЯ, и это не лень, а свойство самой полосы: она
// связывает список с чтением, а чтения берутся из контрактов службы доступа.
// Белый список соседнего домена не связался бы ни с одним из них (`Bound` пуст),
// и полоса объявила бы находкой КАЖДЫЙ такой список с термом `user_id` — то есть
// стала бы производителем ложных находок на живом дереве. Живых вызовов разбора
// фильтра в дереве 37 файлов у пяти доменов, так что цена ошибки измерена, а не
// предположена.
//
// Механизм полосы был доказан инъекцией на СОБРАННОМ дереве, и доказательство
// снято вместе с ним: держать проверку, у которой в дереве нет и не может быть
// предмета, значит держать её ради зелёного.

// Предпосылка полосы C: идентификатор членства ВЫЧИСЛЯЕТСЯ из пары, а не
// чеканится.
//
// # Предмет тот же, а ПРОИЗВОДИТЕЛЬ сменился — и это не ослабление
//
// Прежде предпосылка читалась в корпусе МИГРАЦИЙ службы доступа: искалось само
// выражение деривации в теле функции схемы. Довод был верен ровно до выноса
// службы отдельным продуктом: схемы в этом дереве нет ни одним файлом, читать
// нечего, и «признака нет» стало неотличимо от «читать было нечего» — ровно то
// различие, ради которого перепись и печатает объём.
//
// Производителем стал КОНТРАКТ, и он единственный оставшийся в этом дереве:
// поверхность, которую полоса судит, объявлена здесь и правится здесь, а
// объяснение сокрытия при чтении по одному идентификатору называет деривацию
// прямо. Значит снятие предпосылки и снятие сокрытия видны ОДНИМ чтением одного
// файла, а не двумя в разных репозиториях.
//
// # ГРАНИЦА НАЗВАНА, а не умолчана
//
// Контракт делает ЗАЯВЛЕНИЕ, а схема делала ФАКТ. Что идентификатор в самом деле
// выводится из пары, держит владелец реализации в своём дереве; здесь
// утверждается ровно то, что этому дереву принадлежит, — поверхность объясняет
// сокрытие деривацией. Ослаблением это не является: полоса C и прежде опиралась
// на предпосылку, а не проверяла арифметику; сменилось место, где предпосылка
// объявлена.
//
// Перестанет контракт называть деривацию — запрет обязан быть ПЕРЕСМОТРЕН, а не
// унаследован молча; ровно это и роняет прогон.
const (
	oracleMembershipIDCorpus = oracleProtoDir
	// oracleMembershipIDMark — как контракт называет деривацию.
	//
	// Фраза, а не слово: одиночное «вычислим» встречается в прозе о другом, и
	// распознаватель по нему зачёл бы за предпосылку любое соседнее объяснение.
	oracleMembershipIDMark = "вычислим офлайн из пары"
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
	// ScopeFiltered — контракт объявляет пообъектное сужение страницы правами
	// вызывающего (`corelib.authz.v1.scope_filtered`). Это ДОКАЗАТЕЛЬСТВО
	// условия «г», и живёт оно там же, где судимая поверхность, — в контракте.
	ScopeFiltered bool
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

// OracleCensus — исход обхода. Объём осмотренного ВХОДИТ в исход, а не в лог.
type OracleCensus struct {
	ProtoFiles   int
	Messages     int
	RPCs         int
	PublicReads  int
	LaneASeen    int
	LaneCSeen    int
	Depth        int
	TruncatedAt  []string
	Dictionary   []string
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
	// oracleScopeFilteredRe — объявление пообъектного сужения страницы в ТЕЛЕ rpc.
	//
	// Судится тело САМОГО глагола (группа 4 разбора rpc), а не файл: опция
	// соседнего глагола не доказывает ничего об этом, а поиск по файлу гасил бы
	// целую службу первой же такой опцией у одного её чтения.
	oracleScopeFilteredRe = regexp.MustCompile(`scope_filtered\s*\)?\s*=\s*true`)
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
			Service:       svc,
			Method:        s[m[2]:m[3]],
			File:          rel,
			HTTPPath:      path,
			Request:       oracleBare(s[m[4]:m[5]]),
			Response:      oracleBare(s[m[6]:m[7]]),
			ScopeFiltered: oracleScopeFilteredRe.MatchString(oracleStripProtoComments(s[m[8]:m[9]])),
		})
	}
	return out
}

// oracleStripProtoComments — снятие построчных комментариев тела глагола.
//
// Без него гейт зачитывал бы за объявление собственное ОБЪЯСНЕНИЕ: тела оба
// несут разбор сужения прозой и называют в нём то же имя опции, поэтому поиск по
// слову оставался бы зелёным при снятом объявлении.
func oracleStripProtoComments(s string) string {
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		if j := strings.Index(ln, "//"); j >= 0 {
			lines[i] = ln[:j]
		}
	}
	return strings.Join(lines, "\n")
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
	if oracleNarrowedByCallerRights(r) {
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
	FQN string
	Why string
}

var oracleQuenchedByNarrowing = []oracleQuench{
	{
		FQN: "AccessBindingService/ListBySubject",
		Why: "страница полосы распорядителя аккаунта проходит пообъектный вопрос к модели " +
			"прав, а полосы собственного чтения и надзора облака шире принадлежащего им не " +
			"бывают — ответ поэтому не называет областей, к которым вызывающий отношения " +
			"не имеет (#1352)",
	},
	{
		FQN: "AccessBindingService/ListSubjectPrivileges",
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
//
// Требуется ДВОЕ сразу: запись ведомости (решение названо вслух и со ссылкой на
// задачу) И объявление сужения в САМОМ контракте. Одной записи недостаточно —
// иначе ведомость гасила бы чтение своим существованием; одной опции тоже —
// иначе снятие обязательного аккаунта гасилось бы молча, без чьего-либо решения.
func oracleNarrowedByCallerRights(r OracleRPC) bool {
	for _, q := range oracleQuenchedByNarrowing {
		if q.FQN == r.FQN() {
			return r.ScopeFiltered
		}
	}
	return false
}

// oracleContractRPCs — глаголы контракта службы доступа, прочитанные из дерева.
//
// Вынесено из обхода, потому что читателей стало ДВА: сам обход и перепись
// доказательств гасящих записей. Второй разбор тех же файлов разошёлся бы с
// первым молча — и разошёлся бы именно там, где расхождение не видно.
func oracleContractRPCs(tree *treecorpus.Tree) ([]OracleRPC, error) {
	var rpcs []OracleRPC
	for _, rel := range tree.SortedFiles() {
		if !strings.HasPrefix(rel, oracleProtoDir+"/") || !strings.HasSuffix(rel, ".proto") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(tree.Root(), filepath.FromSlash(rel)))
		if err != nil {
			return nil, fmt.Errorf("чтение %s: %w", rel, err)
		}
		rpcs = append(rpcs, oracleRPCsIn(rel, string(body))...)
	}
	return rpcs, nil
}

// OracleQuenchProof — доказательство одной гасящей записи, прочитанное в дереве.
type OracleQuenchProof struct {
	FQN   string
	File  string
	Found bool
}

// SurveyOracleQuenchProofs проверяет предпосылку КАЖДОЙ гасящей записи.
//
// # Доказательство переехало из РЕАЛИЗАЦИИ в КОНТРАКТ, и это не ослабление
//
// Прежде оно читалось разбором прод-кода службы доступа: искался узел вызова
// функции сужения страницы. Служба вынесена отдельным продуктом, файлов в этом
// дереве нет, и обе записи покраснели — доказательство пережило свой предмет.
//
// Контракт при этом остался ЗДЕСЬ и здесь же правится, поэтому доказательством
// стало его собственное объявление `corelib.authz.v1.scope_filtered = true` в
// теле судимого глагола. Оно живёт в том же файле, что и судимая поверхность,
// значит снятие сужения и снятие обязательного аккаунта видны ОДНИМ чтением, а
// не двумя в разных репозиториях.
//
// ГРАНИЦА НАЗВАНА: опция есть ОБЪЯВЛЕНИЕ, а не исполнение. Что страница
// действительно сужается построчно, держит владелец реализации — и держит в
// своём дереве. Здесь утверждается ровно то, что этому дереву принадлежит:
// поверхность объявляет сужение, а решение о гашении названо вслух ведомостью.
func SurveyOracleQuenchProofs(tree *treecorpus.Tree) []OracleQuenchProof {
	rpcs, err := oracleContractRPCs(tree)
	if err != nil {
		// Нечитаемый контракт — НЕ доказательство: перепись отдаст «не найдено»
		// по каждой записи, и гейт скажет об этом отказом, а не молчанием.
		rpcs = nil
	}
	byFQN := make(map[string]OracleRPC, len(rpcs))
	for _, r := range rpcs {
		byFQN[r.FQN()] = r
	}
	out := make([]OracleQuenchProof, 0, len(oracleQuenchedByNarrowing))
	for _, q := range oracleQuenchedByNarrowing {
		r, ok := byFQN[q.FQN]
		out = append(out, OracleQuenchProof{
			FQN:   q.FQN,
			File:  r.File,
			Found: ok && r.ScopeFiltered,
		})
	}
	return out
}

// ЗДЕСЬ БЫЛА функция чтения ВЫЗОВА в прод-файле — единственный её вызывающий
// (перепись доказательств гасящих записей) переведён на объявление контракта,
// потому что прод-файлов службы доступа в этом дереве нет. Функция снята вместе
// со своим предметом: помощник без вызывающего есть тот же мёртвый код, что и
// проверка без предмета.

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
		if !strings.HasPrefix(rel, prefix) || !strings.HasSuffix(rel, ".proto") {
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
