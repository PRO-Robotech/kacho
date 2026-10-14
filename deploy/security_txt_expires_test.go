// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// security_txt_expires_test.go — `Expires` у security.txt есть ОБЪЯВЛЕННАЯ
// величина, а рендер карты детерминирован по входу (kacho#2876).
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО БЫЛО
//
// Шаблон писал `Expires: {{ now | date … }}` — момент рендера. По RFC 9116 §2.5.5
// файл с прошедшим `Expires` устарел, и его сведениям не следует доверять: канал
// сообщения о проблемах безопасности, который продукт объявляет, истекал в ту
// секунду, в которую выкатывался. Вдобавок `date` форматирует МЕСТНОЕ время, а
// литерал `Z` объявлял его UTC — метка лгала на смещение зоны. И каждый upgrade
// давал дифф карты при неизменном входе.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО УТВЕРЖДАЕТСЯ
//
//  1. шаблон не читает часов: ни слова `now` (граница слова, а не подстрока:
//     «known» и «Acknowledgments» — законные поля), ни функций времени в
//     действиях шаблона;
//     и строки `Expires` и аннотация даты выпуска выводят ровно объявленные
//     величины `security.txt.expires` и `security.txt.published`;
//  2. в объявлении КАЖДОЙ цепочки deploy/stacks.txt (профили накладываются, как
//     их накладывает helm) `expires` — UTC в форме RFC 3339, позже объявленной
//     даты выпуска, не дальше года от неё и позже момента проверки с запасом
//     `securityTxtRenewMargin`. Последнее судится РЕАЛЬНЫМИ часами: на
//     управляемых «позже момента проверки» было бы вечно зелёным, и объявленная
//     дата истекла бы молча. Запас и есть самоистечение: проверка краснеет ДО
//     истечения, а не после. Судится объявление, а не рендер: рендер умбреллы
//     требует собранных зависимостей, которых у прогона юнитов нет, а что
//     рендер выводит ровно объявленное, держит утверждение 1;
//  3. два рендера одной цепочки дают побайтно одну и ту же карту, и в ней ровно
//     одно поле Expires в форме UTC — deploy/tests/helm/security-txt-render-test.sh
//     (обход каталога манифест-проверок, где зависимости собраны).
//
// Решение вынесено чистой функцией `judgeSecurityTxt(момент, истечение, выпуск)`:
// инъекции (Expires = момент проверки, дальше года, местное время, нет поля)
// подаются ей на УПРАВЛЯЕМЫХ часах вместе с законным близнецом —
// TestSecurityTxtJudge_InjectionsOnAControlledClock.
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

const (
	securityTxtTemplate = umbrellaDir + "/templates/security-txt-configmap.yaml"
	// securityTxtPublished — аннотация карты с объявленной датой выпуска файла.
	securityTxtPublished = "kacho.cloud/security-txt-published"
	// securityTxtRenewMargin — за сколько до объявленного истечения проверка
	// дерева краснеет: столько остаётся на перевыпуск даты и выкатку.
	securityTxtRenewMargin = 30 * 24 * time.Hour
	// securityTxtInjectionStack — цепочка, чей рендер даёт НАСТОЯЩИЙ вход инъекциям.
	securityTxtInjectionStack = "a8f60d"
)

