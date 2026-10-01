// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts

// own_render_carries_no_vendor_object_injection_test.go — СПОСОБНОСТЬ гейта
// own_render_carries_no_vendor_object_test.go УПАСТЬ И СМОЛЧАТЬ (kacho#2929).
//
// На голове утверждаемое свойство уже выполнено (объектов поставщика 0 на 7 из
// 7), поэтому красная сторона гейта — не родитель, а инъекция. Каждая стоит на
// НАСТОЯЩЕМ рендере каждой цепочки deploy/stacks.txt и меняет ровно один факт
// против законного близнеца:
//
//   - объект поставщика, названный ровно одним узлом-идентификатором (имя,
//     образ, метка чарта, путь подчарта в `# Source:`), добавлен к рендеру —
//     находка называет цепочку и объект; тот же объект с нейтральными узлами —
//     молчание, и число объектов выросло на один (разбор его видел);
//   - посадка половины стенда переведена настоящей ручкой профиля
//     (`--set`) — находка называет цепочку и половину; та же ручка с законным
//     значением — молчание;
//   - пустой обход и рендер без объектов — отказ, а не «находок ноль»;
//   - снятый пином ключ посадки: присутствие в рендере — находка, отсутствие —
//     посадка по построению пина.
//
// Инъекции ветви «не поднимается» живого контроля (отказ подъёма, названный
// шаблоном, снятым kacho#2818) сняты вместе с ветвью: зависимостей поставщика в
// зонте нет с kacho#1276, и входа у них не осталось — их предикат снятия
// исполнен.
//
// Словарь поставщика в этих файлах литералом не пишется: имя собирается из
// `internal/identityvendor` во время прогона. Литерал с именем в строке кода —
// единица потолка привязок (`internal/repohygiene/retiredidentityvendorceiling.go`),
// и проба, заведённая против возврата поставщика, сама была бы возвратом.
package deploy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/identityvendor"
)

// vendorWords — каждое слово словаря поставщика: отметки и бренд.
func vendorWords() []string {
	return append(identityvendor.Marks(), identityvendor.Brand())
}

// injectedDoc — объект с управляемыми узлами-идентификаторами.
type injectedDoc struct {
	Sub, Name, Chart, Image string
}

// neutralDoc — законный близнец: ни один узел поставщика не называет.
func neutralDoc() injectedDoc {
	return injectedDoc{
		Sub:   "probe-sub",
		Name:  "probe-injected",
		Chart: "probe-0.1.0",
		Image: "docker.io/prorobotech/probe:1",
	}
}

// text — документ так, как его печатает helm: разделитель, `# Source:`, тело.
func (d injectedDoc) text() string {
	return "---\n# Source: kacho-umbrella/charts/" + d.Sub + "/templates/probe.yaml\n" +
		"apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: " + d.Name + "\n" +
		"  labels:\n    helm.sh/chart: " + d.Chart + "\n" +
		"spec:\n  template:\n    spec:\n      containers:\n        - name: c\n" +
		"          image: " + d.Image + "\n"
}

// withAxis — близнец, у которого ровно один узел назван словом поставщика.
func withAxis(axis, word string) injectedDoc {
	d := neutralDoc()
	v := "probe-" + word + "-x"
	switch axis {
	case vendorAxisName:
		d.Name = v
	case vendorAxisImage:
		d.Image = "docker.io/" + v + ":1"
	case vendorAxisChart:
		d.Chart = v
	case vendorAxisSource:
		d.Sub = v
	}
	return d
}

func judgeOne(t *testing.T, name string, profiles []string, text string, rules postureRules) chainVerdict {
	t.Helper()
	v, err := judgeOwnRenders([]chainRender{{Name: name, Profiles: profiles, Text: text}}, rules)
	if err != nil {
		t.Fatalf("цепочка %s: судья отказал там, где обязан судить: %v", name, err)
	}
	return v[0]
}

