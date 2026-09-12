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

	marks := map[string]gvMark{
		"STAND_PRECONDITION_UNMET": {
			Name: "STAND_PRECONDITION_UNMET", Value: "1", Known: true,
			Writers: []string{"синтетика/stand-up.sh:1"},
		},
	}
	const gated = "        if: ${{ env.STAND_PRECONDITION_UNMET != '1' }}\n"

	cases := []struct {
		name    string
		yaml    string
		scripts map[string]string
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
			name: "форма 3 — делегирование нижестоящему заданию с владельцем, молчит",
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
			name: "законный близнец: отметку не читает НИ ОДИН шаг — молчит",
			yaml: "jobs:\n  stand:\n    steps:\n      - name: стенд\n" +
				"        run: .github/scripts/stand-up.sh -- make dev-prod-up\n" +
				"      - name: вердикт числами\n        if: ${{ always() }}\n" +
				"        run: go test ./deploy/ -run Posture\n",
			wantHit: false,
		},
		{
			name: "значение отметки из дерева не выводится — находка, а не молчание",
			yaml: "jobs:\n  probes:\n    steps:\n      - name: прогон проб\n" +
				"        if: ${{ env.KACHO_UNKNOWN_MARK != '1' }}\n        run: npx playwright test\n",
			scripts: map[string]string{},
			wantHit: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := marks
			if strings.Contains(tc.yaml, "KACHO_UNKNOWN_MARK") {
				m = map[string]gvMark{
					"KACHO_UNKNOWN_MARK": {Name: "KACHO_UNKNOWN_MARK", Known: false,
						Writers: []string{"синтетика/writer.sh:1"}},
				}
			}
			findings, _, _ := judgeGatedVerdict("синтетика.yml", tc.yaml, m, tc.scripts, nil)
			if got := len(findings) > 0; got != tc.wantHit {
				t.Fatalf("ожидалась находка=%v, получено %v: %v", tc.wantHit, got, findings)
			}
		})
	}
}

// TestDeclaredDebtExpiresFromATreeFact — объявленный долг в обе стороны.
//
// Запись существует затем, чтобы известные два места не запирали работу над
// третьим; она НЕ прощение, и разница проверяема ровно здесь:
//
//	находка, которой запись НЕ названа, остаётся красной;
//	запись, которой нечего исключать, сама становится красной.
//
// Без первой половины запись стала бы всеразрешением; без второй — пережила бы
// свой предмет, и полоса, приносящая производителя, ушла бы, оставив прощение
// действовать на другое задание под прежним именем.
func TestDeclaredDebtExpiresFromATreeFact(t *testing.T) {
	t.Parallel()
	marks := map[string]gvMark{
		"STAND_PRECONDITION_UNMET": {
			Name: "STAND_PRECONDITION_UNMET", Value: "1", Known: true,
			Writers: []string{"синтетика/stand-up.sh:1"},
		},
	}
	const gated = "        if: ${{ env.STAND_PRECONDITION_UNMET != '1' }}\n"
	const twoJobs = "jobs:\n  helm:\n    steps:\n      - name: umbrella template\n" +
		gated + "        run: helm template deploy/helm/umbrella\n" +
		"  probes:\n    steps:\n      - name: прогон проб\n" +
		gated + "        run: npx playwright test\n"

	// Запись названа на одно задание из двух: второе обязано остаться красным.
	known := map[string]string{"синтетика.yml: задание helm": "чинит соседняя полоса"}
	findings, debt, _ := judgeGatedVerdict("синтетика.yml", twoJobs, marks, scriptsNone(), known)
	if len(debt) != 1 {
		t.Fatalf("объявленным долгом названо %d записей, ждали 1: %v", len(debt), debt)
	}
	if len(findings) != 1 {
		t.Fatalf("находок %d, ждали 1 (задание probes записью не названо): %v", len(findings), findings)
	}
	if !strings.Contains(findings[0], "probes") {
		t.Fatalf("находка не про то задание: %v", findings)
	}

	// Та же запись на дереве, где заданию helm производитель красного УЖЕ
	// заведён: исключать больше нечего — красное.
	fixed := strings.Replace(twoJobs,
		"  probes:\n",
		"      - name: условие не создано — работа красная\n"+
			"        if: ${{ always() && env.STAND_PRECONDITION_UNMET == '1' }}\n"+
			"        run: exit 1\n"+
			"  probes:\n", 1)
	_, debt, _ = judgeGatedVerdict("синтетика.yml", fixed, marks, scriptsNone(), known)
	used := map[string]bool{}
	for _, d := range debt {
		used[strings.SplitN(d, " — ", 2)[0]] = true
	}
	stale := gvStaleDebt(known, used)
	if len(stale) != 1 {
		t.Fatalf("устаревших записей %d, ждали 1: %v", len(stale), stale)
	}
	// Законный близнец: пока производителя нет, запись использована и молчит.
	used = map[string]bool{}
	_, debt, _ = judgeGatedVerdict("синтетика.yml", twoJobs, marks, scriptsNone(), known)
	for _, d := range debt {
		used[strings.SplitN(d, " — ", 2)[0]] = true
	}
	if stale := gvStaleDebt(known, used); len(stale) != 0 {
		t.Fatalf("запись с живым предметом объявлена устаревшей: %v", stale)
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
	if findings, _, _ := judgeGatedVerdict(subject, string(raw), marks, scripts, nil); len(findings) != 0 {
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
	findings, _, _ := judgeGatedVerdict(subject, string(injected), marks, scripts, nil)
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
