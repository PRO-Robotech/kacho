// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_callback_transport_test.go — обратный вызов к слушателю хуков iam
// обязан идти тем же транспортом, каким этот слушатель работает на СВОЁМ стенде,
// и транспорт обязан быть решением ПРОФИЛЯ, а не константой шаблона.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// У слушателя хуков iam несколько вызывающих полос. По ним едут сведения о
// личности — заведение пользователя, завершение восстановления доступа,
// обогащение токена. Инвариант платформы требует защищённого транспорта на
// КАЖДОМ слушателе, включая внутренние (`security.md` §«AuthN+AuthZ ВЕЗДЕ»,
// §«Production-mode обязателен ВЕЗДЕ»): «внутренний — значит доверенный»
// объявлено запрещённым допущением.
//
// Нарушить это можно двумя разными способами, и проверка ловит оба:
//
//  1. ТРАНСПОРТ ЗАШИТ В ШАБЛОН. Схема выписана в шаблоне чарта константой —
//     тогда профиль не может исправить транспорт, даже если захочет: значения
//     он не читает. Боевая посадка физически не в состоянии объявить такую
//     полосу защищённой, и никакая правка профиля этого не изменит.
//
//  2. ТРАНСПОРТ РАСХОДИТСЯ СО СЛУШАТЕЛЕМ. Полоса объявлена открытым текстом
//     там, где слушатель работает под TLS (сведения о личности едут читаемыми),
//     ЛИБО объявлена под TLS там, где слушатель открыт (рукопожатие не
//     состоится, вызов не доедет НИКОГДА — и это тихо: вызывающий считает
//     вызов сделанным).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ПЕРЕЧЕНЬ ПОЛОС ВЫВОДИТСЯ, А НЕ ВЫПИСАН
//
// Выписанный перечень разойдётся при добавлении следующей полосы МОЛЧА: новая
// полоса просто не попадёт под проверку, и её состояние не сможет покраснеть.
// Поэтому полосы находятся обходом — по пути маршрута хуков — в двух местах,
// где их вообще можно объявить: в значениях профилей стека и в шаблонах наших
// подчартов. Ни одно имя полосы и ни одно имя чарта здесь не выписано.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ПРОВЕРКА ПЕРЕЖИВЁТ СМЕНУ ПОСТАВЩИКА ЛИЧНОСТИ
//
// Она не знает ни имени внешнего сервера OAuth, ни его чарта, ни его ключей
// значений: и полосы, и их транспорт выводятся из дерева. Уедет внешний сервер
// — уедут ОБЪЯВЛЕННЫЕ ИМ полосы, а остальные останутся под проверкой, и обход
// не потеряет ни предмета, ни способности покраснеть. Это записано отдельным
// утверждением ниже (TestScanCallbackTransport_SurvivesTheOAuthServerLeaving):
// перечню, которому подали вход БЕЗ полос внешнего сервера, по-прежнему есть
// что судить.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ОБЪЯВЛЕНИЯ, А НЕ РЕНДЕР
//
// Та же причина, что у соседних posture_parity_test.go и
// dbtls_declaration_test.go: контракт — то, что профиль ОБЪЯВЛЯЕТ. Проверке не
// нужны ни `helm`, ни скачанные зависимости чартов, поэтому она не умеет
// пропуститься. Рендер тут и не помог бы: схема, приехавшая из умолчания чарта,
// в манифесте выглядит точно так же, как объявленная профилем.
//
// ─────────────────────────────────────────────────────────────────────────────
// ГРАНИЦА ПРЕДМЕТА (названа, чтобы «зелено» не читалось шире, чем есть)
//
//   - Проверяется СОГЛАСИЕ полосы со слушателем, а НЕ решение стенда включить
//     TLS на слушателе. Стенд, чей слушатель хуков объявлен открытым, приходит
//     сюда законным: требовать https к открытому слушателю значило бы требовать
//     соединения, которое не установится. Профиль, включивший TLS на этом
//     слушателе, приходит под проверку САМ, без правки этого файла.
//
//   - Проверяются ТОЛЬКО подчарты НАШЕГО дерева. Чужой чарт объявляет свои
//     полосы значениями, и они попадают сюда через дерево значений стека.
//
//   - Проверяется ТРАНСПОРТ, а не аутентификация вызывающего: подпись запроса
//     общим секретом — отдельный предмет, здесь не рассматривается.
//
//   - Проверяется ОБЪЯВЛЕНИЕ полосы, а не то, смонтирована ли она вызывающему.
//     Полоса, объявленная в конфигурации, которую никто не монтирует, осталась бы
//     под проверкой намеренно: она станет живой в тот день, когда её смонтируют,
//     и транспорт к тому дню обязан быть уже верным.
//
//     ЗДЕСЬ СТОЯЛО «конфигурация службы личности сегодня не монтируется», со
//     ссылкой на предмет #904 как на открытый. Утверждение пережило свой предмет
//     ДВАЖДЫ: карту монтируют и читают (том, монтирование контейнеру подстановки,
//     довод `--config` на отрендеренный файл), а сам предмет закрыт. Граница,
//     объявленная шире факта, читается как действующее ограничение — и потому её
//     никто не перепроверяет. Число монтирующих профилей теперь ВЫВОДИТ
//     deploy/identity_config_mount_census_test.go, и объявление, отставшее от
//     дерева, роняет прогон. Граница самого этого гейта от поправки не меняется:
//     он судит объявления, а не монтирование.
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

