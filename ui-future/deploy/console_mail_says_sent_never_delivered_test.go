// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// console_mail_says_sent_never_delivered_test.go — КОНСОЛЬ ГОВОРИТ ОБ ОТПРАВКЕ
// ПИСЬМА И НИГДЕ НЕ УТВЕРЖДАЕТ, ЧТО ОНО ПОЛУЧЕНО.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ (#1776, сценарий MAIL-44 приёмки ID-MAIL-1)
//
// Продукт видит СДАЧУ письма ретранслятору, а не получение его человеком.
// Дальше ретранслятора наш вердикт не идёт ни одним путём: отправитель
// классифицирует исход одной попытки разговора с узлом (сдано · временный отказ ·
// отказ по настройке), и клетка `sent` означает СДАНО, а не ДОШЛО.
//
// Сказать в консоли «доставлено» значит объявить возможность, которой у продукта
// нет: он этого не знает и узнать не может. Арендатор, прочитавший «доставлено»,
// перестанет искать письмо в спаме и придёт с вопросом, на который у поддержки
// нет ответа, — потому что ответ живёт у чужого ретранслятора.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ГЕЙТ, А НЕ ОБЗОР ИЗМЕНЕНИЯ
//
// Половина MAIL-44 со стороны ПРОИЗВОДИТЕЛЯ уже держится
// (`services/iam/internal/clients/invite_mail_test.go`,
// `Test_InviteMailBody_SaysSentNeverDelivered`): тело письма и словарь исходов
// проверены. Половина КОНСОЛИ не держалась ничем, и её собственный комментарий
// это говорил — «половина консоли — предмет своей полосы».
//
// Слово «доставлено» приезжает в интерфейс не решением, а по дороге: подпись
// кнопки, текст уведомления, колонка списка. Оно выглядит естественным именно
// потому, что человек ждёт письма, — и ни одно утверждение о содержимом экрана
// на нём не краснеет: строка есть, она просто обещает больше, чем продукт знает.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ГЕЙТ ТРЕБУЕТ — ОБЕ ПОЛОВИНЫ, И ВТОРАЯ НЕ УКРАШЕНИЕ
//
//	отрицание   ни один пользовательский текст консоли не утверждает ДОСТАВКУ
//	            письма/приглашения;
//	положительный
//	контроль    хотя бы один текст утверждает ОТПРАВКУ.
//
// Без второй половины отрицание зеленело бы на консоли, которая о письме не
// говорит вообще ничего, — то есть на экране, где не написано ничего
// (`testing.md` §«Гейт на класс», п. 2 и §«Отрицание годится только в паре с
// положительным»).
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ГЕЙТ НЕ ТРЕБУЕТ, И ЭТО РЕШЕНИЕ, А НЕ ПРОПУСК
//
// ВРЕМЕНИ ОТПРАВКИ он не требует. Сценарий MAIL-44 называет его рядом с
// «отправлено», но величина до клиента не выходит — и это записано решением
// (`services/iam/docs/engineering/architecture/known-divergences.md`, §20: время
// сдачи письма ретранслятору есть ЖИВОСТЬ ОЧЕРЕДИ, а не факт для арендатора; у
// строки очереди три читателя на пути дренажа, а уборка доставленных снимает её,
// поэтому клиентское поле означало бы РАЗОМ «ещё не сдано» и «сдано и убрано»).
// Требовать здесь показ времени значило бы завести проверку, у которой нет
// производителя и быть не может.
//
// ─────────────────────────────────────────────────────────────────────────────
// ДВЕ ЗАКОННЫЕ ФОРМЫ ЗАПИСИ ТЕКСТА, И РАСПОЗНАВАТЕЛЬ ЗНАЕТ ОБЕ
//
//	литерал    toast.success("Приглашение отправлено")
//	разметка   <Text>Приглашение отправлено</Text>
//
// Форма, распознавателю неизвестная, не даёт ни красного, ни зелёного — она
// МОЛЧИТ, и всё записанное в ней оказывается вне наблюдения (`testing.md`
// §«Гейт на класс», п. 7). Обе формы в этом дереве живые: замер на момент
// заведения — кириллический текст разметки встречается сотней с лишним мест.
//
// Отрезки разметки, разделённые ПРОСТЫМ тегом, склеиваются: «Письмо
// <b>доставлено</b>» иначе распалось бы на два текста, ни в одном из которых
// нет обоих слов сразу, и утверждение уехало бы из-под наблюдения целым.
//
// ─────────────────────────────────────────────────────────────────────────────
// КОММЕНТАРИИ НЕ СУДЯТСЯ — И ЭТО НЕСУЩЕЕ, А НЕ УДОБСТВО
//
// Разбор идёт по СОСТОЯНИЮ лексера: комментарий не даёт текста вовсе. Гейт по
// подстроке краснел бы на собственном объяснении и на разборе снятого дефекта в
// соседнем файле — и его сняли бы как непонятный (`testing.md` §«Гейт на класс»,
// п. 4). Законный близнец на это стоит в доказательстве.
//
// ─────────────────────────────────────────────────────────────────────────────
// ГРАНИЦЫ, НАЗВАННЫЕ ЧЕСТНО
//
//  1. Литерал распознаётся лексером и точен; разметка — ЭВРИСТИКА: отрезок между
//     тегами принимается за текст, если несёт букву и не несёт признаков кода
//     (`{`, `}`, `;`, `=`). Поэтому перепись разметки — оценка, а не точное
//     число, и она печатается отдельно от переписи литералов, а не сливается с
//     ней в одно число.
//  2. Обход — ТОЛЬКО прод-дерево консоли: пробы модулей, стенды `e2e/` и
//     оснастка `src/test/` исключены. Они не поверхность, которую видит
//     арендатор, а их фикстуры цитируют запрещённые формулировки по делу.
//  3. Гейт судит УТВЕРЖДЕНИЕ О ПИСЬМЕ, а не слово «доставлен» вообще: текст
//     обязан нести И почтовое слово, И слово доставки. «Образ доставлен в зону» —
//     не про письмо и находкой не является.
package deploy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// Формы записи пользовательского текста. Каждая доказана своей пробой в
// доказательстве способности упасть.
const (
	mailTextFormLiteral = "литерал"
	mailTextFormMarkup  = "разметка"
)

