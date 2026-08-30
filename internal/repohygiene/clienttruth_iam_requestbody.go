// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clienttruth_iam_requestbody.go — анализатор «ключ тела запроса в клиентской
// документации существует в сообщении запроса».
//
// # Предмет
//
// Пример `curl` на странице документации — это не иллюстрация, а ИНСТРУКЦИЯ:
// клиент копирует его целиком. Ключ, которого в сообщении запроса нет, не
// вызывает ошибку разбора — край выбрасывает неизвестное поле МОЛЧА
// (`DiscardUnknown`), и запрос доходит до сервиса без него. Дальше исходов два,
// и оба хуже отказа на самом ключе:
//
//   - поле было ОБЯЗАТЕЛЬНЫМ под другим именем → сервис отвечает «<поле> is
//     required» о поле, которое клиент, по его мнению, прислал;
//   - поле СНЯТО с входа и стало выходным → сервис отвергает его отдельной
//     веткой, и отказ читается как «значение не то», хотя верно «поля здесь нет».
//
// Замер на день заведения (kacho#1603 / #1614 / #1615): первая команда быстрого
// старта iam не проходила НИ ПРИ КАКОМ теле, потому что несла `ownerUserId` —
// поле, выведенное из вызывающего и отвергаемое с любым значением, включая
// собственный верный идентификатор. Тело выдачи прав несло `scopeRef` — ключ,
// снятый с контракта тумбстоуном; край отбрасывал его молча, и клиент получал
// три отказа подряд, из которых третий неугадываем.
//
// # Что судит анализатор
//
// Сообщение запроса ВЫВОДИТСЯ из дескрипторов — не из текста `.proto` и не из
// перечня в этом файле. Служба, метод, путь и вид тела читаются из
// зарегистрированных дескрипторов пакета контрактов (`google.api.http`), поэтому
// переименование поля или переезд пути доезжают сюда сами.
//
// В документации распознаются ДВЕ формы команды. `curl`: метод (`-X`, по
// умолчанию GET), адрес и тело (`-d '{…}'`). `grpcurl`: тело и ПОЛНОЕ имя
// службы с методом (`kacho.cloud.iam.v1.AccountService/Create`) — его сопоставлять
// с путём не надо вовсе, и вместе с ним под наблюдение попадают методы
// `Internal*`, у которых HTTP-привязки нет by construction.
//
// Форм ровно столько, сколько нашлось замером; третья, найденная и НЕ покрытая,
// названа в границах ниже. Адрес сопоставляется с шаблоном пути метода
// (сегмент `{…}` матчит любой один сегмент), тело разбирается как JSON, и каждый
// его ключ обязан быть полем сообщения — в любом из ДВУХ написаний, которые
// принимает край: camelCase (`ownerUserId`) и proto (`owner_user_id`). Судится не
// стиль, а исполнимость.
//
// Разбор РЕКУРСИВНЫЙ: вложенный объект судится по полю-сообщению, в котором
// лежит. Иначе `"target": {"allInScope": {}}` проверялся бы только по имени
// `target`, а ветвь внутри — самое неугадываемое место — осталась бы вне
// наблюдения.
//
// # ВТОРОЙ предикат: поле есть в сообщении, но код его ОТВЕРГАЕТ
//
// Первого предиката НЕДОСТАТОЧНО, и это измерено, а не предположено. Прогон на
// возвращённом настоящем дефекте #1603 остался ЗЕЛЁНЫМ: `owner_user_id` из
// `CreateAccountRequest` никуда не снят — поле объявлено, дескриптор его знает,
// а отвергается оно ВЕТКОЙ ПРОВЕРКИ ВХОДА, которой в дескрипторе нет. Гипотеза
// «раз клиент получает отказ, значит поля в сообщении нет» была ложной, и без
// перепроверки гейт объявил бы закрытым класс, которого не видит.
//
// Поэтому набор отвергаемых имён ВЫВОДИТСЯ разбором прод-кода use-case'ов:
// вызов `shared.InvalidArg("<поле>", "<текст>")`, чей текст помечает поле
// невходным (`derived from caller` / `output-only`). Такое имя не вправе стоять
// ключом ни в одном теле, сопоставленном с методом, чьё сообщение это поле
// несёт. Отказ у него ТЕРМИНАЛЬНЫЙ: значение подобрать нельзя, потому что
// отвергается сам факт присутствия ключа.
//
// # ЧЕГО ОН НЕ СУДИТ, и это названо, а не подразумевается
//
//  1. ЗНАЧЕНИЯ не судятся — только имена ключей. Неверный идентификатор зоны или
//     несуществующая роль в примере этим гейтом не ловятся: у них нет предиката
//     в дереве.
//
//  2. ОБЯЗАТЕЛЬНОСТЬ не судится ни одним из двух предикатов. Пример, не
//     назвавший обязательное поле, здесь молчит: `proto3` не отличает «не
//     задано» от «задано нулём», поэтому требование живёт в коде проверки входа,
//     а не в дескрипторе. Это ровно та половина #1615, которую гейт НЕ
//     закрывает, — `target` обязателен по коду, и его отсутствие в примере
//     остаётся за обзором. Симметрию с ВТОРЫМ предикатом («отвергается»
//     выводится из кода, «требуется» — нет) стоит назвать вслух: обязательность
//     выражается десятком разных форм, и распознаватель по одной из них дал бы
//     слепую зону, поданную как покрытие.
//
//  3. КАРТЫ и известные типы обхода не углубляют: у `map<string,string>`
//     (`labels`) ключи произвольны by construction, а у `google.protobuf.Struct`
//     и `Any` — тем более. Рекурсия в них означала бы находки на законном.
//
//  4. ТЕЛА ВНЕ КОМАНДЫ вне охвата, и здесь две разные причины. Блок JSON,
//     показывающий ОТВЕТ, судиться НЕ ДОЛЖЕН: в ответе законны выходные поля,
//     которых на входе нет (`ownerUserId` в ответе Create — верен). А вот тело,
//     нарисованное узлом ДИАГРАММЫ последовательности
//     (`Cli->>GW: POST /iam/v1/accounts<br/>{…}`), судиться должно бы — и не
//     судится: таких мест в дереве пять, они правились руками, и распознаватель
//     под них не заводился, потому что метка узла — свободный текст, а не
//     команда. Это объявленная слепая зона, а не покрытие.
//
//  5. ПУТЬ, НЕ СОПОСТАВИВШИЙСЯ ни с одним методом, находкой НЕ считается, но
//     СЧИТАЕТСЯ переписью отдельным числом: примеры ходят и к соседним доменам
//     (`/vpc/v1/networks`), и объявлять их дефектом значило бы краснеть на
//     законном. Число печатается, чтобы «сопоставилось ноль» не выглядело как
//     «нарушений ноль», — и однажды оно уже отработало: четырнадцать тел
//     инженерной части стояли в нём не потому, что домен чужой, а потому, что
//     распознаватель не знал ГОЛОГО адреса без кавычек. Растущее число здесь —
//     повод проверить распознаватель, а не признак чужого домена.
//
// # Падает на ПУСТОМ ОБХОДЕ
//
// Ноль методов из дескрипторов, ноль прочитанных страниц, ноль разобранных тел
// либо ноль рассуженных ключей — «находок ноль» неотличимо от «прочитано ноль».
package repohygiene

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"

	// Регистрация дескрипторов домена: источник путей, методов и сообщений.
	_ "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
)

