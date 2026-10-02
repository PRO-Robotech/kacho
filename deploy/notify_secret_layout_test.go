// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// notify_secret_layout_test.go — пробы рендера чарта notify (NTF-1, полоса D1;
// замысел З28, CX1-66, CX1-68 (г), CX1-80, CX1-82 (в), CX1-97, CX1-98, УК91,
// Д44–Д47, Д74; приёмка NTF1-I01, NTF1-I05, NTF1-I06).
//
// ─────────────────────────────────────────────────────────────────────────────
// НА ЧЁМ РЕНДЕРИТСЯ И ПОЧЕМУ — ФИКСТУРНАЯ КОПИЯ ЧАРТА
//
// Объекты чарта notify рендерятся только при непустом выведенном перечне
// источников (NTF1-N04). Перечень выводится из закрытой таблицы подключаемых
// источников `templates/_sources.tpl`, и в этой полосе таблица ПУСТА: ни один
// модуль-источник ещё не подключён (строку `notify-probe` и флаг вносит D2).
// Поэтому настоящий чарт в любой цепочке и на ноге без зонтика рендерит ноль
// объектов — это утверждает [TestNotifyChartRendersNothingWithAnEmptyRoster].
//
// Всё прочее рендерится на фикстурной копии чарта, в которой таблица несёт ровно
// одну строку — источник `probe-b` формы таблицы N02. Копия правит ровно тело
// таблицы (одно вхождение, иначе «НЕ ВЫПОЛНИЛОСЬ»), остальной чарт — дерево.
//
// ─────────────────────────────────────────────────────────────────────────────
// НОГА — БЕЗ ЗОНТИКА (CX1-97, CX1-98)
//
// Рендер чарта `deploy/helm/notify/` отдельно, с файлом собственных значений
// ноги `deploy/testdata/notify-standalone/values.yaml` и образцом узла почты из
// каталога образцов D9 (`deploy/testdata/mail-node/operator.yaml`). Стража
// зонтика в этом рендере нет, поэтому отказ на снятый адрес даёт только шаблон
// notify. Нога фикстурной копии ЗОНТИКА (цепочка `prod` через обёртку D9,
// инъекции «страж первым» и `alias`) этим файлом не проводится.
//
// ─────────────────────────────────────────────────────────────────────────────
// ИСХОДОВ ТРИ (verdict-and-landing §1)
//
// Утверждение проводится и держится — молчание; проводится и не держится —
// красный с именем; не проводится (предпосылка не создана) — `t.Fatalf` со
// словами «НЕ ВЫПОЛНИЛОСЬ» и причиной: в зелёные и в красные это не входит.
package deploy_test

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const (
	notifyChartDir         = "helm/notify"
	notifyStandaloneValues = "testdata/notify-standalone/values.yaml"
	notifyStandaloneSample = mailNodeSamplesDir + "/operator.yaml"
	notifyRelease          = "kacho-notify"

	// Ожидаемый путь отказа шаблона `ConfigMap` notify — константа на ногу,
	// полной строкой, сравнение на равенство (CX1-98 (г), М46). Путь ноги без
	// зонтика — суффикс пути ноги зонтика, поэтому сравнение по суффиксу
	// перепутанных ног не различило бы.
	notifyStandaloneConfigMapPath = "notify/templates/configmap.yaml"
	notifyUmbrellaConfigMapPath   = "kacho-umbrella/charts/notify/templates/configmap.yaml"

	notifyMailNodeKey   = "global.kacho.identity.smtp.connectionURI"
	notifyConnectionEnv = "KACHO_NOTIFY_SMTP_CONNECTION_URI"
	notifyCredentialEnv = "KACHO_NOTIFY_SMTP_CREDENTIAL"
	notifyRecipientEnv  = "KACHO_NOTIFY_RECIPIENT_KEY"
	notifyAnchorEnv     = "KACHO_NOTIFY_SMTP_TRUST_ANCHOR_FILE"

	// notifyEmptyTableBody — тело закрытой таблицы подключаемых источников в
	// дереве; копия заменяет ровно его.
	notifyEmptyTableBody = `{{- list | toJson -}}`
	// notifyFixtureTableBody — строка `probe-b` формы таблицы N02.
	notifyFixtureTableBody = `{{- list (dict "module" "probe-b" "feedAddr" "probe-b:9091" ` +
		`"san" "spiffe://kacho.cloud/ns/kacho/sa/probe-b" "classes" (list "notice") ` +
		`"recipientForms" (list) "authorization" "resolveSend") | toJson -}}`
)

// ─── рендер ────────────────────────────────────────────────────────────────

// requireHelmForNotify — helm в PATH; при CI отсутствие — провал, а не пропуск.
func requireHelmForNotify(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("helm не в PATH при CI — рендер-проба чарта notify обязана исполняться, а не пропускаться")
		}
		t.Skip("helm не в PATH — рендер-проба чарта notify пропущена")
	}
}

// notifyChartCopy — копия чарта notify во временном каталоге; edits — путь от
// корня чарта → функция правки текста. Каждая правка обязана изменить файл,
// иначе инъекция не проверила бы то, ради чего заведена («НЕ ВЫПОЛНИЛОСЬ»).
func notifyChartCopy(t *testing.T, edits map[string]func(string) string) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "notify")
	err := filepath.WalkDir(notifyChartDir, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		rel, _ := filepath.Rel(notifyChartDir, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		body, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		return os.WriteFile(target, body, 0o600)
	})
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: копия чарта %s не снята: %v", notifyChartDir, err)
	}
	for rel, edit := range edits {
		p := filepath.Join(dst, rel)
		body, rerr := os.ReadFile(p)
		if rerr != nil {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: правка копии — %s не прочитан: %v", rel, rerr)
		}
		next := edit(string(body))
		if next == string(body) {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: правка копии %s ничего не изменила — инъекция не нашла своего входа", rel)
		}
		if werr := os.WriteFile(p, []byte(next), 0o600); werr != nil {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: правка копии %s: %v", rel, werr)
		}
	}
	return dst
}