// hooksRoute — путь, по которому узнаётся обратный вызов к слушателю хуков iam.
// Имя полосы — сегмент за ним. Ни одна полоса поимённо здесь не названа.
const hooksRoute = "/iam/v1/hooks/"

var hooksRouteLane = regexp.MustCompile(regexp.QuoteMeta(hooksRoute) + `([a-z][a-z0-9-]*)`)

// СХЕМА ЧИТАЕТСЯ У ГОЛОВЫ АДРЕСА, А НЕ РЯДОМ С МАРШРУТОМ. Между схемой и путём
// стоят узел и порт, и каждый из них — своё действие шаблона; предикат, искавший
// схему вплотную к маршруту, находил ПОСЛЕДНЕЕ действие строки и объявлял схемой
// номер порта. Поэтому: находим `://`, смотрим на то, что стоит НЕПОСРЕДСТВЕННО
// перед ним, — литерал либо действие.
var trailingLiteralScheme = regexp.MustCompile(`(?i)(https?)$`)

// trailingAction — действие шаблона, стоящее вплотную перед `://`.
var trailingAction = regexp.MustCompile(`\{\{[^{}]*\}\}$`)

// valuesPathInAction — путь значений внутри действия. Именно он делает транспорт
// решением профиля.
var valuesPathInAction = regexp.MustCompile(`\.Values\.([A-Za-z0-9_]+(?:\.[A-Za-z0-9_]+)*)`)

// templateComment — комментарий шаблона `{{/* … */}}`, возможно многострочный.
// Его текст рассказывает о полосе, но её не объявляет; вырезается с сохранением
// числа строк, чтобы координаты оставались верными.
var templateComment = regexp.MustCompile(`(?s)\{\{-?/\*.*?\*/-?\}\}`)

func stripTemplateComments(src string) string {
	return templateComment.ReplaceAllStringFunc(src, func(m string) string {
		return strings.Repeat("\n", strings.Count(m, "\n"))
	})
}

// hookDecl — ОДНО объявление полосы в шаблоне нашего подчарта.
type hookDecl struct {
	chart string // ключ значений подчарта, чей шаблон это объявил
	lane  string
	coord string // файл:строка
	// Ровно одно из двух непусто. Литеральная схема — и есть находка уровня
	// дерева: профиль такую полосу переопределить не может.
	literalScheme string
	schemePath    []string // ключ подчарта + путь внутри его значений
}

