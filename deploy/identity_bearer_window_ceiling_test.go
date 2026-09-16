// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_bearer_window_ceiling_test.go — MAIL-51 приёмки ID-MAIL-1: потолок
// сроков ЧУЖИХ предъявителей читает ВСЕ объявления, способные расширить окно.
//
// ПРЕДМЕТ. Коды подтверждения адреса и восстановления доступа принадлежат
// поставщику личности: выкупаются они его самообслуживанием мимо нас, и отозвать
// их немедленно нам нечем. Решение Р21б принимает это и называет окном их
// действия ОБЪЯВЛЕННЫЙ СРОК. Решение, у которого нет механизма истечения, —
// обещание: подняв срок, посадка молча расширила бы окно, и заметить это было бы
// неоткуда.
//
// ЧТО ИМЕННО УТВЕРЖДАЕТСЯ — ТРИ ВЕЩИ, И ТРЕТЬЯ ВАЖНЕЕ ДВУХ ПЕРВЫХ.
//
//	A. ВСЯКОЕ объявление срока в конфигурации личности ОТНЕСЕНО: либо к
//	   ограничивающим окно (тогда у него есть потолок), либо к неограничивающим
//	   — с названной причиной. Необъявленное — находка. Это и есть механизм
//	   истечения: новое объявление срока краснеет, пока его не разобрали.
//
//	B. Объявление, ограничивающее окно, не превышает своего потолка.
//
//	C. Обе полосы остаются `use: code`. Смена на ссылку меняет ПРЕДМЕТ решения
//	   Р21б: предъявителем становится ссылка, живущая в почтовом ящике, и довод
//	   «окно есть срок» перестаёт описывать то, что происходит.
//
// ЧЕГО ГЕЙТ НЕ УТВЕРЖДАЕТ. Какое из объявлений «побеждает» у чужого процесса —
// свойство этого процесса, из нашего дерева не измеримое. Утверждается, что ни
// одно способное расширить окно не осталось непрочитанным: тогда окно ограничено
// при любом ответе на вопрос о победителе.
//
// Круг 5 приёмки перемерил охват и нашёл прежнюю формулировку УЖЕ предмета: окно
// задают ТРИ объявления, а читались два. Гейт, читающий два из трёх, зелен при
// поднятом третьем — то есть послабление не истекло бы.
package deploy_test

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

// bearerWindowLifespanRe — объявление срока: ключ с величиной, привязанный к
// началу строки. Проза об этих же сроках несёт слово посреди предложения, и
// образец без привязки читал бы собственное объяснение как объявление.
var bearerWindowLifespanRe = regexp.MustCompile(`^(\s*)lifespan:\s*([0-9]+[a-z]+)\s*(?:#.*)?$`)

// bearerWindowKeyRe — ключ YAML для восстановления пути объявления.
var bearerWindowKeyRe = regexp.MustCompile(`^(\s*)([a-z_]+):`)

// bearerWindowUseRe — объявление способа доставки предъявителя. Читается ТЕМ ЖЕ
// обходом, что и сроки: второй разбор того же файла разошёлся бы с первым ровно
// там, где расхождение не видно, — на форме, которую знает один и не знает
// другой.
var bearerWindowUseRe = regexp.MustCompile(`^(\s*)use:\s*([a-z]+)\s*(?:#.*)?$`)

// bearerWindowRule — как разобрано ОДНО объявление срока.
type bearerWindowRule struct {
	// Ceiling — потолок; ноль означает «окно не ограничивает».
	Ceiling time.Duration
	// Why — причина, по которой объявление окна НЕ ограничивает. Обязательна у
	// неограничивающих: «не ограничивает» без причины есть послабление, которое
	// не истечёт никогда.
	Why string
}

// bearerWindowRuleset — РАЗБОР объявлений сроков, поимённо по пути.
//
// Перечень ЗАКРЫТ намеренно: объявление, которого здесь нет, — находка, и это
// единственный механизм, которым решение Р21б истекает само. Приписать сюда
// строку «чтобы прошло» — не исход: каждая запись есть место, куда окно
// расширяют незамеченным.
var bearerWindowRuleset = map[string]bearerWindowRule{
	// ─── ОГРАНИЧИВАЮТ ОКНО. Потолки — величины решения Р21б, а не сегодняшние
	// значения: подняв объявление, посадка обязана упереться в гейт.
	"selfservice.methods.code.config.lifespan": {Ceiling: 5 * time.Minute},
	"selfservice.flows.recovery.lifespan":      {Ceiling: 5 * time.Minute},
	"selfservice.flows.verification.lifespan":  {Ceiling: 15 * time.Minute},

	// ─── НЕ ОГРАНИЧИВАЮТ, и у каждого названа причина.
	"selfservice.flows.registration.lifespan": {
		Why: "срок ФОРМЫ регистрации: он ограничивает, сколько живёт незаполненная " +
			"форма в браузере, и предъявителя не выдаёт вовсе",
	},
	"selfservice.flows.login.lifespan": {
		Why: "срок ФОРМЫ входа — то же самое: предъявителя не выдаёт",
	},
	"selfservice.flows.settings.lifespan": {
		Why: "срок ФОРМЫ правки своих данных: предъявителя не выдаёт",
	},
	"session.lifespan": {
		Why: "срок СЕССИИ браузера. Он не окно предъявителя письма, а окно уже " +
			"состоявшегося входа, и отзывается он нашей же полосой выхода — то есть " +
			"механизм у него есть, и другой",
	},
}