// replaceOnce — замена ровно одного вхождения; иное число — пустая строка
// (правка «ничего не изменила», и копия отказывает с именем файла).
func replaceOnce(old, repl string) func(string) string {
	return func(s string) string {
		if strings.Count(s, old) != 1 {
			return s
		}
		return strings.Replace(s, old, repl, 1)
	}
}

// notifyFixtureChart — копия с таблицей из одного источника плюс прочие правки.
func notifyFixtureChart(t *testing.T, extra map[string]func(string) string) string {
	t.Helper()
	edits := map[string]func(string) string{
		"templates/_sources.tpl": replaceOnce(notifyEmptyTableBody, notifyFixtureTableBody),
	}
	for k, f := range extra {
		if prev, ok := edits[k]; ok {
			edits[k] = func(s string) string { return f(prev(s)) }
			continue
		}
		edits[k] = f
	}
	return notifyChartCopy(t, edits)
}

// renderNotify — `helm template` ноги без зонтика. Возврат — вывод и ошибка.
func renderNotify(t *testing.T, chart string, files []string, sets ...string) (string, error) {
	t.Helper()
	requireHelmForNotify(t)
	args := []string{"template", notifyRelease, chart, "-n", "kacho"}
	for _, f := range files {
		args = append(args, "-f", f)
	}
	for _, s := range sets {
		args = append(args, "--set", s)
	}
	out, err := exec.Command("helm", args...).CombinedOutput() // #nosec G204 -- фиксированный бинарь, аргументы из дерева и пробы
	return string(out), err
}

// mustRenderNotify — рендер, который обязан пройти (опора, близнец).
func mustRenderNotify(t *testing.T, chart string, files []string, sets ...string) []renderedObj {
	t.Helper()
	out, err := renderNotify(t, chart, files, sets...)
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: опорный рендер чарта notify отказал: %v\n%s", err, out)
	}
	objs := parseRendered(t, out)
	if len(objs) == 0 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: опорный рендер чарта notify пуст — судить нечего")
	}
	return objs
}

// standaloneLeg — файлы значений ноги без зонтика с образцом узла целиком.
func standaloneLeg() []string { return []string{notifyStandaloneValues, notifyStandaloneSample} }

// renderedObj — документ рендера и его строка `# Source:`.
type renderedObj struct {
	source string
	kind   string
	name   string
	doc    map[string]any
}

var sourceLineRe = regexp.MustCompile(`(?m)^# Source: (\S+)\s*$`)

func parseRendered(t *testing.T, out string) []renderedObj {
	t.Helper()
	var objs []renderedObj
	for _, chunk := range regexp.MustCompile(`(?m)^---\s*$`).Split(out, -1) {
		src := ""
		if m := sourceLineRe.FindStringSubmatch(chunk); m != nil {
			src = m[1]
		}
		var doc map[string]any
		if err := yaml.Unmarshal([]byte(chunk), &doc); err != nil {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: документ рендера не разобран (%v):\n%s", err, chunk)
		}
		if doc == nil {
			continue
		}
		kind, _ := doc["kind"].(string)
		objs = append(objs, renderedObj{source: src, kind: kind, name: nstr(ndig(doc, "metadata", "name")), doc: doc})
	}
	return objs
}

func ndig(v any, path ...string) any {
	for _, k := range path {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[k]
	}
	return v
}

func nstr(v any) string { s, _ := v.(string); return s }

func nlist(v any) []any { l, _ := v.([]any); return l }

func objsOfKind(objs []renderedObj, kind string) []renderedObj {
	var out []renderedObj
	for _, o := range objs {
		if o.kind == kind {
			out = append(out, o)
		}
	}
	return out
}

// podSpecOf — спецификация пода рабочей нагрузки, либо nil.
func nPodSpec(o renderedObj) map[string]any {
	switch o.kind {
	case "Deployment", "StatefulSet", "DaemonSet", "Job", "ReplicaSet":
		m, _ := ndig(o.doc, "spec", "template", "spec").(map[string]any)
		return m
	case "CronJob":
		m, _ := ndig(o.doc, "spec", "jobTemplate", "spec", "template", "spec").(map[string]any)
		return m
	case "Pod":
		m, _ := ndig(o.doc, "spec").(map[string]any)
		return m
	}
	return nil
}

func containersOf(spec map[string]any) []map[string]any {
	var out []map[string]any
	for _, key := range []string{"initContainers", "containers"} {
		for _, c := range nlist(spec[key]) {
			if m, ok := c.(map[string]any); ok {
				out = append(out, m)
			}
		}
	}
	return out
}

// envOf — переменная окружения контейнера notify по имени, либо nil.
func notifyEnv(objs []renderedObj, name string) map[string]any {
	for _, d := range objsOfKind(objs, "Deployment") {
		for _, c := range containersOf(nPodSpec(d)) {
			if nstr(c["name"]) != "notify" {
				continue
			}
			for _, e := range nlist(c["env"]) {
				if m, ok := e.(map[string]any); ok && nstr(m["name"]) == name {
					return m
				}
			}
		}
	}
	return nil
}

// configMapsWithKey — объекты ConfigMap, несущие ключ данных key.
func configMapsWithKey(objs []renderedObj, key string) []renderedObj {
	var out []renderedObj
	for _, cm := range objsOfKind(objs, "ConfigMap") {
		if data, ok := ndig(cm.doc, "data").(map[string]any); ok {
			if _, has := data[key]; has {
				out = append(out, cm)
			}
		}
	}
	return out
}