// mailSubjectWords — слова, по которым текст опознаётся как ПРО ПИСЬМО.
//
// Без этого условия гейт запрещал бы слово «доставлено» на всей поверхности
// консоли — то есть требовал бы от продукта не говорить о доставке образа, тома
// или чего угодно ещё. Предмет MAIL-44 уже: утверждение о ПИСЬМЕ.
var mailSubjectWords = []string{"письм", "приглашен", "почт", "mail", "invite", "invitation"}

// mailDeliveryClaimWords — слова, которыми утверждается ПОЛУЧЕНИЕ письма.
//
// Набор узкий намеренно. «Получено» в него не входит: оно живёт в консоли о
// токенах, ответах и списках, и запрет на него был бы запретом на слово, а не на
// утверждение.
var mailDeliveryClaimWords = []string{"доставлен", "delivered", "вручен"}

// mailSendingClaimWords — слова, которыми утверждается ОТПРАВКА. Ими держится
// положительный контроль.
var mailSendingClaimWords = []string{"отправлен", "выслан", " sent", "sent."}

// consoleTextsSkipPathParts — что НЕ является поверхностью арендатора.
var consoleTextsSkipPathParts = []string{
	string(filepath.Separator) + "e2e" + string(filepath.Separator),
	filepath.Join("src", "test") + string(filepath.Separator),
	filepath.Join("src", "__mocks__") + string(filepath.Separator),
}

// consoleUserText — один пользовательский текст с координатой и формой записи.
type consoleUserText struct {
	line int
	form string
	text string
}

