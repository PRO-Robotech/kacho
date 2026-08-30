// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// subscriptionstatedocs_injection_test.go — доказательство, что гейт «страница
// говорит про владельца то же, что делает его журнал» СПОСОБЕН упасть.
//
// Каждая ось прогоняется дважды: на внесённом дефекте (обязано найтись, и находка
// обязана называть владельца) и на ЗАКОННОМ БЛИЗНЕЦЕ той же формы (обязано
// смолчать). Односторонняя проба зеленела бы и на гейте, который краснеет всегда.
//
// Инъекция подаёт СИНТЕТИЧЕСКОЕ дерево и синтетическую страницу, а не правит
// настоящие: гейт в бою берёт состав у индекса git, и проба, пишущая в индекс
// запустившего её репозитория, портит чужое состояние. Судящие функции при этом
// те же самые — `subscriptionJournalStates`, `subscriptionOwnerRows`,
// `subscriptionStateFindings`.

// syntheticJournal кладёт в дерево объявление журнала одного сервиса.
//
// `packs` решает, доходит ли функция состояния до упаковки. `mentionsInComment`
// ставит имя упаковщика в КОММЕНТАРИЙ — законный близнец: объявления журналов,
// состояния не производящих, обсуждают упаковку прозой, и гейт, судящий
// подстроку, принял бы такое объявление за производящее.
func syntheticJournal(t *testing.T, root, service string, packs, mentionsInComment bool) {
	t.Helper()
	dir := filepath.Join(root, "services", service, "internal", "subscriptionjournal")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("синтетическое дерево: %v", err)
	}
	body := "package subscriptionjournal\n\n"
	if mentionsInComment {
		body += "// Состояние здесь не собирается: звать anypb.New было бы не из чего.\n"
	}
	body += "func state(r any) (any, int, error) {\n"
	if packs {
		body += "\treturn packed(anypb.New(r))\n"
	}
	body += "\treturn nil, 1, nil\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "journal.go"), []byte(body), 0o600); err != nil {
		t.Fatalf("синтетическое дерево: %v", err)
	}
}

// syntheticPage собирает страницу с таблицей владельцев из готовых клеток.
//
// Рядом ставится ВТОРАЯ таблица того же вида, что живёт на настоящей странице
// («кадры потока»): её первая ячейка тоже несёт `<code>`, и гейт, читающий все
// строки страницы подряд, принял бы имя кадра за владельца.
func syntheticPage(claims map[string]string, order []string) string {
	var b strings.Builder
	b.WriteString("# Подписка\n\nПроза.\n\n")
	b.WriteString(ownerTableHeading + " и видов\n\n<table>\n")
	b.WriteString("  <thead><tr><th><code>owner</code></th><th>Домен</th><th>Состояние</th></tr></thead>\n  <tbody>\n")
	for _, owner := range order {
		b.WriteString("    <tr>\n      <td><code>" + owner + "</code></td><td>Домен</td>\n")
		b.WriteString("      <td>" + claims[owner] + "</td>\n    </tr>\n")
	}
	b.WriteString("  </tbody>\n</table>\n\n## Кадры потока\n\n<table>\n  <tbody>\n")
	b.WriteString("    <tr><td><code>opened</code></td><td>первым</td><td>служебное сообщение</td></tr>\n")
	b.WriteString("  </tbody>\n</table>\n")
	return b.String()
}

const (
	// claimStateless — клетка владельца, состояния не производящего.
	claimStateless = "состояния нет ни у одного вида — приходит только оболочка события"
	// claimStateful — клетка владельца, состояние производящего. Она НАМЕРЕННО
	// содержит слова «состояния нет»: у производящего владельца их нет у снятия,
	// и он обязан это сказать. Гейт, ищущий отрицание, покраснел бы здесь на
	// верном тексте.
	claimStateful = "<code>state</code> типа <code>kacho.cloud.compute.v1.Instance</code> — " +
		"у создания и правки; у снятия состояния нет"
)

