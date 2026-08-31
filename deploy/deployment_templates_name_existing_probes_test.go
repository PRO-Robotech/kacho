// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// deployment_templates_name_existing_probes_test.go — имя пробы, названное
// шаблонами и профилями развёртывания, обязано РЕЗОЛВИТЬСЯ в дереве.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ, И ЧЕМ ОН ХУЖЕ ПРОТУХШЕЙ ГРАНИЦЫ
//
// Страж величины обратного вызова называл держателя одного из своих утверждений —
// гейт, которого в дереве НЕ БЫЛО (замер: файлов с этим именем 0 при контроле в
// 48 других имён, резолвящихся все). Граница, объявленная шире факта, вводит
// читателя в заблуждение. Ссылка на несуществующий гейт делает больше: она
// ОБЪЯВЛЯЕТ СВОЙСТВО УДЕРЖАННЫМ. Следующий, увидев имя пробы рядом с
// ограничением, заключит, что нарушение упадёт на прогоне, — и не станет
// проверять. Свойство при этом не держит ничто.
//
// Это тот же класс, что «эталон — не проверка» (`testing.md` §«Гейт на класс»,
// п.6): правило, называющее держателя, обязано называть СУЩЕСТВУЮЩИЙ артефакт.
// Проверяемый признак для автора такого текста — назови файл и убедись, что он
// резолвится; здесь этот признак становится машинным.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ИМЕННО УТВЕРЖДАЕТСЯ
//
// Каждое имя вида `…_test.go` либо `…-test.sh`, встреченное в шаблонах, профилях
// и рецептах каталога развёртывания, резолвится в отслеживаемый файл дерева.
// Перепись печатает «имён названо · резолвится · нет», падает на пустом обходе.
//
// ─────────────────────────────────────────────────────────────────────────────
// ГРАНИЦА ПРЕДМЕТА
//
//   - Проверяется СУЩЕСТВОВАНИЕ файла, а не то, что он утверждает названное.
//     Второе машинно не решается: держатель может существовать и проверять
//     другое. Существование — необходимое условие, и оно единственное, которое
//     можно потребовать от текста.
//   - Имя разрешается тремя способами по очереди: как путь от корня репозитория,
//     как путь от каталога развёртывания, как имя файла где угодно в дереве.
//     Последнее намеренно: половина упоминаний в дереве — короткие имена
//     соседних проб, и требовать от прозы полного пути значило бы краснеть на
//     исправном тексте.
package deploy_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// probeName — имя пробы. Один символ перед суффиксом обязателен: иначе под
// выражение попадает голый хвост `_test.go` из прозы о самих пробах.
var probeName = regexp.MustCompile(`[A-Za-z0-9][A-Za-z0-9_./-]*(?:_test\.go|-test\.sh)`)

