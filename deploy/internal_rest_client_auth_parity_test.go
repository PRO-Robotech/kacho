// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// internal_rest_client_auth_parity_test.go — режим проверки клиента, которого
// СТРАЖ СТАРТА требует, боевой профиль обязан ОБЪЯВЛЯТЬ.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ — два правила об одном предмете, и разойтись им было нечем
//
// Ребро, чей страж требует взаимного режима, поднимает профиль, а требование
// исполняет процесс. Пока эти два объявления никто не сверяет, расхождение
// узнаётся ПОДЪЁМОМ СТЕНДА — то есть на MR релизной линии, четвертью часа позже
// и дороже. А профиль, объявивший режим НЕИСПОЛНИМЫЙ (опечатка, запрашивающий
// режим вместо взаимного), не узнаётся и там, пока страж не откажет.
//
// Соседний гейт (kaname_listener_knobs_test.go) этого не закрывает и закрыть не
// может: его признак — суффикс `_SERVER_MTLS_ENABLE`, то есть ручка ВКЛЮЧЕНИЯ
// транспорта. Ручка РЕЖИМА проверки клиента его распознавателю не отвечает и
// потому вне осмотра — форма, о которой распознаватель не знает, не даёт ни
// красного, ни зелёного, она молчит (testing.md §«Гейт на класс», п. 7).
//
// ─────────────────────────────────────────────────────────────────────────────
// ДВЕ СТОРОНЫ БЕРУТСЯ ИЗ СВОИХ ИСТОЧНИКОВ — ИНАЧЕ ЭТО ТАВТОЛОГИЯ
//
// Сторона СТРАЖА читается из кода службы: какой режим он называет требуемым и
// какой переменной процесса эта величина приходит. Сторона ПРОФИЛЯ читается из
// объявлений посадки. Посчитай обе одним выражением — и проверка выродится в
// сверку значения с самим собой: она пройдёт при любом значении.
//
// НИ ОДНА КООРДИНАТА ЗДЕСЬ НЕ ВЫПИСАНА, и это не эстетика. Выписанное имя ручки,
// имя переменной или имя режима — второе место об одном предмете; оно разошлось
// бы с деревом молча, и разошлось бы ровно там, где расхождение не видно.
// Выводятся ВСЕ пять:
//
//	что                        откуда
//	──────────────────────────────────────────────────────────────────────────
//	требуемое значение режима  функция службы, отдающая его стражу (константа
//	                           объявления, а не литерал у стража)
//	переменная процесса        тег объявления конфигурации того же поля
//	ручка транспорта ребра     условие шаблона, накрывающее эту поверхность
//	ключ профиля с режимом     выражение шаблона, которым переменная заполняется
//	перечень стендов           таблица стеков (deployStacks)
//
// Ребро тоже не выписано: гейт находит ВСЕ функции композиционного корня,
// спрашивающие у конфигурации имя взаимного режима. Заведут второй такой страж —
// он попадёт под проверку сам, а не будет ждать, пока о нём вспомнят.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ГЕЙТ ЖИВЁТ В КОРНЕВОМ МОДУЛЕ, А НЕ В МОДУЛЕ СЛУЖБЫ
//
// Служба отделяема от монорепо и собирается своим модулем. Проба, читающая
// зонтичное дерево из её модуля, привязала бы её к нему — то есть сделала бы
// отделимость ложной. Здесь же наоборот: корневой модуль читает исходники службы
// РАЗБОРОМ (go/parser), а не импортом, потому что импортировать их нельзя
// дважды — они и во внутреннем каталоге, и в чужом модуле.
//
// ─────────────────────────────────────────────────────────────────────────────
// ГРАНИЦА ПРЕДМЕТА (названа, чтобы «зелено» не читалось шире, чем есть)
//
//   - Судится ОБЪЯВЛЕНИЕ, а не поднятый под: подъём стенда — третья категория
//     («не выполнилось»), и этой проверке он недоступен. Что процесс СДЕЛАЕТ с
//     объявленным режимом, проверяют пробы самого стража в модуле службы.
//   - Судятся стенды БОЕВОЙ посадки: в не-боевой страж — no-op by construction,
//     и требовать объявлений от стенда значило бы требовать того, чего никто не
//     проверит.
//   - Судятся стенды, где ребро ПОДНЯТО под транспортом: у опущенного ребра
//     режима проверки клиента не бывает — переменная не эмитируется вовсе.
package deploy_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Каталоги службы доступа. Координаты каталогов, а не файлов: файл, в котором
// живёт страж, — деталь раскладки, и перечислять его значило бы завести
// перечень, стареющий при первом же переименовании.
const (
	guardCmdDir    = "../services/iam/cmd/kaname"
	guardConfigDir = "../services/iam/internal/apps/kaname/config"
)

