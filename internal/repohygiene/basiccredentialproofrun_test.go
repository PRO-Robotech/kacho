// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// basiccredentialproofrun_test.go — доказательство формы базового удостоверения
// обязано ПРОИЗВОДИТЬСЯ ПРОГОНОМ, а не просто лежать в дереве
// (`PRO-Robotech/kacho#1253`).
//
// # Предмет
//
// Во всём сквозном прогоне не было НИ ОДНОГО зелёного утверждения, читающего
// секрет базового удостоверения из УСПЕШНОГО ответа. Следствие названо в задаче
// и оно не теоретическое: образец формы ждал разделитель после префикса вида
// (`kacho_uoc_…`), тогда как `ids.NewID` чеканит префикс СЛИТНО с телом. Такое
// утверждение не совпало бы НИ ПРИ КАКОМ ответе — и было неотличимо от
// исправного ровно потому, что положительного прохода не существовало.
//
// Отсюда предикат снятия задачи, и он состоит из двух частей: форма сверена со
// значением, ОТЧЕКАНЕННЫМ продуктом, И это утверждение произведено ПРОГОНОМ.
// Вторую часть держит этот гейт.
//
// # Почему «файл есть» — не то же самое, что «прогон есть»
//
// Проверка, которую никто не зовёт, ничего не производит: её зелёное существует
// только в чужой голове. Класс измерен на этом же дереве в день заведения гейта
// — перепись самопроверок сквозных наборов и их вызывающих:
//
//	самопроверок в дереве                 15
//	из них без единого вызывающего         5
//
// Одна из пяти — `selftest_basic_access_token.py`, то есть ЕДИНСТВЕННЫЙ
// производитель зелёного утверждения о форме, который не требует поднятого
// стенда. Остальные четыре — предметы своих задач, и этот гейт их не судит: он
// узкий по построению, потому что чужой предмет, затянутый в чужую же полосу,
// делает вердикт непрослеживаемым.
//
// # Читается ИСПОЛНЯЕМАЯ часть, а не текст
//
// Имя скрипта стоит и в прозе — в шапке набора, в его README и в комментарии
// шага, который его зовёт. Гейт по подстроке краснел бы на собственном
// объяснении и зеленел бы на шаге, откуда вызов сняли, а комментарий оставили.
// Поэтому: YAML разбирается, берутся тела `run:`, и из них выбрасываются строки
// оболочечных комментариев.
//
// # Пара на каждой оси
//
// Инъекция ниже требует обеих сторон: вызов в исполняемой части — молчание;
// упоминание в комментарии YAML, в оболочечном комментарии внутри `run:`, вызов
// СОСЕДНЕГО скрипта и отсутствие упоминания вовсе — находка. Без второй стороны
// гейт мерил бы наличие слова, а не факт вызова.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const (
	// credentialFormDeclaration — ЕДИНСТВЕННОЕ объявление формы. Вторая копия
	// образца уже расходилась молча, ради чего задача и заведена.
	credentialFormDeclaration = "services/iam/tests/newman/credential-secret-form.json"

	// credentialFormMintProof — сверка объявления с ЧЕКАНКОЙ продукта. Идёт
	// обычным `go test ./...`, то есть свой прогон у неё уже есть.
	credentialFormMintProof = "services/iam/tests/newman/scripts/credsecretmint/form_test.go"

	// credentialFormRunProof — НАСТОЯЩИЙ newman по НАСТОЯЩЕЙ коллекции против
	// подставного края: единственный производитель зелёного утверждения о форме,
	// которому не нужен поднятый стенд. Своего прогона у него нет by
	// construction — его обязан звать конвейер.
	credentialFormRunProof = "services/iam/tests/newman/scripts/selftest_basic_access_token.py"
)