func podAnnotation(objs []renderedObj, key string) string {
	for _, d := range objsOfKind(objs, "Deployment") {
		if v := nstr(ndig(d.doc, "spec", "template", "metadata", "annotations", key)); v != "" {
			return v
		}
	}
	return ""
}

// ─── перечень пуст → объектов нет (NTF1-N04 в этой полосе) ─────────────────

// TestNotifyChartRendersNothingWithAnEmptyRoster — настоящий чарт (таблица
// пуста) рендерит ноль объектов и не отказывает; близнец — копия с одной
// строкой таблицы → объекты есть.
func TestNotifyChartRendersNothingWithAnEmptyRoster(t *testing.T) {
	out, err := renderNotify(t, notifyChartDir, standaloneLeg())
	if err != nil {
		t.Fatalf("пустой перечень: рендер чарта notify отказал — отказа быть не должно: %v\n%s", err, out)
	}
	objs := parseRendered(t, out)
	if len(objs) != 0 {
		var names []string
		for _, o := range objs {
			names = append(names, o.kind+"/"+o.name+" ("+o.source+")")
		}
		t.Errorf("пустой перечень источников, а чарт notify рендерит %d объектов: %s", len(objs), strings.Join(names, ", "))
	}
	twin := mustRenderNotify(t, notifyFixtureChart(t, nil), standaloneLeg())
	t.Logf("пустой перечень: объектов notify %d; близнец (перечень из одного источника): %d", len(objs), len(twin))
}

// ─── раскладка секретов (CX1-66 (а), CX1-80, CX1-82 (в); NTF1-I01) ─────────

// secretRef — объект и ключ ссылки на секрет.
type secretRef struct{ name, key string }

// notifyLayoutFindings — РЕШЕНИЕ гейта раскладки по разобранному рендеру.
//
//	ключ сетки   единственный Secret чарта, `<полное имя>-recipient-key`;
//	             ссылается ровно одна рабочая нагрузка и только переменной
//	             `secretKeyRef`; в ConfigMap его нет; объект ключа и объект
//	             удостоверения — разные;
//	почта        объектов Secret с данными почты 0; переменная удостоверения —
//	             `secretKeyRef` на объект и ключ узла, при пустом узле её нет;
//	             адреса или URI из секрета нет.
func notifyLayoutFindings(objs []renderedObj, keySecret string, cred *secretRef) []string {
	var out []string
	for _, s := range objsOfKind(objs, "Secret") {
		if s.name != keySecret {
			out = append(out, "Secret "+s.name+" в рендере notify — единственный Secret чарта обязан быть "+
				"объектом ключа сетки "+keySecret+"; секрет почты чарт не рендерит (CX1-82 (а))")
		}
	}
	if len(objsOfKind(objs, "Secret")) == 0 {
		out = append(out, "объекта ключа сетки "+keySecret+" в рендере нет")
	}
	for _, cm := range objsOfKind(objs, "ConfigMap") {
		data, _ := ndig(cm.doc, "data").(map[string]any)
		for k := range data {
			if strings.Contains(k, "RECIPIENT_KEY") {
				out = append(out, "ConfigMap "+cm.name+" несёт ключ сетки ("+k+") — ключ живёт только в своём Secret")
			}
		}
	}
	mounters := map[string]bool{}
	for _, o := range objs {
		spec := nPodSpec(o)
		if spec == nil {
			continue
		}
		for _, v := range nlist(spec["volumes"]) {
			if nstr(ndig(v, "secret", "secretName")) == keySecret {
				mounters[o.kind+"/"+o.name] = true
				out = append(out, o.kind+"/"+o.name+" монтирует ключ сетки томом — только переменной secretKeyRef (З24)")
			}
			for _, src := range nlist(ndig(v, "projected", "sources")) {
				if nstr(ndig(src, "secret", "name")) == keySecret {
					mounters[o.kind+"/"+o.name] = true
					out = append(out, o.kind+"/"+o.name+" монтирует ключ сетки проецируемым томом — только переменной secretKeyRef")
				}
			}
		}
		for _, c := range containersOf(spec) {
			for _, ef := range nlist(c["envFrom"]) {
				if nstr(ndig(ef, "secretRef", "name")) == keySecret {
					mounters[o.kind+"/"+o.name] = true
					out = append(out, o.kind+"/"+o.name+" берёт ключ сетки через envFrom — только переменной secretKeyRef")
				}
			}
			for _, e := range nlist(c["env"]) {
				ref := secretRef{nstr(ndig(e, "valueFrom", "secretKeyRef", "name")), nstr(ndig(e, "valueFrom", "secretKeyRef", "key"))}
				name := nstr(ndig(e, "name"))
				if ref.name == keySecret {
					mounters[o.kind+"/"+o.name] = true
				}
				if name == notifyRecipientEnv && cred != nil && ref.name == cred.name {
					out = append(out, o.kind+"/"+o.name+": ссылка ключа сетки называет объект удостоверения почты "+
						cred.name+" — ключ сетки не поле секрета почты, а свой объект (Д23)")
				}
				if strings.Contains(name, "CONNECTION_URI") && ref.name != "" {
					out = append(out, o.kind+"/"+o.name+": "+name+" берётся из секрета "+ref.name+
						" — адрес узла едет в ConfigMap, из секрета только удостоверение (CX1-80)")
				}
			}
		}
	}
	switch {
	case len(mounters) == 0:
		out = append(out, "на объект ключа сетки "+keySecret+" не ссылается ни одна рабочая нагрузка")
	case len(mounters) > 1:
		var who []string
		for w := range mounters {
			who = append(who, w)
		}
		sort.Strings(who)
		out = append(out, "на объект ключа сетки ссылаются "+strings.Join(who, ", ")+
			" — в NTF-1 монтирующий один, notify")
	}
	credEnv := notifyEnv(objs, notifyCredentialEnv)
	switch {
	case cred == nil && credEnv != nil:
		out = append(out, "узел без credentialSecret, а переменная "+notifyCredentialEnv+" отрендерена")
	case cred != nil && credEnv == nil:
		out = append(out, "узел называет credentialSecret, а переменной "+notifyCredentialEnv+" нет")
	case cred != nil:
		got := secretRef{nstr(ndig(credEnv, "valueFrom", "secretKeyRef", "name")), nstr(ndig(credEnv, "valueFrom", "secretKeyRef", "key"))}
		if got != *cred {
			out = append(out, notifyCredentialEnv+" ссылается на "+got.name+"/"+got.key+", а узел называет "+
				cred.name+"/"+cred.key)
		}
	}
	sort.Strings(out)
	return out
}

