// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Доказательство того, что гейт «идентификатор задания и шага машиночитаем»
// СПОСОБЕН упасть — и что падает он на существе, а не на языке текста.
//
// Инъекция идёт в обе стороны, потому что одного «краснеет» мало (проверка,
// краснеющая на всём, ничего не измеряет), и одного «молчит» мало тоже (молчание
// бывает от того, что читать не стали):
//
//	кириллический ключ задания      → краснеет, называя координату и сам ключ;
//	законный близнец                → молчит, и перепись его ЗАСЧИТЫВАЕТ;
//	кириллический id шага           → краснеет (второй вид идентификатора);
//	ключ, начатый цифрой            → краснеет (форма шире одной кириллицы);
//	ключ с точкой                   → краснеет (латиница сама по себе не спасает);
//	объявление вовсе без заданий    → молчит, перепись заданий = 0;
//	неразбираемый YAML              → находка «файл НЕ проверен», а не тишина.
//
// БЛИЗНЕЦ НАСТОЯЩИЙ, А НЕ КОПИЯ КРАСНОГО СЛУЧАЯ. Он несёт кириллицу ВЕЗДЕ, где
// она законна и встречается в этом дереве: подпись процесса, подпись задания,
// подпись шага, комментарий, текст команды, значение переменной окружения. Отличие
// от красного случая ровно одно — ключ. Побайтовая копия с латинским ключом
// доказывала бы лишь то, что гейт умеет молчать на пустом месте; этот близнец
// доказывает, что он не путает ИМЯ с ПОДПИСЬЮ.
//
// Обе половины гоняют ТУ ЖЕ функцию (`auditWorkflowIdentifiers`), что и прогон по
// дереву: проба, повторяющая логику гейта своей копией, доказывала бы свойство
// копии.
package repohygiene

