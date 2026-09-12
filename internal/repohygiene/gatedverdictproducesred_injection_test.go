// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// gatedverdictproducesred_injection_test.go — доказательство того, что судья
// [judgeGatedVerdict] СПОСОБЕН упасть и способен смолчать.
//
// Прогоняется ТА ЖЕ функция суждения, которой судит гейт дерева, а не её
// пересказ: проверка, воспроизводящая цикл своей копией, доказывает свойство
// копии и остаётся зелёной при снятой судящей ветке.
//
// Три оси, и ни одна не заменяет другую:
//
//  1. СИНТЕТИКА по каждой законной форме — [TestGatedVerdictJudgeKnowsEveryForm].
//     Форма, о которой распознаватель не знает, даёт МОЛЧАНИЕ, а не красное;
//     поэтому каждая перечисленная форма доказывается своим случаем, а рядом с
//     ней стоит законный близнец, обязанный смолчать.
//  2. НАСТОЯЩИЙ ВХОД ИЗ ДЕРЕВА — [TestGatedVerdictJudgeOnRealTreeInput].
//     Синтетика доказывает форму, а не существо: она не знает, совпадает ли
//     форма в дереве с той, которую судья умеет читать. Поэтому берётся
//     настоящее объявление процесса, у него снимается РОВНО ОДНО свойство, и
//     находка обязана назвать именно то задание.
//  3. ИСПОЛНЕНИЕ ВЛАДЕЛЬЦА СВОДНОГО ВЕРДИКТА —
//     [TestRunLevelVerdictOwnerRedsOnTheMark]. Делегирование судья принимает
//     СТРУКТУРНО: задание, от которого зависит нижестоящее, и владелец в его
//     шаге. Структура не отвечает на вопрос, краснеет ли владелец от отметки, —
//     это ОБЪЯВЛЕНИЕ, а не исход. Поэтому владелец ИСПОЛНЯЕТСЯ на
//     синтезированном входе, в обе стороны: с отметкой обязан вернуть
//     ненулевой код, без неё — нулевой.
func TestGatedVerdictJudgeKnowsEveryForm(t *testing.T) {
	t.Parallel()

	// Отметка по умолчанию: значение выведено, СУППРЕССИВНА (в дереве гасит
	// вердиктные шаги). Случай, которому нужна другая отметка, объявляет свою.
	marks := map[string]gvMark{
		"STAND_PRECONDITION_UNMET": {
			Name: "STAND_PRECONDITION_UNMET", Value: "1", Known: true,
			Suppresses: true,
			Writers:    []string{"синтетика/stand-up.sh:1"},
		},
	}
	const gated = "        if: ${{ env.STAND_PRECONDITION_UNMET != '1' }}\n"
	// Писатель отметки — отслеживаемый скрипт, объявляющий СВОЮ самопробу.
	// Разбор `--self-test` в его теле и есть признак, по которому судья
	// отличает вызов по делу от вызова самопробы.
	writerScripts := map[string]string{
		".github/scripts/stand-up.sh": "if [ \"$1\" = --self-test ]; then\n  run_cases; exit $ok\nfi\n" +
			"echo \"STAND_PRECONDITION_UNMET=1\" >> \"$GITHUB_ENV\"\nexit 0\n",
	}
	// Та же отметка, но её писатель — ОТСЛЕЖИВАЕМЫЙ скрипт выше. Разделение
	// нарочное: у случаев о признаке «гасит» писатель неотслеживаемый, поэтому
	// производства в них нет и каждый доказывает РОВНО своё свойство.
	trackedWriter := map[string]gvMark{
		"STAND_PRECONDITION_UNMET": {
			Name: "STAND_PRECONDITION_UNMET", Value: "1", Known: true,
			Suppresses: true,
			Writers:    []string{".github/scripts/stand-up.sh:284"},
		},
	}

	cases := []struct {
		name    string
		yaml    string
		scripts map[string]string
		marks   map[string]gvMark
		wantHit bool
	}{
		{
			name: "вердиктный шаг погашен отметкой, производителя красного нет — находка",
			yaml: "jobs:\n  probes:\n    steps:\n      - name: прогон проб\n" +
				gated + "        run: npx playwright test\n",
			wantHit: true,
		},
		{
			name: "форма 1 — шаг-отказ по отметке в обратной полярности, молчит",
			yaml: "jobs:\n  probes:\n    steps:\n      - name: прогон проб\n" +
				gated + "        run: npx playwright test\n" +
				"      - name: условие не создано — вердикта о продукте нет\n" +
				"        if: ${{ always() && env.STAND_PRECONDITION_UNMET == '1' }}\n" +
				"        run: |\n          echo \"::error::стенд не поднялся\"\n          exit 1\n",
			wantHit: false,
		},
		{
			name: "форма 1 без ненулевого выхода — аннотация без отказа, находка",
			yaml: "jobs:\n  probes:\n    steps:\n      - name: прогон проб\n" +
				gated + "        run: npx playwright test\n" +
				"      - name: условие не создано\n" +
				"        if: ${{ always() && env.STAND_PRECONDITION_UNMET == '1' }}\n" +
				"        run: echo \"::error::стенд не поднялся\"\n",
			wantHit: true,
		},
		{
			name: "форма 1 с exit 0 — выход нулевой, находка",
			yaml: "jobs:\n  probes:\n    steps:\n      - name: прогон проб\n" +
				gated + "        run: npx playwright test\n" +
				"      - name: условие не создано\n" +
				"        if: ${{ always() && env.STAND_PRECONDITION_UNMET == '1' }}\n" +
				"        run: |\n          echo \"::error::стенд не поднялся\"\n          exit 0\n",
			wantHit: true,
		},
		{
			name: "форма 1 под continue-on-error — отказ проглочен, находка",
			yaml: "jobs:\n  probes:\n    steps:\n      - name: прогон проб\n" +
				gated + "        run: npx playwright test\n" +
				"      - name: условие не создано\n" +
				"        if: ${{ always() && env.STAND_PRECONDITION_UNMET == '1' }}\n" +
				"        continue-on-error: true\n" +
				"        run: exit 1\n",
			wantHit: true,
		},
		{
			name: "форма 2 — ветвь внутри всегда-исполняемого шага, молчит",
			yaml: "jobs:\n  probes:\n    steps:\n      - name: прогон проб\n" +
				gated + "        run: npx playwright test\n" +
				"      - name: разметка исхода\n        if: ${{ always() }}\n" +
				"        run: |\n          if [ \"${STAND_PRECONDITION_UNMET:-}\" = 1 ]; then\n" +
				"            echo \"::error::условие не создано\"; exit 1\n          fi\n",
			wantHit: false,
		},
		{
			name: "форма 1 через отслеживаемый скрипт (полярность в условии), молчит",
			yaml: "jobs:\n  probes:\n    steps:\n      - name: прогон проб\n" +
				gated + "        run: npx playwright test\n" +
				"      - name: условие не создано — работа красная\n" +
				"        if: ${{ always() && env.STAND_PRECONDITION_UNMET == '1' }}\n" +
				"        run: bash .github/scripts/unmet-is-red.sh\n",
			scripts: map[string]string{
				".github/scripts/unmet-is-red.sh": "echo \"::error::условие не создано\"\nexit 1\n",
			},
			wantHit: false,
		},
		{
			name: "тот же вызов, но скрипт ненулевым выходить не умеет — находка",
			yaml: "jobs:\n  probes:\n    steps:\n      - name: прогон проб\n" +
				gated + "        run: npx playwright test\n" +
				"      - name: условие не создано\n" +
				"        if: ${{ always() && env.STAND_PRECONDITION_UNMET == '1' }}\n" +
				"        run: bash .github/scripts/unmet-is-red.sh\n",
			scripts: map[string]string{
				".github/scripts/unmet-is-red.sh": "echo \"::error::условие не создано\"\nexit 0\n",
			},
			wantHit: true,
		},
		{
			name: "владелец подъёма называет отметку и умеет падать — производителем НЕ является",
			yaml: "jobs:\n  probes:\n    steps:\n      - name: стенд (dev-up)\n" +
				"        run: .github/scripts/stand-up.sh --dir deploy -- make dev-up\n" +
				"      - name: прогон проб\n" + gated + "        run: npx playwright test\n",
			scripts: map[string]string{
				".github/scripts/stand-up.sh": "# ставит отметку и выходит НУЛЁМ\n" +
					"echo \"STAND_PRECONDITION_UNMET=1\" >> \"$GITHUB_ENV\"\nexit 0\n" +
					"die() { echo \"$1\" >&2; exit 2; }\n",
			},
			wantHit: true,
		},
		{
			name: "форма 4 — делегирование нижестоящему заданию с владельцем, молчит",
			yaml: "jobs:\n  shard:\n    steps:\n      - name: newman — суиты шарда\n" +
				gated + "        run: bash .github/scripts/newman-shard-run.sh\n" +
				"  summary:\n    needs: [shard]\n    if: ${{ !cancelled() }}\n    steps:\n" +
				"      - name: СВОДНЫЙ ВЕРДИКТ\n" +
				"        run: python3 .github/scripts/aggregate-shard-verdicts.py --dir shard-verdicts\n",
			wantHit: false,
		},
		{
			name: "то же делегирование без needs — связи нет, находка",
			yaml: "jobs:\n  shard:\n    steps:\n      - name: newman — суиты шарда\n" +
				gated + "        run: bash .github/scripts/newman-shard-run.sh\n" +
				"  summary:\n    if: ${{ !cancelled() }}\n    steps:\n" +
				"      - name: СВОДНЫЙ ВЕРДИКТ\n" +
				"        run: python3 .github/scripts/aggregate-shard-verdicts.py --dir shard-verdicts\n",
			wantHit: true,
		},
		{
			name: "делегат есть, но владелец не из перечня доказанных — находка",
			yaml: "jobs:\n  shard:\n    steps:\n      - name: newman — суиты шарда\n" +
				gated + "        run: bash .github/scripts/newman-shard-run.sh\n" +
				"  summary:\n    needs: [shard]\n    if: ${{ !cancelled() }}\n    steps:\n" +
				"      - name: СВОДНЫЙ ВЕРДИКТ\n        run: python3 .github/scripts/my-own-summary.py\n",
			wantHit: true,
		},
		{
			name: "делегат исполняется только на падении — отметка его не поднимет, находка",
			yaml: "jobs:\n  shard:\n    steps:\n      - name: newman — суиты шарда\n" +
				gated + "        run: bash .github/scripts/newman-shard-run.sh\n" +
				"  summary:\n    needs: [shard]\n    if: ${{ failure() }}\n    steps:\n" +
				"      - name: СВОДНЫЙ ВЕРДИКТ\n" +
				"        run: python3 .github/scripts/aggregate-shard-verdicts.py --dir shard-verdicts\n",
			wantHit: true,
		},
		{
			name: "форма 2 — отметка доезжает в шаг через его блок env, молчит",
			yaml: "jobs:\n  probes:\n    steps:\n      - name: прогон проб\n" +
				gated + "        run: npx playwright test\n" +
				"      - name: разметка исхода\n        if: ${{ always() }}\n" +
				"        env:\n          UNMET: ${{ env.STAND_PRECONDITION_UNMET }}\n" +
				"        run: |\n          [ \"${UNMET:-}\" = 1 ] && { echo \"::error::условие не создано\"; exit 1; }\n" +
				"          exit 0\n",
			wantHit: false,
		},
		{
			name: "форма 3 — отметка передана отслеживаемому скрипту через env, молчит",
			yaml: "jobs:\n  stand:\n    steps:\n      - name: вердикт числами\n" +
				gated + "        run: go test ./deploy/ -run Posture\n" +
				"      - name: перепись исходов\n        if: ${{ always() }}\n" +
				"        env:\n          UNMET: ${{ env.STAND_PRECONDITION_UNMET }}\n" +
				"        run: |\n          python3 .github/scripts/verdict-census.py --flag \"$UNMET\"\n",
			scripts: map[string]string{
				".github/scripts/verdict-census.py": "def main():\n    if flag:\n        return 1\n",
			},
			wantHit: false,
		},
		{
			name: "та же форма, но отметка скрипту НЕ передана — находка",
			yaml: "jobs:\n  stand:\n    steps:\n      - name: вердикт числами\n" +
				gated + "        run: go test ./deploy/ -run Posture\n" +
				"      - name: перепись исходов\n        if: ${{ always() }}\n" +
				"        env:\n          UNMET: ${{ env.STAND_PRECONDITION_UNMET }}\n" +
				"        run: |\n          python3 .github/scripts/verdict-census.py --dir shard\n",
			scripts: map[string]string{
				".github/scripts/verdict-census.py": "def main():\n    if flag:\n        return 1\n",
			},
			wantHit: true,
		},
		{
			name: "форма 3, скрипт краснеет через sys.exit — молчит",
			yaml: "jobs:\n  stand:\n    steps:\n      - name: вердикт числами\n" +
				gated + "        run: go test ./deploy/ -run Posture\n" +
				"      - name: перепись исходов\n        if: ${{ always() }}\n" +
				"        env:\n          UNMET: ${{ env.STAND_PRECONDITION_UNMET }}\n" +
				"        run: |\n          python3 .github/scripts/verdict-census.py --flag \"$UNMET\"\n",
			scripts: map[string]string{
				".github/scripts/verdict-census.py": "if flag:\n    sys.exit(1)\n",
			},
			wantHit: false,
		},
		{
			name: "форма 3, скрипт умеет только sys.exit(0) — находка",
			yaml: "jobs:\n  stand:\n    steps:\n      - name: вердикт числами\n" +
				gated + "        run: go test ./deploy/ -run Posture\n" +
				"      - name: перепись исходов\n        if: ${{ always() }}\n" +
				"        env:\n          UNMET: ${{ env.STAND_PRECONDITION_UNMET }}\n" +
				"        run: |\n          python3 .github/scripts/verdict-census.py --flag \"$UNMET\"\n",
			scripts: map[string]string{
				".github/scripts/verdict-census.py": "if flag:\n    sys.exit(0)\n",
			},
			wantHit: true,
		},
		{
			name: "законный близнец: под отметкой только кэш — молчит",
			yaml: "jobs:\n  probes:\n    steps:\n      - uses: actions/cache/save@v6\n" +
				gated + "        with:\n          path: ~/.cache/x\n          key: k\n",
			wantHit: false,
		},
		{
			name: "законный близнец: под отметкой только вход в реестр образов — молчит",
			yaml: "jobs:\n  build:\n    steps:\n      - name: login to Docker Hub\n" +
				"        uses: docker/login-action@v3\n" + gated,
			wantHit: false,
		},
		{
			name: "законный близнец: под отметкой только уборка и опись — молчит",
			yaml: "jobs:\n  probes:\n    steps:\n      - name: снести стенд\n" +
				gated + "        run: kind delete cluster --name k\n" +
				"      - name: что поднялось\n" + gated +
				"        run: |\n          kubectl get pods -A\n          df -h\n",
			wantHit: false,
		},
		{
			// ЗДЕСЬ СТОЯЛ «ЗАКОННЫЙ БЛИЗНЕЦ: ОТМЕТКУ НЕ ЧИТАЕТ НИ ОДИН ШАГ —
			// МОЛЧИТ», И ЭТО БЫЛА МОЯ ОШИБКА, А НЕ БЛИЗНЕЦ. Ровно этим
			// состоянием жила работа `production-posture.yml::stand`: два
			// производителя отметки, ноль читателей, зелёный исход без
			// вердикта — замерено исполнением настоящих тел шагов на
			// отсутствующем кластере (`lane/posture-verdict`). Случай оставлен
			// с ОБРАТНЫМ ожиданием и стоит среди признака «производит» выше.
			//
			// Настоящий близнец этого класса — работа, которая отметку не
			// производит и не читает: ей требовать нечего.
			name: "законный близнец: работа отметку не производит и не читает — молчит",
			yaml: "jobs:\n  lint:\n    steps:\n      - name: go vet\n" +
				"        run: go vet ./...\n" +
				"      - name: сборка\n        if: ${{ always() }}\n        run: go build ./...\n",
			scripts: writerScripts,
			marks:   trackedWriter,
			wantHit: false,
		},
		{
			name: "значение отметки из дерева не выводится — находка, а не молчание",
			yaml: "jobs:\n  probes:\n    steps:\n      - name: прогон проб\n" +
				"        if: ${{ env.KACHO_UNKNOWN_MARK != '1' }}\n        run: npx playwright test\n",
			scripts: map[string]string{},
			marks: map[string]gvMark{
				"KACHO_UNKNOWN_MARK": {Name: "KACHO_UNKNOWN_MARK", Known: false,
					Writers: []string{"синтетика/writer.sh:1"}},
			},
			wantHit: true,
		},
		// ── признак «РАБОТА ПРОИЗВОДИТ ОТМЕТКУ» ──────────────────────────────
		{
			name: "работа производит суппрессивную отметку, читателя нет — находка",
			yaml: "jobs:\n  stand:\n    steps:\n      - name: стенд\n" +
				"        run: .github/scripts/stand-up.sh --dir deploy -- make dev-prod-up\n" +
				"      - name: вердикт числами\n        if: ${{ always() }}\n" +
				"        run: go test ./deploy/ -run Posture\n",
			scripts: writerScripts,
			marks:   trackedWriter,
			wantHit: true,
		},
		{
			name: "та же работа с производителем красного формы 2 — молчит",
			yaml: "jobs:\n  stand:\n    steps:\n      - name: стенд\n" +
				"        run: .github/scripts/stand-up.sh --dir deploy -- make dev-prod-up\n" +
				"      - name: вердикт числами\n        if: ${{ always() }}\n" +
				"        run: go test ./deploy/ -run Posture\n" +
				"      - name: перепись исходов\n        if: ${{ always() }}\n" +
				"        run: |\n          [ \"${STAND_PRECONDITION_UNMET:-}\" = 1 ] && " +
				"{ echo \"::error::условие не создано\"; exit 1; }\n          exit 0\n",
			scripts: writerScripts,
			marks:   trackedWriter,
			wantHit: false,
		},
		{
			name: "законный близнец: работа зовёт писателя его САМОПРОБОЙ — производством не является",
			yaml: "jobs:\n  probes:\n    steps:\n      - name: гейт — самопроверка владельца подъёма\n" +
				"        run: bash .github/scripts/stand-up.sh --self-test\n" +
				"      - name: вердикт числами\n        if: ${{ always() }}\n" +
				"        run: go test ./deploy/ -run Posture\n",
			scripts: writerScripts,
			marks:   trackedWriter,
			wantHit: false,
		},
		{
			name: "законный близнец: отметка НЕ суппрессивна в дереве — производство не требует красного",
			yaml: "jobs:\n  probes:\n    steps:\n      - name: браузер — сам chromium\n" +
				"        run: bash .github/scripts/install-pinned-browser.sh\n" +
				"      - name: прогон проб\n        if: ${{ always() }}\n        run: npx playwright test\n",
			scripts: map[string]string{
				".github/scripts/install-pinned-browser.sh": "echo \"KACHO_CHROMIUM=$img\" >> \"$GITHUB_ENV\"\nexit 1\n",
			},
			marks: map[string]gvMark{
				"KACHO_CHROMIUM": {Name: "KACHO_CHROMIUM", Value: "$img", Known: false,
					Writers: []string{".github/scripts/install-pinned-browser.sh:195"}},
			},
			wantHit: false,
		},
		{
			name: "работа ПИШЕТ отметку своим телом, читателя нет — находка",
			yaml: "jobs:\n  stand:\n    steps:\n      - name: стенд\n" +
				"        run: |\n          make dev-prod-up || " +
				"echo \"STAND_PRECONDITION_UNMET=1\" >> \"$GITHUB_ENV\"\n" +
				"      - name: вердикт числами\n        if: ${{ always() }}\n" +
				"        run: go test ./deploy/ -run Posture\n",
			scripts: map[string]string{},
			wantHit: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := marks
			if tc.marks != nil {
				m = tc.marks
			}
			findings, _ := judgeGatedVerdict("синтетика.yml", tc.yaml, m, tc.scripts)
			if got := len(findings) > 0; got != tc.wantHit {
				t.Fatalf("ожидалась находка=%v, получено %v: %v", tc.wantHit, got, findings)
			}
		})
	}
}

