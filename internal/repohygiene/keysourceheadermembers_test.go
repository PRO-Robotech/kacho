// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// keysourceheadermembers_test.go — перечень членов заголовка, из которых ключ
// выбираться НЕ МОЖЕТ, объявлен в ОДНОМ месте (приёмка F2, сценарий F2-07, §5).
//
// # Предмет
//
// Требование #4: ключ берётся только из нашего реестра. Встроенный в заголовок
// ключ и ссылки на ключ (сам ключ, ссылка на набор, ссылка на цепочку, сама
// цепочка, её отпечаток) ключ не выбирают и в разрешении не участвуют. Доверять
// ключу, приехавшему вместе с подписью, значит принимать ЛЮБУЮ подпись.
//
// Членов несколько, и выписанный по месту употребления перечень — это вторая
// его копия. Место, закрывшее один член и забывшее второй, выглядит закрытым:
// положительный путь работает, отрицательных утверждений просто нет.
//
// # Почему гейт молчит на месте, называющем ОДИН член
//
// То же доверие в соседнем механизме дерева законно и является его сутью:
// в доказательстве владения ключом (RFC 9449) ключ приходит в самом
// доказательстве, и проверяющий связывает его с токеном по отпечатку. Такое
// место называет СВОЙ член по своему решению — это употребление, а не вторая
// копия перечня. Гейт, запрещающий всякое упоминание, запретил бы механизм,
// ради которого §5 и написан.
//
// # Что здесь считается деревом
//
// Индекс git — то же множество, которое увидит свежий клон и CI.
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/tokenpolicy"
)

const (
	// keySourceOwnerFile — единственный дом перечня.
	keySourceOwnerFile = "pkg/tokenpolicy/policy.go"
	// keySourceOwnerDecl — объявление, которым перечень выражен.
	keySourceOwnerDecl = "функция KeySourceHeaderMembers"
	// keySourceCensusFloor — порог переписи: ниже него «ноль находок» означало
	// бы «ноль прочитанного».
	keySourceCensusFloor = 1000
	// keySourceListArity — сколько членов перечня делают его ПЕРЕЧНЕМ. Один
	// член перечнем не является: назвать его — значит принять решение об этом
	// члене, а не завести вторую копию списка.
	keySourceListArity = 2
)