// TestNotifySecretLayout — гейт раскладки на рендере фикстурной копии, с
// инъекциями по обе стороны.
func TestNotifySecretLayout(t *testing.T) {
	chart := notifyFixtureChart(t, nil)
	objs := mustRenderNotify(t, chart, standaloneLeg())
	keySecret := notifyRelease + "-recipient-key"
	cred := &secretRef{"kacho-identity-smtp", "password"}
	t.Logf("осмотрено объектов рендера notify: %d", len(objs))

	if f := notifyLayoutFindings(objs, keySecret, cred); len(f) != 0 {
		t.Errorf("раскладка секретов notify не сходится:\n  %s", strings.Join(f, "\n  "))
	}

	// Узел без удостоверения (приёмник стенда) — ссылки нет, и это законно.
	noCred := mustRenderNotify(t, chart, standaloneLeg(),
		"global.kacho.identity.smtp.credentialSecret=null")
	if f := notifyLayoutFindings(noCred, keySecret, nil); len(f) != 0 {
		t.Errorf("узел без credentialSecret:\n  %s", strings.Join(f, "\n  "))
	}

	clone := func() []renderedObj { return parseRendered(t, mustRenderOut(t, chart)) }
	injections := []struct {
		name   string
		mutate func([]renderedObj) []renderedObj
	}{
		{"recipientKey полем секрета почты", func(o []renderedObj) []renderedObj {
			e := notifyEnv(o, notifyRecipientEnv)
			ndig(e, "valueFrom", "secretKeyRef").(map[string]any)["name"] = cred.name
			return o
		}},
		{"второй монтирующий", func(o []renderedObj) []renderedObj {
			d := objsOfKind(o, "Deployment")[0]
			twin := parseRendered(t, mustRenderOut(t, chart))
			second := objsOfKind(twin, "Deployment")[0]
			second.name = d.name + "-api"
			return append(o, second)
		}},
		{"ключ томом", func(o []renderedObj) []renderedObj {
			spec := nPodSpec(objsOfKind(o, "Deployment")[0])
			spec["volumes"] = append(nlist(spec["volumes"]), map[string]any{
				"name": "recipient-key", "secret": map[string]any{"secretName": keySecret}})
			return o
		}},
	}
	for _, inj := range injections {
		if f := notifyLayoutFindings(inj.mutate(clone()), keySecret, cred); len(f) == 0 {
			t.Errorf("инъекция %q: гейт раскладки промолчал", inj.name)
		} else {
			t.Logf("инъекция %q → красный: %s", inj.name, f[0])
		}
	}
	if f := notifyLayoutFindings(clone(), keySecret, cred); len(f) != 0 {
		t.Errorf("близнец (отдельный объект у notify): гейт раскладки заговорил: %v", f)
	}
}

func mustRenderOut(t *testing.T, chart string, sets ...string) string {
	t.Helper()
	out, err := renderNotify(t, chart, standaloneLeg(), sets...)
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: рендер копии чарта notify отказал: %v\n%s", err, out)
	}
	return out
}

// ─── объект ключа: постоянное имя, без порождения, аннотация (CX1-68 (г), УК91, CX1-66 (б)) ─

// recipientKeyNames — имя объекта ключа и `secretKeyRef.name` переменной.
func recipientKeyNames(objs []renderedObj) (secret, ref string) {
	if s := objsOfKind(objs, "Secret"); len(s) == 1 {
		secret = s[0].name
	}
	ref = nstr(ndig(notifyEnv(objs, notifyRecipientEnv), "valueFrom", "secretKeyRef", "name"))
	return secret, ref
}

// TestNotifyRecipientKeyObjectNameIsConstant — два рендера с разными ключами:
// имя объекта и ссылка равны; инъекция — имя с суффиксом от содержимого.
func TestNotifyRecipientKeyObjectNameIsConstant(t *testing.T) {
	judge := func(chart string) (bool, string) {
		a := mustRenderNotify(t, chart, standaloneLeg(), "recipientKey=first-key-value-0123456789abcdef")
		b := mustRenderNotify(t, chart, standaloneLeg(), "recipientKey=second-key-value-fedcba9876543210")
		as, ar := recipientKeyNames(a)
		bs, br := recipientKeyNames(b)
		if as == "" || ar == "" {
			return false, "НЕ ВЫПОЛНИЛОСЬ: объекта ключа или ссылки нет"
		}
		return as == bs && ar == br && as == ar, as + " / " + ar + " против " + bs + " / " + br
	}
	if ok, why := judge(notifyFixtureChart(t, nil)); !ok {
		t.Errorf("имя объекта ключа сетки зависит от значения ключа: %s", why)
	}
	suffix := ` }}-recipient-key-{{ .Values.recipientKey | sha256sum | trunc 8 }}`
	inj := notifyFixtureChart(t, map[string]func(string) string{
		"templates/recipient-key-secret.yaml": replaceOnce(` }}-recipient-key`, suffix),
		"templates/deployment.yaml":           replaceOnce(` }}-recipient-key`, suffix),
	})
	if ok, why := judge(inj); ok {
		t.Errorf("инъекция «имя от содержимого»: проба промолчала (%s)", why)
	} else {
		t.Logf("инъекция «имя от содержимого» → красный: %s", why)
	}
}

