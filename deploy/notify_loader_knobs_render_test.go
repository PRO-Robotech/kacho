// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// notify_loader_knobs_render_test.go — каждая ручка загрузчика notify
// выводится рендером в каждой цепочке, где notify поднимается (NTF-4 Р16,
// DoD S1 п.5; ban #16 «values реально поднимается»).
//
// Страж старта notify отказывает на незаданной ручке («умолчания у неё нет»):
// ручка, которую загрузчик объявил, а чарт не вывел, — под в отказе старта на
// каждой установке. Так 21 ручка группы NTF-4 доехала до загрузчика без чарта,
// и стенд dev-prod встал на `Available: 0/2` (PR #3072). Держатель судит рендер,
// а не values: ручка «выведена», когда переменная видна контейнеру `notify` —
// ключом карты, которую он берёт `envFrom`, либо записью `env`.
//
// Перепись ручек — разбором загрузчика (go/parser), а не поиском по тексту:
// теги `envconfig` полей `Config` плюс ручки, объявленные литералом `Knob{…}`
// (ключ сетки, удостоверение ретранслятора). Исключение одно и названо с
// причиной: удостоверение ретранслятора — половина пары «имя в адресе ⇔
// удостоверение» (Д45), стенд без имени в адресе его не несёт, и Validate его
// незаданность не судит.
//
// Утверждения по каждой цепочке stacks.txt, где рендерятся объекты notify:
//
//	(а) каждая обязательная ручка видна контейнеру `notify`;
//	(б) значение ключа карты либо литерала `env` не пусто — пустое страж
//	    отверг бы на старте;
//	(в) KACHO_NOTIFY_ADDRESS_KEY_DIR — путь монтирования тома объекта ключа
//	    отпечатка: том целиком (без `items`), монтирование без `subPath`,
//	    только чтение (Р11, Д23).
//
// Обход, осмотревший ноль цепочек с notify либо ноль ручек, — «НЕ ВЫПОЛНИЛОСЬ».
// Способность упасть — инъекции настоящим входом дерева
// (TestNotifyLoaderKnobsRenderInjections): снятая строка карты краснеет с
// именем ровно этой ручки, пустое значение профиля — тоже.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const (
	notifyLoaderDir = "../services/notify/internal/config"
	notifyKeyDirEnv = "KACHO_NOTIFY_ADDRESS_KEY_DIR"
)

// notifyKnobExemptions — ручки, которые рендер вправе не выводить, и почему.
var notifyKnobExemptions = map[string]string{
	"KACHO_NOTIFY_SMTP_CREDENTIAL": "половина пары «имя в адресе ⇔ удостоверение» (Д45): узел без имени её не несёт, Validate незаданность не судит",
}

// notifyLoaderKnobs — переменные ручек загрузчика notify: теги `envconfig`
// полей типа Config и поле Env литералов `Knob{…}` уровня пакета.
func notifyLoaderKnobs(t *testing.T) []string {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, notifyLoaderDir, func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: загрузчик notify (%s) не разобран: %v", notifyLoaderDir, err)
	}
	seen := map[string]bool{}
	files := 0
	for _, p := range pkgs {
		for _, f := range p.Files {
			files++
			ast.Inspect(f, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.TypeSpec:
					st, ok := x.Type.(*ast.StructType)
					if !ok || x.Name.Name != "Config" {
						return true
					}
					for _, fld := range st.Fields.List {
						if fld.Tag == nil {
							continue
						}
						raw, uerr := strconv.Unquote(fld.Tag.Value)
						if uerr != nil {
							continue
						}
						if env := reflect.StructTag(raw).Get("envconfig"); env != "" {
							seen[env] = true
						}
					}
				case *ast.CompositeLit:
					id, ok := x.Type.(*ast.Ident)
					if !ok || id.Name != "Knob" {
						return true
					}
					for _, el := range x.Elts {
						kv, ok := el.(*ast.KeyValueExpr)
						if !ok {
							continue
						}
						if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "Env" {
							if lit, ok := kv.Value.(*ast.BasicLit); ok && lit.Kind == token.STRING {
								if v, uerr := strconv.Unquote(lit.Value); uerr == nil {
									seen[v] = true
								}
							}
						}
					}
				}
				return true
			})
		}
	}
	if files == 0 || len(seen) == 0 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: файлов загрузчика %d, ручек %d — перепись слепа", files, len(seen))
	}
	for env := range notifyKnobExemptions {
		if !seen[env] {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: исключение %s называет ручку, которой у загрузчика нет — исключать нечего", env)
		}
	}
	out := make([]string, 0, len(seen))
	for env := range seen {
		out = append(out, env)
	}
	sort.Strings(out)
	return out
}

