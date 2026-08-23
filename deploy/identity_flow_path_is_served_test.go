// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_flow_path_is_served_test.go — адрес, который служба личности выдаёт
// браузеру, обязан быть адресом, который раздача консоли умеет обслужить.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Об одном предмете говорят ДВА места, и они разошлись. Служба личности
// объявляет браузерные адреса потоков (`ui_url:` в чартах), раздача консоли
// объявляет, какие пути она обслуживает (регулярка полосы потоков в
// `ui-future/deploy/templates/configmap-nginx.yaml`). Совпадение между ними не
// проверяло НИЧТО: обе половины покрыты своими пробами, а согласие половин —
// ничем.
//
// Цена расхождения измерена на боевом стенде 2026-08-23. Раздача обслуживает
// потоки в КОРНЕ и проксирует их отдельной службе Ory; путь с лишним сегментом
// в эту регулярку не попадает и уезжает в SPA-заглушку `location /`, которая
// отвечает `200` с `index.html`. То есть отказ выглядит как УСПЕХ: браузер
// получает двухсотый код и пустую оболочку консоли, маршрутизатор которой такого
// пути не знает и уводит на `/dashboard`. Ни `404`, ни ошибки в журнале — только
// «регистрация не открывается».
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕЙ АДРЕС ВЕРЕН — РЕШЕНО ЗАМЕРОМ, А НЕ ВКУСОМ
//
// Верно то место, которое ОТВЕЧАЕТ ПОЛЬЗОВАТЕЛЮ. На боевом стенде проверено
// тремя наблюдениями: корневой `/registration` отдаёт `303` в поток Ory; поток
// входа уводит браузер на корневой `/login?flow=…`; путь с лишним сегментом
// отдаёт `200` и оболочку. Значит объявления обязаны выводить КОРЕНЬ, а не
// раздача обязана учиться лишнему сегменту.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЧИТАЕТ ГЕЙТ И ЧЕГО НЕ ЧИТАЕТ
//
// Только ОБЪЯВЛЕНИЯ — регулярку раздачи, каждое `ui_url:` под `deploy/helm` и
// значения профилей, — поэтому ему не нужны ни helm, ни кластер, и он не умеет
// пропуститься. Ни один перечень путей здесь не выписан: множество
// обслуживаемых сегментов ВЫВОДИТСЯ из самой регулярки, поэтому расширение
// раздачи не требует правки гейта, а сужение немедленно делает его строже.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПУТЬ, НАЧАЛО КОТОРОГО ВЫЧИСЛЯЕТ ШАБЛОН
//
// Прежде такая форма (`{{ $flow }}/registration`) объявлялась НЕРАЗРЕШИМОЙ и
// роняла прогон. Отказ был честным — «не установили» обязано быть отличимо от
// «установили, что верно», — но после того как настройки службы личности свелись
// к одному шаблону с ручкой префикса (#904), неразрешимой стала ВСЯ полоса: гейт
// перестал измерять ровно то, ради чего заведён.
//
// Поэтому гейт научен читать вычисляемое начало пути. Порядок такой:
//
//  1. сперва объявление судится КАК ЛИТЕРАЛ — `adjudicateFlowURL`, ровно как
//     прежде. Шаблон внутри схемы и власти адреса (`https://{{ … }}/login`)
//     этой ветке не мешает и разрешения по значениям не требует;
//  2. и ТОЛЬКО если литеральная сторона ответила «неразрешимо», выражения
//     `{{ … }}` подставляются значениями и получившийся ЛИТЕРАЛ судится ТОЙ ЖЕ
//     функцией. Второго судьи не заводится — копия разошлась бы молча.
//
// Разрешение идёт ПО КАЖДОМУ ИСТОЧНИКУ ЗНАЧЕНИЙ отдельно, потому что ответ от
// источника и зависит: одна и та же строка шаблона даёт корень в одном профиле
// и лишний сегмент в другом. Источники ВЫВОДЯТСЯ (см. `flowValuesSources`), а не
// перечисляются, и находка называет ИМЯ ФАЙЛА источника — иначе непонятно, что
// править.
//
// Разбор выражений намеренно УЗОК: понятны только те формы, что есть в дереве
// (строковый литерал · `.Values.a.b` · `$var` и `$var.поле` · `X | default Y` ·
// `X | toString` · `printf` из одних `%s`/`%v`). Всё прочее — и ручка, которой
// нет ни в профиле, ни в умолчании, — даёт «неразрешимо», то есть КРАСНОЕ.
// Это не строгость ради строгости: разбор, молча пропускающий непонятую форму,
// имел бы дыру ровно там, где живёт дефект. Второй движок шаблонов здесь не
// строится — непонятое отвергается, а не угадывается.
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