// TestNotifyRecipientKeyIsNeverGenerated — пустой ключ — отказ рендера с именем
// ручки; два рендера одних значений — аннотации равны; инъекция `randAlphaNum`.
func TestNotifyRecipientKeyIsNeverGenerated(t *testing.T) {
	chart := notifyFixtureChart(t, nil)
	out, err := renderNotify(t, chart, standaloneLeg(), "recipientKey=")
	if err == nil {
		t.Errorf("пустой notify.recipientKey: рендер прошёл — ключ обязан быть задан, а не порождён")
	} else if !strings.Contains(out, "notify.recipientKey") {
		t.Errorf("пустой notify.recipientKey: отказ без имени ручки:\n%s", out)
	}

	stable := func(c string) (bool, string) {
		a := podAnnotation(mustRenderNotify(t, c, standaloneLeg()), "checksum/recipient-key")
		b := podAnnotation(mustRenderNotify(t, c, standaloneLeg()), "checksum/recipient-key")
		return a != "" && a == b, a + " / " + b
	}
	if ok, why := stable(chart); !ok {
		t.Errorf("два рендера одних значений дали разные аннотации ключа: %s", why)
	}
	inj := notifyFixtureChart(t, map[string]func(string) string{
		"templates/recipient-key-secret.yaml": replaceOnce("type: Opaque", "type: Opaque\n  # nonce {{ randAlphaNum 32 }}"),
	})
	if ok, why := stable(inj); ok {
		t.Errorf("инъекция randAlphaNum: аннотации равны — проба детерминизма промолчала (%s)", why)
	} else {
		t.Logf("инъекция randAlphaNum → красный: %s", why)
	}
}

// TestNotifyRecipientKeyAnnotationFollowsTheKeyOnly — смена ключа меняет
// `checksum/recipient-key` и не меняет `checksum/config`; смена несвязанного
// значения не меняет `checksum/recipient-key`.
func TestNotifyRecipientKeyAnnotationFollowsTheKeyOnly(t *testing.T) {
	chart := notifyFixtureChart(t, nil)
	base := mustRenderNotify(t, chart, standaloneLeg())
	rotated := mustRenderNotify(t, chart, standaloneLeg(), "recipientKey=rotated-key-value-0123456789abcdef")
	unrelated := mustRenderNotify(t, chart, standaloneLeg(), "claimInterval=45s")

	if podAnnotation(base, "checksum/recipient-key") == podAnnotation(rotated, "checksum/recipient-key") {
		t.Errorf("смена notify.recipientKey не сменила checksum/recipient-key — реплики не перекатятся")
	}
	if podAnnotation(base, "checksum/config") != podAnnotation(rotated, "checksum/config") {
		t.Errorf("смена notify.recipientKey сменила checksum/config — ключ попал в ConfigMap")
	}
	if podAnnotation(base, "checksum/recipient-key") != podAnnotation(unrelated, "checksum/recipient-key") {
		t.Errorf("смена claimInterval сменила checksum/recipient-key — каждое обновление стало бы ротацией")
	}
	if podAnnotation(base, "checksum/config") == podAnnotation(unrelated, "checksum/config") {
		t.Errorf("НЕ ВЫПОЛНИЛОСЬ: смена claimInterval не сменила checksum/config — несвязанное значение не доехало до ConfigMap")
	}
}

// ─── узел почты → ключ процесса, нога без зонтика (Д44, Д46, CX1-77, CX1-97, CX1-98) ─

type legOutcome int

const (
	legHeld legOutcome = iota
	legRed
	legNotRun
)

func (o legOutcome) String() string {
	return [...]string{"держится", "КРАСНЫЙ", "НЕ ВЫПОЛНИЛОСЬ"}[o]
}

var execErrorPathRe = regexp.MustCompile(`execution error at \(([^:()]+):\d+:\d+\)`)