// templateHookDecls — объявления полос в шаблонах НАШИХ подчартов, выведенные
// из дерева. Ни имя чарта, ни имя полосы не выписаны.
func templateHookDecls(t *testing.T) []hookDecl {
	t.Helper()
	var out []hookDecl
	dirs := subchartDirs(t)
	keys := make([]string, 0, len(dirs))
	for k := range dirs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		for _, f := range chartTemplateFiles(t, dirs[key]) {
			raw, err := os.ReadFile(f)
			if err != nil {
				t.Fatalf("read %s: %v", f, err)
			}
			for i, line := range strings.Split(stripTemplateComments(string(raw)), "\n") {
				// Объявлением считается строка, которая АДРЕСУЕТ маршрут, а не
				// строка, которая про него рассказывает: комментарии шаблона
				// вырезаны выше, комментарий YAML отбрасывается здесь.
				if strings.HasPrefix(strings.TrimSpace(line), "#") {
					continue
				}
				for _, m := range hooksRouteLane.FindAllStringSubmatchIndex(line, -1) {
					d := hookDecl{chart: key, lane: line[m[2]:m[3]],
						coord: fmt.Sprintf("%s:%d", f, i+1)}
					d.literalScheme, d.schemePath = schemeSource(line[:m[0]], key)
					out = append(out, d)
				}
			}
		}
	}
	return out
}

// schemeSource — откуда берётся схема адреса, объявленного в шаблоне: константа
// либо значение. Пустой ответ по обоим полям означает «взять неоткуда» — и это
// тоже находка, а не молчание.
func schemeSource(prefix, subchartKey string) (literal string, valuesPath []string) {
	sep := strings.LastIndex(prefix, "://")
	if sep < 0 {
		return "", nil // маршрут назван, но адресом не является
	}
	head := prefix[:sep]
	if m := trailingLiteralScheme.FindStringSubmatch(head); m != nil {
		return strings.ToLower(m[1]), nil
	}
	if act := trailingAction.FindString(head); act != "" {
		if vp := valuesPathInAction.FindStringSubmatch(act); vp != nil {
			parts := strings.Split(vp[1], ".")
			// `global` — КОРНЕВОЙ узел значений, а не узел подчарта. Helm
			// раздаёт его каждому подчарту, поэтому `.Values.global.X`,
			// написанное в шаблоне подчарта, читается из корня как `global.X`.
			// Приписывать ему ключ подчарта значит искать `<чарт>.global.X`,
			// чего в дереве значений нет НИКОГДА — и тогда объявление, схему
			// которого профиль задаёт вполне определённо, объявляется
			// «схему взять неоткуда». Проверка при этом краснеет на исправном
			// дереве, то есть перестаёт что-либо измерять.
			if parts[0] == "global" {
				return "", parts
			}
			return "", append([]string{subchartKey}, parts...)
		}
	}
	return "", nil
}

// chartTemplateFiles — шаблоны чарта, включая частичные (`*.tpl`): адрес может
// собираться и там.
func chartTemplateFiles(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	for _, pat := range []string{"*.yaml", "*.yml", "*.tpl"} {
		m, err := filepath.Glob(filepath.Join(dir, "templates", pat))
		if err != nil {
			t.Fatalf("glob %s: %v", dir, err)
		}
		files = append(files, m...)
	}
	sort.Strings(files)
	return files
}

// ─────────────────────────────────────────────────────────────────────────────
// Факты одного стека.

type callbackStackFacts struct {
	listenerTLS bool           // слушатель хуков iam на этом стенде работает под TLS
	declared    map[string]any // слияние ТОЛЬКО профилей стека
	effective   map[string]any // умолчания подчартов + значения умбреллы + профили
}

// hooksListenerTLS повторяет ВЫЧИСЛЕНИЕ ШАБЛОНА, а не пересказывает его словами:
// полистенное переопределение сильнее общей ручки. Два расчёта одного и того же
// разъезжаются молча, поэтому расчёт здесь один.
func hooksListenerTLS(effective map[string]any, iamKey string) bool {
	if v, ok := lookup(effective, iamKey, "mtls", "hooksAndMetrics"); ok {
		return v == true
	}
	v, ok := lookup(effective, iamKey, "mtls", "httpListeners")
	return ok && v == true
}

type hookURLHit struct {
	path   []string
	lane   string
	scheme string
}

