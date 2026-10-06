// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// notify_delivery_knobs_render_test.go — ручки процесса notify доезжают до пода
// (NTF-1 §8 замысла, З21, З23, З25; полоса A2 «цикл доставки»).
//
// Класс дефекта: загрузчик notify объявил ручку без умолчания, а чарт её не
// выводит. `helm template` при этом зелёный, а под отказывает в старте стражем —
// «рендерится, но не поднимается» (`ban16-values-prod`). Так было с ручками цикла
// доставки: KACHO_NOTIFY_WORKERS, _DEFER_FOR, _KANAME_ADDR, _KANAME_SAN объявлены
// загрузчиком, а в чарте их не было.
//
// Утверждения:
//
//	(а) перепись: КАЖДОЕ имя переменной, объявленное загрузчиком notify (тег
//	    `envconfig` поля и `Env` литерала Knob), выводит рендер — в карте
//	    конфигурации notify либо в `env` контейнера. Читается на рендере
//	    каждой цепочки stacks.txt, где notify рендерится, и на ноге без зонтика
//	    (фикстурная копия с одним источником) с объявленным якорем узла почты.
//	    Ручек, отсутствие переменной которых законно, две — якорь и
//	    удостоверение ретранслятора ([notifyConditionalKnobs]): на ноге оба
//	    условия созданы и судятся наравне с прочими, в цепочке — пропускаются;
//	(б) ручки цикла доставки — без умолчания: незаданный ключ чарта — отказ
//	    рендера с ИМЕНЕМ ключа; близнец (ключ задан) рендерится и несёт значение.
//
// Способность упасть: (а) — инъекция настоящим входом — копия чарта без строки
// карты ровно одной ручки — красная с именем переменной; (б) — отрицание
// рядом с положительным близнецом.

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

	"gopkg.in/yaml.v3"
)

// notifyConfigDir — пакет загрузчика notify: источник переписи ручек.
const notifyConfigDir = "../services/notify/internal/config"

// notifyAnchorSets — объявленный якорь узла почты: при нём рендерится
// KACHO_NOTIFY_SMTP_TRUST_ANCHOR_FILE (без якоря её отсутствие законно).
var notifyAnchorSets = []string{
	"global.kacho.identity.smtp.trustAnchorSecret.name=relay-anchor",
	"global.kacho.identity.smtp.trustAnchorSecret.key=ca.crt",
}

// notifyConditionalKnobs — ручки, отсутствие переменной которых законно, и
// условие, при котором чарт её рендерит. На ноге без зонтика оба условия
// созданы (образец узла несёт удостоверение, якорь объявлен), и там они
// судятся наравне с прочими; в цепочке — по узлу почты профиля.
var notifyConditionalKnobs = map[string]string{
	"KACHO_NOTIFY_SMTP_TRUST_ANCHOR_FILE": "узел почты объявляет trustAnchorSecret",
	"KACHO_NOTIFY_SMTP_CREDENTIAL":        "узел почты объявляет credentialSecret (приёмник стенда без входа — без него)",
}

// deliveryKnobKeys — ключ чарта → переменная процесса (полоса A2).
var deliveryKnobKeys = []struct{ key, env, value string }{
	{"workers", "KACHO_NOTIFY_WORKERS", "8"},
	{"deferFor", "KACHO_NOTIFY_DEFER_FOR", "30s"},
	{"kaname.addr", "KACHO_NOTIFY_KANAME_ADDR", "kaname-internal.kacho.svc:9091"},
	{"kaname.san", "KACHO_NOTIFY_KANAME_SAN", "spiffe://kacho.cloud/ns/kacho/sa/kaname"},
}

