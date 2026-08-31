// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// contractadvice.go — совет в комментарии контракта называет ГЛАГОЛ, который
// объявлен.
//
// # Предмет
//
// Комментарий вправе сказать вызывающему, что делать вместо отказа: «сделай X».
// Если глагола X не существует, вызывающий уходит искать путь, которого нет, и
// остаётся без следующего шага — то есть отказ и совет вместе НЕ восстанавливают
// следующий шаг клиента (ban #18, «разработчик»: говорит ли отказ, что делать
// дальше).
//
// Это близнец запрета «принято-и-проигнорировано» (`api-conventions.md`): там
// поле принимают и не читают, здесь советуют путь, которого нет. Класс ТИХИЙ —
// он не ломает сборку, не роняет ни одной пробы и не виден в обзоре изменения:
// имя глагола в прозе выглядит ровно как имя глагола в контракте.
//
// # Почему разбор, а не поиск по образцу
//
// Имя глагола встречается в прозе о самом гейте, в тексте ошибки и в чужом
// сообщении. Проверка по подстроке краснела бы на собственном объяснении
// (`testing.md` §«Гейт на класс», п.4). Здесь суждение выносится по РАЗОБРАННОМУ
// комментарию — блоку `//`, отделённому от кода, — и по объявлениям `rpc`,
// прочитанным из тех же файлов.
//
// # Почему НЕ «всякое имя в бэктиках обязано резолвиться»
//
// Такой предикат рассматривался и ОТВЕРГНУТ ЗАМЕРОМ, а не по вкусу. Замер: по
// своим контрактам имён формы идентификатора в бэктиках — 271, из них НЕ
// резолвятся ни в объявленную службу, ни в глагол, ни в сообщение, ни в
// перечисление — 111 (65 уникальных). Предикат — обход отслеживаемых `*.proto`,
// отбор своих по объявленному пакету, сбор токенов между обратными кавычками из
// строк `//` и сверка с объявлениями `service`/`rpc`/`message`/`enum` того же
// корпуса; ревизия — база этой ветки. Величина «своих контрактов» здесь НЕ
// выписана намеренно: её печатает сама перепись гейта, и второе место о том же
// предмете разошлось бы с ней молча (прежняя редакция этого абзаца называла 123
// при 124 в переписи).
//
// Что там лежит: коды gRPC (`ALREADY_EXISTS`), состояния (`ACTIVE`), имена
// сообщений и типов (`ArtifactRef`, `AttachedTargetGroup`), союзы разметки
// (`AND`), имена функций Go, имена проб, надгробия снятых глаголов
// (`UpdateMetadata` — «которого в контракте нет»). Надгробие называет
// несуществующий глагол НАМЕРЕННО и является ЗАКОННЫМ близнецом: широкий
// предикат объявил бы находкой ровно то место, где автор честно сказал, что
// глагола нет.
//
// Поэтому предмет сужен до СОВЕТА: имя судится только там, где проза
// рекомендует его взять.
//
// # Формы записи, которые распознаватель знает (`testing.md` §«Гейт на класс», п.7)
//
// Осей ДВЕ, и они ортогональны: один и тот же оборот пишут любым обрамлением.
// Перечень нужен целиком — форма, о которой распознаватель не знает, даёт не
// красное и не зелёное, а МОЛЧАНИЕ, и всё записанное в ней уходит из-под
// наблюдения. Каждая клетка обеих осей доказана своей инъекцией.
//
// Ось СОВЕТА (корпус двуязычен, поэтому обе половины обязательны):
//
//	F1-en-directive  use / call / invoke — в обоих написаниях первой буквы
//	F2-en-instead    повелительное предложение с ОДИНОКИМ instead
//	F3-ru-directive  вызовите / вызывайте / зовите / звать / обращайтесь /
//	                 используйте / пользуйтесь
//	F4-ru-vmesto     вместо X, вместо этого X
//	F5-ru-glagol     …глаголом (X)
//
// Ось ИМЕНИ (обрамление и состав):
//
//	обрамление       обратные кавычки `X` · квадратные скобки [X] · голое X
//	S2-dotted        Служба.Глагол — судится НАЗВАННАЯ служба
//	S3-camel         составное имя (Frobnicate → FooBar)
//	S4-vocab         односоставное, объявленное глаголом где-либо в дереве
//
// # Границы, названные вслух (`testing.md` §«Гейт на класс», п.3)
//
//  1. Чужие контракты (`google.*`) не судятся: их поверхность — не наше
//     обещание. Граница выводится из ОБЪЯВЛЕННОГО ПАКЕТА (`package kacho.…`), а
//     не из пути, иначе она разошлась бы с деревом при переезде каталога.
//  2. Вне наблюдения остаётся ОДНА подформа: имя голое, односоставное
//     (`Frobnicate`) и не объявленное глаголом НИГДЕ в дереве. Такой токен
//     неотличим от обычной прозы. Это измерено, а не предположено: вариант
//     «голое односоставное слово в повелительной позиции» дал по всему дереву
//     ОДИН кандидат, и им оказался артикль `The`. Расширение, не меняющее
//     осмотренного, — холостое, и оно снято (п.7), а не оставлено «на всякий
//     случай». Односоставное имя, которое дерево где-либо объявляет глаголом,
//     наблюдается (форма S4) — именно ею найден #1442.
//  3. Судится СОВЕТ, а не всякое упоминание производителя. Описательная форма
//     «ставится X, снимается Y» под предикат НЕ подпадает; её цена измерена и
//     названа: в дереве один живой экземпляр
//     (`vpc/v1/network_interface.proto`, `AttachToInstance`/`DetachFromInstance`
//     при живых `AttachNetworkInterface`/`DetachNetworkInterface`). Расширение
//     предиката на неё — отдельный предмет, потому что маркеры `ставится`
//     и `снимается` пассивны и в этом дереве частотны.
package repohygiene

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
)

