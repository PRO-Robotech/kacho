// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// newmanproofclaim_test.go — утверждение о ДОКАЗАННОСТИ обязано называть
// координату, которая резолвится (`PRO-Robotech/kacho#1277`).
//
// # Предмет
//
// Комментарий «Доказательство в обе стороны — `<файл>`» стоял в генераторах
// ШЕСТИ наборов; файл существовал у ОДНОГО. Пять координат пережили свой предмет.
//
// Это опаснее обычной устаревшей ссылки, и опаснее ровно тем, ЧТО именно
// утверждается. Ссылка на переехавший модуль сообщает «смотри туда» — читатель
// не находит и ищет дальше. Здесь утверждается ДОКАЗАННОСТЬ: читатель, правящий
// предикат в одном из пяти, видит обещание, что его правка проверена в обе
// стороны, — и не проверяет сам. Ложное обещание снимает работу, а не добавляет.
//
// # Что именно стережётся
//
// НЕ всякая координата в обратных кавычках: их в корпусе 229, и среди них
// законно живут ОБРАЗЦЫ (`cases/*.py`), голые имена без каталога и обрывки
// регулярок — требовать от них резолва значило бы завести перечень прощённых, а
// каждая запись в нём есть место, куда неправда вносится незамеченной.
//
// Стережётся УЗКИЙ предмет: координата, стоящая ПРИ УТВЕРЖДЕНИИ О
// ДОКАЗАТЕЛЬСТВЕ. Замер по дереву на день заведения: таких координат 18, из них
// не резолвились 5 — ровно предмет #1277, и ни одного прощённого не
// потребовалось.
//
// # Как резолвится
//
// Три основания, и все три — живая практика этого дерева, а не догадка:
// каталог самого файла · корень НАБОРА (`ci.yaml` зовёт `scripts/…` именно
// оттуда, задавая `working-directory`) · корень репозитория. Координата,
// не резолвящаяся ни от одного, — находка.
//
// # Предикат ищет НА ДВУХ ЯЗЫКАХ
//
// Корпус двуязычен: координаты и имена по-английски, разборы по-русски
// (`testing.md` §«Предикат по ДВУЯЗЫЧНОМУ корпусу»). Предикат, ищущий слово
// «доказательство» только по-русски, недобрал бы молча — и недобрал бы именно
// там, где предмет объясняли словами.
package repohygiene_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

var (
	// Файлы корпуса сквозных проб: генераторы, кейсы и их оснастка.
	newmanScriptRe = regexp.MustCompile(`^(?:services/[^/]+|gateway)/tests/newman/.*\.py$`)

	// Утверждение о доказанности — на обоих языках корпуса.
	proofClaimRe = regexp.MustCompile(
		`(?i)доказательств|доказано|доказана|доказывает|\bproof\b|\bproven\b`)

	// Координата в обратных кавычках. Образцы (`*`), составные фразы с пробелом и
	// всё, что не похоже на исполнимый артефакт, отсеиваются ниже.
	backtickedRe = regexp.MustCompile("`([^`\n]+)`")

	proofArtifactRe = regexp.MustCompile(`\.(py|sh|go)$`)
)

// proofClaimCoordinate — координата, годная в предмет проверки.
//
// Отсеиваются ОБРАЗЦЫ и обрывки: они не координаты, и требовать от них резолва
// значило бы мерить форму записи, а не факт существования доказательства.
func proofClaimCoordinate(tok string) bool {
	tok = strings.TrimSpace(tok)
	if tok == "" || strings.ContainsAny(tok, " *?()[]{}\\") {
		return false
	}
	// Голое имя без каталога координатой не является: оно не говорит, где искать,
	// и резолвиться от трёх оснований не обязано.
	if !strings.Contains(tok, "/") {
		return false
	}
	return proofArtifactRe.MatchString(tok)
}

type proofClaimCensus struct {
	Files       int
	ClaimLines  int
	Coordinates int
}

type proofClaimFinding struct {
	File string
	Line int
	Ref  string
	Text string
}

