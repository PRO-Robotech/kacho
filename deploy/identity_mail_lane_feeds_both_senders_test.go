// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_mail_lane_feeds_both_senders_test.go — MAIL-48 приёмки ID-MAIL-1.
//
// ПРЕДМЕТ. Узел, адрес отправителя и удостоверение объявляются ОДНАЖДЫ
// (решение Р23) — узлом `global.kacho.identity.smtp`, — и отправитель берёт их
// оттуда. Прежде отправителей было два: почтовый процесс внешнего поставщика
// личности (раздел `courier.smtp` его настроек) и наш отправитель, и гейт
// утверждал, что путь значений у двух читателей один. Настройки поставщика
// подчарт службы больше не производит (kacho#2818), и отправитель остался один
// — наш, настройка `invite-mail`, которую рендерит подчарт службы. Утверждение
// стало безусловным: раздел нашего отправителя питается РОВНО узлом
// `global.kacho.identity.smtp`. Выкинуть половину пары и оставить «сошлось с
// самим собой» было бы ослаблением; здесь судится узел, названный решением.
//
// ЗАЧЕМ. Отправитель, питающийся другим узлом, продолжает отправлять исправно —
// просто не туда и не от того имени, что объявлено, и заметить это неоткуда:
// отказа нет, красного нет. Сигнал появляется только у адресата, которого мы
// не спрашиваем.
//
// ЧЕМ ЭТОТ ГЕЙТ ОТЛИЧАЕТСЯ ОТ СОСЕДЕЙ — граница названа, чтобы не завелось
// дублирование:
//
//   - `identity_mail_lane_single_declaration_test.go` (MAIL-54) судит
//     ЕДИНСТВЕННОСТЬ объявления полосы; он не спрашивает, чем раздел питается.
//
// ЧТО ЗДЕСЬ НЕ СУДИТСЯ. Якорь доверия к сертификату почтового узла — отдельная
// пара того же узла (`trustAnchorSecret`); решение Р23 называет три величины —
// узел, адрес отправителя и удостоверение, — и гейт судит ровно их питание.
//
// Читаются ОБЪЯВЛЕНИЯ, а не рендер: ни helm, ни кластер не нужны, поэтому
// проверка не умеет пропускаться.
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

// mailSenderConfigTemplate — шаблон, рендерящий настройку НАШЕГО отправителя.
const mailSenderConfigTemplate = "helm/umbrella/charts/kaname/templates/configmap.yaml"

// mailSenderSectionRe — начало раздела нашего отправителя. Привязано к началу
// строки: проза об этом же разделе несёт его имя посреди предложения, и образец
// без привязки читал бы собственное объяснение как объявление.
var mailSenderSectionRe = regexp.MustCompile(`(?m)^\s{4}invite-mail:\s*$`)

// mailLaneNode — узел значений, которым решение Р23 объявляет почтовую полосу.
const mailLaneNode = "global.kacho.identity.smtp"

// mailValuesRefRe — ссылка на узел значений. Скобки снимаются ДО совпадения:
// защищённая форма `(((.Values.global).kacho).identity).smtp` — законное
// написание того же пути, и предикат, её не знающий, увёл бы целый шаблон
// из-под наблюдения, не дав ни красного, ни зелёного.
var mailValuesRefRe = regexp.MustCompile(`\.Values((?:\.[A-Za-z0-9_]+)+)`)

// mailVarAssignRe — присваивание переменной шаблона. `:=` и только оно:
// переприсвоения в этом шаблоне нет намеренно.
var mailVarAssignRe = regexp.MustCompile(`\$([A-Za-z0-9_]+)\s*:=`)

// mailVarRefRe — употребление переменной шаблона.
var mailVarRefRe = regexp.MustCompile(`\$([A-Za-z0-9_]+)`)

// mailStripParens убирает скобки, оставляя путь ссылки целым.
func mailStripParens(s string) string {
	return strings.NewReplacer("(", "", ")", "").Replace(s)
}