var absoluteHookURL = regexp.MustCompile(`(?i)^(https?)://[^\s"']*` + regexp.QuoteMeta(hooksRoute) + `([a-z][a-z0-9-]*)`)

// hookURLLeaves — строковые листья дерева значений, адресующие маршрут хуков.
func hookURLLeaves(node any, path []string) []hookURLHit {
	var out []hookURLHit
	switch v := node.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			out = append(out, hookURLLeaves(v[k], append(append([]string{}, path...), k))...)
		}
	case []any:
		for i, e := range v {
			out = append(out, hookURLLeaves(e, append(append([]string{}, path...), fmt.Sprintf("[%d]", i)))...)
		}
	case string:
		if m := absoluteHookURL.FindStringSubmatch(strings.TrimSpace(v)); m != nil {
			out = append(out, hookURLHit{path: path, lane: m[2], scheme: strings.ToLower(m[1])})
		}
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// ЯДРО — чистая функция над фактами, чтобы самопроверка ниже могла подать ей
// синтетический вход, а не подделывать дерево.

type callbackFinding struct {
	stack string // пусто у находок уровня дерева
	lane  string
	coord string
	kind  string
	value string
}

const (
	kindHardWired  = "транспорт зашит в шаблон"
	kindUnresolved = "схема не резолвится"
	kindPlaintext  = "открытый транспорт к слушателю под TLS"
	kindOverTLS    = "шифрованный транспорт к открытому слушателю"
	kindUndeclared = "транспорт не объявлен профилем стека"
)

// callbackCensus — объём осмотренного. «Ноль находок» обязано быть отличимо от
// «ноль прочитанного».
type callbackCensus struct {
	lanes    []string // имена полос, выведенные из дерева
	decls    int      // объявлений всего (шаблоны + значения, по всем стекам)
	checked  int      // пар «стек × объявление», по которым вынесен вердикт
	secured  int      // из них под защищённым транспортом
	tlsStack int      // стеков, чей слушатель хуков работает под TLS
}

func scanCallbackTransport(tmpl []hookDecl, stacks map[string]callbackStackFacts,
	iamKey string) ([]callbackFinding, callbackCensus) {
	var out []callbackFinding
	c := callbackCensus{}
	lanes := map[string]bool{}

	// Уровень дерева: схема, выписанная константой, — находка независимо от
	// стенда. Профиль такую полосу переопределить не может ВООБЩЕ.
	var judgeable []hookDecl
	for _, d := range tmpl {
		lanes[d.lane] = true
		switch {
		case d.literalScheme != "":
			out = append(out, callbackFinding{lane: d.lane, coord: d.coord,
				kind: kindHardWired, value: d.literalScheme})
		case len(d.schemePath) == 0:
			out = append(out, callbackFinding{lane: d.lane, coord: d.coord,
				kind: kindUnresolved, value: "ни константы, ни значения"})
		default:
			judgeable = append(judgeable, d)
		}
	}

	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)

	type observation struct {
		lane, coord, scheme string
		declared            bool
	}

	for _, name := range names {
		f := stacks[name]
		if f.listenerTLS {
			c.tlsStack++
		}
		var seen []observation

		for _, d := range judgeable {
			v, ok := lookup(f.effective, d.schemePath...)
			if !ok {
				out = append(out, callbackFinding{stack: name, lane: d.lane, coord: d.coord,
					kind: kindUnresolved, value: strings.Join(d.schemePath, ".")})
				continue
			}
			_, declared := lookup(f.declared, d.schemePath...)
			seen = append(seen, observation{d.lane, d.coord,
				strings.ToLower(strings.TrimSpace(fmt.Sprint(v))), declared})
		}
		for _, h := range hookURLLeaves(f.effective, nil) {
			lanes[h.lane] = true
			_, declared := lookup(f.declared, h.path...)
			seen = append(seen, observation{h.lane, strings.Join(h.path, "."), h.scheme, declared})
		}

		c.decls += len(seen)
		for _, o := range seen {
			c.checked++
			if o.scheme == "https" {
				c.secured++
			}
			switch {
			case f.listenerTLS && o.scheme != "https":
				out = append(out, callbackFinding{stack: name, lane: o.lane, coord: o.coord,
					kind: kindPlaintext, value: o.scheme})
			case !f.listenerTLS && o.scheme == "https":
				out = append(out, callbackFinding{stack: name, lane: o.lane, coord: o.coord,
					kind: kindOverTLS, value: o.scheme})
			case f.listenerTLS && !o.declared:
				out = append(out, callbackFinding{stack: name, lane: o.lane, coord: o.coord,
					kind: kindUndeclared, value: o.scheme})
			}
		}
	}

	for l := range lanes {
		c.lanes = append(c.lanes, l)
	}
	sort.Strings(c.lanes)
	return out, c
}