// adviceSource — один контракт: путь относительно корня и тело.
//
// Разбор отделён от чтения дерева намеренно: проба способности гейта падать
// подаёт корпус В ПАМЯТИ и потому не заводит ни репозитория, ни временного
// индекса — чужое состояние остаётся нетронутым (`multi-agent-flow.md` §13).
type adviceSource struct {
	Rel  string
	Body string
}

// adviceFinding — один совет, называющий необъявленный глагол.
type adviceFinding struct {
	Rel   string
	Line  int
	Form  string
	Shape string
	// Named — имя ровно так, как оно записано в прозе.
	Named string
	// DeclaredIn — службы, которые этот глагол объявляют. Пусто означает «нигде
	// в дереве»; непустое при находке означает «глагол есть, но НЕ здесь, и
	// служба-владелец в совете не названа».
	DeclaredIn []string
	Sentence   string
}

// adviceCensus — исход обхода ВМЕСТЕ с объёмом осмотренного.
//
// Обе величины по каждой оси, а не одна: «предложений осмотрено» без «из них
// советов» и «советов» без «глаголов проверено» скрывают ровно тот случай, ради
// которого гейт заведён, — распознаватель ослеп, а находок ноль.
type adviceCensus struct {
	Files    int
	Foreign  []string
	Services int
	Verbs    int
	// Blocks — блоков комментариев (подряд идущих строк `//`).
	Blocks          int
	Sentences       int
	AdviceSentences int
	// ByForm — сколько раз сработала каждая форма записи совета.
	ByForm map[string]int
	// ByShape — сколько кандидатов дала каждая форма записи ИМЕНИ.
	ByShape map[string]int
	// Checked — глаголов проверено (уникальных имён внутри предложения-совета).
	Checked int
	// CrossService — советов, законно указавших на глагол СОСЕДА: служба названа
	// явно, она не та, в которой стоит комментарий, и глагол она объявляет.
	//
	// Ось заведена ради ДОПУЩЕНИЯ, которое иначе осталось бы непроверенным:
	// «советуемый глагол обязан быть в той же службе». Он не обязан — и это не
	// предположение, а замер, печатаемый на каждом прогоне. Ноль здесь означал бы,
	// что ветка разбора имени через службу-владельца не исполняется в дереве
	// вовсе, то есть держится одной инъекцией; непустое означает, что форма живая
	// и покрыта.
	CrossService int
	Findings     []adviceFinding
}