// mailVarFeeds строит карту «переменная шаблона → пути значений, из которых она
// выведена», разрешая цепочки до неподвижной точки.
//
// Разрешение транзитивно НЕ ради красоты: раздел `invite-mail` берёт адрес узла
// не прямой ссылкой, а через цепочку переменных (адрес → схема → части → узел).
// Предикат, читающий только прямые ссылки, вывел бы питание из части величин —
// то есть путь, которого никто не объявлял.
func mailVarFeeds(body string) map[string]map[string]bool {
	direct := map[string]map[string]bool{}
	deps := map[string]map[string]bool{}

	for _, line := range strings.Split(body, "\n") {
		flat := mailStripParens(line)
		m := mailVarAssignRe.FindStringSubmatchIndex(flat)
		if m == nil {
			continue
		}
		name := flat[m[2]:m[3]]
		rhs := flat[m[1]:]
		if direct[name] == nil {
			direct[name] = map[string]bool{}
			deps[name] = map[string]bool{}
		}
		for _, v := range mailValuesRefRe.FindAllStringSubmatch(rhs, -1) {
			direct[name][strings.TrimPrefix(v[1], ".")] = true
		}
		for _, v := range mailVarRefRe.FindAllStringSubmatch(rhs, -1) {
			if v[1] != name {
				deps[name][v[1]] = true
			}
		}
	}

	// Неподвижная точка. Число проходов ограничено числом переменных: цикл в
	// присваиваниях `:=` невыразим, но ограничитель стоит, чтобы дефект формы
	// давал отказ, а не вечный прогон.
	for pass := 0; pass <= len(direct); pass++ {
		grew := false
		for name, ds := range deps {
			for d := range ds {
				for p := range direct[d] {
					if !direct[name][p] {
						direct[name][p] = true
						grew = true
					}
				}
			}
		}
		if !grew {
			break
		}
	}
	return direct
}

// mailSectionBody вырезает тело раздела: от строки-заголовка до первой строки
// той же либо меньшей глубины, не являющейся комментарием и не пустой.
func mailSectionBody(body string, at int) string {
	lines := strings.Split(body[at:], "\n")
	head := lines[0]
	indent := len(head) - len(strings.TrimLeft(head, " "))
	out := []string{head}
	for _, l := range lines[1:] {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "{{/*") {
			out = append(out, l)
			continue
		}
		if len(l)-len(strings.TrimLeft(l, " ")) <= indent {
			break
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

// mailFeedSide — прочитанное об ОДНОМ читателе полосы.
type mailFeedSide struct {
	// File — шаблон, рендерящий раздел.
	File string
	// Found — найден ли раздел вообще. Ложь есть НАХОДКА, а не сорванная
	// предпосылка: раздел, который не рендерит ни один шаблон, питается ничем,
	// и это ровно тот дефект, ради которого гейт заведён.
	Found bool
	// Paths — пути значений, питающие раздел.
	Paths []string
	// Refs — ссылок прочитано (объём осмотренного).
	Refs int
	// Lines — строк шаблона прочитано (предпосылка: пустой файл молчит так же,
	// как согласный).
	Lines int
}

// Feed — общий узел значений этого читателя.
func (s mailFeedSide) Feed() string { return mailLongestCommonPrefix(s.Paths) }

// mailFeedPathsOf — пути значений, питающие раздел, и объём осмотренного.
//
// Корень параметризован затем, чтобы доказательство способности гейта упасть
// шло по КОПИИ в t.TempDir(), а не по рабочему дереву: состояние, которого
// проверка не заводила, она не трогает.
func mailFeedPathsOf(t *testing.T, root, file string, sec *regexp.Regexp) mailFeedSide {
	t.Helper()
	out := mailFeedSide{File: file}
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file)))
	if err != nil {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: шаблон %s не читается: %v", file, err)
	}
	body := string(raw)
	out.Lines = strings.Count(body, "\n") + 1
	if out.Lines < mailTemplateLineFloor {
		t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: шаблон %s прочитан %d строками при пороге %d — "+
			"он уехал либо опустел, и молчание гейта ничего не значит",
			file, out.Lines, mailTemplateLineFloor)
	}

	loc := sec.FindStringIndex(body)
	if loc == nil {
		return out
	}
	out.Found = true
	section := mailSectionBody(body, loc[0])
	// ОБЛАСТЬ ВИДИМОСТИ ПЕРЕМЕННОЙ — ЕЁ ОПРЕДЕЛЕНИЕ ШАБЛОНА, а не файл. Одно имя
	// может жить в двух определениях одного файла независимо, и карта,
	// построенная по файлу целиком, СЛИЛА БЫ их питание: раздел объявлялся бы
	// питающимся из узла, которого он не видит.
	feeds := mailVarFeeds(body[mailDefineStartBefore(body, loc[0]):loc[0]])

	seen := map[string]bool{}
	flat := mailStripParens(section)
	for _, v := range mailValuesRefRe.FindAllStringSubmatch(flat, -1) {
		out.Refs++
		seen[strings.TrimPrefix(v[1], ".")] = true
	}
	for _, v := range mailVarRefRe.FindAllStringSubmatch(flat, -1) {
		for p := range feeds[v[1]] {
			out.Refs++
			seen[p] = true
		}
	}
	for p := range seen {
		out.Paths = append(out.Paths, p)
	}
	sort.Strings(out.Paths)
	return out
}

