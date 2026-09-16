// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_mail_lane_feeds_both_senders_test.go — MAIL-48 приёмки ID-MAIL-1.
//
// ПРЕДМЕТ. Узел, адрес отправителя и удостоверение объявляются ОДНАЖДЫ
// (решение Р23), и оба отправителя берут их оттуда: почтовый процесс
// поставщика личности — разделом `courier.smtp` нашей конфигурации личности,
// наш отправитель письма приглашения — настройкой, которую ему рендерит
// подчарт службы. Гейт утверждает, что ПУТЬ ЗНАЧЕНИЙ у этих двух мест ОДИН И
// ТОТ ЖЕ.
//
// ЗАЧЕМ. Два читателя одной величины расходятся МОЛЧА. Каждое письмо по
// отдельности продолжает уходить исправно — просто письма разных видов уезжают
// в разные места, и заметить это неоткуда: отказа нет, красного нет, счётчик
// отправки у каждого свой и оба зелёные. Сигнал появляется только у адресата,
// которого мы не спрашиваем.
//
// ЧЕМ ЭТОТ ГЕЙТ ОТЛИЧАЕТСЯ ОТ СОСЕДЕЙ — граница названа, чтобы не завелось
// дублирование:
//
//   - `identity_mail_lane_single_declaration_test.go` (MAIL-54) судит
//     ЕДИНСТВЕННОСТЬ объявления полосы: сколько мест фиксируют СОСТАВ раздела
//     `courier`. Он не спрашивает, чем эти места ПИТАЮТСЯ;
//   - `identity_delivery_flows_declared_once_test.go` (MAIL-18) судит ПРОФИЛЬ,
//     заводящий второе мнение о потоке доставки;
//   - `identity_courier_reads_what_it_mounts_test.go` судит, что почтовый и
//     основной процессы поставщика читают ОДИН НАБОР ФАЙЛОВ.
//
// Ни один не задаёт вопроса «из какого узла значений кормится настройка
// НАШЕГО отправителя» — его задаёт только этот.
//
// ЧТО ЗДЕСЬ НЕ СУДИТСЯ. Якорь доверия к сертификату почтового узла частью
// объявления полосы не является — ни у одного из двух читателей: у поставщика
// он приезжает переменной окружения рабочего объекта, у нас пусто означает
// системные корни. Решение Р23 называет три величины — узел, адрес отправителя
// и удостоверение, — и гейт судит ровно их питание.
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

// mailCourierSectionRe / mailSenderSectionRe — начало раздела у каждого из двух
// читателей. Привязано к началу строки: проза об этих же разделах несёт их
// имена посреди предложения, и образец без привязки читал бы собственное
// объяснение как объявление.
var (
	mailCourierSectionRe = regexp.MustCompile(`(?m)^courier:\s*$`)
	mailSenderSectionRe  = regexp.MustCompile(`(?m)^\s{4}invite-mail:\s*$`)
)

// mailValuesRefRe — ссылка на узел значений. Скобки снимаются ДО совпадения:
// защищённая форма `(((.Values.global).kacho).identity).smtp` — законное
// написание того же пути, и предикат, её не знающий, увёл бы целый шаблон
// из-под наблюдения, не дав ни красного, ни зелёного.
var mailValuesRefRe = regexp.MustCompile(`\.Values((?:\.[A-Za-z0-9_]+)+)`)

