// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// consoleprobeenvgiven_injection_test.go — гейт отдачи переменных набора
// доказан инъекцией в обе стороны: недоданная переменная краснеет и называет
// себя и читателя; каждая законная форма чтения и отдачи — молчит.
package repohygiene

import (
	"strings"
	"testing"
)

const injWorkflowHead = `
env:
  CLUSTER_NAME: x
jobs:
  probes:
    steps:
      - id: probes
`

func injWorkflow(stepTail string) string { return injWorkflowHead + stepTail }

func injSuite(extra map[string]string) map[string]string {
	s := map[string]string{
		"ui-future/e2e/playwright.config.ts": "const BASE = process.env.KACHO_CONSOLE_URL;\n" +
			"const exe = process.env.KACHO_CHROMIUM ? 1 : 0;\n" +
			"const measure = process.env.KACHO_CONSOLE_MEASURE === \"1\";\n",
	}
	for k, v := range extra {
		s[k] = v
	}
	return s
}

const injGivesURL = `        env:
          KACHO_CONSOLE_URL: u
`

func TestProbeEnvGivenInjection_ControlIsSilent(t *testing.T) {
	t.Parallel()
	findings, census := checkProbeEnvGiven("wf.yml", injWorkflow(injGivesURL+"        run: npx playwright test\n"), injSuite(nil))
	if len(findings) != 0 {
		t.Fatalf("контроль: ждали тишины, получили %v", findings)
	}
	if census.ProbeJobs != 1 || census.Variables != 2 {
		t.Fatalf("контроль: перепись %+v, ждали 1 джобу и 2 переменные", census)
	}
}

func TestProbeEnvGivenInjection_ConstantNotGivenIsFound(t *testing.T) {
	t.Parallel()
	suite := injSuite(map[string]string{
		"ui-future/e2e/specs/admin.ts": `export const ADMIN_ENV = "KACHO_CLOUD_ADMIN_EMAIL";` + "\n",
	})
	findings, _ := checkProbeEnvGiven("wf.yml", injWorkflow(injGivesURL+"        run: npx playwright test\n"), suite)
	if len(findings) != 1 || !strings.Contains(findings[0], "`KACHO_CLOUD_ADMIN_EMAIL`") ||
		!strings.Contains(findings[0], "ui-future/e2e/specs/admin.ts") || !strings.Contains(findings[0], "`probes`") {
		t.Fatalf("недоданная переменная константой: ждали одну находку с именем, читателем и шагом, получили %v", findings)
	}
}

func TestProbeEnvGivenInjection_BracketReadNotGivenIsFound(t *testing.T) {
	t.Parallel()
	suite := injSuite(map[string]string{
		"ui-future/e2e/preconditions/x.precondition.ts": `const v = process.env["KACHO_X_Y"];` + "\n",
	})
	findings, _ := checkProbeEnvGiven("wf.yml", injWorkflow(injGivesURL+"        run: npx playwright test\n"), suite)
	if len(findings) != 1 || !strings.Contains(findings[0], "`KACHO_X_Y`") {
		t.Fatalf("недоданная переменная чтением в скобках: ждали одну находку, получили %v", findings)
	}
}

func TestProbeEnvGivenInjection_CommentIsNotARead(t *testing.T) {
	t.Parallel()
	suite := injSuite(map[string]string{
		"ui-future/e2e/specs/doc.ts": "// process.env.KACHO_ONLY_IN_PROSE\n/**\n * const A = \"KACHO_ALSO_PROSE\";\n */\n",
	})
	findings, census := checkProbeEnvGiven("wf.yml", injWorkflow(injGivesURL+"        run: npx playwright test\n"), suite)
	if len(findings) != 0 || census.Variables != 2 {
		t.Fatalf("имя в комментарии чтением не является: находки %v, перепись %+v", findings, census)
	}
}

func TestProbeEnvGivenInjection_EveryLawfulGivingFormIsSilent(t *testing.T) {
	t.Parallel()
	suite := injSuite(map[string]string{
		"ui-future/e2e/specs/admin.ts": `export const A = "KACHO_A";` + "\n" + `export const B: string = "KACHO_B";` + "\n" +
			`const c = process.env.KACHO_C;` + "\n",
	})
	tail := injGivesURL + "          KACHO_C: c\n" +
		"        run: |\n" +
		"          export KACHO_A=\"$(cat a)\"\n" +
		"          KACHO_B=b npx playwright test\n"
	findings, census := checkProbeEnvGiven("wf.yml", injWorkflow(tail), suite)
	if len(findings) != 0 || census.Variables != 6 {
		t.Fatalf("законные формы отдачи (env шага, export, префикс команды): находки %v, перепись %+v", findings, census)
	}
}

func TestProbeEnvGivenInjection_WorkflowAndJobEnvAreGiving(t *testing.T) {
	t.Parallel()
	wf := `
env:
  KACHO_CONSOLE_URL: u
jobs:
  probes:
    env:
      KACHO_A: a
    steps:
      - id: probes
        run: npx playwright test
`
	suite := injSuite(map[string]string{"ui-future/e2e/specs/a.ts": `const A = "KACHO_A";` + "\n"})
	if findings, _ := checkProbeEnvGiven("wf.yml", wf, suite); len(findings) != 0 {
		t.Fatalf("env уровня файла и джобы — законная отдача: %v", findings)
	}
}

func TestProbeEnvGivenInjection_GivingInACommentIsNotGiving(t *testing.T) {
	t.Parallel()
	suite := injSuite(map[string]string{"ui-future/e2e/specs/a.ts": `const A = "KACHO_A";` + "\n"})
	tail := injGivesURL + "        run: |\n          # export KACHO_A=x\n          npx playwright test\n"
	findings, _ := checkProbeEnvGiven("wf.yml", injWorkflow(tail), suite)
	if len(findings) != 1 || !strings.Contains(findings[0], "`KACHO_A`") {
		t.Fatalf("присваивание в комментарии тела отдачей не является: %v", findings)
	}
}

func TestProbeEnvGivenInjection_OptionalKnobWithoutReaderExpires(t *testing.T) {
	t.Parallel()
	// Читатель второй необязательной ручки остаётся: истекает ровно одна — та, чьего читателя сняли.
	suite := map[string]string{"ui-future/e2e/playwright.config.ts": "const BASE = process.env.KACHO_CONSOLE_URL;\n" +
		"const measure = process.env.KACHO_CONSOLE_MEASURE === \"1\";\n"}
	findings, _ := checkProbeEnvGiven("wf.yml", injWorkflow(injGivesURL+"        run: npx playwright test\n"), suite)
	if len(findings) != 1 || !strings.Contains(findings[0], "`KACHO_CHROMIUM`") || !strings.Contains(findings[0], "нечего исключать") {
		t.Fatalf("необязательная ручка без читателя обязана истечь находкой: %v", findings)
	}
}

func TestProbeEnvGivenInjection_NoProbeStepIsAnEmptyWalk(t *testing.T) {
	t.Parallel()
	_, census := checkProbeEnvGiven("wf.yml", injWorkflow("        run: echo нет проб\n"), injSuite(nil))
	if census.ProbeJobs != 0 {
		t.Fatalf("без шага проб перепись обязана показать ноль джоб: %+v", census)
	}
}
