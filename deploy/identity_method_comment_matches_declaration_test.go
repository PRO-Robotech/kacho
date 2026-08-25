// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_method_comment_matches_declaration_test.go — ПЕРЕЧЕНЬ ПОЛОС ВХОДА
// СВЕРЯЕТСЯ С РАЗОБРАННЫМ ОБЪЯВЛЕНИЕМ, А НЕ С СОСЕДНИМ КОММЕНТАРИЕМ (#1256).
//
// # Предмет
//
// Комментарий в объявлении службы личности перечислял полосы входа как
// выключенные и лгал по двум пунктам из четырёх: обе названные полосы были
// объявлены включёнными в том же файле, несколькими строками ниже. Одна из них —
// метод ШТАТНОГО ВОССТАНОВЛЕНИЯ ДОСТУПА, на котором держится возврат арендатору
// потерянного доступа.
//
// Класс известный и дорогой: комментарий, противоречащий коду, — ловушка для
// следующего, он «починит» код под неверный текст. Здесь цена почти была
// уплачена: приёмка соседней фазы обосновывала решение этим перечнем, и
// неверность нашлась только потому, что перепись блока прогнали машинно.
//
// # Почему гейт читает РАЗОБРАННОЕ объявление
//
// Сверять текст комментария с текстом другого комментария значило бы проверять
// согласие лжи с самой собой. Авторитет здесь ровно один — величины `enabled:`,
// снятые обходом блока `selfservice.methods` по отступам. Перечень же —
// подсудимый, а не источник.
//
// # Почему только ОТМЕЧЕННЫЕ строки
//
// Сплошной обход прозы («имя полосы рядом со словом о состоянии») здесь
// негоден, и это не осторожность, а замер: тот же комментарий, который
// ОБЪЯСНЯЕТ дефект, называет все четыре имени рядом со словом «выключен» —
// значит сплошной предикат краснел бы на собственном разборе. Поэтому перечень
// несёт отметку (`ВЫКЛЮЧЕНЫ:` / `ВКЛЮЧЕНЫ:`), а проза не читается вовсе. То же
// решение и по той же причине принято у гейта имён заданий конвейера.
//
// # Что здесь НЕ утверждается
//
// Не утверждается, что состав включённых полос верен: он верен, неверен был
// рассказ о нём. И не требуется, чтобы перечень называл ВСЕ включённые полосы —
// это дублировало бы блок целиком. Полным обязан быть перечень ОТКЛЮЧЁННЫХ:
// полоса, выключенная молча, — ровно то, о чём читатель узнаёт последним.
//
// Читается ОБЪЯВЛЕНИЕ, а не рендер: ни helm, ни кластер не нужны, поэтому
// проверка не умеет пропускаться.
package deploy_test

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// identityMethodState — что объявление говорит о полосе. Величин две, потому
// что тело — Go-шаблон: одна и та же полоса бывает объявлена по-разному в
// разных ветках (`webauthn` на посадке без доменного имени). Такая полоса
// УСЛОВНА, и безусловного утверждения о ней в перечне быть не может.
type identityMethodState struct {
	SawEnabled  bool
	SawDisabled bool
}

func (s identityMethodState) conditional() bool { return s.SawEnabled && s.SawDisabled }

// identityStateClaim — одна отмеченная строка перечня.
type identityStateClaim struct {
	Line    int
	Enabled bool // что строка УТВЕРЖДАЕТ о названных полосах
	Names   []string
	Raw     string
}

// identityClaimMarker — отметка, по которой строка перечня отличается от прозы.
// Альтернатива упорядочена так, что «ВЫКЛЮЧЕНЫ» проверяется первой.
var identityClaimMarker = regexp.MustCompile(`^(ВЫКЛЮЧЕНЫ|ВКЛЮЧЕНЫ)\s*:\s*(.+)$`)

// identityMethodName — имя полосы: только строчная латиница и подчёркивание.
var identityMethodName = regexp.MustCompile(`^[a-z_]+$`)

// scanIdentityMethodsBlock обходит блок `selfservice.methods` по отступам и
// снимает ОБЕ стороны спора: объявленные величины `enabled:` и отмеченные
// строки перечня.
//
// Разбор строчный, а не через YAML-библиотеку: тело — Go-шаблон, в котором
// величины стоят подстановками, поэтому валидным YAML оно не является ни на
// одной ревизии. Тот же приём у соседних проверок этого каталога.
func scanIdentityMethodsBlock(body string) (map[string]identityMethodState, []identityStateClaim, int) {
	indentOf := func(s string) int { return len(s) - len(strings.TrimLeft(s, " ")) }

	states := map[string]identityMethodState{}
	var claims []identityStateClaim
	commentsRead := 0

	inMethods := false
	methodsIndent := -1
	method := ""

	for i, ln := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(ln)

		// Отметка перечня ищется в тексте ЛЮБОГО комментария блока — как
		// отдельной строкой, так и хвостом за величиной.
		comment := ""
		if idx := strings.Index(trimmed, "#"); idx >= 0 {
			comment = strings.TrimSpace(trimmed[idx+1:])
		}
		if inMethods && comment != "" {
			commentsRead++
			if m := identityClaimMarker.FindStringSubmatch(comment); m != nil {
				claims = append(claims, identityStateClaim{
					Line:    i + 1,
					Enabled: m[1] == "ВКЛЮЧЕНЫ",
					Names:   splitIdentityMethodNames(m[2]),
					Raw:     comment,
				})
			}
		}

		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "{{") {
			continue
		}
		if !inMethods {
			if trimmed == "methods:" {
				inMethods, methodsIndent = true, indentOf(ln)
			}
			continue
		}
		ind := indentOf(ln)
		if ind <= methodsIndent {
			break // вышли из `methods:`
		}
		switch {
		case ind == methodsIndent+2 && strings.HasSuffix(trimmed, ":"):
			method = strings.TrimSuffix(trimmed, ":")
			if _, ok := states[method]; !ok {
				states[method] = identityMethodState{}
			}
		case method == "":
			// величина до первого имени полосы — не наша
		case ind == methodsIndent+4:
			key, val, ok := splitYAMLPair(trimmed)
			if !ok || key != "enabled" {
				continue
			}
			st := states[method]
			if val == "true" {
				st.SawEnabled = true
			} else {
				st.SawDisabled = true
			}
			states[method] = st
		}
	}
	return states, claims, commentsRead
}