// adviceIndex — что дерево ОБЪЯВЛЯЕТ: службы, их глаголы и границы тел служб.
type adviceIndex struct {
	// serviceVerbs — служба → множество её глаголов.
	serviceVerbs map[string]map[string]bool
	// verbOwners — глагол → службы, которые его объявляют.
	verbOwners map[string]map[string]bool
	// messages — объявленные сообщения. Имя, которое дерево объявляет
	// СООБЩЕНИЕМ и не объявляет глаголом, ссылкой на глагол не является:
	// `вместо BlockRequest` говорит о теле запроса, а не о том, что вызвать.
	messages map[string]bool
	// spans — файл → отрезки строк, занятые телом каждой службы.
	spans map[string][]adviceSpan
	// dirServices — каталог → службы, объявленные в нём.
	dirServices map[string]map[string]bool
}

type adviceSpan struct {
	Service    string
	From, Thru int
}

var (
	adPackageRe = regexp.MustCompile(`^\s*package\s+([A-Za-z0-9_.]+)\s*;`)
	adServiceRe = regexp.MustCompile(`^\s*service\s+([A-Za-z0-9_]+)\s*\{`)
	adMessageRe = regexp.MustCompile(`^\s*message\s+([A-Za-z0-9_]+)\s*\{`)
	// adRPCRe — объявление глагола. Пробел между `rpc` и именем обязателен,
	// скобка после имени — вплотную ЛИБО через пробел: в этом дереве пишут обе
	// формы (`rpc Get (Req)` в iam, `rpc Get(Req)` в nlb), и образец, знающий
	// одну, недобирал бы молча целую службу.
	adRPCRe = regexp.MustCompile(`^\s*rpc\s+([A-Za-z0-9_]+)\s*\(`)
	// adCommentRe — строка комментария контракта.
	adCommentRe = regexp.MustCompile(`^\s*//\s?(.*)$`)
)

// adviceIsOurs — контракт наш, если он объявляет пакет платформы.
//
// Признак берётся из СОДЕРЖИМОГО, а не из пути: путь переезжает, объявление
// пакета — нет.
func adviceIsOurs(body string) bool {
	for _, ln := range strings.Split(body, "\n") {
		if m := adPackageRe.FindStringSubmatch(ln); m != nil {
			return strings.HasPrefix(m[1], "kacho.")
		}
	}
	return false
}

// buildAdviceIndex читает объявления служб и глаголов.
func buildAdviceIndex(sources []adviceSource) *adviceIndex {
	ix := &adviceIndex{
		serviceVerbs: map[string]map[string]bool{},
		verbOwners:   map[string]map[string]bool{},
		spans:        map[string][]adviceSpan{},
		dirServices:  map[string]map[string]bool{},
		messages:     map[string]bool{},
	}
	for _, s := range sources {
		lines := strings.Split(s.Body, "\n")
		dir := filepath.Dir(s.Rel)
		cur, depth, from := "", 0, 0
		for i, raw := range lines {
			// Комментарий отрезается ДО подсчёта скобок: `//` с фигурной
			// скобкой внутри иначе сдвинул бы границу тела службы.
			code := raw
			if j := strings.Index(code, "//"); j >= 0 {
				code = code[:j]
			}
			if m := adMessageRe.FindStringSubmatch(code); m != nil {
				ix.messages[m[1]] = true
			}
			if m := adServiceRe.FindStringSubmatch(code); m != nil {
				cur, depth, from = m[1], 1, i+1
				if ix.serviceVerbs[cur] == nil {
					ix.serviceVerbs[cur] = map[string]bool{}
				}
				if ix.dirServices[dir] == nil {
					ix.dirServices[dir] = map[string]bool{}
				}
				ix.dirServices[dir][cur] = true
				continue
			}
			if cur == "" {
				continue
			}
			depth += strings.Count(code, "{") - strings.Count(code, "}")
			if m := adRPCRe.FindStringSubmatch(code); m != nil {
				ix.serviceVerbs[cur][m[1]] = true
				if ix.verbOwners[m[1]] == nil {
					ix.verbOwners[m[1]] = map[string]bool{}
				}
				ix.verbOwners[m[1]][cur] = true
			}
			if depth <= 0 {
				ix.spans[s.Rel] = append(ix.spans[s.Rel], adviceSpan{Service: cur, From: from, Thru: i + 1})
				cur = ""
			}
		}
	}
	return ix
}

