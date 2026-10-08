// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// anonmail_fleet_test.go — пара «хранилище ↔ флот» для ОБОИХ потребителей
// объявления хранилища края: однократности `Idempotency-Key` и ограничителя
// анонимной почты (замысел issue-2917 З8 «Хранилище», CX2-11 (б), УК13; полоса E3).
//
// # Предмет
//
// Ограничитель анонимной почты держит счёт окон источника и подсети, ведро
// глобального темпа и пометку одноразовости доказательства работы. Вид его
// хранилища — то же объявление, что у однократности (`KACHO_IDEMPOTENCY_STORE`),
// второго объявления «где живёт состояние края» не заводится. При виде `memory`
// и флоте из N реплик каждая реплика считает своё: порог источника и подсети
// становится N-кратным, темп ведра — тоже, а доказательство, израсходованное на
// одной реплике, на соседней проходит ещё раз. Отказа нет, признака нет.
//
// Гейт `idempotency_fleet_test.go` судит пару по объявлениям, по профилю
// отдельно и для одного потребителя. Здесь пара судится:
//
//	(1) по потребителям — разбором исходника корня края: каждая функция, читающая
//	    вид хранилища, печатается; построитель хранилища ограничителя (функция с
//	    результатом `anonmail.Store`) обязан читать то же поле конфигурации, что
//	    однократность, а конфигурация не несёт ручки хранилища ограничителя;
//	(2) по РЕНДЕРУ чарта без зонтика и каждой цепочки `deploy/stacks.txt` — тем,
//	    что получает процесс: вид, размер флота, полоса личности (ограничитель
//	    строится только при `own`), активные потребители; размер флота равен
//	    выведенному из сложенных слоёв (`autoscaling.maxReplicas` либо `replicas`);
//	(3) инъекцией: профиль `memory` + флот 2 на цепочке, где ограничитель
//	    построен, — рендер отказывает; законные близнецы (флот 1; `postgres` с
//	    адресом при флоте 2) — рендер проходит, судья молчит.
//
// Перепись печатается всегда: «ноль находок» отличимо от «ноль прочитанного»,
// пустой обход — отказ.
package deploy_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/identityposture"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// edgeRootDir — исходник корня края, где строятся оба хранилища.
var edgeRootDir = filepath.Join("..", "cmd", "api-gateway")

const (
	storeKindField = "IdempotencyStoreKind"
	storeKindKnob  = "KACHO_IDEMPOTENCY_STORE"
	fleetSizeKnob  = "KACHO_GATEWAY_FLEET_SIZE"
)

// storeConsumer — потребитель объявления хранилища края: построитель хранилища,
// узнаваемый по типу результата, а не по имени функции.
type storeConsumer struct {
	name string // как печатается
	pkg  string // пакет типа результата
	typ  string // тип результата
}

// edgeStoreConsumers — оба потребителя (З8 «Хранилище»): однократность и ограничитель.
var edgeStoreConsumers = []storeConsumer{
	{"однократность Idempotency-Key", "middleware", "IdempotencyStore"},
	{"ограничитель анонимной почты", "anonmail", "Store"},
}

// storeConsumerCensus — что разбор исходника корня края увидел.
type storeConsumerCensus struct {
	files    int
	readers  []string            // функции, читающие вид хранилища: «<файл>:<функция>»
	builders map[string][]string // потребитель → его построители «<файл>:<функция>»
	findings []string
}

// returnsSelector — несёт ли список результатов функции тип <pkg>.<name>.
func returnsSelector(fn *ast.FuncDecl, pkg, name string) bool {
	if fn.Type.Results == nil {
		return false
	}
	for _, f := range fn.Type.Results.List {
		if sel, ok := f.Type.(*ast.SelectorExpr); ok && sel.Sel.Name == name {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == pkg {
				return true
			}
		}
	}
	return false
}

// readsField — читает ли тело функции поле с данным именем (селектор `x.<field>`).
// Судится узел-селектор, а не текст: слово в комментарии или строке не считается.
func readsField(fn *ast.FuncDecl, field string) bool {
	if fn.Body == nil {
		return false
	}
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == field {
			found = true
		}
		return !found
	})
	return found
}