func scriptsNone() map[string]string { return map[string]string{} }

// TestGatedVerdictJudgeOnRealTreeInput — инъекция НАСТОЯЩИМ входом из дерева.
//
// Берётся объявление, которое судья обязан пропускать по форме 3
// (`e2e-newman.yml`: шард гасит вердикты отметкой, красное производит
// нижестоящий свод), и у него снимается РОВНО ОДНО свойство. Дерево не
// правится: правится копия в памяти.
func TestGatedVerdictJudgeOnRealTreeInput(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	marks, scripts := gvCollectMarks(t, root)

	const subject = workflowsDir + "/e2e-newman.yml"
	raw, err := os.ReadFile(filepath.Join(root, subject))
	if err != nil {
		t.Fatalf("не прочитан %s: %v — инъекции не на чем ставить", subject, err)
	}

	// Контроль: как есть в дереве — судья молчит про это задание.
	if findings, _ := judgeGatedVerdict(subject, string(raw), marks, scripts); len(findings) != 0 {
		t.Fatalf("контроль: на настоящем %s судья дал находки %v — "+
			"инъекция ниже будет неотличима от этого же", subject, findings)
	}

	// Инъекция: у свода снято `needs` на шард. Связи «шард зелен → свод судит»
	// больше нет, значит красное производить некому.
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("%s не разобран: %v", subject, err)
	}
	jobs, ok := doc["jobs"].(map[string]any)
	if !ok {
		t.Fatalf("%s: у объявления нет разбираемого `jobs:` — предпосылка инъекции не выполняется", subject)
	}
	summary, ok := jobs["summary"].(map[string]any)
	if !ok {
		t.Fatalf("%s: задания `summary` нет — предпосылка инъекции не выполняется", subject)
	}
	if _, had := summary["needs"]; !had {
		t.Fatalf("%s: у `summary` нет `needs` — снимать нечего, инъекция бессодержательна", subject)
	}
	delete(summary, "needs")
	injected, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatalf("инъекция не собралась обратно: %v", err)
	}
	findings, _ := judgeGatedVerdict(subject, string(injected), marks, scripts)
	if len(findings) == 0 {
		t.Fatal("инъекция: у свода снято `needs` на шард, красное производить некому — " +
			"судья обязан назвать находку, а он смолчал")
	}
	var named bool
	for _, f := range findings {
		if strings.Contains(f, "shard") {
			named = true
		}
	}
	if !named {
		t.Fatalf("инъекция поймана, но задание не названо: %v", findings)
	}
}

