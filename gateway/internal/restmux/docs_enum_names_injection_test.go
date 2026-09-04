// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

import (
	"strings"
	"testing"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// docs_enum_names_injection_test.go — ДОКАЗАТЕЛЬСТВО, что соседний гейт способен
// упасть и способен смолчать.
//
// Инъекция подаётся НАСТОЯЩИМ входом — текстом страницы той же формы, что в
// корпусе, — и правит РОВНО ОДИН факт против положительного близнеца: имя
// значения перечисления. Всё остальное в паре побайтово одинаково, поэтому
// красное не могло прийти от соседа.
//
// Прогонов по каждой оси ТРИ, а не два: близнец (молчание), инъекция (красное),
// и — там, где есть существующий контроль, — прогон, показывающий, что инъекция
// не роняет ЕГО. Без третьего молчание близнеца неотличимо от молчания мёртвой
// проверки.

// docsInjectionPage — страница той же формы, что корпус: операция, объявленная
// разметкой, и пример ответа внутри неё.
//
// `%s` — единственное подставляемое место. Оно и есть тот один факт, которым
// инъекция отличается от близнеца.
const docsInjectionPage = `# Роль

<ApiOperation method="GET" endpoint="/iam/v1/roles/{role_id}">

#### Пример ответа

` + "```json" + `
{
  "id": "rol-1",
  "ruleStates": [
    { "ruleIndex": 0, "state": "%s" }
  ]
}
` + "```" + `

</ApiOperation>
`

// docsInjectionCodeBlockPage — та же страница ВТОРОЙ законной записью примера.
//
// Она здесь не для полноты: распознаватель, знающий одну запись, объявлял бы
// вторую не «редкой», а НЕВИДИМОЙ, и его молчание на ней читалось бы как
// чистота. Ровно это и было — 110 примеров из 178 считались неразбираемыми.
const docsInjectionCodeBlockPage = `# Роль

<ApiOperation method="GET" endpoint="/iam/v1/roles/{role_id}">

<CodeBlock language="json">
  {dedent` + "`" + `
    {
      "id": "rol-1",
      "ruleStates": [
        { "ruleIndex": 0, "state": "%s" }
      ]
    }
  ` + "`" + `}
</CodeBlock>

</ApiOperation>
`

// judgeInjectedPage прогоняет текст страницы ТЕМ ЖЕ путём, каким его проходит
// обход дерева: разбор примеров → биндинг → суждение.
func judgeInjectedPage(t *testing.T, text string) (findings []docsEnumFinding, walked int) {
	t.Helper()
	for _, ex := range extractDocsJSONExamples("страница-инъекции.mdx", text) {
		md, fqn, ok := docsResolveResponseMessage(ex.Method, ex.Endpoint)
		if !ok {
			continue
		}
		got, walkedOne := docsExampleEnumFindings(md, fqn, ex)
		if !walkedOne {
			continue
		}
		walked++
		findings = append(findings, got...)
	}
	return findings, walked
}

func TestDocsEnumGateCanFailAndCanStaySilent(t *testing.T) {
	for _, form := range []struct {
		name string
		page string
	}{
		{"забором ```json", docsInjectionPage},
		{"разметкой CodeBlock", docsInjectionCodeBlockPage},
	} {
		t.Run(form.name, func(t *testing.T) {
			t.Run("законный близнец — объявленное имя — МОЛЧАНИЕ", func(t *testing.T) {
				findings, walked := judgeInjectedPage(t, strings.Replace(form.page, "%s", "RULE_LIFECYCLE_ACTIVE", 1))
				if walked != 1 {
					t.Fatalf("пример не обойдён (обойдено %d): близнец не доказывает молчания, "+
						"если гейт до него не дошёл", walked)
				}
				if len(findings) != 0 {
					t.Fatalf("гейт краснеет на ЗАКОННОМ имени — он ловит форму, а не существо: %v", findings)
				}
			})

			t.Run("инъекция — сокращённое имя — КРАСНОЕ с координатой", func(t *testing.T) {
				findings, walked := judgeInjectedPage(t, strings.Replace(form.page, "%s", "ACTIVE", 1))
				if walked != 1 {
					t.Fatalf("пример не обойдён (обойдено %d): молчание гейта означало бы "+
						"не чистоту, а непрочтение", walked)
				}
				if len(findings) != 1 {
					t.Fatalf("инъекция не поймана: находок %d, ожидалась 1", len(findings))
				}
				f := findings[0]
				// Находка обязана называть ПОЛЕ и ЗНАЧЕНИЕ: находка, называющая
				// симптом, посылает читателя искать не там.
				if f.Field != "ruleStates[0].state" || f.Value != "ACTIVE" {
					t.Fatalf("находка не называет виновника: поле %q, значение %q", f.Field, f.Value)
				}
				if !strings.Contains(f.String(), "RULE_LIFECYCLE_ACTIVE") {
					t.Fatalf("отказ не говорит, ЧТО принимается вместо: %s", f)
				}
				if !strings.Contains(f.String(), "страница-инъекции.mdx") {
					t.Fatalf("отказ не называет координату: %s", f)
				}
			})

			t.Run("имя ЧУЖОГО словаря — тоже находка", func(t *testing.T) {
				// `ACTIVE` объявлено голым у нескольких перечислений дерева
				// (`interactive_client`, `membership`, `user`, nlb, storage, vpc).
				// Распознаватель, спрашивающий «объявлено ли такое имя
				// где-нибудь», промолчал бы здесь — то есть ровно на предмете.
				findings, _ := judgeInjectedPage(t, strings.Replace(form.page, "%s", "ACTIVE", 1))
				if len(findings) != 1 {
					t.Fatalf("сверка идёт не по полю, а по дереву: находок %d", len(findings))
				}
			})
		})
	}

	t.Run("пример ВНЕ операции не выдаётся за обойдённый", func(t *testing.T) {
		page := strings.ReplaceAll(docsInjectionPage, "<ApiOperation method=\"GET\" endpoint=\"/iam/v1/roles/{role_id}\">", "")
		findings, walked := judgeInjectedPage(t, strings.Replace(page, "%s", "ACTIVE", 1))
		if walked != 0 || len(findings) != 0 {
			t.Fatalf("пример без операции обойдён (обойдено %d, находок %d): "+
				"сообщения ответа у него нет, и сверять было не с чем", walked, len(findings))
		}
	})
}

// TestDocsEnumProducerHalfCanFail — вторая половина предиката тоже обязана
// уметь краснеть.
//
// Инъекция здесь — маршаллер, собранный с `UseEnumNumbers`: он отдаёт номер
// вместо имени, то есть ровно то состояние сборки, при котором все имена в
// примерах страниц становятся ложью, а первая половина гейта остаётся зелёной.
//
// Один факт против боевого маршаллера правится ровно один: `EmitUnpopulated`
// и `UseProtoNames` берутся у него же.
func TestDocsEnumProducerHalfCanFail(t *testing.T) {
	live := newPublicJSONPb()

	t.Run("боевой маршаллер — МОЛЧАНИЕ", func(t *testing.T) {
		probes, marshaled, mismatched, _ := edgeEnumNameMismatches(live)
		if probes == 0 || marshaled == 0 {
			t.Fatalf("прогон беспредметен: перечислений %d, прогнано %d", probes, marshaled)
		}
		if mismatched != 0 {
			t.Fatalf("боевой маршаллер объявлен расходящимся: %d", mismatched)
		}
	})

	t.Run("инъекция UseEnumNumbers — КРАСНОЕ", func(t *testing.T) {
		injected := &runtime.JSONPb{
			MarshalOptions: protojson.MarshalOptions{
				UseProtoNames:   live.MarshalOptions.UseProtoNames,
				EmitUnpopulated: live.MarshalOptions.EmitUnpopulated,
				UseEnumNumbers:  true,
			},
			UnmarshalOptions: live.UnmarshalOptions,
		}
		_, marshaled, mismatched, report := edgeEnumNameMismatches(injected)
		if marshaled == 0 {
			t.Fatal("инъекция не прогналась: красное не пришло бы и от настоящего дефекта")
		}
		if mismatched == 0 {
			t.Fatal("маршаллер отдаёт НОМЕРА, а половина гейта молчит: она не сторожит форму значения")
		}
		if !strings.Contains(report, "контракт объявляет") {
			t.Fatalf("отказ не называет, чего ждали: %s", report)
		}
	})
}

// TestDocsEnumGateReadsEveryLegalExampleForm — перепись форм записи примера
// выводится ИЗ ДЕРЕВА, а не выписывается.
//
// Предмет: распознаватель обязан знать все законные записи примера. Число форм
// в корпусе меняется молча, поэтому проверяется не «форм две», а «каждая
// встреченная в корпусе запись примера JSON распознаётся».
func TestDocsEnumGateReadsEveryLegalExampleForm(t *testing.T) {
	root := repoRoot(t)
	pages := docsContentPages(t, root)
	if len(pages) == 0 {
		t.Fatal("страниц не найдено: обход пуст, и вердикт беспредметен")
	}

	// Обе записи считаются НЕЗАВИСИМО от распознавателя — прямым счётом
	// открывающих строк. Считай их сам распознаватель, проверка сверяла бы его
	// с самим собой.
	//
	// Счёт ведётся по ОТКРЫВАЮЩЕЙ СТРОКЕ, а не по её точному тексту: у блока
	// бывают атрибуты (`title` объявляет полосу примера, см.
	// `docs_example_keys_test.go`), и счёт по литералу `<CodeBlock
	// language="json">` объявил бы такой блок необъявленным. Тогда проверка
	// краснела бы на РАСШИРЕНИИ распознавателя — то есть ровно там, где
	// слепая зона закрывается.
	declaredFence, declaredBlock := 0, 0
	seenFence, seenBlock := 0, 0
	for _, page := range pages {
		body, err := readFileForTest(t, page)
		if err != nil {
			t.Fatalf("чтение %s: %v", page, err)
		}
		for _, line := range strings.Split(body, "\n") {
			trimmed := strings.TrimSpace(line)
			if rest, ok := strings.CutPrefix(trimmed, "```json"); ok &&
				(rest == "" || strings.HasPrefix(rest, " ") || strings.HasPrefix(rest, "\t")) {
				declaredFence++
			}
			if strings.Contains(line, "<CodeBlock") && strings.Contains(line, `language="json"`) {
				declaredBlock++
			}
		}
		for _, ex := range extractDocsJSONExamples(page, body) {
			switch ex.Form {
			case "fence":
				seenFence++
			case "codeblock":
				seenBlock++
			}
		}
	}
	t.Logf("объявлено примеров: забором %d, разметкой %d; распознано: забором %d, разметкой %d",
		declaredFence, declaredBlock, seenFence, seenBlock)

	if declaredFence == 0 || declaredBlock == 0 {
		t.Fatalf("в корпусе представлена не всякая форма (забором %d, разметкой %d): "+
			"проверка охвата стала бы беспредметной на второй", declaredFence, declaredBlock)
	}
	if seenFence != declaredFence || seenBlock != declaredBlock {
		t.Fatalf("распознаватель видит не все примеры: забором %d из %d, разметкой %d из %d — "+
			"невидимый пример не краснеет и не зеленеет, он молчит",
			seenFence, declaredFence, seenBlock, declaredBlock)
	}
}

// enumDescriptorsAreReachable — контроль предпосылки producer-половины: обход
// реестра и в самом деле доходит до перечислений домена.
func TestDocsEnumProducerWalkSeesKachoEnums(t *testing.T) {
	found := 0
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if !strings.HasPrefix(string(fd.Package()), "kacho.") {
			return true
		}
		found += fd.Enums().Len()
		return true
	})
	if found == 0 {
		t.Fatal("в слинкованном реестре нет перечислений kacho.*: producer-половина сторожила бы пустоту")
	}
	t.Logf("перечислений верхнего уровня в контрактах kacho.*: %d", found)
}

// readFileForTest — чтение страницы; вынесено, чтобы проба охвата не заводила
// собственного обхода дерева рядом с гейтовым.
func readFileForTest(t *testing.T, path string) (string, error) {
	t.Helper()
	b, err := osReadFile(path)
	return string(b), err
}