// mailVarAssignRe — присваивание переменной шаблона. `:=` и только оно:
// переприсвоения в этих шаблонах нет намеренно (о том же говорит разбор нашей
// стороны в identity_replaced_lists_are_decided_test.go).
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
// Разрешение транзитивно НЕ ради красоты: раздел `courier.smtp` берёт адрес
// узла не прямой ссылкой, а через четыре переменные подряд (адрес → схема →
// части → сращённый адрес). Предикат, читающий только прямые ссылки, объявил бы
// у этого раздела питание из двух величин вместо трёх — то есть вывел бы путь,
// которого никто не объявлял.
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
	// живёт в двух определениях одного файла независимо (в шаблоне личности
	// `$mailCred` заведён дважды — в конфигурации и в рабочем объекте), и карта,
	// построенная по файлу целиком, СЛИЛА БЫ их питание: раздел объявлялся бы
	// питающимся из двух узлов, один из которых он не видит. Измерено на
	// инъекции: сторона поставщика показывала лишний путь, которого в её
	// определении нет.
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
// любого из двух (оба — сотни строк) и служит одному: отличить «раздел не
// найден» от «файла больше нет».
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

// mailLaneFeedFindings — вердикт по обоим читателям над НАЗВАННЫМ деревом.
func mailLaneFeedFindings(t *testing.T, root string) []string {
	t.Helper()
	courier := mailFeedPathsOf(t, root, identityConfigTemplate, mailCourierSectionRe)
	sender := mailFeedPathsOf(t, root, mailSenderConfigTemplate, mailSenderSectionRe)

	t.Logf("перепись: мест питания раздела `courier.smtp` прочитано %d (строк шаблона %d) · "+
		"мест питания настройки нашего отправителя прочитано %d (строк шаблона %d)",
		courier.Refs, courier.Lines, sender.Refs, sender.Lines)
	t.Logf("питание: поставщик ← %s (пути: %v); наш отправитель ← %s (пути: %v)",
		mailFeedOrNone(courier.Feed()), courier.Paths,
		mailFeedOrNone(sender.Feed()), sender.Paths)

	var findings []string
	for _, side := range []struct {
		who  string
		s    mailFeedSide
		what string
	}{
		{"почтовый процесс поставщика", courier, "раздел `courier.smtp`"},
		{"наш отправитель письма приглашения", sender, "раздел `invite-mail`"},
	} {
		switch {
		case !side.s.Found:
			findings = append(findings, fmt.Sprintf(
				"%s: %s не рендерится шаблоном %s ВОВСЕ — питания у него нет ни одного. "+
					"Отправитель без объявленной полосы не отправляет ничего, и отказ его "+
					"неотличим от «почты на этой установке не бывает»",
				side.who, side.what, side.s.File))
		case side.s.Refs == 0:
			findings = append(findings, fmt.Sprintf(
				"%s: %s в %s не питается ни одной ссылкой на значения — величины вписаны "+
					"в шаблон литералом, то есть объявлены ВТОРЫМ местом",
				side.who, side.what, side.s.File))
		}
	}
	if len(findings) > 0 {
		return findings
	}

	if courier.Feed() != sender.Feed() {
		findings = append(findings, fmt.Sprintf(
			"пути питания почтовой полосы РАЗЛИЧНЫ и обе координаты названы:\n"+
				"  поставщик (%s, раздел `courier.smtp`) ← %s, пути %v\n"+
				"  наш отправитель (%s, раздел `invite-mail`) ← %s, пути %v\n"+
				"Решение Р23 объявляет узел, адрес отправителя и удостоверение ОДНАЖДЫ, и оба "+
				"читателя берут их оттуда. Два читателя одной величины, разошедшиеся молча, "+
				"дают письма разных видов, уехавшие в разные места, — при том что каждое по "+
				"отдельности уходит исправно, и сигнала нет ни одного.",
			identityConfigTemplate, mailFeedOrNone(courier.Feed()), courier.Paths,
			mailSenderConfigTemplate, mailFeedOrNone(sender.Feed()), sender.Paths))
	}
	return findings
}

// TestMAIL48BothMailSendersAreFedByOneDeclaration — сам гейт.
//
// Что делать, если он сработал, — исходов два, третьего нет: свести питание
// обоих читателей к одному узлу значений ЛИБО пересмотреть решение Р23
// приёмкой. Завести второй узел «пока так» исходом не является: расхождение
// двух читателей не даёт ни отказа, ни красного — письма просто уезжают в
// разные места.
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
