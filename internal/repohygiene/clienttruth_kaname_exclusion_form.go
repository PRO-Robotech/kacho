// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clienttruth_kaname_exclusion_form.go — форма взаимоисключения, НАЗВАННАЯ
// оператору, есть та, которой оно держится.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Назваться на публичном слушателе Kaname можно двумя способами: предъявить
// удостоверение самому либо получить личность от нашего края. Способы
// взаимоисключающи, и держится это ПОСТРОЕНИЕМ: край снимает арендаторское
// удостоверение перед пересылкой за себя, установив личность сам. За краем
// действует переданная личность, напрямую — предъявленная, и сочетания не
// бывает.
//
// Держалось оно не всегда так. Прежняя редакция требовала ОТВЕРГАТЬ запрос,
// несущий обе формы, — и эта проверка отвергала бы каждый запрос, проксированный
// нашим же краем, потому что сочетание производил он сам. Ветвь отказа снята
// вместе со своим предметом; страница оператора об этом узнать не могла — она
// не собирается и ничего не импортирует.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ИМЕННО УТВЕРЖДАЕТСЯ — ДВА УТВЕРЖДЕНИЯ, И ВТОРОЕ ПОЛОЖИТЕЛЬНОЕ
//
//	A. отказ, ОБЕЩАННЫЙ страницей, обязан иметь производителя в не-тестовом
//	   коде читателя. Обещание без производителя — утверждение, пережившее свой
//	   предмет: оператор ждёт отказа, которого служба не делает;
//	B. пока построение живо, страница обязана НАЗЫВАТЬ его.
//
// Второе — положительное, и это не стилистика. Отрицание («страница не говорит
// такого-то слова») ЗАМОЛКАЕТ при первой же переформулировке: вход, на котором
// оно находит нарушение, перестаёт быть представимым, а ветвь остаётся в коде и
// вердикт остаётся зелёным. Отличить это от исправной работы нельзя ничем
// (`testing.md` §«Гейт на класс», п. 9). Утверждение B умолкнуть не может: оно
// краснеет ровно тогда, когда объяснение исчезает.
//
// ─────────────────────────────────────────────────────────────────────────────
// САМОИСТЕЧЕНИЕ: форма взаимоисключения — РЕШЕНИЕ ВЛАДЕЛЬЦА, а не константа
//
// Обратное решение («вернуть проверку на пути запроса») гейт не запрещает и
// запрещать не вправе: он судит СОГЛАСИЕ страницы с деревом, а не выбор формы.
// Вернётся ветвь отказа — производитель у обещания появится, и утверждение A
// умолкнет само. Уедет снятие — умолкнет B, а тестовая премиса скажет об этом
// вслух, вместо того чтобы отдать зелёный на мёртвом механизме.
//
// ─────────────────────────────────────────────────────────────────────────────
// РАСПОЗНАВАТЕЛИ — ЧЕМ СУДЯТ, И ПОЧЕМУ НЕ ПОДСТРОКОЙ ПО ФАЙЛУ
//
// Сторона ДЕРЕВА судится РАЗБОРОМ, а не текстом, и это несущее: файл,
// объявляющий снятие, называет его же тремя строками комментария, а файл
// читателя несёт НАДГРОБИЕ снятой ветви — связный абзац по-русски о том самом
// сочетании форм. Проверка по подстроке нашла бы там и снятие, и отказ, то есть
// объявила бы живым ровно то, что снято (`testing.md` §«Гейт на класс», п. 4).
// Поэтому считаются УЗЛЫ-ВЫЗОВЫ и литеральные аргументы, а комментарии не
// читаются вовсе.
//
// Сторона СТРАНИЦЫ судится по абзацу — прозы иначе не судить, и это сказано
// прямо, а не спрятано. Смягчает три вещи:
//
//   - корень предмета один и это ИМЯ предмета (`взаимоисключ`), а не оборот
//     речи: переформулировать объяснение, не назвав предмета, значит перестать
//     его объяснять, и это ловит утверждение B;
//   - регистр снимается. Страница пишет `ОБЕ` прописными, и предикат, читающий
//     написанное, промолчал бы на живом производителе — та же слепота, что
//     четырежды за жизнь приёмки KAN-AUTHN-1;
//   - вердикт выносится ПОАБЗАЦНО. Соседний абзац той же страницы законно
//     говорит об отказе — о единообразии его текста, — и файл целиком читать
//     нельзя: отказ соседа зачёлся бы обещанием предмета.
//
// ─────────────────────────────────────────────────────────────────────────────
// «НОЛЬ НАХОДОК» ОТЛИЧИМО ОТ «НОЛЬ ПРОЧИТАННОГО»
//
// Перепись печатается ВСЕГДА и несёт обе стороны разом: сколько абзацев
// прочитано и сколько признано предметом, сколько файлов края разобрано и
// сколько вызовов снятия найдено, сколько отказов читателя встречено и сколько
// из них о сочетании форм. Одна величина скрывает ровно тот случай, ради
// которого гейт заведён.