// mailNodeLeg — РЕШЕНИЕ пробы ноги: опора (образец целиком) и отрицание
// (образец минус адрес). Опора — код 0, объект с ключом ровно один, путь его
// `# Source:` равен константе ноги, ключ равен адресу образца; иначе
// «НЕ ВЫПОЛНИЛОСЬ». Отрицание — код ≠ 0, путь отказа равен константе ноги
// (иной — «НЕ ВЫПОЛНИЛОСЬ»), текст несёт имя узла; код 0 — КРАСНЫЙ.
func mailNodeLeg(support string, supportErr error, neg string, negErr error, wantPath, address string) (legOutcome, string) {
	if supportErr != nil {
		return legNotRun, "опора отказала: " + support
	}
	var holders []string
	var value string
	for _, chunk := range regexp.MustCompile(`(?m)^---\s*$`).Split(support, -1) {
		var doc map[string]any
		if yaml.Unmarshal([]byte(chunk), &doc) != nil || nstr(doc["kind"]) != "ConfigMap" {
			continue
		}
		data, _ := doc["data"].(map[string]any)
		if v, ok := data[notifyConnectionEnv]; ok {
			src := ""
			if m := sourceLineRe.FindStringSubmatch(chunk); m != nil {
				src = m[1]
			}
			holders = append(holders, src)
			value = nstr(v)
		}
	}
	switch {
	case len(holders) == 0:
		return legNotRun, "в опоре нет объекта с " + notifyConnectionEnv
	case len(holders) > 1:
		return legNotRun, "в опоре объектов с " + notifyConnectionEnv + " больше одного: " + strings.Join(holders, ", ")
	case holders[0] != wantPath:
		return legNotRun, "путь объекта опоры " + holders[0] + " не равен константе ноги " + wantPath
	case value != address:
		return legRed, notifyConnectionEnv + " = " + value + ", а адрес образца — " + address
	}
	if negErr == nil {
		return legRed, "образец без адреса узла отрендерен (код 0) — required снят или подменён"
	}
	m := execErrorPathRe.FindStringSubmatch(neg)
	if m == nil {
		return legNotRun, "путь отказа не разобран: " + neg
	}
	if m[1] != wantPath {
		return legNotRun, "отказал шаблон " + m[1] + ", а не " + wantPath + ": " + neg
	}
	if !strings.Contains(neg, notifyMailNodeKey) {
		return legRed, "отказ шаблона notify без имени узла " + notifyMailNodeKey + ": " + neg
	}
	return legHeld, "опора: " + holders[0] + " = " + value + "; отрицание: отказ " + m[1]
}

// runMailNodeLeg — опора и отрицание на чарте chart.
func runMailNodeLeg(t *testing.T, chart, wantPath, address string) (legOutcome, string) {
	t.Helper()
	support, serr := renderNotify(t, chart, standaloneLeg())
	neg, nerr := renderNotify(t, chart, standaloneLeg(), notifyMailNodeKey+"=null")
	return mailNodeLeg(support, serr, neg, nerr, wantPath, address)
}

// TestNotifyMailNodeReachesTheProcessStandalone — нога без зонтика.
func TestNotifyMailNodeReachesTheProcessStandalone(t *testing.T) {
	// Предпосылка (CX1-98 (б), (в)): в файле ноги узла нет — иначе снятый
	// адрес вернулся бы из него.
	own := readYAML(t, notifyStandaloneValues)
	if ndig(own, "global", "kacho", "identity", "smtp") != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: %s несёт узел global.kacho.identity.smtp — ожидалось absent", notifyStandaloneValues)
	}
	if _, has := own["api-gateway"]; has {
		t.Errorf("%s несёт ключ api-gateway — строка формы зонтика в файле ноги (N24)", notifyStandaloneValues)
	}
	sample := readYAML(t, notifyStandaloneSample)
	address := nstr(ndig(sample, "global", "kacho", "identity", "smtp", "connectionURI"))
	if address == "" {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: образец %s без адреса узла", notifyStandaloneSample)
	}
	t.Logf("образец %s; файл ноги %s (узел: absent); снятые ключи: [%s]", notifyStandaloneSample,
		notifyStandaloneValues, notifyMailNodeKey)

	chart := notifyFixtureChart(t, nil)
	outcome, why := runMailNodeLeg(t, chart, notifyStandaloneConfigMapPath, address)
	switch outcome {
	case legNotRun:
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: %s", why)
	case legRed:
		t.Errorf("узел почты не доезжает до процесса: %s", why)
	default:
		t.Logf("нога без зонтика: %s", why)
	}

	// Константа чужой ноги → «НЕ ВЫПОЛНИЛОСЬ» с фактическим путём; своя —
	// утверждение проводится (CX1-98 (г)).
	if o, w := runMailNodeLeg(t, chart, notifyUmbrellaConfigMapPath, address); o != legNotRun {
		t.Errorf("константа ноги зонтика, поданная ноге без зонтика: ожидалось НЕ ВЫПОЛНИЛОСЬ, получено %s (%s)", o, w)
	}

	cmKey := notifyConnectionEnv + ": {{ tpl (required"
	injections := []struct {
		name string
		edit func(string) string
	}{
		{"снятый required", func(s string) string {
			i := strings.Index(s, cmKey)
			if i < 0 {
				return s
			}
			return s[:i] + notifyConnectionEnv + ": {{ tpl (.Values.global.kacho.identity.smtp.connectionURI | toString) . | quote }}\n" + s[strings.Index(s[i:], "\n")+i+1:]
		}},
		{"default у ключа", func(s string) string {
			i := strings.Index(s, cmKey)
			if i < 0 {
				return s
			}
			return s[:i] + notifyConnectionEnv + ": {{ tpl (.Values.global.kacho.identity.smtp.connectionURI | default \"smtp://fallback:25/\") . | quote }}\n" + s[strings.Index(s[i:], "\n")+i+1:]
		}},
		{"ключ из пути вне узла", func(s string) string {
			i := strings.Index(s, cmKey)
			if i < 0 {
				return s
			}
			return s[:i] + notifyConnectionEnv + ": {{ .Values.origin | quote }}\n" + s[strings.Index(s[i:], "\n")+i+1:]
		}},
	}
	for _, inj := range injections {
		c := notifyFixtureChart(t, map[string]func(string) string{"templates/configmap.yaml": inj.edit})
		o, w := runMailNodeLeg(t, c, notifyStandaloneConfigMapPath, address)
		switch o {
		case legRed:
			t.Logf("инъекция %q → красный: %s", inj.name, w)
		case legNotRun:
			t.Errorf("инъекция %q: НЕ ВЫПОЛНИЛОСЬ — %s", inj.name, w)
		default:
			t.Errorf("инъекция %q: проба промолчала (%s)", inj.name, w)
		}
	}
}