// judgeSynthetic прогоняет ТЕ ЖЕ судящие функции на синтетическом дереве.
func judgeSynthetic(t *testing.T, root, page string) []string {
	t.Helper()
	reports, filesRead, err := subscriptionJournalStates(root, syntheticLister)
	if err != nil {
		t.Fatalf("перепись журналов синтетического дерева: %v", err)
	}
	if filesRead == 0 {
		t.Fatal("синтетическое дерево не дало ни одного объявления журнала — инъекция " +
			"проверяла бы пустоту, а не свойство")
	}
	return subscriptionStateFindings(reports, subscriptionOwnerRows(page))
}

// TestSubscriptionStateDocsGateCatchesAPageThatDeniesAProducedState — ось 1:
// журнал состояние собирает, страница обещает обратное.
//
// Это ровно тот дефект, что приехал слиянием линии эпика и жил в стволе.
func TestSubscriptionStateDocsGateCatchesAPageThatDeniesAProducedState(t *testing.T) {
	root := t.TempDir()
	syntheticJournal(t, root, "vpc", true, false)

	defective := judgeSynthetic(t, root, syntheticPage(
		map[string]string{"vpc": claimStateless}, []string{"vpc"}))
	if len(defective) == 0 {
		t.Fatal("гейт смолчал на странице, отрицающей собираемое состояние, — он не " +
			"способен упасть на дефекте, ради которого заведён")
	}
	if !strings.Contains(defective[0], `"vpc"`) {
		t.Errorf("находка не называет владельца: %q", defective[0])
	}
	t.Logf("дефект найден: %s", defective[0])

	legit := judgeSynthetic(t, root, syntheticPage(
		map[string]string{"vpc": claimStateful}, []string{"vpc"}))
	if len(legit) != 0 {
		t.Errorf("гейт краснеет на ЗАКОННОМ близнеце (страница называет тип состояния, "+
			"журнал его собирает): %v", legit)
	}
}

// TestSubscriptionStateDocsGateCatchesAPagePromisingStateThatIsNotProduced —
// ось 2: журнал состояния не собирает, страница называет тип.
//
// Обратная сторона первой оси, и она про клиента дороже: он отберёт по меткам,
// которых не получал, и увидит пустой список вместо своих ресурсов.
func TestSubscriptionStateDocsGateCatchesAPagePromisingStateThatIsNotProduced(t *testing.T) {
	root := t.TempDir()
	syntheticJournal(t, root, "storage", false, false)

	defective := judgeSynthetic(t, root, syntheticPage(
		map[string]string{"storage": claimStateful}, []string{"storage"}))
	if len(defective) == 0 {
		t.Fatal("гейт смолчал на странице, обещающей непроизводимое состояние")
	}
	if !strings.Contains(defective[0], `"storage"`) {
		t.Errorf("находка не называет владельца: %q", defective[0])
	}
	t.Logf("дефект найден: %s", defective[0])

	legit := judgeSynthetic(t, root, syntheticPage(
		map[string]string{"storage": claimStateless}, []string{"storage"}))
	if len(legit) != 0 {
		t.Errorf("гейт краснеет на ЗАКОННОМ близнеце (страница состояния не обещает, "+
			"журнал его не собирает): %v", legit)
	}
}

