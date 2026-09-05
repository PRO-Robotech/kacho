// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

// Имя в коде — ТОЛЬКО латиницей. Комментарии и тексты — на любом языке.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЗАЧЕМ ГЕЙТ, ЕСЛИ ЭТО ВОПРОС ВКУСА
//
// Не вкуса. Кириллица даёт ОМОГЛИФЫ — знаки, неотличимые от латинских на глаз:
// «В» и «B», «С» и «C», «Р» и «P», «А» и «A», «о» и «o». Имя, содержащее такой
// знак, выглядит обычным и не находится ни поиском, ни отбором по имени.
//
// Цена измерена, а не предположена. Пять проб разбора доступа назывались
// `TestExpandAccess_В3_…` с кириллической «В», и отбор по видимому имени
// отвечал «no tests to run»: пробы существовали, были зелёными и НЕ
// ЗАПУСКАЛИСЬ тем, кто их звал. «Прогнал B3» означало «не прогнал ничего», и
// отличить это можно было только счётом.
//
// # ЧТО ИМЕННО ЗАПРЕЩЕНО — и что нет
//
// Запрещено не-ASCII в ИМЕНИ: функции, типа, поля, переменной, параметра,
// константы. Разрешено — и не проверяется — всё остальное: комментарии, строки,
// имена файлов, содержимое данных. Правило владельца дословно: «никаких
// русскоязычных вставок в коде; на русском могут быть комментарии и
// документация, но не сам код».
//
// # ПОЧЕМУ РАЗБОР, А НЕ ПОИСК ПО ОБРАЗЦУ
//
// Поиск по образцу этот класс не измеряет: он не отличает имя от строки и от
// комментария, а объявления видит не все. Перепись, сделанная образцом при
// заведении гейта, занизила счёт на четверть — она не увидела имён из
// деструктуризации. Разбор видит каждое объявленное имя by construction.
func TestIdentifiersAreASCII(t *testing.T) {
	root := repoRoot(t)

	out, err := gitenv.Command(root, "ls-files", "-z", "*.go").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v — без переписи «ноль находок» неотличимо от "+
			"«ноль прочитанного»", err)
	}

	var filesRead, identsSeen int
	var findings []string

	for _, rel := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if rel == "" || strings.Contains(rel, "/node_modules/") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 — путь из индекса git
		if err != nil {
			continue
		}
		seen, found, err := nonASCIIIdents(rel, src)
		if err != nil {
			// Неразбираемый файл — не находка: он и собраться не может, об этом
			// скажет сборка. Но и молчать о нём нельзя, иначе перепись завысит
			// объём осмотренного.
			t.Logf("не разобран (пропущен): %s: %v", rel, err)
			continue
		}
		filesRead++
		identsSeen += seen
		findings = append(findings, found...)
	}

	t.Logf("перепись: файлов Go прочитано %d, имён осмотрено %d, находок %d",
		filesRead, identsSeen, len(findings))

	if filesRead == 0 {
		t.Fatal("прочитано НОЛЬ файлов Go — перепись беспредметна, и «ноль находок» " +
			"означало бы «ноль прочитанного»")
	}

	if len(findings) > 0 {
		t.Errorf("имя в коде обязано быть латинским — найдено %d:\n  %s\n\n"+
			"Кириллица даёт омоглифы: имя выглядит латинским и не находится ни поиском, "+
			"ни отбором по имени. Пробу с такой буквой нельзя выбрать через `-run`, и "+
			"«прогнал» молча означает «не прогнал».\n"+
			"Комментарии и тексты сообщений правило НЕ ограничивает.",
			len(findings), strings.Join(findings, "\n  "))
	}
}

// nonASCIIIdents разбирает ОДИН исходник и возвращает: сколько имён осмотрено и
// какие из них нелатинские.
//
// Вынесена ради второй пробы: гейт обязан доказать, что различает ИМЯ и текст, —
// а доказать это можно только позвав его синтетикой, где обе формы стоят рядом.
func nonASCIIIdents(name string, src []byte) (seen int, findings []string, err error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, src, parser.ParseComments)
	if err != nil {
		return 0, nil, err
	}
	ast.Inspect(file, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		seen++
		for _, r := range id.Name {
			if r > unicode.MaxASCII {
				findings = append(findings, fset.Position(id.Pos()).String()+": "+id.Name+
					" — знак "+string(r)+" не латинский")
				return true
			}
		}
		return true
	})
	return seen, findings, nil
}

