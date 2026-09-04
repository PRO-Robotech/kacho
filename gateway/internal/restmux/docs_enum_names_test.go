// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
)

// docs_enum_names_test.go — ГЕЙТ: имя значения перечисления в примере ответа на
// странице арендатора совпадает с тем, что край действительно ПРОИЗВОДИТ.
//
// # Предмет
//
// Страница арендатора показывает пример ответа, и клиент пишет по нему разбор.
// Сокращённое имя значения (`"state": "ACTIVE"` там, где контракт объявляет
// `RULE_LIFECYCLE_ACTIVE`) не даёт отказа: поле просто не совпадает ни с одной
// веткой разбора, и узнаётся это в чужой отладке. Обратная сторона того же —
// запрос: край отвергает имя вне словаря синхронно (`strict_enum.go`), поэтому
// сокращённая форма, скопированная со страницы в тело, получает `400`.
//
// # Почему гейт, а не вычитка
//
// Форма значения перечисления — свойство СБОРКИ края, а не текста страницы:
// её задаёт `protojson`, которому маршаллер края передаёт свои опции. Прочитать
// страницу и сверить глазами значит закрепить сегодняшнюю сборку в чужой
// памяти. Поэтому здесь два утверждения, и второе связывает первое с
// производителем:
//
//	TestTenantDocExamplesUseDeclaredEnumValueNames — каждое имя значения в
//	    примере есть имя ЭТОГО поля по дескриптору контракта;
//	TestEdgeMarshalerEmitsDeclaredEnumValueNames  — маршаллер края, собранный
//	    ТОЙ ЖЕ функцией, что боевой путь, отдаёт именно объявленное имя.
//
// Порознь ни одно не закрывает класс: первое зеленеет, если край когда-нибудь
// начнёт отдавать НОМЕРА (`UseEnumNumbers`) — имена в примерах останутся
// объявленными и станут ложью; второе ничего не говорит о самих страницах.
//
// # Пример адресуется биндингом, а не догадкой
//
// Страница объявляет операцию разметкой `<ApiOperation method= endpoint=>`, и
// пример внутри неё — тело ОТВЕТА этой операции. Отсюда сообщение, против
// которого пример обходится, берётся из таблицы биндингов (`rest_bindings.go`),
// а не выводится из имени страницы: имя файла к контракту отношения не имеет.
//
// Сверять по ИМЕНИ ПОЛЯ без сообщения нельзя — и это не педантизм: `state` есть
// у нескольких перечислений дерева, и часть из них объявляет значение `ACTIVE`
// голым, без приставки типа. Распознаватель, спрашивающий «объявлено ли такое
// имя где-нибудь», ответил бы «да» на сокращённой форме ЧУЖОГО словаря и
// промолчал бы ровно там, где предмет.
//
// # Две законные записи примера, и обе обязаны читаться
//
// Корпус страниц пишет JSON двумя способами — забором ```json и разметкой
// `<CodeBlock language="json">{dedent`…`}</CodeBlock>`. Знай распознаватель одну,
// вторая была бы не «редкой», а НЕВИДИМОЙ: заборов в корпусе меньшинство.
// Перепись печатает обе формы отдельно, чтобы расширение распознавателя было
// видно числом, а не на слово.
//
// # Чего гейт НЕ проверяет
//
// Упоминание значения в ПРОЗЕ страницы (`<code>ACTIVE</code>` в таблице полей)
// адресовать нечем: слово в тексте не привязано ни к полю, ни к сообщению.
// Согласие прозы с примерами держится обзором, и это сказано прямо, чтобы
// зелёное этого гейта не читалось шире сделанного.

// osReadFile — чтение файла дерева. Обёртка нужна одна на пакет: соседняя проба
// охвата читает те же страницы, и второй вызов с иным набором аннотаций линтера
// разошёлся бы с этим молча.
func osReadFile(path string) ([]byte, error) {
	return os.ReadFile(path) // #nosec G304 -- путь получен обходом СОБСТВЕННОГО дерева репозитория
}

// docsJSONExample — один пример ответа со страницы арендатора.
type docsJSONExample struct {
	// File — путь страницы от корня дерева.
	File string
	// Line — строка, с которой начинается тело примера.
	Line int
	// Method / Endpoint — операция, внутри которой пример объявлен.
	Method   string
	Endpoint string
	// Form — которой из двух записей пример набран (для переписи).
	Form string
	// Title — значение атрибута `title` блока, дословно; пусто, если атрибута
	// нет. Им ОБЪЯВЛЯЕТСЯ полоса примера (ответ · запрос · отказ) — см.
	// `docs_example_keys_test.go`. Снимается здесь, а не вторым обходом
	// страниц: два разбора одной разметки разошлись бы молча.
	Title string
	// Body — сам текст примера.
	Body string
}