// TestRunLevelVerdictOwnerRedsOnTheMark — ИСПОЛНЕНИЕ владельца сводного
// вердикта, а не чтение его объявления.
//
// Форму 3 судья принимает структурно, и структура о красноте не говорит
// ничего. Здесь владелец запускается на синтезированном наборе описей в двух
// прогонах, различающихся РОВНО ОДНИМ фактом — отметкой на одном шарде:
//
//	без отметки, все коллекции дерева отчитались → код 0;
//	с отметкой на одном шарде                    → код НЕ 0.
//
// Без второго прогона неизвестно, способен ли владелец быть зелёным вообще
// (отрицание принимается только в паре с положительным); без первого — краснеет
// ли он ИМЕННО от отметки.
//
// Перечень владельцев ведёт судья ([gvRunLevelVerdictOwners]), и каждому здесь
// обязано найтись доказательство: владелец, объявленный без доказательства, —
// то же обещание, которое этот гейт и ловит.
func TestRunLevelVerdictOwnerRedsOnTheMark(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	proofs := map[string]func(*testing.T, string){
		".github/scripts/aggregate-shard-verdicts.py": proveShardAggregateRedsOnTheMark,
	}
	if len(gvRunLevelVerdictOwners) == 0 {
		t.Fatal("перечень владельцев сводного вердикта пуст — доказывать нечего, " +
			"и форма 3 стала бы всеразрешением")
	}
	for _, owner := range gvRunLevelVerdictOwners {
		proof, ok := proofs[owner]
		if !ok {
			t.Errorf("владелец %s объявлен судьёй, а доказательства его красноты "+
				"от отметки здесь нет — объявление без исхода", owner)
			continue
		}
		if _, err := os.Stat(filepath.Join(root, owner)); err != nil {
			t.Errorf("владелец %s объявлен, а в дереве его нет: %v", owner, err)
			continue
		}
		t.Run(owner, func(t *testing.T) { proof(t, root) })
	}
}

