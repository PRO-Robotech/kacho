// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: AGPL-3.0-or-later

package check_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Замер, объявленный в документе, обязан называть ревизию ХЕШЕМ.
//
// # ПОЧЕМУ ЭТО НЕ ОФОРМЛЕНИЕ
//
// «Замер на ревизии записи» — самоссылка: восстановить ревизию можно только
// раскопками по истории, а числа за это время расходятся молча. Наблюдалось на
// §19 этого же документа: записано 51/48, на названной ревизии — 53/50.
//
// # ЧЕМ ПРОВЕРЯЕТСЯ УСТАРЕВАНИЕ — ПРЕДКОМ, А НЕ РЕЗОЛВОМ
//
// Годный предикат — `git merge-base --is-ancestor <хеш> HEAD`. Проверка
// «резолвится ли ревизия» (`rev-parse --verify`, `cat-file -e`) для этого негодна:
// у рабочих копий одного клона ОБЩАЯ база объектов, поэтому она отвечает «да» на
// ревизию чужой линии, которой в этой истории нет.
//
// # ГРАНИЦА — НАЗВАНА, А НЕ УМОЛЧАНА
//
// Гейт судит ФОРМУ датировки, а не вхождение ревизии в историю: в неполном клоне
// (`--depth`) ответа о предке не существует, и гейт, краснеющий на глубине клона,
// отключат первым же ложным срабатыванием. Предком проверяет команда, названная в
// самом документе.
//
// # РАСПОЗНАВАТЕЛЬ ЗНАЕТ ВСЕ ЗАКОННЫЕ ФОРМЫ, А НЕ ОДНУ
//
// Первая редакция судила только жирный заголовок с начала строки — и прошла мимо
// ДВУХ живых самоссылок, записанных иначе (`* Замер на ревизии: …` и `Замер на
// ревизии измерения, …`). Форма, о которой распознаватель не знает, даёт не
// красное и не зелёное, а МОЛЧАНИЕ. Поэтому судится всякое утверждение о замере,
// начинающееся с прописной «З».
//
// Законный близнец — цитата негодной формы ПРОЗОЙ: разбор собственной ошибки
// пишет её строчной буквой внутри кавычек-ёлочек («замер на ревизии записи»).
// Проверка по подстроке без этого различения краснела бы на объяснении самой себя.
const iamDocsRelDir = "services/iam/docs"

// reMeasurementClaim — утверждение о замере: прописная «З», любая разметка вокруг.
// Строчная «замер» — цитата в прозе, и она намеренно вне охвата.
var reMeasurementClaim = regexp.MustCompile(`Замер на ревизии([^\n]{0,60})`)

// reRevisionHash — годная датировка: хеш в обратных кавычках, не короче семи.
var reRevisionHash = regexp.MustCompile("`[0-9a-f]{7,40}`")

type datingCensus struct {
	filesRead int
	headings  int
	byHash    int
}

// auditDating — чистое ядро: решает по текстам, а не по дереву.
func auditDating(docs map[string]string) ([]string, datingCensus) {
	var findings []string
	c := datingCensus{filesRead: len(docs)}

	for _, name := range sortedDocNames(docs) {
		for _, m := range reMeasurementClaim.FindAllStringSubmatch(docs[name], -1) {
			c.headings++
			token := strings.TrimSpace(m[1])
			if reRevisionHash.MatchString(token) {
				c.byHash++
				continue
			}
			findings = append(findings, fmt.Sprintf(
				"%s: замер датирован как %q — ревизия обязана называться ХЕШЕМ, "+
					"иначе числа расходятся молча, а восстановить их не на чем",
				name, token))
		}
	}
	return findings, c
}

func sortedDocNames(docs map[string]string) []string {
	out := make([]string, 0, len(docs))
	for k := range docs {
		out = append(out, k)
	}
	// порядок обхода не значим для вердикта, но значим для читаемости находок
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// readMarkdownTree читает все .md каталога рекурсивно.
func readMarkdownTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		raw, err := os.ReadFile(path) // #nosec G304 -- путь получен обходом собственного дерева
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out[rel] = string(raw)
		return nil
	})
	require.NoError(t, err)
	return out
}

// TestMeasurementsAreDatedByHash — несущее утверждение.
func TestMeasurementsAreDatedByHash(t *testing.T) {
	root := monorepoRoot(t)
	docs := readMarkdownTree(t, filepath.Join(root, iamDocsRelDir))

	findings, c := auditDating(docs)

	t.Logf("перепись: документов прочитано %d · заголовков замера %d · "+
		"датированы хешем %d · находок %d", c.filesRead, c.headings, c.byHash, len(findings))

	require.NotZerof(t, c.filesRead, "обход пуст — вердикт беспредметен (%s)", iamDocsRelDir)
	require.NotZerof(t, c.headings, "заголовков замера не прочитано ни одного — предмет гейта исчез")

	require.Emptyf(t, findings,
		"замер датирован самоссылкой — ревизия не восстанавливается, числа расходятся молча:\n%s",
		strings.Join(findings, "\n"))
}