// scope — какие службы «свои» для комментария, стоящего на данной строке.
//
// Порядок сужения: тело службы, охватывающее строку → службы, объявленные в
// ЭТОМ файле → службы, объявленные в этом каталоге (пакете). Последняя ступень
// нужна файлам без служб (`user.proto`, `role.proto`): у комментария там нет
// охватывающей службы вовсе, и всякое голое имя иначе стало бы находкой.
func (ix *adviceIndex) scope(rel string, line int) map[string]bool {
	for _, sp := range ix.spans[rel] {
		if line >= sp.From && line <= sp.Thru {
			return map[string]bool{sp.Service: true}
		}
	}
	inFile := map[string]bool{}
	for _, sp := range ix.spans[rel] {
		inFile[sp.Service] = true
	}
	if len(inFile) > 0 {
		return inFile
	}
	return ix.dirServices[filepath.Dir(rel)]
}

// grpcCodeNames — имена кодов gRPC в написании Go.
//
// Это НЕ список удобства: набор кодов закрыт спецификацией gRPC и в этом дереве
// встречается в прозе постоянно («возвращает FailedPrecondition «Use Invite»»).
// Без него `FailedPrecondition`, `PermissionDenied` и `InvalidArgument` читались
// бы как имена глаголов — измерено, три ложные находки из восьми.
var grpcCodeNames = map[string]bool{
	"OK": true, "Ok": true, "Canceled": true, "Unknown": true,
	"InvalidArgument": true, "DeadlineExceeded": true, "NotFound": true,
	"AlreadyExists": true, "PermissionDenied": true, "ResourceExhausted": true,
	"FailedPrecondition": true, "Aborted": true, "OutOfRange": true,
	"Unimplemented": true, "Internal": true, "Unavailable": true,
	"DataLoss": true, "Unauthenticated": true,
}

// Формы записи СОВЕТА. Каждая доказана инъекцией отдельно
// (`contractadvice_injection_test.go`) — форма, о которой распознаватель не
// знает, даёт не красное и не зелёное, а молчание (`testing.md` §«Гейт на
// класс», п.7).
//
// Кириллические маркеры ограждены классом «не буква»: `\b` в Go считает
// словесными только знаки ASCII, поэтому `звать` без ограждения совпадал бы
// внутри «на-звать» — измерено, одна ложная находка.
// adviceName — имя в прозе, вместе с ОБОИМИ способами его выделить.
//
// Обрамление бывает двух видов, и оба законны в этом дереве: обратные кавычки
// (`GroupService.ListMembers`) и квадратные скобки ([VolumeService.List]) —
// вторая форма пришла из соглашения о перекрёстных ссылках в комментариях
// контрактов и в этом дереве ЧАСТОТНЕЕ первой.
//
// Незнание скобок было слепой зоной, БОЛЬШЕЙ, чем вся наблюдаемая полоса:
// советов в скобочной форме 26 против 22 распознанных всего. Это ровно тот
// класс, ради которого п.7 требует перечислить КАЖДУЮ законную форму записи
// предмета: форма, о которой распознаватель не знает, даёт не красное и не
// зелёное, а молчание.
const adviceName = "[`\\[]?([A-Z][A-Za-z0-9_]*(?:\\.[A-Z][A-Za-z0-9_]*)?)[`\\]]?"

const adviceNonLetter = `(?:^|[^А-Яа-яЁёA-Za-z0-9_])`