// ClientTruthIAMRequestBodyOptions — вход анализатора.
type ClientTruthIAMRequestBodyOptions struct {
	// Tree — СОСТАВ дерева, а не его корень: гейт берёт индекс git
	// (`treecorpus.NewTree`), инъекционная проба — синтетическое дерево
	// (`treecorpus.SyntheticTree`). Разбор — clienttruth_treefiles.go.
	Tree *treecorpus.Tree
	// ProtoPackage — пакет контрактов, чьи дескрипторы задают истину.
	ProtoPackage string
	// DocsDirs — каталоги клиентской документации (от корня дерева).
	DocsDirs []string
	// DocExts — расширения страниц.
	DocExts []string
	// UseCaseDirs — каталоги прод-кода use-case'ов (от корня дерева), откуда выводится
	// набор полей, отвергаемых на входе. Пусто — второй предикат не работает, и
	// анализатор об этом отказывается молчать.
	UseCaseDirs []string
}

// ClientTruthIAMRequestBodyCensus — объём осмотренного.
type ClientTruthIAMRequestBodyCensus struct {
	// Methods — методов с телом выведено из дескрипторов.
	Methods int
	// DocFiles — страниц прочитано.
	DocFiles int
	// CurlBlocks — команд curl распознано.
	CurlBlocks int
	// BodiesParsed — тел разобрано как JSON.
	BodiesParsed int
	// BodiesMatched — тел, чей адрес сопоставился с методом домена.
	BodiesMatched int
	// BodiesUnmatched — тел, чей адрес НЕ сопоставился ни с одним методом домена:
	// чужой домен, адрес из переменной, путь с опечаткой. Не находка, но и не
	// «нарушений ноль» — потому и печатается отдельным числом.
	BodiesUnmatched int
	// KeysJudged — ключей рассужено.
	KeysJudged int
	// RejectedFields — сколько невходных полей выведено из прод-кода.
	RejectedFields int
}