// proveShardAggregateRedsOnTheMark — синтезирует набор описей шардов и
// исполняет свод дважды.
func proveShardAggregateRedsOnTheMark(t *testing.T, root string) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Fatalf("python3 не найден: %v — владелец не исполнен, и о его красноте "+
			"НЕ ИЗВЕСТНО НИЧЕГО. Это «не выполнилось», и по решению владельца "+
			"продукта оно красное, а не пропуск", err)
	}
	manifestRaw, err := os.ReadFile(filepath.Join(root, "deploy", "e2e-shards.json"))
	if err != nil {
		t.Fatalf("манифест шардов не прочитан: %v — синтезировать вход неоткуда", err)
	}
	var manifest struct {
		Shards []struct {
			ID     string   `json:"id"`
			Suites []string `json:"suites"`
		} `json:"shards"`
	}
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		t.Fatalf("манифест шардов не разобран: %v", err)
	}
	if len(manifest.Shards) == 0 {
		t.Fatal("в манифесте ноль шардов — синтезировать нечего")
	}
	// Сколько коллекций в дереве — свод сверяет сумму описей именно с этим
	// числом, поэтому оно спрашивается у дерева, а не выписывается.
	total := gvTreeCollectionCount(t, root)
	if total == 0 {
		t.Fatal("коллекций в дереве ноль — синтезированный зелёный был бы зелёным на пустом")
	}

	write := func(dir string, marked string) {
		t.Helper()
		for i, s := range manifest.Shards {
			v := map[string]any{
				"shard": s.ID, "suites": s.Suites,
				"expected": 0, "reported": 0, "requests": 0, "assertions": 0,
				"failed": 0, "unanswered": 0, "script_failed": 0,
				"empty": []string{}, "missing": []string{},
			}
			if i == 0 {
				v["expected"] = total
				v["reported"] = total
				v["requests"] = total
				v["assertions"] = total
			}
			if s.ID == marked {
				v["reported"] = 0
				v["assertions"] = 0
				v["precondition"] = map[string]any{
					"unmet": true, "kind": "external-chart-source",
					"detail": "синтезировано доказательством гейта",
				}
			}
			b, err := json.Marshal(v)
			if err != nil {
				t.Fatalf("опись шарда не собрана: %v", err)
			}
			if err := os.WriteFile(filepath.Join(dir, "shard-verdict-"+s.ID+".json"), b, 0o644); err != nil {
				t.Fatalf("опись шарда не записана: %v", err)
			}
		}
	}
	run := func(dir string) (int, string) {
		t.Helper()
		cmd := exec.Command("python3", ".github/scripts/aggregate-shard-verdicts.py", "--dir", dir,
			"--plan-result", "success", "--shard-result", "success")
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		code := 0
		var ee *exec.ExitError
		if err != nil {
			if ok := asExitError(err, &ee); ok {
				code = ee.ExitCode()
			} else {
				t.Fatalf("владелец не исполнился вовсе: %v — это «не выполнилось», "+
					"а не вердикт о нём", err)
			}
		}
		return code, string(out)
	}

	// Положительный близнец: отметки нет, дерево отчиталось целиком → нулевой код.
	green := t.TempDir()
	write(green, "")
	if code, out := run(green); code != 0 {
		t.Fatalf("законный полный набор дал код %d — владелец не способен быть зелёным, "+
			"и его краснота ниже ничего про отметку не докажет.\n%s", code, out)
	}
	// Инъекция одного факта: отметка на первом шарде.
	red := t.TempDir()
	write(red, manifest.Shards[0].ID)
	code, out := run(red)
	if code == 0 {
		t.Fatalf("с отметкой на шарде %q владелец вышел нулём — «условие не создано» "+
			"прошло как зелёное, и форма 3 судьи была бы всеразрешением.\n%s",
			manifest.Shards[0].ID, out)
	}
	t.Logf("владелец сводного вердикта исполнен дважды: без отметки код 0, "+
		"с отметкой на шарде %q код %d; коллекций в дереве %d",
		manifest.Shards[0].ID, code, total)
}

