// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// kaname_listener_knobs_test.go — у КАЖДОГО HTTP-слушателя службы доступа своя
// ручка транспорта, и боевой профиль объявляет их ВСЕ явно.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ — не «забыли ручку», а ДВА ПРАВИЛА ОБ ОДНОМ ПРЕДМЕТЕ
//
// Транспорт слушателя объявляет профиль, а поднимает его процесс, и страж старта
// процесса требует TLS на каждом поднятом HTTP-ребре в боевой посадке. Пока одна
// ручка чарта накрывает ДВА слушателя, эти два требования становятся
// невыполнимыми одновременно: у слушателей разные вызывающие, и то, что одному
// обязательно, другому невозможно. Профиль тогда не может быть верным ни в одном
// положении ручки — включит, и падает полоса, чей вызывающий сертификата не
// носит; выключит, и процесс отказывается стартовать.
//
// Наблюдалось: слушатель скрейпа и слушатель обратных вызовов службы личности
// сидели под общей ручкой, а оба собственных REST-фронта — под второй общей.
// Профиль боевой посадки стенда не объявлял ни ту, ни другую, и служба не
// поднималась вовсе: страж называл ЧЕТЫРЕ ребра сразу.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ИМЕННО СУДИТСЯ, И ПОЧЕМУ ОБЪЯВЛЕНИЯ, А НЕ РЕНДЕР
//
// Контракт — то, что профиль ОБЪЯВЛЯЕТ. Рендер умбреллы требует скачанных
// зависимостей (helm dep build ходит в сеть), поэтому проверка по рендеру в
// обычном прогоне ПРОПУСКАЕТСЯ, — а пропускающаяся проверка на измерении
// «ключа нет вовсе» бесполезна by construction: именно отсутствие ключа она и
// ловит. Тот же довод — у соседних dbtls_declaration_test.go и
// peer_transport_profiles_test.go.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ПЕРЕЧЕНЬ ПОВЕРХНОСТЕЙ ВЫВОДИТСЯ, А НЕ ВЫПИСАН
//
// Выписанный перечень разошёлся бы при заведении следующего слушателя МОЛЧА:
// новая поверхность просто не попала бы под проверку. Поэтому и ручки, и
// поверхности выводятся ОБХОДОМ шаблона: ручка — условие вида
// `dig "<ручка>" .Values.mtls.httpListeners .Values.mtls`, поверхность — имя
// переменной `KANAME_<ПОВЕРХНОСТЬ>_SERVER_MTLS_ENABLE` внутри этого условия.
// Ни одно имя ручки и ни одно имя поверхности здесь не выписано; не выписано и
// имя подчарта — он находится по тому, что объявляет общую ручку.
//
// ─────────────────────────────────────────────────────────────────────────────
// ГРАНИЦА ПРЕДМЕТА (названа, чтобы «зелено» не читалось шире, чем есть)
//
//   - Судится ОБЪЯВЛЕНИЕ, а не поднятый под: подъём стенда — третья категория
//     («не выполнилось»), и этой проверке он недоступен.
//   - Судятся стенды БОЕВОЙ посадки: у стенда разработки страж транспорта —
//     no-op by construction, и требовать от него объявлений значило бы требовать
//     того, чего никто не проверит.
//   - Судится наличие и непротиворечивость ОБЪЯВЛЕНИЙ, а не то, верно ли решение
//     оставить поверхность открытой: у объявленного исключения основание
//     прозаическое, и машинного предиката у него нет. Проверяется, что оно
//     ОБЪЯВЛЕНО — то есть что открытый текст стал решением, а не умолчанием.
package deploy_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// Разбор ШАБЛОНА: ручки и поверхности, которые они накрывают.

// listenerKnobGate — условие шаблона, включающее транспорт слушателей.
// `dig` берёт полистенное переопределение, а при его отсутствии — общую ручку.
var listenerKnobGate = regexp.MustCompile(
	`\{\{-?\s*if\s*\(\s*dig\s+"([A-Za-z0-9_]+)"\s+\.Values\.mtls\.([A-Za-z0-9_]+)\s+\.Values\.mtls\s*\)\s*-?\}\}`)

// listenerSurfaceEnv — переменная, которой поднимается транспорт ОДНОГО
// слушателя. Ключевое — суффикс `_SERVER_MTLS_ENABLE`: удостоверение, которым
// фронт представляется собственному слушателю (`_UPSTREAM_MTLS_ENABLE`), под него
// не подпадает и поверхностью не считается — это исходящий вызов, а не слушатель.
var listenerSurfaceEnv = regexp.MustCompile(`KANAME_([A-Z0-9]+)_SERVER_MTLS_ENABLE`)