// nginxServingChart — раздача консоли. Единственный источник того, какие пути
// потоков обслуживаются; перечень отсюда ВЫВОДИТСЯ, а не выписывается рядом.
const nginxServingChart = "../ui-future/deploy/templates/configmap-nginx.yaml"

// helmDeclarationsRoot — где ищутся объявления браузерных адресов потоков.
const helmDeclarationsRoot = "helm"

// flowBaseValuesPath — умолчание чарта: значения, поверх которых ложится любой
// профиль умбреллы. Ручка, которую профиль не объявил, берётся отсюда.
const flowBaseValuesPath = umbrellaDir + "/values.yaml"

// flowLocationRe — полоса потоков в раздаче: `location ~ ^/(a|b|c)(/|$)`.
// Захватывается сама альтернатива, поэтому множество сегментов есть функция
// дерева, а не константа этого файла.
var flowLocationRe = regexp.MustCompile(`location\s+~\s+\^/\(([a-z0-9|_-]+)\)\(/\|\$\)`)

// uiURLRe — объявление браузерного адреса потока в чарте.
var uiURLRe = regexp.MustCompile(`(?m)^\s*ui_url:\s*(.+?)\s*$`)

// templateVarDefRe — определение шаблонной переменной: `{{- $имя := тело -}}`.
// Отсюда берётся, ЧТО именно вычисляет начало пути; знание не выписывается
// рядом с гейтом, иначе переименование ручки в шаблоне разошлось бы с ним молча.
var templateVarDefRe = regexp.MustCompile(`(?m)^\s*\{\{-?\s*\$([A-Za-z_][A-Za-z0-9_]*)\s*:=\s*(.*?)\s*-?\}\}\s*$`)

// templateActionRe — одно выражение `{{ … }}` внутри объявленного адреса.
var templateActionRe = regexp.MustCompile(`\{\{-?\s*(.*?)\s*-?\}\}`)

// servedFlowSegments — множество первых сегментов пути, которые раздача
// обслуживает как поток личности.
func servedFlowSegments(t *testing.T) map[string]bool {
	t.Helper()
	b, err := os.ReadFile(nginxServingChart)
	if err != nil {
		t.Fatalf("раздача консоли %s не читается (%v) — предпосылка гейта исчезла", nginxServingChart, err)
	}
	m := flowLocationRe.FindSubmatch(b)
	if m == nil {
		t.Fatalf("в %s не найдена полоса потоков вида `location ~ ^/(…)(/|$)`. "+
			"Либо раздача перестала обслуживать потоки, либо изменилась её форма — "+
			"в обоих случаях согласие с объявлениями больше не установлено", nginxServingChart)
	}
	out := map[string]bool{}
	for _, seg := range strings.Split(string(m[1]), "|") {
		if seg = strings.TrimSpace(seg); seg != "" {
			out[seg] = true
		}
	}
	if len(out) == 0 {
		t.Fatal("полоса потоков найдена, но не назвала ни одного сегмента — сверять не с чем")
	}
	return out
}

// flowPathOf — путь, который объявление кладёт в адресную строку браузера.
//
// Возвращает (путь, ok). ok=false означает НЕРАЗРЕШИМО: начало пути вычисляет
// шаблон, и какой сегмент окажется первым — отсюда не видно.
func flowPathOf(raw string) (string, bool) {
	v := strings.TrimSpace(raw)
	v = strings.Trim(v, `"'`)
	if i := strings.Index(v, "#"); i >= 0 && !strings.Contains(v[:i], "{{") {
		v = strings.TrimSpace(v[:i])
	}
	// Абсолютный адрес: снять схему и власть (в них шаблон законен — он задаёт хост).
	for _, scheme := range []string{"https://", "http://"} {
		if strings.HasPrefix(v, scheme) {
			rest := v[len(scheme):]
			if i := firstSlashOutsideTemplate(rest); i >= 0 {
				v = rest[i:]
			} else {
				return "", false // адрес без пути — первого сегмента не существует
			}
			break
		}
	}
	// Путь обязан быть literal: шаблон в нём делает первый сегмент неизвестным.
	if strings.Contains(v, "{{") || !strings.HasPrefix(v, "/") {
		return "", false
	}
	return v, true
}