// ClientTruthIAMRequestBodyFinding — один ключ, которого в сообщении нет.
type ClientTruthIAMRequestBodyFinding struct {
	File    string
	Line    int
	Method  string
	Path    string
	Message string
	KeyPath string
	// Rejected — поле в сообщении ЕСТЬ, но код отвергает его присутствие.
	Rejected bool
}

func (f ClientTruthIAMRequestBodyFinding) String() string {
	if f.Rejected {
		return fmt.Sprintf("%s:%d: %s %s — ключ %q есть в %s, но код отвергает его присутствие",
			f.File, f.Line, f.Method, f.Path, f.KeyPath, f.Message)
	}
	return fmt.Sprintf("%s:%d: %s %s — ключа %q нет в %s",
		f.File, f.Line, f.Method, f.Path, f.KeyPath, f.Message)
}

// httpMethodBinding — один метод контракта: глагол, шаблон пути, сообщение входа.
type httpMethodBinding struct {
	verb  string
	tmpl  []string
	input protoreflect.MessageDescriptor
}

var (
	curlLineRe = regexp.MustCompile(`\b(?:grpcurl|curl)\b`)
	verbRe     = regexp.MustCompile(`-X\s+([A-Z]+)`)
	// Адрес пишут ТРЕМЯ законными способами: в одинарных кавычках, в двойных и
	// без кавычек вовсе. Первая редакция знала только кавычки — и не видела
	// ни одного примера инженерной части, где адрес голый: четырнадцать тел
	// уходили в «не сопоставилось», и среди них жил настоящий дефект
	// (`owner_user_id` в теле Create). Распознаватель, не знающий одной из
	// законных форм, не даёт ни красного, ни зелёного — он молчит.
	urlRe  = regexp.MustCompile(`['"](https?://[^'"\s]+)['"]|(https?://[^'"\s\\]+)`)
	bodyRe = regexp.MustCompile(`(?s)-d\s+'(\{.*?\})'`)
	// grpcurl называет метод ПОЛНЫМ именем прямо в команде, поэтому сопоставлять
	// его с шаблоном пути не нужно вовсе — и заодно становятся судимы методы
	// Internal*, у которых HTTP-привязки нет by construction и которые первым
	// распознавателем не наблюдались никак.
	grpcurlRe    = regexp.MustCompile(`\bgrpcurl\b`)
	grpcMethodRe = regexp.MustCompile(`([a-z][\w.]*\.[A-Z]\w*)/(\w+)`)
)