// TestCIRunsGatedVerdictGate — провязка. Гейт, которого конвейер не зовёт с
// `-v`, печатает перепись в никуда: `make test-unit` идёт без `-v`, поэтому
// «ноль находок» стало бы неотличимо от «ноль прочитанного» для читателя
// журнала.
//
// Сверяется РАЗОБРАННОЕ объявление, а не его текст: имя проверки стоит и в
// комментариях этого дерева, и страж по подстроке краснел бы на собственном
// объяснении.
func TestCIRunsGatedVerdictGate(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, workflowsDir, "ci.yaml"))
	if err != nil {
		t.Fatalf("ci.yaml не прочитан: %v — провязка не установлена", err)
	}
	var doc gvDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("ci.yaml не разобран: %v", err)
	}
	pkg := gvPackageRel(t, root)
	want := "go test ./" + pkg + "/ -run " + gvGateTestName
	var found, withShort bool
	for _, job := range doc.Jobs {
		for _, st := range job.Steps {
			if !strings.Contains(st.Run, want) {
				continue
			}
			found = true
			for _, line := range strings.Split(st.Run, "\n") {
				if strings.Contains(line, want) && strings.Contains(line, "-short") {
					withShort = true
				}
			}
		}
	}
	if !found {
		t.Fatalf("ci.yaml не зовёт %q ни в одном шаге. Гейт, которого никто не "+
			"зовёт с -v, — тот же класс, что он ловит: перепись печатается в никуда", want)
	}
	if withShort {
		t.Fatal("ci.yaml зовёт гейт с -short — вердикт остаётся, а перепись прячется")
	}
}