// censusStoreConsumers разбирает исходники (имя файла → текст) корня края.
func censusStoreConsumers(sources map[string]string) storeConsumerCensus {
	c := storeConsumerCensus{builders: map[string][]string{}}
	reads := map[string]bool{}
	names := make([]string, 0, len(sources))
	for n := range sources {
		names = append(names, n)
	}
	sort.Strings(names)
	fset := token.NewFileSet()
	for _, name := range names {
		f, err := parser.ParseFile(fset, name, sources[name], parser.SkipObjectResolution)
		if err != nil {
			c.findings = append(c.findings, fmt.Sprintf("%s не разобран: %v — потребителей в нём не сосчитать", name, err))
			continue
		}
		c.files++
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok {
				continue
			}
			label := name + ":" + fn.Name.Name
			if readsField(fn, storeKindField) {
				c.readers = append(c.readers, label)
				reads[label] = true
			}
			for _, sc := range edgeStoreConsumers {
				if returnsSelector(fn, sc.pkg, sc.typ) {
					c.builders[sc.name] = append(c.builders[sc.name], label)
				}
			}
		}
	}
	if c.files == 0 {
		c.findings = append(c.findings, "исходников корня края не прочитано — обход пуст, и это отказ, а не пустой успех")
		return c
	}
	for _, sc := range edgeStoreConsumers {
		built := c.builders[sc.name]
		if len(built) == 0 {
			c.findings = append(c.findings, fmt.Sprintf("в корне края нет построителя хранилища потребителя «%s» "+
				"(функции с результатом %s.%s) — предпосылка гейта не держится: потребителя, чью пару судить, "+
				"не найдено", sc.name, sc.pkg, sc.typ))
		}
		for _, b := range built {
			if !reads[b] {
				c.findings = append(c.findings, fmt.Sprintf("%s строит хранилище потребителя «%s», не читая %s: "+
					"вид его хранилища объявлен ВТОРЫМ местом, и пару «вид ↔ флот», которую сверяют отказ старта "+
					"и отказ рендера по %s, он не проходит", b, sc.name, storeKindField, storeKindKnob))
			}
		}
	}
	return c
}

// edgeRootSources — неиспытательные исходники корня края.
func edgeRootSources(t *testing.T) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(edgeRootDir)
	if err != nil {
		t.Fatalf("каталог корня края %s не читается: %v", edgeRootDir, err)
	}
	out := map[string]string{}
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(edgeRootDir, n)) // #nosec G304 -- путь из дерева
		if err != nil {
			t.Fatalf("%s не читается: %v", n, err)
		}
		out[n] = string(raw)
	}
	return out
}

// limiterStoreKnobs — ручки конфигурации, объявляющие хранилище ограничителя
// вторым местом: тег окружения про анонимную почту с видом или адресом хранилища.
func limiterStoreKnobs(cfgType reflect.Type) (knobs []string, total int) {
	for i := 0; i < cfgType.NumField(); i++ {
		tag := cfgType.Field(i).Tag.Get("envconfig")
		if tag == "" {
			continue
		}
		total++
		if strings.Contains(tag, "ANON_MAIL") && (strings.HasSuffix(tag, "_STORE") || strings.HasSuffix(tag, "_DSN")) {
			knobs = append(knobs, tag)
		}
	}
	return knobs, total
}

// TestAnonMailFleet_BothConsumersReadTheOneStoreDeclaration — у объявления
// хранилища края два потребителя, и оба читают ОДНО поле конфигурации.
func TestAnonMailFleet_BothConsumersReadTheOneStoreDeclaration(t *testing.T) {
	c := censusStoreConsumers(edgeRootSources(t))
	t.Logf("перепись корня края: исходников %d; читают %s: %s", c.files, storeKindField, strings.Join(c.readers, ", "))
	for _, sc := range edgeStoreConsumers {
		t.Logf("  потребитель «%s» (результат %s.%s): построители %s",
			sc.name, sc.pkg, sc.typ, strings.Join(c.builders[sc.name], ", "))
	}
	for _, f := range c.findings {
		t.Error(f)
	}

	knobs, total := limiterStoreKnobs(reflect.TypeOf(config.Config{}))
	t.Logf("перепись конфигурации края: ручек окружения %d; ручек хранилища ограничителя %d %v", total, len(knobs), knobs)
	if total == 0 {
		t.Fatal("в конфигурации края не прочитано ни одной ручки окружения — перепись пуста, судить не о чем")
	}
	if len(knobs) > 0 {
		t.Errorf("конфигурация края объявляет хранилище ограничителя своими ручками %v — второе объявление "+
			"«где живёт состояние края», пару с флотом по нему не сверяет никто", knobs)
	}
}

