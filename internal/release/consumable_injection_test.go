// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// consumable_injection_test.go — доказательство падучести гейта годности.
//
// # Зачем отдельная проба
//
// `TestExternalConsumerCanBuildTheModule` зелен на исправном дереве, и на
// исправном дереве зелен ТАКЖЕ гейт, потерявший способность краснеть. Отличить
// их нельзя ничем, кроме внесённого дефекта.
//
// # Почему дефект вносится в СИНТЕТИЧЕСКИЙ репозиторий
//
// Внести его в наше дерево значило бы сломать сборку всем остальным пробам:
// красное пришло бы от соседа, и новый гейт мог бы оказаться вакуумным, не
// показав этого ничем. Синтетический репозиторий изолирует предмет, а гоняется
// по нему ТОТ ЖЕ `packAndBuild`, что и по нашему.
//
// # Одно-фактность
//
// У каждого дефекта есть ЗАКОННЫЙ БЛИЗНЕЦ, отличающийся ровно одним названным
// фактом. Без близнеца красное ничего не доказывает: покраснеть мог сосед.
//
// # Три оси, и каждая — свой класс
//
//  1. файл не отслеживается git — у нас есть, у потребителя нет вовсе;
//  2. go.mod объявляет ОДИН путь модуля, а публикуется он под другим — ровно
//     то, что происходит при мажоре ≥2 без суффикса `/vN` (предпосылка П2
//     механизма публикации);
//  3. имена, различающиеся только регистром, — правила упаковки Go отвергают
//     ревизию ЦЕЛИКОМ, и сборка дерева об этом не говорит ничего.
package release

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// synthetic — описание синтетического репозитория. Поля меняются по ОДНОМУ:
// именно это делает красное доказательством, а не совпадением.
type synthetic struct {
	untrackLibrary  bool // не добавлять библиотечный файл в git
	wrongModulePath bool // объявить в go.mod путь, отличный от того, под которым публикуем
	caseCollision   bool // завести имя, отличающееся от соседнего только регистром
}

const syntheticPath = "example.com/synthetic"

// buildSynthetic — репозиторий из двух файлов: библиотека и её go.mod.
func buildSynthetic(t *testing.T, s synthetic) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(dir, "lib"), 0o755); err != nil {
		t.Fatalf("каталог не создан: %v", err)
	}

	declared := syntheticPath
	if s.wrongModulePath {
		declared = syntheticPath + "/v2"
	}
	write(t, filepath.Join(dir, "go.mod"), "module "+declared+"\n\ngo 1.21\n")
	write(t, filepath.Join(dir, "lib", "lib.go"), "package lib\n\nfunc Answer() int { return 42 }\n")
	if s.caseCollision {
		write(t, filepath.Join(dir, "lib", "Lib.go"), "package lib\n\nfunc Other() int { return 7 }\n")
	}

	env := cleanGitEnv()
	git := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(env,
			"GIT_AUTHOR_NAME=probe", "GIT_AUTHOR_EMAIL=probe@invalid",
			"GIT_COMMITTER_NAME=probe", "GIT_COMMITTER_EMAIL=probe@invalid")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "--quiet", "-b", "main")
	if s.untrackLibrary {
		// РОВНО ОДИН факт: библиотека остаётся в рабочем дереве и не попадает
		// в индекс. Локально модуль собирается, у потребителя пакета нет.
		git("add", "go.mod")
	} else {
		git("add", "-A")
	}
	git("commit", "--quiet", "-m", "synthetic")
	return dir
}

func packSynthetic(t *testing.T, s synthetic) packResult {
	t.Helper()
	return packAndBuild(t, packRequest{
		vcsRoot:    buildSynthetic(t, s),
		modulePath: syntheticPath,
		importPath: syntheticPath + "/lib",
		program: "package main\n\nimport lib \"" + syntheticPath +
			"/lib\"\n\nfunc main() { _ = lib.Answer() }\n",
	})
}

// TestConsumabilityGateStaysSilentOnALegitimateTwin — положительный контроль.
//
// Без него отрицания ниже зеленели бы на чём угодно: механизм, отвергающий
// всякий вход, краснеет на дефекте так же исправно, как годный.
func TestConsumabilityGateStaysSilentOnALegitimateTwin(t *testing.T) {
	res := packSynthetic(t, synthetic{})
	t.Logf("законный близнец: файлов в зипе %d, зип %d Б", res.filesInZip, res.zipBytes)
	if res.err != nil {
		t.Fatalf("законный близнец обязан собираться, а не собрался: %v\n%s", res.err, res.output)
	}
	if res.filesInZip == 0 {
		t.Fatalf("обход пуст: в зипе ноль файлов — контроль беспредметен")
	}
}

// TestConsumabilityGateRedOnAnUntrackedSource — ось 1.
func TestConsumabilityGateRedOnAnUntrackedSource(t *testing.T) {
	res := packSynthetic(t, synthetic{untrackLibrary: true})
	if res.err == nil {
		t.Fatalf("неотслеживаемый исходник обязан ронять гейт, а он смолчал\n%s", res.output)
	}
	if !strings.Contains(res.output, syntheticPath+"/lib") {
		t.Fatalf("находка не называет координату (ожидался пакет %s/lib):\n%s", syntheticPath, res.output)
	}
	t.Logf("ось 1 (файл не отслеживается): гейт краснеет и называет пакет %s/lib", syntheticPath)
}

// TestConsumabilityGateRedOnADeclaredPathMismatch — ось 2.
//
// go.mod ревизии объявляет `<путь>/v2`, а публикуется она под `<путь>`. Это не
// выдуманный случай: ровно так выглядит выпуск мажора ≥2, забывшего суффикс,
// и наоборот. У нас всё собирается, потребитель получает отказ «модуль
// объявляет свой путь как …».
func TestConsumabilityGateRedOnADeclaredPathMismatch(t *testing.T) {
	res := packSynthetic(t, synthetic{wrongModulePath: true})
	if res.err == nil {
		t.Fatalf("расхождение объявленного и публикуемого пути обязано ронять гейт, а он смолчал\n%s", res.output)
	}
	if !strings.Contains(res.output, syntheticPath) {
		t.Fatalf("находка не называет путь модуля:\n%s", res.output)
	}
	t.Logf("ось 2 (объявленный путь ≠ публикуемый): гейт краснеет — %v", firstLine(res.output))
}

// firstLine — первая содержательная строка вывода, для короткой переписи.
func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(l); t != "" && !strings.HasPrefix(t, "$ ") {
			return t
		}
	}
	return ""
}

// TestConsumabilityGateRedOnACaseCollision — ось 3.
//
// Дефект ломает УПАКОВКУ, а не сборку: синтетический модуль собирается у себя
// и не может быть опубликован вовсе. Ровно этот класс сборка дерева не видит.
func TestConsumabilityGateRedOnACaseCollision(t *testing.T) {
	res := packSynthetic(t, synthetic{caseCollision: true})
	if res.err == nil {
		t.Fatalf("имена, различающиеся регистром, обязаны ронять упаковку, а она прошла\n%s", res.output)
	}
	t.Logf("ось 3 (коллизия регистра): упаковка отвергнута — %v", res.err)
}