// deploymentTexts — файлы каталога развёртывания, в которых имя пробы вообще
// может быть названо: шаблоны, профили, рецепты. Перечень выводится обходом.
func deploymentTexts(t *testing.T) []string {
	t.Helper()
	var out []string
	err := filepath.Walk("helm", func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		switch filepath.Ext(p) {
		case ".yaml", ".yml", ".tpl", ".sh":
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("обход каталога чартов: %v", err)
	}
	sort.Strings(out)
	return out
}

// unresolvedProbeNames — ядро. Чистая функция над фактами: имена и перечень
// отслеживаемых файлов, — чтобы самопроверка подавала ей синтетический вход.
func unresolvedProbeNames(named map[string][]string, tracked []string) []string {
	byPath := map[string]bool{}
	byBase := map[string]bool{}
	for _, f := range tracked {
		byPath[f] = true
		byBase[filepath.Base(f)] = true
	}
	var bad []string
	names := make([]string, 0, len(named))
	for n := range named {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		switch {
		case byPath[n], byPath["deploy/"+n], byBase[filepath.Base(n)]:
			continue
		}
		bad = append(bad, n+" ← "+strings.Join(named[n], ", "))
	}
	return bad
}

func TestDeploymentTemplatesNameExistingProbes(t *testing.T) {
	texts := deploymentTexts(t)
	if len(texts) == 0 {
		t.Fatalf("файлов развёртывания не найдено ни одного — обход пуст, и это отказ, а не успех")
	}

	named := map[string][]string{}
	for _, p := range texts {
		b, err := os.ReadFile(p) // #nosec G304 -- путь получен обходом собственного дерева
		if err != nil {
			t.Fatalf("чтение %s: %v", p, err)
		}
		for i, ln := range strings.Split(string(b), "\n") {
			for _, m := range probeName.FindAllString(ln, -1) {
				coord := fmt.Sprintf("%s:%d", p, i+1)
				named[m] = append(named[m], coord)
			}
		}
	}

	tracked := trackedFiles(t) // весь перечень дерева: имя пробы резолвится где угодно
	bad := unresolvedProbeNames(named, tracked)

	t.Logf("осмотрено: файлов развёртывания %d, отслеживаемых файлов дерева %d; "+
		"имён проб названо %d, резолвится %d, нет %d",
		len(texts), len(tracked), len(named), len(named)-len(bad), len(bad))

	if len(named) == 0 {
		t.Fatalf("ни одного имени пробы не найдено — обход слеп, а не дерево чисто: "+
			"«ноль находок» здесь неотличимо от «ноль прочитанного» (осмотрено файлов %d)",
			len(texts))
	}
	for _, b := range bad {
		t.Errorf("имя пробы не резолвится в дереве: %s.\n"+
			"    Текст развёртывания, называющий держателя, ОБЪЯВЛЯЕТ свойство удержанным. "+
			"Имя, которого в дереве нет, объявляет удержанным то, что не держит ничто.", b)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// САМОПРОВЕРКА — инъекция в обе стороны на синтетическом входе.

func TestUnresolvedProbeNames_SelfTest(t *testing.T) {
	tracked := []string{
		"deploy/сосед_test.go",
		"deploy/tests/helm/сосед-test.sh",
	}

	// (0) КОНТРОЛЬ: имена, резолвящиеся тремя способами, — молчание.
	ok := map[string][]string{
		"deploy/сосед_test.go":            {"страж.yaml:36"}, // путь от корня
		"сосед_test.go":                   {"страж.yaml:37"}, // короткое имя
		"deploy/tests/helm/сосед-test.sh": {"страж.yaml:38"}, // путь от корня, оболочка
		"tests/helm/сосед-test.sh":        {"страж.yaml:39"}, // путь от каталога развёртывания
	}
	if got := unresolvedProbeNames(ok, tracked); len(got) != 0 {
		t.Errorf("(0) резолвящиеся имена обязаны молчать: %v", got)
	}

	// (A) ИНЪЕКЦИЯ: страж называет пробу, которой в дереве нет — ровно исходный
	//     дефект.
	bad := map[string][]string{
		"deploy/сосед_test.go":      {"страж.yaml:36"},
		"deploy/выдуманный_test.go": {"страж.yaml:40"},
	}
	got := unresolvedProbeNames(bad, tracked)
	switch {
	case len(got) != 1:
		t.Errorf("(A) выдуманное имя пробы ПРОПУЩЕНО либо задето лишнее: %v", got)
	case !strings.Contains(got[0], "выдуманный_test.go"):
		t.Errorf("(A) находка не называет имя: %s", got[0])
	case !strings.Contains(got[0], "страж.yaml:40"):
		t.Errorf("(A) находка не называет координату упоминания: %s", got[0])
	}

	// (B) КОНТРОЛЬ ТОЙ ЖЕ ФОРМЫ: имя, отличающееся от выдуманного только тем,
	//     что файл существует, — молчание. Без этого (A) зеленело бы на любом
	//     имени, и проверка ловила бы форму, а не существо.
	if got := unresolvedProbeNames(map[string][]string{
		"deploy/сосед_test.go": {"страж.yaml:40"},
	}, tracked); len(got) != 0 {
		t.Errorf("(B) существующее имя обязано молчать: %v", got)
	}

	// (C) ИНЪЕКЦИЯ: файл был и снят — то же имя становится находкой. Проверка
	//     истекает вместе с предметом, а не только ловит опечатку.
	if got := unresolvedProbeNames(map[string][]string{
		"deploy/сосед_test.go": {"страж.yaml:36"},
	}, []string{"deploy/другой_test.go"}); len(got) != 1 {
		t.Errorf("(C) снятая проба ПРОПУЩЕНА: %v", got)
	}
}

// TestProbeNamePredicate_RecognisesTheRealTree — выражение узнаёт обе формы имён
// НАСТОЯЩЕГО дерева и не подбирает голый хвост из прозы о самих пробах.
func TestProbeNamePredicate_RecognisesTheRealTree(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"держит deploy/posture_parity_test.go — читает объявления", "deploy/posture_parity_test.go"},
		{"выполнимость держит deploy/tests/helm/prerequisite-secrets-test.sh;", "deploy/tests/helm/prerequisite-secrets-test.sh"},
		{"проба соседа iam_lane_service_aud_test.go рядом", "iam_lane_service_aud_test.go"},
	}
	for _, c := range cases {
		got := probeName.FindString(c.in)
		if got != c.want {
			t.Errorf("выражение прочло %q вместо %q в %q", got, c.want, c.in)
		}
	}
	// Голый хвост именем не является: иначе проза о самих пробах становилась бы
	// находкой, и проверку сняли бы первой.
	if got := probeName.FindString("файлы вида _test.go в этом каталоге"); got != "" {
		t.Errorf("голый хвост принят за имя: %q", got)
	}
}