// fleetPairOnRender — что получил процесс края в рендере одной цепочки.
type fleetPairOnRender struct {
	store     string
	fleet     int
	lane      identityposture.Provider
	consumers []string
}

// limiterActive — ограничитель анонимной почты строится только в полосе личности own.
func (p fleetPairOnRender) limiterActive() bool { return p.lane == identityposture.Own }

// judgeFleetPairEnv — вердикт пары по окружению пода края. label называет цепочку.
func judgeFleetPairEnv(label string, env map[string]string) (fleetPairOnRender, []string) {
	var p fleetPairOnRender
	var findings []string
	p.store = strings.ToLower(strings.TrimSpace(env[storeKindKnob]))
	if p.store == "" {
		findings = append(findings, fmt.Sprintf("%s: под края не получает %s — вид хранилища выбирает "+
			"умолчание сборки, а не профиль", label, storeKindKnob))
	}
	raw, has := env[fleetSizeKnob]
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	switch {
	case !has:
		findings = append(findings, fmt.Sprintf("%s: под края не получает %s — отказ старта судит пару по "+
			"умолчанию сборки, а не по объявленному флоту", label, fleetSizeKnob))
	case err != nil || n < 1:
		findings = append(findings, fmt.Sprintf("%s: %s = %q — не размер флота", label, fleetSizeKnob, raw))
	default:
		p.fleet = n
	}
	lane, lerr := configFromEnv(env).ResolvedIdentityProvider()
	if lerr != nil {
		findings = append(findings, fmt.Sprintf("%s: полоса личности не разобрана: %v — строится ли "+
			"ограничитель, неизвестно", label, lerr))
	}
	p.lane = lane
	p.consumers = []string{"однократность Idempotency-Key"}
	if p.limiterActive() {
		p.consumers = append(p.consumers, "ограничитель анонимной почты")
	}
	if p.store == "memory" && p.fleet > 1 {
		harm := []string{"однократность: повтор, попавший в соседнюю реплику, записи не найдёт, мутация исполнится второй раз"}
		if p.limiterActive() {
			harm = append(harm, fmt.Sprintf("ограничитель: каждая из %d реплик считает пороги источника и подсети и "+
				"ведро темпа своими (порог флота — %d-кратный), израсходованное доказательство работы проходит "+
				"на соседней реплике ещё раз", p.fleet, p.fleet))
		}
		findings = append(findings, fmt.Sprintf("%s: хранилище %q при флоте %d — пара неисполнима для "+
			"потребителей [%s]:\n    %s\n  Исходов два: одна реплика либо idempotency.store=postgres с адресом.",
			label, p.store, p.fleet, strings.Join(p.consumers, ", "), strings.Join(harm, "\n    ")))
	}
	return p, findings
}

// declaredFleet — размер флота, выведенный из сложенных слоёв значений края:
// потолок автомасштабирования при включённом, иначе число реплик.
func declaredFleet(merged map[string]any) (int, string) {
	if auto, ok := merged["autoscaling"].(map[string]any); ok {
		if on, _ := auto["enabled"].(bool); on {
			if n, ok := asInt(auto["maxReplicas"]); ok {
				return n, "autoscaling.maxReplicas"
			}
		}
	}
	if n, ok := asInt(merged["replicas"]); ok {
		return n, "replicas"
	}
	return 0, "не объявлен"
}

// mergedEdgeValues — значения чарта края, сложенные со слоями цепочки.
func mergedEdgeValues(t *testing.T, layers []map[string]any) map[string]any {
	t.Helper()
	merged := mergeInto(map[string]any{}, gatewayChartValues(t))
	for _, l := range layers {
		merged = mergeInto(merged, l)
	}
	return merged
}