// notifyPodKnobs — переменные, видимые контейнеру `notify` рендера: имя →
// значение (ключа карты либо литерала env) или "<ref>" (запись env со
// ссылкой). objs == 0 — notify в рендере нет.
type notifyPodView struct {
	objs   int
	vars   map[string]string
	keyDir string // путь монтирования тома объекта ключа отпечатка (нарушения — в keyBad)
	keyBad []string
}

func notifyPodKnobs(t *testing.T, rendered string) notifyPodView {
	t.Helper()
	v := notifyPodView{vars: map[string]string{}}
	var mine []renderedObj
	for _, o := range parseRendered(t, rendered) {
		if strings.Contains(o.source, "/charts/notify/") || strings.HasPrefix(o.source, "notify/") {
			mine = append(mine, o)
		}
	}
	v.objs = len(mine)
	maps := map[string]map[string]any{}
	for _, cm := range objsOfKind(mine, "ConfigMap") {
		if d, ok := ndig(cm.doc, "data").(map[string]any); ok {
			maps[cm.name] = d
		}
	}
	for _, d := range objsOfKind(mine, "Deployment") {
		spec := nPodSpec(d)
		vols := map[string]map[string]any{}
		for _, vol := range nlist(spec["volumes"]) {
			if m, ok := vol.(map[string]any); ok {
				vols[nstr(m["name"])] = m
			}
		}
		for _, c := range nlist(spec["containers"]) {
			cm, _ := c.(map[string]any)
			if nstr(cm["name"]) != "notify" {
				continue
			}
			for _, ef := range nlist(cm["envFrom"]) {
				name := nstr(ndig(ef, "configMapRef", "name"))
				for k, val := range maps[name] {
					v.vars[k] = nstr(val)
				}
			}
			for _, e := range nlist(cm["env"]) {
				m, _ := e.(map[string]any)
				if m["valueFrom"] != nil {
					v.vars[nstr(m["name"])] = "<ref>"
				} else {
					v.vars[nstr(m["name"])] = nstr(m["value"])
				}
			}
			// (в) каталог ключа отпечатка — путь монтирования тома секрета.
			dir := v.vars[notifyKeyDirEnv]
			found := false
			for _, vm := range nlist(cm["volumeMounts"]) {
				m, _ := vm.(map[string]any)
				if nstr(m["mountPath"]) != dir || dir == "" {
					continue
				}
				found = true
				vol := vols[nstr(m["name"])]
				switch {
				case nstr(ndig(vol, "secret", "secretName")) == "":
					v.keyBad = append(v.keyBad, "том "+nstr(m["name"])+" по пути "+dir+" — не объект секрета")
				case ndig(vol, "secret", "items") != nil:
					v.keyBad = append(v.keyBad, "том объекта ключа отпечатка с items — объект монтируется целиком (Р11)")
				}
				if nstr(m["subPath"]) != "" {
					v.keyBad = append(v.keyBad, "монтирование ключа отпечатка с subPath — новое поколение объекта не доедет (Р11)")
				}
				if m["readOnly"] != true {
					v.keyBad = append(v.keyBad, "монтирование ключа отпечатка не только для чтения")
				}
			}
			if dir != "" && !found {
				v.keyBad = append(v.keyBad, notifyKeyDirEnv+"="+dir+" — ни одно монтирование контейнера notify не стоит по этому пути")
			}
			v.keyDir = dir
		}
	}
	return v
}

// judgeNotifyKnobs — РЕШЕНИЕ держателя по виду пода: находки «ручка — почему».
func judgeNotifyKnobs(knobs []string, v notifyPodView) []string {
	var out []string
	for _, env := range knobs {
		if _, ex := notifyKnobExemptions[env]; ex {
			continue
		}
		val, ok := v.vars[env]
		switch {
		case !ok:
			out = append(out, env+": (а) рендер ручку не выводит — страж старта откажет «ручка не задана»")
		case strings.TrimSpace(val) == "":
			out = append(out, env+": (б) значение пусто — страж старта отвергнет его с именем ручки")
		}
	}
	for _, b := range v.keyBad {
		out = append(out, notifyKeyDirEnv+": (в) "+b)
	}
	return out
}

// notifyChainViews — цепочки stacks.txt, рендер которых несёт объекты
// notify, с видом пода. Рендер — цепочками ГЕЙТА: `prod` несёт слой оператора
// из каталога образцов (число доверенных прыжков края и узел почты поставка не
// несёт, приёмка NTF-2 Р8, Д51, Д48), иначе рендер `prod` отказывает до суждения.
func notifyChainViews(t *testing.T, c umbrellaCopy, sets ...string) (map[string]notifyPodView, int) {
	t.Helper()
	stacks := deployStacksForRender(t, renderGateOperatorSample)
	var names []string
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)
	out := map[string]notifyPodView{}
	for _, n := range names {
		rendered, err := renderStandProfile(t, c, stacks[n], sets...)
		if err != nil {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: рендер цепочки %s отказал: %v\n%s", n, err, lastLines(rendered, 5))
		}
		if v := notifyPodKnobs(t, rendered); v.objs > 0 {
			out[n] = v
		}
	}
	return out, len(names)
}