func TestOwnRenderInjection_VendorObjectOnEveryAxisIsFoundOnEveryChain(t *testing.T) {
	stacks := deployStacks(t)
	rules := currentPostureRules(t)
	axes := []string{vendorAxisName, vendorAxisImage, vendorAxisChart, vendorAxisSource}
	words := vendorWords()
	injected, inapplicable := 0, 0
	for _, name := range sortedStackNames(stacks) {
		base := renderChainCached(t, stacks[name])
		clean := judgeOne(t, name, stacks[name], base, rules)
		if len(clean.Findings) != 0 {
			t.Fatalf("цепочка %s: находки на НЕИЗМЕНЁННОМ рендере (%v) — инъекция судила бы не "+
				"одну внесённую разницу", name, clean.Findings)
		}

		twin := judgeOne(t, name, stacks[name], base+neutralDoc().text(), rules)
		if len(twin.Findings) != 0 {
			t.Errorf("цепочка %s: законный близнец объявлен находкой: %v", name, twin.Findings)
		}
		if twin.Objects != clean.Objects+1 {
			t.Fatalf("цепочка %s: близнец добавил %d объектов вместо одного — разбор не видит "+
				"внесённый документ, и молчание ничего не доказывает", name, twin.Objects-clean.Objects)
		}

		for _, axis := range axes {
			for _, w := range words {
				// Путь подчарта делится по `/`, и сегмент косой черты не несёт by
				// construction: отметка пространства образов на этой оси
				// неприменима, а не пропущена.
				if axis == vendorAxisSource && strings.Contains(w, "/") {
					inapplicable++
					continue
				}
				doc := withAxis(axis, w)
				got := judgeOne(t, name, stacks[name], base+doc.text(), rules)
				injected++
				if len(got.VendorObjects) != 1 || len(got.Findings) == 0 {
					t.Errorf("цепочка %s, ось %s, слово %q: объектов поставщика найдено %d (ждали 1), "+
						"находок %d", name, axis, w, len(got.VendorObjects), len(got.Findings))
					continue
				}
				f := strings.Join(got.Findings, "\n")
				if !strings.Contains(f, "цепочка "+name+" ") || !strings.Contains(f, "Deployment/"+doc.Name) {
					t.Errorf("цепочка %s, ось %s: находка не называет цепочку и объект Deployment/%s:\n%s",
						name, axis, doc.Name, f)
				}
				if axis == vendorAxisImage && len(got.VendorImages) != 1 {
					t.Errorf("цепочка %s: образ поставщика %q не сосчитан в образах (%d)",
						name, doc.Image, len(got.VendorImages))
				}
			}
		}
	}
	if injected == 0 {
		t.Fatal("инъекций ноль — способность упасть не проверена ни на одной цепочке")
	}
	t.Logf("перепись: цепочек %d · инъекций %d (осей %d × слов %d на цепочку, неприменимых к пути "+
		"подчарта %d) · близнецов %d", len(stacks), injected, len(axes), len(words), inapplicable, len(stacks))
}

// Граница буквы у бренда: слово внутри другого слова поставщиком не является.
func TestVendorWord_BrandNeedsALetterBoundaryAndMarksIgnoreCase(t *testing.T) {
	b := identityvendor.Brand()
	m := identityvendor.Marks()
	found := []string{b, "x-" + b + "-y", b + ".example", "a/" + b + "/b", strings.ToUpper(m[0]), "x" + m[len(m)-1] + "y"}
	silent := []string{"memory", "factory", "story", "inventory-" + "svc", "prorobotech/kaname:main-1"}
	for _, s := range found {
		if vendorWordIn(s) == "" {
			t.Errorf("%q: слово поставщика не узнано", s)
		}
	}
	for _, s := range silent {
		if w := vendorWordIn(s); w != "" {
			t.Errorf("%q: узнано слово %q внутри другого слова — граница буквы не держит", s, w)
		}
	}
	t.Logf("перепись: узнаваемых %d · близнецов %d", len(found), len(silent))
}