// docsEnumFinding — одно имя значения перечисления, которого у этого поля нет.
type docsEnumFinding struct {
	Example docsJSONExample
	FQN     string
	// Field — путь до поля внутри примера.
	Field string
	// Value — то, что написано на странице.
	Value string
	// Allowed — что объявляет контракт, в порядке объявления.
	Allowed []string
}

func (f docsEnumFinding) String() string {
	return fmt.Sprintf("%s:%d [%s %s -> %s] %s: %q (контракт объявляет: %s)",
		f.Example.File, f.Example.Line, f.Example.Method, f.Example.Endpoint, f.FQN,
		f.Field, f.Value, strings.Join(f.Allowed, ", "))
}

var (
	docsApiOperationOpen = regexp.MustCompile(`<ApiOperation\b[^>]*method="([A-Z]+)"[^>]*endpoint="([^"]+)"`)
	docsPathParam        = regexp.MustCompile(`\{[^{}/]*\}`)
	// docsCodeBlockJSON / docsJSONFence — открывающая строка каждой из двух
	// законных записей примера. Регуляркой, а не сравнением со строкой: у блока
	// есть атрибуты помимо `language`, и запись «```json title="…"» законна для
	// забора. Прежнее точное сравнение приняло бы за прозу и то и другое.
	docsCodeBlockJSON = regexp.MustCompile(`<CodeBlock\b[^>]*language="json"[^>]*>`)
	docsJSONFence     = regexp.MustCompile("^```json(?:[ \t]+(.*))?$")
	docsTitleAttr     = regexp.MustCompile(`\btitle="([^"]*)"`)
)

// docsContentRoots — где живут страницы арендатора.
//
// Обе площадки названы: страницы края лежат в `gateway/docs`, а не в
// `services/`, и одна маска их не покрыла бы — их примеры остались бы
// неизмеренными, а перепись выглядела бы исчерпывающей.
func docsContentPages(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	for _, base := range []string{
		filepath.Join(root, "services"),
		filepath.Join(root, "gateway"),
	} {
		err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			ext := filepath.Ext(path)
			if ext != ".mdx" && ext != ".md" {
				return nil
			}
			if !strings.Contains(filepath.ToSlash(path), "/docs/content/") {
				return nil
			}
			out = append(out, path)
			return nil
		})
		if err != nil {
			t.Fatalf("обход %s: %v", base, err)
		}
	}
	sort.Strings(out)
	return out
}

// extractDocsJSONExamples вынимает примеры JSON вместе с операцией, внутри
// которой они объявлены.
//
// Пример вне `<ApiOperation>` возвращается с пустыми Method/Endpoint: он
// СЧИТАЕТСЯ, но не обходится — необойдённый пример не имеет права выглядеть как
// обойдённый и чистый.
func extractDocsJSONExamples(rel, text string) []docsJSONExample {
	lines := strings.Split(text, "\n")
	var out []docsJSONExample
	var method, endpoint string
	var inFence, inBlock bool
	var buf []string
	var start int
	var form, title string

	flush := func() {
		out = append(out, docsJSONExample{
			File: rel, Line: start, Method: method, Endpoint: endpoint,
			Form: form, Title: title, Body: strings.Join(buf, "\n"),
		})
		buf, form, title = nil, "", ""
	}

	for i, line := range lines {
		n := i + 1
		trimmed := strings.TrimSpace(line)
		switch {
		case inFence:
			if strings.HasPrefix(trimmed, "```") {
				inFence = false
				flush()
				continue
			}
			buf = append(buf, line)
		case inBlock:
			if strings.Contains(line, "`}") || strings.Contains(trimmed, "</CodeBlock>") {
				inBlock = false
				flush()
				continue
			}
			// Открывающая строка шаблонного литерала (`{dedent` + обратная
			// кавычка) — разметка, а не тело примера. Не сняв её, распознаватель
			// получал НЕразбираемый JSON у КАЖДОГО примера этой формы, то есть у
			// подавляющего большинства корпуса, и молчал на всех: 110 примеров
			// из 178 считались «не-объектом», при том что объектом они и были.
			if len(buf) == 0 && strings.HasPrefix(trimmed, "{dedent") {
				start = n + 1
				continue
			}
			buf = append(buf, line)
		default:
			if m := docsApiOperationOpen.FindStringSubmatch(line); m != nil {
				method, endpoint = m[1], m[2]
				continue
			}
			if strings.Contains(line, "</ApiOperation>") {
				method, endpoint = "", ""
				continue
			}
			if m := docsJSONFence.FindStringSubmatch(trimmed); m != nil {
				inFence, start, form, title = true, n+1, "fence", docsTitleOf(m[1])
				continue
			}
			if m := docsCodeBlockJSON.FindString(line); m != "" {
				inBlock, start, form, title = true, n+1, "codeblock", docsTitleOf(m)
				continue
			}
		}
	}
	return out
}