// AuditClientTruthIAMRequestBody требует, чтобы каждый ключ тела запроса в
// клиентской документации существовал в сообщении запроса этого метода.
func AuditClientTruthIAMRequestBody(
	opts ClientTruthIAMRequestBodyOptions, log io.Writer,
) ([]ClientTruthIAMRequestBodyFinding, ClientTruthIAMRequestBodyCensus, error) {
	var census ClientTruthIAMRequestBodyCensus

	bindings, err := collectHTTPBindings(opts.ProtoPackage)
	if err != nil {
		return nil, census, err
	}
	census.Methods = len(bindings)
	if len(bindings) == 0 {
		return nil, census, fmt.Errorf(
			"из дескрипторов пакета %s не выведено ни одного метода с телом — "+
				"судить примеры не по чему", opts.ProtoPackage)
	}

	rejected, rerr := collectRejectedInputFields(opts.Tree, opts.UseCaseDirs)
	if rerr != nil {
		return nil, census, rerr
	}
	census.RejectedFields = len(rejected)
	if len(rejected) == 0 {
		return nil, census, fmt.Errorf(
			"из %v не выведено ни одного невходного поля — второй предикат беспредметен, "+
				"а «находок ноль» получено даром", opts.UseCaseDirs)
	}

	var findings []ClientTruthIAMRequestBodyFinding
	for _, dir := range opts.DocsDirs {
		for _, rel := range clientTruthTreeFiles(opts.Tree, dir, true, opts.DocExts...) {
			raw, rerr := clientTruthReadTreeFile(opts.Tree, rel)
			if rerr != nil {
				return nil, census, fmt.Errorf("чтение %s: %w", rel, rerr)
			}
			census.DocFiles++
			findings = append(findings,
				auditOneDoc(rel, string(raw), bindings, rejected, &census)...)
		}
	}

	if log != nil {
		names := make([]string, 0, len(rejected))
		for n := range rejected {
			names = append(names, n)
		}
		sort.Strings(names)
		_, _ = fmt.Fprintf(log, "перепись: методов с телом %d · страниц %d · команд curl %d · "+
			"тел разобрано %d · сопоставлено с методом %d · адрес не сопоставился %d (НЕ судятся) · "+
			"ключей рассужено %d · невходных полей выведено %d (%s)\n",
			census.Methods, census.DocFiles, census.CurlBlocks, census.BodiesParsed,
			census.BodiesMatched, census.BodiesUnmatched, census.KeysJudged,
			census.RejectedFields, strings.Join(names, ", "))
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		if findings[i].Line != findings[j].Line {
			return findings[i].Line < findings[j].Line
		}
		return findings[i].KeyPath < findings[j].KeyPath
	})
	return findings, census, nil
}

