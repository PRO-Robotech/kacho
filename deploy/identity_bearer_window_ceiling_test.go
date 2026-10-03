// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_bearer_window_ceiling_test.go — MAIL-51 приёмки ID-MAIL-1: потолок
// сроков предъявителей, уходящих ПИСЬМОМ, читает ВСЕ объявления срока полосы
// входа, способные расширить окно.
//
// ПРЕДМЕТ. Код восстановления доступа и код подтверждения адреса уходят письмом
// и живут в почтовом ящике: окно их действия — объявленный срок. Решение, у
// которого нет механизма истечения, — обещание: подняв срок, посадка молча
// расширила бы окно, и заметить это было бы неоткуда.
//
// ЧЕЙ ЭТО НОСИТЕЛЬ ТЕПЕРЬ (kacho#2818). Прежде коды чеканил внешний поставщик
// личности, и гейт читал его настройки — шаблон, который подчарт службы больше
// не производит. Под единственной посадкой службы (kaname#363) оба кода чеканит
// НАША полоса входа, и их сроки объявляет НАШ шаблон — блок `authn.login`
// карты настроек службы (`charts/kaname/templates/configmap.yaml`). Судится
// рендер этого блока на каждом стеке deploy/stacks.txt.
//
// ЧТО ИМЕННО УТВЕРЖДАЕТСЯ.
//
//	A. ВСЯКОЕ объявление срока блока (ключ, оканчивающийся на `-ttl`) ОТНЕСЕНО:
//	   либо к ограничивающим окно письма (тогда у него есть потолок), либо к
//	   неограничивающим — с названной причиной. Неотнесённое — находка. Это и
//	   есть механизм истечения: новое объявление срока краснеет, пока его не
//	   разобрали, — так же краснеет и полоса, сменившая код на ссылку новым
//	   ключом.
//
//	B. Объявление, ограничивающее окно, не превышает своего потолка. Потолки —
//	   величины решений, а не сегодняшние значения профиля: код восстановления —
//	   5 минут (Ф5 Р1, перенос прежней величины), код подтверждения адреса —
//	   30 минут (kaname#456 Р9), код регистрации — 24 часа (NTF-2 службы Р8).
//
// ЧЕГО ГЕЙТ НЕ УТВЕРЖДАЕТ. Годность величины по правилам службы — её страж
// старта (у кода подтверждения свой потолок, 24 часа); здесь — что окно письма
// не шире решения, и что ни одно объявление срока не осталось неразобранным.
package deploy_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"
)

// bearerWindowRule — как разобрано ОДНО объявление срока.
type bearerWindowRule struct {
	// Ceiling — потолок; ноль означает «окно письма не ограничивает».
	Ceiling time.Duration
	// Why — причина, по которой объявление окна НЕ ограничивает. Обязательна у
	// неограничивающих: «не ограничивает» без причины есть послабление, которое
	// не истечёт никогда.
	Why string
}

// bearerWindowRuleset — РАЗБОР объявлений сроков блока `authn.login`, поимённо.
//
// Перечень ЗАКРЫТ намеренно: объявление, которого здесь нет, — находка. Приписать
// сюда строку «чтобы прошло» — не исход: каждая запись есть место, куда окно
// расширяют незамеченным.
var bearerWindowRuleset = map[string]bearerWindowRule{
	// ─── ОГРАНИЧИВАЮТ ОКНО ПИСЬМА.
	"recovery-code-ttl":     {Ceiling: 5 * time.Minute},
	"verification-code-ttl": {Ceiling: 30 * time.Minute},
	// Код регистрации «сначала письмо» (З14) уходит письмом назначения
	// `registration` и живёт в почтовом ящике так же, как два кода выше.
	// Потолок — величина решения: граница срока в таблице Р8 приёмки NTF-2
	// службы (5m…24h; ленту привела ветка kacho#2915, разбор добавлен догоном
	// main, kacho#2914).
	"registration-code-ttl": {Ceiling: 24 * time.Hour},

	// ─── НЕ ОГРАНИЧИВАЮТ, и у каждого названа причина.
	"session-ttl": {
		Why: "срок СЕССИИ браузера: это окно уже состоявшегося входа, а не предъявителя " +
			"письма, и закрывается оно нашей же полосой выхода — механизм у него есть, и другой",
	},
}

