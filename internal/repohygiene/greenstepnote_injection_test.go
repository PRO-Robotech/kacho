// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// greenstepnote_injection_test.go — доказательство того, что
// TestGreenStepNoteIsNotRaisedToAFailureAnnotation СПОСОБЕН упасть, и падает он
// на существе, а не на форме.
//
// Инъекция подаёт синтетику в ТУ ЖЕ функцию (checkGreenStepNote), которая судит
// дерево. Инъекция здесь не украшение: на чистом дереве гейт зелен по построению,
// поэтому «находок 0» о его способности упасть не говорит НИЧЕГО.
//
// У каждой оси — законный близнец, отличающийся ОДНИМ фактом. Без него красное
// доказывало бы лишь то, что гейт умеет краснеть, а не то, что он различает.
package repohygiene

import (
	"strings"
	"testing"
)

// injectedPin — пин, с которого «снята» копия сопоставителя в этих пробах.
const injectedPin = "b7ad1dad31e06c5925ef5d2fc7ad053ef454303e"

func TestGreenStepNoteDetectorSeesBothForms(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		yaml    string
		wantHit string // подстрока ожидаемой находки; пусто — гейт обязан молчать
	}{
		{
			name:    "установка Go с ЧУЖОГО пина — копия сопоставителя могла протухнуть",
			yaml:    "jobs:\n  b:\n    steps:\n      - uses: actions/setup-go@deadbeefdeadbeefdeadbeefdeadbeefdeadbeef\n",
			wantHit: "копия сопоставителя",
		},
		{
			name: "тот же шаг с ТЕМ ЖЕ пином — законный близнец, молчит",
			yaml: "jobs:\n  b:\n    steps:\n      - uses: actions/setup-go@" + injectedPin + "\n",
		},
		{
			name: "цель зовётся через make В ОБХОД судьи — находка",
			yaml: "jobs:\n  b:\n    steps:\n      - name: listauthz\n        run: |\n" +
				"          for svc in vpc registry; do\n" +
				"            make -C \"services/${svc}\" audit-list-filter\n" +
				"          done\n",
			wantHit: "В ОБХОД судьи",
		},
		{
			name: "тот же цикл, но вывод отдан судье — законный близнец, молчит",
			yaml: "jobs:\n  b:\n    steps:\n      - name: listauthz\n        run: |\n" +
				"          for svc in vpc registry; do\n" +
				"            make -C \"services/${svc}\" audit-list-filter >\"${logs}/${svc}.out\" 2>&1 && rc=0 || rc=$?\n" +
				"            if [ \"$rc\" -eq 0 ]; then mv \"${logs}/${svc}.out\" \"${logs}/${svc}.green\"; fi\n" +
				"          done\n" +
				"          python3 .github/scripts/assert-green-gate-notes.py --logs \"${logs}\"\n",
		},
		{
			name: "цель названа КОММЕНТАРИЕМ ДОКУМЕНТА — узла нет, гейт молчит",
			yaml: "jobs:\n  b:\n    steps:\n" +
				"      # make -C services/vpc audit-list-filter — так делать нельзя\n" +
				"      - name: nothing\n        run: echo hi\n",
		},
		{
			name: "цель названа комментарием ОБОЛОЧКИ внутри run — не вызов, гейт молчит",
			yaml: "jobs:\n  b:\n    steps:\n      - name: nothing\n        run: |\n" +
				"          # раньше здесь стоял make -C services/vpc audit-list-filter\n" +
				"          echo hi\n",
		},
		{
			name: "make без цели — не предмет, гейт молчит",
			yaml: "jobs:\n  b:\n    steps:\n      - name: build\n        run: make -C gateway build\n",
		},
		{
			name: "цель НАЗВАНА, но make не зовётся — не предмет, гейт молчит",
			yaml: "jobs:\n  b:\n    steps:\n      - name: talk\n        run: " +
				"echo 'audit-list-filter гоняется соседним заданием'\n",
		},
		{
			name: "самопроверка судьи — не вызов цели, гейт молчит",
			yaml: "jobs:\n  b:\n    steps:\n      - name: judge\n        run: " +
				"python3 .github/scripts/assert-green-gate-notes.py --self-test\n",
		},
		{
			name:    "неразбираемый YAML — файл НЕ проверен, а не чист",
			yaml:    "jobs:\n  b:\n   steps:\n  - uses: [\n",
			wantHit: "НЕ проверен",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			findings, _ := checkGreenStepNote("синтетика.yaml", tc.yaml, injectedPin)
			joined := strings.Join(findings, "\n")
			switch {
			case tc.wantHit == "" && len(findings) != 0:
				t.Fatalf("законный близнец объявлен находкой — гейт ловит форму, а не существо:\n%s", joined)
			case tc.wantHit != "" && len(findings) == 0:
				t.Fatal("заведомый экземпляр НЕ пойман — гейт не способен упасть на этой оси")
			case tc.wantHit != "" && !strings.Contains(joined, tc.wantHit):
				t.Fatalf("находка есть, но называет не тот предмет (ждали %q):\n%s", tc.wantHit, joined)
			}
			if len(findings) > 0 {
				t.Logf("находка: %s", joined)
			}
		})
	}
}

// TestGreenStepNoteCensusCountsWhatItRead — перепись обязана быть отдельным
// утверждением: «ноль находок» отличимо от «ноль прочитанного» только по ней.
func TestGreenStepNoteCensusCountsWhatItRead(t *testing.T) {
	t.Parallel()

	const wired = "jobs:\n  b:\n    steps:\n" +
		"      - uses: actions/setup-go@" + injectedPin + "\n" +
		"      - name: judge self-test\n        run: python3 .github/scripts/assert-green-gate-notes.py --self-test\n" +
		"      - name: listauthz\n        run: |\n" +
		"          for svc in vpc; do echo \"${svc}\"; done |\n" +
		"            python3 .github/scripts/assert-green-gate-notes.py\n"

	findings, census := checkGreenStepNote("синтетика.yaml", wired, injectedPin)
	if len(findings) != 0 {
		t.Fatalf("провязанный конвейер объявлен находкой:\n%s", strings.Join(findings, "\n"))
	}
	if census.SetupGo != 1 || census.PinsOK != 1 {
		t.Errorf("перепись установки Go: получили %d/%d, ждали 1/1", census.SetupGo, census.PinsOK)
	}
	if census.JudgeRuns != 1 {
		t.Errorf("перепись вызовов судьи: получили %d, ждали 1", census.JudgeRuns)
	}
	if census.SelfTests != 1 {
		t.Errorf("перепись самопроверок судьи: получили %d, ждали 1", census.SelfTests)
	}
	if census.DirectMk != 0 {
		t.Errorf("перепись прямых вызовов: получили %d, ждали 0", census.DirectMk)
	}

	// Пустой документ обязан давать ПУСТУЮ перепись, а не молчаливое «чисто»:
	// именно на нём тревогу поднимают утверждения о нуле в самом гейте.
	_, empty := checkGreenStepNote("пусто.yaml", "jobs: {}\n", injectedPin)
	if empty.SetupGo != 0 || empty.JudgeRuns != 0 || empty.SelfTests != 0 {
		t.Errorf("пустой документ дал непустую перепись: %+v", empty)
	}
}
