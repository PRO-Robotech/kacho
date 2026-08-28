// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// subscriptionformsingularity.go — анализатор «запрос подписки объявлен ОДИН раз».
//
// # Что он считает
//
// Объявления запроса подписки в дереве контракта: общее — в пакете
// `kacho.cloud.subscription`, доменные — где угодно ещё. Ожидаемое число
// ВЫВОДИТСЯ: одна общая форма плюс столько доменных, сколько стоит в ведомости
// послаблений. Снял запись — ожидание уменьшилось само; выписанной константы,
// которую надо помнить и править, здесь нет.
//
// # Чем запрос подписки ОПОЗНАЁТСЯ
//
// Тремя признаками, и каждый САМОСТОЯТЕЛЬНО достаточен. Перепись печатает объём
// по КАЖДОЙ ветви отдельно: ветвь, у которой предмета ноль, не наблюдает ничего,
// и её молчание обязано быть отличимо от чистого дерева.
//
//	ПО УПОТРЕБЛЕНИЮ  сообщение стоит ВХОДОМ серверно-потокового глагола
//	                 (`rpc … (Msg) returns (stream …)`). Имени не читает вовсе:
//	                 поток и есть подписка, одиночный ответ так не объявляют.
//	ПО СОСТАВУ       сообщение несёт ПОЗИЦИЮ ВОЗОБНОВЛЕНИЯ и ОСЬ ВИДОВ предмета,
//	                 и при этом НЕ несёт размера страницы.
//	ПО ИМЕНИ         имя из закрытого семейства (`WatchRequest`,
//	                 `SubscribeRequest`, суффикс `SubscriptionRequest`).
//
// # ЧЕМ ЭТОТ ПРИЗНАК НЕ ЯВЛЯЕТСЯ — читать прежде, чем «упростить»
//
// Он НЕ ЕСТЬ ИМЯ СООБЩЕНИЯ. Ветвь имени оставлена третьей и не расширяется: она
// достаточна, но не необходима, и одной её было бы мало. Гейт, судивший ТОЛЬКО
// по имени, снимался ПЕРЕИМЕНОВАНИЕМ — не злым умыслом, а обычной свободой
// автора нового домена: имя своему сообщению он выбирает сам, и
// `WatchNetworksRequest` ничем не хуже прочих. Это проверено опытом, а не
// прочитано: сообщение подписки под чужим именем читалось анализатором (перепись
// росла на файл и на сообщение) и НЕ давало ни одной находки.
//
// Поэтому: вернуть узнавание к одному имени «для простоты» — значит вернуть
// слепоту, которой эта редакция и посвящена. Ветвь снимается только вместе со
// своим предметом, и снятие обязано пройти инъекцию заново.
//
// Он НЕ СВОБОДЕН ОТ СЛОВАРЕЙ, и это сказано прямо. Ветвь употребления имён не
// читает совсем. Ветвь состава читает имена ПОЛЕЙ — и различие с именем
// сообщения не косметическое: имя поля ЕСТЬ ПРОВОД (оно едет в JSON и
// перечисляется значениями `SubscriptionOpened.honored_filters`), его смена —
// ломающее изменение; имя сообщения провода не образует и меняется даром.
// Словари полей закрыты и объявлены ниже переменными — расширяются они правкой
// с инъекцией, а не по вкусу читающего.
//
// Он НЕ ЛОВИТ ПОДПИСКУ БЕЗ ОСИ ВИДОВ, пока она не провязана глаголом. Узость
// названа, а не скрыта: ось видов — то, чем подписка структурно отличается от
// страничного списка (у списка вид предмета задан ГЛАГОЛОМ и в запросе не
// называется). Подписка на один вид без такой оси опознаётся ветвью
// употребления, то есть с момента, когда её вообще можно позвать.
//
// Он НЕ ЛОВИТ ОСЬ, НАЗВАННУЮ ВНЕ СЛОВАРЯ, и практически это та же слепота, но
// наступает она чаще: `repeated string categories` рядом с позицией даёт
// молчание ветви состава. Причём наступает она у НОВОГО ДОМЕНА — то есть ровно
// у того, ради кого гейт написан. Противоядие то же и уже названо: ветвь
// употребления имён не читает вовсе, поэтому провязанная подписка опознаётся при
// любом словаре. Расширять словарь по одному имени, не заводя инъекции на него,
// — значит наращивать вид покрытия вместо покрытия.
//
// # Почему не поиск по тексту
//
// Слово `message` встречается в комментариях, а комментариев в этом дереве
// больше, чем объявлений. Поэтому текст сперва вычищается от комментариев, и
// только потом считаются объявления ВЕРХНЕГО УРОВНЯ — вложенное сообщение с
// подходящим именем формой подписки не является и в счёт не идёт.
//
// РАЗБОРОМ ТЕЛА здесь берётся ГРАНИЦА: тело вложенного сообщения в счёт полей
// владельца не идёт, а тело `oneof` — идёт (позиция общей формы лежит именно
// там). Само извлечение поля внутри этой границы — регулярка, и это сказано
// прямо: прежняя редакция называла разбором и её тоже, а фраза «разбором, а не
// построчным поиском» ровно тем и опасна, что читается как «формы записи
// покрыты» и удерживает следующего от проверки. Формы перечислены у `subFieldRe`
// поимённо, и у каждой своя инъекция; сверх того полнота разбора проверяется
// НЕЗАВИСИМЫМ счётом номеров полей — расхождение двух счётов есть отказ, а не
// молчание.
//
// Отдельно: `git grep -c` для этого не годится вовсе — он печатает `файл:число`
// по каждому файлу, а через `wc -l` считает ФАЙЛЫ, а не объявления. Два запроса
// в одном файле дали бы единицу.
//
// # Почему гейт, а не договорённость
//
// Второй язык фильтров заводится не злым умыслом, а естественным ходом: домену
// нужна подписка, общая форма чем-то не подошла, и рядом появляется своя — со
// своим набором осей, своей позицией и своим смыслом пустого поля. Через семь
// доменов их семь, и «единая подписка» существует только в названии эпика.
// Заметить это на обзоре нельзя: каждое отдельное объявление защитимо.
//
// # Ведомость послаблений и её самоистечение
//
// Доменное объявление, ещё не переведённое на общую форму, стоит в ведомости с
// причиной и условием истечения. Запись, которой в дереве больше нечего
// исключать, — САМА НАХОДКА: иначе она переживёт своё снятие и разрешит
// следующему завести доменный запрос под тем же именем.
//
// # Пустая ведомость — это ЦЕЛЬ, а не поломка
//
// Соседнее надгробие (`retiredrpcsurface.go`) на пустой переписи падает, и это
// верно ДЛЯ НЕГО: надгробие не истекает, пустое означает «гейту нечего
// охранять». Здесь всё наоборот — пустая ведомость означает, что переводить
// больше нечего, то есть ровно то состояние, ради которого эпик и заведён.
// Падение на ней толкало бы держать запись ради зелёного.
//
// Падает анализатор на ПУСТОМ ОБХОДЕ: ноль прочитанных файлов контракта или ноль
// прочитанных сообщений — тогда «ноль находок» неотличимо от «ноль прочитанного».
package repohygiene

