// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// gitcommandenv_injection_test.go — доказательство, что гейт вызова git способен
// упасть И способен смолчать.
//
// Инъекция в обе стороны обязательна: гейт, доказанный только красной стороной,
// ловит форму, а не существо, и первый же ложный срабат его отключит. Поэтому у
// каждого запрещённого силуэта здесь стоит ЗАКОННЫЙ близнец той же формы.
//
// Инъекция настоящим входом из дерева выполнена отдельно и записана числами: на
// ревизии, где помощник только появился, гейт назвал **39** прямых вызовов в 22
// файлах (осмотрено 3766 файлов `.go`), после перевода их на помощник —
// **4** возврата окружения, после правки и их — **0**. Обе стороны получены
// одним и тем же гейтом на одном и том же дереве.
package repohygiene

import (
	"go/parser"
	"go/token"
	"testing"
)

func scanSource(t *testing.T, src string) (calls int, leaks int) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "synthetic.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("разбор синтетического исходника: %v", err)
	}
	c, l := scanGitUsage(fset, file, "/synthetic")
	return len(c), len(l)
}

func TestGitCommandGateCutsBothWays(t *testing.T) {
	const preamble = `package p

import (
	"context"
	"os"
	"os/exec"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

var ctx = context.Background()
var dir = "/tmp/x"

`
	cases := []struct {
		name      string
		body      string
		wantCalls int
		wantLeaks int
		why       string
	}{
		{
			name:      "ЛОВИТСЯ: прямой вызов",
			body:      `func f() { _ = exec.Command("git", "init", "-q") }`,
			wantCalls: 1,
			why:       "ровно та форма, что схлопывала индекс рабочей копии",
		},
		{
			name:      "ЛОВИТСЯ: вызов со сроком",
			body:      `func f() { _ = exec.CommandContext(ctx, "git", "status") }`,
			wantCalls: 1,
			why:       "срок вызова окружения не меняет; argv[0] стоит вторым",
		},
		{
			name: "ЛОВИТСЯ: имя двоичного файла вынесено в константу",
			body: `const gitBin = "git"

func f() { _ = exec.Command(gitBin, "status") }`,
			wantCalls: 1,
			why:       "иначе запрет обходится одной строкой объявления",
		},
		{
			name:      "ЛОВИТСЯ: путь до двоичного файла",
			body:      `func f() { _ = exec.Command("/usr/bin/git", "status") }`,
			wantCalls: 1,
			why:       "предикат по роли: имя файла, а не строка целиком",
		},
		{
			name:      "МОЛЧИТ: законный близнец — тот же вызов через помощника",
			body:      `func f() { _ = gitenv.Command(dir, "init", "-q") }`,
			wantCalls: 0,
			why:       "без этой половины гейт был бы доказан только красной стороной",
		},
		{
			name:      "МОЛЧИТ: соседняя команда",
			body:      `func f() { _ = exec.Command("go", "build", "./...") }`,
			wantCalls: 0,
			why:       "запрет про git, а не про os/exec вообще",
		},
		{
			name:      "МОЛЧИТ: имя, СОДЕРЖАЩЕЕ git",
			body:      `func f() { _ = exec.Command("gitleaks", "detect") }`,
			wantCalls: 0,
			why:       "подстрока «git» — не признак; gitleaks чужим индексом не распоряжается",
		},
		{
			name: "МОЛЧИТ: запрещённая форма в комментарии и в литерале",
			body: `// Здесь стоит exec.Command("git", "init") — и это объяснение защиты.
func f() { _ = "exec.Command(\"git\", \"add\", \"-A\")" }`,
			wantCalls: 0,
			why:       "текстовый поиск принял бы объяснение защиты за её нарушение",
		},
		{
			name: "ЛОВИТСЯ: окружение возвращено поверх помощника",
			body: `func f() {
	cmd := gitenv.Command(dir, "commit", "-m", "x")
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t")
	_ = cmd
}`,
			wantLeaks: 1,
			why:       "перевод на помощник без этой правки оставил бы дефект на месте",
		},
		{
			name: "МОЛЧИТ: окружение ДОПИСАНО к снятому",
			body: `func f() {
	cmd := gitenv.Command(dir, "commit", "-m", "x")
	cmd.Env = append(cmd.Env, "GIT_AUTHOR_NAME=t")
	_ = cmd
}`,
			wantLeaks: 0,
			why:       "законный способ задать подпись фикстуры обязан проходить",
		},
		{
			name: "МОЛЧИТ: os.Environ() у команды, которая git не является",
			body: `func f() {
	other := exec.Command("go", "test", "./...")
	other.Env = os.Environ()
	_ = other
}`,
			wantLeaks: 0,
			why:       "область запрета — команды помощника; иначе первый же ложный срабат снимет гейт",
		},
		{
			name: "МОЛЧИТ: os.Environ() у чужой команды рядом с командой помощника",
			body: `func f() {
	cmd := gitenv.Command(dir, "status")
	other := exec.Command("go", "env")
	other.Env = os.Environ()
	_, _ = cmd, other
}`,
			wantLeaks: 0,
			why:       "признак — переменная ОТ помощника, а не соседство в файле",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls, leaks := scanSource(t, preamble+tc.body)
			if calls != tc.wantCalls {
				t.Errorf("прямых вызовов: получено %d, ожидалось %d — %s", calls, tc.wantCalls, tc.why)
			}
			if leaks != tc.wantLeaks {
				t.Errorf("возвратов окружения: получено %d, ожидалось %d — %s", leaks, tc.wantLeaks, tc.why)
			}
		})
	}
}

// TestGitCommandGateNamesTheCoordinate — падение обязано называть место.
// Гейт, который знает о находке и не говорит где, заставляет искать её руками, и
// его снимут первым.
func TestGitCommandGateNamesTheCoordinate(t *testing.T) {
	const src = `package p

import "os/exec"

func f() { _ = exec.Command("git", "init") }
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "/synthetic/a.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	calls, _ := scanGitUsage(fset, file, "/synthetic")
	if len(calls) != 1 {
		t.Fatalf("находок %d, ожидалась одна", len(calls))
	}
	if calls[0].pos != "a.go:5:16" {
		t.Errorf("координата %q — ожидалось «a.go:5:16» (путь от корня дерева, "+
			"строка и колонка вызова)", calls[0].pos)
	}
}
