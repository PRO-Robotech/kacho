// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// commentgateradius_injection_test.go — доказательство, что гейт радиуса
// СПОСОБЕН упасть и СПОСОБЕН смолчать.
//
// Прогонов ТРИ, и третий обязателен: контроль (всё цело — молчат обе стороны) ·
// инъекция нового свойства (краснеет только оно) · инъекция уже действовавшего
// (краснеет только оно). Без третьего молчание существующего контроля
// неотличимо от молчания мёртвого.
//
// Вход — СИНТЕТИЧЕСКИЙ: настоящее дерево инъекцию не переживает, а вердикт,
// снятый правкой общей рабочей копии, есть свойство диска, а не коммита.
//
// Судит здесь ТА ЖЕ функция, что и гейт (`JudgeServiceLocalList`). Своя копия
// разошлась бы с гейтом молча — и разошлась бы ровно там, где расхождение не
// видно: обе отвечают «совпало» на совпадающем входе.
package repohygiene

import (
	"strings"
	"testing"
)

// prohibitionSource собирает исходник инструмента, судящего комментарии по
// набору запретов: все четыре признака сходятся.
//
// head — то, что стоит ДО слова package (шапка файла).
func prohibitionSource(head string, patterns ...string) []byte {
	var b strings.Builder
	b.WriteString("// Copyright (c) PRO-Robotech\n")
	if head != "" {
		b.WriteString(head)
		if !strings.HasSuffix(head, "\n") {
			b.WriteString("\n")
		}
	}
	b.WriteString("package probe\n\n")
	b.WriteString("import (\n\t\"go/ast\"\n\t\"go/parser\"\n\t\"go/token\"\n\t\"path/filepath\"\n\t\"regexp\"\n)\n\n")
	b.WriteString("var forbidden = []*regexp.Regexp{\n")
	for _, p := range patterns {
		b.WriteString("\tregexp.MustCompile(`" + p + "`),\n")
	}
	b.WriteString("}\n\n")
	b.WriteString(`func run(root string) {
	_ = filepath.Walk(root, func(path string, _ interface{}, _ error) error {
		fset := token.NewFileSet()
		f, _ := parser.ParseFile(fset, path, nil, parser.ParseComments)
		for _, cg := range f.Comments {
			for _, c := range cg.List {
				for _, re := range forbidden {
					_ = re.MatchString(c.Text)
				}
			}
		}
		var _ ast.Node
		return nil
	})
}
`)
	return []byte(b.String())
}

// liveTree — координаты, существующие в синтетическом дереве. Одна живая, одной
// нет: объявление радиуса обязано называть живую.
func liveTree(rel string) bool { return rel == "tools/foreignclouds" }

// declaration — шапка с объявлением радиуса, называющая координату coord.
func declaration(coord string) string {
	return "//\n// РАДИУС: инструмент служебный, читает ОДИН сервис; общий предмет держит\n" +
		"// " + coord + " по всему дереву.\n"
}

// forbidsMandated — набор, запрещающий нормативную форму (ссылку на номер
// непреложного запрета и ссылку на раздел правила).
var forbidsMandated = []string{`#\d`, `\x{00a7}`, `(?i)\bphase\b`}

// forbidsOnlyRealViolations — набор из настоящих запретов: нормативных форм он
// не трогает. Это ЗАКОННЫЙ БЛИЗНЕЦ — гейт обязан на нём молчать даже без
// объявления радиуса.
var forbidsOnlyRealViolations = []string{`\bTODO\b`, `\bKAC-\d`, `(?i)netbox`}

func scanOne(t *testing.T, rel string, src []byte) *CommentProhibitionList {
	t.Helper()
	list, census, err := ScanCommentProhibitionList(rel, src)
	if err != nil {
		t.Fatalf("разбор %s: %v", rel, err)
	}
	if census.Literals == 0 {
		t.Fatalf("%s: осмотрено ноль образцов — разбор не увидел предмета", rel)
	}
	return list
}