// bearerWindowLifespan — одно прочитанное объявление срока.
type bearerWindowLifespan struct {
	Key   string
	Raw   string
	Value time.Duration
	Err   error
}

// bearerWindowLifespansOf — объявления срока блока `authn.login` тела настроек.
func bearerWindowLifespansOf(cfg map[string]any) []bearerWindowLifespan {
	login, _ := configSection(cfg, "authn", "login")
	var out []bearerWindowLifespan
	for k, v := range login {
		if !strings.HasSuffix(k, "-ttl") {
			continue
		}
		raw := fmt.Sprint(v)
		d, err := time.ParseDuration(raw)
		out = append(out, bearerWindowLifespan{Key: k, Raw: raw, Value: d, Err: err})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// bearerWindowFindings — вердикт над объявлениями одного стека.
func bearerWindowFindings(stack string, decls []bearerWindowLifespan) (findings []string, bounding int) {
	for _, d := range decls {
		rule, known := bearerWindowRuleset[d.Key]
		switch {
		case d.Err != nil:
			findings = append(findings, fmt.Sprintf("стек %s: величина срока `authn.login.%s` = %q "+
				"не разбирается — гейт не может судить то, чего не прочитал", stack, d.Key, d.Raw))
		case !known:
			findings = append(findings, fmt.Sprintf(
				"стек %s: объявление срока `authn.login.%s` (= %s) не отнесено НИ к ограничивающим "+
					"окно письма, НИ к неограничивающим с причиной — всякое новое объявление "+
					"обязано быть разобрано, иначе окно расширяют молча", stack, d.Key, d.Raw))
		case rule.Ceiling == 0:
			// Не ограничивает — причина названа в разборе, проверять нечего.
		default:
			bounding++
			if d.Value > rule.Ceiling {
				findings = append(findings, fmt.Sprintf(
					"стек %s: срок `authn.login.%s` = %s превышает потолок %s — это окно, в "+
						"течение которого код письма действует из почтового ящика", stack, d.Key, d.Raw, rule.Ceiling))
			}
		}
	}
	return findings, bounding
}

// TestMAIL51BearerWindowCeilingReadsEveryLifespan — сам гейт, по каждому стеку.
func TestMAIL51BearerWindowCeilingReadsEveryLifespan(t *testing.T) {
	stacks := deployStacks(t)
	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)
	var read, bounding int
	for _, name := range names {
		out, err := renderStackSubchart(t, name, stackIdentityValues(t, stacks[name]))
		if err != nil {
			t.Fatalf("стек %s: рендер подчарта службы отказал: %v\n%s", name, err, out)
		}
		decls := bearerWindowLifespansOf(kanameServiceConfig(t, out))
		findings, b := bearerWindowFindings(name, decls)
		for _, f := range findings {
			t.Error(f)
		}
		read += len(decls)
		bounding += b
		if b == 0 {
			t.Errorf("стек %s: ограничивающих окно объявлений не прочитано ни одного при %d "+
				"прочитанных — разбор перестал узнавать блок, и потолок не судит ничего", name, len(decls))
		}
	}
	t.Logf("перепись: стеков %d · объявлений срока прочитано %d · из них ограничивающих окно %d",
		len(names), read, bounding)
	if read == 0 {
		t.Fatal("объявлений срока не прочитано ни одного — «ноль находок» здесь неотличимо от " +
			"«ноль прочитанного», поэтому это отказ, а не тишина")
	}
}