type adviceForm struct {
	Name string
	Re   *regexp.Regexp
}

var adviceForms = []adviceForm{
	// F1 — английский повелительный оборот: «use X», «call X», «invoke X».
	//
	// Первая буква директивы — в ОБОИХ написаниях: повелительное предложение
	// начинается с заглавной («Use Invite»), и образец, знающий только строчную,
	// не видел бы ровно ту позицию, где совет и стоит. Найдено собственной
	// инъекцией, а не чтением.
	{"F1-en-directive", regexp.MustCompile(`\b[Uu]se\s+(?:the\s+|a\s+|an\s+)?` + adviceName +
		`|\b[Cc]all\s+(?:the\s+|a\s+|an\s+)?` + adviceName +
		`|\b[Ii]nvoke\s+(?:the\s+|a\s+|an\s+)?` + adviceName)},
	// F3 — русский повелительный оборот.
	{"F3-ru-directive", regexp.MustCompile(adviceNonLetter +
		`(?:вызовите|вызывайте|зовите|звать|обращайтесь|используйте|пользуйтесь)\s+(?:в\s+)?` + adviceName)},
	// F4 — русское замещение: «вместо X», «вместо этого X».
	{"F4-ru-vmesto", regexp.MustCompile(adviceNonLetter + `вместо\s+(?:этого\s+)?` + adviceName)},
	// F5 — русское указание на глагол по имени: «… своим пагинированным
	// глаголом (`GroupService.ListMembers`)».
	{"F5-ru-glagol", regexp.MustCompile(adviceNonLetter + `глагол[а-яё]*[^.;]{0,40}?` + adviceName)},
}

var (
	// F2 — английское замещение: повелительное предложение с одиноким
	// `instead`. Именно этой формой записан #1442.
	adviceInsteadRe    = regexp.MustCompile(`\binstead\b`)
	adviceHeadRe       = regexp.MustCompile(`^` + adviceName + `\b`)
	adviceCamelHumpRe  = regexp.MustCompile(`^[A-Z][a-z0-9]*(?:[A-Z][a-z0-9]*)+$`)
	adviceHasLowercase = regexp.MustCompile(`[a-z]`)
)

// splitAdviceSentences режет блок комментария на предложения.
//
// Go не знает ретроспективных утверждений, поэтому раздел идёт вручную: конец
// предложения — точка, восклицательный или вопросительный знак, за которым
// стоит пробел. Точка внутри `Address.used_by` и `#1102).` предложения не рвёт.
func splitAdviceSentences(block string) []string {
	var out []string
	start := 0
	runes := []rune(block)
	for i := 0; i < len(runes)-1; i++ {
		if runes[i] != '.' && runes[i] != '!' && runes[i] != '?' {
			continue
		}
		if runes[i+1] != ' ' {
			continue
		}
		out = append(out, strings.TrimSpace(string(runes[start:i+1])))
		start = i + 1
	}
	if s := strings.TrimSpace(string(runes[start:])); s != "" {
		out = append(out, s)
	}
	return out
}

// adviceHasBareInstead — в предложении есть ОДИНОКОЕ `instead`.
//
// «instead of» исключено намеренно: это сравнительная проза («X instead of Y»),
// а не совет — измерено, десять вхождений из двадцати в дереве именно такие.
// Проверка написана явным просмотром, а не отрицательным заглядыванием вперёд:
// его в RE2 нет, и попытка записать предикат образцом дала бы отказ сборки —
// либо, что хуже, соблазн ослабить сам предикат под возможности инструмента.
func adviceHasBareInstead(sentence string) bool {
	low := strings.ToLower(sentence)
	for _, loc := range adviceInsteadRe.FindAllStringIndex(low, -1) {
		rest := strings.TrimLeft(low[loc[1]:], " \t")
		if strings.HasPrefix(rest, "of ") || rest == "of" {
			continue
		}
		return true
	}
	return false
}