package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// ClientTruthKanameExclusionFormOptions — что судить. Координаты приходят
// параметрами, а не литералами внутри: инъекция подаёт синтетическое дерево тем
// же входом, что и прогон по настоящему.
type ClientTruthKanameExclusionFormOptions struct {
	Tree *treecorpus.Tree

	// GuidePath — страница установки: то, что читает оператор.
	GuidePath string

	// EdgeDir — дерево края, в котором ищутся вызовы снятия.
	EdgeDir string
	// StripFuncs — имена снятия удостоверения перед пересылкой.
	StripFuncs []string
	// StripDeclFile — файл, ОБЪЯВЛЯЮЩИЙ снятие. Его собственные определения
	// вызовами не являются: объявление без вызывающего механизма не даёт.
	StripDeclFile string

	// ReaderDir — не-тестовый код читателя предъявленного удостоверения.
	ReaderDir string
	// RefuseFunc — имя конструктора отказа читателя.
	RefuseFunc string
	// CoPresenceTerms — слова, которыми сообщение отказа называет СОЧЕТАНИЕ
	// двух форм. Требуются ВСЕ: отказ, назвавший одну форму, — про неё одну.
	CoPresenceTerms []string
}

// exclusionSubjectRoots — корень имени предмета.
//
// Один и именно этот: `взаимоисключ` есть ИМЯ предмета, а не оборот речи. Им
// зовут его и приёмка, и надгробие снятой ветви, и объявление снятия.
var exclusionSubjectRoots = []string{"взаимоисключ"}

// exclusionRefusalRoots — чем абзац обещает ОТКАЗ службы.
var exclusionRefusalRoots = []string{"отверга", "отказыва", "не принима", "получает отказ"}

// exclusionBuildRoots — чем абзац называет ПОСТРОЕНИЕ.
//
// Все три о снятии удостоверения на крае: «снимает», «снято/снятие», «не
// пересылает». Оборот «не несёт» сюда НЕ входит намеренно — им равно
// описывается и мир проверки («запрос несёт обе формы»), и мир построения, а
// маркер, годный для обоих миров, не различает ничего.
var exclusionBuildRoots = []string{"снима", "снят", "не пересыла"}

// ClientTruthKanameExclusionFormFinding — одно расхождение страницы с деревом.
type ClientTruthKanameExclusionFormFinding struct {
	// Kind — вид расхождения: "refusal-without-producer" | "construction-unnamed".
	Kind string
	Rel  string
	// Line — первая строка абзаца предмета; 0, когда абзаца предмета нет вовсе.
	Line    int
	Excerpt string
}

func (f ClientTruthKanameExclusionFormFinding) String() string {
	where := f.Rel
	if f.Line > 0 {
		where = fmt.Sprintf("%s:%d", f.Rel, f.Line)
	}
	switch f.Kind {
	case "refusal-without-producer":
		return fmt.Sprintf("%s — страница обещает оператору ОТКАЗ на сочетании двух форм "+
			"личности, а производителя такого отказа в не-тестовом коде читателя НЕТ: %q",
			where, f.Excerpt)
	case "construction-unnamed":
		return fmt.Sprintf("%s — край СНИМАЕТ арендаторское удостоверение перед пересылкой "+
			"за себя, а страница взаимоисключения этим построением не объясняет: %q",
			where, f.Excerpt)
	default:
		return fmt.Sprintf("%s — %s: %q", where, f.Kind, f.Excerpt)
	}
}