// ─────────────────────────────────────────────────────────────────────────────
// Сбор фактов из дерева.

// iamSubchartKey — ключ значений подчарта, ВЫВЕДЕННЫЙ из дерева: тот из наших
// подчартов, чьи шаблоны адресуют маршрут хуков. Имя не выписано: чарт
// переименуют — проверка последует за ним.
func iamSubchartKey(decls []hookDecl) string {
	for _, d := range decls {
		if d.chart != "" {
			return d.chart
		}
	}
	return ""
}

func callbackStacks(t *testing.T, iamKey string) map[string]callbackStackFacts {
	t.Helper()
	chains := deployStacks(t)
	base := readYAML(t, filepath.Join(umbrellaDir, "values.yaml"))

	// Умолчания подчартов — под их ключом, как их видит helm.
	defaults := map[string]any{}
	dirs := subchartDirs(t)
	keys := make([]string, 0, len(dirs))
	for k := range dirs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		vf := filepath.Join(dirs[key], "values.yaml")
		if _, err := os.Stat(vf); err != nil {
			continue
		}
		defaults[key] = readYAML(t, vf)
	}

	out := map[string]callbackStackFacts{}
	for name, chain := range chains {
		declared := map[string]any{}
		for _, p := range chain {
			declared = mergeValues(declared, readYAML(t, filepath.Join(umbrellaDir, p)))
		}
		effective := mergeValues(map[string]any{}, defaults)
		effective = mergeValues(effective, base)
		effective = mergeValues(effective, declared)
		out[name] = callbackStackFacts{
			listenerTLS: hooksListenerTLS(effective, iamKey),
			declared:    declared,
			effective:   effective,
		}
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// САМА ПРОВЕРКА.

func TestIdentityCallbacks_TransportIsProfileDecidedAndMatchesTheListener(t *testing.T) {
	tmpl := templateHookDecls(t)
	iamKey := iamSubchartKey(tmpl)
	if iamKey == "" {
		t.Fatalf("ни один шаблон наших подчартов не адресует %s — обход перестал узнавать "+
			"дерево, а не полосы исчезли", hooksRoute)
	}
	stacks := callbackStacks(t, iamKey)

	findings, c := scanCallbackTransport(tmpl, stacks, iamKey)

	// Проверка СВОЕЙ предпосылки: «ноль находок» обязано быть отличимо от
	// «ноль прочитанного».
	if len(c.lanes) == 0 || len(tmpl) == 0 || len(stacks) == 0 || c.checked == 0 {
		t.Fatalf("обход ничего не прочитал: полос=%d, объявлений в шаблонах=%d, стеков=%d, "+
			"проверено пар=%d — предикат перестал узнавать дерево",
			len(c.lanes), len(tmpl), len(stacks), c.checked)
	}
	t.Logf("осмотрено: подчарт-хозяин %q, полос найдено=%d (%s), объявлений в шаблонах=%d, "+
		"стеков=%d, из них со слушателем хуков под TLS=%d, проверено пар стек×объявление=%d, "+
		"из них под защищённым транспортом=%d",
		iamKey, len(c.lanes), strings.Join(c.lanes, " "), len(tmpl),
		len(stacks), c.tlsStack, c.checked, c.secured)

	for _, f := range findings {
		switch f.kind {
		case kindHardWired:
			t.Errorf("полоса %q: %s — схема %q ВЫПИСАНА В ШАБЛОНЕ КОНСТАНТОЙ. Профиль её "+
				"переопределить не может: значения он не читает, поэтому боевая посадка "+
				"физически не в состоянии объявить эту полосу защищённой. Схему обязано "+
				"задавать значение (.Values.…), а профиль стенда — объявлять его",
				f.lane, f.coord, f.value)
		case kindUnresolved:
			t.Errorf("%sполоса %q: %s — схему взять неоткуда (%s). Адрес обязан выводиться из "+
				"значения, иначе транспорт не решает никто", stackPrefix(f.stack), f.lane,
				f.coord, f.value)
		case kindPlaintext:
			t.Errorf("%sполоса %q (%s) объявлена схемой %q, а слушатель хуков этого стенда "+
				"работает под TLS. По этой полосе едут сведения о личности; открытый транспорт "+
				"на внутреннем слушателе запрещён (security.md §«AuthN+AuthZ ВЕЗДЕ»)",
				stackPrefix(f.stack), f.lane, f.coord, f.value)
		case kindOverTLS:
			t.Errorf("%sполоса %q (%s) объявлена схемой %q, а слушатель хуков этого стенда "+
				"ОТКРЫТ. Рукопожатие не состоится, и вызов не доедет НИКОГДА — тихо, потому "+
				"что вызывающий считает вызов сделанным", stackPrefix(f.stack), f.lane,
				f.coord, f.value)
		case kindUndeclared:
			t.Errorf("%sполоса %q (%s) защищена, но схема НЕ ОБЪЯВЛЕНА ни одним профилем "+
				"стека — значение приехало из умолчания. Умолчание чарта есть свойство чарта: "+
				"оно меняется под профилем без единой правки профиля",
				stackPrefix(f.stack), f.lane, f.coord)
		}
	}
}

func stackPrefix(stack string) string {
	if stack == "" {
		return ""
	}
	return stack + ": "
}

// ─────────────────────────────────────────────────────────────────────────────
// САМОПРОВЕРКА — инъекция в обе стороны на синтетическом входе той же формы.
//
// Без положительного контроля отрицание зеленеет на всём сломанном: обход,
// который перестал что-либо узнавать, «не находит» ровно так же, как чистое
// дерево.

func synthCallbackStack(tls bool, declared, effective map[string]any) map[string]callbackStackFacts {
	return map[string]callbackStackFacts{
		"injected": {listenerTLS: tls, declared: declared, effective: effective},
	}
}

func synthSchemeTree(scheme string) map[string]any {
	return map[string]any{"iam": map[string]any{"hooks": map[string]any{"scheme": scheme}}}
}

// schemeSource читает голову адреса, а не соседнее действие. Проба стоит отдельно,
// потому что ПЕРВАЯ её редакция искала схему вплотную к маршруту и на настоящем
// дереве объявляла схемой номер порта: между схемой и путём стоят узел и порт,
// каждый — своё действие шаблона.
func TestSchemeSource_ReadsTheHeadOfTheAddress(t *testing.T) {
	const port = `{{ .Values.service.internal.hooksHttpPort | default 9092 }}`

	// (а) константа схемы — находка, и её не заслоняет действие порта справа.
	lit, path := schemeSource("                    url: http://kaname-internal."+
		"{{ .Release.Namespace }}.svc:"+port, "kaname")
	if lit != "http" || path != nil {
		t.Fatalf("константа схемы не опознана у головы адреса: lit=%q path=%v", lit, path)
	}

	// (б) схема из значений — путь берётся из действия ПЕРЕД `://`, а не из
	//     последнего действия строки.
	lit, path = schemeSource("                    url: {{ .Values.kratos.config.hooks.scheme }}"+
		"://kaname-internal.{{ .Release.Namespace }}.svc:"+port, "kaname")
	if lit != "" || strings.Join(path, ".") != "kaname.kratos.config.hooks.scheme" {
		t.Fatalf("схема из значений прочитана неверно: lit=%q path=%v", lit, path)
	}

	// (б2) схема из ГЛОБАЛЬНЫХ значений — путь корневой, ключом подчарта не
	//      предваряется: `global` раздаётся подчартам, но живёт в корне.
	lit, path = schemeSource("                    url: {{ .Values.global.kacho.identity.hooks.scheme }}"+
		"://kaname-internal.{{ .Release.Namespace }}.svc:"+port, "kaname")
	if lit != "" || strings.Join(path, ".") != "global.kacho.identity.hooks.scheme" {
		t.Fatalf("глобальный путь схемы прочитан как путь подчарта: lit=%q path=%v", lit, path)
	}

	// (в) строка называет маршрут, но адресом не является — схему взять неоткуда.
	if lit, path = schemeSource("  описание маршрута ", "kaname"); lit != "" || path != nil {
		t.Fatalf("строка без адреса объявлена объявлением: lit=%q path=%v", lit, path)
	}
}

// Комментарий шаблона рассказывает о полосе, но её не объявляет. Без вырезания
// обход считал бы объявлением каждое упоминание маршрута в шапке файла.
func TestStripTemplateComments_KeepsLineNumbers(t *testing.T) {
	src := "a\n{{/*\nурок про " + hooksRoute + "provision\n*/}}\nb\n"
	got := stripTemplateComments(src)
	if strings.Contains(got, hooksRoute) {
		t.Fatalf("комментарий шаблона не вырезан: %q", got)
	}
	if strings.Count(got, "\n") != strings.Count(src, "\n") {
		t.Fatalf("вырезание сдвинуло номера строк: было %d, стало %d",
			strings.Count(src, "\n"), strings.Count(got, "\n"))
	}
}

func TestScanCallbackTransport_SelfTest(t *testing.T) {
	valuesDecl := hookDecl{lane: "provision", coord: "tpl:1",
		schemePath: []string{"iam", "hooks", "scheme"}}

	// (а) внесённый дефект №1 — схема выписана в шаблоне константой. Обязан
	//     покраснеть НЕЗАВИСИМО от стенда и назвать полосу с координатой.
	hard := []hookDecl{{lane: "recovery", coord: "tpl:9", literalScheme: "http"}}
	got, _ := scanCallbackTransport(hard, synthCallbackStack(false, nil, map[string]any{}), "iam")
	sawHardWired := false
	for _, f := range got {
		if f.kind == kindHardWired && f.lane == "recovery" && f.coord == "tpl:9" {
			sawHardWired = true
		}
	}
	if !sawHardWired {
		t.Fatalf("константа схемы в шаблоне не поймана или поймана без координаты: %+v", got)
	}

	// (б) внесённый дефект №2 — открытая полоса при слушателе под TLS.
	got, _ = scanCallbackTransport([]hookDecl{valuesDecl},
		synthCallbackStack(true, synthSchemeTree("http"), synthSchemeTree("http")), "iam")
	if len(got) != 1 || got[0].kind != kindPlaintext || got[0].lane != "provision" {
		t.Fatalf("открытая полоса при слушателе под TLS не поймана: %+v", got)
	}

	// (в) внесённый дефект №3 — открытая полоса, объявленная ЗНАЧЕНИЯМИ (не
	//     шаблоном): та же находка, другой источник объявления.
	got, _ = scanCallbackTransport(nil,
		synthCallbackStack(true, nil, map[string]any{
			"other": map[string]any{"url": "http://iam-internal.kacho.svc:9092" + hooksRoute + "token"},
		}), "iam")
	if len(got) != 1 || got[0].kind != kindPlaintext || got[0].lane != "token" {
		t.Fatalf("открытая полоса, объявленная значениями, не поймана: %+v", got)
	}

	// (г) внесённый дефект №4 — https к ОТКРЫТОМУ слушателю: вызов не доедет.
	got, _ = scanCallbackTransport([]hookDecl{valuesDecl},
		synthCallbackStack(false, synthSchemeTree("https"), synthSchemeTree("https")), "iam")
	if len(got) != 1 || got[0].kind != kindOverTLS {
		t.Fatalf("https к открытому слушателю не пойман: %+v", got)
	}

	// (д) внесённый дефект №5 — защищено, но не объявлено профилем стека.
	got, _ = scanCallbackTransport([]hookDecl{valuesDecl},
		synthCallbackStack(true, map[string]any{}, synthSchemeTree("https")), "iam")
	if len(got) != 1 || got[0].kind != kindUndeclared {
		t.Fatalf("унаследованное умолчание на стенде под TLS не поймано: %+v", got)
	}

	// (е) ЗАКОННЫЙ БЛИЗНЕЦ ТОЙ ЖЕ ФОРМЫ №1 — схема из значений, объявлена
	//     профилем, https, слушатель под TLS. Молчит.
	got, c := scanCallbackTransport([]hookDecl{valuesDecl},
		synthCallbackStack(true, synthSchemeTree("https"), synthSchemeTree("https")), "iam")
	if len(got) != 0 {
		t.Fatalf("законная защищённая полоса покрашена: %+v", got)
	}
	if c.secured != 1 || c.checked != 1 {
		t.Fatalf("перепись законной полосы неверна: %+v", c)
	}

	// (ж) ЗАКОННЫЙ БЛИЗНЕЦ ТОЙ ЖЕ ФОРМЫ №2 — открытый слушатель и открытая
	//     полоса: требовать TLS там значило бы требовать несуществующего.
	got, _ = scanCallbackTransport([]hookDecl{valuesDecl},
		synthCallbackStack(false, synthSchemeTree("http"), synthSchemeTree("http")), "iam")
	if len(got) != 0 {
		t.Fatalf("согласованная открытая полоса покрашена: %+v", got)
	}
}

// Проверка обязана пережить уход внешнего сервера OAuth: полосы, которые он
// объявлял значениями, исчезнут вместе с ним — оставшиеся обязаны остаться под
// судом. Ни имени того сервера, ни его ключей в предикатах нет; здесь это
// утверждается ИСХОДОМ, а не прочтением.
func TestScanCallbackTransport_SurvivesTheOAuthServerLeaving(t *testing.T) {
	valuesDecl := hookDecl{lane: "provision", coord: "tpl:1",
		schemePath: []string{"iam", "hooks", "scheme"}}

	// Дерево значений БЕЗ единой полосы внешнего сервера — только шаблонная.
	got, c := scanCallbackTransport([]hookDecl{valuesDecl},
		synthCallbackStack(true, synthSchemeTree("http"), synthSchemeTree("http")), "iam")
	if len(got) != 1 || got[0].kind != kindPlaintext {
		t.Fatalf("после ухода внешнего сервера оставшаяся полоса перестала судиться: %+v", got)
	}
	if len(c.lanes) != 1 || c.checked != 1 {
		t.Fatalf("перепись после ухода внешнего сервера пуста: %+v", c)
	}
}

// Предикаты обязаны узнавать НАСТОЯЩЕЕ дерево, а не только синтетику: иначе
// самопроверка выше зелёная, а обход читает ноль.
func TestCallbackPredicates_RecogniseTheRealTree(t *testing.T) {
	tmpl := templateHookDecls(t)
	iamKey := iamSubchartKey(tmpl)
	if iamKey == "" {
		t.Fatalf("подчарт-хозяин маршрута %s не выведен из дерева", hooksRoute)
	}
	if len(tmpl) == 0 {
		t.Errorf("в шаблонах наших подчартов не найдено ни одного объявления полосы — "+
			"обход перестал их узнавать (подчарт-хозяин %q)", iamKey)
	}
	stacks := callbackStacks(t, iamKey)
	tls := 0
	valuesLanes := 0
	for _, f := range stacks {
		if f.listenerTLS {
			tls++
		}
		valuesLanes += len(hookURLLeaves(f.effective, nil))
	}
	if tls == 0 {
		t.Errorf("ни один стек дерева не объявил слушатель хуков под TLS — предикат " +
			"hooksListenerTLS перестал узнавать ручку либо посадка ушла из дерева")
	}
	if valuesLanes == 0 {
		t.Errorf("в деревьях значений стеков не найдено ни одной полосы — предикат " +
			"обхода значений перестал узнавать адреса")
	}
}