// consoleUserTexts извлекает пользовательские тексты из исходника консоли.
//
// Функция ЧИСТАЯ намеренно: доказательство подаёт ей вход строкой. Инъекция,
// трогающая дерево, испортила бы чужую рабочую копию (`multi-agent-flow.md`
// §13), а инъекция на копии разбора говорила бы о копии, а не о том, что
// исполняется.
//
// растаскивать по функциям, между которыми пришлось бы возить позицию и строку.
//
//nolint:gocognit // лексер: состояния разбора дешевле держать в одном месте, чем
func consoleUserTexts(src string) []consoleUserText {
	rs := []rune(src)
	n := len(rs)
	line := 1

	var out []consoleUserText
	var run []rune
	runLine := 1
	mergeWithPrev := false
	// Текст разметки существует ТОЛЬКО между тегами. До первого тега его нет
	// вовсе — иначе исходник без разметки целиком уезжал бы в перепись «текстов
	// осмотрено», и число перестало бы отличать текст от кода.
	inMarkup := false

	// flushMarkup завершает отрезок разметки. simpleSeparatorAhead — ПРЕДВАРИТЕЛЬНОЕ
	// допущение о разделителе: простоту тега видно только после его разбора,
	// поэтому вызывающий уточняет решение сразу за вызовом.
	flushMarkup := func(simpleSeparatorAhead bool) {
		t := strings.TrimSpace(stripBraceGroups(string(run)))
		run = run[:0]
		switch {
		case t == "":
			// Пустой промежуток между тегами склейку не рвёт: «Письмо
			// <b>доставлено</b>» и «Письмо<b>доставлено</b>» суть одна фраза.
			mergeWithPrev = mergeWithPrev && simpleSeparatorAhead
		case !looksLikeMarkupText(t):
			mergeWithPrev = false
		case mergeWithPrev && len(out) > 0 && out[len(out)-1].form == mailTextFormMarkup:
			out[len(out)-1].text += " " + t
			mergeWithPrev = simpleSeparatorAhead
		default:
			out = append(out, consoleUserText{line: runLine, form: mailTextFormMarkup, text: t})
			mergeWithPrev = simpleSeparatorAhead
		}
	}

	for i := 0; i < n; i++ {
		c := rs[i]
		switch {
		case c == '\n':
			line++
			run = append(run, c)

		// Комментарий строки: текста не производит вовсе.
		case c == '/' && i+1 < n && rs[i+1] == '/':
			flushMarkup(false)
			for i < n && rs[i] != '\n' {
				i++
			}
			line++

		// Комментарий блока.
		case c == '/' && i+1 < n && rs[i+1] == '*':
			flushMarkup(false)
			i += 2
			for i+1 < n && !(rs[i] == '*' && rs[i+1] == '/') {
				if rs[i] == '\n' {
					line++
				}
				i++
			}
			i++

		// Строковый литерал — точная форма, разбирается лексером.
		case c == '"' || c == '\'' || c == '`':
			flushMarkup(false)
			quote := c
			startLine := line
			var lit []rune
			i++
			for i < n && rs[i] != quote {
				if rs[i] == '\\' && i+1 < n {
					lit = append(lit, rs[i+1])
					i += 2
					continue
				}
				if rs[i] == '\n' {
					line++
				}
				lit = append(lit, rs[i])
				i++
			}
			if t := strings.TrimSpace(string(lit)); t != "" {
				out = append(out, consoleUserText{
					line: startLine, form: mailTextFormLiteral, text: t,
				})
			}

		// Тег разметки: отрезок закрывается, разделитель оценивается ПОСЛЕ разбора.
		case c == '<':
			flushMarkup(true)
			var tag []rune
			i++
			for i < n && rs[i] != '>' {
				if rs[i] == '\n' {
					line++
				}
				tag = append(tag, rs[i])
				i++
			}
			mergeWithPrev = mergeWithPrev && isSimpleMarkupTag(string(tag))
			runLine = line
			inMarkup = true

		default:
			if !inMarkup {
				continue
			}
			if len(run) == 0 {
				runLine = line
			}
			run = append(run, c)
		}
	}
	if inMarkup {
		flushMarkup(false)
	}
	return out
}