// modeFuncSuffix — хвост имени функции, которой страж спрашивает у конфигурации
// ТРЕБУЕМОЕ имя взаимного режима. Признак — форма имени, а не список имён.
const modeFuncSuffix = "MutualModeName"

// clientAuthFieldSuffix — хвост имени поля конфигурации, несущего режим проверки
// клиента того же ребра.
const clientAuthFieldSuffix = "ClientAuthMode"

// envconfigTag — тег объявления, которым поле связано с переменной процесса.
var envconfigTag = regexp.MustCompile(`envconfig:"([A-Z0-9_]+)"`)

// guardedEdge — ребро, чей страж старта требует взаимного режима, со всеми
// координатами, ВЫВЕДЕННЫМИ из дерева.
type guardedEdge struct {
	edge      string // «InternalREST» — общий префикс имён обеих сторон
	guardFile string // где найден зов
	required  string // требуемое значение режима (константа объявления)
	envSuffix string // хвост имени переменной процесса (из тега объявления)
	envName   string // полное имя переменной, найденное в шаблоне
	surface   string // поверхность в терминах шаблона («INTERNALREST»)
	knob      string // полистенная ручка транспорта ребра
	fallback  string // общая ручка, из которой берётся её умолчание
	valuesKey string // ключ профиля, которым заполняется режим
}

// stackModeFacts — что ОДИН стенд говорит про это ребро.
type stackModeFacts struct {
	stack      string
	production bool
	transport  bool   // ребро поднято под транспортом
	declared   bool   // режим объявлен профилем
	mode       string // объявленное значение
}

// parityFinding — находка. Стенд и ребро называются всегда: находка, не
// называющая, ГДЕ она, посылает читателя искать по всему дереву.
type parityFinding struct {
	stack  string
	edge   string
	kind   string
	detail string
}

// ─────────────────────────────────────────────────────────────────────────────
// СУЖДЕНИЕ. Чистая функция: вход строится вызывающим, поэтому доказательство
// инъекцией подаёт ей синтетику, не трогая общий клон.