import (
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// Синтетические объявления. Каркас взят у настоящего процесса этого дерева
// (`.github/workflows/required-verdict.yml`), а не выдуман: в нём и был дефект.
// ─────────────────────────────────────────────────────────────────────────────

// ДЕФЕКТ. Ключ задания кириллицей — ровно #1073.
const synthWorkflowCyrillicJobKey = `# Сводный вердикт запроса на слияние.
name: сводный вердикт

on:
  pull_request:

jobs:
  все-проверки-зелены:
    name: сводный вердикт (все проверки завершились и зелены)
    if: github.event_name == 'pull_request'
    runs-on: ubuntu-latest
    steps:
      - name: ждать остальные проверки и вынести вердикт
        run: echo "вердикт"
`

// ЗАКОННЫЙ БЛИЗНЕЦ. Кириллица во всех местах, где она законна; ключ — латиницей.
const synthWorkflowCyrillicNamesOnly = `# Сводный вердикт запроса на слияние.
name: сводный вердикт

on:
  pull_request:

jobs:
  all-checks-green:
    name: сводный вердикт (все проверки завершились и зелены)
    if: github.event_name == 'pull_request'
    runs-on: ubuntu-latest
    steps:
      - name: ждать остальные проверки и вынести вердикт
        id: wait-for-checks
        env:
          SELF: сводный вердикт (все проверки завершились и зелены)
        run: echo "вердикта НЕТ, и это не «зелено»"
`

// ДЕФЕКТ. Ключ задания законен, а идентификатор ШАГА — нет.
const synthWorkflowCyrillicStepID = `name: проба
on:
  pull_request:
jobs:
  all-checks-green:
    runs-on: ubuntu-latest
    steps:
      - name: подписать шаг по-русски — это законно
        id: ждать-проверки
        run: echo "да"
`

// ДЕФЕКТ. Форма шире одной кириллицы: имя, начатое цифрой, тоже не имя.
const synthWorkflowDigitLeadingJobKey = `name: проба
on:
  pull_request:
jobs:
  2nd-wave:
    runs-on: ubuntu-latest
    steps:
      - run: echo "да"
`

// ДЕФЕКТ. Латиница сама по себе не спасает: точка вне допустимого набора.
const synthWorkflowDottedJobKey = `name: проба
on:
  pull_request:
jobs:
  verdict.all:
    runs-on: ubuntu-latest
    steps:
      - run: echo "да"
`

// ЗАКОННО. Объявление без заданий — например, только расписание и вызов вручную.
const synthWorkflowWithoutJobs = `name: снос по расписанию
on:
  schedule:
    - cron: '17 6 * * 1'
  workflow_dispatch:
`

// НЕ ПРОВЕРЕН. Разбор не удался — это находка, а не молчание: файл, который гейт
// не смог прочесть, нельзя засчитать в перепись как осмотренный.
const synthWorkflowBrokenYAML = `name: проба
on:
  pull_request:
jobs:
   a:
  b:
     - и это не отображение
`

func TestWorkflowJobIdentifierDetectorSeesBothForms(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		wantHit  bool
		wantIn   string // подстрока, которую находка обязана назвать
		wantJobs int    // сколько заданий обязана засчитать перепись
	}{
		{
			name:     "кириллический ключ задания — находка с координатой и самим ключом",
			body:     synthWorkflowCyrillicJobKey,
			wantHit:  true,
			wantIn:   "все-проверки-зелены",
			wantJobs: 1,
		},
		{
			name:     "кириллица только в подписях, комментарии и командах — молчит, и задание ЗАСЧИТАНО",
			body:     synthWorkflowCyrillicNamesOnly,
			wantHit:  false,
			wantJobs: 1,
		},
		{
			name:     "кириллический id шага — находка",
			body:     synthWorkflowCyrillicStepID,
			wantHit:  true,
			wantIn:   "ждать-проверки",
			wantJobs: 1,
		},
		{
			name:     "ключ, начатый цифрой — находка (форма шире одной кириллицы)",
			body:     synthWorkflowDigitLeadingJobKey,
			wantHit:  true,
			wantIn:   "2nd-wave",
			wantJobs: 1,
		},
		{
			name:     "ключ с точкой — находка (латиница сама по себе не спасает)",
			body:     synthWorkflowDottedJobKey,
			wantHit:  true,
			wantIn:   "verdict.all",
			wantJobs: 1,
		},
		{
			name:     "объявление без заданий — законно, молчит",
			body:     synthWorkflowWithoutJobs,
			wantHit:  false,
			wantJobs: 0,
		},
		{
			name:     "неразбираемый YAML — находка «файл НЕ проверен», а не тишина",
			body:     synthWorkflowBrokenYAML,
			wantHit:  true,
			wantIn:   "НЕ проверен",
			wantJobs: 0,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			findings, census := auditWorkflowIdentifiers(".github/workflows/synthetic.yml", c.body)

			if got := len(findings) > 0; got != c.wantHit {
				t.Fatalf("находок %d, ожидалось hit=%v; находки: %v", len(findings), c.wantHit, findings)
			}
			if census.jobs != c.wantJobs {
				t.Errorf("перепись заданий %d, ожидалось %d — «ноль находок» обязано быть "+
					"отличимо от «ноль прочитанного»", census.jobs, c.wantJobs)
			}
			if !c.wantHit {
				return
			}

			joined := strings.Join(findings, "\n")
			if c.wantIn != "" && !strings.Contains(joined, c.wantIn) {
				t.Errorf("находка не называет %q — по такому тексту виновника не найти:\n%s", c.wantIn, joined)
			}
			// Координата обязательна: находка без адреса не чинится.
			if !strings.Contains(joined, ".github/workflows/synthetic.yml:") {
				t.Errorf("находка без координаты `файл:строка`:\n%s", joined)
			}
		})
	}
}

// TestWorkflowJobIdentifierGateNamesTheLine — координата указывает на ту строку,
// где стоит виновный ключ, а не на начало файла: правка идёт по адресу.
func TestWorkflowJobIdentifierGateNamesTheLine(t *testing.T) {
	findings, _ := auditWorkflowIdentifiers("w.yml", synthWorkflowCyrillicJobKey)
	if len(findings) != 1 {
		t.Fatalf("находок %d, ожидалась одна: %v", len(findings), findings)
	}
	// `все-проверки-зелены` стоит восьмой строкой синтетики.
	if !strings.HasPrefix(findings[0], "w.yml:8:") {
		t.Errorf("координата не указывает на строку ключа (ожидалось w.yml:8): %s", findings[0])
	}
}
