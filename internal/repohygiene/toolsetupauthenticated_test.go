// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// toolsetupauthenticated_test.go — действие, ставящее инструмент по релизу с
// GitHub, обязано ходить туда С ТОКЕНОМ.
//
// # Предмет
//
// `bufbuild/buf-setup-action` резолвит нужный релиз через GitHub API. Без входа
// `github_token` запрос идёт НЕАУТЕНТИФИЦИРОВАННО, и квота у него общая на IP
// ранера, а не на репозиторий. Пока прогон один, это незаметно; при нескольких
// параллельных квота исчерпывается, действие падает — и шаги, ради которых
// джоба существует, получают `skipped`.
//
// # Почему это не «мигающий прогон», а третья категория
//
// Джоба краснеет, поэтому ложного зелёного здесь нет. Но красное это говорит НЕ
// о предмете проверок: `buf lint`, `buf breaking` и сверка стабов не выполнялись
// вовсе. Вердикт о них выдан, а проверок не было — «не выполнилось», которое
// нельзя ни вычесть из числа находок, ни объяснить. Наблюдалось на прогоне
// 31101264016 (2026-08-06): три шага job `proto` — `skipped`, причина в самом
// действии, к дереву отношения не имеющая.
//
// # Что здесь утверждается
//
// Каждое употребление действия во ВСЕХ файлах рабочих процессов несёт
// `github_token`. Перечень употреблений НЕ выписан списком — он обходится по
// дереву, поэтому новый шаг с тем же действием попадает под правило сам, а не
// когда о нём вспомнят.
//
// Гейт несёт проверку СВОЕЙ предпосылки: ноль прочитанных файлов и ноль
// найденных употреблений — провал, а не чистота. Иначе «ноль находок» стало бы
// неотличимо от «ноль прочитанного» ровно в тот день, когда действие
// переименуют.
package repohygiene

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// tokenNeedingAction — действие, чей вход `github_token` обязателен. Одно, и это
// не упрощение: правило выведено из ЕГО механики (резолв релиза через API с
// квотой по IP). Появится второе такое — добавляется сюда вместе с причиной, а
// не «по аналогии»: у действия без обращения к API предмета у этого требования
// нет.
const tokenNeedingAction = "bufbuild/buf-setup-action"

// tokenInput — имя входа, объявленное самим действием (`action.yml`, вход
// `github_token`). Сверено с описанием действия, а не угадано по привычке:
// соседние действия называют тот же вход `token`, и подстановка чужого имени
// дала бы шаг, который выглядит исправленным и по-прежнему ходит без токена.
const tokenInput = "github_token"

// stepUsingAction — одно употребление действия: файл, строка, есть ли токен.
type stepUsingAction struct {
	File  string
	Line  int
	Token bool
}

// TestToolSetupActionsAuthenticate — обход дерева.
func TestToolSetupActionsAuthenticate(t *testing.T) {
	root := repoRoot(t)
	steps, scanned := stepsUsingTokenNeedingAction(t, root)

	t.Logf("осмотрено файлов рабочих процессов: %d; употреблений %s: %d",
		scanned, tokenNeedingAction, len(steps))

	if scanned == 0 {
		t.Fatalf("не прочитано НИ ОДНОГО файла рабочих процессов — гейт ничего не " +
			"осмотрел, и его молчание описывает не это дерево")
	}
	if len(steps) == 0 {
		t.Fatalf("употреблений %s не нашлось ни одного при %d прочитанных файлах. "+
			"Либо действие переименовали, либо его больше нет: в первом случае гейт "+
			"ослеп, во втором — ему нечего охранять. Оба требуют правки здесь, а не "+
			"зелёного молчания", tokenNeedingAction, scanned)
	}

	for _, f := range judgeTokenNeedingSteps(steps) {
		t.Errorf("%s", f)
	}
}

// judgeTokenNeedingSteps — решающая часть, вынесенная отдельно, чтобы её можно
// было проверить подставными входами, а не только зелёным на дереве.
func judgeTokenNeedingSteps(steps []stepUsingAction) []string {
	var out []string
	for _, s := range steps {
		if s.Token {
			continue
		}
		out = append(out, s.File+":"+itoa(s.Line)+" — шаг ставит "+tokenNeedingAction+
			" без входа `"+tokenInput+"`. Релиз резолвится через GitHub API "+
			"неаутентифицированно, квота такого запроса общая на IP ранера: на её "+
			"исчерпании действие падает, а шаги, ради которых заведена джоба, получают "+
			"`skipped` — вердикт выдаётся о проверках, которые не выполнялись. "+
			"Добавьте `"+tokenInput+": ${{ github.token }}`")
	}
	sort.Strings(out)
	return out
}

var actionUseRe = regexp.MustCompile(`^\s*-\s+uses:\s*` + regexp.QuoteMeta(tokenNeedingAction) + `@`)

