// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// console_mail_says_sent_never_delivered_injection_test.go — доказательство, что
// гейт MAIL-44 СПОСОБЕН упасть, СПОСОБЕН смолчать и знает ОБЕ законные формы
// записи пользовательского текста.
//
// Вход подаётся СТРОКОЙ: доказательство, трогающее дерево, испортило бы чужую
// рабочую копию (`multi-agent-flow.md` §13), а доказательство на копии разбора
// говорило бы о копии, а не о том, что исполняется.
//
// ПО ПРОБЕ НА ФОРМУ (`testing.md` §«Гейт на класс», п. 7): форма, распознавателю
// неизвестная, не даёт ни красного, ни зелёного — она МОЛЧИТ, и всё записанное в
// ней оказывается вне наблюдения. У каждой формы стоит ЗАКОННЫЙ БЛИЗНЕЦ: без
// него гейт ловил бы форму записи, а не утверждение, и первый же ложный срабат
// его отключил бы.
//
// Пара RED → GREEN на ЖИВОМ дереве снята отдельно и в доказательство не входит:
// она трогает файл консоли, а такой инъекции здесь не место (см. абзац выше).
package deploy_test

import (
	"strings"
	"testing"
)

// claimsIn — что распознаватель нашёл в одном исходнике. Утверждения считаются
// ПОРОЗНЬ: одно число скрыло бы ровно тот случай, ради которого гейт заведён.
func claimsIn(src string) (delivery, sending, aboutMail, texts int) {
	for _, ut := range consoleUserTexts(src) {
		texts++
		k := classifyMailText(ut.text)
		if !k.aboutMail {
			continue
		}
		aboutMail++
		if k.delivery {
			delivery++
		}
		if k.sending {
			sending++
		}
	}
	return delivery, sending, aboutMail, texts
}

// ─────────────────────────────────────────────────────────────────────────────
// ФОРМА A — строковый литерал.
const srcMailClaimLiteral = `export function InviteUserPage() {
  const onOk = () => {
    toast.success("Приглашение доставлено");
  };
}`

// ФОРМА B — текст разметки.
const srcMailClaimMarkup = `export function Hint() {
  return (
    <Typography.Text>
      Письмо доставлено адресату.
    </Typography.Text>
  );
}`

// ФОРМА B, разорванная ПРОСТЫМ тегом: без склейки ни в одном отрезке нет обоих
// слов сразу, и утверждение уехало бы из-под наблюдения целым.
const srcMailClaimMarkupSplitByInlineTag = `export function Hint() {
  return <Text>Письмо <b>доставлено</b> адресату</Text>;
}`

// ФОРМА B со ЗНАЧЕНИЕМ внутри фразы: подстановка — не текст, и оставить её
// значило бы отбросить весь отрезок как «похожий на код».
const srcMailClaimMarkupWithInterpolation = `export function Hint({ email }: Props) {
  return <Text>Письмо на {email} доставлено</Text>;
}`

// ФОРМА B, разорванная тегом с АТРИБУТОМ-выражением: скобки оформления простоты
// не отменяют.
const srcMailClaimMarkupSplitByStyledTag = `export function Hint() {
  return <Text>Письмо <span style={{ fontSize: 12 }}>доставлено</span></Text>;
}`