// firstSlashOutsideTemplate — индекс первого `/`, не попавшего внутрь `{{ … }}`.
func firstSlashOutsideTemplate(s string) int {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch {
		case strings.HasPrefix(s[i:], "{{"):
			depth++
			i++
		case strings.HasPrefix(s[i:], "}}"):
			if depth > 0 {
				depth--
			}
			i++
		case s[i] == '/' && depth == 0:
			return i
		}
	}
	return -1
}

// firstSegment — первый сегмент литерального пути.
func firstSegment(path string) string {
	return strings.SplitN(strings.TrimPrefix(path, "/"), "/", 2)[0]
}

// adjudicateFlowURL — вердикт по одному ЛИТЕРАЛЬНОМУ объявлению. Одна функция
// на гейт, на разрешение по значениям и на инъекцию: копия разошлась бы с
// оригиналом молча.
//
// Исходы: "обслуживается" | "не обслуживается" | "неразрешимо".
func adjudicateFlowURL(raw string, served map[string]bool) string {
	path, ok := flowPathOf(raw)
	if !ok {
		return "неразрешимо"
	}
	if served[firstSegment(path)] {
		return "обслуживается"
	}
	return "не обслуживается"
}

// ─────────────────────────────────────────────────────────────────────────────
// РАЗРЕШЕНИЕ ВЫЧИСЛЯЕМОГО НАЧАЛА ПУТИ
//
// Ниже — разбор ровно тех форм, что есть в дереве. Всё непонятое отвергается с
// названной причиной и роняет прогон; ничего не угадывается.
// ─────────────────────────────────────────────────────────────────────────────

// flowValuesSource — один источник значений: умолчание чарта либо профиль,
// который ложится поверх него. Ручка, профилем не объявленная, берётся из
// умолчания — это семантика `helm -f`, а не наше упрощение.
type flowValuesSource struct {
	name string          // путь файла: им называется источник в тексте находки
	own  map[string]any  // что объявляет сам файл
	base map[string]any  // умолчание под ним; nil у самого умолчания
	seen map[string]bool // если не nil — сюда пишутся ПРОЧИТАННЫЕ пути ручек
}

// lookup — величина ручки при этом источнике.
func (s flowValuesSource) lookup(path []string) (string, bool) {
	if s.seen != nil {
		s.seen[strings.Join(path, ".")] = true
	}
	if v, ok := digValuesLeaf(s.own, path); ok {
		return v, true
	}
	if s.base != nil {
		if v, ok := digValuesLeaf(s.base, path); ok {
			return v, true
		}
	}
	return "", false
}

// digValuesLeaf — лист дерева значений как строка. Узел-отображение листом не
// является: подставлять его в адрес нечем.
func digValuesLeaf(tree map[string]any, path []string) (string, bool) {
	var cur any = tree
	for _, k := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return "", false
		}
		cur, ok = m[k]
		if !ok {
			return "", false
		}
	}
	if cur == nil {
		return "", true // объявлено пустым — это ЗНАЧЕНИЕ, а не отсутствие
	}
	if _, isMap := cur.(map[string]any); isMap {
		return "", false
	}
	return fmt.Sprintf("%v", cur), true
}

// declaresAnyOf — объявляет ли файл хоть одну из названных ручек.
func declaresAnyOf(tree map[string]any, knobs []string) bool {
	for _, k := range knobs {
		if _, ok := digValuesLeaf(tree, strings.Split(k, ".")); ok {
			return true
		}
	}
	return false
}

const flowEvalMaxDepth = 8