// docsTitleOf вынимает значение атрибута `title` из открывающей строки блока
// либо из метастроки забора. Пусто — атрибута нет.
func docsTitleOf(s string) string {
	if m := docsTitleAttr.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return ""
}

// docsResolveResponseMessage — сообщение ОТВЕТА операции примера.
func docsResolveResponseMessage(method, endpoint string) (protoreflect.MessageDescriptor, string, bool) {
	if method == "" || endpoint == "" {
		return nil, "", false
	}
	// Шаблонный сегмент `{roleId}` заменяется литералом: биндинг сверяется по
	// форме пути, а не по значению параметра.
	path := docsPathParam.ReplaceAllString(endpoint, "x")
	b, ok := resolveHTTPBinding(method, path)
	if !ok || b.output == nil {
		return nil, "", false
	}
	return b.output, b.fqn, true
}

// docsExampleEnumFindings — суждение по ОДНОМУ примеру: какие имена значений
// перечислений в нём не объявлены их полем.
//
// Вынесено отдельной функцией не ради красоты: ею правится проба инъекции, и
// правится ТА ЖЕ, что исполняется обходом дерева. Проверка, собранная в пробе
// иначе, чем в боевом пути, — форма без содержания.
//
// Второе возвращаемое — БЫЛ ЛИ пример обойдён. Ложь означает «сверять не с
// чем» (фрагмент, не-объект), и это отдельная величина переписи, а не чистота.
func docsExampleEnumFindings(md protoreflect.MessageDescriptor, fqn string, ex docsJSONExample) ([]docsEnumFinding, bool) {
	var obj map[string]any
	if err := json.Unmarshal([]byte(ex.Body), &obj); err != nil || obj == nil {
		return nil, false
	}
	var found bodyRefusals
	walkEnumValueNames(md, obj, "", &found)
	out := make([]docsEnumFinding, 0, len(found.enums))
	for _, ev := range found.enums {
		out = append(out, docsEnumFinding{
			Example: ex, FQN: fqn, Field: ev.Path, Value: ev.Value, Allowed: ev.Allowed,
		})
	}
	return out, true
}

func TestTenantDocExamplesUseDeclaredEnumValueNames(t *testing.T) {
	root := repoRoot(t)
	pages := docsContentPages(t, root)
	if len(pages) == 0 {
		t.Fatal("страниц арендатора не найдено: гейту нечего сверять, и это отказ, а не проход")
	}

	var findings []docsEnumFinding
	examples, byFence, byBlock := 0, 0, 0
	unbound, unparsed, walked, enumValues := 0, 0, 0, 0

	for _, page := range pages {
		body, err := osReadFile(page)
		if err != nil {
			t.Fatalf("чтение %s: %v", page, err)
		}
		rel := mustRel(root, page)
		for _, ex := range extractDocsJSONExamples(rel, string(body)) {
			examples++
			switch ex.Form {
			case "fence":
				byFence++
			case "codeblock":
				byBlock++
			}
			md, fqn, ok := docsResolveResponseMessage(ex.Method, ex.Endpoint)
			if !ok {
				unbound++
				continue
			}
			got, ok := docsExampleEnumFindings(md, fqn, ex)
			if !ok {
				// Фрагмент с многоточием или не-объект: сверять не с чем.
				// Считается отдельно — «не обойдено» не растворяется в
				// «обойдено и чисто».
				unparsed++
				continue
			}
			walked++
			enumValues += len(got)
			findings = append(findings, got...)
		}
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Example.File != findings[j].Example.File {
			return findings[i].Example.File < findings[j].Example.File
		}
		return findings[i].Example.Line < findings[j].Example.Line
	})

	t.Logf("страниц осмотрено %d; примеров JSON %d (забором %d, разметкой CodeBlock %d); "+
		"привязано к ответу операции %d, БЕЗ операции %d, неразбираемых/не-объектов %d; "+
		"имён значений перечислений вне словаря своего поля %d",
		len(pages), examples, byFence, byBlock, walked, unbound, unparsed, len(findings))

	if walked == 0 {
		t.Fatal("ни один пример не привязан к ответу операции: обход пуст, и вердикт беспредметен")
	}
	if len(findings) == 0 {
		return
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%d значени(й) перечисления в примерах страниц не производятся краем — "+
		"клиент пишет по ним разбор и получает то, чего его код не ждёт:\n", len(findings))
	for _, f := range findings {
		fmt.Fprintf(&b, "  %s\n", f)
	}
	t.Fatal(b.String())
}

