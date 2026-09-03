// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// iam_module_manifest_digest_binds_the_pod_injection_test.go — доказательство,
// что судья привязки СПОСОБЕН упасть и способен смолчать (задача #1981).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ОТДЕЛЬНОЕ ДОКАЗАТЕЛЬСТВО
//
// Зелёный прогон на чистом дереве не отличает работающего судью от судьи,
// потерявшего способность краснеть: оба печатают ноль находок. Поэтому дефект
// вносится НАСТОЯЩИМ входом, а рядом ставится ЗАКОННЫЙ БЛИЗНЕЦ той же формы —
// без него судья ловил бы форму, а не существо, и первый ложный срабат его бы
// и отключил (`testing.md` §«Гейт на класс», п.2).
//
// ИНЪЕКЦИЯ ИДЁТ В СИНТЕТИКУ, А НЕ В ДЕРЕВО. `auditDigestBinding` — чистая
// функция: ей подаются тела, и живая рабочая копия не трогается вовсе. Снятие
// прогона посреди инъекции поэтому не может оставить дерево с внесённым
// дефектом (#696).
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ДОКАЗЫВАЕТСЯ ПО КАЖДОМУ ЗВЕНУ
//
// Инъекция роняет ТОЛЬКО своё звено (п.2в): у каждого случая объявлено, чьё имя
// обязано быть в находке И чьё имя обязано в ней отсутствовать. Без второй
// половины «покраснел» не отличалось бы от «покраснел из-за соседа», и звено
// могло бы оказаться вакуумным, не показав этого ничем.

import (
	"strings"
	"testing"
)

// ── законные тела: то, к чему дерево приведено фиксом ───────────────────────

const goodChartBody = `
      annotations:
        prometheus.io/path: /metrics
        kacho.cloud/image-id: {{ dig "kachoImageIds" "iam" "unset" (.Values.global | default dict) | quote }}
        kacho.cloud/config-checksum: {{ include (print $.Template.BasePath "/configmap.yaml") . | sha256sum }}
        {{- if .Values.manifests.configMapName }}
        # Привязка к содержимому доставки манифестов (kacho#1981).
        kacho.cloud/module-manifests-digest: {{ dig "kachoModuleManifests" "digest" "unset" (.Values.global | default dict) | quote }}
        {{- end }}
`

const goodMakefileBody = "IMAGE_IDS_VALUES := helm/umbrella/values.image-ids.yaml\n" +
	"MODULE_MANIFESTS_VALUES := helm/umbrella/values.module-manifests.yaml\n" +
	"\n" +
	"module-manifests-configmap: guard-declared-context\n" +
	"\t@set -e; \\\n" +
	"\trc=0; \"$$work/producer\" -root .. $$profiles > \"$$work/configmap.yaml\" || rc=$$?; \\\n" +
	"\tcase \"$$rc\" in \\\n" +
	"\t  0) kubectl apply -f \"$$work/configmap.yaml\"; \\\n" +
	"\t     digest=\"$$(sha256sum \"$$work/configmap.yaml\" | cut -d' ' -f1)\" ;; \\\n" +
	"\t  3) digest=\"unset\" ;; \\\n" +
	"\t  *) exit \"$$rc\" ;; \\\n" +
	"\tesac; \\\n" +
	"\tprintf '%s\\n' \"global:\" \"  kachoModuleManifests:\" \"    digest: \\\"$$digest\\\"\" > $(MODULE_MANIFESTS_VALUES)\n"

// goodCarrier — путь выкатки, отдающий величину helm многострочным вызовом.
func goodCarrier() deployCarrier {
	return deployCarrier{
		Path: "deploy/Makefile",
		Kind: "Makefile",
		Text: "dev-up: preflight\n" +
			"\t@set -e; \\\n" +
			"\thelm upgrade --install kacho-umbrella ./helm/umbrella -n kacho \\\n" +
			"\t  -f ./$(IMAGE_IDS_VALUES) \\\n" +
			"\t  -f ./$(MODULE_MANIFESTS_VALUES) \\\n" +
			"\t  --wait --timeout 10m; \\\n" +
			"\techo done\n",
	}
}