// TestAnonMailFleet_EveryChainPairsTheStoreWithTheFleet — по рендеру чарта без
// зонтика и каждой цепочки: пара исполнима для всех активных потребителей, а
// размер флота в поде выведен из сложенных слоёв.
func TestAnonMailFleet_EveryChainPairsTheStoreWithTheFleet(t *testing.T) {
	chains := edgeAlertChains(t)
	if len(chains) < 2 {
		t.Fatalf("цепочек стендов не прочитано (рендеров %d) — обход пуст, и это отказ", len(chains))
	}
	withLimiter := 0
	for _, c := range chains {
		label := "чарт края без зонтика"
		if c.chain != nil {
			label = "цепочка " + c.name
		}
		out, err := renderEdgeChain(t, c)
		if err != nil {
			if strings.Contains(out, "idempotency.store=memory") {
				// Отказ рендера по паре — это находка о самой цепочке, а не
				// несозданное условие: профиль объявил неисполнимую пару.
				t.Errorf("%s: рендер отказал по паре «хранилище ↔ флот» — цепочка объявляет хранилище в памяти "+
					"процесса при флоте больше одной реплики (потребители: однократность, ограничитель анонимной "+
					"почты):\n%s", label, out)
				continue
			}
			t.Fatalf("%s: рендер не выполнен (%v) — условие не создано, вердикта нет:\n%s", label, err, out)
		}
		got, found := readEdgeContainer(t, out)
		if !found {
			t.Fatalf("%s: в рендере нет пода края — смотреть было не на что", label)
		}
		p, findings := judgeFleetPairEnv(label, got.env)
		for _, f := range findings {
			t.Error(f)
		}
		want, from := declaredFleet(mergedEdgeValues(t, edgeLayers(t, c)))
		if p.fleet != want {
			t.Errorf("%s: в поде %s = %d, а сложенные слои объявляют флот %d (из %s) — размер флота выписан "+
				"вторым литералом и разошёлся с объявлением", label, fleetSizeKnob, p.fleet, want, from)
		}
		if p.limiterActive() {
			withLimiter++
		}
		t.Logf("  %s: хранилище %q, флот %d (из %s), полоса личности %q, потребители [%s]",
			label, p.store, p.fleet, from, p.lane, strings.Join(p.consumers, ", "))
	}
	t.Logf("осмотрено рендеров %d (чарт без зонтика + цепочек %d); ограничитель построен на %d",
		len(chains), len(chains)-1, withLimiter)
	if withLimiter == 0 {
		t.Error("ни на одной цепочке ограничитель анонимной почты не строится — пара второго потребителя " +
			"не судилась ни разу, предпосылка гейта не держится")
	}
}

// fleetLayer — слой значений края: вид хранилища и флот.
func fleetLayer(store, dsn string, fleet int, autoscale bool) map[string]any {
	idem := map[string]any{"store": store}
	if dsn != "" {
		idem["dsn"] = dsn
	}
	layer := map[string]any{"idempotency": idem, "replicas": fleet}
	if autoscale {
		layer["autoscaling"] = map[string]any{"enabled": true, "minReplicas": 1, "maxReplicas": fleet}
		layer["replicas"] = 1
	} else {
		layer["autoscaling"] = map[string]any{"enabled": false}
	}
	return layer
}

// fixtureSharedStoreDSN — адрес общего хранилища в близнеце: отличим от
// настоящего и никуда не ведёт — рендер его только передаёт.
const fixtureSharedStoreDSN = "postgres://e3-fixture@fixture-not-a-host.invalid:5432/e3_fixture?sslmode=require"