// auditOneDoc разбирает одну страницу.
func auditOneDoc(
	rel, text string, bindings []httpMethodBinding, rejected map[string]bool,
	census *ClientTruthIAMRequestBodyCensus,
) []ClientTruthIAMRequestBodyFinding {
	var out []ClientTruthIAMRequestBodyFinding
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		if !curlLineRe.MatchString(lines[i]) {
			continue
		}
		// Команда продолжается, пока строка кончается признаком продолжения. В
		// шаблонной строке страницы он удвоен (`\\`), в обычном коде — одинарен.
		block := []string{lines[i]}
		j := i
		for j < len(lines)-1 && strings.HasSuffix(strings.TrimRight(lines[j], " \t"), `\`) {
			j++
			block = append(block, lines[j])
		}
		// Тело может продолжаться и после последней строки продолжения — оно
		// многострочное и закрывается кавычкой. Дочитываем до неё.
		joined := strings.Join(block, "\n")
		if strings.Contains(joined, "-d '") && !bodyRe.MatchString(joined) {
			for j < len(lines)-1 && !strings.Contains(lines[j], "}'") {
				j++
				block = append(block, lines[j])
			}
			joined = strings.Join(block, "\n")
		}
		census.CurlBlocks++
		i = j
		isGRPC := grpcurlRe.MatchString(joined)

		m := bodyRe.FindStringSubmatch(joined)
		if m == nil {
			continue
		}
		var body map[string]any
		if err := json.Unmarshal([]byte(m[1]), &body); err != nil {
			// Тело с плейсхолдером вместо JSON — не инструкция и не находка.
			continue
		}
		census.BodiesParsed++

		if isGRPC {
			gm := grpcMethodRe.FindStringSubmatch(joined)
			if gm == nil {
				census.BodiesUnmatched++
				continue
			}
			input, ok := grpcInput(gm[1], gm[2])
			if !ok {
				census.BodiesUnmatched++
				continue
			}
			census.BodiesMatched++
			out = append(out, judgeObject(rel, i+1, "gRPC", gm[1]+"/"+gm[2],
				input, "", body, rejected, census)...)
			continue
		}

		verb := "GET"
		if v := verbRe.FindStringSubmatch(joined); v != nil {
			verb = v[1]
		}
		u := urlRe.FindStringSubmatch(joined)
		if u == nil {
			census.BodiesUnmatched++
			continue
		}
		path := urlPath(firstNonEmpty(u[1:]))
		bind, ok := matchBinding(bindings, verb, path)
		if !ok {
			census.BodiesUnmatched++
			continue
		}
		census.BodiesMatched++
		out = append(out, judgeObject(rel, i+1, verb, path, bind.input, "", body, rejected, census)...)
	}
	return out
}

// judgeObject рекурсивно сверяет ключи объекта с полями сообщения.
func judgeObject(
	rel string, line int, verb, path string, msg protoreflect.MessageDescriptor,
	prefix string, obj map[string]any, rejected map[string]bool,
	census *ClientTruthIAMRequestBodyCensus,
) []ClientTruthIAMRequestBodyFinding {
	var out []ClientTruthIAMRequestBodyFinding
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		census.KeysJudged++
		fd := lookupField(msg, k)
		if fd == nil {
			out = append(out, ClientTruthIAMRequestBodyFinding{
				File: rel, Line: line, Method: verb, Path: path,
				Message: string(msg.FullName()), KeyPath: prefix + k,
			})
			continue
		}
		// ВТОРОЙ предикат: поле в сообщении есть, но код отвергает его присутствие.
		if rejected[fd.JSONName()] || rejected[string(fd.Name())] {
			out = append(out, ClientTruthIAMRequestBodyFinding{
				File: rel, Line: line, Method: verb, Path: path,
				Message: string(msg.FullName()), KeyPath: prefix + k, Rejected: true,
			})
			continue
		}
		// Углубляемся только туда, где имена полей закрыты контрактом: карты и
		// известные типы имеют произвольные ключи by construction.
		if fd.Kind() != protoreflect.MessageKind || fd.IsMap() || isOpaqueMessage(fd.Message()) {
			continue
		}
		nested := fd.Message()
		switch v := obj[k].(type) {
		case map[string]any:
			out = append(out, judgeObject(rel, line, verb, path, nested,
				prefix+k+".", v, rejected, census)...)
		case []any:
			for _, el := range v {
				if em, ok := el.(map[string]any); ok {
					out = append(out, judgeObject(rel, line, verb, path, nested,
						prefix+k+"[].", em, rejected, census)...)
				}
			}
		}
	}
	return out
}

// lookupField принимает ОБА написания, которые принимает край: camelCase и proto.
func lookupField(msg protoreflect.MessageDescriptor, key string) protoreflect.FieldDescriptor {
	if fd := msg.Fields().ByJSONName(key); fd != nil {
		return fd
	}
	return msg.Fields().ByTextName(key)
}

// isOpaqueMessage — сообщение, у которого имена «полей» задаёт не контракт.
func isOpaqueMessage(md protoreflect.MessageDescriptor) bool {
	if md == nil {
		return true
	}
	switch md.FullName() {
	case "google.protobuf.Struct", "google.protobuf.Value", "google.protobuf.Any",
		"google.protobuf.ListValue":
		return true
	}
	return false
}

// collectHTTPBindings выводит методы С ТЕЛОМ из зарегистрированных дескрипторов.
func collectHTTPBindings(pkg string) ([]httpMethodBinding, error) {
	var out []httpMethodBinding
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if string(fd.Package()) != pkg {
			return true
		}
		for si := 0; si < fd.Services().Len(); si++ {
			svc := fd.Services().Get(si)
			for mi := 0; mi < svc.Methods().Len(); mi++ {
				m := svc.Methods().Get(mi)
				rule, hasRule := httpRule(m)
				if !hasRule || rule.GetBody() == "" {
					continue
				}
				verb, tmpl := verbAndTemplate(rule)
				if verb == "" {
					continue
				}
				out = append(out, httpMethodBinding{
					verb: verb, tmpl: splitPath(tmpl), input: m.Input(),
				})
			}
		}
		return true
	})
	return out, nil
}

func httpRule(m protoreflect.MethodDescriptor) (*annotations.HttpRule, bool) {
	opts := m.Options()
	if opts == nil {
		return nil, false
	}
	ext := proto.GetExtension(opts, annotations.E_Http)
	rule, ok := ext.(*annotations.HttpRule)
	if !ok || rule == nil {
		return nil, false
	}
	return rule, true
}

func verbAndTemplate(r *annotations.HttpRule) (string, string) {
	switch p := r.GetPattern().(type) {
	case *annotations.HttpRule_Get:
		return "GET", p.Get
	case *annotations.HttpRule_Post:
		return "POST", p.Post
	case *annotations.HttpRule_Put:
		return "PUT", p.Put
	case *annotations.HttpRule_Patch:
		return "PATCH", p.Patch
	case *annotations.HttpRule_Delete:
		return "DELETE", p.Delete
	default:
		return "", ""
	}
}

func splitPath(p string) []string {
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}

func urlPath(u string) string {
	if i := strings.Index(u, "://"); i >= 0 {
		u = u[i+3:]
	}
	if i := strings.Index(u, "/"); i >= 0 {
		u = u[i:]
	} else {
		u = "/"
	}
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u = u[:i]
	}
	return u
}

// matchBinding сопоставляет адрес примера с шаблоном: `{…}` матчит один сегмент,
// остальное — дословно (включая суффикс `:verb` последнего сегмента).
func matchBinding(bs []httpMethodBinding, verb, path string) (httpMethodBinding, bool) {
	segs := splitPath(path)
	for _, b := range bs {
		if b.verb != verb || len(b.tmpl) != len(segs) {
			continue
		}
		ok := true
		for i, t := range b.tmpl {
			if strings.HasPrefix(t, "{") {
				// Шаблон вида `{id}` либо `{id}:verb` — суффикс обязан совпасть.
				if j := strings.Index(t, "}"); j >= 0 && j+1 < len(t) {
					if !strings.HasSuffix(segs[i], t[j+1:]) {
						ok = false
						break
					}
				}
				continue
			}
			if t != segs[i] {
				ok = false
				break
			}
		}
		if ok {
			return b, true
		}
	}
	return httpMethodBinding{}, false
}

// nonInputMarkers — тексты, которыми отказ помечает поле НЕВХОДНЫМ. Судится
// текст самого отказа, а не имя функции: `InvalidArg` зовётся сотнями мест по
// любому поводу (формат, диапазон, взаимоисключение), и без маркера набор
// вобрал бы каждое проверяемое поле разом — то есть запретил бы присылать всё.
var nonInputMarkers = []string{"derived from caller", "output-only", "compiled/output-only"}

// collectRejectedInputFields выводит имена полей, чьё ПРИСУТСТВИЕ на входе
// прод-код отвергает, — разбором вызовов `shared.InvalidArg("<поле>", "<текст>")`.
//
// Разбор, а не поиск по образцу: те же имена стоят в комментариях рядом с самими
// ветками (и в этом файле тоже), поэтому гейт по подстроке краснел бы на
// собственном объяснении. Судится узел-вызов и его строковые аргументы.
func collectRejectedInputFields(tree *treecorpus.Tree, dirs []string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, dir := range dirs {
		for _, rel := range clientTruthTreeFiles(tree, dir, true, ".go") {
			if strings.HasSuffix(rel, "_test.go") {
				continue
			}
			// Исходник подаётся разбору ТЕКСТОМ, а не именем файла: имя открыл бы
			// файл сам разбор, то есть чтение вернулось бы в обход. Здесь оно
			// одно и то же для всех — [clientTruthReadTreeFile].
			src, rerr := clientTruthReadTreeFile(tree, rel)
			if rerr != nil {
				return nil, fmt.Errorf("чтение %s: %w", rel, rerr)
			}
			fset := token.NewFileSet()
			file, perr := parser.ParseFile(fset, rel, src, 0)
			if perr != nil {
				return nil, fmt.Errorf("разбор %s: %w", rel, perr)
			}
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) < 2 {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "InvalidArg" {
					return true
				}
				field, ok1 := clientTruthStringLit(call.Args[0])
				msg, ok2 := clientTruthStringLit(call.Args[1])
				if !ok1 || !ok2 || field == "" {
					return true
				}
				for _, marker := range nonInputMarkers {
					if strings.Contains(msg, marker) {
						out[field] = true
						break
					}
				}
				return true
			})
		}
	}
	return out, nil
}

// grpcInput резолвит сообщение входа по ПОЛНОМУ имени службы и метода — так,
// как его называет сама команда. Служба вне регистра (соседний домен, опечатка)
// находкой не считается: она уходит в «адрес не сопоставился».
func grpcInput(service, method string) (protoreflect.MessageDescriptor, bool) {
	d, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(service))
	if err != nil {
		return nil, false
	}
	sd, ok := d.(protoreflect.ServiceDescriptor)
	if !ok {
		return nil, false
	}
	md := sd.Methods().ByName(protoreflect.Name(method))
	if md == nil {
		return nil, false
	}
	return md.Input(), true
}

func clientTruthStringLit(e ast.Expr) (string, bool) {
	bl, ok := e.(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(bl.Value)
	if err != nil {
		return "", false
	}
	return s, true
}