// stepsUsingTokenNeedingAction обходит файлы рабочих процессов и собирает
// употребления действия вместе с тем, несёт ли шаг вход с токеном.
//
// Разбор построчный и НАМЕРЕННО ограничен блоком `with:` того же шага: токен,
// найденный где-то ещё в файле (в соседнем шаге, в переменных окружения job),
// этот шаг не аутентифицирует. Признак конца блока — строка, чей отступ не
// больше отступа самой строки `- uses:`, либо начало следующего элемента списка.
func stepsUsingTokenNeedingAction(t *testing.T, root string) (steps []stepUsingAction, scanned int) {
	t.Helper()
	dir := filepath.Join(root, ".github", "workflows")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("чтение %s: %v — каталога рабочих процессов нет, предпосылка гейта "+
			"потеряла предмет", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml") {
			continue
		}
		raw, readErr := os.ReadFile(filepath.Join(dir, name))
		if readErr != nil {
			t.Fatalf("чтение %s: %v", name, readErr)
		}
		scanned++
		rel := ".github/workflows/" + name
		steps = append(steps, scanWorkflowForAction(rel, string(raw))...)
	}
	sort.Slice(steps, func(i, j int) bool {
		if steps[i].File != steps[j].File {
			return steps[i].File < steps[j].File
		}
		return steps[i].Line < steps[j].Line
	})
	return steps, scanned
}

// scanWorkflowForAction — чистый разбор одного файла; вынесен, чтобы инъекция
// шла НАСТОЯЩИМ содержимым рабочего процесса, а не подставной структурой.
func scanWorkflowForAction(rel, body string) []stepUsingAction {
	var out []stepUsingAction
	lines := strings.Split(body, "\n")
	for i, ln := range lines {
		if !actionUseRe.MatchString(ln) {
			continue
		}
		out = append(out, stepUsingAction{
			File:  rel,
			Line:  i + 1,
			Token: stepDeclaresToken(lines, i),
		})
	}
	return out
}

// stepDeclaresToken — несёт ли ЭТОТ шаг вход с токеном. Область поиска
// ограничена телом шага: следующая строка с отступом не больше, чем у `- uses:`,
// закрывает его.
func stepDeclaresToken(lines []string, at int) bool {
	base := indentOf(lines[at])
	for j := at + 1; j < len(lines); j++ {
		ln := lines[j]
		if strings.TrimSpace(ln) == "" {
			continue
		}
		if indentOf(ln) <= base {
			return false
		}
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, tokenInput+":") {
			return true
		}
	}
	return false
}

func indentOf(s string) int {
	n := 0
	for _, r := range s {
		if r != ' ' && r != '\t' {
			break
		}
		n++
	}
	return n
}

// TestToolSetupJudgeFiresAndStaysSilent — инъекция в обе стороны.
//
// Зелёный на дереве сам по себе не значит ничего: ровно так же гейт выглядел бы,
// не умей он краснеть. Каждый случай, который разбор ОБЯЗАН поймать, и каждый,
// который обязан пропустить, — настоящим содержимым рабочего процесса.
func TestToolSetupJudgeFiresAndStaysSilent(t *testing.T) {
	const withToken = `jobs:
  proto:
    steps:
      - uses: actions/checkout@v4
      - uses: bufbuild/buf-setup-action@v1
        with:
          version: 1.69.0
          github_token: ${{ github.token }}
      - name: buf lint
        run: buf lint
`
	const withoutToken = `jobs:
  proto:
    steps:
      - uses: actions/checkout@v4
      - uses: bufbuild/buf-setup-action@v1
        with:
          version: 1.69.0
      - name: buf lint
        run: buf lint
`
	// Токен есть, но у СОСЕДНЕГО шага. Форма выглядит исполненной, предмет —
	// нет: именно так выглядела бы «починка», сделанная не в том блоке.
	const tokenOnNeighbour = `jobs:
  proto:
    steps:
      - uses: bufbuild/buf-setup-action@v1
        with:
          version: 1.69.0
      - uses: some/other-action@v2
        with:
          github_token: ${{ github.token }}
`
	// Слово есть только в комментарии — разбор не должен считать это входом.
	const tokenInComment = `jobs:
  proto:
    steps:
      - uses: bufbuild/buf-setup-action@v1
        with:
          # github_token: раньше стоял здесь
          version: 1.69.0
`
	// Законный близнец другой формы: чужое действие без токена — не наш предмет.
	const foreignAction = `jobs:
  ui:
    steps:
      - uses: actions/setup-node@v4
        with:
          node-version: 20
`

	cases := []struct {
		name    string
		body    string
		wantHit bool
	}{
		{"молчит: токен объявлен в том же шаге", withToken, false},
		{"краснеет: токена нет", withoutToken, true},
		{"краснеет: токен у соседнего шага, не у этого", tokenOnNeighbour, true},
		{"краснеет: токен только в комментарии", tokenInComment, true},
		{"молчит: чужое действие без токена — не предмет", foreignAction, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings := judgeTokenNeedingSteps(scanWorkflowForAction("t.yaml", tc.body))
			if tc.wantHit && len(findings) == 0 {
				t.Fatalf("разбор смолчал там, где обязан краснеть")
			}
			if !tc.wantHit && len(findings) != 0 {
				t.Fatalf("разбор покраснел на законной конструкции: %v", findings)
			}
			if tc.wantHit && !strings.Contains(findings[0], "t.yaml:") {
				t.Fatalf("находка не называет координату: %q", findings[0])
			}
		})
	}
}