// adviceShape — форма записи ИМЕНИ, либо пусто, если токен именем глагола не
// является.
func (ix *adviceIndex) adviceShape(tok string) string {
	parts := strings.Split(tok, ".")
	last := parts[len(parts)-1]
	// Акроним без строчных букв (`URL`, `JSON`, `ID`) именем глагола в этом
	// дереве не бывает: все 113 объявленных глаголов несут строчную букву.
	if !adviceHasLowercase.MatchString(last) {
		return ""
	}
	if grpcCodeNames[last] || (len(parts) == 2 && grpcCodeNames[parts[0]]) {
		return ""
	}
	if len(parts) == 2 {
		// `Resource.Verb` — проза о ресурсе (тон отказа
		// `"<field> is immutable after <Resource>.Create"`), а не ссылка на
		// глагол. Ссылкой считается только запись через ОБЪЯВЛЕННУЮ службу.
		if _, ok := ix.serviceVerbs[parts[0]]; !ok {
			return ""
		}
		return "S2-dotted"
	}
	if _, isVerb := ix.verbOwners[last]; !isVerb && ix.messages[last] {
		// Объявленное сообщение — тело запроса или ответа, а не путь действия.
		return ""
	}
	if adviceCamelHumpRe.MatchString(last) {
		return "S3-camel"
	}
	if _, ok := ix.verbOwners[last]; ok {
		return "S4-vocab"
	}
	return ""
}

// auditContractAdvice — обход корпуса контрактов.
func auditContractAdvice(sources []adviceSource) adviceCensus {
	c := adviceCensus{ByForm: map[string]int{}, ByShape: map[string]int{}}

	var ours []adviceSource
	for _, s := range sources {
		if adviceIsOurs(s.Body) {
			ours = append(ours, s)
			continue
		}
		c.Foreign = append(c.Foreign, s.Rel)
	}
	sort.Strings(c.Foreign)
	c.Files = len(ours)

	ix := buildAdviceIndex(ours)
	c.Services = len(ix.serviceVerbs)
	c.Verbs = len(ix.verbOwners)

	for _, s := range ours {
		lines := strings.Split(s.Body, "\n")
		// Блок — подряд идущие строки `//`. Пустая строка и код блок рвут: так
		// же отделяет ведущий комментарий от объявления сам protoc.
		var buf []string
		blockAt := 0
		flush := func() {
			if len(buf) == 0 {
				return
			}
			c.Blocks++
			c.adjudicateBlock(ix, s.Rel, blockAt, buf)
			buf = nil
		}
		for i, raw := range lines {
			m := adCommentRe.FindStringSubmatch(raw)
			if m == nil {
				flush()
				continue
			}
			if len(buf) == 0 {
				blockAt = i + 1
			}
			buf = append(buf, m[1])
		}
		flush()
	}
	sort.Slice(c.Findings, func(i, j int) bool {
		if c.Findings[i].Rel != c.Findings[j].Rel {
			return c.Findings[i].Rel < c.Findings[j].Rel
		}
		return c.Findings[i].Line < c.Findings[j].Line
	})
	return c
}