// TestSubscriptionStateDocsGateJudgesTheNodeNotTheProse — ось 3: упоминание
// упаковщика в КОММЕНТАРИИ производством не является.
//
// Без этой оси гейт был бы сверкой по подстроке и краснел бы на объявлениях,
// которые ОБЪЯСНЯЮТ, почему состояния нет, — то есть на собственном объяснении.
func TestSubscriptionStateDocsGateJudgesTheNodeNotTheProse(t *testing.T) {
	root := t.TempDir()
	syntheticJournal(t, root, "nlb", false, true)

	findings := judgeSynthetic(t, root, syntheticPage(
		map[string]string{"loadbalancer": claimStateless}, []string{"loadbalancer"}))
	if len(findings) != 0 {
		t.Errorf("гейт принял имя упаковщика из КОММЕНТАРИЯ за производство состояния: %v",
			findings)
	}

	// Контроль в другую сторону: тот же комментарий рядом с настоящей упаковкой
	// — производство, и оно обязано найтись.
	root2 := t.TempDir()
	syntheticJournal(t, root2, "nlb", true, true)
	defective := judgeSynthetic(t, root2, syntheticPage(
		map[string]string{"loadbalancer": claimStateless}, []string{"loadbalancer"}))
	if len(defective) == 0 {
		t.Fatal("гейт смолчал на упаковке, стоящей рядом с комментарием, — он судит " +
			"не узел, а окружение")
	}
}

// TestSubscriptionStateDocsGateHoldsTheBijection — ось 4: журнал без строки и
// строка без журнала.
//
// Биекция — то, чем держится повторённая величина `subscriptionOwnerOfService`:
// без неё шестой владелец и переименование пятого прошли бы молча, оставив
// строку вне наблюдения.
func TestSubscriptionStateDocsGateHoldsTheBijection(t *testing.T) {
	root := t.TempDir()
	syntheticJournal(t, root, "compute", true, false)

	noRow := judgeSynthetic(t, root, syntheticPage(map[string]string{}, nil))
	if len(noRow) == 0 {
		t.Fatal("гейт смолчал на владельце, которого таблица не называет вовсе")
	}
	t.Logf("журнал без строки: %s", noRow[0])

	orphan := judgeSynthetic(t, root, syntheticPage(map[string]string{
		"compute": claimStateful,
		"geo":     claimStateless,
	}, []string{"compute", "geo"}))
	if len(orphan) == 0 {
		t.Fatal("гейт смолчал на строке, у которой в дереве нет журнала")
	}
	if !strings.Contains(orphan[0], `"geo"`) {
		t.Errorf("находка не называет осиротевшую строку: %q", orphan[0])
	}
	t.Logf("строка без журнала: %s", orphan[0])

	// Законный близнец обеих находок: биекция сходится — гейт молчит.
	legit := judgeSynthetic(t, root, syntheticPage(
		map[string]string{"compute": claimStateful}, []string{"compute"}))
	if len(legit) != 0 {
		t.Errorf("гейт краснеет на сошедшейся биекции: %v", legit)
	}
}

// TestSubscriptionStateDocsGateCatchesAServiceOutsideTheOwnerVocabulary — ось 5:
// журнал, которому в перечне гейта не назначено написание владельца.
//
// Без этой находки шестой владелец остался бы ВНЕ НАБЛЮДЕНИЯ молча: гейт не
// нашёл бы для него строки и не сказал бы об этом ничего.
func TestSubscriptionStateDocsGateCatchesAServiceOutsideTheOwnerVocabulary(t *testing.T) {
	root := t.TempDir()
	syntheticJournal(t, root, "geo", true, false)

	findings := judgeSynthetic(t, root, syntheticPage(
		map[string]string{"geo": claimStateful}, []string{"geo"}))
	if len(findings) == 0 {
		t.Fatal("гейт смолчал на журнале, которому не назначено написание владельца, — " +
			"такой владелец остался бы вне наблюдения")
	}
	if !strings.Contains(findings[0], "services/geo") {
		t.Errorf("находка не называет каталог: %q", findings[0])
	}
	t.Logf("вне словаря: %s", findings[0])
}