func judgeClientAuthParity(edge guardedEdge, stacks []stackModeFacts) []parityFinding {
	var out []parityFinding
	for _, s := range stacks {
		// Не-боевая посадка: страж — no-op, судить нечего.
		// Ребро опущено: переменной режима не бывает вовсе.
		if !s.production || !s.transport {
			continue
		}
		if !s.declared {
			out = append(out, parityFinding{
				stack: s.stack, edge: edge.edge, kind: "режим не объявлен",
				detail: fmt.Sprintf(
					"ребро поднято под транспортом (ручка %q), а ключ %q профилем не объявлен — "+
						"процесс возьмёт умолчание чарта, и страж старта откажет, назвав %s; "+
						"требуется %q",
					edge.knob, edge.valuesKey, edge.envName, edge.required),
			})
			continue
		}
		if s.mode != edge.required {
			out = append(out, parityFinding{
				stack: s.stack, edge: edge.edge, kind: "режим не взаимный",
				detail: fmt.Sprintf(
					"ключ %q объявлен как %q, а страж старта требует %q (переменная %s): "+
						"на этом режиме рукопожатие не решает, кому позволено дотянуться до "+
						"поверхности, — служба откажется стартовать",
					edge.valuesKey, s.mode, edge.required, edge.envName),
			})
		}
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// СТОРОНА СТРАЖА. Читается РАЗБОРОМ исходника службы: судится узел объявления,
// а не строка текста — имя режима встречается и в прозе, и в сообщениях отказа,
// и сверка по подстроке нашла бы их (testing.md §«Гейт на класс», п. 4).

// guardedEdgesFromSource — рёбра, чей страж требует взаимного режима.
//
// Ищется ЗОВ функции с именем `<Ребро>MutualModeName`: страж, называющий
// оператору требуемое значение, обязан спросить его у конфигурации, а не писать
// литералом (так сказано в объявлении самой функции). Значит зов и есть признак
// «здесь требуют взаимного режима».
func guardedEdgesFromSource(t *testing.T) []guardedEdge {
	t.Helper()

	// 1. Кто спрашивает требуемое имя режима — и о каком ребре.
	type call struct{ edge, file string }
	var calls []call
	seen := map[string]bool{}
	fset := token.NewFileSet()
	files, err := filepath.Glob(filepath.Join(guardCmdDir, "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("композиционный корень службы (%s) не обходится (%v, файлов %d) — "+
			"предпосылка проверки исчезла, а не дерево стало чистым", guardCmdDir, err, len(files))
	}
	readGo := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatalf("разбор %s: %v", f, err)
		}
		readGo++
		ast.Inspect(file, func(n ast.Node) bool {
			ce, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := ce.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			name := sel.Sel.Name
			if !strings.HasSuffix(name, modeFuncSuffix) || name == modeFuncSuffix {
				return true
			}
			edge := strings.TrimSuffix(name, modeFuncSuffix)
			if seen[edge] {
				return true
			}
			seen[edge] = true
			calls = append(calls, call{edge: edge, file: f})
			return true
		})
	}
	if readGo == 0 {
		t.Fatalf("в %s не прочитано ни одного не-тестового файла — вердикт беспредметен", guardCmdDir)
	}
	if len(calls) == 0 {
		t.Fatalf("в %s нет ни одного зова `<Ребро>%s` (прочитано файлов %d) — распознаватель "+
			"перестал узнавать стража взаимного режима, а не дерево стало чистым",
			guardCmdDir, modeFuncSuffix, readGo)
	}

	// 2. Что эта функция отдаёт и какой переменной приходит то же поле.
	cfgFiles, err := filepath.Glob(filepath.Join(guardConfigDir, "*.go"))
	if err != nil || len(cfgFiles) == 0 {
		t.Fatalf("объявление конфигурации службы (%s) не обходится (%v, файлов %d)",
			guardConfigDir, err, len(cfgFiles))
	}
	consts := map[string]string{}  // имя константы → значение
	returns := map[string]string{} // имя функции   → имя возвращаемой константы
	envTags := map[string]string{} // имя поля      → тег envconfig
	readCfg := 0
	for _, f := range cfgFiles {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatalf("разбор %s: %v", f, err)
		}
		readCfg++
		ast.Inspect(file, func(n ast.Node) bool {
			switch d := n.(type) {
			case *ast.ValueSpec:
				for i, nm := range d.Names {
					if i >= len(d.Values) {
						continue
					}
					if lit, ok := d.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						if v, err := strconv.Unquote(lit.Value); err == nil {
							consts[nm.Name] = v
						}
					}
				}
			case *ast.FuncDecl:
				if d.Recv != nil || d.Body == nil || len(d.Body.List) != 1 {
					return true
				}
				ret, ok := d.Body.List[0].(*ast.ReturnStmt)
				if !ok || len(ret.Results) != 1 {
					return true
				}
				if id, ok := ret.Results[0].(*ast.Ident); ok {
					returns[d.Name.Name] = id.Name
				}
			case *ast.Field:
				if len(d.Names) != 1 || d.Tag == nil {
					return true
				}
				if m := envconfigTag.FindStringSubmatch(d.Tag.Value); m != nil {
					envTags[d.Names[0].Name] = m[1]
				}
			}
			return true
		})
	}
	if readCfg == 0 {
		t.Fatalf("в %s не прочитано ни одного не-тестового файла — вердикт беспредметен", guardConfigDir)
	}

	var out []guardedEdge
	for _, c := range calls {
		fn := c.edge + modeFuncSuffix
		constName, ok := returns[fn]
		if !ok {
			t.Fatalf("страж (%s) спрашивает %s, а объявления этой функции в %s нет "+
				"(прочитано файлов %d) — одна сторона пары исчезла", c.file, fn, guardConfigDir, readCfg)
		}
		required, ok := consts[constName]
		if !ok || required == "" {
			t.Fatalf("%s отдаёт константу %s, значение которой не разобрано — "+
				"требуемое значение режима неизвестно, судить нечем", fn, constName)
		}
		field := c.edge + clientAuthFieldSuffix
		tag, ok := envTags[field]
		if !ok {
			t.Fatalf("поля %s с тегом envconfig в %s нет — переменная процесса, которой "+
				"приходит режим, неизвестна; вторая координата пары не выводится", field, guardConfigDir)
		}
		out = append(out, guardedEdge{
			edge: c.edge, guardFile: c.file, required: required, envSuffix: tag,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].edge < out[j].edge })
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// СТОРОНА ШАБЛОНА. Ручка транспорта и ключ профиля выводятся ОБХОДОМ шаблона по
// уже провязанной переменной процесса — то есть по той же координате, что дала
// сторона стража. Второго перечня поверхностей здесь не заводится.