// adjudicateBlock судит один блок комментария.
func (c *adviceCensus) adjudicateBlock(ix *adviceIndex, rel string, at int, buf []string) {
	joined := strings.Join(buf, " ")
	for _, sent := range splitAdviceSentences(joined) {
		c.Sentences++
		type hit struct{ form, tok string }
		var hits []hit
		for _, f := range adviceForms {
			for _, m := range f.Re.FindAllStringSubmatch(sent, -1) {
				hits = append(hits, hit{f.Name, m[1]})
			}
		}
		if adviceHasBareInstead(sent) {
			if m := adviceHeadRe.FindStringSubmatch(sent); m != nil {
				hits = append(hits, hit{"F2-en-instead", m[1]})
			}
		}
		if len(hits) == 0 {
			continue
		}
		c.AdviceSentences++
		seen := map[string]bool{}
		for _, h := range hits {
			c.ByForm[h.form]++
			shape := ix.adviceShape(h.tok)
			if shape == "" || seen[h.tok] {
				continue
			}
			seen[h.tok] = true
			c.ByShape[shape]++
			c.Checked++

			parts := strings.Split(h.tok, ".")
			var declared bool
			var owners map[string]bool
			if len(parts) == 2 {
				// Служба названа явно — совет вправе указывать на соседа, и это
				// в дереве законная, применяемая форма
				// (`GroupService.ListMembers` в контракте выдач). Тогда судится
				// НАЗВАННАЯ служба, а не та, в которой стоит комментарий.
				declared = ix.serviceVerbs[parts[0]][parts[1]]
				owners = ix.verbOwners[parts[1]]
				if declared && !ix.scope(rel, at)[parts[0]] {
					c.CrossService++
				}
			} else {
				for svc := range ix.scope(rel, at) {
					if ix.serviceVerbs[svc][parts[0]] {
						declared = true
						break
					}
				}
				owners = ix.verbOwners[parts[0]]
			}
			if declared {
				continue
			}
			var where []string
			for o := range owners {
				where = append(where, o)
			}
			sort.Strings(where)
			c.Findings = append(c.Findings, adviceFinding{
				Rel: rel, Line: adviceLineOf(buf, at, h.tok), Form: h.form,
				Shape: shape, Named: h.tok, DeclaredIn: where, Sentence: sent,
			})
		}
	}
}

// adviceLineOf — строка блока, на которой имя записано.
//
// Координата находки обязана указывать в файл, а не в начало блока: блоки здесь
// бывают по тридцать строк, и «где-то выше» стоило бы читателю всего разбора.
func adviceLineOf(buf []string, at int, tok string) int {
	for i, ln := range buf {
		if strings.Contains(ln, tok) {
			return at + i
		}
	}
	return at
}

// Describe — находка словами, вместе с тем, что делать.
//
// Три разных диагноза, а не один: «глагола нет нигде», «служба названа и его не
// объявляет» и «служба не названа, а здесь его нет» требуют разных правок, и
// находка, называющая симптом вместо причины, посылает читателя искать не там
// (`testing.md` §«Гейт на класс», п.8).
func (f adviceFinding) Describe() string {
	where := "его не объявляет НИ ОДНА служба дерева"
	if len(f.DeclaredIn) > 0 {
		where = "объявляют его " + strings.Join(f.DeclaredIn, ", ")
	}
	if i := strings.IndexByte(f.Named, '.'); i >= 0 {
		return fmt.Sprintf("%s:%d [%s/%s] совет называет глагол %q — служба %s в совете "+
			"НАЗВАНА, но глагола %s не объявляет; %s\n      %s",
			f.Rel, f.Line, f.Form, f.Shape, f.Named, f.Named[:i], f.Named[i+1:],
			where, f.Sentence)
	}
	return fmt.Sprintf("%s:%d [%s/%s] совет называет глагол %q — здесь, куда пойдёт "+
		"вызывающий, он не объявлен, и служба-владелец в совете не названа; %s\n      %s",
		f.Rel, f.Line, f.Form, f.Shape, f.Named, where, f.Sentence)
}

// loadContractAdviceSources читает корпус контрактов из дерева.
func loadContractAdviceSources(root string) ([]adviceSource, error) {
	// Корпус — ВСЕ отслеживаемые контракты дерева, а не подкаталог `proto/`:
	// один наш контракт живёт вне его (`gateway/proto/…`), и обход, знающий
	// только канонический каталог, молча не судил бы его. Чужое отделяется
	// объявленным пакетом, а не путём.
	paths, err := treecorpus.UnderWithSuffix(root, ".proto")
	if err != nil {
		return nil, fmt.Errorf("состав контрактов: %w", err)
	}
	var out []adviceSource
	for _, abs := range paths {
		body, rerr := readFileString(abs)
		if rerr != nil {
			return nil, rerr
		}
		rel, rerr := filepath.Rel(root, abs)
		if rerr != nil {
			rel = abs
		}
		out = append(out, adviceSource{Rel: filepath.ToSlash(rel), Body: body})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Rel < out[j].Rel })
	return out, nil
}