// TestSubscriptionStateDocsReaderFindsNoRowsWhenTheSectionIsGone — ось 6: раздел
// таблицы переименован.
//
// Ноль прочитанных строк обязан быть ОТЛИЧИМ от «все строки верны»: разбор
// возвращает пустоту, а проба на этом падает (`t.Fatal` в самом гейте), а не
// одобряет любую страницу.
func TestSubscriptionStateDocsReaderFindsNoRowsWhenTheSectionIsGone(t *testing.T) {
	page := syntheticPage(map[string]string{"vpc": claimStateful}, []string{"vpc"})
	if got := len(subscriptionOwnerRows(page)); got != 1 {
		t.Fatalf("контроль: на целой странице прочитано строк %d, ожидалась 1 — разбор "+
			"негоден, и остальные оси измеряли бы не то", got)
	}
	renamed := strings.Replace(page, ownerTableHeading, "## Перечень владельцев", 1)
	if got := len(subscriptionOwnerRows(renamed)); got != 0 {
		t.Errorf("раздел переименован, а разбор прочитал строк %d: он читает страницу "+
			"целиком и принял бы соседнюю таблицу за таблицу владельцев", got)
	}
}

// syntheticJournalPackingBehindACall кладёт объявление журнала, у которого
// упаковка стоит НЕ в функции состояния, а в функции пакета, которую та зовёт.
//
// `generic` выбирает написание вызова: обычное (`packState(r)`) либо явно
// инстанцированное у обобщённой функции (`packState[any](r)`). Написания два,
// оба законны, и распознаватель обязан знать оба — иначе записанное вторым
// окажется не разрешённым, а НЕ ОСМОТРЕННЫМ.
//
// `helperPacks` выключает упаковку в помощнике: законный близнец, на котором
// гейт обязан по-прежнему говорить «не собирает». Без него ось доказывала бы
// лишь, что гейт стал отвечать «собирает» чаще.
func syntheticJournalPackingBehindACall(t *testing.T, root, service string, generic, helperPacks bool) {
	t.Helper()
	dir := filepath.Join(root, "services", service, "internal", "subscriptionjournal")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("синтетическое дерево: %v", err)
	}
	call := "packState(r)"
	sig := "func packState(r any) (any, int, error) {"
	if generic {
		call = "packState[any](r)"
		sig = "func packState[T any](r T) (any, int, error) {"
	}
	body := "package subscriptionjournal\n\n" +
		"func state(r any) (any, int, error) {\n\treturn " + call + "\n}\n\n" + sig + "\n"
	if helperPacks {
		body += "\treturn anypb.New(r)\n"
	}
	body += "\treturn nil, 1, nil\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "journal.go"), []byte(body), 0o600); err != nil {
		t.Fatalf("синтетическое дерево: %v", err)
	}
}

