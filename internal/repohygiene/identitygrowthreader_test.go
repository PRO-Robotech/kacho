// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identitygrowthreader_test.go — гейт: у величины роста числа личностей есть
// ЧИТАТЕЛЬ, и он назван.
//
// ЗАЧЕМ ОН СУЩЕСТВУЕТ. Величина, которую никто не смотрит, наблюдаемостью не
// является: она печатается на витрине, ничего не утверждает и создаёт
// уверенность, которой нет. Задача #619 просила именно наблюдаемость — «число
// личностей и скорость их появления видны, превышение порога порождает
// оповещение», — и ряд без правила закрывал бы её формой, а не существом.
//
// ЧТО ЗДЕСЬ СЧИТАЕТСЯ ЧИТАТЕЛЕМ. Только выражение правила оповещения (`expr:`).
// Упоминание имени ряда в пояснении к тревоге или в таблице метрик читателем НЕ
// является: пояснение не срабатывает. Это различие несущее — оба текста лежат в
// одном файле, и гейт, ищущий имя по всему документу, зеленел бы на ряде, о
// котором лишь написано.
//
// ГРАНИЦА ГЕЙТА НАЗВАНА. Он судит ОДНО семейство — то, которое заведено #619, —
// и выводит его состав из файла коллектора, а не из выписанного перечня. На
// прочие ряды iam он не распространяется, и это не оплошность: у части из них
// правил сегодня нет, и заведение им читателей — отдельная работа с отдельным
// предметом, а не побочный эффект этой. Расширение гейта на всё семейство
// метрик службы обязано прийти вместе с этими правилами, иначе оно потребует
// перечня исключений — то есть заведёт ровно ту форму, в которой послабление
// переживает свой предмет.
package repohygiene

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const (
	// identityGrowthCollectorFile — где объявлено семейство величин.
	identityGrowthCollectorFile = "services/iam/internal/observability/metrics/identity_growth_collector.go"
	// identityGrowthReadersFile — где живут правила оповещений службы.
	identityGrowthReadersFile = "services/iam/docs/engineering/components/32-observability.md"
)

var (
	// имя ряда в объявлении константы Go
	identityGrowthMetricNameRe = regexp.MustCompile(`"(kacho_iam_[a-z0-9_]+)"`)
	// строчный комментарий Go
	identityGrowthGoCommentRe = regexp.MustCompile(`(?m)^\s*//.*$`)
)

// TestIdentityGrowthMetricsHaveANamedReader — каждый ряд семейства назван хотя
// бы одним выражением правила оповещения.
func TestIdentityGrowthMetricsHaveANamedReader(t *testing.T) {
	root := repoRoot(t)

	declared := readIdentityGrowthMetricNames(t, root)
	if len(declared) == 0 {
		t.Fatalf("предпосылка гейта не выполнена: в %s не найдено ни одного имени ряда — "+
			"форма объявления изменилась, и гейт судит пустоту", identityGrowthCollectorFile)
	}

	docBytes, err := os.ReadFile(filepath.Join(root, identityGrowthReadersFile))
	if err != nil {
		t.Fatalf("документ наблюдаемости не прочитан (%s): %v", identityGrowthReadersFile, err)
	}
	exprs := alertExpressionsIn(string(docBytes))
	if len(exprs) == 0 {
		t.Fatalf("предпосылка гейта не выполнена: в %s не найдено ни одного выражения "+
			"правила — разбор перестал их видеть, и гейт судит пустоту",
			identityGrowthReadersFile)
	}

	t.Logf("перепись: рядов объявлено %d; выражений правил прочитано %d",
		len(declared), len(exprs))

	joined := strings.Join(exprs, "\n")
	var findings []string
	for _, metric := range declared {
		if !strings.Contains(joined, metric) {
			findings = append(findings, "ряд «"+metric+"» объявлен, но его не читает ни одно "+
				"правило оповещения: величина печатается на витрине, ничего не утверждает "+
				"и создаёт уверенность, которой нет")
		}
	}

	sort.Strings(findings)
	if len(findings) > 0 {
		t.Fatalf("у величины роста числа личностей нет читателя (%d):\n  %s",
			len(findings), strings.Join(findings, "\n  "))
	}
}

