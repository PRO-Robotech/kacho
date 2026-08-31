// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_callback_credential_source_test.go — величина обратного вызова у
// ОТПРАВИТЕЛЯ и у ПРОВЕРЯЮЩЕЙ стороны обязана приходить из ОДНОЙ координаты
// секрета, и это обязано быть свойством дерева, а не совпадением.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Полосу обратного вызова исполняют два разных пода. Отправитель (провайдер
// личности и провайдер токенов) кладёт величину в заголовок; проверяющая сторона
// (служба прав) сверяет её с тем, что прочитала сама. Величина у них общая — и
// ровно поэтому координата секрета, из которой каждая сторона её берёт, обязана
// быть ОДНОЙ.
//
// Сегодня она одна СЛУЧАЙНО: у отправителя имя секрета стоит ЛИТЕРАЛОМ, у
// проверяющей стороны приходит ПАРАМЕТРОМ (`kacho.hooks.tokenHookSecretName`).
// Профиль, переопределивший параметр, разводит стороны МОЛЧА: рендер зелёный,
// поды стартуют, обратные вызовы отвергаются `401`, край переводит это
// арендатору в общий отказ, и ни одна проверка не краснеет — обе стороны валидны
// по отдельности, неверна их РАЗНИЦА.
//
// Это тот же класс, что «параллельные полосы одного механизма обязаны сверяться
// МЕЖДУ СОБОЙ» (`architecture.md`): проверка каждой стороны отдельно требует
// знать, какой величина ДОЛЖНА быть, — а это и есть спорный вопрос. Сверка
// сторон спрашивает другое: решал ли кто-нибудь, что они различаются.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ГЕЙТ, А НЕ ЕДИНЫЙ ИСТОЧНИК
//
// Единый источник — предпочтительный исход, и он здесь недостижим ЦЕЛИКОМ, а не
// по лени. Стороны рендерятся в РАЗНЫХ контекстах значений: проверяющая — в
// контексте нашего подчарта (`.Values.kacho.hooks.*` виден), отправитель —
// в контексте подчарта ПРОВАЙДЕРА, куда из наших значений доходит только
// `.Values.global`. Свести их одним ключом значило бы переселить объявление в
// `global` и оставить у подчарта его собственное умолчание — то есть вернуть те
// же два места об одном предмете, только под другими именами. Отдельно: часть
// объявлений отправителя живёт в ПРОФИЛЯХ, статическим YAML, и превращается в
// шаблон только если чужой чарт прогоняет эту ручку через `tpl`, — свойство
// чужого чарта, которое здесь нечем измерить.
//
// Поэтому: расхождение остаётся ВЫРАЗИМЫМ, но перестаёт быть ТИХИМ.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ОБЪЯВЛЕНИЯ, А НЕ РЕНДЕР
//
// Та же причина, что у соседей (posture_parity_test.go, dbtls_declaration_test.go):
// рендер требует `helm` и скачанных зависимостей, поэтому проверка над рендером
// умеет пропуститься, а пропущенная проверка не краснеет никогда.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ИМЕНА ПЕРЕМЕННЫХ ВЫВОДЯТСЯ, А НЕ ВЫПИСАНЫ
//
// Выписанный перечень разошёлся бы при заведении следующей полосы МОЛЧА: новая
// переменная просто не попала бы под сверку. Имена берутся из двух мест, где
// полоса вообще объявляется:
//
//   - конфигурация службы личности — ссылка `${ИМЯ}` в блоке `auth:` того хука,
//     чей адрес несёт маршрут обратных вызовов;
//   - страж величины обратного вызова — словарь полос, который он сам и держит.
//
// Ни одно имя переменной, ни одно имя секрета здесь не выписано.
//
// ─────────────────────────────────────────────────────────────────────────────
// ГРАНИЦА ПРЕДМЕТА
//
//   - Проверяется КООРДИНАТА (имя секрета + ключ), а не его СОДЕРЖИМОЕ: что в
//     секрете лежит и непуст ли он — рантайм, и это предмет предполёта раскатки.
//   - Проверяется СОГЛАСИЕ сторон, а не решение стенда полосу включить. Стек, ни
//     одной стороны не объявляющий, приходит сюда законным.
//   - СОСЕД С ПОХОЖИМ ИМЕНЕМ СУДИТ ДРУГОЕ, и это надо сказать вслух, иначе
//     следующий сочтёт их двумя местами об одном предмете.
//     deploy/tests/helm/identity-callback-credential-source-test.sh спрашивает,
//     ЕСТЬ ЛИ у величины источник в поде (переменная из секрета) — по каждой
//     стороне ОТДЕЛЬНО и по рендеру. Здесь спрашивается другое: ОДИН ЛИ секрет
//     у сторон. Проверка каждой стороны отдельно этого вопроса не задаёт by
//     construction: обе стороны валидны порознь, неверна их разница.
//   - `.tpl`, рендерящийся в контексте ЧУЖОГО подчарта, обязан нести литерал:
//     путь `.Values` там указывает в чужое дерево значений, и доказать согласие
//     нечем. Такой путь — находка, а не молчание.
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