// declaredNotifyKnobEnvs — имена переменных, объявленные загрузчиком: теги
// `envconfig` полей и поле `Env` составных литералов типа Knob. Разбор, а не
// поиск по образцу: строка в комментарии ручкой не считается.
func declaredNotifyKnobEnvs(t *testing.T) (map[string]string, int) {
	t.Helper()
	fset := token.NewFileSet()
	files, err := filepath.Glob(filepath.Join(notifyConfigDir, "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: исходники загрузчика notify в %s не найдены (%v)", notifyConfigDir, err)
	}
	out := map[string]string{}
	read := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, perr := parser.ParseFile(fset, f, nil, 0)
		if perr != nil {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: %s не разобран: %v", f, perr)
		}
		read++
		ast.Inspect(file, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.Field:
				if x.Tag == nil {
					return true
				}
				tag, uerr := strconv.Unquote(x.Tag.Value)
				if uerr != nil {
					return true
				}
				if env := reflect.StructTag(tag).Get("envconfig"); env != "" {
					out[env] = fset.Position(x.Pos()).String()
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
								out[v] = fset.Position(lit.Pos()).String()
							}
						}
					}
				}
			}
			return true
		})
	}
	if len(out) == 0 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: в загрузчике notify (%d файлов) не найдено ни одной ручки — разбор не нашёл своего входа", read)
	}
	return out, read
}

// renderedNotifyEnvs — имена переменных, которые рендер выводит процессу
// notify: ключи карты конфигурации notify (её подключает envFrom) и имена
// `env` контейнера notify.
func renderedNotifyEnvs(objs []renderedObj) map[string]bool {
	out := map[string]bool{}
	for _, cm := range objsOfKind(objs, "ConfigMap") {
		if !strings.HasSuffix(cm.name, "notify-config") {
			continue
		}
		if data, ok := ndig(cm.doc, "data").(map[string]any); ok {
			for k := range data {
				out[k] = true
			}
		}
	}
	for _, d := range objsOfKind(objs, "Deployment") {
		for _, c := range containersOf(nPodSpec(d)) {
			if nstr(c["name"]) != "notify" {
				continue
			}
			for _, e := range nlist(c["env"]) {
				if m, ok := e.(map[string]any); ok {
					out[nstr(m["name"])] = true
				}
			}
		}
	}
	return out
}

// missingKnobs — объявленные загрузчиком переменные, которых рендер не выводит.
func missingKnobs(declared map[string]string, rendered map[string]bool) []string {
	var miss []string
	for env, at := range declared {
		if !rendered[env] {
			miss = append(miss, env+" ("+at+")")
		}
	}
	sort.Strings(miss)
	return miss
}

// TestNotifyEveryLoaderKnobReachesThePod — (а) на ноге без зонтика и на каждой
// цепочке, где notify рендерится.
func TestNotifyEveryLoaderKnobReachesThePod(t *testing.T) {
	requireHelmNTF(t)
	declared, files := declaredNotifyKnobEnvs(t)
	t.Logf("загрузчик notify: файлов %d, ручек %d", files, len(declared))

	chart := notifyFixtureChart(t, nil)
	objs := mustRenderNotify(t, chart, standaloneLeg(), notifyAnchorSets...)
	rendered := renderedNotifyEnvs(objs)
	if miss := missingKnobs(declared, rendered); len(miss) > 0 {
		t.Errorf("КРАСНЫЙ: нога без зонтика — ручки загрузчика notify, которых рендер не выводит: %s", strings.Join(miss, "; "))
	}
	t.Logf("нога без зонтика: выведено переменных %d, объявлено %d", len(rendered), len(declared))

	c := notifyUmbrellaCopy(t, umbrellaCopyOpts{})
	judged := 0
	for _, n := range ntfChains(t) {
		cobjs := ntfMustRender(t, c, "цепочки "+n, c.chainFiles(t, n))
		if notifySourceCount(cobjs) == 0 {
			t.Logf("цепочка %s: notify не рендерится — судить нечего", n)
			continue
		}
		judged++
		got := renderedNotifyEnvs(cobjs)
		decl := map[string]string{}
		for k, v := range declared {
			if _, cond := notifyConditionalKnobs[k]; cond {
				continue
			}
			decl[k] = v
		}
		if miss := missingKnobs(decl, got); len(miss) > 0 {
			t.Errorf("КРАСНЫЙ: цепочка %s — ручки загрузчика notify, которых рендер не выводит: %s", n, strings.Join(miss, "; "))
		}
		for _, k := range deliveryKnobKeys {
			if v := configMapValue(cobjs, k.env); v == "" {
				t.Errorf("КРАСНЫЙ: цепочка %s — %s пуст", n, k.env)
			}
		}
		t.Logf("цепочка %s: выведено переменных %d", n, len(got))
	}
	if judged == 0 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: ни в одной цепочке notify не рендерится — перепись по цепочкам беспредметна")
	}
}