// TestIdentityGrowthReaderGate_CanFailAndJudgesOnlyExecutableText — доказательство
// того, что разбор способен упасть и что законная форма его не тревожит.
//
// БЛИЗНЕЦ ПОДБИРАЕТСЯ ПОД КЛАСС, А НЕ ПОД РЕАЛИЗАЦИЮ. Прежняя редакция ставила
// рядом комментарий, занимающий строку целиком, — то есть ровно ту форму,
// которую снятие и умело снимать, — и потому подтверждала только уже
// написанное. Хвостовая форма (`… > 100  # снято правило про <ряд>`) проходила
// мимо гейта в ОБЕИХ формах выражения. Поэтому близнецы ниже перечисляют класс:
// где комментарий, а где `#`, комментария не открывающий, — и по каждой оси
// утверждается ОБЕ стороны.
func TestIdentityGrowthReaderGate_CanFailAndJudgesOnlyExecutableText(t *testing.T) {
	// Инъекция: документ, где ряд назван ТОЛЬКО в пояснении. Пояснение не
	// срабатывает, поэтому читателем не является.
	const proseOnly = "```yaml\n" +
		"- alert: SomethingElse\n" +
		"  expr: rate(kaname_authz_check_decisions_total[5m]) > 1\n" +
		"  annotations:\n" +
		"    summary: \"смотреть также kaname_identities_total\"\n" +
		"```\n"
	got := strings.Join(alertExpressionsIn(proseOnly), "\n")
	if strings.Contains(got, "kaname_identities_total") {
		t.Fatalf("имя ряда из пояснения принято за читателя: %q", got)
	}
	// Законный близнец 1: ряд, действительно стоящий в выражении, читателем
	// является. Без этой половины предыдущее утверждение прошло бы на разборе,
	// не находящем вообще ничего.
	if !strings.Contains(got, "kaname_authz_check_decisions_total") {
		t.Fatalf("ряд из выражения правила не найден: %q", got)
	}

	// Законный близнец 2: многострочное выражение. Правила службы пишут его
	// через `|`, и разбор, читающий одну строку, потерял бы каждого такого
	// читателя — то есть объявил бы находкой исправное состояние.
	const multiline = "```yaml\n" +
		"- alert: Multi\n" +
		"  expr: |\n" +
		"    sum(rate(kaname_identities_total[1h]))\n" +
		"      / sum(rate(kaname_identity_ledger_samples_total[1h])) > 0.5\n" +
		"  for: 10m\n" +
		"```\n"
	multi := strings.Join(alertExpressionsIn(multiline), "\n")
	for _, want := range []string{"kaname_identities_total", "kaname_identity_ledger_samples_total"} {
		if !strings.Contains(multi, want) {
			t.Fatalf("многострочное выражение прочитано не целиком: %q не найден в %q", want, multi)
		}
	}
	if strings.Contains(multi, "for: 10m") {
		t.Fatalf("разбор выражения перешагнул границу правила и забрал соседние поля: %q", multi)
	}

	// Законный близнец 3: комментарий ВНУТРИ выражения. Он объясняет отбор и
	// называет ряды; принять его за выражение значило бы зеленеть на правиле,
	// которое ничего не считает.
	const commented = "```yaml\n" +
		"- alert: Commented\n" +
		"  expr: |\n" +
		"    # раньше здесь стоял kaname_identities_total\n" +
		"    rate(kaname_lro_reconcile_runs_total[5m]) > 0\n" +
		"```\n"
	com := strings.Join(alertExpressionsIn(commented), "\n")
	if strings.Contains(com, "kaname_identities_total") {
		t.Fatalf("ряд из комментария внутри выражения принят за читателя: %q", com)
	}
	if !strings.Contains(com, "kaname_lro_reconcile_runs_total") {
		t.Fatalf("исполняемая часть выражения потеряна вместе с комментарием: %q", com)
	}

	// Законный близнец 4: ХВОСТОВОЙ комментарий, ОДНОСТРОЧНАЯ форма. Это и есть
	// подмена, которой снимают правило: выражение переписано на другой ряд, а
	// снятый оставлен памяткой в конце строки. Пока памятка засчитывалась
	// читателем, гейт зеленел при отсутствующем читателе.
	const tailInline = "```yaml\n" +
		"- alert: TailInline\n" +
		"  expr: increase(kaname_lro_inflight[1h]) > 100  # снято правило про kaname_identities_total\n" +
		"```\n"
	tail := strings.Join(alertExpressionsIn(tailInline), "\n")
	if strings.Contains(tail, "kaname_identities_total") {
		t.Fatalf("ряд из ХВОСТОВОГО комментария принят за читателя: %q", tail)
	}
	if !strings.Contains(tail, "kaname_lro_inflight") {
		t.Fatalf("исполняемая часть выражения снята вместе с хвостовым комментарием: %q", tail)
	}

	// Законный близнец 5: тот же комментарий в БЛОЧНОЙ форме. Формы правила
	// пишутся обе, и починка одной оставила бы вторую дырой.
	const tailBlock = "```yaml\n" +
		"- alert: TailBlock\n" +
		"  expr: |\n" +
		"    increase(kaname_lro_inflight[1h]) > 100  # снято правило про kaname_identities_total\n" +
		"```\n"
	tailB := strings.Join(alertExpressionsIn(tailBlock), "\n")
	if strings.Contains(tailB, "kaname_identities_total") {
		t.Fatalf("ряд из хвостового комментария БЛОЧНОЙ формы принят за читателя: %q", tailB)
	}
	if !strings.Contains(tailB, "kaname_lro_inflight") {
		t.Fatalf("исполняемая часть блочного выражения снята вместе с комментарием: %q", tailB)
	}

	// Законный близнец 6: `#` ВНУТРИ строкового литерала комментария не
	// открывает. Отрезать по нему значило бы вырезать половину исполняемого
	// выражения — то есть объявить находкой правило, которое читателем является.
	// Три вида кавычек: двойные и одинарные (YAML и PromQL) и обратные — сырая
	// строка PromQL.
	for _, quoted := range []struct {
		name string
		doc  string
	}{
		{"двойные кавычки", "```yaml\n- alert: Q1\n  expr: rate(kaname_identities_total{path=\"/a #b\"}[5m]) > 0\n```\n"},
		{"одинарные кавычки", "```yaml\n- alert: Q2\n  expr: rate(kaname_identities_total{path='/a #b'}[5m]) > 0\n```\n"},
		{"сырая строка PromQL", "```yaml\n- alert: Q3\n  expr: rate(kaname_identities_total{path=`/a #b`}[5m]) > 0\n```\n"},
		{"блочная форма", "```yaml\n- alert: Q4\n  expr: |\n    rate(kaname_identities_total{path=\"/a #b\"}[5m]) > 0\n```\n"},
	} {
		got := strings.Join(alertExpressionsIn(quoted.doc), "\n")
		if !strings.Contains(got, "kaname_identities_total") {
			t.Fatalf("%s: ряд из выражения потерян — `#` внутри литерала принят за комментарий: %q",
				quoted.name, got)
		}
		if !strings.Contains(got, "> 0") {
			t.Fatalf("%s: хвост выражения отрезан по `#` внутри литерала: %q", quoted.name, got)
		}
	}

	// Законный близнец 7: `#`, прижатый к предыдущему знаку, комментария не
	// открывает ни в YAML, ни в PromQL.
	const glued = "```yaml\n" +
		"- alert: Glued\n" +
		"  expr: rate(kaname_identities_total{ref=\"a#b\"}[5m]) > 0\n" +
		"```\n"
	gl := strings.Join(alertExpressionsIn(glued), "\n")
	if !strings.Contains(gl, "kaname_identities_total") || !strings.Contains(gl, "> 0") {
		t.Fatalf("прижатый `#` принят за начало комментария: %q", gl)
	}

	// Разбор объявления: имя ряда берётся из кода, а не из его комментария.
	names := identityGrowthMetricNamesIn(`
// раньше ряд назывался "kaname_identity_old_total"
const IdentitiesTotalMetric = "kaname_identities_total"
`)
	if len(names) != 1 || names[0] != "kaname_identities_total" {
		t.Fatalf("имена рядов прочитаны неверно: %v", names)
	}
}