// TestRadiusGateFiresOnlyOnTheMissingDeclaration — три прогона.
func TestRadiusGateFiresOnlyOnTheMissingDeclaration(t *testing.T) {
	const rel = "services/probe/internal/commentlint/commentlint_test.go"

	// ── ПРОГОН 1: контроль. Объявление на месте и называет живую координату.
	t.Run("контроль: объявление на месте — молчание", func(t *testing.T) {
		list := scanOne(t, rel, prohibitionSource(declaration("tools/foreignclouds"), forbidsMandated...))
		if list == nil {
			t.Fatal("набор не опознан — четыре признака сошлись, а разбор их не увидел")
		}
		finding, collisions := JudgeServiceLocalList(list, MandatedCommentForms, liveTree)
		if collisions == 0 {
			t.Fatal("контроль беспредметен: набор не запретил ни одной нормативной формы, " +
				"значит молчание вердикта ничего не доказывает")
		}
		if finding != "" {
			t.Fatalf("гейт нашёл замечание там, где всё цело:\n%s", finding)
		}
	})

	// ── ПРОГОН 2: инъекция НОВОГО свойства — объявления нет.
	t.Run("инъекция: объявления нет — находка с именем файла", func(t *testing.T) {
		list := scanOne(t, rel, prohibitionSource("", forbidsMandated...))
		if list == nil {
			t.Fatal("набор не опознан")
		}
		if list.Declaration != "" {
			t.Fatalf("инъекция не состоялась: объявление найдено — %q", list.Declaration)
		}
		finding, _ := JudgeServiceLocalList(list, MandatedCommentForms, liveTree)
		if finding == "" {
			t.Fatal("гейт промолчал на наборе БЕЗ объявления радиуса — он не способен упасть")
		}
		for _, must := range []string{rel, radiusMarker, "ТРЕБУЕТ", "не объявляет своего радиуса"} {
			if !strings.Contains(finding, must) {
				t.Errorf("находка не называет %q — читателю некуда идти:\n%s", must, finding)
			}
		}
		if strings.Contains(finding, "живой координаты") {
			t.Errorf("находка об отсутствии объявления пересказывает находку о мёртвой "+
				"координате — два разных дефекта обязаны читаться по-разному:\n%s", finding)
		}
	})

	// ── ПРОГОН 3: инъекция УЖЕ ДЕЙСТВОВАВШЕГО свойства — объявление есть, но
	// координата в нём мёртвая. Без этого прогона молчание проверки координаты
	// неотличимо от её отсутствия.
	t.Run("инъекция: координата мёртвая — своя, ОТЛИЧНАЯ находка", func(t *testing.T) {
		list := scanOne(t, rel, prohibitionSource(declaration("tools/nowhere-at-all"), forbidsMandated...))
		if list == nil {
			t.Fatal("набор не опознан")
		}
		if list.Declaration == "" {
			t.Fatal("инъекция не состоялась: объявление не найдено")
		}
		finding, _ := JudgeServiceLocalList(list, MandatedCommentForms, liveTree)
		if finding == "" {
			t.Fatal("гейт промолчал на объявлении с мёртвой координатой — самоистечение " +
				"объявления не работает")
		}
		// Два разных дефекта обязаны читаться по-разному. Сверяем по РАЗЛИЧАЮЩЕЙ
		// фразе каждого, а не по маркеру: маркер стоит и в процитированном
		// объявлении, поэтому по нему находки неразличимы — что эта проба и
		// показала на своей первой редакции.
		if !strings.Contains(finding, "живой координаты") {
			t.Errorf("находка не называет предмет — мёртвую координату:\n%s", finding)
		}
		if strings.Contains(finding, "не объявляет своего радиуса") {
			t.Errorf("находка о мёртвой координате пересказывает находку об ОТСУТСТВИИ "+
				"объявления — читатель починит не то:\n%s", finding)
		}
	})
}

// TestRadiusGateStaysSilentOnLegitimateTwins — законные близнецы. Без них гейт
// ловит форму, а не существо, и первый же ложный вердикт его отключит.
func TestRadiusGateStaysSilentOnLegitimateTwins(t *testing.T) {
	t.Run("служебный набор без нормативных совпадений — объявления не требует", func(t *testing.T) {
		list := scanOne(t, "services/probe/internal/commentlint/commentlint_test.go",
			prohibitionSource("", forbidsOnlyRealViolations...))
		if list == nil {
			t.Fatal("набор не опознан")
		}
		finding, collisions := JudgeServiceLocalList(list, MandatedCommentForms, liveTree)
		if collisions != 0 {
			t.Fatalf("близнец подобран неверно: он запрещает нормативную форму (%d совпадений)", collisions)
		}
		if finding != "" {
			t.Fatalf("гейт краснеет на наборе, который запрещает только настоящие "+
				"нарушения:\n%s", finding)
		}
		if CountControlHits([]*CommentProhibitionList{list}, ControlViolationForms) == 0 {
			t.Fatal("близнец не находит ни одного настоящего нарушения — тогда он не " +
				"инструмент, и его молчание ничего не доказывает")
		}
	})

	t.Run("файл с ТРЕМЯ признаками из четырёх набором не считается", func(t *testing.T) {
		// Образцов меньше порога: одиночная проверка формы набором запретов не
		// является, и требовать от неё объявления радиуса было бы ложной находкой.
		src := prohibitionSource("", `#\d`, `\x{00a7}`)
		list, census, err := ScanCommentProhibitionList("services/probe/internal/x/x_test.go", src)
		if err != nil {
			t.Fatalf("разбор: %v", err)
		}
		if list != nil {
			t.Fatalf("набором опознан файл с %d образцами при пороге %d",
				len(list.Patterns), minProhibitionPatterns)
		}
		if census.SignalsMet != 3 {
			t.Fatalf("инъекция не состоялась: сошлось %d признаков вместо трёх", census.SignalsMet)
		}
	})

	t.Run("прогонщик способен найти настоящее нарушение", func(t *testing.T) {
		list := scanOne(t, "internal/probe/x_test.go", prohibitionSource("", forbidsOnlyRealViolations...))
		if list == nil {
			t.Fatal("набор не опознан")
		}
		if got := CountControlHits([]*CommentProhibitionList{list}, ControlViolationForms); got != len(ControlViolationForms) {
			t.Fatalf("контрольных совпадений %d при %d формах — прогонщик видит не все",
				got, len(ControlViolationForms))
		}
	})
}
