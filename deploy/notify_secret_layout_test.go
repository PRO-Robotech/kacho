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
// notify.
//
// ─────────────────────────────────────────────────────────────────────────────
// НОГА — ФИКСТУРНАЯ КОПИЯ ЗОНТИКА (N02, N20, N25; CX1-94, CX1-97)
//
// Цепочка `prod` через обёртку D9 на копии зонтика во временном каталоге (раздел
// «нога фикстурной копии ЗОНТИКА» ниже): опора и отрицание, инъекции «страж
// первым», `alias: notifyx`, копия шаблона под другим именем, состав копии.
// Там же — решения Д76: пустой выведенный перечень даёт закрытый
// детерминированный исход в каждой цепочке, notify без тега образа — отказ
// рендера с именем ручки.
//
// Чего здесь НЕТ: I02, I06 приёмки NTF-1 (подъём — D6); I01 по цепочкам дерева —
// deploy/notifications_flag_test.go (D2).
//
// ─────────────────────────────────────────────────────────────────────────────
// ИСХОДОВ ТРИ (verdict-and-landing §1)
//
// Утверждение проводится и держится — молчание; проводится и не держится —
// красный с именем; не проводится (предпосылка не создана) — `t.Fatalf` со
// словами «НЕ ВЫПОЛНИЛОСЬ» и причиной: в зелёные и в красные это не входит.
package deploy_test

import (
	"encoding/base64"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/gitenv"
	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/kacho/internal/migrationchains"
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

	// notifyTableTail — хвост тела таблицы модулей `_sources.tpl` дерева (строка
	// после последней строки таблицы); копия дописывает строки перед ним.
	notifyTableTail = "\n| toJson -}}"
	// notifyFixtureRow — строка `probe-b` формы таблицы модулей (N02): ключ
	// модуля, путь собственного ключа флага, запись источника.
	notifyFixtureRow = `  (dict "key" "probeB" "ownFlagKey" "probeB.notifications.enabled" "source" ` +
		`(dict "module" "probe-b" "feedAddr" "probe-b:9091" ` +
		`"san" "spiffe://kacho.cloud/ns/kacho/sa/probe-b" "classes" (list "notice") ` +
		`"recipientForms" (list) "authorization" "resolveSend"))`
	// notifyValuesLimitsAnchor — пустой словарь ручек на источник в values.yaml
	// чарта; копия заменяет его записью своего источника.
	notifyValuesLimitsAnchor = "sourceLimits: {}"
)

// withTableRows — правка `_sources.tpl` копии: строки rows дописаны в таблицу
// модулей перед её хвостом. Хвост не единственный — правка ничего не меняет, и
// копия отказывает «НЕ ВЫПОЛНИЛОСЬ».
func withTableRows(rows ...string) func(string) string {
	return replaceOnce(notifyTableTail, "\n"+strings.Join(rows, "\n")+notifyTableTail)
}

// withSourceLimits — правка values.yaml копии чарта: ручки на источник для
// модулей modules (фикстурные величины в границах процесса).
func withSourceLimits(modules ...string) func(string) string {
	var b strings.Builder
	b.WriteString("sourceLimits:")
	for _, m := range modules {
		fmt.Fprintf(&b, "\n  %s:\n    rate: 5\n    burst: 5\n    paused: false", m)
	}
	return replaceOnce(notifyValuesLimitsAnchor, b.String())
}

// fixtureRowEdits — правки копии чарта notify: строка `probe-b` в таблице
// модулей и её ручки на источник.
func fixtureRowEdits() map[string]func(string) string {
	return map[string]func(string) string{
		"templates/_sources.tpl": withTableRows(notifyFixtureRow),
		"values.yaml":            withSourceLimits("probe-b"),
	}
}

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
	applyCopyEdits(t, dst, edits)
	return dst
}