// TestNotifyLoaderKnobsReachThePodInEveryChain — держатель на дереве как оно есть.
func TestNotifyLoaderKnobsReachThePodInEveryChain(t *testing.T) {
	knobs := notifyLoaderKnobs(t)
	views, total := notifyChainViews(t, notifyUmbrellaCopy(t, umbrellaCopyOpts{}))
	t.Logf("ручек загрузчика notify %d (исключено %d), цепочек %d, из них notify рендерится в %d",
		len(knobs), len(notifyKnobExemptions), total, len(views))
	if len(views) == 0 {
		t.Fatal("НЕ ВЫПОЛНИЛОСЬ: ни в одной цепочке notify не рендерится — судить нечего")
	}
	var chains []string
	for n := range views {
		chains = append(chains, n)
	}
	sort.Strings(chains)
	for _, n := range chains {
		f := judgeNotifyKnobs(knobs, views[n])
		t.Logf("  %s: переменных у контейнера notify %d, каталог ключа отпечатка %q, находок %d",
			n, len(views[n].vars), views[n].keyDir, len(f))
		for _, x := range f {
			t.Errorf("КРАСНЫЙ: %s: %s", n, x)
		}
	}
}

// TestNotifyLoaderKnobsRenderInjections — держатель краснеет на настоящем
// входе дерева, и находка называет ровно снятую ручку.
func TestNotifyLoaderKnobsRenderInjections(t *testing.T) {
	knobs := notifyLoaderKnobs(t)
	expectNamed := func(name, env string, views map[string]notifyPodView) {
		t.Helper()
		if len(views) == 0 {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: инъекция «%s»: notify не рендерится ни в одной цепочке", name)
		}
		for chain, v := range views {
			hit := false
			for _, f := range judgeNotifyKnobs(knobs, v) {
				// Граница имени: находка начинается ровно с переменной и двоеточия.
				if strings.HasPrefix(f, env+":") {
					hit = true
					t.Logf("инъекция «%s» → %s: %s", name, chain, f)
				}
			}
			if !hit {
				t.Errorf("инъекция «%s»: держатель промолчал о %s в цепочке %s", name, env, chain)
			}
		}
	}

	// Близнец — дерево как есть: находок нет (держит предыдущий тест; здесь —
	// предпосылка, что инъекции ниже меняют ровно одну ручку).
	base, _ := notifyChainViews(t, notifyUmbrellaCopy(t, umbrellaCopyOpts{}))
	for chain, v := range base {
		if f := judgeNotifyKnobs(knobs, v); len(f) != 0 {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: близнец %s красный — инъекции недействительны: %v", chain, f)
		}
	}

	// Снятая строка карты — по одной на каждую подгруппу NTF-4 и ключ отпечатка.
	for _, env := range []string{
		"KACHO_NOTIFY_FEEDBACK_LEASE_TTL",
		"KACHO_NOTIFY_SUPPRESSION_HARD_TTL",
		"KACHO_NOTIFY_REPUTATION_WINDOW",
		notifyKeyDirEnv,
	} {
		c := notifyUmbrellaCopy(t, umbrellaCopyOpts{notifyEdits: map[string]func(string) string{
			"templates/configmap.yaml": dropLineWith("  " + env + ":"),
		}})
		views, _ := notifyChainViews(t, c)
		expectNamed("снята строка "+env, env, views)
	}

	// Пустое значение профиля — у ключа карты и у литерала `env`.
	views, _ := notifyChainViews(t, notifyUmbrellaCopy(t, umbrellaCopyOpts{}), "notify.feedback.pollInterval=")
	expectNamed("пустой notify.feedback.pollInterval", "KACHO_NOTIFY_FEEDBACK_POLL_INTERVAL", views)
	views, _ = notifyChainViews(t, notifyUmbrellaCopy(t, umbrellaCopyOpts{}), "notify.secretReloadInterval=")
	expectNamed("пустой notify.secretReloadInterval", "KACHO_NOTIFY_SECRET_RELOAD_INTERVAL", views)

	// Том ключа отпечатка с subPath.
	c := notifyUmbrellaCopy(t, umbrellaCopyOpts{notifyEdits: map[string]func(string) string{
		"templates/deployment.yaml": replaceOnce(
			`mountPath: {{ include "notify.addressKeyDir" . | quote }}`,
			`mountPath: {{ include "notify.addressKeyDir" . | quote }}
              subPath: addressKey`),
	}})
	views, _ = notifyChainViews(t, c)
	expectNamed("монтирование ключа отпечатка с subPath", notifyKeyDirEnv, views)
}