// stripBraceGroups снимает выражения `{...}` из отрезка разметки.
//
// Значение — не текст, и оставить его значило бы отбросить весь отрезок как
// «похожий на код»: «Письмо {адрес} доставлено» ушло бы из-под наблюдения целым,
// оставаясь ровно тем утверждением, которое гейт ловит.
func stripBraceGroups(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch r {
		case '{':
			depth++
			b.WriteRune(' ')
		case '}':
			if depth > 0 {
				depth--
			}
			b.WriteRune(' ')
		default:
			if depth == 0 {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

// isSimpleMarkupTag — разделитель, через который отрезки склеиваются.
//
// Простой тег — оформление внутри одной фразы (`<b>`, `<span style={{…}}>`):
// текст по обе его стороны читается человеком как одно предложение. Длинный тег
// принят за границу БЛОКА и склейку рвёт — иначе две соседние фразы слиплись бы
// в одну, и гейт нашёл бы утверждение, которого никто не писал: почтовое слово
// из первой и слово доставки из второй.
//
// Скобки АТРИБУТА простоты не отменяют: `style={{ fontSize: 12 }}` — оформление,
// а не значение между текстами, поэтому парные группы снимаются до проверки.
// Длина считается по СЫРОМУ тегу: она и есть признак блока.
func isSimpleMarkupTag(tag string) bool {
	return len([]rune(tag)) <= 60 && !strings.ContainsAny(stripBraceGroups(tag), "{}")
}

// looksLikeMarkupText — эвристика формы разметки, и она названа эвристикой.
//
// Признаки кода (`;`, `=`) отбрасывают отрезок: между `>` и `<` в обычном коде
// стоит выражение, а не текст. Буква обязательна — иначе в перепись уехали бы
// запятые и скобки.
func looksLikeMarkupText(t string) bool {
	if strings.ContainsAny(t, ";=") {
		return false
	}
	for _, r := range t {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

// mailTextKind — что утверждает текст о письме.
type mailTextKind struct {
	aboutMail bool
	delivery  bool
	sending   bool
}

// classifyMailText — утверждение, а не слово.
func classifyMailText(text string) mailTextKind {
	low := strings.ToLower(text)
	k := mailTextKind{aboutMail: containsAnyWord(low, mailSubjectWords)}
	if !k.aboutMail {
		return k
	}
	k.delivery = containsAnyWord(low, mailDeliveryClaimWords)
	k.sending = containsAnyWord(low, mailSendingClaimWords)
	return k
}

func containsAnyWord(low string, words []string) bool {
	for _, w := range words {
		if strings.Contains(low, w) {
			return true
		}
	}
	return false
}

// consoleSurfaceFiles — прод-дерево консоли ПО СОСТАВУ ИНДЕКСА, а не обходом
// диска: вердикт обязан быть свойством коммита.
func consoleSurfaceFiles(t *testing.T, root string) []string {
	t.Helper()
	files, err := treecorpus.UnderWithSuffix(filepath.Join(root, "ui-future"), ".ts", ".tsx")
	if err != nil {
		t.Fatalf("состав дерева консоли: %v — без индекса вердикт недействителен", err)
	}
	var out []string
	for _, abs := range files {
		rel, rerr := filepath.Rel(root, abs)
		if rerr != nil {
			continue
		}
		base := filepath.Base(rel)
		if strings.HasSuffix(base, ".d.ts") ||
			strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") {
			continue
		}
		if !strings.Contains(rel, filepath.Join("src")+string(filepath.Separator)) {
			continue
		}
		skip := false
		for _, part := range consoleTextsSkipPathParts {
			if strings.Contains(rel, part) {
				skip = true
			}
		}
		if skip {
			continue
		}
		out = append(out, abs)
	}
	return out
}

// TestConsoleSaysSentAndNeverClaimsTheLetterWasDelivered — MAIL-44, половина
// консоли.
func TestConsoleSaysSentAndNeverClaimsTheLetterWasDelivered(t *testing.T) {
	root := repoRootFromTest(t)
	files := consoleSurfaceFiles(t, root)

	var literals, markup, aboutMail int
	var deliveryClaims, sendingClaims []string

	for _, abs := range files {
		body, err := os.ReadFile(abs) // #nosec G304 -- путь пришёл из индекса git этого дерева
		if err != nil {
			t.Fatalf("%s: %v", abs, err)
		}
		rel, _ := filepath.Rel(root, abs)
		for _, ut := range consoleUserTexts(string(body)) {
			if ut.form == mailTextFormLiteral {
				literals++
			} else {
				markup++
			}
			k := classifyMailText(ut.text)
			if !k.aboutMail {
				continue
			}
			aboutMail++
			where := rel + ":" + itoa(ut.line) + " (" + ut.form + ") " + shorten(ut.text)
			if k.delivery {
				deliveryClaims = append(deliveryClaims, where)
			}
			if k.sending {
				sendingClaims = append(sendingClaims, where)
			}
		}
	}

	// ПРОВЕРКА СОБСТВЕННОЙ ПРЕДПОСЫЛКИ: «ноль находок» обязано быть отличимо от
	// «ноль прочитанного».
	if len(files) == 0 {
		t.Fatalf("прочитано ноль файлов консоли под %s — обход беспредметен, "+
			"вердикт недействителен", filepath.Join(root, "ui-future"))
	}
	if literals+markup == 0 {
		t.Fatalf("файлов консоли прочитано %d, а пользовательских текстов не найдено НИ ОДНОГО — "+
			"распознаватель перестал видеть предмет; вердикт недействителен", len(files))
	}

	t.Logf("перепись: файлов консоли прочитано %d · текстов осмотрено %d "+
		"(литералов %d · разметки %d) · текстов о письме %d · "+
		"утверждений об ОТПРАВКЕ %d · утверждений о ДОСТАВКЕ %d",
		len(files), literals+markup, literals, markup, aboutMail,
		len(sendingClaims), len(deliveryClaims))

	if aboutMail == 0 {
		t.Fatalf("консоль не говорит о письме НИ ОДНИМ текстом — отрицание ниже зеленело бы "+
			"на экране, где не написано ничего (осмотрено текстов %d)", literals+markup)
	}

	// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ (MAIL-44): утверждение об отправке присутствует.
	if len(sendingClaims) == 0 {
		t.Errorf("ни один текст консоли не говорит об ОТПРАВКЕ письма при %d текстах о письме.\n\n"+
			"Арендатору, нажавшему «пригласить», продукт обязан сказать, что письмо ушло: "+
			"иначе он не знает, ждать ли его вовсе. И без этой половины запрет ниже "+
			"утверждает лишь то, что консоль о письме молчит.", aboutMail)
	}

	// ОТРИЦАНИЕ: продукт не знает о получении и не вправе его утверждать.
	if len(deliveryClaims) > 0 {
		t.Errorf("консоль утверждает ДОСТАВКУ письма — %d:\n\t%s\n\n"+
			"Продукт видит СДАЧУ письма ретранслятору и ничего дальше: клетка исхода "+
			"`sent` означает «сдано», а не «дошло». Сказать «доставлено» значит объявить "+
			"возможность, которой нет, — арендатор перестанет искать письмо и придёт с "+
			"вопросом, ответ на который живёт у чужого ретранслятора. Скажите об ОТПРАВКЕ.",
			len(deliveryClaims), strings.Join(deliveryClaims, "\n\t"))
	}
}

func shorten(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 80 {
		return string(r[:80]) + "…"
	}
	return s
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