// applyCopyEdits — правки копии: путь от base → функция правки текста. Правка,
// ничего не изменившая, — «НЕ ВЫПОЛНИЛОСЬ»: инъекция не нашла своего входа.
func applyCopyEdits(t *testing.T, dst string, edits map[string]func(string) string) {
	t.Helper()
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
	edits := fixtureRowEdits()
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

// TestNotifyChartRendersNothingWithAnEmptyRoster — настоящий чарт с пустым
// выведенным перечнем (нога без зонтика: единственный источник таблицы
// выключен переопределением) рендерит ноль объектов и не отказывает; близнец —
// копия со строкой `probe-b` таблицы → объекты есть.
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
//	ключи        Secret чарта ровно два — объекты ключей: ключ сетки
//	             `<полное имя>-recipient-key` и ключ отпечатка адреса
//	             `<полное имя>-address-key` (NTF-4 Д23); иной Secret — находка;
//	ключ сетки   `<полное имя>-recipient-key`;
//	             ссылается ровно одна рабочая нагрузка и только переменной
//	             `secretKeyRef`; в ConfigMap его нет; объект ключа и объект
//	             удостоверения — разные;
//	почта        объектов Secret с данными почты 0; переменная удостоверения —
//	             `secretKeyRef` на объект и ключ узла, при пустом узле её нет;
//	             адреса или URI из секрета нет.
func notifyLayoutFindings(objs []renderedObj, keySecret string, cred *secretRef) []string {
	var out []string
	addressKeySecret := strings.TrimSuffix(keySecret, "-recipient-key") + "-address-key"
	for _, s := range objsOfKind(objs, "Secret") {
		if s.name != keySecret && s.name != addressKeySecret {
			out = append(out, "Secret "+s.name+" в рендере notify — Secret чарта обязан быть объектом ключа "+
				"сетки "+keySecret+" либо ключа отпечатка "+addressKeySecret+" (NTF-4 Д23); секрет почты чарт "+
				"не рендерит (CX1-82 (а))")
		}
	}
	hasKey := false
	for _, s := range objsOfKind(objs, "Secret") {
		hasKey = hasKey || s.name == keySecret
	}
	if !hasKey {
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
		{"секрет почты третьим Secret чарта", func(o []renderedObj) []renderedObj {
			return append(o, renderedObj{kind: "Secret", name: notifyRelease + "-smtp",
				doc: map[string]any{"kind": "Secret", "metadata": map[string]any{"name": notifyRelease + "-smtp"}}})
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
	if s := recipientKeyObjects(objs); len(s) == 1 {
		secret = s[0].name
	}
	ref = nstr(ndig(notifyEnv(objs, notifyRecipientEnv), "valueFrom", "secretKeyRef", "name"))
	return secret, ref
}

// recipientKeyObjects — объекты Secret рендера, несущие ключ данных
// `recipientKey`. Secret чарта не один (ключ отпечатка адреса — свой объект,
// NTF-4 Д23), поэтому объект ключа сетки выбирается по ключу данных, а не по
// виду.
func recipientKeyObjects(objs []renderedObj) []renderedObj {
	var out []renderedObj
	for _, s := range objsOfKind(objs, "Secret") {
		if _, ok := ndig(s.doc, "data", "recipientKey").(string); ok {
			out = append(out, s)
		}
	}
	return out
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
	// Имя ссылки пода выводит помощник `notify.recipientKeySecretName`
	// (_helpers.tpl; объект стенда Д123 либо объект чарта) — инъекция правит
	// оба написания имени: объект и помощник.
	suffix := ` }}-recipient-key-{{ .Values.recipientKey | sha256sum | trunc 8 }}`
	inj := notifyFixtureChart(t, map[string]func(string) string{
		"templates/recipient-key-secret.yaml": replaceOnce(` }}-recipient-key`, suffix),
		"templates/_helpers.tpl": replaceOnce(`(printf "%s-recipient-key" (include "notify.fullname" .))`,
			`(printf "%s-recipient-key-%s" (include "notify.fullname" .) (.Values.recipientKey | sha256sum | trunc 8))`),
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

// TestNotifyRecipientKeyShorterThanTheHashIsRefusedAtRender — ключ сетки короче
// 32 байт — отказ РЕНДЕРА с именем ручки и границей (Д94), той же границей, что
// страж старта (Д89). Близнец — ключ ровно 32 байт: рендер проходит, и объект
// ключа несёт его. Граница — в БАЙТАХ, как у стража (`len` Go): 16 кириллических
// букв — 32 байта (проходит), 15 — 30 (отказ). Ни значение, ни длина в текст
// отказа не попадают.
func TestNotifyRecipientKeyShorterThanTheHashIsRefusedAtRender(t *testing.T) {
	chart := notifyFixtureChart(t, nil)
	cases := []struct {
		name, key string
		refused   bool
	}{
		{"31 байт ASCII", strings.Repeat("k", 31), true},
		{"32 байта ASCII (близнец)", strings.Repeat("k", 32), false},
		{"15 кириллических букв — 30 байт", strings.Repeat("ключ", 3) + "клю", true},
		{"16 кириллических букв — 32 байта (близнец)", strings.Repeat("ключ", 4), false},
	}
	for _, c := range cases {
		if got := len(c.key) < 32; got != c.refused {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: кейс %q построен неверно — длина %d байт", c.name, len(c.key))
		}
		out, err := renderNotify(t, chart, standaloneLeg(), "recipientKey="+c.key)
		switch {
		case c.refused && err == nil:
			t.Errorf("%s: рендер прошёл — короткий ключ обязан быть отказом рендера (Д94)", c.name)
		case c.refused && !(strings.Contains(out, "notify.recipientKey") && strings.Contains(out, "короче 32 байт")):
			t.Errorf("%s: отказ без имени ручки или границы:\n%s", c.name, out)
		case c.refused && strings.Contains(out, c.key):
			t.Errorf("%s: значение ключа попало в текст отказа", c.name)
		case !c.refused && err != nil:
			t.Errorf("%s: рендер отказал на ключе, равном границе:\n%s", c.name, out)
		case !c.refused:
			var got string
			if sec := recipientKeyObjects(mustRenderNotify(t, chart, standaloneLeg(), "recipientKey="+c.key)); len(sec) == 1 {
				raw, decErr := base64.StdEncoding.DecodeString(nstr(ndig(sec[0].doc, "data", "recipientKey")))
				if decErr == nil {
					got = string(raw)
				}
			}
			if got != c.key {
				t.Errorf("%s: объект ключа не несёт заданный ключ (получено %d байт)", c.name, len(got))
			}
		default:
			t.Logf("%s → отказ рендера с именем ручки и границей", c.name)
		}
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
		// dbname — ТОЧНЫМ значением, не подстрокой: подстрока принимает
		// dbname=kacho_notifyprobe (база пробы) за базу службы (Д84). Читает его
		// та же функция, которой точка наката выбирает цепочку
		// (migrationchains.DatabaseOf над разбором драйвера, Д93), — своего
		// разбора DSN у пробы нет.
		if db, err := migrationchains.DatabaseOf(dsn); err != nil || db != "kacho_notify" {
			out = append(out, fmt.Sprintf("DSN контейнера migrate называет базу «%s» (%v), а не ровно kacho_notify", db, err))
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
	// Близнец подстроки: имя базы пробы содержит имя базы службы префиксом.
	if f := migrateFindings(mustRenderNotify(t, chart, standaloneLeg(), "db.name=kacho_notifyprobe")); len(f) == 0 {
		t.Errorf("инъекция dbname=kacho_notifyprobe: проба промолчала — dbname сверяется подстрокой")
	} else {
		t.Logf("инъекция dbname=kacho_notifyprobe → красный: %s", f[0])
	}
}

// migrateDeployment — синтетическое развёртывание notify с контейнером migrate
// и DSN dsn: вход решения migrateFindings без рендера.
func migrateDeployment(dsn string) []renderedObj {
	return []renderedObj{{kind: "Deployment", name: "kacho-notify", doc: map[string]any{
		"spec": map[string]any{"template": map[string]any{"spec": map[string]any{
			"initContainers": []any{map[string]any{
				"name": "migrate", "command": []any{"kacho-migrator"}, "args": []any{"up"},
				"env": []any{
					map[string]any{"name": "KACHO_MIGRATOR_DSN", "value": dsn},
					map[string]any{"name": "KACHO_NOTIFY_DB_PASSWORD",
						"valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": "s", "key": "k"}}},
				},
			}},
		}}},
	}}}
}

// Д93 — имя базы DSN контейнера migrate читается разбором драйвера, а не своим:
// каждая законная запись libpq, которую драйвер читает как kacho_notify, —
// молчание; запись с другой базой — красный. Свой разбор «k=v через пробел»
// не видел кавычек и пробелов вокруг «=» и называл годную запись чужой.
func TestMigrateDSNDatabaseIsReadByTheDriverParse(t *testing.T) {
	t.Setenv("PGDATABASE", "")
	for _, dsn := range []string{
		"host=h port=5432 user=notify dbname=kacho_notify sslmode=require",
		"host=h port=5432 user=notify dbname = kacho_notify sslmode=require",
		"host=h port=5432 user=notify dbname='kacho_notify' sslmode=require",
		"postgres://notify@h:5432/kacho_notify?sslmode=require",
	} {
		if f := migrateFindings(migrateDeployment(dsn)); len(f) != 0 {
			t.Errorf("DSN %q называет kacho_notify, а проба красная: %v", dsn, f)
		}
	}
	for _, dsn := range []string{
		"host=h dbname=kacho_notifyprobe sslmode=require",
		"host=h dbname='kacho_notify x' sslmode=require",
		"host=h sslmode=require",
		"host=h dbname='open",
	} {
		if f := migrateFindings(migrateDeployment(dsn)); len(f) == 0 {
			t.Errorf("DSN %q не называет ровно kacho_notify, а проба молчит", dsn)
		}
	}
}

// ─── нога фикстурной копии ЗОНТИКА (N02, N20, N25; CX1-94, CX1-97, CX1-99, М43, М45) ─
//
// Цепочка `prod` через обёртку D9 (`deployStacksForRender`, образец
// `operator.yaml` последним слоем) рендерится на КОПИИ зонтика во временном
// каталоге: зонтик, каталоги его `file://`-зависимостей по путям их
// `repository` и каталог образцов — в прежнем взаимном положении от корня
// репозитория. Копия, а не дерево: инъекции правят чарт notify и `Chart.yaml`
// зонтика, а таблица источников в дереве пуста (строку `probe-b` несёт копия).
//
// Состав копии выводится разбором `dependencies` зонтика, а не выписан здесь:
// новая `file://`-зависимость входит в копию без правки пробы, недостающий
// каталог — «НЕ ВЫПОЛНИЛОСЬ» с именем зависимости. Файлы каталога — те, что
// дерево ведёт (`git ls-files --cached --others --exclude-standard`): собранные
// архивы локальных сабчартов и отметки материализации в копию не попадают, их
// производит шаг материализации самой копии — владелец
// `deploy/scripts/helm-umbrella-deps.sh` (гейт единственного владельца).
//
// Слой значений notify копии — собственные значения ноги без зонтика
// (`notify-standalone/values.yaml`) под именем зависимости в `Chart.yaml`
// копии: значения подчарта адресуются этим именем, и инъекция `alias` сменила
// бы и его. Узла почты в слое нет — он приходит только образцом.

const (
	// umbrellaGuardPath — путь отказа стража почтовой полосы зонтика (М45).
	umbrellaGuardPath = "kacho-umbrella/templates/identity-mail-lane-guard.yaml"
	// umbrellaGuardFile — его шаблон от каталога зонтика.
	umbrellaGuardFile = "templates/identity-mail-lane-guard.yaml"
	// notifyImageKnobUmbrella — имя ручки тега образа так, как его задаёт
	// установка зонтика (Д76 (2)).
	notifyImageKnobUmbrella = "notify.image.tag"
)

// umbrellaDep — запись `dependencies` зонтика.
type umbrellaDep struct{ name, alias, repo string }

// umbrellaDeps — записи `dependencies` из `Chart.yaml` каталога dir.
func umbrellaDeps(t *testing.T, dir string) []umbrellaDep {
	t.Helper()
	chart := readYAML(t, filepath.Join(dir, "Chart.yaml"))
	var out []umbrellaDep
	for _, d := range nlist(chart["dependencies"]) {
		m, _ := d.(map[string]any)
		out = append(out, umbrellaDep{nstr(m["name"]), nstr(m["alias"]), nstr(m["repository"])})
	}
	if len(out) == 0 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: в %s/Chart.yaml зависимостей ноль", dir)
	}
	return out
}

// copyMemberDirs — каталоги копии от корня репозитория: зонтик, каталог
// образцов и каталог каждой `file://`-зависимости по пути её `repository` от
// зонтика. Каталога нет либо путь выходит из репозитория — ошибка с именем
// зависимости. Второй возврат — число `file://`-зависимостей.
func copyMemberDirs(repoRoot string, deps []umbrellaDep) ([]string, int, error) {
	umbrellaRel := filepath.Join("deploy", umbrellaDir)
	dirs := []string{umbrellaRel, filepath.Join("deploy", mailNodeSamplesDir)}
	local := 0
	var missing []string
	for _, d := range deps {
		rel, ok := strings.CutPrefix(d.repo, "file://")
		if !ok {
			continue
		}
		local++
		dir := filepath.Clean(filepath.Join(umbrellaRel, rel))
		if dir == ".." || strings.HasPrefix(dir, "../") || filepath.IsAbs(dir) {
			missing = append(missing, d.name+" ("+d.repo+" выходит из репозитория)")
			continue
		}
		if st, err := os.Stat(filepath.Join(repoRoot, dir)); err != nil || !st.IsDir() {
			missing = append(missing, d.name+" ("+d.repo+" → "+dir+")")
			continue
		}
		dirs = append(dirs, dir)
	}
	if len(missing) > 0 {
		return nil, local, fmt.Errorf("каталогов file://-зависимостей нет: %s", strings.Join(missing, ", "))
	}
	if local == 0 {
		return nil, 0, fmt.Errorf("file://-зависимостей у зонтика ноль — копировать нечего")
	}
	return dirs, local, nil
}

// umbrellaCopyOpts — что копия несёт сверх дерева.
type umbrellaCopyOpts struct {
	// fixtureRow — таблица источников notify несёт строку `probe-b` (N02).
	fixtureRow bool
	// notifyEdits — правки чарта notify копии (путь от корня чарта).
	notifyEdits map[string]func(string) string
	// notifyDuplicates — новый файл чарта notify копии → файл, копией
	// которого он заводится (путь от корня чарта).
	notifyDuplicates map[string]string
	// umbrellaEdits — правки зонтика копии (путь от каталога зонтика).
	umbrellaEdits map[string]func(string) string
	// afterBuild — правка уже материализованного зонтика (путь каталога).
	afterBuild func(t *testing.T, umbrella string)
}

// umbrellaCopy — копия зонтика: корень и каталог зонтика в нём.
type umbrellaCopy struct{ root, umbrella string }

// notifyUmbrellaCopy — фикстурная копия зонтика, материализованная владельцем.
func notifyUmbrellaCopy(t *testing.T, opts umbrellaCopyOpts) umbrellaCopy {
	t.Helper()
	requireHelmForNotify(t)
	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	dirs, local, err := copyMemberDirs(repoRoot, umbrellaDeps(t, umbrellaDir))
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: состав фикстурной копии зонтика: %v", err)
	}
	root := t.TempDir()
	files := 0
	for _, dir := range dirs {
		out, gerr := gitenv.Command(repoRoot, "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--", dir).Output()
		if gerr != nil {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: перечень файлов %s не снят: %v", dir, gerr)
		}
		n := 0
		for _, f := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
			if f == "" {
				continue
			}
			body, rerr := os.ReadFile(filepath.Join(repoRoot, f)) // #nosec G304 -- путь из перечня дерева
			if rerr != nil {
				t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: %s не прочитан: %v", f, rerr)
			}
			dst := filepath.Join(root, f)
			if merr := os.MkdirAll(filepath.Dir(dst), 0o750); merr != nil {
				t.Fatal(merr)
			}
			if werr := os.WriteFile(dst, body, 0o600); werr != nil {
				t.Fatal(werr)
			}
			n++
		}
		if n == 0 {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: каталог копии %s пуст — дерево его не ведёт", dir)
		}
		files += n
	}
	t.Logf("копия зонтика: file://-зависимостей %d; каталогов %d (%s); файлов %d",
		local, len(dirs), strings.Join(dirs, ", "), files)

	c := umbrellaCopy{root: root, umbrella: filepath.Join(root, "deploy", umbrellaDir)}
	notifyEdits := map[string]func(string) string{}
	if opts.fixtureRow {
		notifyEdits = fixtureRowEdits()
	}
	for k, f := range opts.notifyEdits {
		if prev, ok := notifyEdits[k]; ok {
			notifyEdits[k] = func(s string) string { return f(prev(s)) }
			continue
		}
		notifyEdits[k] = f
	}
	notifyCopyDir := filepath.Join(root, "deploy", notifyChartDir)
	applyCopyEdits(t, notifyCopyDir, notifyEdits)
	for dst, src := range opts.notifyDuplicates {
		body, rerr := os.ReadFile(filepath.Join(notifyCopyDir, src))
		if rerr != nil {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: копия шаблона %s не снята: %v", src, rerr)
		}
		if _, serr := os.Stat(filepath.Join(notifyCopyDir, dst)); serr == nil {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: %s в чарте notify уже есть — копия его заменила бы", dst)
		}
		if werr := os.WriteFile(filepath.Join(notifyCopyDir, dst), body, 0o600); werr != nil {
			t.Fatal(werr)
		}
	}
	applyCopyEdits(t, c.umbrella, opts.umbrellaEdits)

	owner := filepath.Join(repoRoot, helmDepsOwner)
	cmd := exec.Command("bash", owner, c.umbrella) // #nosec G204 -- владелец материализации из дерева, каталог копии
	out, berr := cmd.CombinedOutput()
	code := 0
	if berr != nil {
		code = -1
		if ee, ok := berr.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
	}
	t.Logf("материализация зависимостей копии: %s %s → код %d", helmDepsOwner, c.umbrella, code)
	if code != 0 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: материализация зависимостей копии — код %d:\n%s", code, out)
	}
	archives, _ := filepath.Glob(filepath.Join(c.umbrella, "charts", "notify-*.tgz"))
	if len(archives) != 1 {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: после материализации архивов notify в копии %d, ожидался один", len(archives))
	}
	t.Logf("материализация копии собрала зависимость notify: %s", filepath.Base(archives[0]))
	if opts.afterBuild != nil {
		opts.afterBuild(t, c.umbrella)
	}
	return c
}

// effectiveDepName — имя, которым копия адресует чарт notify: алиас либо имя.
func (c umbrellaCopy) effectiveDepName(t *testing.T) string {
	t.Helper()
	for _, d := range umbrellaDeps(t, c.umbrella) {
		if strings.TrimPrefix(d.repo, "file://") == "../notify" {
			if d.alias != "" {
				return d.alias
			}
			return d.name
		}
	}
	t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: в Chart.yaml копии нет зависимости file://../notify")
	return ""
}

// notifyLayer — слой значений notify копии; drop — пути ключей, снятые из него.
func (c umbrellaCopy) notifyLayer(t *testing.T, drop ...[]string) string {
	t.Helper()
	own := readYAML(t, notifyStandaloneValues)
	for _, path := range drop {
		parent, _ := ndig(own, path[:len(path)-1]...).(map[string]any)
		if len(path) == 1 {
			parent = own
		}
		if _, has := parent[path[len(path)-1]]; !has {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: снимаемого ключа %s в %s нет", strings.Join(path, "."), notifyStandaloneValues)
		}
		delete(parent, path[len(path)-1])
	}
	body, err := yaml.Marshal(map[string]any{c.effectiveDepName(t): own})
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "notify-layer.yaml")
	if err := os.WriteFile(p, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// chainFiles — элементы цепочки chain обёртки D9, разрешённые в копии.
// Элемента нет в копии — «НЕ ВЫПОЛНИЛОСЬ».
func (c umbrellaCopy) chainFiles(t *testing.T, chain string) []string {
	t.Helper()
	elems, ok := deployStacksForRender(t, "operator.yaml")[chain]
	if !ok {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: цепочки %s в таблице нет", chain)
	}
	var out []string
	for _, e := range elems {
		p := filepath.Join(c.umbrella, e)
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: элемент цепочки %s (%s) в копии не существует", e, chain)
		}
		out = append(out, p)
	}
	return out
}

// render — `helm template` копии зонтика цепочкой файлов и наборами.
func (c umbrellaCopy) render(files []string, sets ...string) (string, error) {
	args := []string{"template", "kacho-umbrella", c.umbrella, "-n", "kacho"}
	for _, f := range files {
		args = append(args, "-f", f)
	}
	for _, s := range sets {
		args = append(args, "--set", s)
	}
	out, err := exec.Command("helm", args...).CombinedOutput() // #nosec G204 -- фиксированный бинарь, аргументы пробы
	return string(out), err
}

// mustRender — рендер копии, который обязан пройти (опора, близнец).
func mustRender(t *testing.T, c umbrellaCopy, files []string, sets ...string) string {
	t.Helper()
	out, err := c.render(files, sets...)
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: опорный рендер копии зонтика отказал: %v\n%s", err, lastLines(out, 5))
	}
	return out
}

// negationRemoves — РЕШЕНИЕ предпосылки отрицания: наборы sets снимают ровно
// ключ want (`<ключ>=null`). Иное — ошибка с именами всех снятых ключей.
func negationRemoves(sets []string, want string) error {
	var removed []string
	for _, s := range sets {
		if k, ok := strings.CutSuffix(s, "=null"); ok {
			removed = append(removed, k)
		}
	}
	if len(removed) != 1 || removed[0] != want {
		return fmt.Errorf("снято ключей %d (%s), а отрицание снимает ровно %s",
			len(removed), strings.Join(removed, ", "), want)
	}
	return nil
}

// runUmbrellaMailNodeLeg — опора (образец целиком) и отрицание (образец минус
// sets) на цепочке `prod` копии со слоем notify.
func runUmbrellaMailNodeLeg(t *testing.T, c umbrellaCopy, wantPath, address string, sets ...string) (legOutcome, string) {
	t.Helper()
	if err := negationRemoves(sets, notifyMailNodeKey); err != nil {
		return legNotRun, err.Error()
	}
	files := append(c.chainFiles(t, prodChainName), c.notifyLayer(t))
	support, serr := c.render(files)
	neg, nerr := c.render(files, sets...)
	return mailNodeLeg(support, serr, neg, nerr, wantPath, address)
}

// notifySourceCount — объекты рендера зонтика, рождённые чартом notify (любым
// именем зависимости, несущим шаблоны `charts/notify*/`).
func notifySourceCount(objs []renderedObj) int {
	n := 0
	for _, o := range objs {
		if strings.HasPrefix(o.source, "kacho-umbrella/charts/notify") {
			n++
		}
	}
	return n
}

func sampleAddress(t *testing.T) string {
	t.Helper()
	a := nstr(ndig(readYAML(t, notifyStandaloneSample), "global", "kacho", "identity", "smtp", "connectionURI"))
	if a == "" {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: образец %s без адреса узла", notifyStandaloneSample)
	}
	return a
}

// TestNotifyMailNodeReachesTheProcessThroughTheUmbrella — нога фикстурной
// копии зонтика: `prod` через обёртку, из образца снят ровно адрес узла.
func TestNotifyMailNodeReachesTheProcessThroughTheUmbrella(t *testing.T) {
	address := sampleAddress(t)
	neg := notifyMailNodeKey + "=null"
	c := notifyUmbrellaCopy(t, umbrellaCopyOpts{fixtureRow: true})

	// Предпосылка (CX1-94 (б)): снятые ключи и код рендера образца целиком.
	files := append(c.chainFiles(t, prodChainName), c.notifyLayer(t))
	if out, err := c.render(files); err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: рендер prod образца целиком отказал: %v\n%s", err, out)
	}
	t.Logf("предпосылка: цепочка prod %v + слой notify; образец %s; снятые ключи [%s]; рендер образца целиком — код 0",
		deployStacksForRender(t, "operator.yaml")[prodChainName], notifyStandaloneSample, notifyMailNodeKey)

	outcome, why := runUmbrellaMailNodeLeg(t, c, notifyUmbrellaConfigMapPath, address, neg)
	switch outcome {
	case legNotRun:
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: %s", why)
	case legRed:
		t.Errorf("узел почты не доезжает до процесса через зонтик: %s", why)
	default:
		t.Logf("нога зонтика: %s", why)
	}

	// Константа чужой ноги → «НЕ ВЫПОЛНИЛОСЬ» с фактическим путём.
	if o, w := runUmbrellaMailNodeLeg(t, c, notifyStandaloneConfigMapPath, address, neg); o != legNotRun {
		t.Errorf("константа ноги без зонтика, поданная ноге зонтика: ожидалось НЕ ВЫПОЛНИЛОСЬ, получено %s (%s)", o, w)
	} else {
		t.Logf("константа чужой ноги → НЕ ВЫПОЛНИЛОСЬ: %s", w)
	}

	// Снято два ключа → «НЕ ВЫПОЛНИЛОСЬ» с обоими именами.
	two := "global.kacho.identity.smtp.fromAddress=null"
	if o, w := runUmbrellaMailNodeLeg(t, c, notifyUmbrellaConfigMapPath, address, neg, two); o != legNotRun ||
		!strings.Contains(w, notifyMailNodeKey) || !strings.Contains(w, "fromAddress") {
		t.Errorf("снято два ключа: ожидалось НЕ ВЫПОЛНИЛОСЬ с обоими именами, получено %s (%s)", o, w)
	} else {
		t.Logf("снято два ключа → НЕ ВЫПОЛНИЛОСЬ: %s", w)
	}

	cmKey := notifyConnectionEnv + ": {{ tpl (required"
	swapLine := func(repl string) func(string) string {
		return func(s string) string {
			i := strings.Index(s, cmKey)
			if i < 0 {
				return s
			}
			return s[:i] + notifyConnectionEnv + ": " + repl + "\n" + s[strings.Index(s[i:], "\n")+i+1:]
		}
	}
	guardCopyPath := "kacho-umbrella/charts/kaname/charts/guardfirst/" + umbrellaGuardFile
	cases := []struct {
		name string
		opts umbrellaCopyOpts
		want legOutcome
		// path — путь, который обязан стоять в причине «НЕ ВЫПОЛНИЛОСЬ».
		path []string
	}{
		{"ключ из пути вне узла", umbrellaCopyOpts{notifyEdits: map[string]func(string) string{
			"templates/configmap.yaml": swapLine("{{ .Values.origin | quote }}")}}, legRed, nil},
		// Снятый `required` и `default`: notify отказа не даёт, и отказывает
		// страж зонтика — «НЕ ВЫПОЛНИЛОСЬ» с его путём, а не зелёный.
		{"снятый required", umbrellaCopyOpts{notifyEdits: map[string]func(string) string{
			"templates/configmap.yaml": swapLine("{{ tpl (.Values.global.kacho.identity.smtp.connectionURI | toString) . | quote }}")}},
			legNotRun, []string{umbrellaGuardPath}},
		{"default у ключа", umbrellaCopyOpts{notifyEdits: map[string]func(string) string{
			"templates/configmap.yaml": swapLine(`{{ tpl (.Values.global.kacho.identity.smtp.connectionURI | default "smtp://fallback:25/") . | quote }}`)}},
			legNotRun, []string{umbrellaGuardPath}},
		{"страж первым", umbrellaCopyOpts{afterBuild: func(t *testing.T, u string) {
			t.Helper()
			sub := filepath.Join(u, "charts", "kaname", "charts", "guardfirst")
			body, err := os.ReadFile(filepath.Join(u, umbrellaGuardFile))
			if err != nil {
				t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: шаблон стража зонтика не прочитан: %v", err)
			}
			if err := os.MkdirAll(filepath.Join(sub, "templates"), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(sub, "Chart.yaml"), []byte("apiVersion: v2\nname: guardfirst\nversion: 0.0.1\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(sub, umbrellaGuardFile), body, 0o600); err != nil {
				t.Fatal(err)
			}
		}}, legNotRun, []string{guardCopyPath}},
		{"alias notifyx", umbrellaCopyOpts{umbrellaEdits: map[string]func(string) string{
			"Chart.yaml": replaceOnce("  - name: notify\n", "  - name: notify\n    alias: notifyx\n")}},
			legNotRun, []string{"kacho-umbrella/charts/notifyx/templates/configmap.yaml"}},
		{"копия шаблона ConfigMap под другим именем", umbrellaCopyOpts{notifyDuplicates: map[string]string{
			"templates/configmap-copy.yaml": "templates/configmap.yaml"}},
			legNotRun, []string{notifyUmbrellaConfigMapPath, "kacho-umbrella/charts/notify/templates/configmap-copy.yaml"}},
	}
	for _, cs := range cases {
		cs.opts.fixtureRow = true
		ic := notifyUmbrellaCopy(t, cs.opts)
		o, w := runUmbrellaMailNodeLeg(t, ic, notifyUmbrellaConfigMapPath, address, neg)
		pathsOK := true
		for _, p := range cs.path {
			if !strings.Contains(w, p) {
				pathsOK = false
			}
		}
		if o != cs.want || !pathsOK {
			t.Errorf("инъекция %q: ожидалось %s с путями %v, получено %s (%s)", cs.name, cs.want, cs.path, o, w)
			continue
		}
		t.Logf("инъекция %q → %s: %s", cs.name, o, w)
	}

	// Близнец — перечень пуст: notify нет, отказа нет.
	empty := notifyUmbrellaCopy(t, umbrellaCopyOpts{})
	out, err := empty.render(append(empty.chainFiles(t, prodChainName), empty.notifyLayer(t)))
	if err != nil {
		t.Errorf("перечень пуст: рендер prod отказал — отказа быть не должно: %v\n%s", err, out)
	} else if n := notifySourceCount(parseRendered(t, out)); n != 0 {
		t.Errorf("перечень пуст, а объектов notify в рендере зонтика %d", n)
	} else {
		t.Logf("близнец «перечень пуст»: код 0, объектов notify 0")
	}
}

// TestNotifyUmbrellaCopyCompositionNamesAMissingDependency — состав копии
// выводится разбором `dependencies`: каталога зависимости нет либо путь выходит
// из репозитория — ошибка с именем зависимости; близнец — записи дерева.
func TestNotifyUmbrellaCopyCompositionNamesAMissingDependency(t *testing.T) {
	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	deps := umbrellaDeps(t, umbrellaDir)
	declared := 0
	for _, d := range deps {
		if strings.HasPrefix(d.repo, "file://") {
			declared++
		}
	}
	dirs, local, err := copyMemberDirs(repoRoot, deps)
	if err != nil {
		t.Fatalf("состав копии дерева: %v", err)
	}
	if local != declared || len(dirs) != declared+2 {
		t.Errorf("состав копии: file://-записей в Chart.yaml %d, учтено %d, каталогов %d (ожидалось %d)",
			declared, local, len(dirs), declared+2)
	}
	t.Logf("file://-зависимостей %d; каталоги копии: %s", local, strings.Join(dirs, ", "))

	for _, ghost := range []umbrellaDep{
		{name: "ghost", repo: "file://../ghost"},
		{name: "escape", repo: "file://../../../../outside"},
	} {
		_, _, gerr := copyMemberDirs(repoRoot, append(append([]umbrellaDep(nil), deps...), ghost))
		if gerr == nil || !strings.Contains(gerr.Error(), ghost.name) {
			t.Errorf("инъекция «зависимость %s (%s)»: ожидалась ошибка с её именем, получено %v", ghost.name, ghost.repo, gerr)
		} else {
			t.Logf("инъекция %q → %v", ghost.name, gerr)
		}
	}
}

// ─── пустой выведенный перечень — закрытый детерминированный исход (Д76 (1)) ─

// inventedSourcesLayer — слой, называющий источник во всех местах, откуда
// перечень мог бы его «взять»: ручного перечня у notify нет (NTF1-N03), и
// пустой вывод обязан остаться пустым перечнем при любом таком слое.
func inventedSourcesLayer(t *testing.T, under string) (string, int) {
	t.Helper()
	src := []any{map[string]any{"module": "invented", "feedAddr": "invented:9091",
		"san": "spiffe://kacho.cloud/ns/kacho/sa/invented", "classes": []any{"notice"},
		"recipientForms": []any{}, "authorization": "resolveSend"}}
	own := map[string]any{"sources": src, "pluggableSources": src, "sourceRoster": src}
	global := map[string]any{"kacho": map[string]any{
		"notify":        map[string]any{"sources": src},
		"notifications": map[string]any{"sources": src, "enabled": true},
	}}
	doc := map[string]any{"global": global}
	if under == "" {
		for k, v := range own {
			doc[k] = v
		}
	} else {
		doc[under] = own
	}
	body, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "invented-sources.yaml")
	if err := os.WriteFile(p, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return p, len(own) + 3
}

// rosterOffSets — наборы «перечень пуст по флагу»: установка выключена,
// переопределения модулей сняты (замысел З28, NTF1-N04).
var rosterOffSets = []string{
	"global.kacho.notifications.enabled=false",
	"global.kacho.notifications.modules=null",
}

// TestNotifyEmptyRosterIsAClosedDeterministicOutcome — выведенный перечень
// пуст (нога без зонтика: единственный источник дерева выключен
// переопределением; цепочки: флаг установки выключен): рендер не падает,
// объектов notify 0, источников не выдумывает (слой, называющий источник,
// ничего не меняет), два рендера побайтно равны; то же — в каждой цепочке
// таблицы стендов на копии зонтика. Инъекция — перечень, берущий источники из
// значений при пустом выводе, → красный.
func TestNotifyEmptyRosterIsAClosedDeterministicOutcome(t *testing.T) {
	judge := func(chart string) (int, string, error) {
		layer, _ := inventedSourcesLayer(t, "")
		files := append(standaloneLeg(), layer)
		a, aerr := renderNotify(t, chart, files)
		b, berr := renderNotify(t, chart, files)
		if aerr != nil || berr != nil {
			return 0, a + b, fmt.Errorf("рендер отказал: %v / %v", aerr, berr)
		}
		if a != b {
			return 0, "", fmt.Errorf("два рендера одних значений различаются")
		}
		return len(parseRendered(t, a)), a, nil
	}
	n, out, err := judge(notifyChartDir)
	switch {
	case err != nil:
		t.Errorf("перечень пуст: %v\n%s", err, out)
	case n != 0:
		t.Errorf("перечень пуст и слой, называющий источник: объектов notify %d — перечень выдуман из значений", n)
	default:
		_, places := inventedSourcesLayer(t, "")
		t.Logf("перечень пуст, слой с источником в %d местах: код 0, объектов 0, два рендера равны", places)
	}

	fallback := `{{- if $out }}{{ $out | toJson }}{{ else }}{{ dig "kacho" "notifications" "sources" (list) $global | toJson }}{{ end -}}`
	inj := notifyChartCopy(t, map[string]func(string) string{
		"templates/_sources.tpl": replaceOnce(`{{- $out | toJson -}}`, fallback),
	})
	if n, _, err := judge(inj); err == nil && n == 0 {
		t.Errorf("инъекция «перечень из значений при пустом выводе»: проба промолчала")
	} else {
		t.Logf("инъекция «перечень из значений при пустом выводе» → красный: объектов %d, %v", n, err)
	}

	// Каждая цепочка таблицы стендов на копии зонтика (prod — через обёртку).
	c := notifyUmbrellaCopy(t, umbrellaCopyOpts{})
	chains := deployStacksForRender(t, "operator.yaml")
	names := make([]string, 0, len(chains))
	for name := range chains {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		layer, _ := inventedSourcesLayer(t, c.effectiveDepName(t))
		files := append(c.chainFiles(t, name), layer)
		// Перечень пуст по флагу: установка выключена, переопределений нет.
		out, err := c.render(files, rosterOffSets...)
		if err != nil {
			t.Errorf("цепочка %s, перечень пуст по флагу: рендер зонтика отказал: %v\n%s", name, err, lastLines(out, 5))
			continue
		}
		if k := notifySourceCount(parseRendered(t, out)); k != 0 {
			t.Errorf("цепочка %s, перечень пуст по флагу: объектов notify %d", name, k)
			continue
		}
		t.Logf("цепочка %s: код 0, объектов notify 0", name)
	}
	if len(names) == 0 {
		t.Fatal("НЕ ВЫПОЛНИЛОСЬ: цепочек ноль")
	}
}

func lastLines(s string, n int) string {
	l := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(l) > n {
		l = l[len(l)-n:]
	}
	return strings.Join(l, "\n")
}

// ─── notify включён без тега образа — отказ рендера с именем ручки (Д76 (2)) ─

// imageTagRefusal — РЕШЕНИЕ: рендер отказал шаблоном wantPath с именем ручки
// knob. Код 0 — КРАСНЫЙ; иной шаблон либо путь не разобран — «НЕ ВЫПОЛНИЛОСЬ».
func imageTagRefusal(out string, err error, wantPath, knob string) (legOutcome, string) {
	if err == nil {
		return legRed, "notify рендерится без тега образа (код 0)"
	}
	m := execErrorPathRe.FindStringSubmatch(out)
	switch {
	case m == nil:
		return legNotRun, "путь отказа не разобран: " + lastLines(out, 3)
	case m[1] != wantPath:
		return legNotRun, "отказал шаблон " + m[1] + ", а не " + wantPath + ": " + lastLines(out, 3)
	case !strings.Contains(out, knob):
		return legRed, "отказ без имени ручки " + knob + ": " + lastLines(out, 3)
	}
	return legHeld, "отказ " + m[1] + " с именем " + knob
}

// notifyImagesCarryTag — у каждого контейнера рабочей нагрузки notify образ
// с непустым тегом tag.
func notifyImagesCarryTag(objs []renderedObj, tag string) []string {
	var out []string
	n := 0
	for _, d := range objsOfKind(objs, "Deployment") {
		for _, c := range containersOf(nPodSpec(d)) {
			n++
			if img := nstr(c["image"]); !strings.HasSuffix(img, ":"+tag) {
				out = append(out, "контейнер "+nstr(c["name"])+": образ "+img+" без тега "+tag)
			}
		}
	}
	if n == 0 {
		out = append(out, "контейнеров notify 0 — судить нечего")
	}
	return out
}

// TestNotifyRefusesToRenderWithoutAnImageTag — notify включён (таблица с
// источником), тег пуст → отказ рендера шаблоном развёртывания с именем ручки;
// на копии зонтика — `notify.image.tag`, так её задаёт установка. Близнец —
// тег задан → у каждого контейнера образ с тегом. Инъекция — снятый `required`
// → красный.
func TestNotifyRefusesToRenderWithoutAnImageTag(t *testing.T) {
	const standaloneDeployment = "notify/templates/deployment.yaml"
	const umbrellaDeployment = "kacho-umbrella/charts/notify/templates/deployment.yaml"

	chart := notifyFixtureChart(t, nil)
	out, err := renderNotify(t, chart, standaloneLeg(), "image.tag=")
	switch o, w := imageTagRefusal(out, err, standaloneDeployment, "image.tag"); o {
	case legNotRun:
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: нога без зонтика: %s", w)
	case legRed:
		t.Errorf("нога без зонтика: %s", w)
	default:
		t.Logf("нога без зонтика: %s", w)
	}
	if f := notifyImagesCarryTag(mustRenderNotify(t, chart, standaloneLeg()), "fixture"); len(f) != 0 {
		t.Errorf("близнец (тег задан): %s", strings.Join(f, "; "))
	}

	c := notifyUmbrellaCopy(t, umbrellaCopyOpts{fixtureRow: true})
	files := append(c.chainFiles(t, prodChainName), c.notifyLayer(t, []string{"image", "tag"}))
	out, err = c.render(files)
	switch o, w := imageTagRefusal(out, err, umbrellaDeployment, notifyImageKnobUmbrella); o {
	case legNotRun:
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: нога зонтика: %s", w)
	case legRed:
		t.Errorf("нога зонтика: %s", w)
	default:
		t.Logf("нога зонтика (prod, тег базового слоя пуст): %s", w)
	}

	twin := parseRendered(t, mustRender(t, c, append(c.chainFiles(t, prodChainName), c.notifyLayer(t))))
	var own []renderedObj
	for _, o := range twin {
		if strings.HasPrefix(o.source, "kacho-umbrella/charts/notify/") {
			own = append(own, o)
		}
	}
	if f := notifyImagesCarryTag(own, "fixture"); len(f) != 0 {
		t.Errorf("близнец зонтика (тег задан): %s", strings.Join(f, "; "))
	}

	noRequired := regexp.MustCompile(`\(required "[^"]*" \.Values\.image\.tag\)`)
	inj := notifyFixtureChart(t, map[string]func(string) string{
		"templates/_helpers.tpl": func(s string) string { return noRequired.ReplaceAllString(s, ".Values.image.tag") },
	})
	out, err = renderNotify(t, inj, standaloneLeg(), "image.tag=")
	if o, w := imageTagRefusal(out, err, standaloneDeployment, "image.tag"); o != legRed {
		t.Errorf("инъекция «снятый required у тега»: ожидался красный, получено %s (%s)", o, w)
	} else {
		t.Logf("инъекция «снятый required у тега» → красный: %s", w)
	}
}