// TestSubscriptionStateDocsGateFollowsPackingBehindAPackageCall — ось 7:
// упаковка вынесена в функцию пакета, которую зовёт функция состояния.
//
// # Что это была за слепота
//
// Распознаватель искал `anypb.New` в теле самой функции состояния. Журнал nlb
// вынес разбор конверта, перенос в контракт и упаковку в ОДНУ полосу, зовомую по
// видам, — и гейт объявил его непроизводящим ПРИ ЖИВОМ ПРОИЗВОДИТЕЛЕ, потребовав
// править правдивую страницу. Записанное новой формой оказалось не «разрешено», а
// не осмотрено: ни красного, ни зелёного — молчание.
//
// # Что доказывается
//
// Обе стороны, и обе на КАЖДОМ из двух написаний вызова: помощник упаковывает —
// «собирает» (страница, обещающая состояние, молчит; страница, отрицающая его,
// краснеет); помощник не упаковывает — «не собирает» (наоборот). Без второй
// половины ось доказывала бы лишь, что гейт стал сговорчивее.
func TestSubscriptionStateDocsGateFollowsPackingBehindAPackageCall(t *testing.T) {
	for _, form := range []struct {
		name    string
		generic bool
	}{
		{"обычный вызов", false},
		{"инстанцированный обобщённый вызов", true},
	} {
		t.Run(form.name, func(t *testing.T) {
			// Помощник УПАКОВЫВАЕТ: журнал производит состояние.
			packing := t.TempDir()
			syntheticJournalPackingBehindACall(t, packing, "vpc", form.generic, true)

			denied := judgeSynthetic(t, packing, syntheticPage(
				map[string]string{"vpc": claimStateless}, []string{"vpc"}))
			if len(denied) == 0 {
				t.Fatal("гейт смолчал на странице, отрицающей состояние, которое журнал " +
					"собирает через вызов функции пакета: форма записи ему неизвестна, и " +
					"записанное ею не осмотрено")
			}
			t.Logf("дефект найден: %s", denied[0])

			legit := judgeSynthetic(t, packing, syntheticPage(
				map[string]string{"vpc": claimStateful}, []string{"vpc"}))
			if len(legit) != 0 {
				t.Errorf("гейт краснеет на ЗАКОННОМ близнеце (страница называет тип, "+
					"помощник упаковывает): %v", legit)
			}

			// Помощник НЕ упаковывает: журнал состояния не производит, и обход
			// вызова не смеет этого изменить.
			silent := t.TempDir()
			syntheticJournalPackingBehindACall(t, silent, "vpc", form.generic, false)

			overclaim := judgeSynthetic(t, silent, syntheticPage(
				map[string]string{"vpc": claimStateful}, []string{"vpc"}))
			if len(overclaim) == 0 {
				t.Fatal("гейт счёл журнал производящим только потому, что функция состояния " +
					"кого-то зовёт: обход стал отвечать «да» на любой вызов")
			}
			t.Logf("перерасширения нет: %s", overclaim[0])

			quiet := judgeSynthetic(t, silent, syntheticPage(
				map[string]string{"vpc": claimStateless}, []string{"vpc"}))
			if len(quiet) != 0 {
				t.Errorf("гейт краснеет на законном близнеце (страница состояния не "+
					"обещает, помощник не упаковывает): %v", quiet)
			}
		})
	}
}

// TestSubscriptionStateDocsGateSaysWhenItsOwnPremiseBreaks — ось 8: предпосылка
// распознавателя перестала выполняться.
//
// Он обходит вызовы, объявленные в `journal.go`. Пока пакет журнала этим файлом
// исчерпывается, обход полон. Заведётся второй не-тестовый файл — упаковка,
// вынесенная туда, окажется НЕ ОСМОТРЕНА, а вердикт «не собирает» будет ложным и
// на вид исправным. Это тот же класс, что привёл к этой правке, поэтому
// предпосылка истекает сама, а не держится памятью.
func TestSubscriptionStateDocsGateSaysWhenItsOwnPremiseBreaks(t *testing.T) {
	root := t.TempDir()
	syntheticJournal(t, root, "vpc", true, false)

	// Контроль: одним файлом пакет исчерпан — гейт молчит.
	legit := judgeSynthetic(t, root, syntheticPage(
		map[string]string{"vpc": claimStateful}, []string{"vpc"}))
	if len(legit) != 0 {
		t.Fatalf("контроль: гейт краснеет на пакете из одного файла: %v", legit)
	}

	second := filepath.Join(root, "services", "vpc", "internal", "subscriptionjournal", "helpers.go")
	if err := os.WriteFile(second, []byte("package subscriptionjournal\n"), 0o600); err != nil {
		t.Fatalf("синтетическое дерево: %v", err)
	}
	findings := judgeSynthetic(t, root, syntheticPage(
		map[string]string{"vpc": claimStateful}, []string{"vpc"}))
	if len(findings) == 0 {
		t.Fatal("гейт смолчал на пакете журнала из двух файлов — его собственная " +
			"предпосылка сломалась незаметно, и он продолжил судить по неполному обходу")
	}
	if !strings.Contains(findings[0], "services/vpc") {
		t.Errorf("находка не называет каталог: %q", findings[0])
	}
	t.Logf("предпосылка сломалась: %s", findings[0])
}
