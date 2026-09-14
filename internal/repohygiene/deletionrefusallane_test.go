// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene_test

// Отказ на СНЯТИЕ ресурса обязан нести машинный признак полосы.
//
// ПРЕДМЕТ. Клиент различает полосы отказа по `reason`-токену
// `google.rpc.ErrorInfo`, а не разбором прозы (`api-conventions.md` §By-lane
// code-split). У отказов на снятие признака не было вовсе: «ресурс защищён от
// удаления», «контейнер не пуст», «на лист ссылаются» и «зависимые есть, вид не
// назван» приезжали ОДНИМ кодом и различались только предложением — а действия
// вызывающего у них разные. Программа, которой надо отличить одно от другого,
// была вынуждена читать текст, то есть ломаться от любой его правки.
//
// ПОЧЕМУ ГЕЙТ, А НЕ ПАМЯТЬ. Признак ставится в ДВУХ местах на службу —
// у производителя (он знает, почему отказал) и в классификации (она знает
// домен), — и разойтись они обязаны молча: каждая половина по отдельности
// защитима, код собирается, а отсутствие признака видно только вызывающему и
// только на живом отказе.
//
// ЧТО ГЕЙТ ДЕРЖИТ СТРУКТУРНО (по узлам разбора, без разбора слов):
//   - у каждой из четырёх полос словаря есть производитель в прод-коде;
//   - служба, называющая полосу, зовёт и `refusal.Attach` — иначе признак
//     чеканится и теряется в классификации;
//   - `refusal.Wrap` обёртывает sentinel ПРЕДУСЛОВИЯ — иначе признак уедет с
//     чужим кодом (предел, названный в godoc `refusal.Attach`).
//
// ЧТО ОН ДЕРЖИТ ОБРАЗЦОМ ПРОЗЫ И НЕ ПРИТВОРЯЕТСЯ БОЛЬШИМ: перечень КАНДИДАТОВ
// в отказы на снятие. Отказ, написанный незнакомой формой, распознаватель не
// найдёт — и промолчит (testing.md §Гейт на класс, п.7). Поэтому перепись
// печатает объём осмотренного: расширение образца обязано менять число
// кандидатов, иначе расширение холостое.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/corelib/treecorpus"
	"github.com/PRO-Robotech/kacho/internal/repohygiene"
)

func TestEveryDeletionRefusalCarriesItsLane(t *testing.T) {
	t.Parallel()

	root := repoRootFor(t)

	goFiles, err := treecorpus.UnderWithSuffix(filepath.Join(root, "services"), ".go")
	require.NoError(t, err, "перечень исходников берётся у индекса дерева, а не обходом диска")
	pkgFiles, err := treecorpus.UnderWithSuffix(filepath.Join(root, "pkg"), ".go")
	require.NoError(t, err)
	goFiles = append(goFiles, pkgFiles...)

	// Перечень служб ВЫВОДИТСЯ из дерева, а не выписывается: служба, заведённая
	// после этой пробы, молча не проверялась бы.
	services := map[string]bool{}
	serviceOf := func(path string) string {
		p := filepath.ToSlash(path)
		i := strings.Index(p, "/services/")
		if i < 0 {
			return ""
		}
		seg := strings.Split(p[i+len("/services/"):], "/")
		if len(seg) == 0 {
			return ""
		}
		return seg[0]
	}
	for _, f := range goFiles {
		if svc := serviceOf(f); svc != "" {
			services[svc] = true
		}
	}
	require.NotEmpty(t, services,
		"служб в дереве не найдено — гейту нечего рассматривать, и его молчание "+
			"было бы неотличимо от согласия")

	shorten := func(path string) string {
		p := filepath.ToSlash(path)
		for _, anchor := range []string{"/services/", "/pkg/"} {
			if i := strings.Index(p, anchor); i >= 0 {
				return p[i+1:]
			}
		}
		return p
	}

	facts, parseErrs := repohygiene.ScanDeletionRefusalLanes(goFiles, serviceOf, shorten)
	require.Emptyf(t, parseErrs,
		"файл не разобрался — перепись объявила бы осмотренным то, чего не читала:\n%s",
		strings.Join(parseErrs, "\n"))

	var laned, unlaned int
	for _, s := range facts.Sites {
		if s.Laned {
			laned++
		} else {
			unlaned++
		}
	}

	// Объём осмотренного — отдельное утверждение: «ноль находок» обязано быть
	// отличимо от «ноль прочитанного». Печатаются ОБЕ величины: одно число
	// скрывает ровно тот случай, ради которого гейт заведён.
	t.Logf("осмотрено: служб %d, файлов Go разобрано %d, кандидатов в отказы на снятие %d "+
		"(с полосой %d, без полосы %d, записей ведомости исключений %d), "+
		"полос словаря с производителем %d",
		len(services), facts.Parsed, len(facts.Sites), laned, unlaned,
		len(repohygiene.DeletionRefusalExemptions), len(facts.LaneNamedBy))
	require.NotZero(t, facts.Parsed,
		"гейт не разобрал НИ ОДНОГО файла: предикат перестал находить предмет, "+
			"и «ноль находок» здесь означало бы «ноль прочитанного»")
	require.NotZero(t, len(facts.Sites),
		"образец не нашёл НИ ОДНОГО кандидата: распознаватель ослеп, и его молчание "+
			"неотличимо от дерева без отказов на снятие")

	serviceList := make([]string, 0, len(services))
	for svc := range services {
		serviceList = append(serviceList, svc)
	}

	findings, stale := repohygiene.DeletionRefusalFindings(facts, serviceList, repohygiene.DeletionRefusalExemptions)

	require.Emptyf(t, stale,
		"запись ведомости исключений потеряла предмет — послабление обязано истекать само:\n%s",
		strings.Join(stale, "\n"))
	require.Emptyf(t, findings,
		"отказ на снятие обязан нести машинный признак полосы — токеном в деталях ответа, "+
			"а не только прозой.\n%s",
		strings.Join(findings, "\n"))

	// ─── страница арендатора сверяется с производителями В ОБЕ СТОРОНЫ ───────
	//
	// Токен, которого клиент не может ПРОЧИТАТЬ в документации, ему не помогает:
	// признак есть, а узнать о нём неоткуда. Обратная сторона хуже: страница,
	// обещающая полосу без производителя, посылает клиента ловить отказ, которого
	// не бывает.
	pageOf := map[string]string{}
	for svc := range services {
		page := filepath.Join(root, "services", svc, "docs", "content", "api", "overview.mdx")
		body, rerr := os.ReadFile(page) // #nosec G304 -- путь собран из имени службы, выведенного из индекса git
		if rerr != nil {
			continue
		}
		pageOf[svc] = string(body)
	}
	require.NotEmpty(t, pageOf,
		"страниц арендатора не прочитано ни одной — «ноль находок» означало бы «ноль прочитанного»")

	docFindings := repohygiene.DeletionRefusalDocFindings(facts.LanesByService, pageOf)
	t.Logf("страниц арендатора осмотрено: %d, служб с производителями полос %d",
		len(pageOf), len(facts.LanesByService))
	require.Emptyf(t, docFindings,
		"страница арендатора обязана называть те полосы, что служба производит, и молчать о прочих.\n%s",
		strings.Join(docFindings, "\n"))
}