// clientAuthValueExpr — выражение, которым шаблон заполняет режим:
// `.Values.mtls.<ключ>`.
var clientAuthValueExpr = regexp.MustCompile(`\.Values\.mtls\.([A-Za-z0-9_]+)`)

// bindEdgeToTemplate дописывает ребру его шаблонные координаты.
//
// Возвращает ОШИБКУ, а не роняет прогон сама: связывание — то место, где
// распознаватель может перестать узнавать форму записи, и доказательство
// инъекцией обязано уметь подать ему вход, о котором он не знает. Функция,
// роняющая прогон изнутри, такому доказательству недоступна by construction.
func bindEdgeToTemplate(e *guardedEdge, knobs []knobFacts, template, templatePath string) error {
	// Поверхность — то, что стоит в имени переменной перед хвостом объявления.
	// Хвост берётся из тега (`INTERNALREST_SERVER_MTLS_CLIENTAUTHMODE`), поэтому
	// поверхность («INTERNALREST») выводится, а не выписывается.
	parts := strings.SplitN(e.envSuffix, "_", 2)
	if len(parts) != 2 {
		return fmt.Errorf("тег объявления %q не разбирается на поверхность и хвост — "+
			"распознаватель перестал узнавать форму имени", e.envSuffix)
	}
	e.surface = parts[0]

	// Полное имя переменной ищется в ШАБЛОНЕ: префикс продукта объявлен там, и
	// выписывать его здесь значило бы завести третье место об одном предмете.
	envRe := regexp.MustCompile(`\b([A-Z0-9]+_` + regexp.QuoteMeta(e.envSuffix) + `)\b`)
	hits := map[string]bool{}
	lines := strings.Split(template, "\n")
	valuesKey, atLine := "", -1
	for i, ln := range lines {
		m := envRe.FindStringSubmatch(ln)
		if m == nil {
			continue
		}
		hits[m[1]] = true
		if atLine < 0 {
			atLine = i
			e.envName = m[1]
		}
	}
	if len(hits) == 0 {
		return fmt.Errorf("в шаблоне %s нет переменной с хвостом %q, которым объявлена сторона "+
			"стража — пара не связывается, и вердикт был бы беспредметен", templatePath, e.envSuffix)
	}
	if len(hits) > 1 {
		names := make([]string, 0, len(hits))
		for n := range hits {
			names = append(names, n)
		}
		sort.Strings(names)
		return fmt.Errorf("хвост %q в шаблоне %s носят ДВЕ переменные (%s) — какая из них ребро %q, "+
			"предикат не решает, и молчать здесь значило бы выбрать наугад",
			e.envSuffix, templatePath, strings.Join(names, ", "), e.edge)
	}

	// Ключ профиля — из выражения, которым шаблон заполняет ЭТУ переменную.
	// Значение стоит на следующей строке после имени; ищем от неё вниз до
	// первого выражения, чтобы форма записи «имя / значение» не была выписана
	// жёстче, чем шаблон её пишет.
	for j := atLine; j < len(lines) && j <= atLine+2; j++ {
		if m := clientAuthValueExpr.FindStringSubmatch(lines[j]); m != nil {
			valuesKey = m[1]
			break
		}
	}
	if valuesKey == "" {
		return fmt.Errorf("переменная %s в %s заполняется не из `.Values.mtls.<ключ>` — "+
			"ключ профиля не выводится, и сверять объявление профиля не с чем",
			e.envName, templatePath)
	}
	e.valuesKey = valuesKey

	// Ручка транспорта — та, что накрывает эту поверхность. Перечень поверхностей
	// даёт уже провязанный обход соседнего гейта: второй распознаватель об одном
	// предмете разошёлся бы с первым молча.
	for _, k := range knobs {
		for _, s := range k.surfaces {
			if s == e.surface {
				e.knob, e.fallback = k.knob, k.fallback
				break
			}
		}
	}
	if e.knob == "" {
		return fmt.Errorf("поверхность %q, выведенная из объявления стража, не накрыта НИ ОДНОЙ "+
			"ручкой транспорта шаблона %s — либо ребро поднимается иначе, либо распознаватель "+
			"ручек перестал его узнавать", e.surface, templatePath)
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────

func TestProductionStacksDeclareTheMutualModeTheirGuardRequires(t *testing.T) {
	edges := guardedEdgesFromSource(t)

	probe := scanListenerKnobsFromTree(t)
	chartDefaults := readYAML(t, filepath.Join(probe.dir, "values.yaml"))
	body, err := os.ReadFile(probe.template) // #nosec G304 -- путь выведен обходом дерева
	if err != nil {
		t.Fatalf("шаблон %s не читается (%v) — предпосылка проверки исчезла", probe.template, err)
	}
	for i := range edges {
		if err := bindEdgeToTemplate(&edges[i], probe.knobs, string(body), probe.template); err != nil {
			t.Fatalf("ребро %s не связывается с шаблоном: %v", edges[i].edge, err)
		}
	}

	stacksTbl := deployStacks(t)
	names := make([]string, 0, len(stacksTbl))
	for n := range stacksTbl {
		names = append(names, n)
	}
	sort.Strings(names)

	// ПЕРЕПИСЬ ДВУМЯ ВЕЛИЧИНАМИ. Одно число («стендов N») скрывает ровно тот
	// случай, ради которого гейт заведён: профиль, переставший объявлять режим,
	// уменьшает ВТОРУЮ величину, не трогая первую.
	var (
		findings   []parityFinding
		production int
		underWire  = map[string]int{}
		declaring  = map[string]int{}
	)
	for _, e := range edges {
		underWire[e.edge], declaring[e.edge] = 0, 0
	}

	for _, name := range names {
		declared := map[string]any{}
		for _, p := range stacksTbl[name] {
			declared = mergeValues(declared, readYAML(t, filepath.Join(umbrellaDir, p)))
		}
		prod := productionPosture(chartDefaults, declared, probe.key)
		if prod {
			production++
		}
		for _, e := range edges {
			f := stackModeFacts{stack: name, production: prod}

			// ТРАНСПОРТ. Полистенная ручка, а при её отсутствии — общая: ровно
			// тем значением, каким его увидит процесс (`dig`).
			transport, ok := lookup(declared, probe.key, "mtls", e.knob)
			if !ok {
				transport, ok = lookup(declared, probe.key, "mtls", e.fallback)
			}
			if !ok {
				transport, ok = lookup(chartDefaults, "mtls", e.knob)
			}
			if !ok {
				transport, _ = lookup(chartDefaults, "mtls", e.fallback)
			}
			b, _ := transport.(bool)
			f.transport = b

			if v, ok := lookup(declared, probe.key, "mtls", e.valuesKey); ok {
				f.declared = true
				f.mode = fmt.Sprint(v)
			}
			if prod && f.transport {
				underWire[e.edge]++
				if f.declared && f.mode == e.required {
					declaring[e.edge]++
				}
			}
			findings = append(findings, judgeClientAuthParity(e, []stackModeFacts{f})...)
		}
	}

	t.Logf("осмотрено: шаблон %s · стендов в таблице %d, из них боевой посадки %d · "+
		"рёбер под стражем взаимного режима %d", probe.template, len(names), production, len(edges))
	for _, e := range edges {
		t.Logf("   ребро %-14s требуемый режим %-8q · переменная %s · ручка %q (умолчание из %q) · "+
			"ключ профиля %q · страж %s",
			e.edge, e.required, e.envName, e.knob, e.fallback, e.valuesKey, e.guardFile)
		t.Logf("   %-14s боевых стендов с поднятым ребром %d · объявляют требуемый режим %d",
			"", underWire[e.edge], declaring[e.edge])
	}

	// ПУСТОЙ ОБХОД — ОТКАЗ. «Ноль находок» обязано быть отличимо от «ноль
	// прочитанного»: перечень стендов, выродившийся в пустой, дал бы зелёное при
	// любом состоянии профилей.
	if production == 0 {
		t.Fatalf("ни один стенд не объявлен боевой посадкой — вердикт беспредметен: " +
			"проверка не вправе считать, что боевых стендов не осталось")
	}
	total := 0
	for _, e := range edges {
		total += underWire[e.edge]
	}
	if total == 0 {
		t.Fatalf("ни на одном боевом стенде ребро под стражем взаимного режима не поднято — "+
			"вердикт беспредметен: либо ручка транспорта перестала узнаваться, либо страж "+
			"стережёт то, чего ни один профиль не поднимает (рёбер %d)", len(edges))
	}

	for _, f := range findings {
		t.Errorf("%s · ребро %s: %s — %s", f.stack, f.edge, f.kind, f.detail)
	}
}