// Собственная предпосылка гейта: он ловит ИМЯ, а не знак.
//
// Без этой пробы гейт ловил бы форму: «нет находок» означало бы и «имена
// латинские», и «разбор ничего не увидел». Обе стороны утверждаются по каждой
// оси — дефект обязан находиться, законный близнец обязан молчать.
func TestIdentifiersASCIIGateTellsNameFromText(t *testing.T) {
	const withDefect = `package p

func собрать(вход string) string { return вход }
`
	// Законный близнец: та же кириллица, но НЕ в именах. Каждая форма ниже
	// встречается в дереве сотнями и обязана остаться нетронутой.
	const legitimate = `package p

// Разбор по-русски: почему здесь именно так.

/* Документация тоже по-русски. */

var label = "Облачные сети"

var byKey = map[string]int{"имя": 1}
`
	// Формы имени, которые легко ускользают: разбор видит их все как *ast.Ident,
	// но утверждать это надо, а не полагать. Способность этой проверки упасть
	// доказана инъекцией: латинское имя в синтетике при кириллическом ожидании
	// даёт красное с названием формы.
	forms := []struct {
		name string
		src  string
		want string
	}{
		{"поле структуры", "package p\n\ntype T struct{ поле string }\n", "поле"},
		{"метод", "package p\n\ntype T struct{}\n\nfunc (T) метод() {}\n", "метод"},
		{"константа", "package p\n\nconst Предел = 1\n", "Предел"},
		{"параметр типа", "package p\n\nfunc f[Тип any](x Тип) Тип { return x }\n", "Тип"},
		{"псевдоним импорта", "package p\n\nimport фмт \"fmt\"\n\nvar _ = фмт.Sprint\n", "фмт"},
	}
	for _, form := range forms {
		_, found, err := nonASCIIIdents("form.go", []byte(form.src))
		if err != nil {
			t.Errorf("%s: синтетика не разобралась: %v", form.name, err)
			continue
		}
		var hit bool
		for _, f := range found {
			if strings.Contains(f, form.want) {
				hit = true
			}
		}
		if !hit {
			t.Errorf("%s: имя %q обязано находиться, найдено: %v", form.name, form.want, found)
		}
	}

	// Омоглиф: кириллическое «с» вместо латинского «c». Глазом два имени
	// неотличимы — ради этого случая гейт и заведён.
	const homoglyph = `package p

var сount = 1
`

	seen, found, err := nonASCIIIdents("withdefect.go", []byte(withDefect))
	if err != nil {
		t.Fatalf("синтетика с дефектом не разобралась: %v", err)
	}
	if len(found) != 3 { // собрать, вход (параметр), вход (возврат)
		t.Errorf("дефект обязан находиться: имён осмотрено %d, находок %d, ожидалось 3: %v",
			seen, len(found), found)
	}

	seen, found, err = nonASCIIIdents("legit.go", []byte(legitimate))
	if err != nil {
		t.Fatalf("законная синтетика не разобралась: %v", err)
	}
	// Положительный контроль: имена в близнеце ЕСТЬ и осмотрены — значит «ноль
	// находок» означает отсутствие предмета, а не пустой разбор.
	if seen == 0 {
		t.Fatal("близнец не дал ни одного имени — «молчит» неотличимо от «не читал»")
	}
	if len(found) != 0 {
		t.Errorf("кириллица в комментарии, строке и ключе-строке законна, а гейт "+
			"нашёл %d: %v", len(found), found)
	}

	_, found, err = nonASCIIIdents("homoglyph.go", []byte(homoglyph))
	if err != nil {
		t.Fatalf("синтетика с омоглифом не разобралась: %v", err)
	}
	if len(found) != 1 {
		t.Errorf("омоглиф обязан находиться — он и есть предмет гейта; находок %d: %v",
			len(found), found)
	}
}