// ClientTruthKanameExclusionFormCensus — объём осмотренного, по обеим сторонам.
type ClientTruthKanameExclusionFormCensus struct {
	// GuideParagraphs — абзацев страницы прочитано.
	GuideParagraphs int
	// SubjectParagraphs — из них говорят о предмете.
	SubjectParagraphs int
	// ExplainByRefusal — из абзацев предмета объясняют ОТКАЗОМ службы.
	ExplainByRefusal int
	// ExplainByBuild — из абзацев предмета называют ПОСТРОЕНИЕ.
	ExplainByBuild int

	// EdgeGoFiles — не-тестовых файлов края разобрано.
	EdgeGoFiles int
	// StripCalls — вызовов снятия вне объявляющего файла.
	StripCalls int

	// ReaderGoFiles — не-тестовых файлов читателя разобрано.
	ReaderGoFiles int
	// RefuseCalls — вызовов отказа у читателя встречено.
	RefuseCalls int
	// CoPresenceRefusals — из них о СОЧЕТАНИИ двух форм.
	CoPresenceRefusals int
}

// exclusionParagraph — абзац страницы вместе с номером своей первой строки.
type exclusionParagraph struct {
	Line int
	Text string
}

// exclusionSplitParagraphs режет страницу на абзацы по пустой строке, храня
// номер первой строки каждого: находка обязана называть координату, а не файл.
func exclusionSplitParagraphs(body string) []exclusionParagraph {
	var out []exclusionParagraph
	cur := exclusionParagraph{Line: 1}
	var buf []string
	flush := func(next int) {
		if strings.TrimSpace(strings.Join(buf, "\n")) != "" {
			cur.Text = strings.Join(buf, "\n")
			out = append(out, cur)
		}
		buf = nil
		cur = exclusionParagraph{Line: next}
	}
	for i, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == "" {
			flush(i + 2)
			continue
		}
		if len(buf) == 0 {
			cur.Line = i + 1
		}
		buf = append(buf, line)
	}
	flush(0)
	return out
}

// exclusionHasAny — несёт ли текст хоть один корень. Регистр снят вызывающим.
func exclusionHasAny(lowered string, roots []string) bool {
	for _, r := range roots {
		if strings.Contains(lowered, r) {
			return true
		}
	}
	return false
}

// exclusionExcerpt — короткая выдержка абзаца для находки.
func exclusionExcerpt(text string) string {
	flat := strings.Join(strings.Fields(text), " ")
	const limit = 140
	if len(flat) <= limit {
		return flat
	}
	cut := flat[:limit]
	if i := strings.LastIndex(cut, " "); i > 0 {
		cut = cut[:i]
	}
	return cut + "…"
}

// exclusionCalleeName — имя вызываемого: `f(…)` и `x.f(…)` дают `f`.
func exclusionCalleeName(c *ast.CallExpr) string {
	switch fn := c.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		return fn.Sel.Name
	}
	return ""
}

// exclusionLiteralArgs — строковые литералы аргументов вызова, склеенные.
//
// Склейка `"a" + "b"` разбирается: сообщение отказа собирают конкатенацией, и
// разбор, читающий только одиночный литерал, такое сообщение НЕ УВИДЕЛ БЫ — то
// есть дал бы молчание вместо вердикта.
func exclusionLiteralArgs(c *ast.CallExpr) string {
	var sb strings.Builder
	var walk func(ast.Expr)
	walk = func(e ast.Expr) {
		switch v := e.(type) {
		case *ast.BasicLit:
			if v.Kind == token.STRING {
				if s, err := strconv.Unquote(v.Value); err == nil {
					sb.WriteString(s)
					sb.WriteString(" ")
				}
			}
		case *ast.BinaryExpr:
			walk(v.X)
			walk(v.Y)
		}
	}
	for _, a := range c.Args {
		walk(a)
	}
	return sb.String()
}

// exclusionGoFiles — отслеживаемые не-тестовые файлы Go под каталогом.
func exclusionGoFiles(tree *treecorpus.Tree, dir string) []string {
	var out []string
	for _, rel := range clientTruthTreeFiles(tree, dir, true, ".go") {
		if strings.HasSuffix(rel, "_test.go") {
			continue
		}
		out = append(out, rel)
	}
	sort.Strings(out)
	return out
}

