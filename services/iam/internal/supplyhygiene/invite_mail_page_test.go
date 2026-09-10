// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: AGPL-3.0-or-later

// invite_mail_page_test.go — КЛИЕНТСКАЯ страница не вправе отрицать механизм,
// который дерево несёт (#2525).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Страница — единственный артефакт, по которому арендатор решает, ждать ли
// письма. Фраза «автоотправка email не интегрирована» есть утверждение о
// ДЕРЕВЕ, и утверждение это не компилируется: полосу заводят, а фраза остаётся.
//
// Цена не косметическая и приходит арендатору дважды. Прочитав страницу,
// оператор не станет объявлять почтовый узел вовсе — и получит ровно то
// поведение, которое страница обещает, но ПО ДРУГОЙ ПРИЧИНЕ: письмо не уходит,
// потому что узел не назван, а не потому, что отправлять некому. Второе:
// админ продолжит передавать ссылку руками, не зная, что этого можно не делать.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЖИВОСТЬ ПОЛОСЫ УСТАНАВЛИВАЕТСЯ ЗАМЕРОМ, А НЕ ДОПУЩЕНИЕМ
//
// Отрицание на странице законно ровно тогда, когда полосы в дереве НЕТ. Поэтому
// проверка сперва спрашивает дерево — тремя независимыми производителями, — и
// лишь затем судит страницы. Мир, где полосы нет, а страница её отрицает,
// молчит: это законный близнец, и он проверен инъекцией.
//
// Производители спрашиваются ЛИТЕРАЛАМИ разобранного исходника, а не поиском
// слова: имя очереди и имя ряда встречаются и в прозе, и в комментариях, и
// поиск словом принял бы за производителя собственное объяснение.
//
// ─────────────────────────────────────────────────────────────────────────────
// СУДИТСЯ ОТРИЦАНИЕ СУЩЕСТВОВАНИЯ, А НЕ УСЛОВНАЯ ФРАЗА — И ЭТО ГРАНИЦА
//
// Словарь отрицаний закрыт и содержит только формы «механизма нет»
// (`не интегрирован`, `not implemented`, …). Условная фраза — «пока почтовый
// узел не объявлен, письмо не уходит» — ЗАКОННА и намеренно вне наблюдения: она
// описывает настройку, а не отсутствие механизма. Отличить их машинно в общем
// случае нельзя, поэтому проверка берёт узкую полосу и называет её вслух;
// широкий словарь краснел бы на правдивом тексте, и его сняли бы первым.
//
// Корпус ДВУЯЗЫЧНЫЙ, поэтому оба словаря несут русскую и английскую форму:
// предикат на одном языке недобирает МОЛЧА.
//
// Способность упасть и смолчать доказана инъекцией — invite_mail_page_injection_test.go.
package supplyhygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// clientPagesDir — то, что арендатор ЧИТАЕТ, относительно корня службы.
// Инженерные страницы сайт не служит, и у поставившего продукт их нет.
const clientPagesDir = "docs/content"

// inviteMailLaneMarkers — производители почтовой полосы: имя очереди, объявление
// полосы живым процессом при старте и ряд исходов отправки. Три независимых
// предмета, и все три обязаны быть литералами не-тестового исходника.
var inviteMailLaneMarkers = []string{
	"kaname.invite_mail_outbox",
	"kaname invite mail drainer starting",
	"kaname_invite_mail_outcomes_total",
}

// autoSendTokens — как страница называет ПРЕДМЕТ (отправку письма за админа).
var autoSendTokens = []string{
	"автоотправк", "автоматическая отправка", "автоматически отправ",
	"auto-send", "autosend", "automatic email", "automatically sends",
}

// existenceDenials — закрытый словарь отрицания СУЩЕСТВОВАНИЯ. Условные формы
// («не уходит», «не отправляется, пока …») сюда не входят намеренно: см. шапку.
var existenceDenials = []string{
	"не интегрирован", "не реализован", "не поддерживается", "не предусмотрен",
	"not integrated", "not implemented", "not supported",
}

// denialWindow — сколько знаков вокруг предмета читается в поисках отрицания.
// Фраза переносится по строкам, поэтому окно берётся по ТЕКСТУ страницы, а не
// по строке: судить строку значило бы не видеть перенос.
const denialWindow = 220

// absenceClaim — страница, отрицающая существование предмета.
type absenceClaim struct {
	Page   string
	Line   int
	Denial string
}

// pageAbsenceCensus — объём осмотренного. Печатается ВСЕГДА: «ноль находок»
// обязано быть отличимо от «ноль прочитанного».
type pageAbsenceCensus struct {
	Pages    int
	Subjects int // вхождений предмета осмотрено
	Claims   int
}