// bearerWindowCodeLanes — полосы, обязанные оставаться кодовыми.
var bearerWindowCodeLanes = []string{"recovery", "verification"}

// bearerWindowLifespan — одно прочитанное объявление.
type bearerWindowLifespan struct {
	Path  string
	Value time.Duration
	Line  int
	Raw   string
}

// readBearerWindowLifespans восстанавливает путь каждого объявления по отступам.
func readBearerWindowLifespans(t *testing.T, body string) []bearerWindowLifespan {
	t.Helper()
	decls, _ := readBearerWindowDeclarations(t, body)
	return decls
}

// readBearerWindowDeclarations — ОДИН обход: сроки и способы доставки.
func readBearerWindowDeclarations(t *testing.T, body string) ([]bearerWindowLifespan, map[string]string) {
	t.Helper()
	uses := map[string]string{}
	type frame struct {
		indent int
		key    string
	}
	var stack []frame
	var out []bearerWindowLifespan

	for i, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") ||
			strings.HasPrefix(trimmed, "{{") || strings.HasPrefix(trimmed, "-") {
			continue
		}
		m := bearerWindowKeyRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		indent := len(m[1])
		for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
			stack = stack[:len(stack)-1]
		}
		key := m[2]
		if um := bearerWindowUseRe.FindStringSubmatch(line); um != nil {
			var path []string
			for _, f := range stack {
				path = append(path, f.key)
			}
			uses[strings.Join(path, ".")+".use"] = um[2]
			continue
		}
		if lm := bearerWindowLifespanRe.FindStringSubmatch(line); lm != nil {
			var path []string
			for _, f := range stack {
				path = append(path, f.key)
			}
			path = append(path, "lifespan")
			d, err := time.ParseDuration(lm[2])
			if err != nil {
				t.Errorf("строка %d: величина срока %q не разбирается — гейт не может "+
					"судить то, чего не прочитал", i+1, lm[2])
				continue
			}
			out = append(out, bearerWindowLifespan{
				Path: strings.Join(path, "."), Value: d, Line: i + 1, Raw: lm[2],
			})
			continue
		}
		stack = append(stack, frame{indent: indent, key: key})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return out, uses
}

// bearerWindowFindings — вердикт над НАЗВАННЫМ телом шаблона.
func bearerWindowFindings(t *testing.T, body string) []string {
	t.Helper()
	decls, uses := readBearerWindowDeclarations(t, body)

	var bounding int
	var findings []string
	for _, d := range decls {
		rule, known := bearerWindowRuleset[d.Path]
		switch {
		case !known:
			findings = append(findings, fmt.Sprintf(
				"строка %d: объявление срока `%s` (= %s) не отнесено НИ к ограничивающим "+
					"окно чужого предъявителя, НИ к неограничивающим с причиной. Решение "+
					"Р21б называет окном чужих кодов объявленный срок — значит всякое новое "+
					"объявление обязано быть разобрано, иначе окно расширяют молча.",
				d.Line, d.Path, d.Raw))
		case rule.Ceiling == 0:
			// Не ограничивает — причина названа в разборе, проверять нечего.
		default:
			bounding++
			if d.Value > rule.Ceiling {
				findings = append(findings, fmt.Sprintf(
					"строка %d: срок `%s` = %s превышает потолок %s. Это окно, в течение "+
						"которого чужой предъявитель действует, а отозвать его немедленно нам "+
						"нечем: выкупается он самообслуживанием поставщика мимо нас.",
					d.Line, d.Path, d.Raw, rule.Ceiling))
			}
		}
	}

	// C. Обе полосы остаются кодовыми. Путь берётся ТЕМ ЖЕ обходом, что и сроки:
	// нарезка тела строками читала бы соседний блок при первом же сдвиге отступа.
	for _, lane := range bearerWindowCodeLanes {
		key := "selfservice.flows." + lane + ".use"
		got, declared := uses[key]
		switch {
		case !declared:
			findings = append(findings, fmt.Sprintf(
				"полоса `%s` не объявляет способа доставки (`%s`) — форма сменилась либо "+
					"объявление снято, и утверждение о ней стало беспредметным", lane, key))
		case got != "code":
			findings = append(findings, fmt.Sprintf(
				"полоса `%s` объявлена как `%s`, а не `code`. Это меняет ПРЕДМЕТ решения "+
					"Р21б: предъявителем становится ссылка, живущая в почтовом ящике, и довод "+
					"«окно есть объявленный срок» перестаёт описывать происходящее.", lane, got))
		}
	}

	t.Logf("перепись: объявлений срока прочитано %d · из них ограничивающих окно %d",
		len(decls), bounding)

	if len(decls) == 0 {
		t.Fatalf("объявлений срока не прочитано ни одного — предикат перестал их узнавать. " +
			"«Ноль находок» здесь неотличимо от «ноль прочитанного», поэтому это отказ, " +
			"а не тишина")
	}
	if bounding == 0 {
		t.Fatalf("ограничивающих окно объявлений не найдено ни одного при %d прочитанных — "+
			"разбор путей сломался, и потолок не судит ничего", len(decls))
	}
	return findings
}

// TestMAIL51BearerWindowCeilingReadsEveryLifespan — сам гейт.
func TestMAIL51BearerWindowCeilingReadsEveryLifespan(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(identityConfigTemplate)
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: шаблон %s не читается: %v", identityConfigTemplate, err)
	}
	for _, f := range bearerWindowFindings(t, string(raw)) {
		t.Errorf("%s\n  (%s)", f, identityConfigTemplate)
	}
}