// TestAnonMailFleet_MemoryProfileWithFleetTwoIsRefused — инъекция профиля
// `memory` + флот 2 на КАЖДОЙ цепочке, где ограничитель построен: рендер
// отказывает (оба вида объявления флота — `replicas` и автомасштабирование).
// Близнецы той же цепочки — флот 1 и `postgres` с адресом при флоте 2 — рендер
// проходит, судья молчит.
func TestAnonMailFleet_MemoryProfileWithFleetTwoIsRefused(t *testing.T) {
	judged := 0
	for _, c := range edgeAlertChains(t) {
		label := "чарт края без зонтика"
		if c.chain != nil {
			label = "цепочка " + c.name
		}
		out, err := renderEdgeChain(t, c)
		if err != nil {
			t.Fatalf("%s: контрольный рендер не выполнен (%v) — условие не создано:\n%s", label, err, out)
		}
		got, _ := readEdgeContainer(t, out)
		if p, _ := judgeFleetPairEnv(label, got.env); !p.limiterActive() {
			continue
		}
		judged++
		for _, auto := range []bool{false, true} {
			form := "replicas: 2"
			if auto {
				form = "autoscaling.maxReplicas: 2"
			}
			out, err := renderEdgeChain(t, c, fleetLayer("memory", "", 2, auto))
			switch {
			case err == nil:
				_, findings := judgeFleetPairEnv(label, mustEdgeEnv(t, label, out))
				t.Errorf("%s + слой «memory, %s»: рендер ПРОШЁЛ — неисполнимая пара доезжает до кластера; "+
					"судья окружения: %v", label, form, findings)
			case !strings.Contains(out, "idempotency.store=memory"):
				t.Errorf("%s + слой «memory, %s»: рендер отказал не по паре — причина отказа иная:\n%s",
					label, form, out)
			default:
				t.Logf("  %s + «memory, %s» → отказ рендера по паре", label, form)
			}

			for _, twin := range []struct {
				name  string
				layer map[string]any
			}{
				{"memory, флот 1", fleetLayer("memory", "", 1, auto)},
				{"postgres с адресом, флот 2", fleetLayer("postgres", fixtureSharedStoreDSN, 2, auto)},
			} {
				out, err := renderEdgeChain(t, c, twin.layer)
				if err != nil {
					t.Errorf("%s + близнец «%s» (%s): рендер отказал — гейт ловит законную пару:\n%s",
						label, twin.name, form, out)
					continue
				}
				if _, findings := judgeFleetPairEnv(label, mustEdgeEnv(t, label, out)); len(findings) > 0 {
					t.Errorf("%s + близнец «%s» (%s): судья не молчит: %v", label, twin.name, form, findings)
				}
			}
		}
	}
	t.Logf("инъекций выполнено на %d цепочках с ограничителем", judged)
	if judged == 0 {
		t.Fatal("ни одной цепочки с ограничителем — инъекция не исполнилась, вердикта нет")
	}
}

// mustEdgeEnv — окружение пода края из рендера; без пода — отказ пробы.
func mustEdgeEnv(t *testing.T, label, rendered string) map[string]string {
	t.Helper()
	got, found := readEdgeContainer(t, rendered)
	if !found {
		t.Fatalf("%s: в рендере нет пода края", label)
	}
	return got.env
}

// TestAnonMailFleet_PairJudgeFiresAndStaysSilent — судья окружения на
// синтетике: дефект краснеет и называет потребителей, законный близнец молчит.
func TestAnonMailFleet_PairJudgeFiresAndStaysSilent(t *testing.T) {
	own := map[string]string{config.IdentityProviderKnob: identityposture.Own.String()}
	with := func(base map[string]string, kv ...string) map[string]string {
		out := map[string]string{}
		for k, v := range base {
			out[k] = v
		}
		for i := 0; i+1 < len(kv); i += 2 {
			out[kv[i]] = kv[i+1]
		}
		return out
	}

	_, f := judgeFleetPairEnv("синтетика", with(own, storeKindKnob, "memory", fleetSizeKnob, "2"))
	if len(f) != 1 || !strings.Contains(f[0], "ограничитель анонимной почты") ||
		!strings.Contains(f[0], "однократность") {
		t.Errorf("memory + флот 2 при own: ожидалась одна находка, называющая обоих потребителей; получено %v", f)
	}

	_, f = judgeFleetPairEnv("синтетика", map[string]string{storeKindKnob: "memory", fleetSizeKnob: "2"})
	if len(f) != 1 || strings.Contains(f[0], "ограничитель") {
		t.Errorf("memory + флот 2 без полосы own: ожидалась находка только об однократности; получено %v", f)
	}

	for _, twin := range []map[string]string{
		with(own, storeKindKnob, "memory", fleetSizeKnob, "1"),
		with(own, storeKindKnob, "postgres", fleetSizeKnob, "2"),
	} {
		if _, f := judgeFleetPairEnv("синтетика", twin); len(f) > 0 {
			t.Errorf("законная пара %v: судья не молчит: %v", twin, f)
		}
	}

	if _, f := judgeFleetPairEnv("синтетика", with(own, storeKindKnob, "memory")); len(f) != 1 ||
		!strings.Contains(f[0], fleetSizeKnob) {
		t.Errorf("размер флота не передан: ожидалась находка с именем %s; получено %v", fleetSizeKnob, f)
	}
}

