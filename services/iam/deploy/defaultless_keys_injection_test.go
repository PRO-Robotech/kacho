// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: AGPL-3.0-or-later

// defaultless_keys_injection_test.go — доказательство того, что соседняя проба
// СПОСОБНА упасть, и падает ровно на своём предмете.
//
// ГЛАВНОЕ ЗДЕСЬ — ПЕРЕЧЕНЬ ФОРМ. Проба ищет предмет по образцу, а форма, о
// которой распознаватель не знает, не даёт ни красного, ни зелёного — она
// МОЛЧИТ, и всё записанное в ней оказывается вне наблюдения. Поэтому каждая
// законная форма доказана СВОИМ случаем, и у всех них общий отрицательный
// близнец: тот же ключ, подставленный без формы.
//
// Фикстура — настоящий каталог поставки (`copyChartDeliveryFixture`), и каждый
// случай меняет против целой копии РОВНО ОДИН факт.
package deploy_test

import (
	"fmt"
	"strings"
	"testing"
)

// probeKnobPath — ключ, заводимый инъекцией. Имя нарочно постороннее: случай,
// опирающийся на СУЩЕСТВУЮЩИЙ ключ чарта, менял бы два факта разом — предмет и
// то, чем он укрыт.
const probeKnobPath = "injectedProbeKnob"

// declareDefaultlessKnob — заводит в базовых значениях фикстуры ключ с пустым
// умолчанием и кладёт в шаблон строку, которая его называет.
func declareDefaultlessKnob(t *testing.T, chartDir, templateLine string) {
	t.Helper()
	values := readChartFile(t, chartDir, "values.yaml")
	writeChartFile(t, chartDir, "values.yaml",
		values+fmt.Sprintf("\n%s: \"\"\n", probeKnobPath))

	tmpl := readChartFile(t, chartDir, "templates/service.yaml")
	writeChartFile(t, chartDir, "templates/service.yaml", tmpl+"\n"+templateLine+"\n")
}

func TestDefaultlessKeysInjection(t *testing.T) {
	cases := []chartFixtureCase{
		{
			// Положительный контроль. Без него все отрицания ниже зеленели бы на
			// входе, который проба вообще не читает.
			name:          "целая копия — молчание",
			mutate:        func(*testing.T, string) {},
			wantSubstring: "",
		},
		{
			// НЕСУЩИЙ СЛУЧАЙ: перечень отказа рендера перестал называть
			// координату образа, и она подставляется дословно — ровно то
			// состояние, в котором задача #2094 оставила бы чарт, сняв умолчание
			// и не тронув перечень.
			name: "перечень отказа рендера не называет образ — находка",
			mutate: func(t *testing.T, chartDir string) {
				b := readChartFile(t, chartDir, "templates/_helpers.tpl")
				start := strings.Index(b, "{{- if not .Values.image -}}")
				if start < 0 {
					t.Fatal("инъекция беспредметна: во входе нет ветви образа")
				}
				tail := b[start:]
				end := strings.Index(tail, "{{- end -}}\n")
				if end < 0 {
					t.Fatal("инъекция беспредметна: ветвь образа не закрыта")
				}
				writeChartFile(t, chartDir, "templates/_helpers.tpl",
					b[:start]+tail[end+len("{{- end -}}\n"):])
			},
			wantSubstring: "подставляет ключ ДОСЛОВНО",
		},
		{
			// ОТРИЦАНИЕ ДЛЯ ВСЕХ ФОРМ НИЖЕ: тот же заведённый ключ, названный
			// шаблоном БЕЗ формы, узнающей пустоту.
			name: "ключ со снятым умолчанием подставлен без формы — находка",
			mutate: func(t *testing.T, chartDir string) {
				declareDefaultlessKnob(t, chartDir,
					fmt.Sprintf("  probe: {{ .Values.%s }}", probeKnobPath))
			},
			wantSubstring: probeKnobPath,
		},
	}

	// ЗАКОННЫЕ БЛИЗНЕЦЫ, по одному на форму. От случая выше каждый отличается
	// РОВНО ОДНИМ фактом — формой, которой ключ укрыт.
	for _, form := range []struct{ name, line string }{
		{"if", fmt.Sprintf("{{- if .Values.%s }}\n  probe: yes\n{{- end }}", probeKnobPath)},
		{"else if", fmt.Sprintf("{{- if false }}\n{{- else if .Values.%s }}\n  probe: yes\n{{- end }}", probeKnobPath)},
		{"with", fmt.Sprintf("{{- with .Values.%s }}\n  probe: {{ . }}\n{{- end }}", probeKnobPath)},
		{"range", fmt.Sprintf("{{- range .Values.%s }}\n  - {{ . }}\n{{- end }}", probeKnobPath)},
		{"required", fmt.Sprintf(`  probe: {{ required "не задано" .Values.%s }}`, probeKnobPath)},
		{"default", fmt.Sprintf(`  probe: {{ default "x" .Values.%s }}`, probeKnobPath)},
	} {
		line := form.line
		cases = append(cases, chartFixtureCase{
			name: "форма `" + form.name + "` узнаёт пустоту — молчание",
			mutate: func(t *testing.T, chartDir string) {
				declareDefaultlessKnob(t, chartDir, line)
			},
			wantSubstring: "",
		})
	}

	runChartFixtureCases(t, cases, auditDefaultlessKeys, "")
}

// TestDefaultlessKeysEmptyTraversalIsNotGreen — обход, которому нечего читать,
// обязан быть ОТЛИЧИМ от обхода без находок.
func TestDefaultlessKeysEmptyTraversalIsNotGreen(t *testing.T) {
	t.Run("ни одного снятого умолчания", func(t *testing.T) {
		chartDir := copyChartDeliveryFixture(t)
		b := readChartFile(t, chartDir, "values.yaml")
		// Один факт: каждое снятое умолчание чем-нибудь заполнено. Популяция
		// пуста, и судить нечего — это отказ, а не чистый чарт.
		for _, key := range []string{
			"image: \"\"", "host: \"\"", "passwordSecretName: \"\"", "passwordSecretKey: \"\"",
			"issuer: \"\"", "service: \"\"", "secretName: \"\"", "identityProvider: \"\"",
		} {
			b = strings.ReplaceAll(b, key, strings.TrimSuffix(key, "\"\"")+"\"filled\"")
		}
		writeChartFile(t, chartDir, "values.yaml", b)

		_, census, err := auditDefaultlessKeys(chartDir)
		if err == nil {
			t.Fatalf("популяция пуста, а вердикт вынесен — беспредметное зелёное "+
				"неотличимо от чистого чарта\nперепись: %s", census)
		}
		t.Logf("отказ подтверждён: %v", err)
	})

	t.Run("шаблоны без единого действия", func(t *testing.T) {
		chartDir := copyChartDeliveryFixture(t)
		for _, rel := range []string{"templates/_helpers.tpl", "templates/configmap.yaml",
			"templates/deployment.yaml", "templates/service.yaml"} {
			writeChartFile(t, chartDir, rel, "# шаблон без действий\n")
		}
		if _, _, err := auditDefaultlessKeys(chartDir); err == nil {
			t.Fatal("шаблоны без единого действия дали вердикт вместо отказа")
		}
	})

	t.Run("пустой каталог", func(t *testing.T) {
		if _, _, err := auditDefaultlessKeys(t.TempDir()); err == nil {
			t.Fatal("пустой каталог дал вердикт вместо отказа")
		}
	})
}