// mailDefineStartBefore — начало определения шаблона, внутри которого лежит
// позиция at. Ноль, если определений до неё нет (обычный файл манифеста).
func mailDefineStartBefore(body string, at int) int {
	idx := strings.LastIndex(body[:at], "{{- define ")
	if idx < 0 {
		idx = strings.LastIndex(body[:at], "{{ define ")
	}
	if idx < 0 {
		return 0
	}
	return idx
}

// mailTemplateLineFloor — порог непустоты шаблона. Величина заведомо ниже
// шаблона (сотни строк) и служит одному: отличить «раздел не найден» от
// «файла больше нет».
const mailTemplateLineFloor = 20

// mailLongestCommonPrefix — общий узел значений набора путей, по СЕГМЕНТАМ.
func mailLongestCommonPrefix(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	pref := strings.Split(paths[0], ".")
	for _, p := range paths[1:] {
		seg := strings.Split(p, ".")
		n := len(pref)
		if len(seg) < n {
			n = len(seg)
		}
		i := 0
		for i < n && pref[i] == seg[i] {
			i++
		}
		pref = pref[:i]
	}
	return strings.Join(pref, ".")
}

// mailLaneFeedFindings — вердикт над НАЗВАННЫМ деревом.
func mailLaneFeedFindings(t *testing.T, root string) []string {
	t.Helper()
	sender := mailFeedPathsOf(t, root, mailSenderConfigTemplate, mailSenderSectionRe)

	t.Logf("перепись: мест питания настройки нашего отправителя прочитано %d (строк шаблона %d) · "+
		"питание ← %s (пути: %v)", sender.Refs, sender.Lines, mailFeedOrNone(sender.Feed()), sender.Paths)

	switch {
	case !sender.Found:
		return []string{fmt.Sprintf(
			"наш отправитель: раздел `invite-mail` не рендерится шаблоном %s ВОВСЕ — питания у "+
				"него нет ни одного. Отправитель без объявленной полосы не отправляет ничего, и "+
				"отказ его неотличим от «почты на этой установке не бывает»", sender.File)}
	case sender.Refs == 0:
		return []string{fmt.Sprintf(
			"наш отправитель: раздел `invite-mail` в %s не питается ни одной ссылкой на значения — "+
				"величины вписаны в шаблон литералом, то есть объявлены ВТОРЫМ местом", sender.File)}
	case sender.Feed() != mailLaneNode:
		return []string{fmt.Sprintf(
			"путь питания почтовой полосы НЕ тот, что объявлен решением Р23: наш отправитель "+
				"(%s, раздел `invite-mail`) ← %s, пути %v, а полоса объявлена узлом `%s`. "+
				"Отправитель, питающийся другим узлом, отправляет исправно — не туда и не от того "+
				"имени, и сигнала нет ни одного",
			sender.File, mailFeedOrNone(sender.Feed()), sender.Paths, mailLaneNode)}
	}
	return nil
}

// TestMAIL48BothMailSendersAreFedByOneDeclaration — сам гейт.
//
// Что делать, если он сработал, — исходов два, третьего нет: свести питание
// отправителя к объявленному узлу ЛИБО пересмотреть решение Р23 приёмкой.
func TestMAIL48BothMailSendersAreFedByOneDeclaration(t *testing.T) {
	t.Parallel()
	for _, f := range mailLaneFeedFindings(t, ".") {
		t.Error(f)
	}
}

func mailFeedOrNone(s string) string {
	if s == "" {
		return "<общего узла нет: ссылки расходятся уже на первом сегменте>"
	}
	return fmt.Sprintf("`%s`", s)
}