// configMapValue — значение ключа key карты конфигурации notify.
func configMapValue(objs []renderedObj, key string) string {
	for _, cm := range objsOfKind(objs, "ConfigMap") {
		if !strings.HasSuffix(cm.name, "notify-config") {
			continue
		}
		if v := nstr(ndig(cm.doc, "data", key)); v != "" {
			return v
		}
	}
	return ""
}

// TestNotifyDeliveryKnobsRefuseRenderWhenUnset — (б): каждый ключ цикла
// доставки, заданный пустым, — отказ рендера с именем ключа; близнец несёт
// значение ноги дословно.
func TestNotifyDeliveryKnobsRefuseRenderWhenUnset(t *testing.T) {
	requireHelmNTF(t)
	chart := notifyFixtureChart(t, nil)
	objs := mustRenderNotify(t, chart, standaloneLeg())
	for _, k := range deliveryKnobKeys {
		if got := configMapValue(objs, k.env); got != k.value {
			t.Errorf("КРАСНЫЙ: близнец — %s = %q, ключ чарта %s = %q", k.env, got, k.key, k.value)
		}
		out, err := renderNotify(t, chart, standaloneLeg(), k.key+"=")
		if rerr := ntfRefusalNames(out, err, k.key+" (в зонтике notify."+k.key+")"); rerr != nil {
			t.Errorf("КРАСНЫЙ: ключ %s пуст: %v", k.key, rerr)
			continue
		}
		t.Logf("ключ %s пуст → отказ рендера с именем ключа", k.key)
	}
}

// TestNotifyKnobCensusInjection — инъекция настоящим входом: копия чарта без
// строки карты ровно одной ручки цикла доставки краснеет переписью с именем
// именно этой переменной.
func TestNotifyKnobCensusInjection(t *testing.T) {
	requireHelmNTF(t)
	declared, _ := declaredNotifyKnobEnvs(t)
	for _, k := range deliveryKnobKeys {
		line := fmt.Sprintf("  %s: {{ required", k.env)
		body, err := os.ReadFile(filepath.Join(notifyChartDir, "templates", "configmap.yaml"))
		if err != nil {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: карта чарта не прочитана: %v", err)
		}
		var drop string
		for _, l := range strings.Split(string(body), "\n") {
			if strings.HasPrefix(l, line) {
				drop = l + "\n"
			}
		}
		if drop == "" {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: строка %s в карте чарта не найдена — инъекции не во что попасть", k.env)
		}
		chart := notifyFixtureChart(t, map[string]func(string) string{
			"templates/configmap.yaml": replaceOnce(drop, ""),
		})
		objs := mustRenderNotify(t, chart, standaloneLeg(), notifyAnchorSets...)
		miss := missingKnobs(declared, renderedNotifyEnvs(objs))
		if len(miss) != 1 || !strings.HasPrefix(miss[0], k.env+" ") {
			t.Errorf("КРАСНЫЙ: инъекция «нет строки %s» — перепись назвала %v, ожидалась ровно эта переменная", k.env, miss)
			continue
		}
		t.Logf("инъекция «нет строки %s» → перепись красная: %s", k.env, miss[0])
	}
}

// ─── ребро notify → kaname ResolveSend: обе стороны о ОДНОМ предмете ─────────
//
// Части, исправные по отдельности, не сходятся в целое (`hard-parts-must-
// converge`): notify называет SAN kaname, kaname называет SAN notify, и каждая
// половина может быть «задана» и при этом не про тот лист. Проба сводит их на
// рендере каждой цепочки, где notify рендерится:
//
//	(в) KACHO_NOTIFY_KANAME_SAN равен URI SAN серверного листа kaname;
//	(г) таблица `authn.service-identity` kaname несёт строку {SAN клиентского
//	    листа notify → notify}, а перечень методов — ResolveSend.
//
// Отрицание — половина блока звена: отказ рендера с именем ручки.

const resolveSendMethod = "kaname.cloud.iam.v1.InternalNotificationGrantService/ResolveSend"