// ─── прочие поля узла (Д47, CX1-81 (б)) ───────────────────────────────────

// profileMailNode — узел почты цепочки стенда: база умбреллы и профили
// цепочки из таблицы стендов (`deployStacks`), сложенные так, как их
// накладывает helm. Состав цепочки здесь не выписывается.
func profileMailNode(t *testing.T, chain string) map[string]any {
	t.Helper()
	layers, ok := deployStacks(t)[chain]
	if !ok {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: цепочки %s в таблице стендов нет", chain)
	}
	merged := readYAML(t, filepath.Join(umbrellaDir, "values.yaml"))
	for _, l := range layers {
		merged = mergeValues(merged, readYAML(t, filepath.Join(umbrellaDir, l)))
	}
	node, _ := ndig(merged, "global", "kacho", "identity", "smtp").(map[string]any)
	if node == nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: цепочка %s не даёт узла почты", chain)
	}
	return node
}

// nodeLayer — файл значений с узлом почты node.
func nodeLayer(t *testing.T, node map[string]any) string {
	t.Helper()
	body, err := yaml.Marshal(map[string]any{"global": map[string]any{"kacho": map[string]any{
		"identity": map[string]any{"smtp": node}}}})
	if err != nil {
		t.Fatalf("узел почты не сериализуется: %v", err)
	}
	p := filepath.Join(t.TempDir(), "node.yaml")
	if err := os.WriteFile(p, body, 0o600); err != nil {
		t.Fatalf("%v", err)
	}
	return p
}

// TestNotifyOtherMailNodeFieldsReachTheProcess — узел стенда разработки (якорь,
// без удостоверения) и узел управляемой площадки (удостоверение, без якоря).
func TestNotifyOtherMailNodeFieldsReachTheProcess(t *testing.T) {
	chart := notifyFixtureChart(t, nil)
	cmValue := func(objs []renderedObj, key string) (string, bool) {
		cms := configMapsWithKey(objs, key)
		if len(cms) != 1 {
			return "", false
		}
		return nstr(ndig(cms[0].doc, "data", key)), true
	}

	dev := profileMailNode(t, "dev")
	devObjs := mustRenderNotify(t, chart, []string{notifyStandaloneValues, nodeLayer(t, dev)})
	for key, field := range map[string]string{"KACHO_NOTIFY_SMTP_FROM_ADDRESS": "fromAddress", "KACHO_NOTIFY_SMTP_FROM_NAME": "fromName"} {
		if got, ok := cmValue(devObjs, key); !ok || got != nstr(dev[field]) {
			t.Errorf("dev: %s = %q, а узел называет %s = %q", key, got, field, nstr(dev[field]))
		}
	}
	if got, _ := cmValue(devObjs, notifyConnectionEnv); got != "smtp://"+notifyRelease+"-mailpit:1025/" {
		t.Errorf("dev: %s = %q, ожидался адрес приёмника релиза", notifyConnectionEnv, got)
	}
	anchor, _ := ndig(dev, "trustAnchorSecret").(map[string]any)
	anchorFile, ok := cmValue(devObjs, notifyAnchorEnv)
	if anchor == nil || !ok {
		t.Errorf("dev: узел с якорем, а %s нет (якорь узла: %v)", notifyAnchorEnv, anchor)
	} else {
		found := false
		spec := nPodSpec(objsOfKind(devObjs, "Deployment")[0])
		for _, v := range nlist(spec["volumes"]) {
			if nstr(ndig(v, "secret", "secretName")) != nstr(anchor["name"]) {
				continue
			}
			for _, it := range nlist(ndig(v, "secret", "items")) {
				if nstr(ndig(it, "key")) == nstr(anchor["key"]) {
					for _, c := range containersOf(spec) {
						for _, vm := range nlist(c["volumeMounts"]) {
							if nstr(ndig(vm, "name")) == nstr(ndig(v, "name")) &&
								filepath.Join(nstr(ndig(vm, "mountPath")), nstr(ndig(it, "path"))) == anchorFile {
								found = true
							}
						}
					}
				}
			}
		}
		if !found {
			t.Errorf("dev: %s = %s не путь файла тома %s/%s", notifyAnchorEnv, anchorFile, nstr(anchor["name"]), nstr(anchor["key"]))
		}
	}
	if notifyEnv(devObjs, notifyCredentialEnv) != nil {
		t.Errorf("dev: узел без удостоверения, а %s отрендерена", notifyCredentialEnv)
	}

	managed := profileMailNode(t, "a8f60d")
	mObjs := mustRenderNotify(t, chart, []string{notifyStandaloneValues, nodeLayer(t, managed)})
	if _, ok := cmValue(mObjs, notifyAnchorEnv); ok {
		t.Errorf("a8f60d: якоря у узла нет, а %s отрендерен", notifyAnchorEnv)
	}
	cs, _ := managed["credentialSecret"].(map[string]any)
	if f := notifyLayoutFindings(mObjs, notifyRelease+"-recipient-key",
		&secretRef{nstr(cs["name"]), nstr(cs["key"])}); len(f) != 0 {
		t.Errorf("a8f60d: %s", strings.Join(f, "; "))
	}

	// Статика: строки, читающие узел, без `default`; `fail` по полям узла 0.
	if f := mailNodeTemplateFindings(t, chart); len(f) != 0 {
		t.Errorf("шаблоны notify читают узел почты не по правилу:\n  %s", strings.Join(f, "\n  "))
	}
	inj := notifyFixtureChart(t, map[string]func(string) string{
		"templates/configmap.yaml": replaceOnce("$smtp.fromName | quote", `$smtp.fromName | default "Kacho" | quote`),
	})
	if f := mailNodeTemplateFindings(t, inj); len(f) == 0 {
		t.Errorf("инъекция default у поля узла: проба промолчала")
	}
}

