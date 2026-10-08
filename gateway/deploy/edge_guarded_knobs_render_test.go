// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// edge_guarded_knobs_render_test.go — каждая ручка, которую страж старта края
// требует без умолчания, выводится рендером на КАЖДОЙ цепочке, где край поднят,
// и рендер с этим окружением проходит самого стража (приёмка NTF-2 Р5, Р8;
// замысел issue-2917 З8, З9, З28; kacho#2917).
//
// # Чего не хватало
//
// Страж старта края (`config.ResolveEdgeLimits`, `config.ReadAnonMailPoWKey`)
// требует 19 ключей и файл ключа подписи вызовов, умолчаний у процесса нет. Чарт
// их не выводил: под не стартовал ни на одной цепочке, а рендер и проба
// двухколоночного гейта (`knob_producer_parity_test.go`) были зелёными — тот судит
// «назад» (эмиссия без читателя), а «вперёд» для ручек без умолчания не судит
// вовсе. Отказ был виден только на поднятом стенде, по сроку готовности.
//
// # Что судится
//
//	(1) перечень обязательных ручек берётся у САМОГО стража — из текста его
//	    отказа на пустой конфигурации, а не из списка в пробе;
//	(2) на рендере чарта без зонтика и каждой цепочки `deploy/stacks.txt` (у
//	    `prod` — со слоем оператора) под края несёт каждую ручку перечня, и
//	    конфигурация, собранная из его окружения, проходит стража; файл ключа
//	    лежит в смонтированном томе объекта Secret;
//	(3) число доверенных прыжков приходит из файла профиля стенда (у `prod` — из
//	    слоя оператора), а не из базы зонта, базы чарта края или `values.prod.yaml`;
//	(4) инъекции «снятая строка» — по каждому листу `anonMail` базы чарта, по
//	    строке прыжков в `values.own-stand.yaml` (файл площадки стенда `own`) и
//	    в слое оператора `prod` — рендер отказывает, и текст отказа называет
//	    ручку; значение `0` выводится, а не теряется.
package deploy_test