// scriptCarrier — боевой путь: скрипт ИЗ КАТАЛОГА ЧАРТА, называющий файл
// переменной оболочки. Третья законная форма записи имени.
func scriptCarrier() deployCarrier {
	return deployCarrier{
		Path: umbrellaChartDir + "/cutover-fe3455.sh",
		Kind: "скрипт",
		Text: "#!/usr/bin/env bash\n" +
			"MANIFEST_DIGEST_VALUES=\"values.module-manifests.yaml\"\n" +
			"if ! helm upgrade \"$RELEASE\" . -n \"$NS\" \\\n" +
			"      \"${FE_ARGS[@]}\" \\\n" +
			"      -f \"$MANIFEST_DIGEST_VALUES\" \\\n" +
			"      --wait --timeout 15m; then\n" +
			"  exit 1\n" +
			"fi\n",
	}
}

// digestInjection — случай доказательства.
type digestInjection struct {
	Name string
	// Red — обязан ли судья покраснеть.
	Red bool
	// Mentions — что находка обязана НАЗВАТЬ (координата либо предмет).
	Mentions string
	// Absent — чьё имя в находках стоять НЕ должно: инъекция роняет только
	// своё звено, и без этой половины красное соседа читалось бы как своё.
	Absent   string
	Chart    string
	Makefile string
	Carriers []deployCarrier
}