// auditProofClaims — ЧИСТОЕ ЯДРО: та же функция, что гоняет проба инъекции.
// Проба, повторяющая логику своей копией, доказывала бы свойство копии.
//
// `sources` — путь в репозитории -> содержимое. `exists` — резолвер (в дереве
// читает диск, в инъекции подставляется), принимающий путь ОТ КОРНЯ репозитория.
func auditProofClaims(
	sources map[string]string,
	exists func(relFromRoot string) bool,
) (proofClaimCensus, []proofClaimFinding) {
	var census proofClaimCensus
	var findings []proofClaimFinding

	for _, file := range sortedKeys(sources) {
		census.Files++
		lines := strings.Split(sources[file], "\n")
		suiteRoot := file
		if i := strings.Index(file, "/tests/newman/"); i >= 0 {
			suiteRoot = file[:i] + "/tests/newman"
		}
		fileDir := filepath.Dir(file)

		for i, ln := range lines {
			if !proofClaimRe.MatchString(ln) {
				continue
			}
			census.ClaimLines++
			// Комментарий переносится, поэтому координата ищется на этой строке И
			// на следующей: привязка только к своей строке недобрала бы ровно там,
			// где утверждение длинное — то есть где оно и объясняется.
			window := ln
			if i+1 < len(lines) {
				window += " " + lines[i+1]
			}
			for _, m := range backtickedRe.FindAllStringSubmatch(window, -1) {
				ref := strings.TrimSpace(m[1])
				if !proofClaimCoordinate(ref) {
					continue
				}
				census.Coordinates++
				if exists(ref) ||
					exists(filepath.Join(fileDir, ref)) ||
					exists(filepath.Join(suiteRoot, ref)) {
					continue
				}
				findings = append(findings, proofClaimFinding{
					File: file, Line: i + 1, Ref: ref, Text: strings.TrimSpace(ln),
				})
			}
		}
	}
	return census, findings
}

func newmanScriptSources(t *testing.T, root string) map[string]string {
	t.Helper()

	out, err := gitenv.Command(root, "ls-files").Output()
	require.NoError(t, err,
		"git ls-files: без переписи «ноль находок» неотличимо от «ноль прочитанного»")

	sources := map[string]string{}
	for _, rel := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if !newmanScriptRe.MatchString(rel) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь из индекса git
		require.NoErrorf(t, err, "%s: состав корпуса неизвестен", rel)
		sources[rel] = string(b)
	}
	return sources
}

// TestNewmanProofClaimsNameAProofThatExists — гейт класса.
func TestNewmanProofClaimsNameAProofThatExists(t *testing.T) {
	t.Parallel()

	root := repoRootFor(t)
	sources := newmanScriptSources(t, root)
	require.NotEmpty(t, sources,
		"скриптов корпуса сквозных проб не найдено — гейт беспредметен, и его молчание "+
			"неотличимо от согласия")

	exists := func(rel string) bool {
		_, err := os.Stat(filepath.Join(root, filepath.Clean(rel)))
		return err == nil
	}
	census, findings := auditProofClaims(sources, exists)

	// Предпосылка гейта: если ни одного утверждения о доказательстве с координатой
	// не распознано, разбор сломан либо корпус переехал — и «находок 0» означало бы
	// «не смотрели».
	require.Positivef(t, census.Coordinates,
		"на %d файлах (%d строк с утверждением о доказательстве) не распознано НИ ОДНОЙ "+
			"координаты — разбор сломан. Всякая мёртвая ссылка была бы объявлена чистотой",
		census.Files, census.ClaimLines)

	var lines []string
	for _, f := range findings {
		lines = append(lines, fmt.Sprintf("%s:%d — обещано доказательство `%s`, "+
			"которого нет ни рядом с файлом, ни в корне набора, ни в корне репозитория\n    %s",
			f.File, f.Line, f.Ref, f.Text))
	}

	// Пустой перечень — ЦЕЛЬ, а не поломка.
	t.Logf("перепись: файлов корпуса прочитано %d, строк с утверждением о доказательстве %d, "+
		"координат при них %d; находок %d",
		census.Files, census.ClaimLines, census.Coordinates, len(findings))

	require.Emptyf(t, findings,
		"утверждение о ДОКАЗАННОСТИ называет координату, которой нет. Это не «устаревшая "+
			"ссылка»: читатель, правящий предмет, видит обещание, что его правка проверена "+
			"в обе стороны, и не проверяет сам.\nИсходов два: завести доказательство по "+
			"названной координате ЛИБО сказать правду — назвать то, что существует, и прямо "+
			"оговорить, что этот предмет им НЕ покрыт.\n%s",
		strings.Join(lines, "\n"))
}