func TestMailDeliveryClaimRecognizerKnowsEveryLawfulForm(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"литерал", srcMailClaimLiteral},
		{"разметка", srcMailClaimMarkup},
		{"разметка, разорванная простым тегом", srcMailClaimMarkupSplitByInlineTag},
		{"разметка с подстановкой", srcMailClaimMarkupWithInterpolation},
		{"разметка, разорванная тегом с атрибутом-выражением", srcMailClaimMarkupSplitByStyledTag},
	} {
		t.Run(tc.name, func(t *testing.T) {
			delivery, _, aboutMail, texts := claimsIn(tc.src)
			if texts == 0 {
				t.Fatalf("распознаватель не нашёл НИ ОДНОГО текста — предпосылка пробы не создана")
			}
			if aboutMail == 0 {
				t.Fatalf("текст о письме не опознан (осмотрено текстов %d) — "+
					"утверждение уехало бы из-под наблюдения целым", texts)
			}
			if delivery != 1 {
				t.Errorf("утверждений о доставке %d, ожидалось 1 (осмотрено текстов %d, о письме %d)",
					delivery, texts, aboutMail)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ЗАКОННЫЕ БЛИЗНЕЦЫ. Каждый обязан МОЛЧАТЬ.

// Комментарий, ЦИТИРУЮЩИЙ запрещённую формулировку. Гейт по подстроке краснел бы
// на собственном объяснении — и его сняли бы как непонятный.
const srcTwinCommentQuotesTheClaim = `// Здесь стояло «Письмо доставлено» — снято: продукт видит сдачу ретранслятору.
/* И в блочном комментарии тоже: "Приглашение доставлено" — это разбор, а не текст. */
export const label = "Пригласить";`

// Доставка НЕ письма: предмет гейта — утверждение о ПИСЬМЕ, а не слово.
const srcTwinDeliveryOfSomethingElse = `export const status = {
  image: "Образ доставлен в зону",
  volume: <Text>Том доставлен</Text>,
};`

// Письмо БЕЗ утверждения: подпись кнопки ничего не обещает.
const srcTwinMailWithoutAnyClaim = `export const labels = {
  submit: "Отправить приглашение",
  hint: <Text>Приглашение придёт на указанную почту</Text>,
};`

// Обычный код с оператором сравнения: между `>` и `<` стоит выражение, а не
// текст. Без этого близнеца перепись раздувалась бы кодом, и «осмотрено» перестало
// бы что-либо значить.
const srcTwinPlainCodeIsNotMarkup = `export function pick(a: number, b: number) {
  const bigger = a > b ? a : b;
  return bigger < 100;
}`

func TestMailDeliveryClaimRecognizerStaysSilentOnLawfulTwins(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"комментарий цитирует запрещённое", srcTwinCommentQuotesTheClaim},
		{"доставлено НЕ письмо", srcTwinDeliveryOfSomethingElse},
		{"письмо без утверждения", srcTwinMailWithoutAnyClaim},
		{"сравнение — не разметка", srcTwinPlainCodeIsNotMarkup},
	} {
		t.Run(tc.name, func(t *testing.T) {
			delivery, _, _, _ := claimsIn(tc.src)
			if delivery != 0 {
				t.Errorf("законный близнец назван находкой: утверждений о доставке %d, ожидалось 0",
					delivery)
			}
		})
	}
}

// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ ГЕЙТА: утверждение об ОТПРАВКЕ распознаётся. Без него
// половина «отрицание не зеленеет на пустом экране» не сработала бы никогда —
// она сама была бы вакуумной.
func TestMailSendingClaimIsRecognisedInBothForms(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"литерал", `toast.success("Приглашение отправлено");`},
		{"разметка", `<Text>Письмо отправлено на указанный адрес</Text>;`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			delivery, sending, aboutMail, _ := claimsIn(tc.src)
			if sending != 1 {
				t.Errorf("утверждений об отправке %d, ожидалось 1 (о письме %d)", sending, aboutMail)
			}
			if delivery != 0 {
				t.Errorf("утверждение об ОТПРАВКЕ засчитано как ДОСТАВКА — %d", delivery)
			}
		})
	}
}

// ПРЕДПОСЫЛКА ГЕЙТА: пустой обход обязан быть отличим от «ноль находок».
//
// Проба утверждает свойство РАСПОЗНАВАТЕЛЯ, на котором стоит отказ гейта: на
// исходнике без текстов он даёт ноль, и гейт на таком дереве объявляет вердикт
// недействительным, а не зелёным.
func TestMailTextRecognizerYieldsNothingOnASourceWithoutTexts(t *testing.T) {
	src := `export function add(a: number, b: number) {
  // складывает
  return a + b;
}`
	if _, _, _, texts := claimsIn(src); texts != 0 {
		t.Errorf("на исходнике без пользовательских текстов распознаватель дал %d — "+
			"перепись «осмотрено» перестала бы отличать код от текста", texts)
	}
}

// Разделитель-БЛОК склейку рвёт: две соседние фразы не должны слипаться в одну,
// иначе гейт нашёл бы утверждение, которого никто не писал — почтовое слово из
// первой фразы и слово доставки из второй.
func TestMailTextRecognizerDoesNotGlueTwoNeighbouringParagraphs(t *testing.T) {
	src := `export function Hint() {
  return (
    <div>
      <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginBottom: 0 }}>
        Приглашение
      </Typography.Paragraph>
      <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginBottom: 0 }}>
        доставлено
      </Typography.Paragraph>
    </div>
  );
}`
	if delivery, _, _, _ := claimsIn(src); delivery != 0 {
		t.Errorf("две соседние фразы слиплись в одну: утверждений о доставке %d, ожидалось 0 — "+
			"находка, которой никто не писал, отключила бы гейт первой же", delivery)
	}
}

// Перепись форм ПОРОЗНЬ: расширение распознавателя обязано менять осмотренное, и
// одно число скрыло бы ровно тот случай, ради которого форма добавлена.
func TestMailTextCensusCountsBothFormsSeparately(t *testing.T) {
	src := `export const a = "Приглашение отправлено";
export const b = <Text>Письмо ушло</Text>;`
	var literals, markup int
	for _, ut := range consoleUserTexts(src) {
		switch ut.form {
		case mailTextFormLiteral:
			literals++
		case mailTextFormMarkup:
			markup++
		}
	}
	if literals == 0 || markup == 0 {
		t.Errorf("формы не считаются порознь: литералов %d, разметки %d — "+
			"одно число скрыло бы форму, ушедшую из наблюдения", literals, markup)
	}
}

// Координата находки называется: находка без места посылает читателя искать
// вслепую (`testing.md` §«Переустройство проверки», диагностика — часть свойства).
func TestMailDeliveryClaimCarriesItsLine(t *testing.T) {
	src := strings.Join([]string{
		`export const a = "первая строка";`,
		`export const b = "вторая строка";`,
		`export const c = "Приглашение доставлено";`,
	}, "\n")
	for _, ut := range consoleUserTexts(src) {
		if classifyMailText(ut.text).delivery {
			if ut.line != 3 {
				t.Errorf("координата находки %d, ожидалась 3", ut.line)
			}
			return
		}
	}
	t.Fatal("находка не найдена вовсе — предпосылка пробы не создана")
}