func TestDigestBindingJudgeCanFailAndCanStaySilent(t *testing.T) {
	base := func() ([]deployCarrier, string, string) {
		return []deployCarrier{goodCarrier(), scriptCarrier()}, goodChartBody, goodMakefileBody
	}
	carriers, chart, makefile := base()

	cases := []digestInjection{
		// ── контроль: всё цело — судья МОЛЧИТ ────────────────────────────────
		{
			Name: "контроль: три звена целы → молчит",
			Red:  false, Chart: chart, Makefile: makefile, Carriers: carriers,
		},

		// ── звено A: шаблон пода штампует величину ───────────────────────────
		{
			Name: "A: аннотация снята → красное с координатой шаблона",
			Red:  true, Mentions: iamDeploymentTemplate, Absent: "Makefile:module-manifests-configmap",
			Chart:    strings.ReplaceAll(chart, digestAnnotationKey+":", "kacho.cloud/unrelated:"),
			Makefile: makefile, Carriers: carriers,
		},
		{
			Name: "A: ключ ПЕРЕИМЕНОВАН (граница имени) → красное, а не зелёное по подстроке",
			Red:  true, Mentions: iamDeploymentTemplate,
			Chart:    strings.ReplaceAll(chart, digestAnnotationKey+":", digestAnnotationKey+"-old:"),
			Makefile: makefile, Carriers: carriers,
		},
		{
			Name: "A: имя стоит ТОЛЬКО в комментарии → красное (объяснение не есть объявление)",
			Red:  true, Mentions: iamDeploymentTemplate,
			Chart: strings.ReplaceAll(chart,
				"        "+digestAnnotationKey+": {{ dig",
				"        # "+digestAnnotationKey+" — привязка; {{ dig"),
			Makefile: makefile, Carriers: carriers,
		},
		{
			Name: "A: аннотация приравнена ЛИТЕРАЛУ → красное (литерал не меняется от правки карты)",
			Red:  true, Mentions: iamDeploymentTemplate,
			Chart: strings.ReplaceAll(chart,
				digestAnnotationKey+`: {{ dig "kachoModuleManifests" "digest" "unset" (.Values.global | default dict) | quote }}`,
				digestAnnotationKey+`: "abc"`),
			Makefile: makefile, Carriers: carriers,
		},
		{
			Name:  "A-близнец: соседняя привязка той же формы остаётся законной → молчит",
			Red:   false,
			Chart: chart, Makefile: makefile, Carriers: carriers,
		},

		// ── звено B: производитель пишет отпечаток в ТУ ЖЕ величину ──────────
		{
			Name: "B: рецепт пишет в ДРУГУЮ величину → красное, шаблон не тронут",
			Red:  true, Mentions: "kachoModuleManifests", Absent: iamDeploymentTemplate,
			Chart:    chart,
			Makefile: strings.ReplaceAll(makefile, "kachoModuleManifests", "kachoSomethingElse"),
			Carriers: carriers,
		},
		{
			Name: "B: величина кладётся БЕЗ вычисления по содержимому → красное",
			Red:  true, Mentions: "sha256sum", Absent: iamDeploymentTemplate,
			Chart:    chart,
			Makefile: strings.ReplaceAll(makefile, "sha256sum \"$$work/configmap.yaml\" | cut -d' ' -f1", "echo fixed"),
			Carriers: carriers,
		},
		{
			Name: "B: переменная файла значений не объявлена → красное",
			Red:  true, Mentions: digestValuesVar,
			Chart:    chart,
			Makefile: strings.ReplaceAll(makefile, "MODULE_MANIFESTS_VALUES := ", "MODULE_MANIFESTS_VALUES_OTHER := "),
			Carriers: carriers,
		},
		{
			Name: "B: цели производителя нет вовсе → красное о беспредметности",
			Red:  true, Mentions: digestProducerTarget,
			Chart:    chart,
			Makefile: strings.ReplaceAll(makefile, "module-manifests-configmap: guard-declared-context", "some-other-target: guard-declared-context"),
			Carriers: carriers,
		},

		// ── звено C: величина доезжает до helm ───────────────────────────────
		{
			Name: "C: путь выкатки НЕ отдаёт величину helm → красное с координатой вызова",
			Red:  true, Mentions: "deploy/Makefile:3", Absent: iamDeploymentTemplate,
			Chart: chart, Makefile: makefile,
			Carriers: []deployCarrier{
				{Path: "deploy/Makefile", Kind: "Makefile", Text: strings.ReplaceAll(
					goodCarrier().Text, "\t  -f ./$(MODULE_MANIFESTS_VALUES) \\\n", "")},
				scriptCarrier(),
			},
		},
		{
			Name: "C: имя файла с хвостом (граница) → красное, подстрока не засчитывается",
			Red:  true, Mentions: "cutover-fe3455.sh",
			Chart: chart, Makefile: makefile,
			Carriers: []deployCarrier{goodCarrier(), {
				Path: scriptCarrier().Path, Kind: "скрипт",
				Text: strings.ReplaceAll(scriptCarrier().Text,
					"values.module-manifests.yaml", "values.module-manifests.yaml.bak"),
			}},
		},
		{
			Name:  "C: боевой путь называет файл ПЕРЕМЕННОЙ ОБОЛОЧКИ → молчит (законная форма)",
			Red:   false,
			Chart: chart, Makefile: makefile,
			Carriers: []deployCarrier{goodCarrier(), scriptCarrier()},
		},
		{
			Name:  "C-близнец: вызов helm по ЧУЖОМУ чарту величины не требует → молчит",
			Red:   false,
			Chart: chart, Makefile: makefile,
			Carriers: append(carriers, deployCarrier{
				Path: "deploy/Makefile", Kind: "Makefile",
				Text: "cert:\n\thelm upgrade --install cert-manager ./vendor/cert-manager -n kacho --wait\n",
			}),
		},
	}

	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			findings, census := auditDigestBinding(c.Chart, c.Makefile, c.Carriers)
			joined := strings.Join(findings, "\n")

			if census.ChartBytes == 0 || census.Carriers == 0 {
				t.Fatalf("синтетика пуста (байт %d, носителей %d) — доказывать было нечего",
					census.ChartBytes, census.Carriers)
			}
			switch {
			case c.Red && len(findings) == 0:
				t.Fatalf("судья ОСТАЛСЯ ЗЕЛЁНЫМ на внесённом дефекте — он не способен упасть; "+
					"перепись: %s", census.Summary())
			case !c.Red && len(findings) > 0:
				t.Fatalf("судья покраснел на ЗАКОННОЙ конструкции — он ловит форму, а не "+
					"существо, и первый ложный срабат его отключит:\n%s", joined)
			}
			if c.Mentions != "" && !strings.Contains(joined, c.Mentions) {
				t.Errorf("находка не НАЗЫВАЕТ %q — читателя посылают искать не там:\n%s",
					c.Mentions, joined)
			}
			if c.Absent != "" && strings.Contains(joined, c.Absent) {
				t.Errorf("инъекция уронила ЧУЖОЕ звено (%q в находках) — красное пришло от "+
					"соседа, и проверяемое звено могло остаться вакуумным:\n%s", c.Absent, joined)
			}
		})
	}
}