// evalTemplateExpr — величина шаблонного выражения при данном источнике.
// Возвращает (величина, причина отказа, ok).
func evalTemplateExpr(expr string, vars map[string]string, src flowValuesSource, depth int) (string, string, bool) {
	if depth > flowEvalMaxDepth {
		return "", fmt.Sprintf("разбор %q ушёл глубже %d — похоже на кольцо определений", expr, flowEvalMaxDepth), false
	}
	e := stripOuterParens(strings.TrimSpace(expr))
	if e == "" {
		return "", "пустое выражение", false
	}
	parts := splitTopLevel(e, '|')
	val, why, ok := evalTemplateTerm(strings.TrimSpace(parts[0]), vars, src, depth)
	if !ok {
		return "", why, false
	}
	for _, raw := range parts[1:] {
		p := strings.TrimSpace(raw)
		switch {
		case p == "toString":
		case strings.HasPrefix(p, "default "):
			if val == "" {
				val, why, ok = evalTemplateExpr(strings.TrimSpace(p[len("default "):]), vars, src, depth+1)
				if !ok {
					return "", why, false
				}
			}
		default:
			return "", fmt.Sprintf("функция %q разбору не поддаётся", p), false
		}
	}
	return val, "", true
}

// evalTemplateTerm — величина одного звена до первой трубы.
func evalTemplateTerm(term string, vars map[string]string, src flowValuesSource, depth int) (string, string, bool) {
	t := stripOuterParens(strings.TrimSpace(term))
	switch {
	case len(t) >= 2 && strings.HasPrefix(t, `"`) && strings.HasSuffix(t, `"`):
		return t[1 : len(t)-1], "", true
	case strings.HasPrefix(t, "printf "):
		args := splitArgsTopLevel(strings.TrimSpace(t[len("printf "):]))
		if len(args) == 0 {
			return "", "printf без образца", false
		}
		format, why, ok := evalTemplateTerm(args[0], vars, src, depth+1)
		if !ok {
			return "", why, false
		}
		bare := strings.ReplaceAll(strings.ReplaceAll(format, "%s", "\x00"), "%v", "\x00")
		if strings.Contains(bare, "%") {
			return "", fmt.Sprintf("образец printf %q содержит глагол сложнее %%s/%%v", format), false
		}
		if n := strings.Count(bare, "\x00"); n != len(args)-1 {
			return "", fmt.Sprintf("printf: образец %q ждёт %d значение(й), передано %d", format, n, len(args)-1), false
		}
		out := format
		for _, a := range args[1:] {
			v, why, ok := evalTemplateExpr(a, vars, src, depth+1)
			if !ok {
				return "", why, false
			}
			out = strings.Replace(strings.Replace(out, "%s", v, 1), "%v", v, 1)
		}
		return out, "", true
	case strings.HasPrefix(t, ".Values.") || strings.HasPrefix(t, "$"):
		return evalTemplateRef(t, vars, src, depth)
	}
	return "", fmt.Sprintf("выражение %q не относится ни к одной разбираемой форме", t), false
}

// evalTemplateRef — величина ссылки на ручку либо на переменную.
func evalTemplateRef(ref string, vars map[string]string, src flowValuesSource, depth int) (string, string, bool) {
	if strings.HasPrefix(ref, "$") {
		name, fields := splitVarRef(ref)
		body, ok := vars[name]
		if !ok {
			return "", fmt.Sprintf("переменная $%s в этом файле не определена", name), false
		}
		if len(fields) == 0 {
			return evalTemplateExpr(body, vars, src, depth+1)
		}
		base, why, ok := valuesPathOf(body, vars, depth+1)
		if !ok {
			return "", why, false
		}
		return lookupOrExplain(append(append([]string{}, base...), fields...), src)
	}
	return lookupOrExplain(strings.Split(strings.TrimPrefix(ref, ".Values."), "."), src)
}

// lookupOrExplain — величина ручки либо причина, по которой её нет.
func lookupOrExplain(path []string, src flowValuesSource) (string, string, bool) {
	if v, ok := src.lookup(path); ok {
		return v, "", true
	}
	return "", fmt.Sprintf("ручка .Values.%s не объявлена ни профилем %s, ни умолчанием чарта",
		strings.Join(path, "."), src.name), false
}