// Настоящая ручка профиля переводит одну половину стенда — находка называет
// цепочку и половину; законное значение той же ручки — молчание.
func TestOwnRenderInjection_PostureKnobOffOwnIsFoundOnEveryChain(t *testing.T) {
	stacks := deployStacks(t)
	rules := currentPostureRules(t)
	halves := []struct{ half, knob, want string }{
		{"служба доступа", "kaname.config.authn.identityProvider", accessPostureKeyPath},
		{"край", "api-gateway.authn.identityProvider", edgePostureEnv},
	}
	if rules.AccessKeyRetired {
		// Пин снял ключ: ручка чарта отвергается его стражем, переводить нечем.
		halves = halves[1:]
	}
	if rules.EdgeKnobRetired {
		halves = halves[:len(halves)-1]
	}
	if len(halves) == 0 {
		t.Fatal("обе ручки посадки сняты — инъекции переводом ставить не на что, и суд " +
			"посадки держат только пробы снятых ключей ниже")
	}
	for _, name := range sortedStackNames(stacks) {
		for _, h := range halves {
			off := renderChainCached(t, stacks[name], h.knob+"=external")
			got := judgeOne(t, name, stacks[name], off, rules)
			f := strings.Join(got.Findings, "\n")
			if !strings.Contains(f, "цепочка "+name+" ") || !strings.Contains(f, h.half) || !strings.Contains(f, h.want) {
				t.Errorf("цепочка %s: %s переведена ручкой %s=external, а находка её не называет:\n%s",
					name, h.half, h.knob, f)
			}
			twin := judgeOne(t, name, stacks[name], renderChainCached(t, stacks[name], h.knob+"="+ownPosture), rules)
			if len(twin.Findings) != 0 {
				t.Errorf("цепочка %s: законное значение ручки %s объявлено находкой: %v", name, h.knob, twin.Findings)
			}
		}
	}
	t.Logf("перепись: цепочек %d · половин под инъекцией %d · рендеров %d",
		len(stacks), len(halves), 2*len(stacks)*len(halves))
}

func TestOwnRenderJudge_EmptyWalkAndEmptyRenderAreRefusals(t *testing.T) {
	if _, err := judgeOwnRenders(nil, postureRules{}); err == nil {
		t.Error("пустой перечень цепочек принят — «находок ноль» здесь означало бы «не прочитано ничего»")
	}
	empty := []chainRender{{Name: "probe", Text: "---\n# Source: kacho-umbrella/templates/x.yaml\n# только комментарий\n"}}
	if _, err := judgeOwnRenders(empty, postureRules{}); err == nil {
		t.Error("рендер без единого объекта принят — судить было нечего")
	}
	broken := []chainRender{{Name: "probe", Text: "---\nkind: ConfigMap\nmetadata: [\n"}}
	if _, err := judgeOwnRenders(broken, postureRules{}); err == nil {
		t.Error("неразборный документ пропущен молча — перепись сузилась бы на нём")
	}
}

// Две половины стенда без объектов посадки — находка, а не молчание.
func TestOwnRenderJudge_MissingHalvesAreFindings(t *testing.T) {
	text := neutralDoc().text()
	v := judgeOne(t, "probe", nil, text, postureRules{})
	f := strings.Join(v.Findings, "\n")
	if !strings.Contains(f, accessServiceConfigMap) || !strings.Contains(f, edgeDeploymentName) {
		t.Errorf("рендер без карты службы доступа и без пода края не дал находок о них:\n%s", f)
	}
}

// Снятый пином ключ посадки: его присутствие в рендере — находка (служба
// откажет стартом), отсутствие — посадка по построению пина.
func TestOwnRenderJudge_RetiredPostureKeysFollowThePin(t *testing.T) {
	withKey := accessDoc(true) + edgeDoc(true)
	withoutKey := accessDoc(false) + edgeDoc(false)
	retired := postureRules{AccessKeyRetired: true, EdgeKnobRetired: true}

	v := judgeOne(t, "probe", nil, withKey, retired)
	if len(v.Findings) != 2 {
		t.Errorf("снятые ключи в рендере: находок %d, ждали 2 (по одной на половину): %v", len(v.Findings), v.Findings)
	}
	v = judgeOne(t, "probe", nil, withoutKey, retired)
	if len(v.Findings) != 0 || v.AccessPosture == "" || v.EdgePosture == "" {
		t.Errorf("снятые ключи отсутствуют: находки %v, посадка %q/%q — ждали молчание и посадку по построению",
			v.Findings, v.AccessPosture, v.EdgePosture)
	}
	v = judgeOne(t, "probe", nil, withoutKey, postureRules{})
	if len(v.Findings) != 2 {
		t.Errorf("живые ключи отсутствуют в рендере: находок %d, ждали 2: %v", len(v.Findings), v.Findings)
	}
	v = judgeOne(t, "probe", nil, withKey, postureRules{})
	if len(v.Findings) != 0 {
		t.Errorf("живые ключи несут own: находки %v, ждали молчание", v.Findings)
	}
}