// callbackRoute — маршрут обратных вызовов. Он же делает ссылку в конфигурации
// УЧЁТНЫМИ ДАННЫМИ ПОЛОСЫ, а не просто ссылкой: рядом в той же карте стоят
// ссылки другой природы (секреты печенья и шифра), у которых источник другой.
const callbackRoute = "/iam/v1/hooks/"

// dollarRef — ссылка формы `${ИМЯ}`.
var dollarRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// laneDictEnvName — имя переменной в словаре полос стража. Словарь — его
// собственное объявление, поэтому имена берутся оттуда, а не из головы.
var laneDictEnvName = regexp.MustCompile(`"([A-Z][A-Z0-9_]{3,})"`)

// envDeclLine — объявление переменной окружения списком.
var envDeclLine = regexp.MustCompile(`^(\s*)-\s*name:\s*([A-Za-z_][A-Za-z0-9_]*)\s*$`)

// scalarLine — `ключ: значение` внутри блока.
var scalarLine = regexp.MustCompile(`^(\s*)([a-zA-Z][a-zA-Z0-9_]*):\s*(\S.*?)\s*$`)

// valuesAction — действие шаблона, целиком состоящее из чтения значения.
var valuesAction = regexp.MustCompile(`^\{\{-?\s*\.Values\.([A-Za-z0-9_]+(?:\.[A-Za-z0-9_]+)*)\s*-?\}\}$`)

// ─────────────────────────────────────────────────────────────────────────────
// ФАКТЫ ДЕРЕВА

// credRef — ОДНО объявление источника величины полосы.
type credRef struct {
	file  string // путь относительно каталога deploy
	line  int
	env   string // имя переменной окружения
	name  string // выражение имени секрета — литерал либо действие
	key   string // выражение ключа секрета
	chart string // ключ подчарта, если файл — шаблон нашего подчарта; иначе ""
	// profile — имя файла профиля, если объявление сделано профилем. Такое
	// объявление действует только на стеки, чья цепочка этот профиль несёт.
	profile string
	// foreignCtx — файл рендерится в контексте ЧУЖОГО подчарта (именованный
	// шаблон, который зовут из значений провайдера). Путь `.Values` там
	// недоказуем.
	foreignCtx bool
}

func (r credRef) coord() string { return fmt.Sprintf("%s:%d", r.file, r.line) }