import (
	"errors"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// guardedKnobName — имя ручки в тексте отказа стража: «<имя> не задана».
var guardedKnobName = regexp.MustCompile(`\b(KACHO_[A-Z0-9_]+) не задана`)

// edgeGuardedKnobs — ручки, которые страж старта края требует без умолчания:
// перечень выводится из ОТКАЗА стража на пустой конфигурации.
func edgeGuardedKnobs(t *testing.T) []string {
	t.Helper()
	set := map[string]bool{}
	_, limErr := config.ResolveEdgeLimits(config.Config{})
	_, keyErr := config.ReadAnonMailPoWKey(config.Config{})
	for _, err := range []error{limErr, keyErr} {
		if err == nil {
			t.Fatal("страж края принял ПУСТУЮ конфигурацию — перечня обязательных ручек нет, судить не о чем")
		}
		for _, m := range guardedKnobName.FindAllStringSubmatch(err.Error(), -1) {
			set[m[1]] = true
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	if len(out) == 0 {
		t.Fatal("из отказа стража не прочитано ни одной ручки — распознаватель текста отказа ослеп")
	}
	return out
}

// edgeContainerEnv — окружение контейнера края в рендере (имя → значение) и
// путь файла каждого ключа объекта Secret, смонтированного томом.
type edgeContainerEnv struct {
	env        map[string]string
	secretFile map[string]string // абсолютный путь файла → «<объект>/<ключ>»
}

func readEdgeContainer(t *testing.T, rendered string) (edgeContainerEnv, bool) {
	t.Helper()
	out := edgeContainerEnv{env: map[string]string{}, secretFile: map[string]string{}}
	for _, d := range decodeEdgeDocs(rendered) {
		if d["kind"] != "Deployment" {
			continue
		}
		meta, _ := d["metadata"].(map[string]any)
		if meta["name"] != "api-gateway" {
			continue
		}
		spec := nestedMap(d, "spec", "template", "spec")
		volumes := map[string]map[string]string{} // том → ключ → путь
		vnames := map[string]string{}
		for _, v := range asSlice(spec["volumes"]) {
			vm, _ := v.(map[string]any)
			sec, ok := vm["secret"].(map[string]any)
			if !ok {
				continue
			}
			name, _ := vm["name"].(string)
			vnames[name], _ = sec["secretName"].(string)
			volumes[name] = map[string]string{}
			for _, it := range asSlice(sec["items"]) {
				im, _ := it.(map[string]any)
				k, _ := im["key"].(string)
				p, _ := im["path"].(string)
				volumes[name][k] = p
			}
		}
		for _, c := range asSlice(spec["containers"]) {
			cm, _ := c.(map[string]any)
			for _, e := range asSlice(cm["env"]) {
				em, _ := e.(map[string]any)
				n, _ := em["name"].(string)
				v, _ := em["value"].(string)
				out.env[n] = v
			}
			for _, m := range asSlice(cm["volumeMounts"]) {
				mm, _ := m.(map[string]any)
				vol, _ := mm["name"].(string)
				dir, _ := mm["mountPath"].(string)
				for k, p := range volumes[vol] {
					out.secretFile[dir+"/"+p] = vnames[vol] + "/" + k
				}
			}
		}
		return out, true
	}
	return out, false
}

// configFromEnv — конфигурация края из окружения рендера: строковое поле
// получает значение переменной своего тега. Тот же тег читает загрузчик.
func configFromEnv(env map[string]string) config.Config {
	var cfg config.Config
	v := reflect.ValueOf(&cfg).Elem()
	ty := v.Type()
	for i := 0; i < ty.NumField(); i++ {
		tag := ty.Field(i).Tag.Get("envconfig")
		if val, ok := env[tag]; ok && tag != "" && v.Field(i).Kind() == reflect.String {
			v.Field(i).SetString(val)
		}
	}
	return cfg
}

// judgeGuardedRender — находки рендера одной цепочки против перечня стража.
func judgeGuardedRender(label string, guarded []string, got edgeContainerEnv) []string {
	var findings []string
	for _, k := range guarded {
		if strings.TrimSpace(got.env[k]) == "" {
			findings = append(findings, fmt.Sprintf("%s: ручки %s в поде края нет — страж старта откажет, "+
				"под не стартует", label, k))
		}
	}
	if len(findings) > 0 {
		return findings
	}
	if _, err := config.ResolveEdgeLimits(configFromEnv(got.env)); err != nil {
		findings = append(findings, fmt.Sprintf("%s: окружение рендера не проходит стража края: %v", label, err))
	}
	keyFile := got.env[config.AnonMailPoWKeyFileKnob]
	if _, mounted := got.secretFile[keyFile]; !mounted {
		findings = append(findings, fmt.Sprintf("%s: %s = %q не указывает на файл смонтированного объекта Secret "+
			"(смонтировано: %v)", label, config.AnonMailPoWKeyFileKnob, keyFile, got.secretFile))
	}
	return findings
}

// hopsSource — файл цепочки, последним задающий число прыжков; пусто — ни один.
func hopsSource(trees []edgeChainTree) string {
	src := ""
	for _, ct := range trees {
		if sub, ok := ct.tree["api-gateway"].(map[string]any); ok {
			if _, has := sub["trustedHops"]; has {
				src = ct.file
			}
		}
	}
	return src
}

// hopsForbiddenSources — файлы, из которых число прыжков приходить не вправе:
// база зонта и профили поставки `prod`. Профили поставки берутся из таблицы
// цепочек, а не выписываются: копия цепочки разошлась бы с таблицей молча.
func hopsForbiddenSources(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{"values.yaml": true}
	prod, ok := deployableStacks(t)[edgeOperatorSampleChain]
	if !ok || len(prod) == 0 {
		t.Fatalf("цепочки %s в таблице нет — судить источник прыжков не с чем", edgeOperatorSampleChain)
	}
	for _, p := range prod {
		out[p] = true
	}
	return out
}

// judgeHopsSource — число прыжков не из базы зонта и не из поставки `prod`.
func judgeHopsSource(label, src string, forbidden map[string]bool) []string {
	if forbidden[src] {
		return []string{fmt.Sprintf("%s: число доверенных прыжков приходит из %s — его молча унаследовала бы "+
			"каждая установка (Д51); место значения — файл профиля стенда либо слой оператора", label, src)}
	}
	return nil
}

func TestEdgeGuardedKnobs_EveryChainRendersWhatTheGuardDemands(t *testing.T) {
	guarded := edgeGuardedKnobs(t)
	t.Logf("страж края требует без умолчания %d ручек: %s", len(guarded), strings.Join(guarded, ", "))

	if _, has := gatewayChartValues(t)["trustedHops"]; has {
		t.Errorf("база чарта края (gateway/deploy/values.yaml) объявляет trustedHops — число прыжков молча " +
			"унаследовала бы каждая установка (Д51)")
	}

	forbidden := hopsForbiddenSources(t)
	chains := edgeAlertChains(t)
	for _, c := range chains {
		label := "чарт края без зонтика"
		src := edgeStandaloneLayer
		if c.chain != nil {
			label = fmt.Sprintf("цепочка %s", c.name)
			src = hopsSource(edgeChainTrees(t, c))
		}
		out, err := renderEdgeChain(t, c)
		if err != nil {
			t.Fatalf("%s: рендер не выполнен (%v) — условие не создано, вердикта нет:\n%s", label, err, out)
		}
		got, found := readEdgeContainer(t, out)
		if !found {
			t.Fatalf("%s: в рендере нет пода края — смотреть было не на что", label)
		}
		for _, f := range append(judgeGuardedRender(label, guarded, got), judgeHopsSource(label, src, forbidden)...) {
			t.Error(f)
		}
		t.Logf("  %s: ручек стража в поде %d из %d; %s=%q из %s; ключ — %s",
			label, countPresent(guarded, got.env), len(guarded), config.TrustedHopsKnob,
			got.env[config.TrustedHopsKnob], src, got.secretFile[got.env[config.AnonMailPoWKeyFileKnob]])
	}
	t.Logf("осмотрено рендеров %d (чарт без зонтика + цепочек %d)", len(chains), len(chains)-1)
}

func countPresent(names []string, env map[string]string) int {
	n := 0
	for _, k := range names {
		if strings.TrimSpace(env[k]) != "" {
			n++
		}
	}
	return n
}

// Инъекция «снятая строка» по КАЖДОМУ листу `anonMail` базы чарта: рендер
// отказывает, и текст отказа называет ручку (у листа, ручкой не являющегося, —
// его ключ). Множество названных ручек равно перечню стража без числа прыжков:
// лист, чьё снятие ничего не называет, — находка.
func TestEdgeGuardedKnobs_RemovedLineRefusesWithTheKnobName(t *testing.T) {
	guarded := edgeGuardedKnobs(t)
	base, _ := gatewayChartValues(t)["anonMail"].(map[string]any)
	if len(base) == 0 {
		t.Fatal("в базе чарта края нет узла anonMail — снимать нечего, и проба судила бы ничего")
	}
	leaves := flattenLeaves("", base)
	named := map[string]bool{}
	for _, leaf := range leaves {
		out, err := renderEdgeLayers(t, true, []map[string]any{nullAt("anonMail." + leaf)})
		if err == nil {
			t.Errorf("лист anonMail.%s снят — рендер прошёл: строка без значения ушла бы в под пустой", leaf)
			continue
		}
		m := regexp.MustCompile(`\b(KACHO_[A-Z0-9_]+) не задана`).FindStringSubmatch(out)
		switch {
		case m != nil:
			named[m[1]] = true
			t.Logf("  снят anonMail.%s → отказ называет %s", leaf, m[1])
		case strings.Contains(out, "anonMail."+leaf):
			t.Logf("  снят anonMail.%s → отказ называет ключ anonMail.%s", leaf, leaf)
		default:
			t.Errorf("лист anonMail.%s снят — рендер отказал, но не назвал ни ручки, ни ключа:\n%s", leaf, out)
		}
	}
	for _, k := range guarded {
		if k != config.TrustedHopsKnob && !named[k] {
			t.Errorf("ручка %s требуется стражем, а снятие ни одного листа anonMail её не назвало — "+
				"её значение приходит не из базы чарта либо отказ рендера её не называет", k)
		}
	}
	t.Logf("листов anonMail снято %d; названо ручек %d из %d (без числа прыжков)",
		len(leaves), len(named), len(guarded)-1)
}

// Число прыжков: строка снята из `values.own-stand.yaml` → рендер `own` отказывает
// с именем ручки, близнец — `1` из `values.own-stand.yaml`; строка снята из слоя
// оператора `prod` → отказ, при том что тот же образец целиком рендерится;
// значение `0` выводится как "0"; судья источника краснеет на значении в базе
// зонта и молчит на файле профиля.
func TestEdgeGuardedKnobs_HopsComeFromTheStandProfileOnly(t *testing.T) {
	chains := map[string]edgeAlertChain{}
	for _, c := range edgeAlertChains(t) {
		chains[c.name] = c
	}
	for _, name := range []string{"own", edgeOperatorSampleChain} {
		c, ok := chains[name]
		if !ok {
			t.Fatalf("цепочки %s в таблице нет — инъекции некуда идти", name)
		}
		trees := edgeChainTrees(t, c)
		src := hopsSource(trees)
		if src == "" {
			t.Fatalf("цепочка %s: ни один слой не задаёт число прыжков — близнеца нет", name)
		}
		out, err := renderEdgeLayers(t, false, edgeLayersOf(trees))
		if err != nil {
			t.Fatalf("близнец %s: рендер с числом прыжков из %s отказал:\n%s", name, src, out)
		}
		twin, _ := readEdgeContainer(t, out)
		t.Logf("  близнец %s: %s=%q из %s", name, config.TrustedHopsKnob, twin.env[config.TrustedHopsKnob], src)

		cut := withoutHops(trees, src)
		if left := hopsSource(cut); left != "" {
			t.Fatalf("инъекция %s: после снятия строки из %s значение всё ещё приходит из %s — снято не одно", name, src, left)
		}
		out, err = renderEdgeLayers(t, false, edgeLayersOf(cut))
		if err == nil || !strings.Contains(out, config.TrustedHopsKnob) {
			t.Errorf("инъекция «строка снята из %s» (цепочка %s): рендер обязан отказать с именем %s (err=%v)",
				src, name, config.TrustedHopsKnob, err)
		} else {
			t.Logf("  снята строка из %s → отказ рендера называет %s", src, config.TrustedHopsKnob)
		}
	}

	zero := edgeLayersOf(edgeChainTrees(t, chains["own"]))
	zero = append(zero, map[string]any{"trustedHops": 0})
	out, err := renderEdgeLayers(t, false, zero)
	if err != nil {
		t.Fatalf("вариант «значение 0»: рендер отказал — законное значение потеряно:\n%s", out)
	}
	if got, _ := readEdgeContainer(t, out); got.env[config.TrustedHopsKnob] != "0" {
		t.Errorf("вариант «значение 0»: %s = %q, ожидалось \"0\"", config.TrustedHopsKnob, got.env[config.TrustedHopsKnob])
	}

	forbidden := hopsForbiddenSources(t)
	for src := range forbidden {
		if f := judgeHopsSource("инъекция", src, forbidden); len(f) != 1 {
			t.Errorf("судья источника молчит на значении в %s (база зонта либо поставка prod): %q", src, f)
		}
	}
	ownSrc := hopsSource(edgeChainTrees(t, chains["own"]))
	if f := judgeHopsSource("близнец", ownSrc, forbidden); len(f) != 0 {
		t.Errorf("судья источника краснеет на файле профиля стенда %s: %q", ownSrc, f)
	}
}

// Инъекция на синтетическом рендере: судья находит отсутствующую ручку и файл
// ключа вне смонтированного тома; на полном окружении молчит.
func TestEdgeGuardedKnobs_JudgeFiresAndStaysSilent(t *testing.T) {
	guarded := edgeGuardedKnobs(t)
	out, err := renderEdgeChain(t, edgeAlertChain{name: "chart"})
	if err != nil {
		t.Fatalf("рендер чарта без зонтика не выполнен: %v\n%s", err, out)
	}
	full, _ := readEdgeContainer(t, out)
	if f := judgeGuardedRender("близнец", guarded, full); len(f) != 0 {
		t.Fatalf("законный близнец объявлен находкой: %q", f)
	}
	for _, k := range guarded {
		cut := edgeContainerEnv{env: map[string]string{}, secretFile: full.secretFile}
		for n, v := range full.env {
			if n != k {
				cut.env[n] = v
			}
		}
		f := judgeGuardedRender("инъекция", guarded, cut)
		if len(f) == 0 || !strings.Contains(strings.Join(f, "\n"), k) {
			t.Errorf("снятая ручка %s не найдена судьёй: %q", k, f)
		}
	}
	moved := edgeContainerEnv{env: map[string]string{}, secretFile: full.secretFile}
	for n, v := range full.env {
		moved.env[n] = v
	}
	moved.env[config.AnonMailPoWKeyFileKnob] = "/etc/elsewhere/pow.key"
	if f := judgeGuardedRender("инъекция", guarded, moved); len(f) != 1 {
		t.Errorf("файл ключа вне смонтированного тома не найден судьёй: %q", f)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// помощники

func flattenLeaves(prefix string, m map[string]any) []string {
	var out []string
	for k, v := range m {
		p := k
		if prefix != "" {
			p = prefix + "." + k
		}
		if sub, ok := v.(map[string]any); ok {
			out = append(out, flattenLeaves(p, sub)...)
			continue
		}
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// nullAt — слой значений, снимающий лист по пути (`null` в слое helm снимает
// ключ умолчаний чарта).
func nullAt(path string) map[string]any {
	parts := strings.Split(path, ".")
	var leaf any
	for i := len(parts) - 1; i >= 0; i-- {
		leaf = map[string]any{parts[i]: leaf}
	}
	return leaf.(map[string]any)
}

// withoutHops — копия деревьев цепочки, где у файла src снят ключ
// `api-gateway.trustedHops`; прочие файлы — те же деревья.
func withoutHops(trees []edgeChainTree, src string) []edgeChainTree {
	out := make([]edgeChainTree, len(trees))
	for i, ct := range trees {
		out[i] = ct
		if ct.file != src {
			continue
		}
		tree := map[string]any{}
		for k, v := range ct.tree {
			tree[k] = v
		}
		if sub, ok := ct.tree["api-gateway"].(map[string]any); ok {
			cp := map[string]any{}
			for k, v := range sub {
				if k != "trustedHops" {
					cp[k] = v
				}
			}
			tree["api-gateway"] = cp
		}
		out[i] = edgeChainTree{ct.file, tree}
	}
	return out
}

// decodeEdgeDocs — документы рендера; документ, не являющийся отображением,
// пропускается. Ошибка разбора обрывает чтение: дальше читать нечего.
func decodeEdgeDocs(rendered string) []map[string]any {
	var out []map[string]any
	dec := yaml.NewDecoder(strings.NewReader(rendered))
	for {
		var d map[string]any
		err := dec.Decode(&d)
		if errors.Is(err, io.EOF) || (err != nil && d == nil) {
			return out
		}
		if d != nil {
			out = append(out, d)
		}
	}
}

func nestedMap(m map[string]any, path ...string) map[string]any {
	for _, k := range path {
		m, _ = m[k].(map[string]any)
	}
	return m
}

func asSlice(v any) []any {
	s, _ := v.([]any)
	return s
}