// mailNodeTemplateFindings — строки шаблонов чарта, читающие узел почты, не
// несут `default`, а `fail` по полям узла в чарте нет.
func mailNodeTemplateFindings(t *testing.T, chart string) []string {
	t.Helper()
	var out []string
	files, _ := filepath.Glob(filepath.Join(chart, "templates", "*"))
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("%v", err)
		}
		for i, line := range strings.Split(string(body), "\n") {
			reads := strings.Contains(line, "identity.smtp") || strings.Contains(line, "$smtp")
			if reads && strings.Contains(line, "default") {
				out = append(out, filepath.Base(f)+":"+strconv.Itoa(i+1)+" — поле узла почты с default: "+strings.TrimSpace(line))
			}
			if reads && strings.Contains(line, "fail ") {
				out = append(out, filepath.Base(f)+":"+strconv.Itoa(i+1)+" — fail по полю узла: пару судит страж старта (Д45)")
			}
		}
	}
	return out
}

// ─── доступность (NTF1-I05) и накат схемы (Д74) ───────────────────────────

// notifyAvailabilityFindings — replicas ≥ 2 и PDB с minAvailable ≥ 1,
// выбирающий под notify.
func notifyAvailabilityFindings(objs []renderedObj) []string {
	var out []string
	deps := objsOfKind(objs, "Deployment")
	if len(deps) != 1 {
		return []string{"развёртываний notify " + strconv.Itoa(len(deps)) + ", ожидалось одно"}
	}
	d := deps[0]
	if r, _ := ndig(d.doc, "spec", "replicas").(int); r < 2 {
		out = append(out, "Deployment "+d.name+": replicas "+strconv.Itoa(r)+" < 2")
	}
	labels, _ := ndig(d.doc, "spec", "template", "metadata", "labels").(map[string]any)
	pdbs := objsOfKind(objs, "PodDisruptionBudget")
	ok := false
	for _, p := range pdbs {
		sel, _ := ndig(p.doc, "spec", "selector", "matchLabels").(map[string]any)
		match := len(sel) > 0
		for k, v := range sel {
			if labels[k] != v {
				match = false
			}
		}
		if ma, _ := ndig(p.doc, "spec", "minAvailable").(int); match && ma >= 1 {
			ok = true
		}
	}
	if !ok {
		out = append(out, "нет PodDisruptionBudget с minAvailable ≥ 1, выбирающего под "+d.name)
	}
	return out
}

func TestNotifyAvailability(t *testing.T) {
	chart := notifyFixtureChart(t, nil)
	if f := notifyAvailabilityFindings(mustRenderNotify(t, chart, standaloneLeg())); len(f) != 0 {
		t.Errorf("доступность notify: %s", strings.Join(f, "; "))
	}
	if f := notifyAvailabilityFindings(mustRenderNotify(t, chart, standaloneLeg(), "replicaCount=1")); len(f) == 0 {
		t.Errorf("инъекция replicas: 1 — проба доступности промолчала")
	} else {
		t.Logf("инъекция replicas: 1 → красный: %s", f[0])
	}
}

// migrateFindings — инициализирующий контейнер `migrate`: `kacho-migrator up`,
// DSN базы `kacho_notify`, пароль — ссылкой на секрет.
func migrateFindings(objs []renderedObj) []string {
	deps := objsOfKind(objs, "Deployment")
	if len(deps) != 1 {
		return []string{"развёртываний notify " + strconv.Itoa(len(deps))}
	}
	for _, c := range nlist(nPodSpec(deps[0])["initContainers"]) {
		if nstr(ndig(c, "name")) != "migrate" {
			continue
		}
		cmd := ""
		for _, a := range nlist(ndig(c, "command")) {
			cmd += nstr(a) + " "
		}
		for _, a := range nlist(ndig(c, "args")) {
			cmd += nstr(a) + " "
		}
		var out []string
		if !strings.Contains(cmd, "kacho-migrator") || !strings.Contains(cmd, "up") {
			out = append(out, "migrate исполняет «"+strings.TrimSpace(cmd)+"», а не kacho-migrator up")
		}
		dsn, pw := "", false
		for _, e := range nlist(ndig(c, "env")) {
			switch nstr(ndig(e, "name")) {
			case "KACHO_MIGRATOR_DSN":
				dsn = nstr(ndig(e, "value"))
			case "KACHO_NOTIFY_DB_PASSWORD":
				pw = nstr(ndig(e, "valueFrom", "secretKeyRef", "name")) != ""
			}
		}
		if !strings.Contains(dsn, "dbname=kacho_notify") {
			out = append(out, "DSN контейнера migrate не называет базу kacho_notify: "+dsn)
		}
		if !pw {
			out = append(out, "пароль базы у migrate не ссылкой на секрет")
		}
		return out
	}
	return []string{"у развёртывания notify нет инициализирующего контейнера migrate (Д74)"}
}

func TestNotifyMigratesItsSchemaBeforeStart(t *testing.T) {
	chart := notifyFixtureChart(t, nil)
	if f := migrateFindings(mustRenderNotify(t, chart, standaloneLeg())); len(f) != 0 {
		t.Errorf("накат схемы notify: %s", strings.Join(f, "; "))
	}
	inj := notifyFixtureChart(t, map[string]func(string) string{
		"templates/deployment.yaml": replaceOnce("- name: migrate", "- name: migrate-removed"),
	})
	if f := migrateFindings(mustRenderNotify(t, inj, standaloneLeg())); len(f) == 0 {
		t.Errorf("инъекция «контейнер migrate снят»: проба промолчала")
	}
}