// TestGatedVerdictJudgeOnPostureTreeInput — инъекция НАСТОЯЩИМ входом из дерева
// по двум осям, которых синтетика не закрывает.
//
// Синтетика доказывает форму, а не существо: она не отвечает на вопрос,
// совпадает ли форма, СТОЯЩАЯ В ДЕРЕВЕ, с той, которую судья умеет читать.
// Предмет здесь — `production-posture.yml`, где производитель красного записан
// формой 3 (отметка доезжает в шаг блоком `env:`, ненулевой код даёт
// отслеживаемый скрипт переписи).
//
// Три утверждения, и каждое опровергается своим прогоном:
//
//  1. КОНТРОЛЬ — как есть в дереве судья молчит. Без него всякая находка ниже
//     неотличима от находки, которая была и до инъекции.
//  2. ИНЪЕКЦИЯ ПРОИЗВОДИТЕЛЯ — шаг переписи снят: гасящая отметка осталась,
//     красное производить некому, находка обязана назвать задание.
//  3. ИНЪЕКЦИЯ ПРИЗНАКА «РАБОТА ПРОИЗВОДИТ ОТМЕТКУ» — на дереве БЕЗ
//     производителя дополнительно сняты читатели отметки. Довод «гасит
//     вердиктные шаги» этим снимается целиком, и если бы судья знал только
//     его, он бы СМОЛЧАЛ: ровно этим состоянием работа посадки и жила до
//     полосы `lane/posture-verdict` — два производителя отметки, ноль
//     читателей, зелёный исход без вердикта. Находка обязана остаться.
func TestGatedVerdictJudgeOnPostureTreeInput(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	marks, scripts := gvCollectMarks(t, root)

	const subject = workflowsDir + "/production-posture.yml"
	const mark = "STAND_PRECONDITION_UNMET"
	raw, err := os.ReadFile(filepath.Join(root, subject))
	if err != nil {
		t.Fatalf("не прочитан %s: %v — инъекции не на чем ставить", subject, err)
	}

	if findings, _ := judgeGatedVerdict(subject, string(raw), marks, scripts); len(findings) != 0 {
		t.Fatalf("КОНТРОЛЬ: на настоящем %s судья дал находки %v — производитель "+
			"красного в дереве ЕСТЬ (шаг переписи исходов), значит распознаватель "+
			"его формы не знает, и всякая инъекция ниже неотличима от этого же",
			subject, findings)
	}

	// Разбор общий для обеих инъекций: правится копия в памяти, дерево не трогается.
	load := func() (map[string]any, []any) {
		t.Helper()
		var doc map[string]any
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("%s не разобран: %v", subject, err)
		}
		jobs, ok := doc["jobs"].(map[string]any)
		if !ok {
			t.Fatalf("%s: нет разбираемого `jobs:` — предпосылка инъекции не выполняется", subject)
		}
		job, ok := jobs["stand"].(map[string]any)
		if !ok {
			t.Fatalf("%s: задания `stand` нет — предпосылка инъекции не выполняется", subject)
		}
		steps, ok := job["steps"].([]any)
		if !ok || len(steps) == 0 {
			t.Fatalf("%s: у `stand` нет разбираемых шагов", subject)
		}
		return job, steps
	}
	emit := func(doc map[string]any) string {
		t.Helper()
		b, err := yaml.Marshal(doc)
		if err != nil {
			t.Fatalf("инъекция не собралась обратно: %v", err)
		}
		return string(b)
	}
	// Шаг-производитель НАХОДИТСЯ по признаку, а не по имени: отметка доезжает в
	// него блоком `env:`. Поиск по названию шага пережил бы переименование.
	dropProducer := func(job map[string]any, steps []any) []any {
		t.Helper()
		var kept []any
		var dropped int
		for _, s := range steps {
			st, _ := s.(map[string]any)
			var names bool
			if env, ok := st["env"].(map[string]any); ok {
				for _, v := range env {
					if str, ok := v.(string); ok && strings.Contains(str, mark) {
						names = true
					}
				}
			}
			if names {
				dropped++
				continue
			}
			kept = append(kept, s)
		}
		if dropped != 1 {
			t.Fatalf("%s: шагов, которым отметка доезжает блоком env, оказалось %d, "+
				"а не 1 — предпосылка инъекции не выполняется, и её исход ничего "+
				"не значил бы", subject, dropped)
		}
		job["steps"] = kept
		return kept
	}

	// (2) Снят производитель красного.
	docA, stepsA := load()
	dropProducer(docA, stepsA)
	findings, _ := judgeGatedVerdict(subject, emit(mustDoc(t, raw, docA)), marks, scripts)
	if !namesJob(findings, "stand") {
		t.Fatalf("ИНЪЕКЦИЯ ПРОИЗВОДИТЕЛЯ: шаг переписи снят, гасящая отметка осталась — "+
			"судья обязан назвать задание stand, получено %v", findings)
	}

	// (3) На том же дереве сняты ЧИТАТЕЛИ отметки.
	docB, stepsB := load()
	kept := dropProducer(docB, stepsB)
	var unread int
	for _, s := range kept {
		st, _ := s.(map[string]any)
		cond, _ := st["if"].(string)
		if strings.Contains(cond, "env."+mark) {
			st["if"] = "${{ always() }}"
			unread++
		}
	}
	if unread == 0 {
		t.Fatalf("%s: ни один шаг `stand` не читает отметку условием — снимать нечего, "+
			"и предпосылка третьей инъекции не выполняется", subject)
	}
	findings, _ = judgeGatedVerdict(subject, emit(mustDoc(t, raw, docB)), marks, scripts)
	if !namesJob(findings, "stand") {
		t.Fatalf("ИНЪЕКЦИЯ ПРИЗНАКА «РАБОТА ПРОИЗВОДИТ ОТМЕТКУ»: снято %d читателей "+
			"отметки у работы, которая её ПРОИЗВОДИТ (два шага подъёма), производителя "+
			"красного нет — судья смолчал: %v. Признак «гасит» один эту работу не "+
			"видит, и ровно так она зеленела без вердикта", unread, findings)
	}
}

// mustDoc — общий корень документа с подменённым `jobs:`. Инъекция правит
// задание на месте, а собирать обратно надо ВЕСЬ документ.
func mustDoc(t *testing.T, raw []byte, job map[string]any) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("документ не разобран: %v", err)
	}
	jobs, _ := doc["jobs"].(map[string]any)
	jobs["stand"] = job
	return doc
}

func namesJob(findings []string, job string) bool {
	for _, f := range findings {
		if strings.Contains(f, "задание "+job) {
			return true
		}
	}
	return false
}