// TestEdgeMarshalerEmitsDeclaredEnumValueNames — ВТОРАЯ половина предиката:
// маршаллер края отдаёт именно объявленное имя значения.
//
// Проверяется ПРОГОНОМ, а не чтением опций: `UseEnumNumbers` — свойство сборки,
// и утверждение «край отдаёт имена» обязано падать вместе с ней, а не
// пересказывать её сегодняшнее значение.
//
// Сообщение строится `dynamicpb` по дескриптору РЕАЛЬНОГО поля дерева, а не по
// синтетическому: синтетика доказала бы свойство protojson, а не свойство края.
func TestEdgeMarshalerEmitsDeclaredEnumValueNames(t *testing.T) {
	probes, marshaled, mismatched, report := edgeEnumNameMismatches(newPublicJSONPb())

	t.Logf("перечислений в контрактах kacho.* %d; прогнано через маршаллер края %d; расхождений %d",
		probes, marshaled, mismatched)

	if probes == 0 {
		t.Fatal("перечислений в контрактах не найдено: обход пуст, и вердикт беспредметен")
	}
	if marshaled == 0 {
		t.Fatal("маршаллер не отдал ни одного значения: прогон беспредметен, и его молчание не есть согласие")
	}
	if mismatched > 0 {
		t.Fatalf("%d перечислени(й) край отдаёт не объявленным именем — "+
			"примеры страниц арендатора, показывающие имена, стали ложью:\n%s", mismatched, report)
	}
}

// edgeEnumNameMismatches прогоняет каждое перечисление контрактов через
// ПЕРЕДАННЫЙ маршаллер и возвращает перепись плюс расхождения.
//
// Маршаллер — параметр, а не литерал по месту, чтобы проба инъекции могла
// подать сюда собранный иначе (`UseEnumNumbers`) и увидеть красное: без этого
// «расхождений 0» доказывало бы лишь, что функция умеет считать до нуля.
func edgeEnumNameMismatches(marshaler *runtime.JSONPb) (probeCount, marshaled, mismatched int, report string) {
	type probe struct {
		field protoreflect.FieldDescriptor
		value protoreflect.EnumValueDescriptor
	}
	var probes []probe
	seen := map[protoreflect.FullName]bool{}

	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if !strings.HasPrefix(string(fd.Package()), "kacho.") {
			return true
		}
		var walk func(msgs protoreflect.MessageDescriptors)
		walk = func(msgs protoreflect.MessageDescriptors) {
			for i := 0; i < msgs.Len(); i++ {
				md := msgs.Get(i)
				if md.IsMapEntry() {
					continue
				}
				fields := md.Fields()
				for j := 0; j < fields.Len(); j++ {
					f := fields.Get(j)
					if f.Kind() != protoreflect.EnumKind || f.IsList() || f.IsMap() {
						continue
					}
					ed := f.Enum()
					if seen[ed.FullName()] {
						continue
					}
					seen[ed.FullName()] = true
					vals := ed.Values()
					// Ненулевое значение: нулевое отдаётся и при
					// `EmitUnpopulated`, поэтому на нём проба не отличила бы
					// «поле выставлено» от «поля нет».
					if vals.Len() < 2 {
						continue
					}
					probes = append(probes, probe{field: f, value: vals.Get(1)})
				}
				walk(md.Messages())
			}
		}
		walk(fd.Messages())
		return true
	})

	sort.Slice(probes, func(i, j int) bool {
		return probes[i].field.FullName() < probes[j].field.FullName()
	})

	var b strings.Builder
	for _, p := range probes {
		msg := dynamicpb.NewMessage(p.field.ContainingMessage())
		msg.Set(p.field, protoreflect.ValueOfEnum(p.value.Number()))
		out, err := marshaler.Marshal(msg)
		if err != nil {
			// Сообщение, которое маршаллер не берёт (Any без резолва и т.п.),
			// пробой не является. Считается молчанием этой оси, не успехом.
			continue
		}
		var got map[string]any
		if err := json.Unmarshal(out, &got); err != nil {
			continue
		}
		marshaled++
		want := string(p.value.Name())
		if got[p.field.JSONName()] != want {
			mismatched++
			fmt.Fprintf(&b, "  %s: край отдал %#v, контракт объявляет %q\n",
				p.field.FullName(), got[p.field.JSONName()], want)
		}
	}

	return len(probes), marshaled, mismatched, b.String()
}