// AuditClientTruthKanameExclusionForm сверяет форму взаимоисключения, названную
// оператору, с той, которой оно держится в дереве.
func AuditClientTruthKanameExclusionForm(
	opts ClientTruthKanameExclusionFormOptions, log *strings.Builder,
) ([]ClientTruthKanameExclusionFormFinding, ClientTruthKanameExclusionFormCensus, error) {
	var census ClientTruthKanameExclusionFormCensus

	// ── сторона ДЕРЕВА: живо ли построение ───────────────────────────────────
	strip := map[string]bool{}
	for _, n := range opts.StripFuncs {
		strip[n] = true
	}
	fset := token.NewFileSet()
	for _, rel := range exclusionGoFiles(opts.Tree, opts.EdgeDir) {
		src, err := clientTruthReadTreeFile(opts.Tree, rel)
		if err != nil {
			return nil, census, fmt.Errorf("файл края %s не прочитан: %w", rel, err)
		}
		f, perr := parser.ParseFile(fset, rel, src, 0)
		if perr != nil {
			return nil, census, fmt.Errorf("файл края %s не разобран: %w", rel, perr)
		}
		census.EdgeGoFiles++
		if rel == opts.StripDeclFile {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok && strip[exclusionCalleeName(c)] {
				census.StripCalls++
			}
			return true
		})
	}

	// ── сторона ДЕРЕВА: производит ли читатель отказ на сочетании ────────────
	for _, rel := range exclusionGoFiles(opts.Tree, opts.ReaderDir) {
		src, err := clientTruthReadTreeFile(opts.Tree, rel)
		if err != nil {
			return nil, census, fmt.Errorf("файл читателя %s не прочитан: %w", rel, err)
		}
		f, perr := parser.ParseFile(fset, rel, src, 0)
		if perr != nil {
			return nil, census, fmt.Errorf("файл читателя %s не разобран: %w", rel, perr)
		}
		census.ReaderGoFiles++
		ast.Inspect(f, func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if !ok || exclusionCalleeName(c) != opts.RefuseFunc {
				return true
			}
			census.RefuseCalls++
			msg := strings.ToLower(exclusionLiteralArgs(c))
			for _, term := range opts.CoPresenceTerms {
				if !strings.Contains(msg, strings.ToLower(term)) {
					return true
				}
			}
			census.CoPresenceRefusals++
			return true
		})
	}

	// ── сторона СТРАНИЦЫ: чем она объясняет ──────────────────────────────────
	page, err := clientTruthReadTreeFile(opts.Tree, opts.GuidePath)
	if err != nil {
		return nil, census, fmt.Errorf("страница установки %s не прочитана: %w", opts.GuidePath, err)
	}
	var findings []ClientTruthKanameExclusionFormFinding
	var firstSubject *exclusionParagraph
	for _, p := range exclusionSplitParagraphs(string(page)) {
		census.GuideParagraphs++
		lowered := strings.ToLower(p.Text)
		if !exclusionHasAny(lowered, exclusionSubjectRoots) {
			continue
		}
		census.SubjectParagraphs++
		if firstSubject == nil {
			cp := p
			firstSubject = &cp
		}
		if exclusionHasAny(lowered, exclusionRefusalRoots) {
			census.ExplainByRefusal++
			if census.CoPresenceRefusals == 0 {
				findings = append(findings, ClientTruthKanameExclusionFormFinding{
					Kind: "refusal-without-producer", Rel: opts.GuidePath,
					Line: p.Line, Excerpt: exclusionExcerpt(p.Text),
				})
			}
		}
		if exclusionHasAny(lowered, exclusionBuildRoots) {
			census.ExplainByBuild++
		}
	}
	// Утверждение B — положительное: пока построение живо, страница обязана
	// его называть. Ноль абзацев предмета — тот же случай: объяснение исчезло.
	if census.StripCalls > 0 && census.ExplainByBuild == 0 {
		f := ClientTruthKanameExclusionFormFinding{
			Kind: "construction-unnamed", Rel: opts.GuidePath,
			Excerpt: "абзаца предмета на странице нет вовсе",
		}
		if firstSubject != nil {
			f.Line = firstSubject.Line
			f.Excerpt = exclusionExcerpt(firstSubject.Text)
		}
		findings = append(findings, f)
	}

	if log != nil {
		fmt.Fprintf(log, "перепись страницы %s: абзацев прочитано %d, о предмете %d, "+
			"объясняют отказом %d, называют построение %d\n",
			opts.GuidePath, census.GuideParagraphs, census.SubjectParagraphs,
			census.ExplainByRefusal, census.ExplainByBuild)
		fmt.Fprintf(log, "перепись дерева: файлов края разобрано %d, вызовов снятия %d; "+
			"файлов читателя разобрано %d, отказов %d, из них о сочетании форм %d\n",
			census.EdgeGoFiles, census.StripCalls,
			census.ReaderGoFiles, census.RefuseCalls, census.CoPresenceRefusals)
	}
	return findings, census, nil
}