// certURIs — URI SAN объекта Certificate по имени секрета.
func certURIs(objs []renderedObj, secret string) []string {
	var out []string
	for _, c := range objsOfKind(objs, "Certificate") {
		if nstr(ndig(c.doc, "spec", "secretName")) != secret {
			continue
		}
		for _, u := range nlist(ndig(c.doc, "spec", "uris")) {
			out = append(out, nstr(u))
		}
	}
	return out
}

// kanameServiceIdentity — разобранный ключ authn.service-identity карты kaname.
func kanameServiceIdentity(t *testing.T, objs []renderedObj) (methods []string, services map[string]string, found bool) {
	t.Helper()
	for _, cm := range objsOfKind(objs, "ConfigMap") {
		if cm.name != "kaname-config" {
			continue
		}
		raw := nstr(ndig(cm.doc, "data", "config.yaml"))
		var conf map[string]any
		if err := yaml.Unmarshal([]byte(raw), &conf); err != nil {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: config.yaml карты kaname не разобран: %v", err)
		}
		found = true
		si, _ := ndig(conf, "authn", "service-identity").(map[string]any)
		for _, m := range nlist(si["methods"]) {
			methods = append(methods, nstr(m))
		}
		services = map[string]string{}
		for _, s := range nlist(si["services"]) {
			if m, ok := s.(map[string]any); ok {
				services[nstr(m["san"])] = nstr(m["name"])
			}
		}
	}
	return methods, services, found
}

// TestNotifyKanameEdgeConverges — (в), (г) по цепочкам; отрицание — половина
// блока звена.
func TestNotifyKanameEdgeConverges(t *testing.T) {
	requireHelmNTF(t)
	c := notifyUmbrellaCopy(t, umbrellaCopyOpts{})
	judged := 0
	var devFiles []string
	for _, n := range ntfChains(t) {
		files := c.chainFiles(t, n)
		objs := ntfMustRender(t, c, "цепочки "+n, files)
		if notifySourceCount(objs) == 0 {
			continue
		}
		judged++
		if devFiles == nil {
			devFiles = files
		}
		kanameLeaf := certURIs(objs, "kaname-server-tls")
		notifyLeaf := certURIs(objs, "kacho-notify-peer-tls")
		if len(kanameLeaf) != 1 || len(notifyLeaf) != 1 {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: цепочка %s — листы не найдены однозначно: kaname %v, notify %v", n, kanameLeaf, notifyLeaf)
		}
		if got := configMapValue(objs, "KACHO_NOTIFY_KANAME_SAN"); got != kanameLeaf[0] {
			t.Errorf("КРАСНЫЙ: цепочка %s — notify ждёт от kaname SAN %q, а серверный лист kaname несёт %q", n, got, kanameLeaf[0])
		}
		methods, services, found := kanameServiceIdentity(t, objs)
		if !found {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: цепочка %s — карта kaname-config не найдена", n)
		}
		if services[notifyLeaf[0]] != "notify" {
			t.Errorf("КРАСНЫЙ: цепочка %s — звено идентичности kaname не опознаёт лист notify %q как service:notify (таблица %v)", n, notifyLeaf[0], services)
		}
		if len(methods) != 1 || methods[0] != resolveSendMethod {
			t.Errorf("КРАСНЫЙ: цепочка %s — методы звена %v, ожидался ровно %s", n, methods, resolveSendMethod)
		}
		t.Logf("цепочка %s: notify→kaname SAN %s; kaname опознаёт %s как service:%s", n, kanameLeaf[0], notifyLeaf[0], services[notifyLeaf[0]])
	}
	if judged == 0 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: ни в одной цепочке notify не рендерится — ребро судить не на чем")
	}
	for _, half := range []struct{ set, name string }{
		{"kaname.serviceIdentity.services=null", "serviceIdentity.services"},
		{"kaname.serviceIdentity.methods=null", "serviceIdentity.methods"},
	} {
		_, out, err := ntfRender(t, c, devFiles, half.set)
		if rerr := ntfRefusalNames(out, err, half.name); rerr != nil {
			t.Errorf("КРАСНЫЙ: половина блока звена (%s): %v", half.set, rerr)
			continue
		}
		t.Logf("%s → отказ рендера с именем %s", half.set, half.name)
	}
}