// dropLineWith — правка копии: снята ровно одна строка, содержащая marker.
// Иное число вхождений — текст без изменений («НЕ ВЫПОЛНИЛОСЬ» у копии).
func dropLineWith(marker string) func(string) string {
	return func(s string) string {
		lines := strings.Split(s, "\n")
		idx := -1
		for i, l := range lines {
			if strings.Contains(l, marker) {
				if idx >= 0 {
					return s
				}
				idx = i
			}
		}
		if idx < 0 {
			return s
		}
		return strings.Join(append(lines[:idx:idx], lines[idx+1:]...), "\n")
	}
}

// TestNotifyAddressKeyRenderGuard — объект ключа отпечатка адреса (NTF-4 Р15,
// Д23) на ноге без зонтика: годный ключ — Secret `<полное имя>-address-key` с
// ключом `addressKey` и аннотация переката; незаданный, короткий, не hex и
// «оба способа сразу» — отказ рендера с именем ручки; значение ключа в тексте
// отказа не печатается.
func TestNotifyAddressKeyRenderGuard(t *testing.T) {
	chart := notifyFixtureChart(t, nil)
	objs := mustRenderNotify(t, chart, standaloneLeg())
	var got []string
	for _, s := range objsOfKind(objs, "Secret") {
		if s.name == notifyRelease+"-address-key" {
			if _, ok := ndig(s.doc, "data", "addressKey").(string); !ok {
				t.Errorf("КРАСНЫЙ: Secret %s без ключа addressKey — процесс читает файл addressKey", s.name)
			}
			got = append(got, s.name)
		}
	}
	if len(got) != 1 {
		t.Fatalf("КРАСНЫЙ: объектов ключа отпечатка %d, ожидался 1 (%s-address-key)", len(got), notifyRelease)
	}
	base := podAnnotation(objs, "checksum/address-key")
	rotated := podAnnotation(mustRenderNotify(t, chart, standaloneLeg(),
		"addressKey="+strings.Repeat("ab", 32)), "checksum/address-key")
	if base == "" || base == rotated {
		t.Errorf("КРАСНЫЙ: смена notify.addressKey не меняет checksum/address-key (%q → %q) — реплики не перекатятся", base, rotated)
	}

	short := strings.Repeat("a", 62)
	notHex := strings.Repeat("g", 64)
	for _, c := range []struct {
		name string
		sets []string
		want string
	}{
		{"ключ не задан", []string{"addressKey="}, "notify.addressKey"},
		{"ключ короче 64 hex-символов", []string{"addressKey=" + short}, "notify.addressKey"},
		{"ключ не hex", []string{"addressKey=" + notHex}, "notify.addressKey"},
		{"заданы и значение, и объект стенда", []string{"addressKeySecret.name=kacho-notify-address-key"}, "notify.addressKeySecret.name"},
	} {
		out, err := renderNotify(t, chart, standaloneLeg(), c.sets...)
		switch {
		case err == nil:
			t.Errorf("КРАСНЫЙ: %s — рендер прошёл, отказа нет", c.name)
		case !strings.Contains(out, c.want):
			t.Errorf("КРАСНЫЙ: %s — отказ без имени ручки %s:\n%s", c.name, c.want, lastLines(out, 3))
		case strings.Contains(out, short) || strings.Contains(out, notHex):
			t.Errorf("КРАСНЫЙ: %s — текст отказа печатает значение ключа", c.name)
		default:
			t.Logf("%s → отказ рендера: %s", c.name, lineWith(out, c.want))
		}
	}

	// Объект стенда вместо значения: чарт объекта не рендерит, том — на объект стенда.
	stand := mustRenderNotify(t, chart, standaloneLeg(), "addressKey=", "addressKeySecret.name=kacho-notify-address-key")
	for _, s := range objsOfKind(stand, "Secret") {
		if s.name == notifyRelease+"-address-key" {
			t.Errorf("КРАСНЫЙ: при объекте стенда чарт рендерит свой объект ключа отпечатка %s", s.name)
		}
	}
	mounted := false
	for _, d := range objsOfKind(stand, "Deployment") {
		for _, v := range nlist(nPodSpec(d)["volumes"]) {
			if nstr(ndig(v, "secret", "secretName")) == "kacho-notify-address-key" {
				mounted = true
			}
		}
	}
	if !mounted {
		t.Errorf("КРАСНЫЙ: при объекте стенда под не монтирует kacho-notify-address-key")
	}
}

// lineWith — первая строка вывода, содержащая marker.
func lineWith(out, marker string) string {
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, marker) {
			return strings.TrimSpace(l)
		}
	}
	return ""
}