import (
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// SubscriptionCommonPackage — пакет, в котором форма подписки объявляется один раз.
const SubscriptionCommonPackage = "kacho.cloud.subscription"

// SubscriptionCommonRequest — имя общей формы запроса подписки.
const SubscriptionCommonRequest = "SubscriptionRequest"

// SubscriptionRequestAllowance — одно послабление: доменный запрос подписки,
// который ещё не переведён на общую форму.
type SubscriptionRequestAllowance struct {
	// Symbol — полное имя сообщения (`kacho.cloud.compute.v1.WatchRequest`).
	Symbol string
	// Issue — РАЗРЕШИМАЯ ссылка на задачу перевода. Послабление без задачи не
	// может истечь даже в принципе: нечему закрыться.
	Issue string
	// Reason — почему домен ещё не переведён.
	Reason string
	// ExpiresWhen — предикат снятия записи, а не «когда-нибудь потом».
	ExpiresWhen string
}

// SubscriptionSingularityOptions — вход анализатора.
type SubscriptionSingularityOptions struct {
	// Root — корень репозитория.
	Root string
	// ProtoRoot — путь (относительно Root) к дереву исходного контракта.
	ProtoRoot string
	// Allow — ведомость послаблений. ПУСТАЯ ведомость законна и является целью.
	Allow []SubscriptionRequestAllowance
}

// SubscriptionSingularityCensus — то, что анализатор прочитал.
type SubscriptionSingularityCensus struct {
	ProtoFiles       int
	TopLevelMessages int
	NestedMessages   int
	// MessagesWithFields — сообщений, у которых разбор тела дал хоть одно поле.
	// Ноль здесь означает, что ветвь состава НЕ НАБЛЮДАЛА НИЧЕГО: её молчание —
	// не «чисто», а «не читал».
	MessagesWithFields int
	// FieldsParsed — ПОЛЕЙ разобрано. Это и есть объём ветви состава: сообщение,
	// у которого прочитано одно поле из трёх, по числу сообщений неотличимо от
	// прочитанного целиком, а по числу полей — отличимо.
	FieldsParsed int
	// FieldNumbersSeen — номеров полей встречено независимым счётом. Расходится с
	// FieldsParsed ровно тогда, когда в теле есть форма записи, которой
	// распознаватель не знает.
	FieldNumbersSeen int
	// RPCs — глаголов прочитано; StreamingRPCs — из них серверно-потоковых.
	// Объём ветви употребления: ноль глаголов означает то же самое.
	RPCs          int
	StreamingRPCs int
	// ByName / ByStreaming / ByShape — сколько сообщений опознала КАЖДАЯ ветвь.
	// Сумма больше RequestDecls: одно сообщение опознаётся несколькими сразу, и
	// это не ошибка счёта, а свойство дизъюнкции. Ветвь с нулём — либо предмета
	// нет, либо распознаватель слеп, и различить это можно только по объёму выше.
	ByName      int
	ByStreaming int
	ByShape     int
	// RequestDecls — объявлений запроса подписки найдено (общих и доменных вместе).
	RequestDecls int
	CommonDecls  int
	DomainDecls  int
	Allowances   int
	// Expected — выведенное ожидание: одна общая форма плюс ведомость.
	Expected int
}

// SubscriptionSingularityFinding — одна находка.
type SubscriptionSingularityFinding struct {
	// Kind — "undeclared-domain-request" | "stale-allowance" |
	// "missing-common-form" | "duplicate-common-form" | "second-form-in-common-package".
	Kind   string
	Symbol string
	Where  string
	Reason string
}

func (f SubscriptionSingularityFinding) String() string {
	where := f.Where
	if where == "" {
		where = "в дереве не найдено"
	}
	return f.Kind + " " + f.Symbol + " (" + where + "): " + f.Reason
}

var (
	subMessageRe = regexp.MustCompile(`\bmessage\s+([A-Za-z0-9_]+)`)
	subLineRe    = regexp.MustCompile(`(?m)//.*$`)
	subBlockRe   = regexp.MustCompile(`(?s)/\*.*?\*/`)
	// subFieldRe — поле сообщения: необязательный модификатор, тип, имя, номер.
	//
	// # ЧЕТЫРЕ законные формы записи, и распознаватель обязан знать ВСЕ
	//
	// Прежняя редакция была привязана к началу строки и не допускала модификатора
	// перед типом, поэтому знала ОДНУ форму из четырёх. Остальные три законны и
	// обычны, а не редки:
	//
	//	каждое поле своей строкой                 — знала
	//	`optional string watermark = 2;`          — НЕ знала
	//	`oneof start { string x = 2; }` в строку  — НЕ знала
	//	два поля в одной строке                   — НЕ знала
	//
	// Цена слепоты ДВУСТОРОННЯЯ, и вторая сторона хуже первой. Прямая: подписка,
	// записанная незнакомой формой, невидима. Обратная: незнакомой формой
	// исчезает ДИСКРИМИНАТОР — страничный список с `optional int32 page_size`
	// объявлялся подпиской, и находка дословно утверждала «без размера страницы»
	// при размере, стоящем третьей строкой. Гейт, краснеющий на верном коде,
	// отключают первым.
	//
	// Поэтому: якоря строки нет, модификатор принимается, `map<…>` разбирается
	// наравне с прочими типами. Расширение доказано инъекцией ПО КАЖДОЙ из
	// четырёх форм отдельно, а не одной пробой на все.
	subFieldRe = regexp.MustCompile(
		`\b(?:(repeated|optional|required)\s+)?(?:map\s*<[^>]*>|[A-Za-z_][A-Za-z0-9_.]*)\s+([a-z_][a-z0-9_]*)\s*=\s*\d+\s*[\[;]`)
	// subFieldNumberRe — НЕЗАВИСИМЫЙ счётчик номеров полей в теле.
	//
	// Он нужен не для разбора, а для того, чтобы слепота распознавателя БЫЛА
	// НАБЛЮДАЕМА. Без него сообщение, у которого одно поле молча выброшено,
	// засчитывается в перепись как «поля разобраны», и «находок нет» у ветви
	// состава означает «не читал» — ровно та неотличимость, которую гейт обязан
	// закрывать у других и не вправе иметь у себя.
	//
	// Предикат намеренно ГРУБЕЕ разбора и ключуется на том же терминаторе:
	// присвоение номера, за которым идёт `;` либо начало блока опций. Расхождение
	// двух счётов есть форма записи, которой распознаватель не знает.
	subFieldNumberRe = regexp.MustCompile(`=\s*\d+\s*[\[;]`)
	// subRPCRe — глагол: имя, тип запроса, признак потока в ответе.
	subRPCRe = regexp.MustCompile(`\brpc\s+[A-Za-z0-9_]+\s*\(\s*(stream\s+)?([A-Za-z0-9_.]+)\s*\)\s*returns\s*\(\s*(stream\s+)?([A-Za-z0-9_.]+)\s*\)`)
)

// subResumePositionFields — СЛОВАРЬ ПОЗИЦИИ ВОЗОБНОВЛЕНИЯ: имена полей, которыми
// в этом дереве говорят «продолжить отсюда».
//
// Словарь ЗАКРЫТ и намеренно ШИРОК: сюда входит и `page_token`, хотя он же
// употребляется страничным списком. Отделяет их не он, а РАЗМЕР СТРАНИЦЫ (ниже):
// выдача, у которой есть размер страницы, конечна, а поток бесконечен и размера
// не имеет by construction. Сузить словарь, выкинув `page_token`, значило бы
// снять с проверки исключения ту работу, ради которой она стоит, — и снятие
// перестало бы быть заметным.
var subResumePositionFields = map[string]bool{
	"position": true, "start_position": true, "from_position": true,
	"cursor": true, "start_cursor": true, "from_cursor": true,
	"resume_token": true, "resume_from": true, "continuation_token": true,
	"page_token": true, "next_page_token": true,
	"from_sequence_no": true, "sequence_no": true, "from_sequence": true,
	"from_revision": true, "since_revision": true, "since_token": true,
	"checkpoint": true, "watermark": true, "offset": true,
}

// subPageSizeFields — СЛОВАРЬ РАЗМЕРА СТРАНИЦЫ: признак КОНЕЧНОЙ выдачи.
//
// Сообщение, называющее размер страницы, есть запрос списка, а не подписки:
// поток не заканчивается, и спрашивать «сколько за раз» у него не о чем. Это и
// есть дискриминатор между двумя семействами, и он структурный — про форму
// выдачи, а не про имя сообщения.
var subPageSizeFields = map[string]bool{
	"page_size": true, "limit": true, "max_results": true,
	"max_items": true, "page_limit": true,
}

// subKindsAxisFields — СЛОВАРЬ ОСИ ВИДОВ: чем подписка структурно отличается от
// страничного списка.
//
// У списка вид предмета задан ГЛАГОЛОМ (`ListNetworks` перечисляет сети) и в
// запросе не называется вовсе. Подписка одним потоком отдаёт разные виды, потому
// и вынуждена спрашивать, какие именно, — отсюда ось. Ось МНОЖЕСТВЕННА: одиночное
// `kind` — это поле СОБЫТИЯ, говорящее, чем оказался предмет, а не ось запроса,
// сужающая поток (`SubscriptionEvent.kind` — ровно такое поле, и засчитывать его
// за ось значило бы опознать событие за запрос).
var subKindsAxisFields = map[string]bool{
	"kinds": true, "resource_kinds": true, "object_kinds": true,
	"types": true, "resource_types": true, "object_types": true,
	"subject_types": true, "event_types": true, "event_kinds": true,
}

// subFieldDecl — одно поле сообщения.
type subFieldDecl struct {
	Name     string
	Repeated bool
}

// subMessageDecl — одно объявление сообщения ВЕРХНЕГО УРОВНЯ.
type subMessageDecl struct {
	Symbol string
	Name   string
	Pkg    string
	Where  string
	Fields []subFieldDecl
}

// subBlindSpot — тело, в котором разбор прочитал НЕ ВСЕ поля.
//
// Существует затем, чтобы слепота распознавателя была ВИДНА. Гейт, требующий от
// других отличать «ноль находок» от «ноль прочитанного», не вправе иметь эту
// неотличимость у себя: сообщение с одним выброшенным полем по числу СООБЩЕНИЙ
// неотличимо от прочитанного целиком.
type subBlindSpot struct {
	Symbol  string
	Where   string
	Parsed  int
	Numbers int
}

// subEvidence — ЧЕМ сообщение опознано. Ветви независимы и суммируются: одно
// сообщение бывает опознано несколькими, и каждая ветвь считается отдельно —
// иначе прибавка распознавателя неотличима от холостой.
type subEvidence struct {
	ByName      bool
	ByStreaming bool
	ByShape     bool
}

func (e subEvidence) any() bool { return e.ByName || e.ByStreaming || e.ByShape }

// Why — чем опознано, словами: читается в находке, чтобы автор понимал, какую
// именно ветвь он задел, и не «чинил» её переименованием.
func (e subEvidence) Why() string {
	var by []string
	if e.ByStreaming {
		by = append(by, "стоит входом серверно-потокового глагола")
	}
	if e.ByShape {
		by = append(by, "несёт позицию возобновления и ось видов без размера страницы")
	}
	if e.ByName {
		by = append(by, "названо именем из семейства запроса подписки")
	}
	return strings.Join(by, "; ")
}

// isSubscriptionRequestName — ВЕТВЬ ИМЕНИ: принадлежит ли имя закрытому семейству.
//
// Ветвь ДОСТАТОЧНА, но НЕ НЕОБХОДИМА, и это главное про неё. Одной её было
// мало: гейт, судивший только по имени, снимался переименованием (см. шапку
// файла). Она оставлена, потому что сообщение, названное так, есть запрос
// подписки независимо от состава, — а не потому, что признак сводится к имени.
func isSubscriptionRequestName(name string) bool {
	return name == "WatchRequest" ||
		name == "SubscribeRequest" ||
		strings.HasSuffix(name, "SubscriptionRequest")
}

// subHasShapeOfSubscriptionRequest — ВЕТВЬ СОСТАВА: позиция возобновления плюс
// ось видов при отсутствии размера страницы.
//
// Все три условия вместе. Позиция без оси видов есть страничный список либо
// служебное сообщение потока (`SubscriptionOpened.position`); ось видов без
// позиции — обычный отбор; размер страницы — прямое утверждение о КОНЕЧНОЙ
// выдаче, то есть о том, что это не поток.
func subHasShapeOfSubscriptionRequest(fields []subFieldDecl) bool {
	var position, kindsAxis, pageSize bool
	for _, f := range fields {
		switch {
		case subPageSizeFields[f.Name]:
			pageSize = true
		case subResumePositionFields[f.Name]:
			position = true
		}
		if f.Repeated && subKindsAxisFields[f.Name] {
			kindsAxis = true
		}
	}
	return position && kindsAxis && !pageSize
}

// subParseFields читает поля тела сообщения: тело `oneof` СЧИТАЕТСЯ своим (там
// лежит выбор начала общей формы), тело вложенного `message`/`enum` — НЕ
// считается, оно принадлежит другому типу.
func subParseFields(body string) ([]subFieldDecl, int) {
	var out []subFieldDecl
	// Вырезаем вложенные message/enum вместе с телом; oneof оставляем.
	for {
		loc := subNestedHeadRe.FindStringIndex(body)
		if loc == nil {
			break
		}
		open := strings.Index(body[loc[0]:], "{")
		if open < 0 {
			break
		}
		open += loc[0]
		end := subMatchingBrace(body, open)
		if end < 0 {
			break
		}
		body = body[:loc[0]] + body[end+1:]
	}
	for _, m := range subFieldRe.FindAllStringSubmatch(body, -1) {
		out = append(out, subFieldDecl{Name: m[2], Repeated: strings.TrimSpace(m[1]) == "repeated"})
	}
	return out, len(subFieldNumberRe.FindAllString(body, -1))
}

var subNestedHeadRe = regexp.MustCompile(`\b(message|enum)\s+[A-Za-z0-9_]+\s*\{`)

// subMatchingBrace возвращает позицию `}`, закрывающей `{` в open, либо -1.
func subMatchingBrace(s string, open int) int {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// AuditSubscriptionFormSingularity читает дерево контракта и возвращает
// расхождения между объявленными запросами подписки и ожидаемым составом.
func AuditSubscriptionFormSingularity(
	opts SubscriptionSingularityOptions, out io.Writer,
) ([]SubscriptionSingularityFinding, SubscriptionSingularityCensus, error) {
	var c SubscriptionSingularityCensus
	c.Allowances = len(opts.Allow)
	c.Expected = 1 + c.Allowances

	allow := map[string]SubscriptionRequestAllowance{}
	for _, a := range opts.Allow {
		if !strings.Contains(a.Symbol, ".") {
			return nil, c, fmt.Errorf(
				"запись ведомости %q не имеет формы `<пакет>.<Сообщение>` — сопоставить её не с чем", a.Symbol)
		}
		if a.Issue == "" {
			return nil, c, fmt.Errorf(
				"запись ведомости %q не называет задачу — послабление без задачи не может истечь", a.Symbol)
		}
		allow[a.Symbol] = a
	}

	// ── Проход 1: собрать объявления и потоковые глаголы ─────────────────────
	//
	// Два прохода, а не один, потому что глагол лежит в СОСЕДНЕМ файле: общая
	// форма объявлена в `subscription.proto`, а `Subscribe(SubscriptionRequest)`
	// — в `subscription_service.proto`. Решать по ходу обхода значило бы решать
	// на неполном знании и зависеть от порядка файлов.
	var decls []subMessageDecl
	var blind []subBlindSpot
	streamingInputs := map[string]bool{}

	err := rootedWalk(filepath.Join(opts.Root, opts.ProtoRoot), func(rel string) bool {
		return strings.HasSuffix(rel, ".proto")
	}, func(path string, b []byte) error {
		c.ProtoFiles++
		// Комментарии снимаются ДО всего: скобка в прозе сместила бы уровень
		// вложенности на весь остаток файла, а слово `message` в комментарии
		// засчиталось бы за объявление.
		clean := subBlockRe.ReplaceAllString(string(b), "")
		clean = subLineRe.ReplaceAllString(clean, "")

		pkg := ""
		if m := protoPackageRe.FindStringSubmatch(clean); m != nil {
			pkg = m[1]
		}
		qualify := func(name string) string {
			if strings.Contains(name, ".") || pkg == "" {
				return name
			}
			return pkg + "." + name
		}
		rel, relErr := filepath.Rel(opts.Root, path)
		if relErr != nil {
			rel = path
		}

		for _, m := range subRPCRe.FindAllStringSubmatch(clean, -1) {
			c.RPCs++
			if strings.TrimSpace(m[3]) != "stream" {
				continue
			}
			c.StreamingRPCs++
			streamingInputs[qualify(m[2])] = true
		}

		for _, loc := range subMessageRe.FindAllStringSubmatchIndex(clean, -1) {
			name := clean[loc[2]:loc[3]]
			depth := strings.Count(clean[:loc[0]], "{") - strings.Count(clean[:loc[0]], "}")
			if depth != 0 {
				c.NestedMessages++
				continue
			}
			c.TopLevelMessages++
			open := strings.Index(clean[loc[1]:], "{")
			var fields []subFieldDecl
			if open >= 0 {
				open += loc[1]
				if end := subMatchingBrace(clean, open); end > open {
					var numbers int
					fields, numbers = subParseFields(clean[open+1 : end])
					c.FieldsParsed += len(fields)
					c.FieldNumbersSeen += numbers
					if numbers != len(fields) {
						blind = append(blind, subBlindSpot{
							Symbol: qualify(name), Where: rel,
							Parsed: len(fields), Numbers: numbers,
						})
					}
				}
			}
			decls = append(decls, subMessageDecl{
				Symbol: qualify(name), Name: name, Pkg: pkg, Where: rel, Fields: fields,
			})
		}
		return nil
	})
	if err != nil {
		return nil, c, err
	}
	if c.ProtoFiles == 0 || c.TopLevelMessages == 0 {
		return nil, c, fmt.Errorf(
			"в дереве контракта %q прочитано файлов %d, сообщений верхнего уровня %d — "+
				"гейт не может отличить «ноль находок» от «ноль прочитанного»",
			opts.ProtoRoot, c.ProtoFiles, c.TopLevelMessages)
	}
	// Слепота распознавателя ПОЛЕЙ — отказ, а не молчание. Форма записи, которой
	// он не знает, не является ни краем, ни редкостью: всё записанное в ней
	// уходит из-под наблюдения ветви состава, а перепись продолжает утверждать,
	// что тело прочитано. Отказ самоистекает: научили разбор новой форме — счета
	// сошлись, и отказ пропал сам.
	if len(blind) > 0 {
		var lines []string
		for _, b := range blind {
			lines = append(lines, fmt.Sprintf("  %s (%s): разобрано полей %d, номеров в теле %d",
				b.Symbol, b.Where, b.Parsed, b.Numbers))
		}
		sort.Strings(lines)
		return nil, c, fmt.Errorf(
			"разбор прочитал не все поля — в теле есть форма записи, которой распознаватель "+
				"не знает (тел с расхождением %d, полей разобрано %d из %d номеров). Ветвь "+
				"СОСТАВА на таком теле слепа, и её «находок нет» означает «не читал». Научи "+
				"`subFieldRe` новой форме и докажи инъекцией по ней:\n%s",
			len(blind), c.FieldsParsed, c.FieldNumbersSeen, strings.Join(lines, "\n"))
	}

	// ── Проход 2: опознать ───────────────────────────────────────────────────
	// symbol -> файл, где объявлено
	declaredAt := map[string]string{}
	why := map[string]string{}
	var domain []string
	var common []string

	for _, d := range decls {
		if len(d.Fields) > 0 {
			c.MessagesWithFields++
		}
		e := subEvidence{
			ByName:      isSubscriptionRequestName(d.Name),
			ByStreaming: streamingInputs[d.Symbol],
			ByShape:     subHasShapeOfSubscriptionRequest(d.Fields),
		}
		if e.ByName {
			c.ByName++
		}
		if e.ByStreaming {
			c.ByStreaming++
		}
		if e.ByShape {
			c.ByShape++
		}
		if !e.any() {
			continue
		}
		c.RequestDecls++
		declaredAt[d.Symbol] = d.Where
		why[d.Symbol] = e.Why()
		if d.Pkg == SubscriptionCommonPackage {
			c.CommonDecls++
			common = append(common, d.Symbol)
			continue
		}
		c.DomainDecls++
		domain = append(domain, d.Symbol)
	}

	var findings []SubscriptionSingularityFinding
	sort.Strings(common)
	sort.Strings(domain)

	// ── 1. Общая форма: ровно одна, и именно та ──────────────────────────────
	wantCommon := SubscriptionCommonPackage + "." + SubscriptionCommonRequest
	seenCommon := false
	for _, sym := range common {
		if sym == wantCommon {
			if seenCommon {
				findings = append(findings, SubscriptionSingularityFinding{
					Kind: "duplicate-common-form", Symbol: sym, Where: declaredAt[sym],
					Reason: "общая форма объявлена дважды",
				})
			}
			seenCommon = true
			continue
		}
		findings = append(findings, SubscriptionSingularityFinding{
			Kind: "second-form-in-common-package", Symbol: sym, Where: declaredAt[sym],
			Reason: "в общем пакете объявлен ВТОРОЙ запрос подписки (опознано: " + why[sym] +
				"). Единственная общая форма — " +
				wantCommon + "; второй язык фильтров рядом с ней делает «единую подписку» " +
				"названием, а не свойством",
		})
	}
	if !seenCommon {
		findings = append(findings, SubscriptionSingularityFinding{
			Kind: "missing-common-form", Symbol: wantCommon,
			Reason: "общей формы запроса подписки в дереве нет. Пока её нет, всякое доменное " +
				"объявление — не послабление на время перевода, а единственная форма, и " +
				"следующий домен заведёт свою",
		})
	}

	// ── 2. Доменные объявления: только по ведомости ──────────────────────────
	for _, sym := range domain {
		if _, ok := allow[sym]; ok {
			if out != nil {
				_, _ = fmt.Fprintf(out, "  послабление: %s — %s (%s; истекает: %s)\n",
					sym, allow[sym].Reason, allow[sym].Issue, allow[sym].ExpiresWhen)
			}
			continue
		}
		findings = append(findings, SubscriptionSingularityFinding{
			Kind: "undeclared-domain-request", Symbol: sym, Where: declaredAt[sym],
			Reason: "домен объявил СВОЙ запрос подписки со своим набором осей (опознано: " +
				why[sym] + "). Подписка объявляется один раз в " + SubscriptionCommonPackage +
				", домен её ИМПОРТИРУЕТ. ПЕРЕИМЕНОВАНИЕМ это не снимается: признак — свойство " +
				"объявления, а не форма имени. Если перевод отложен — заведи послабление с " +
				"задачей и предикатом истечения",
		})
	}

	// ── 3. Самоистечение: записи, которой нечего исключать, быть не должно ───
	stale := make([]string, 0, len(allow))
	for sym := range allow {
		if _, ok := declaredAt[sym]; !ok {
			stale = append(stale, sym)
		}
	}
	sort.Strings(stale)
	for _, sym := range stale {
		findings = append(findings, SubscriptionSingularityFinding{
			Kind: "stale-allowance", Symbol: sym,
			Reason: "послаблению больше нечего исключать: объявления с таким именем в дереве нет. " +
				"Запись обязана быть снята — оставленная, она унаследует следующий доменный " +
				"запрос того же имени и сделает его невидимым. Ждала: " + allow[sym].ExpiresWhen,
		})
	}

	// Страховка от расхождения между переписью и находками: если числа не сошлись,
	// а findings пусты, гейт молчит о том, чего сам не объяснил.
	if c.RequestDecls != c.Expected && len(findings) == 0 {
		return nil, c, fmt.Errorf(
			"объявлений запроса подписки %d при ожидаемых %d, а находок ноль — "+
				"анализатор не объяснил расхождение и потому не может быть прочитан как вердикт",
			c.RequestDecls, c.Expected)
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Kind != findings[j].Kind {
			return findings[i].Kind < findings[j].Kind
		}
		return findings[i].Symbol < findings[j].Symbol
	})

	if out != nil {
		_, _ = fmt.Fprintf(out,
			"перепись: файлов контракта %d; сообщений верхнего уровня %d (вложенных %d, "+
				"с разобранными полями %d); полей разобрано %d из %d номеров; "+
				"глаголов %d (серверно-потоковых %d); "+
				"опознано ветвями: по имени %d, по употреблению %d, по составу %d; "+
				"объявлений запроса подписки %d (общих %d, доменных %d); послаблений %d; "+
				"ожидалось %d; находок %d\n",
			c.ProtoFiles, c.TopLevelMessages, c.NestedMessages, c.MessagesWithFields,
			c.FieldsParsed, c.FieldNumbersSeen,
			c.RPCs, c.StreamingRPCs, c.ByName, c.ByStreaming, c.ByShape,
			c.RequestDecls, c.CommonDecls, c.DomainDecls, c.Allowances, c.Expected, len(findings))
	}
	return findings, c, nil
}