// TestKeySourceHeaderMembersAreDeclaredOnce — сам гейт.
func TestKeySourceHeaderMembersAreDeclaredOnce(t *testing.T) {
	root := repoRoot(t)
	tt := newTrackedTree(t, root)

	// (1) Предпосылка: перечень существует и является перечнем. Гейт берёт
	// имена ИЗ НЕГО, а не выписывает своей рукой — вторая копия внутри проверки
	// перечня была бы тем самым дефектом, который проверка ищет.
	members := tokenpolicy.KeySourceHeaderMembers()
	if len(members) < keySourceListArity {
		t.Fatalf("единственное объявление перечня называет %d член(а) при пороге %d — "+
			"перечня больше нет, и «второй копией перечня» становиться нечему. Гейт "+
			"беспредметен: он молчал бы и тогда, когда предмет исчез, и тогда, когда "+
			"сломался он сам. Перечень: %v", len(members), keySourceListArity, members)
	}
	if !tt.hasFile(keySourceOwnerFile) {
		t.Fatalf("файла-владельца перечня (%s) в составе дерева НЕТ — перечень переехал, "+
			"и гейт стережёт координату, которой не существует", keySourceOwnerFile)
	}

	var rels []string
	for rel := range tt.files {
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		if skipPath(rel) {
			continue
		}
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	var (
		parsed, decls, literals, tagNames, mentions int
		lists, singles                              []KeySourceSite
	)
	for _, rel := range rels {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		sites, census, err := ScanKeySourceHeaderMembers(rel, src, members)
		if err != nil {
			t.Fatalf("разбор %s: %v", rel, err)
		}
		parsed++
		decls += census.Decls
		literals += census.StringLiterals
		tagNames += census.TagNames
		mentions += census.Mentions
		for _, s := range sites {
			if len(s.Members) >= keySourceListArity {
				lists = append(lists, s)
				continue
			}
			singles = append(singles, s)
		}
	}

	t.Logf("перепись: не-тестовых файлов Go разобрано %d, верхнеуровневых объявлений %d, "+
		"строковых литералов осмотрено %d, имён из тегов осмотрено %d, упоминаний членов "+
		"найдено %d, объявлений перечня (>=%d разных членов) %d, мест, называющих один "+
		"член, %d; сам перечень: %v",
		parsed, decls, literals, tagNames, mentions,
		keySourceListArity, len(lists), len(singles), members)

	if parsed < keySourceCensusFloor {
		t.Fatalf("перепись обвалилась: разобрано %d файлов при пороге %d — на таком объёме "+
			"«ноль находок» означало бы «ноль прочитанного»", parsed, keySourceCensusFloor)
	}
	if mentions == 0 {
		t.Fatalf("на %d файлах не найдено НИ ОДНОГО упоминания членов %v — разбор перестал "+
			"видеть предмет, и его молчание сказано ни о чём", parsed, members)
	}

	// (2) Предпосылка: объявление перечня в дереве ЕСТЬ и оно у владельца.
	if len(lists) == 0 {
		t.Fatalf("объявлений перечня в дереве НОЛЬ при %d упоминаниях членов — перечень "+
			"перестал быть выражен, и «вторая копия» перестала быть отличима от первой",
			mentions)
	}

	// (3) Находка: перечень выписан ВНЕ владельца.
	var findings []string
	for _, s := range lists {
		if s.File == keySourceOwnerFile {
			continue
		}
		findings = append(findings, fmt.Sprintf("%s:%d  %s — называет %s",
			s.File, s.Line, s.Decl, strings.Join(s.Members, ", ")))
	}
	sort.Strings(findings)
	if len(findings) > 0 {
		t.Fatalf("перечень членов заголовка выписан ВНЕ %s — %d место(а):\n  %s\n\n"+
			"Каждое такое место есть вторая копия перечня. Копия, закрывшая один член и "+
			"забывшая второй, ВЫГЛЯДИТ закрытой: положительный путь работает, а различие "+
			"между копиями не является ничьей находкой — оно не выражено и потому не может "+
			"покраснеть. Ключ, приехавший вместе с подписью, выбирает подпись сам: "+
			"предъявитель приложит тот ключ, которым подписал.\n"+
			"Снятие: звать %s (%s), а не выписывать имена своей рукой.",
			keySourceOwnerFile, len(findings), strings.Join(findings, "\n  "),
			keySourceOwnerDecl, keySourceOwnerFile)
	}

	// (4) Предпосылка различения: место, называющее ОДИН член, в дереве есть.
	// Без него молчание гейта на одночленных местах ничего бы не различало — оно
	// было бы верно и для гейта, запрещающего всякое упоминание, а такой гейт
	// запретил бы соседний механизм, где выбор ключа из предъявленного
	// материала ЗАКОНЕН и является предметом.
	if len(singles) == 0 {
		t.Fatalf("мест, называющих ОДИН член заголовка, в дереве НОЛЬ. Различие «объявление " +
			"перечня против употребления одного члена» на таком дереве ничего не различает: " +
			"гейт вёл бы себя так же, будь он запретом на всякое упоминание. Законный " +
			"близнец обязан существовать — иначе доказано только то, что перечень один.")
	}

	for _, s := range lists {
		t.Logf("единственное объявление перечня: %s:%d (%s) — %s",
			s.File, s.Line, s.Decl, strings.Join(s.Members, ", "))
	}
	var where []string
	for _, s := range singles {
		where = append(where, fmt.Sprintf("%s:%d (%s)", s.File, s.Line, s.Members[0]))
	}
	sort.Strings(where)
	t.Logf("законные употребления по одному члену (их сколько угодно): %s",
		strings.Join(where, ", "))
}