var (
	securityTxtNowWord = regexp.MustCompile(`\bnow\b`)
	// Функции времени sprig внутри действия шаблона.
	securityTxtClockCall = regexp.MustCompile(`\{\{[^}]*\b(now|date|dateInZone|htmlDate|htmlDateInZone|unixEpoch|dateModify|ago)\b[^}]*\}\}`)
	securityTxtUTC       = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`)
)

// judgeSecurityTxtTemplate — находки о шаблоне: чтение часов.
func judgeSecurityTxtTemplate(src string) []string {
	var out []string
	for i, line := range strings.Split(src, "\n") {
		if securityTxtNowWord.MatchString(line) {
			out = append(out, fmt.Sprintf("стр. %d несёт слово `now`: %q", i+1, strings.TrimSpace(line)))
		}
		if m := securityTxtClockCall.FindStringSubmatch(line); m != nil {
			out = append(out, fmt.Sprintf("стр. %d читает часы функцией %s: %q", i+1, m[1], strings.TrimSpace(line)))
		}
	}
	return out
}

// parseUTC — момент в форме RFC 3339 с зоной `Z` и только в ней.
func parseUTC(v string) (time.Time, error) {
	if !securityTxtUTC.MatchString(v) {
		return time.Time{}, fmt.Errorf("%q — не UTC в форме RFC 3339 (YYYY-MM-DDTHH:MM:SSZ)", v)
	}
	return time.Parse(time.RFC3339, v)
}

// judgeSecurityTxt — находки об объявленных истечении `expires` и дате выпуска
// `published` в момент `now`.
func judgeSecurityTxt(now time.Time, expires, published string) []string {
	exp, err := parseUTC(expires)
	if err != nil {
		return []string{"security.txt.expires: " + err.Error()}
	}
	pub, err := parseUTC(published)
	if err != nil {
		return []string{"security.txt.published: " + err.Error()}
	}
	var out []string
	if !exp.After(pub) {
		out = append(out, fmt.Sprintf("expires %s не позже объявленной даты выпуска %s", expires, published))
	}
	if exp.After(pub.AddDate(1, 0, 0)) {
		out = append(out, fmt.Sprintf("expires %s дальше года от объявленной даты выпуска %s (RFC 9116 §2.5.5)", expires, published))
	}
	if !exp.After(now.Add(securityTxtRenewMargin)) {
		out = append(out, fmt.Sprintf("expires %s наступает раньше, чем через %s от момента проверки %s: "+
			"объяви новые security.txt.published и security.txt.expires в values.yaml зонта и выкати",
			expires, securityTxtRenewMargin, now.UTC().Format(time.RFC3339)))
	}
	return out
}

// securityTxtDeclared — объявленные величины цепочки: профили накладываются
// слева направо поверх умолчаний чарта, ровно как их получает helm.
func securityTxtDeclared(t *testing.T, chain []string) (enabled bool, expires, published any) {
	t.Helper()
	merged := readYAML(t, umbrellaDir+"/values.yaml")
	for _, p := range chain {
		merged = mergeValues(merged, readYAML(t, umbrellaDir+"/"+p))
	}
	on, _ := dig(merged, "security", "txt", "enabled").(bool)
	return on, dig(merged, "security", "txt", "expires"), dig(merged, "security", "txt", "published")
}

// asDeclaredString — объявленная величина СТРОКОЙ. Незакавыченная дата в YAML
// разбирается как метка времени, а helm везёт значения через JSON — форма,
// которую видит проверка, и форма, которую видит шаблон, разошлись бы.
func asDeclaredString(v any) string {
	s, ok := v.(string)
	if !ok {
		return fmt.Sprintf("<не строка: %T %v>", v, v)
	}
	return s
}

func TestSecurityTxtTemplateReadsNoClock(t *testing.T) {
	raw, err := os.ReadFile(securityTxtTemplate)
	if err != nil {
		t.Fatalf("шаблон %s не читается (%v) — предпосылка пробы исчезла", securityTxtTemplate, err)
	}
	src := string(raw)
	lines := strings.Count(src, "\n")
	t.Logf("перепись: строк шаблона осмотрено %d", lines)
	if lines == 0 {
		t.Fatal("шаблон пуст — судить нечего, и это отказ, а не чистота")
	}
	for _, f := range judgeSecurityTxtTemplate(src) {
		t.Errorf("%s: %s — Expires и прочие поля объявляются величиной профиля, а не моментом рендера", securityTxtTemplate, f)
	}
	for _, f := range judgeSecurityTxtTemplateReadsTheDeclaration(src) {
		t.Errorf("%s: %s", securityTxtTemplate, f)
	}
}

// securityTxtExpiresLine и securityTxtPublishedLine — строки шаблона, которые
// выводят объявленные величины; любая иная форма — не та величина.
var (
	securityTxtExpiresLine   = regexp.MustCompile(`(?m)^\s*Expires:\s*\{\{-?\s*\$expires\s*-?\}\}\s*$`)
	securityTxtExpiresBind   = regexp.MustCompile(`\$expires\s*:=\s*\.Values\.security\.txt\.expires\b`)
	securityTxtPublishedLine = regexp.MustCompile(`(?m)^\s*kacho\.cloud/security-txt-published:\s*\{\{-?\s*\$published\s*\|\s*quote\s*-?\}\}\s*$`)
	securityTxtPublishedBind = regexp.MustCompile(`\$published\s*:=\s*\.Values\.security\.txt\.published\b`)
)

func judgeSecurityTxtTemplateReadsTheDeclaration(src string) []string {
	var out []string
	if n := len(securityTxtExpiresLine.FindAllString(src, -1)); n != 1 || !securityTxtExpiresBind.MatchString(src) {
		out = append(out, fmt.Sprintf("строка Expires выводит не объявленную величину security.txt.expires (строк формы `Expires: {{ $expires }}` %d, привязка $expires к .Values.security.txt.expires: %t)",
			n, securityTxtExpiresBind.MatchString(src)))
	}
	if n := len(securityTxtPublishedLine.FindAllString(src, -1)); n != 1 || !securityTxtPublishedBind.MatchString(src) {
		out = append(out, fmt.Sprintf("аннотация %s выводит не объявленную величину security.txt.published (строк %d)", securityTxtPublished, n))
	}
	return out
}

func TestSecurityTxtExpiresIsDeclaredOnEveryStack(t *testing.T) {
	stacks := deployStacks(t)
	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)
	now := time.Now().UTC()
	judged, off := 0, 0
	for _, name := range names {
		on, exp, pub := securityTxtDeclared(t, stacks[name])
		if !on {
			off++
			t.Logf("стек %s: security.txt выключен — законный близнец", name)
			continue
		}
		judged++
		for _, f := range judgeSecurityTxt(now, asDeclaredString(exp), asDeclaredString(pub)) {
			t.Errorf("стек %s (%s): %s", name, strings.Join(stacks[name], " + "), f)
		}
	}
	t.Logf("перепись: стеков %d, судимо %d, выключено %d; момент проверки %s", len(names), judged, off, now.Format(time.RFC3339))
	if judged == 0 {
		t.Fatal("ни в одном стеке security.txt не включён — судить нечего, обход пуст")
	}
}

func TestSecurityTxtJudge_InjectionsOnAControlledClock(t *testing.T) {
	chain, ok := deployStacks(t)[securityTxtInjectionStack]
	if !ok {
		t.Fatalf("стека %s нет в таблице — настоящего входа для инъекций нет", securityTxtInjectionStack)
	}
	_, e, p := securityTxtDeclared(t, chain)
	expires, published := asDeclaredString(e), asDeclaredString(p)
	pub, err := parseUTC(published)
	if err != nil {
		t.Fatalf("объявленная дата выпуска стека %s: %v", securityTxtInjectionStack, err)
	}
	exp, err := parseUTC(expires)
	if err != nil {
		t.Fatalf("объявленное истечение стека %s: %v", securityTxtInjectionStack, err)
	}
	const utc = "2006-01-02T15:04:05Z"
	clock := pub // управляемые часы: момент выпуска
	cases := []struct {
		name          string
		now           time.Time
		expires, publ string
		red           bool
	}{
		{"законный близнец: объявленные величины в момент выпуска", clock, expires, published, false},
		{"инъекция Expires = now (момент проверки)", clock, clock.Format(utc), published, true},
		{"инъекция: дальше года от выпуска", clock, pub.AddDate(1, 0, 1).Format(utc), published, true},
		{"близнец: ровно год от выпуска", clock, pub.AddDate(1, 0, 0).Format(utc), published, false},
		{"инъекция: местное время без зоны", clock, pub.AddDate(0, 6, 0).Format("2006-01-02T15:04:05"), published, true},
		{"инъекция: незакавыченная дата (метка времени YAML)", clock, asDeclaredString(pub.AddDate(0, 6, 0)), published, true},
		{"инъекция: истечения нет", clock, asDeclaredString(nil), published, true},
		{"инъекция: даты выпуска нет", clock, expires, asDeclaredString(nil), true},
		{"инъекция: часы дошли до запаса перед истечением", exp.Add(-securityTxtRenewMargin), expires, published, true},
		{"близнец: за сутки до запаса", exp.Add(-securityTxtRenewMargin - 24*time.Hour), expires, published, false},
	}
	for _, c := range cases {
		got := judgeSecurityTxt(c.now, c.expires, c.publ)
		if (len(got) > 0) != c.red {
			t.Errorf("%s: ждали красный=%t, получили находок %d: %v", c.name, c.red, len(got), got)
		}
	}

	raw, err := os.ReadFile(securityTxtTemplate)
	if err != nil {
		t.Fatalf("шаблон %s не читается: %v", securityTxtTemplate, err)
	}
	src := string(raw)
	if got := append(judgeSecurityTxtTemplate(src), judgeSecurityTxtTemplateReadsTheDeclaration(src)...); len(got) != 0 {
		t.Errorf("законный близнец (шаблон дерева) дал находки: %v", got)
	}
	injected := securityTxtExpiresLine.ReplaceAllString(src, `    Expires: {{ now | date "2006-01-02T15:04:05Z" }}`)
	if injected == src {
		t.Fatal("в шаблоне нет строки Expires объявленной формы — инъекции не во что встать")
	}
	if got := judgeSecurityTxtTemplate(injected); len(got) == 0 {
		t.Error("инъекция `now` в шаблон не дала находки о часах — проба шаблона упасть не может")
	}
	if got := judgeSecurityTxtTemplateReadsTheDeclaration(injected); len(got) == 0 {
		t.Error("инъекция `now` в шаблон не дала находки о выводе объявленной величины")
	}
}