// templateBlockOpen / templateBlockClose — действия, меняющие глубину вложенности.
var (
	templateBlockOpen  = regexp.MustCompile(`\{\{-?\s*(if|range|with|define|block)\b`)
	templateBlockClose = regexp.MustCompile(`\{\{-?\s*end\s*-?\}\}`)
)

// knobFacts — что шаблон говорит про одну ручку.
type knobFacts struct {
	knob     string   // имя полистенной ручки
	fallback string   // общая ручка, из которой берётся умолчание
	surfaces []string // поверхности, которые эта ручка накрывает (может быть пусто)
	blocks   int      // сколько условий её читают
}

// scanListenerKnobs выводит ручки и накрытые ими поверхности обходом шаблона.
//
// Глубина считается ПО ДЕЙСТВИЯМ, а не по отступам: отступ в шаблоне ничего не
// значит, и предикат по нему судил бы форматирование, а не вложенность.
func scanListenerKnobs(template string) []knobFacts {
	lines := strings.Split(template, "\n")
	byKnob := map[string]*knobFacts{}
	order := []string{}
	for i, line := range lines {
		m := listenerKnobGate.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		knob, fallback := m[1], m[2]
		f, ok := byKnob[knob]
		if !ok {
			f = &knobFacts{knob: knob, fallback: fallback}
			byKnob[knob] = f
			order = append(order, knob)
		}
		f.blocks++
		// Тело условия — до `end`, закрывающего ИМЕННО ЕГО.
		depth := 1
		seen := map[string]bool{}
		for j := i + 1; j < len(lines) && depth > 0; j++ {
			body := lines[j]
			depth += len(templateBlockOpen.FindAllString(body, -1))
			depth -= len(templateBlockClose.FindAllString(body, -1))
			if depth <= 0 {
				break
			}
			for _, s := range listenerSurfaceEnv.FindAllStringSubmatch(body, -1) {
				seen[s[1]] = true
			}
		}
		for s := range seen {
			if !contains(f.surfaces, s) {
				f.surfaces = append(f.surfaces, s)
			}
		}
	}
	out := make([]knobFacts, 0, len(order))
	for _, k := range order {
		f := byKnob[k]
		sort.Strings(f.surfaces)
		out = append(out, *f)
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// Факты одного стенда.

// stackListenerFacts — то, что проверке нужно знать про один стенд.
type stackListenerFacts struct {
	stack string
	// production — посадка стенда боевая: страж транспорта на ней действует.
	production bool
	// declaredKnob — ручка → объявленное профилями стенда значение.
	declaredKnob map[string]any
	// declaredException — ручка → объявленное профилями стенда исключение.
	declaredException map[string]any
}

// ─────────────────────────────────────────────────────────────────────────────
// ЯДРО — чистая функция над фактами, чтобы самопроверка подавала ей
// синтетический вход, а не подделывала дерево.

type listenerFinding struct {
	stack  string // пусто у находок уровня дерева
	knob   string
	kind   string
	detail string
}

const (
	kindKnobCoversManySurfaces = "одна ручка накрывает несколько слушателей"
	kindSurfaceUnderManyKnobs  = "один слушатель накрыт несколькими ручками"
	kindKnobNotDeclared        = "ручка не объявлена профилем стенда"
	kindPlaintextNotDeclared   = "открытый текст без объявленного исключения"
	kindExceptionContradicts   = "исключение противоречит объявленному транспорту"
)

// judgeListenerKnobs — весь вердикт. Разделено на ручки дерева и ручки стендов:
// первое — свойство чарта, второе — свойство профиля.
func judgeListenerKnobs(knobs []knobFacts, stacks []stackListenerFacts) []listenerFinding {
	var out []listenerFinding

	// (1) Ручка, накрывающая больше одного слушателя. Именно она делает боевой
	// профиль невыразимым: требования у слушателей разные, а ручка одна.
	surfaceOwners := map[string][]string{}
	for _, k := range knobs {
		if len(k.surfaces) > 1 {
			out = append(out, listenerFinding{
				knob: k.knob, kind: kindKnobCoversManySurfaces,
				detail: fmt.Sprintf("слушатели %s — у каждого свой вызывающий, и одним значением их требования не выполнить",
					strings.Join(k.surfaces, ", ")),
			})
		}
		for _, s := range k.surfaces {
			surfaceOwners[s] = append(surfaceOwners[s], k.knob)
		}
	}
	// (2) Зеркало: слушатель, накрытый двумя ручками, — тот же класс с другой
	// стороны, и он тише: две ручки об одном предмете расходятся молча.
	surfaces := make([]string, 0, len(surfaceOwners))
	for s := range surfaceOwners {
		surfaces = append(surfaces, s)
	}
	sort.Strings(surfaces)
	for _, s := range surfaces {
		if owners := surfaceOwners[s]; len(owners) > 1 {
			sort.Strings(owners)
			out = append(out, listenerFinding{
				knob: strings.Join(owners, "+"), kind: kindSurfaceUnderManyKnobs,
				detail: fmt.Sprintf("слушатель %s читает %d ручек", s, len(owners)),
			})
		}
	}

	// (3) Профиль боевого стенда объявляет КАЖДУЮ ручку явно.
	for _, st := range stacks {
		if !st.production {
			continue
		}
		for _, k := range knobs {
			v, declared := st.declaredKnob[k.knob]
			exc, hasExc := st.declaredException[k.knob]
			if !declared {
				out = append(out, listenerFinding{
					stack: st.stack, knob: k.knob, kind: kindKnobNotDeclared,
					detail: fmt.Sprintf("умолчание приходит из общей ручки %q — величина, "+
						"которую построение подставляет само, предметом стража быть не может", k.fallback),
				})
				continue
			}
			if v == true {
				if hasExc && exc == true {
					out = append(out, listenerFinding{
						stack: st.stack, knob: k.knob, kind: kindExceptionContradicts,
						detail: "транспорт объявлен И объявлено исключение из него: два правила об одном предмете",
					})
				}
				continue
			}
			if !hasExc || exc != true {
				out = append(out, listenerFinding{
					stack: st.stack, knob: k.knob, kind: kindPlaintextNotDeclared,
					detail: "слушатель работает открытым текстом, и это не объявлено исключением — " +
						"страж старта откажется пускать процесс",
				})
			}
		}
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// Сбор фактов из дерева.

// kanameChartDir — подчарт, объявляющий общую ручку транспорта HTTP-слушателей.
// Находится ПО ТОМУ, ЧТО ОБЪЯВЛЯЕТ: имя чарта здесь не выписано, поэтому
// переименование подчарта проверку не ослепит.
func kanameChartDir(t *testing.T, knobs []knobFacts) (dir, key string) {
	t.Helper()
	if len(knobs) == 0 {
		t.Fatalf("ручек транспорта не найдено — предмет искать не в чем")
	}
	entries, err := filepath.Glob(filepath.Join(umbrellaDir, "charts", "*", "values.yaml"))
	if err != nil {
		t.Fatalf("обход подчартов: %v", err)
	}
	var hits []string
	for _, vf := range entries {
		if _, ok := lookup(readYAML(t, vf), "mtls", knobs[0].fallback); ok {
			hits = append(hits, filepath.Dir(vf))
		}
	}
	if len(hits) != 1 {
		t.Fatalf("подчартов, объявляющих общую ручку %q, найдено %d (ожидался один): %v",
			knobs[0].fallback, len(hits), hits)
	}
	return hits[0], filepath.Base(hits[0])
}

// listenerTemplate — шаблон рабочей нагрузки подчарта. Читается по имени файла,
// объявляющего контейнер: там и живут переменные транспорта.
func listenerTemplate(t *testing.T, chartDir string) (string, string) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(chartDir, "templates", "*.yaml"))
	if err != nil {
		t.Fatalf("обход шаблонов %s: %v", chartDir, err)
	}
	var hit, body string
	for _, f := range files {
		raw, err := os.ReadFile(f) // #nosec G304 -- путь выведен обходом дерева репозитория
		if err != nil {
			t.Fatalf("чтение %s: %v", f, err)
		}
		if listenerKnobGate.MatchString(string(raw)) {
			if hit != "" {
				t.Fatalf("условия транспорта живут в ДВУХ шаблонах (%s и %s) — "+
					"два места об одном предмете разойдутся молча", hit, f)
			}
			hit, body = f, string(raw)
		}
	}
	if hit == "" {
		t.Fatalf("в шаблонах %s нет ни одного условия транспорта слушателей — "+
			"предикат перестал их узнавать, а не дерево стало чистым", chartDir)
	}
	return hit, body
}

// productionPosture — посадка стенда боевая. Читается ОБЪЯВЛЕНИЕ подчарта плюс
// его умолчание: посадка, не объявленная стендом, приходит из значений чарта, и
// судить её надо тем же значением, каким её увидит процесс.
func productionPosture(chartDefaults, declared map[string]any, key string) bool {
	mode, ok := lookup(declared, key, "authMode")
	if !ok {
		mode, ok = lookup(chartDefaults, "authMode")
	}
	if !ok {
		return false
	}
	s, _ := mode.(string)
	return strings.HasPrefix(s, "production")
}

func TestEveryListenerHasItsOwnTransportKnob(t *testing.T) {
	knobsProbe := scanListenerKnobsFromTree(t)
	knobs, chartDir, chartKey, templatePath := knobsProbe.knobs, knobsProbe.dir, knobsProbe.key, knobsProbe.template

	chartDefaults := readYAML(t, filepath.Join(chartDir, "values.yaml"))
	stacksTbl := deployStacks(t)

	names := make([]string, 0, len(stacksTbl))
	for n := range stacksTbl {
		names = append(names, n)
	}
	sort.Strings(names)

	var facts []stackListenerFacts
	productionStacks := 0
	for _, name := range names {
		declared := map[string]any{}
		for _, p := range stacksTbl[name] {
			declared = mergeValues(declared, readYAML(t, filepath.Join(umbrellaDir, p)))
		}
		f := stackListenerFacts{
			stack:             name,
			production:        productionPosture(chartDefaults, declared, chartKey),
			declaredKnob:      map[string]any{},
			declaredException: map[string]any{},
		}
		for _, k := range knobs {
			if v, ok := lookup(declared, chartKey, "mtls", k.knob); ok {
				f.declaredKnob[k.knob] = v
			}
			if v, ok := lookup(declared, chartKey, "mtls", "plaintextExceptions", k.knob); ok {
				f.declaredException[k.knob] = v
			}
		}
		if f.production {
			productionStacks++
		}
		facts = append(facts, f)
	}

	// ПЕРЕПИСЬ — «ноль находок» обязано быть отличимо от «ноль прочитанного».
	surfaceTotal := 0
	for _, k := range knobs {
		surfaceTotal += len(k.surfaces)
	}
	t.Logf("осмотрено: шаблон %s · ручек транспорта %d · накрытых слушателей %d · "+
		"стендов в таблице %d, из них боевой посадки %d",
		templatePath, len(knobs), surfaceTotal, len(facts), productionStacks)
	for _, k := range knobs {
		t.Logf("   ручка %-16s условий %d · слушатели [%s] · умолчание из %q",
			k.knob, k.blocks, strings.Join(k.surfaces, " "), k.fallback)
	}

	if surfaceTotal == 0 {
		t.Fatalf("ни одна ручка не накрывает ни одного слушателя — вердикт беспредметен: "+
			"распознаватель переменных транспорта (%s) перестал их узнавать", listenerSurfaceEnv)
	}
	if productionStacks == 0 {
		t.Fatalf("ни один стенд не объявлен боевой посадкой — вердикт беспредметен: " +
			"проверка не вправе считать, что боевых стендов не осталось")
	}

	findings := judgeListenerKnobs(knobs, facts)
	if len(findings) == 0 {
		return
	}
	for _, f := range findings {
		where := f.stack
		if where == "" {
			where = "дерево"
		}
		t.Errorf("%s · ручка %q: %s — %s", where, f.knob, f.kind, f.detail)
	}
}

// treeKnobs — результат обхода дерева, собранный один раз.
type treeKnobs struct {
	knobs    []knobFacts
	dir      string
	key      string
	template string
}

func scanListenerKnobsFromTree(t *testing.T) treeKnobs {
	t.Helper()
	// Шаблон ищется во ВСЕХ подчартах: имя подчарта заранее неизвестно.
	dirs, err := filepath.Glob(filepath.Join(umbrellaDir, "charts", "*"))
	if err != nil {
		t.Fatalf("обход подчартов: %v", err)
	}
	var (
		knobs    []knobFacts
		template string
		path     string
		found    int
	)
	for _, d := range dirs {
		files, _ := filepath.Glob(filepath.Join(d, "templates", "*.yaml"))
		for _, f := range files {
			raw, err := os.ReadFile(f) // #nosec G304 -- путь выведен обходом дерева репозитория
			if err != nil {
				continue
			}
			if !listenerKnobGate.MatchString(string(raw)) {
				continue
			}
			found++
			template, path = string(raw), f
		}
	}
	if found == 0 {
		t.Fatalf("в дереве нет ни одного условия транспорта слушателей — предикат %s "+
			"перестал их узнавать, а не дерево стало чистым", listenerKnobGate)
	}
	if found > 1 {
		t.Fatalf("условия транспорта живут в %d шаблонах — два места об одном предмете "+
			"разойдутся молча", found)
	}
	knobs = scanListenerKnobs(template)
	dir := filepath.Dir(filepath.Dir(path))
	_ = template
	chartDir, key := kanameChartDir(t, knobs)
	if chartDir != dir {
		t.Fatalf("шаблон транспорта лежит в %s, а общую ручку объявляет %s — "+
			"чарт и его шаблон разошлись", dir, chartDir)
	}
	return treeKnobs{knobs: knobs, dir: chartDir, key: key, template: path}
}