func accessDoc(withKey bool) string {
	body := "      login: {}\n"
	if withKey {
		body = "      identity-provider: \"own\"\n" + body
	}
	return "---\n# Source: kacho-umbrella/charts/kaname/templates/configmap.yaml\n" +
		"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: " + accessServiceConfigMap + "\n" +
		"data:\n  config.yaml: |\n    authn:\n" + body
}

func edgeDoc(withKnob bool) string {
	env := "            - name: KACHO_API_GATEWAY_OTHER\n              value: x\n"
	if withKnob {
		env = "            - name: " + edgePostureEnv + "\n              value: \"own\"\n" + env
	}
	return "---\n# Source: kacho-umbrella/charts/api-gateway/templates/deployment.yaml\n" +
		"apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: " + edgeDeploymentName + "\n" +
		"spec:\n  template:\n    spec:\n      containers:\n        - name: gw\n" +
		"          image: docker.io/prorobotech/api-gateway:1\n          env:\n" + env
}

// Перечень снятых ключей читается разбором объявления, а не поиском строки.
func TestRetiredSettingKeys_ReadsTheDeclarationAndRefusesAnUnknownForm(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	good := write("good.go", "package config\n\n"+
		"var retiredSettings = []retiredSetting{\n"+
		"\t{Key: \"authn.identity-provider\", Env: \"X\", Why: \"y\"},\n"+
		"\t{Key: \"a.b\", Env: \"Z\", Why: \"w\"},\n}\n")
	keys, exists, err := retiredSettingKeys(good)
	if err != nil || !exists || strings.Join(keys, ",") != "a.b,authn.identity-provider" {
		t.Errorf("объявление прочитано как %v (есть=%v, ошибка %v)", keys, exists, err)
	}
	// Строка в комментарии и в чужой переменной снятым ключом не является.
	twin := write("twin.go", "package config\n\n// Key: \"authn.identity-provider\"\n"+
		"var retiredSettings = []retiredSetting{}\n"+
		"var other = []retiredSetting{{Key: \"authn.identity-provider\"}}\n")
	keys, exists, err = retiredSettingKeys(twin)
	if err != nil || !exists || len(keys) != 0 {
		t.Errorf("пустое объявление прочитано как %v (есть=%v, ошибка %v) — ждали ноль ключей", keys, exists, err)
	}
	if _, exists, err = retiredSettingKeys(filepath.Join(dir, "absent.go")); exists || err != nil {
		t.Errorf("отсутствующий файл: есть=%v ошибка %v — ждали «перечня нет» без ошибки", exists, err)
	}
	gone := write("gone.go", "package config\n\nvar somethingElse = 1\n")
	if _, _, err = retiredSettingKeys(gone); err == nil {
		t.Error("файл без объявления перечня принят молча — форма сменилась, и «ключ жив» было бы неправдой")
	}
}

// Зависимости поставщика читаются из Chart.yaml узлами, а не строкой.
func TestVendorDependencies_ReadsNameAliasAndRepository(t *testing.T) {
	m := identityvendor.Marks()
	chart := "apiVersion: v2\nname: x\ndependencies:\n" +
		"  - name: " + m[0] + "\n    repository: https://example.invalid/charts\n    condition: " + m[0] + ".enabled\n" +
		"  - name: postgresql\n    alias: pg-" + m[1] + "\n    repository: https://example.invalid/b\n    condition: pg-" + m[1] + ".enabled\n" +
		"  - name: widget\n    repository: https://k8s." + identityvendor.Brand() + ".example/c\n    condition: widget.enabled\n" +
		"  - name: postgresql\n    alias: pg-vpc\n    repository: https://example.invalid/b\n    condition: pg-vpc.enabled\n" +
		"  # " + m[0] + " в комментарии зависимостью не является\n"
	deps, err := vendorDependencies([]byte(chart))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, d := range deps {
		got = append(got, d.Key+"="+d.Condition)
	}
	want := m[0] + "=" + m[0] + ".enabled,pg-" + m[1] + "=pg-" + m[1] + ".enabled,widget=widget.enabled"
	if strings.Join(got, ",") != want {
		t.Errorf("зависимости поставщика прочитаны как %v, ждали %s", got, want)
	}
	if _, err := vendorDependencies([]byte("apiVersion: v2\nname: x\n")); err == nil {
		t.Error("Chart.yaml без перечня зависимостей принят молча — обход пуст")
	}
}