// valuesPathOf — путь к ручке, который обозначает выражение. Нужен там, где к
// переменной обращаются полем (`$id.flowPathPrefix`).
func valuesPathOf(expr string, vars map[string]string, depth int) ([]string, string, bool) {
	if depth > flowEvalMaxDepth {
		return nil, fmt.Sprintf("разбор пути %q ушёл глубже %d", expr, flowEvalMaxDepth), false
	}
	e := stripOuterParens(strings.TrimSpace(expr))
	if strings.HasPrefix(e, ".Values.") {
		return strings.Split(strings.TrimPrefix(e, ".Values."), "."), "", true
	}
	if strings.HasPrefix(e, "$") {
		name, fields := splitVarRef(e)
		body, ok := vars[name]
		if !ok {
			return nil, fmt.Sprintf("переменная $%s в этом файле не определена", name), false
		}
		base, why, ok := valuesPathOf(body, vars, depth+1)
		if !ok {
			return nil, why, false
		}
		return append(append([]string{}, base...), fields...), "", true
	}
	return nil, fmt.Sprintf("%q не обозначает путь к ручке, а к нему обращаются полем", e), false
}

// splitVarRef — `$id.flowPathPrefix` → ("id", ["flowPathPrefix"]).
func splitVarRef(ref string) (string, []string) {
	parts := strings.Split(strings.TrimPrefix(ref, "$"), ".")
	return parts[0], parts[1:]
}

// stripOuterParens — снять внешние скобки, если они охватывают всё выражение.
func stripOuterParens(s string) string {
	for {
		if !strings.HasPrefix(s, "(") || !strings.HasSuffix(s, ")") {
			return s
		}
		depth, inQ, wraps := 0, false, false
		for i := 0; i < len(s); i++ {
			switch {
			case s[i] == '"':
				inQ = !inQ
			case inQ:
			case s[i] == '(':
				depth++
			case s[i] == ')':
				depth--
				if depth == 0 {
					wraps = i == len(s)-1
				}
			}
		}
		if !wraps {
			return s
		}
		s = strings.TrimSpace(s[1 : len(s)-1])
	}
}