// splitIdentityMethodNames режет хвост отмеченной строки на имена полос.
func splitIdentityMethodNames(tail string) []string {
	var out []string
	for _, tok := range strings.FieldsFunc(tail, func(r rune) bool {
		return r == ',' || r == ' ' || r == '`' || r == '\t' || r == '.' || r == ';'
	}) {
		if identityMethodName.MatchString(tok) {
			out = append(out, tok)
		}
	}
	return out
}

// judgeIdentityStateClaims — ядро гейта, отделённое от чтения дерева, чтобы
// доказательство инъекцией подавало ему настоящий и синтетический вход.
//
// Возвращает находки словами; пустой срез означает согласие перечня с
// объявлением.
func judgeIdentityStateClaims(states map[string]identityMethodState, claims []identityStateClaim) []string {
	var findings []string

	claimedDisabled := map[string]bool{}
	for _, c := range claims {
		for _, name := range c.Names {
			st, known := states[name]
			switch {
			case !known:
				findings = append(findings, fmt.Sprintf(
					"строка %d: перечень называет полосу %q, которой в объявлении НЕТ. "+
						"Либо полоса снята и перечень её пережил, либо имя написано неверно — "+
						"в обоих случаях читатель ищет в блоке то, чего там не найдёт",
					c.Line, name))
			case st.conditional():
				findings = append(findings, fmt.Sprintf(
					"строка %d: перечень утверждает о полосе %q безусловно, а объявление "+
						"даёт ей РАЗНЫЕ величины в разных ветках шаблона. Безусловного "+
						"утверждения о такой полосе не существует: её состояние решает посадка",
					c.Line, name))
			case c.Enabled && !st.SawEnabled:
				findings = append(findings, fmt.Sprintf(
					"строка %d: перечень называет полосу %q включённой, объявление — "+
						"`enabled: false`", c.Line, name))
			case !c.Enabled && !st.SawDisabled:
				findings = append(findings, fmt.Sprintf(
					"строка %d: перечень называет полосу %q выключенной, объявление — "+
						"`enabled: true`", c.Line, name))
			}
			if !c.Enabled {
				claimedDisabled[name] = true
			}
		}
	}

	var missed []string
	for name, st := range states {
		if st.conditional() || !st.SawDisabled {
			continue
		}
		if !claimedDisabled[name] {
			missed = append(missed, name)
		}
	}
	sort.Strings(missed)
	if len(missed) > 0 {
		findings = append(findings, fmt.Sprintf(
			"объявление выключает полосы %v, а перечень их не называет. Перечень "+
				"отключённых обязан быть ПОЛНЫМ: полоса, выключенная молча, — ровно то, "+
				"о чём читатель узнаёт последним", missed))
	}
	return findings
}

func TestIdentity_MethodStateCommentMatchesDeclaration(t *testing.T) {
	body := readFileForTest(t, identityConfigTemplate)
	states, claims, commentsRead := scanIdentityMethodsBlock(body)

	if len(states) == 0 {
		t.Fatalf("полос службы личности не разобрано ни одной (%s) — вердикта нет: "+
			"«ноль находок» здесь неотличимо от «ноль прочитанного». Либо блок "+
			"`selfservice.methods` переехал, либо разбор перестал его видеть",
			identityConfigTemplate)
	}
	if commentsRead == 0 {
		t.Fatalf("в блоке `selfservice.methods` (%s) не прочитано ни одного комментария — "+
			"предмет проверки не осмотрен, и её зелёный ничего не значит",
			identityConfigTemplate)
	}

	enabled, disabled, conditional := 0, 0, 0
	for _, st := range states {
		switch {
		case st.conditional():
			conditional++
		case st.SawEnabled:
			enabled++
		default:
			disabled++
		}
	}
	judged := 0
	for _, c := range claims {
		judged += len(c.Names)
	}

	t.Logf("перепись объявления: полос %d · включено %d · выключено %d · условных %d",
		len(states), enabled, disabled, conditional)
	t.Logf("перепись перечня: комментариев прочитано %d · отмеченных строк %d · имён сверено %d",
		commentsRead, len(claims), judged)

	if len(claims) == 0 {
		t.Fatalf("отмеченного перечня полос в %s НЕТ, а гейт заведён ради его правдивости "+
			"(#1256). Исходов два, третьего нет: вернуть перечень (строки `ВЫКЛЮЧЕНЫ:` / "+
			"`ВКЛЮЧЕНЫ:` в комментарии блока `selfservice.methods`) ЛИБО снять этот гейт "+
			"вместе с ним. Молчаливое исчезновение предмета делает проверку вакуумной, "+
			"оставляя её зелёной", identityConfigTemplate)
	}

	for _, f := range judgeIdentityStateClaims(states, claims) {
		t.Errorf("перечень полос входа расходится с объявлением (%s): %s.\n"+
			"Комментарий, противоречащий коду, — ловушка для следующего: он «починит» "+
			"код под неверный текст. Правится ПЕРЕЧЕНЬ, если верно объявление, и "+
			"объявление, если решение сменилось, — но никогда не оба порознь",
			identityConfigTemplate, f)
	}
}