// TestAnonMailFleet_ConsumerCensusFiresAndStaysSilent — разбор потребителей на
// синтетике: второе объявление вида у ограничителя краснеет и называет функцию,
// законная форма молчит, слово в комментарии не засчитывается чтением.
func TestAnonMailFleet_ConsumerCensusFiresAndStaysSilent(t *testing.T) {
	const idem = "package main\nfunc buildIdempotencyStore(cfg config.Config) (middleware.IdempotencyStore, error) {\n" +
		"\tswitch cfg.IdempotencyStoreKind {\n\t}\n\treturn nil, nil\n}\n"
	lawful := map[string]string{
		"idem.go": idem,
		"anon.go": "package main\nfunc buildAnonMailStore(cfg config.Config) (anonmail.Store, error) {\n" +
			"\tswitch cfg.IdempotencyStoreKind {\n\t}\n\treturn nil, nil\n}\n",
	}
	if c := censusStoreConsumers(lawful); len(c.findings) > 0 || len(c.readers) != 2 || len(c.builders) != 2 {
		t.Errorf("законная форма: ожидались 2 читателя, 2 потребителя, 0 находок; получено %+v", c)
	}

	second := map[string]string{
		"idem.go": idem,
		"anon.go": "package main\n// cfg.IdempotencyStoreKind здесь только в комментарии\n" +
			"func buildAnonMailStore(cfg config.Config) (anonmail.Store, error) {\n" +
			"\tswitch cfg.AnonMailStoreKind {\n\t}\n\treturn nil, nil\n}\n",
	}
	c := censusStoreConsumers(second)
	if len(c.findings) != 1 || !strings.Contains(c.findings[0], "anon.go:buildAnonMailStore") {
		t.Errorf("второе объявление вида у ограничителя: ожидалась одна находка с именем функции; получено %v", c.findings)
	}

	idemSecond := map[string]string{
		"idem.go": "package main\nfunc buildIdempotencyStore(cfg config.Config) (middleware.IdempotencyStore, error) {\n" +
			"\tswitch cfg.IdemKind {\n\t}\n\treturn nil, nil\n}\n" +
			"func main() { _ = cfg.IdempotencyStoreKind }\n",
		"anon.go": lawful["anon.go"],
	}
	if c := censusStoreConsumers(idemSecond); len(c.findings) != 1 ||
		!strings.Contains(c.findings[0], "idem.go:buildIdempotencyStore") {
		t.Errorf("однократность без чтения вида (вид читает лишь main): ожидалась одна находка с именем "+
			"построителя; получено %v", c.findings)
	}

	if c := censusStoreConsumers(map[string]string{"idem.go": idem}); len(c.findings) != 1 ||
		!strings.Contains(c.findings[0], "предпосылка") || !strings.Contains(c.findings[0], "ограничитель") {
		t.Errorf("построителя ограничителя нет: ожидалась находка предпосылки; получено %v", c.findings)
	}

	if c := censusStoreConsumers(map[string]string{}); len(c.findings) != 1 ||
		!strings.Contains(c.findings[0], "обход пуст") {
		t.Errorf("пустой обход: ожидалась находка пустого обхода; получено %v", c.findings)
	}

	type cfgWithSecond struct {
		Kind string `envconfig:"KACHO_IDEMPOTENCY_STORE"`
		Anon string `envconfig:"KACHO_API_GATEWAY_ANON_MAIL_STORE"`
	}
	if knobs, total := limiterStoreKnobs(reflect.TypeOf(cfgWithSecond{})); total != 2 ||
		len(knobs) != 1 || knobs[0] != "KACHO_API_GATEWAY_ANON_MAIL_STORE" {
		t.Errorf("ручка хранилища ограничителя в конфигурации не найдена: %v из %d", knobs, total)
	}
}