// splitTopLevel — разрез по разделителю вне скобок и вне кавычек.
func splitTopLevel(s string, sep byte) []string {
	var out []string
	depth, inQ, start := 0, false, 0
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '"':
			inQ = !inQ
		case inQ:
		case s[i] == '(':
			depth++
		case s[i] == ')':
			depth--
		case s[i] == sep && depth == 0:
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

// splitArgsTopLevel — разрез на аргументы по пробелам вне скобок и кавычек.
func splitArgsTopLevel(s string) []string {
	var out []string
	var cur strings.Builder
	depth, inQ := 0, false
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			inQ = !inQ
			cur.WriteByte(c)
		case inQ:
			cur.WriteByte(c)
		case c == '(':
			depth++
			cur.WriteByte(c)
		case c == ')':
			depth--
			cur.WriteByte(c)
		case (c == ' ' || c == '\t') && depth == 0:
			flush()
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return out
}

// templateVarsOf — определения шаблонных переменных файла.
func templateVarsOf(body string) map[string]string {
	out := map[string]string{}
	for _, m := range templateVarDefRe.FindAllStringSubmatch(body, -1) {
		out[m[1]] = m[2]
	}
	return out
}

// resolveTemplatedURL — объявление с подставленными значениями, то есть ЛИТЕРАЛ.
func resolveTemplatedURL(raw string, vars map[string]string, src flowValuesSource) (string, string, bool) {
	v := strings.Trim(strings.TrimSpace(raw), `"'`)
	why, ok := "", true
	out := templateActionRe.ReplaceAllStringFunc(v, func(m string) string {
		expr := templateActionRe.FindStringSubmatch(m)[1]
		val, reason, o := evalTemplateExpr(expr, vars, src, 0)
		if !o {
			if ok {
				why = reason
			}
			ok = false
			return ""
		}
		return val
	})
	if !ok {
		return "", why, false
	}
	return out, "", true
}

// adjudicateTemplatedDecl — находки по ОДНОМУ объявлению с вычисляемым началом
// пути, сразу по всем источникам значений.
//
// Одна функция на гейт и на инъекцию: копия разошлась бы с оригиналом молча, и
// доказанной оказалась бы копия, а не то, что исполняется.
//
// Находка НАЗЫВАЕТ ИСТОЧНИК. Без имени файла она сообщает, что где-то в дереве
// неверно, — и починить по ней нельзя: ручку объявляют и умолчание, и профили,
// а величина у каждого своя. Источники с одинаковым исходом сводятся в одну
// строку: перечень из десяти одинаковых находок перестают читать.
func adjudicateTemplatedDecl(d flowDecl, sources []flowValuesSource, served map[string]bool) []string {
	bad := map[string][]string{}     // первый сегмент → источники
	unclear := map[string][]string{} // причина → источники

	for _, s := range sources {
		url, why, ok := resolveTemplatedURL(d.raw, d.vars, s)
		if !ok {
			unclear[why] = append(unclear[why], s.name)
			continue
		}
		switch adjudicateFlowURL(url, served) {
		case "обслуживается":
		case "не обслуживается":
			path, _ := flowPathOf(url)
			seg := firstSegment(path)
			bad[seg] = append(bad[seg], s.name)
		case "неразрешимо":
			// Значения подставились, а путь всё равно не читается: адрес без
			// пути, относительная форма без ведущей косой черты и подобное.
			why := fmt.Sprintf("подставленные значения дали %q — это не путь", url)
			unclear[why] = append(unclear[why], s.name)
		}
	}

	var out []string
	for seg, srcs := range bad {
		sort.Strings(srcs)
		out = append(out, fmt.Sprintf(
			"%s: объявлен %q → при значениях %s первый сегмент пути %q, а раздача консоли его НЕ обслуживает",
			d.file, d.raw, strings.Join(srcs, ", "), seg))
	}
	for why, srcs := range unclear {
		sort.Strings(srcs)
		out = append(out, fmt.Sprintf(
			"%s: объявлен %q → при значениях %s начало пути не разрешилось: %s. "+
				"Согласие с раздачей НЕ УСТАНОВЛЕНО",
			d.file, d.raw, strings.Join(srcs, ", "), why))
	}
	sort.Strings(out)
	return out
}

// flowDecl — объявление, начало пути которого вычисляет шаблон.
type flowDecl struct {
	file string
	raw  string
	vars map[string]string
}

// flowValuesFiles — все файлы значений дерева. Перечень ВЫВОДИТСЯ обходом:
// выписанный рядом список разошёлся бы с деревом молча, и новый профиль остался
// бы неосмотренным.
func flowValuesFiles(t *testing.T) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(helmDeclarationsRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := filepath.Base(path)
		if strings.HasPrefix(name, "values") && (strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml")) {
			out = append(out, filepath.ToSlash(path))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("обход %s за файлами значений прерван: %v", helmDeclarationsRoot, err)
	}
	sort.Strings(out)
	return out
}

// flowValuesSources — источники, при которых объявления разрешаются.
//
// Их два вида, и оба выведены, а не выписаны: умолчание чарта (`values.yaml`
// умбреллы) и КАЖДЫЙ файл значений, объявляющий хоть одну из ручек, которые
// шаблон читает при вычислении начала пути. Файл, не объявляющий ни одной, для
// этого вопроса тождествен умолчанию — оно уже в списке, и второе имя рядом
// добавляло бы имён, а не сведений.
func flowValuesSources(t *testing.T, valuesFiles []string, base map[string]any, knobs []string) []flowValuesSource {
	t.Helper()
	out := []flowValuesSource{{name: flowBaseValuesPath, own: base}}
	for _, f := range valuesFiles {
		if f == flowBaseValuesPath {
			continue
		}
		tree := readYAML(t, f)
		if declaresAnyOf(tree, knobs) {
			out = append(out, flowValuesSource{name: f, own: tree, base: base})
		}
	}
	return out
}

// TestIdentityFlowPathIsServedByTheConsole — каждый браузерный адрес потока,
// который объявляет чарт, раздача консоли обслуживает — при КАЖДОМ источнике
// значений, а не только при умолчании.
func TestIdentityFlowPathIsServedByTheConsole(t *testing.T) {
	served := servedFlowSegments(t)

	// ── 1. объявления: литеральная сторона судится сразу ──────────────────
	var files, decls int
	var findings []string
	var templated []flowDecl

	err := filepath.WalkDir(helmDeclarationsRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if ext := filepath.Ext(path); ext != ".yaml" && ext != ".yml" && ext != ".tpl" {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		body := string(b)
		if !strings.Contains(body, "ui_url:") {
			return nil
		}
		files++
		vars := templateVarsOf(body)
		for _, m := range uiURLRe.FindAllStringSubmatch(body, -1) {
			decls++
			switch adjudicateFlowURL(m[1], served) {
			case "обслуживается":
			case "не обслуживается":
				p, _ := flowPathOf(m[1])
				findings = append(findings, fmt.Sprintf(
					"%s: объявлен %q → первый сегмент пути %q, а раздача консоли его НЕ обслуживает",
					filepath.ToSlash(path), strings.TrimSpace(m[1]), firstSegment(p)))
			case "неразрешимо":
				templated = append(templated, flowDecl{filepath.ToSlash(path), strings.TrimSpace(m[1]), vars})
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("обход %s прерван: %v", helmDeclarationsRoot, err)
	}

	// ── 2. что читает шаблон, вычисляя начало пути ────────────────────────
	valuesFiles := flowValuesFiles(t)
	var base map[string]any
	var knobs []string
	var sources []flowValuesSource
	if len(templated) > 0 {
		base = readYAML(t, flowBaseValuesPath)
		seen := map[string]bool{}
		probe := flowValuesSource{name: flowBaseValuesPath, own: base, seen: seen}
		for _, d := range templated {
			_, _, _ = resolveTemplatedURL(d.raw, d.vars, probe) // важен СЛЕД, а не исход
		}
		for k := range seen {
			knobs = append(knobs, k)
		}
		sort.Strings(knobs)
		sources = flowValuesSources(t, valuesFiles, base, knobs)
	}

	// ── 3. вычисляемое начало пути — по каждому источнику отдельно ────
	for _, d := range templated {
		findings = append(findings, adjudicateTemplatedDecl(d, sources, served)...)
	}

	// ── 4. перепись: «ноль находок» обязано быть отличимо от «ноль прочитанного» ──
	segs := make([]string, 0, len(served))
	for s := range served {
		segs = append(segs, s)
	}
	sort.Strings(segs)
	srcNames := make([]string, 0, len(sources))
	for _, s := range sources {
		srcNames = append(srcNames, s.name)
	}
	t.Logf("перепись: раздача обслуживает %d сегмент(ов) [%s]; прочитано файлов с объявлениями %d; "+
		"объявлений %d, из них с вычисляемым началом пути %d; файлов значений прочитано %d, "+
		"источниками стали %d [%s]; ручек, которые шаблон читает, %d [%s]; находок %d",
		len(segs), strings.Join(segs, " "), files, decls, len(templated),
		len(valuesFiles), len(sources), strings.Join(srcNames, " "),
		len(knobs), strings.Join(knobs, " "), len(findings))

	if decls == 0 {
		t.Fatalf("под %s не найдено ни одного объявления `ui_url:` — гейту нечего было сверять, "+
			"и «ноль находок» здесь означало бы «ноль прочитанного»", helmDeclarationsRoot)
	}
	if len(templated) > 0 && len(valuesFiles) == 0 {
		t.Fatalf("обход %s не нашёл НИ ОДНОГО файла значений, хотя %d объявлени(й) вычисляют начало "+
			"пути — разрешать не по чему, и «ноль находок» означало бы «ноль прочитанного»",
			helmDeclarationsRoot, len(templated))
	}
	if len(templated) > 0 && len(sources) == 0 {
		t.Fatalf("%d объявлени(й) вычисляют начало пути, но источников значений не нашлось ни одного — "+
			"разрешать нечем, и «ноль находок» означало бы «ноль прочитанного»", len(templated))
	}
	if len(templated) > 0 && len(knobs) == 0 {
		t.Fatalf("%d объявлени(й) вычисляют начало пути, но разбор не прочёл НИ ОДНОЙ ручки — "+
			"значит он не разбирает шаблон, а молчит о нём", len(templated))
	}
	if len(findings) > 0 {
		sort.Strings(findings)
		t.Fatalf("адрес(а), которые служба личности выдаёт браузеру, раздача консоли не обслуживает:\n  %s",
			strings.Join(findings, "\n  "))
	}
}