// runBodiesDoc — то немногое из workflow, что нужно этому гейту.
type runBodiesDoc struct {
	Jobs map[string]struct {
		Steps []struct {
			Name string `yaml:"name"`
			Run  string `yaml:"run"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

// executableRunBodies — тела `run:` без строк оболочечных комментариев.
//
// Комментарий YAML отбрасывает сам разборщик; комментарий ОБОЛОЧКИ живёт внутри
// строки `run:` и от разборщика не отличим — его снимаем здесь. Именно он и есть
// тот случай, ради которого гейт не ищет подстроку по сырому файлу: шаг, у
// которого вызов закомментировали, а объяснение оставили, обязан быть находкой.
func executableRunBodies(raw string) ([]string, int, error) {
	var doc runBodiesDoc
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, 0, err
	}
	var out []string
	steps := 0
	for _, job := range doc.Jobs {
		for _, st := range job.Steps {
			if strings.TrimSpace(st.Run) == "" {
				continue
			}
			steps++
			var code []string
			for _, ln := range strings.Split(st.Run, "\n") {
				if strings.HasPrefix(strings.TrimSpace(ln), "#") {
					continue
				}
				code = append(code, ln)
			}
			out = append(out, strings.Join(code, "\n"))
		}
	}
	return out, steps, nil
}

// invocationsOf — сколько тел `run:` действительно зовут названный скрипт.
func invocationsOf(script string, bodies []string) int {
	n := 0
	for _, b := range bodies {
		if strings.Contains(b, script) {
			n++
		}
	}
	return n
}

// TestBasicCredentialFormProofIsProducedByARun — по дереву.
func TestBasicCredentialFormProofIsProducedByARun(t *testing.T) {
	root := repoRoot(t)

	// ── Предпосылка. Гейт стережёт предмет; исчез предмет — гейт обязан сказать
	// это вслух, а не промолчать зелёным на пустом месте.
	for _, rel := range []string{
		credentialFormDeclaration, credentialFormMintProof, credentialFormRunProof,
	} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Fatalf("предпосылка не выполнена: %s не читается (%v).\n"+
				"Это один из трёх предметов задачи #1253 — объявление формы и два его "+
				"читателя. Если предмет снят осознанно, снимайте вместе с ним и этот гейт: "+
				"проверка, потерявшая предмет, утверждает о дереве несуществующее", rel, err)
		}
	}

	// ── Сверка с чеканкой обязана оставаться в умолчательном прогоне Go.
	// Ограничение сборки вынуло бы её из `go test ./...` молча: пакет собрался
	// бы, вердикта бы не было, и «зелёный прогон» относился бы к меньшему.
	mintRaw, err := os.ReadFile(filepath.Join(root, credentialFormMintProof))
	if err != nil {
		t.Fatalf("%s не прочитан: %v", credentialFormMintProof, err)
	}
	if strings.Contains(string(mintRaw), "//go:build") {
		t.Errorf("%s несёт ограничение сборки — умолчательный `go test ./...` может её "+
			"не исполнить, и сверка формы с чеканкой продукта перестанет производиться "+
			"прогоном, оставаясь на вид written и зелёной", credentialFormMintProof)
	}

	// ── Производитель зелёного утверждения обязан иметь вызывающего.
	files := listWorkflows(t, root)
	if len(files) == 0 {
		t.Fatalf("в %s не найдено ни одного workflow — обход сломан, а не дерево чисто",
			workflowsDir)
	}

	runSteps, calls, parsed := 0, 0, 0
	for _, f := range files {
		raw, rerr := os.ReadFile(filepath.Join(root, f))
		if rerr != nil {
			t.Errorf("%s не прочитан: %v — файл НЕ проверен", f, rerr)
			continue
		}
		bodies, steps, perr := executableRunBodies(string(raw))
		if perr != nil {
			t.Errorf("%s: не разобран YAML: %v — файл НЕ проверен", f, perr)
			continue
		}
		parsed++
		runSteps += steps
		calls += invocationsOf(credentialFormRunProof, bodies)
	}

	if parsed == 0 {
		t.Fatal("не разобрано ни одного workflow — вердикт беспредметен")
	}
	if calls == 0 {
		t.Errorf("%s не зовётся НИ ОДНИМ шагом конвейера (осмотрено workflow %d, тел `run:` %d).\n"+
			"Это единственный производитель зелёного утверждения о форме базового "+
			"удостоверения, которому не нужен поднятый стенд, — и он не производит ничего. "+
			"Предикат снятия #1253 требует, чтобы такое утверждение было В ПРОГОНЕ: пока "+
			"вызывающего нет, «форма верна» держится на том, что никто не проверял обратного.\n"+
			"Упоминание в комментарии вызовом не является и здесь намеренно не считается",
			credentialFormRunProof, len(files), runSteps)
	}

	t.Logf("перепись: workflow осмотрено %d (разобрано %d) · тел `run:` %d · "+
		"вызовов производителя %d · читателей объявления 2 (чеканка + newman)",
		len(files), parsed, runSteps, calls)
}

// TestBasicCredentialProofDetectorSeesBothForms — инъекция в обе стороны.
//
// Без второй стороны гейт мерил бы наличие СЛОВА: сырой файл содержит имя
// скрипта и в прозе, и в шапке шага, который его объясняет.
func TestBasicCredentialProofDetectorSeesBothForms(t *testing.T) {
	cases := []struct {
		name     string
		yaml     string
		wantCall bool
	}{
		{
			name: "вызов в исполняемой части — молчит",
			yaml: "jobs:\n  g:\n    steps:\n      - name: проба формы\n" +
				"        run: python3 " + credentialFormRunProof + "\n",
			wantCall: true,
		},
		{
			name: "имя только в комментарии YAML — находка",
			yaml: "jobs:\n  g:\n    steps:\n" +
				"      # тут зовётся " + credentialFormRunProof + "\n" +
				"      - name: проба формы\n        run: echo ok\n",
			wantCall: false,
		},
		{
			name: "вызов закомментирован в оболочке, объяснение осталось — находка",
			yaml: "jobs:\n  g:\n    steps:\n      - name: проба формы\n" +
				"        run: |\n" +
				"          # python3 " + credentialFormRunProof + "\n" +
				"          echo ok\n",
			wantCall: false,
		},
		{
			name: "зовётся СОСЕДНИЙ скрипт того же каталога — находка",
			yaml: "jobs:\n  g:\n    steps:\n      - name: чужая проба\n" +
				"        run: python3 services/iam/tests/newman/scripts/selftest_authz_allow_lanes.py\n",
			wantCall: false,
		},
		{
			name:     "шага `run:` нет вовсе — находка",
			yaml:     "jobs:\n  g:\n    steps:\n      - uses: actions/checkout@v7\n",
			wantCall: false,
		},
		{
			name: "вызов среди нескольких строк тела — молчит",
			yaml: "jobs:\n  g:\n    steps:\n      - name: пробы\n        run: |\n" +
				"          python3 services/iam/tests/newman/scripts/selftest_authz_allow_lanes.py\n" +
				"          python3 " + credentialFormRunProof + "\n",
			wantCall: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bodies, _, err := executableRunBodies(c.yaml)
			if err != nil {
				t.Fatalf("синтетика не разобралась: %v", err)
			}
			got := invocationsOf(credentialFormRunProof, bodies) > 0
			if got != c.wantCall {
				t.Errorf("вызов распознан как %v, ожидалось %v", got, c.wantCall)
			}
		})
	}
}