// readIdentityGrowthMetricNames — имена рядов, объявленные файлом коллектора.
func readIdentityGrowthMetricNames(t *testing.T, root string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, identityGrowthCollectorFile))
	if err != nil {
		t.Fatalf("файл коллектора не прочитан (%s): %v", identityGrowthCollectorFile, err)
	}
	return identityGrowthMetricNamesIn(string(b))
}

// identityGrowthMetricNamesIn — имена рядов в исходнике.
//
// Комментарии снимаются первыми: этот файл подробно объясняет, почему рядов два,
// и называет их имена в прозе. Гейт, читающий сырой текст, засчитал бы
// объявлением упоминание в разборе.
func identityGrowthMetricNamesIn(src string) []string {
	src = identityGrowthGoCommentRe.ReplaceAllString(src, "")
	seen := map[string]bool{}
	var out []string
	for _, m := range identityGrowthMetricNameRe.FindAllStringSubmatch(src, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	sort.Strings(out)
	return out
}

// alertExpressionsIn — тела выражений `expr:` правил оповещения.
//
// Берётся ТОЛЬКО исполняемая часть: однострочное выражение целиком либо блок,
// введённый `|`, до первого поля того же или меньшего отступа. Комментарии YAML
// снимаются — иначе имя ряда, УБРАННОГО из выражения и оставшегося в
// объяснении, продолжало бы считаться читателем.
func alertExpressionsIn(doc string) []string {
	var out []string
	lines := strings.Split(doc, "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimLeft(line, " ")
		if !strings.HasPrefix(trimmed, "expr:") {
			continue
		}
		indent := len(line) - len(trimmed)
		body := strings.TrimSpace(strings.TrimPrefix(trimmed, "expr:"))

		if body != "" && body != "|" && body != ">" {
			out = append(out, stripYAMLComments(body))
			continue
		}
		// Блочное выражение: всё, что отступлено ГЛУБЖЕ самого `expr:`.
		var block []string
		for j := i + 1; j < len(lines); j++ {
			next := lines[j]
			if strings.TrimSpace(next) == "" {
				continue
			}
			nextIndent := len(next) - len(strings.TrimLeft(next, " "))
			if nextIndent <= indent {
				break
			}
			block = append(block, next)
			i = j
		}
		out = append(out, stripYAMLComments(strings.Join(block, "\n")))
	}
	return out
}

// stripYAMLComments снимает комментарии — и строчные, и ХВОСТОВЫЕ.
//
// Хвостовая форма и есть предмет, ради которого снятие заведено: имя ряда,
// убранного из выражения и оставленного памяткой в конце строки, продолжало бы
// считаться читателем. Прежняя редакция снимала только строку-комментарий
// целиком, поэтому подмена вида
//
//	expr: increase(kaname_lro_inflight[1h]) > 100  # снято правило про kaname_identities_total
//
// оставляла гейт зелёным при отсутствующем читателе — в ОБЕИХ формах выражения,
// однострочной и блочной.
//
// Regex по всему тексту здесь не годится, и разбор YAML тоже. Regex не отличает
// комментарий от `#` внутри строкового литерала и вырезал бы исполняемую часть
// выражения. Разбор YAML не годится по другой причине: в блочном скаляре (`|`)
// YAML комментариев не знает вовсе — там `#` начинает комментарий PromQL,
// который снять всё равно надо. Поэтому — свой обход со знанием кавычек, одно
// правило на обе формы.
func stripYAMLComments(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = stripCommentFromLine(line)
	}
	return strings.Join(lines, "\n")
}

// stripCommentFromLine отрезает хвост строки, начиная с `#`, который открывает
// комментарий.
//
// `#` открывает комментарий, когда он первый знак строки либо ему предшествует
// пробельный, и он не внутри строкового литерала. Кавычек три вида: двойные и
// одинарные (ими квотирует и YAML, и PromQL) и обратные — сырая строка PromQL.
// `#`, прижатый к предыдущему знаку (`a#b`), комментария не открывает ни в YAML,
// ни в PromQL.
func stripCommentFromLine(line string) string {
	var quote byte // 0 — вне строки; иначе знак, которым она открыта
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quote != 0:
			if c == '\\' && quote == '"' {
				i++ // экранированный знак строку не закрывает
				continue
			}
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'' || c == '`':
			quote = c
		case c == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t'):
			return strings.TrimRight(line[:i], " \t")
		}
	}
	return line
}