// findAbsenceClaims разбирает ПРОИЗВОЛЬНЫЙ корпус страниц (путь → содержимое):
// настоящее дерево и синтетический мир инъекции проходят одну функцию, поэтому
// доказанное на втором верно для первого.
func findAbsenceClaims(pages map[string][]byte) ([]absenceClaim, pageAbsenceCensus) {
	census := pageAbsenceCensus{Pages: len(pages)}
	var out []absenceClaim

	paths := make([]string, 0, len(pages))
	for p := range pages {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, path := range paths {
		text := strings.ToLower(string(pages[path]))
		for _, subject := range autoSendTokens {
			from := 0
			for {
				idx := strings.Index(text[from:], subject)
				if idx < 0 {
					break
				}
				at := from + idx
				from = at + len(subject)
				census.Subjects++
				if denial, ok := denialNear(text, at, len(subject)); ok {
					census.Claims++
					out = append(out, absenceClaim{
						Page: path, Line: lineOf(text, at), Denial: denial,
					})
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Page != out[j].Page {
			return out[i].Page < out[j].Page
		}
		return out[i].Line < out[j].Line
	})
	return out, census
}

// denialNear — стоит ли рядом с предметом отрицание его существования.
func denialNear(text string, at, width int) (string, bool) {
	lo := at - denialWindow
	if lo < 0 {
		lo = 0
	}
	hi := at + width + denialWindow
	if hi > len(text) {
		hi = len(text)
	}
	window := text[lo:hi]
	for _, denial := range existenceDenials {
		if strings.Contains(window, denial) {
			return denial, true
		}
	}
	return "", false
}

// lineOf — номер строки смещения; координата находки, а не украшение.
func lineOf(text string, at int) int {
	return 1 + strings.Count(text[:at], "\n")
}

// inviteMailLaneProducers — какие маркеры полосы дерево ПРОИЗВОДИТ. Второе
// возвращаемое — файлов разобрано: ноль означает, что обход не состоялся, и
// «полосы нет» тогда значит «не искали».
func inviteMailLaneProducers(root string) (map[string]string, int, error) {
	found := map[string]string{}
	filesRead := 0
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, p, nil, 0)
		if perr != nil {
			// Неразбираемый исходник — предмет компилятора, не этого разбора.
			return nil
		}
		filesRead++
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			rel = p
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			value, uerr := strconv.Unquote(lit.Value)
			if uerr != nil {
				return true
			}
			for _, marker := range inviteMailLaneMarkers {
				if strings.Contains(value, marker) {
					if _, seen := found[marker]; !seen {
						found[marker] = filepath.ToSlash(rel)
					}
				}
			}
			return true
		})
		return nil
	})
	return found, filesRead, err
}

// clientPageCorpus — страницы, которые арендатор читает.
func clientPageCorpus(t *testing.T, root string) map[string][]byte {
	t.Helper()
	corpus := map[string][]byte{}
	dir := filepath.Join(root, filepath.FromSlash(clientPagesDir))
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(p, ".mdx") && !strings.HasSuffix(p, ".md") {
			return nil
		}
		body, rerr := os.ReadFile(p) // #nosec G304 -- путь из обхода своего дерева
		if rerr != nil {
			return rerr
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return relErr
		}
		corpus[filepath.ToSlash(rel)] = body
		return nil
	})
	require.NoErrorf(t, err, "обход клиентских страниц %s", clientPagesDir)
	return corpus
}

// TestClientPagesDoNotDenyTheInviteMailLane — ни одна клиентская страница не
// отрицает существования почтовой полосы, пока дерево её производит.
func TestClientPagesDoNotDenyTheInviteMailLane(t *testing.T) {
	t.Parallel()

	producers, filesRead, err := inviteMailLaneProducers(serviceRoot)
	require.NoError(t, err, "обход исходников службы")
	require.NotZerof(t, filesRead, "не разобрано ни одного исходника службы — "+
		"«полосы нет» здесь означало бы «не искали», и вердикт был бы вакуумным")

	missing := make([]string, 0, len(inviteMailLaneMarkers))
	for _, marker := range inviteMailLaneMarkers {
		if _, ok := producers[marker]; !ok {
			missing = append(missing, marker)
		}
	}

	pages := clientPageCorpus(t, serviceRoot)
	claims, census := findAbsenceClaims(pages)

	t.Logf("перепись: исходников разобрано %d · производителей полосы %d из %d · "+
		"страниц прочитано %d · вхождений предмета осмотрено %d · отрицаний найдено %d",
		filesRead, len(producers), len(inviteMailLaneMarkers),
		census.Pages, census.Subjects, census.Claims)
	for _, marker := range inviteMailLaneMarkers {
		if at, ok := producers[marker]; ok {
			t.Logf("производитель %q — %s", marker, at)
		}
	}

	require.NotZerof(t, census.Pages, "клиентских страниц не прочитано ни одной — "+
		"каталог %s переехал, и «отрицаний нет» означает «не читали»", clientPagesDir)
	require.NotZerof(t, census.Subjects, "предмет не встретился на страницах ни разу — "+
		"словарь предмета ослеп либо страницы перестали говорить об отправке письма; "+
		"в обоих случаях молчание проверки ничего не утверждает")

	if len(missing) > 0 {
		// Полоса СНЯТА — тогда отрицание на страницах законно, а этот держатель
		// потерял предмет и снимается ВМЕСТЕ с ней.
		t.Fatalf("производителей полосы не нашлось: %v — либо полоса снята из дерева "+
			"(тогда снимите этот держатель вместе с ней и верните страницам их "+
			"отрицание), либо распознаватель производителей ослеп. Различает это "+
			"человек, а не проба.", missing)
	}

	for _, c := range claims {
		t.Errorf("%s:%d — страница отрицает существование автоотправки (%q), "+
			"а дерево её производит: очередь, объявление полосы живым процессом и ряд "+
			"исходов отправки. Скажите фактическое состояние: отправка есть, полоса "+
			"объявляется ключами inviteMail.* (INSTALL.md, глава про почтовую полосу), "+
			"необъявленный узел означает, что письмо не отправляется, и наблюдаемо это "+
			"счётчиком исходов.", c.Page, c.Line, c.Denial)
	}
}