// laneEnvNames — имена переменных, которыми полоса обратного вызова несёт свою
// величину. Выводятся из дерева двумя независимыми признаками.
func laneEnvNames(t *testing.T) []string {
	t.Helper()
	set := map[string]bool{}

	tpls, err := filepath.Glob(filepath.Join(umbrellaDir, "charts", "*", "templates", "*"))
	if err != nil {
		t.Fatalf("обход шаблонов подчартов: %v", err)
	}
	for _, p := range tpls {
		b, rerr := os.ReadFile(p) // #nosec G304 -- путь получен обходом собственного дерева
		if rerr != nil {
			continue
		}
		for _, n := range refsAtCallbackAuth(string(b)) {
			set[n] = true
		}
	}

	guards, err := filepath.Glob(filepath.Join(umbrellaDir, "templates", "*.yaml"))
	if err != nil {
		t.Fatalf("обход шаблонов умбреллы: %v", err)
	}
	for _, p := range guards {
		b, rerr := os.ReadFile(p) // #nosec G304 -- путь получен обходом собственного дерева
		if rerr != nil {
			continue
		}
		for _, ln := range strings.Split(string(b), "\n") {
			if !strings.Contains(ln, "$lanes") || !strings.Contains(ln, "dict") {
				continue
			}
			for _, m := range laneDictEnvName.FindAllStringSubmatch(ln, -1) {
				set[m[1]] = true
			}
		}
	}

	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// refsAtCallbackAuth — ссылки `${ИМЯ}`, стоящие в блоке учётных данных хука,
// чей адрес несёт маршрут обратных вызовов. Признак — БЛИЗОСТЬ к адресу внутри
// одного объявления хука, а не совпадение имени: имя и есть то, что выводится.
func refsAtCallbackAuth(body string) []string {
	lines := strings.Split(body, "\n")
	var out []string
	for i, ln := range lines {
		if !strings.Contains(ln, callbackRoute) {
			continue
		}
		// Объявление хука кончается там, где начинается следующий элемент
		// списка того же уровня. Отступ берём по строке адреса.
		indent := len(ln) - len(strings.TrimLeft(ln, " "))
		for j := i + 1; j < len(lines); j++ {
			cur := lines[j]
			if strings.TrimSpace(cur) == "" {
				continue
			}
			ci := len(cur) - len(strings.TrimLeft(cur, " "))
			trimmed := strings.TrimSpace(cur)
			if ci < indent || (ci == indent && strings.HasPrefix(trimmed, "- ")) {
				break
			}
			for _, m := range dollarRef.FindAllStringSubmatch(cur, -1) {
				out = append(out, m[1])
			}
		}
	}
	return out
}

// credRefsInFile — объявления источника величины для названных переменных.
// Разбор строковый: значение бывает действием шаблона, и YAML такой файл не
// читает by construction.
func credRefsInFile(rel, body string, lanes map[string]bool) []credRef {
	lines := strings.Split(body, "\n")
	var out []credRef
	for i, ln := range lines {
		m := envDeclLine.FindStringSubmatch(ln)
		if m == nil || !lanes[m[2]] {
			continue
		}
		indent := len(m[1])
		ref := credRef{file: rel, line: i + 1, env: m[2]}
		inSecretRef := false
		for j := i + 1; j < len(lines); j++ {
			cur := lines[j]
			if strings.TrimSpace(cur) == "" {
				continue
			}
			ci := len(cur) - len(strings.TrimLeft(cur, " "))
			if ci <= indent {
				break // объявление кончилось
			}
			if strings.TrimSpace(cur) == "secretKeyRef:" {
				inSecretRef = true
				continue
			}
			if !inSecretRef {
				continue
			}
			s := scalarLine.FindStringSubmatch(cur)
			if s == nil {
				continue
			}
			switch s[2] {
			case "name":
				ref.name = strings.Trim(s[3], `"'`)
			case "key":
				ref.key = strings.Trim(s[3], `"'`)
			}
		}
		if ref.name == "" && ref.key == "" {
			continue // переменная объявлена не секретом — не наш предмет
		}
		out = append(out, ref)
	}
	return out
}

// allCredRefs — все объявления источника величины полосы в дереве.
func allCredRefs(t *testing.T, lanes []string) []credRef {
	t.Helper()
	set := map[string]bool{}
	for _, n := range lanes {
		set[n] = true
	}

	var files []string
	for _, pat := range []string{
		filepath.Join(umbrellaDir, "charts", "*", "templates", "*"),
		filepath.Join(umbrellaDir, "templates", "*"),
		filepath.Join(umbrellaDir, "values*.yaml"),
	} {
		got, err := filepath.Glob(pat)
		if err != nil {
			t.Fatalf("обход %s: %v", pat, err)
		}
		files = append(files, got...)
	}
	sort.Strings(files)

	var out []credRef
	for _, p := range files {
		b, err := os.ReadFile(p) // #nosec G304 -- путь получен обходом собственного дерева
		if err != nil {
			t.Fatalf("чтение %s: %v", p, err)
		}
		refs := credRefsInFile(p, string(b), set)
		for i := range refs {
			rel := p
			switch {
			case strings.Contains(rel, filepath.Join(umbrellaDir, "charts")+string(filepath.Separator)):
				parts := strings.Split(filepath.ToSlash(rel), "/")
				for k, seg := range parts {
					if seg == "charts" && k+1 < len(parts) {
						refs[i].chart = parts[k+1]
						break
					}
				}
				// Именованный шаблон, который зовут ИЗ ЗНАЧЕНИЙ чужого
				// подчарта, рендерится в чужом контексте значений.
				refs[i].foreignCtx = strings.HasSuffix(rel, ".tpl")
			case strings.HasPrefix(filepath.Base(rel), "values"):
				refs[i].profile = filepath.Base(rel)
			}
		}
		out = append(out, refs...)
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// ЯДРО — чистая функция над фактами, чтобы самопроверка подавала ей
// синтетический вход, а не подделывала дерево.

// credFinding — расхождение координат величины на одном стеке.
type credFinding struct {
	stack  string
	reason string
	detail string
}

// resolveExpr — литерал возвращается как есть; действие `{{ .Values.<путь> }}`
// резолвится по дереву значений, доступному в контексте файла.
func resolveExpr(expr string, scope map[string]any, global map[string]any) (string, bool) {
	if !strings.Contains(expr, "{{") {
		return expr, true
	}
	m := valuesAction.FindStringSubmatch(strings.TrimSpace(expr))
	if m == nil {
		return "", false
	}
	path := strings.Split(m[1], ".")
	tree, from := scope, path
	if path[0] == "global" {
		tree, from = global, path[1:]
	}
	v, ok := lookup(tree, from...)
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// scanCredentialSources — сверяет координаты величины по каждому стеку.
func scanCredentialSources(
	stacks map[string][]string,
	refs []credRef,
	scopeOf func(stack, chart string) map[string]any,
	globalOf func(stack string) map[string]any,
) []credFinding {
	var out []credFinding
	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, st := range names {
		inChain := map[string]bool{}
		for _, p := range stacks[st] {
			inChain[p] = true
		}
		seen := map[string][]string{} // координата секрета → координаты объявлений
		for _, r := range refs {
			if r.profile != "" && !inChain[r.profile] {
				continue // этот профиль в цепочку стека не входит
			}
			if r.foreignCtx && (strings.Contains(r.name, "{{") || strings.Contains(r.key, "{{")) {
				out = append(out, credFinding{stack: st, reason: "путь значений в чужом контексте",
					detail: fmt.Sprintf("%s: переменная %s берёт координату выражением %q/%q, "+
						"а этот шаблон рендерится в дереве значений ЧУЖОГО подчарта — "+
						"доказать согласие сторон нечем", r.coord(), r.env, r.name, r.key)})
				continue
			}
			nm, okN := resolveExpr(r.name, scopeOf(st, r.chart), globalOf(st))
			ky, okK := resolveExpr(r.key, scopeOf(st, r.chart), globalOf(st))
			if !okN || !okK {
				out = append(out, credFinding{stack: st, reason: "координата не резолвится",
					detail: fmt.Sprintf("%s: переменная %s, имя %q, ключ %q — "+
						"значение по этому пути стек не объявляет", r.coord(), r.env, r.name, r.key)})
				continue
			}
			c := nm + "/" + ky
			seen[c] = append(seen[c], r.coord()+" ("+r.env+")")
		}
		if len(seen) <= 1 {
			continue
		}
		coords := make([]string, 0, len(seen))
		for c := range seen {
			coords = append(coords, c)
		}
		sort.Strings(coords)
		var parts []string
		for _, c := range coords {
			sort.Strings(seen[c])
			parts = append(parts, fmt.Sprintf("%s ← %s", c, strings.Join(seen[c], ", ")))
		}
		out = append(out, credFinding{stack: st, reason: "стороны держат РАЗНЫЕ координаты",
			detail: strings.Join(parts, " | ")})
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// ПРОВЕРКА ПО ДЕРЕВУ

func TestCallbackCredentialComesFromOneSecretCoordinate(t *testing.T) {
	lanes := laneEnvNames(t)
	if len(lanes) == 0 {
		t.Fatalf("имён переменных полосы обратного вызова не выведено ни одного — "+
			"либо маршрут %q сменился, либо словарь полос стража переписан; "+
			"«ноль находок» здесь неотличимо от «ноль прочитанного», поэтому это отказ",
			callbackRoute)
	}
	refs := allCredRefs(t, lanes)
	if len(refs) == 0 {
		t.Fatalf("объявлений источника величины не найдено ни одного при %d именах переменных — "+
			"обход слеп, а не дерево чисто", len(lanes))
	}

	chains := deployStacks(t)
	base := readYAML(t, filepath.Join(umbrellaDir, "values.yaml"))
	charts := subchartDirs(t)

	chartDefaults := map[string]map[string]any{}
	for key, dir := range charts {
		p := filepath.Join(dir, "values.yaml")
		if _, err := os.Stat(p); err == nil {
			chartDefaults[key] = readYAML(t, p)
		}
	}

	effective := map[string]map[string]any{}
	for st, chain := range chains {
		e := mergeValues(map[string]any{}, base)
		for _, p := range chain {
			e = mergeValues(e, readYAML(t, filepath.Join(umbrellaDir, p)))
		}
		effective[st] = e
	}

	scopeOf := func(stack, chart string) map[string]any {
		if chart == "" {
			return effective[stack]
		}
		s := mergeValues(map[string]any{}, chartDefaults[chart])
		if over, ok := effective[stack][chart].(map[string]any); ok {
			s = mergeValues(s, over)
		}
		return s
	}
	globalOf := func(stack string) map[string]any {
		g, _ := effective[stack]["global"].(map[string]any)
		return g
	}

	findings := scanCredentialSources(chains, refs, scopeOf, globalOf)

	literal, parametric := 0, 0
	for _, r := range refs {
		if strings.Contains(r.name, "{{") {
			parametric++
		} else {
			literal++
		}
	}
	t.Logf("осмотрено: стеков %d, имён переменных полосы %d (%s), объявлений источника %d "+
		"(литералом %d, параметром %d); расхождений %d",
		len(chains), len(lanes), strings.Join(lanes, ", "), len(refs), literal, parametric, len(findings))

	for _, f := range findings {
		t.Errorf("стек %s: %s — %s.\n"+
			"    Величина обратного вызова у отправителя и у проверяющей стороны обязана "+
			"приходить из ОДНОЙ координаты секрета: разойдясь, стороны продолжают стартовать, "+
			"а обратные вызовы отвергаются, и причина не названа ничем.", f.stack, f.reason, f.detail)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// САМОПРОВЕРКА — инъекция в обе стороны, на СИНТЕТИЧЕСКОМ входе.
//
// Дерево здесь не подделывается: ядро — чистая функция, и ей подаётся вход
// напрямую. Инъекция в настоящий профиль доказала бы то же самое, но стоила бы
// правки общего дерева.

func TestScanCredentialSources_SelfTest(t *testing.T) {
	scope := func(_, chart string) map[string]any {
		if chart == "" {
			return map[string]any{}
		}
		return map[string]any{"kacho": map[string]any{"hooks": map[string]any{
			"tokenHookSecretName": "секрет-владельца",
			"tokenHookSecretKey":  "token",
		}}}
	}
	global := func(string) map[string]any { return map[string]any{} }
	stacks := map[string][]string{"проба": {"values.проба.yaml"}}

	sender := credRef{file: "values.проба.yaml", line: 10, env: "SENDER",
		name: "секрет-владельца", key: "token", profile: "values.проба.yaml"}
	verifier := credRef{file: "charts/наш/templates/deployment.yaml", line: 20, env: "VERIFIER",
		name: "{{ .Values.kacho.hooks.tokenHookSecretName }}",
		key:  "{{ .Values.kacho.hooks.tokenHookSecretKey }}", chart: "наш"}

	// (0) КОНТРОЛЬ: стороны сходятся — молчание.
	if got := scanCredentialSources(stacks, []credRef{sender, verifier}, scope, global); len(got) != 0 {
		t.Errorf("(0) согласованные стороны обязаны молчать, получено: %+v", got)
	}

	// (A) ИНЪЕКЦИЯ: профиль переопределил параметр — стороны разошлись.
	diverged := func(_, chart string) map[string]any {
		if chart == "" {
			return map[string]any{}
		}
		return map[string]any{"kacho": map[string]any{"hooks": map[string]any{
			"tokenHookSecretName": "другой-секрет",
			"tokenHookSecretKey":  "token",
		}}}
	}
	got := scanCredentialSources(stacks, []credRef{sender, verifier}, diverged, global)
	switch {
	case len(got) == 0:
		t.Errorf("(A) расхождение координат ПРОПУЩЕНО — гейт вакуумен")
	case !strings.Contains(got[0].detail, "секрет-владельца") ||
		!strings.Contains(got[0].detail, "другой-секрет"):
		t.Errorf("(A) находка не называет ОБЕ величины: %s", got[0].detail)
	case !strings.Contains(got[0].detail, "values.проба.yaml:10"):
		t.Errorf("(A) находка не называет координату объявления: %s", got[0].detail)
	}

	// (B) КОНТРОЛЬ ТОЙ ЖЕ ФОРМЫ: профиль, которого в цепочке стека НЕТ, развести
	//     стороны не может — его объявление до этого стека не доезжает.
	other := sender
	other.profile = "values.другой.yaml"
	other.name = "третий-секрет"
	if got := scanCredentialSources(stacks, []credRef{other, verifier}, scope, global); len(got) != 0 {
		t.Errorf("(B) объявление вне цепочки стека обязано молчать, получено: %+v", got)
	}

	// (C) ИНЪЕКЦИЯ: путь значений в чужом контексте — недоказуемо, значит находка.
	foreign := credRef{file: "charts/наш/templates/_чужой.tpl", line: 5, env: "SENDER",
		name: "{{ .Values.kacho.hooks.tokenHookSecretName }}", key: "token",
		chart: "наш", foreignCtx: true}
	got = scanCredentialSources(stacks, []credRef{foreign, verifier}, scope, global)
	if len(got) == 0 || !strings.Contains(got[0].detail, "_чужой.tpl:5") {
		t.Errorf("(C) путь значений в чужом контексте ПРОПУЩЕН: %+v", got)
	}

	// (D) КОНТРОЛЬ: литерал в том же чужом контексте — законен и обязан молчать.
	foreignLiteral := foreign
	foreignLiteral.name = "секрет-владельца"
	if got := scanCredentialSources(stacks, []credRef{foreignLiteral, verifier}, scope, global); len(got) != 0 {
		t.Errorf("(D) литерал в чужом контексте обязан молчать, получено: %+v", got)
	}
}

// TestCredentialPredicates_RecogniseTheRealTree — предпосылки разбора верны
// ОТНОСИТЕЛЬНО НАСТОЯЩЕГО дерева, а не только синтетики. Без этого самопроверка
// выше доказывала бы работоспособность ядра на входе, которого не бывает.
func TestCredentialPredicates_RecogniseTheRealTree(t *testing.T) {
	lanes := laneEnvNames(t)
	if len(lanes) < 2 {
		t.Errorf("имён переменных полосы выведено %d — их в дереве больше одного "+
			"(конфигурация личности и словарь полос стража объявляют разные)", len(lanes))
	}
	refs := allCredRefs(t, lanes)
	var lit, par int
	for _, r := range refs {
		if strings.Contains(r.name, "{{") {
			par++
		} else {
			lit++
		}
	}
	if lit == 0 || par == 0 {
		t.Errorf("объявлений литералом %d, параметром %d — предмет сверки в том, что "+
			"в дереве есть ОБЕ формы; ноль в любой из них означает, что разбор ослеп", lit, par)
	}
	t.Logf("предпосылки: имён переменных %d, объявлений источника %d (литералом %d, параметром %d)",
		len(lanes), len(refs), lit, par)
}
