// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// principalabsence_test.go — гейт против утверждения об ОТСУТСТВИИ личности,
// написанного аксессором, который отсутствие выразить не может.
//
// В дереве два аксессора носителя личности, и они отвечают на РАЗНЫЕ вопросы:
//
//   - operations.PrincipalFromContext(ctx) — «кем считать вызывающего». У него
//     есть запасное значение: на контексте без личности он возвращает
//     SystemPrincipal(). То же самое он возвращает и когда системная личность
//     выставлена ЯВНО. Два состояния — одно наблюдаемое;
//   - operations.PrincipalFromContextOK(ctx) — «была ли личность вообще». Он
//     возвращает вторым значением признак наличия и потому эти два состояния
//     различает. Его собственный godoc это прямо и оговаривает.
//
// Отсюда: проверка вида «носитель вычищен» ⇒ «PrincipalFromContext вернул
// SystemPrincipal» ничего не проверяет. Она остаётся зелёной и тогда, когда
// недоверенному пиру системную личность ВЫДАЛИ, — а это ровно та личность, на
// которую матчится предикат владения каждой системно записанной операции, и
// которую authz-слой при AllowSystemPrincipal пропускает без Check'а.
//
// Измерено на подмене одного вызова в pkg/grpcsrv/cert_identity.go (ветка
// недоверенного пира: WithoutPrincipal → WithPrincipal(SystemPrincipal())):
//
//	до правки: 8 утверждений в 6 файлах  -> все ЗЕЛЁНЫЕ
//	после:     те же 8 утверждений       -> все КРАСНЫЕ
//
// Четыре из этих файлов названы именем инварианта, который они не видели.
// Накопилось так потому, что форма проверки выглядит убедительно: сравнение с
// именованной константой читается как строгое утверждение, и ни один обзор
// диффа не отличает его от настоящего.
//
// # Почему сужают ПРОВЕРКИ, а не сам аксессор
//
// Напрашивается убрать запасное значение из PrincipalFromContext — тогда
// «нет личности» и «системная личность» перестанут сливаться, и слепую проверку
// станет невозможно написать. Разбор по вызывающим показывает, что это сломает
// работающее, причём с ОБЕИХ сторон:
//
//   - запасное значение ПРОИЗВОДЯТ намеренно: pkg/auth.PropagateOutgoing кладёт
//     его в исходящие метаданные, чтобы вызовы фоновых воркеров уходили на
//     провод атрибутируемыми как system, а не безымянными. Это задокументировано
//     в его godoc и закреплено собственным тестом;
//   - запасное значение ПОТРЕБЛЯЮТ, компенсируя его на месте: vpc и compute
//     (principalForwarded), iam (authzguard.IsAnonymous), pkg/authz
//     (isAnonymousSubject) и три authzfilter.SubjectFromPrincipal читают
//     неразличающий аксессор и отдельно отсекают пару {system, bootstrap} как
//     «личность не предъявлена». Сужение аксессора обнулило бы эти ветки
//     молча — они перестали бы совпадать с тем, что аксессор возвращает.
//
// Решения о доступе на запасное значение НЕ опираются: authz-слой резолвит
// субъект через PrincipalFromContextOK и на ok=false отказывает до Check'а
// (pkg/authz/subject_extract.go), а ключ владения — через OwnerFromContext,
// который отсутствие и анонимность отбрасывает. То есть неразличимость живёт
// ровно там, где она безвредна, и не живёт там, где решала бы.
//
// Отсюда выбор: аксессор оставлен как есть, а класс закрыт гейтом ниже.
package repohygiene

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/operations"
)

// Освобождения для СЕБЯ этот гейт не держит намеренно. Предпосылка ниже
// формулируется как «два состояния читаются ОДИНАКОВО» и сравнивает два вызова
// PrincipalFromContext между собой — запрещаемая форма (сравнение с
// SystemPrincipal()) для неё не нужна. Освобождение без предмета — та самая
// тихая дыра, которую этот файл и запрещает; заводить его «на всякий случай»
// нельзя.

// contractOwners — файлы, которым сравнение с SystemPrincipal() РАЗРЕШЕНО,
// потому что их предмет — само запасное значение, а не вычищенность носителя.
//
// pkg/operations/principal_test.go пиннит контракт аксессора и репозитория:
// «пустой ctx → SystemPrincipal», «Create без явной личности → SystemPrincipal».
// Это утверждения о ПРИСУТСТВИИ запасного значения там, где оно и задумано;
// если их снять, запасное значение перестанет быть зафиксированным вовсе.
//
// Исключение самоистекающее: TestContractOwnerExemptionsStillHaveSubject падает,
// как только освобождённый файл перестаёт нести предмет освобождения. Пустое
// исключение — тихая дыра: в такой файл можно внести слепую проверку, и никто
// не возразит.
var contractOwners = []string{
	"pkg/operations/principal_test.go",
}

// assertionFuncRe — имена функций сравнения (testify и его обёртки). Проверяется
// ТОЛЬКО последний сегмент селектора, поэтому require.Equal, assert.Equal и
// s.Require().NotEqual попадают одинаково.
var assertionFuncRe = regexp.MustCompile(`^(Equal|Equalf|NotEqual|NotEqualf|EqualValues|EqualValuesf|NotEqualValues|NotEqualValuesf)$`)

// TestNoBlindPrincipalAbsenceAssertions — ни один тест не сравнивает наблюдаемое
// с operations.SystemPrincipal().
//
// Что делать, если гейт сработал, — ровно три исхода, четвёртого нет:
//
//  1. проверка утверждает ОТСУТСТВИЕ личности (вычищенность носителя, отказ
//     недоверенному отправителю) -> переписать на operations.PrincipalFromContextOK
//     и утверждать признак наличия: `_, present := …OK(ctx); if present { … }`.
//     Для ключа владения то же самое делает operations.OwnerFromContext;
//  2. проверка утверждает ПРИСУТСТВИЕ конкретной личности -> сравнивать с её
//     идентификатором ("usr-alice"), а не с запасным значением;
//  3. предмет проверки — само запасное значение -> ей место рядом с аксессором,
//     в pkg/operations, и запись в contractOwners с обоснованием.
//
// Проверить, есть ли предмет вообще, можно подменой одного вызова в
// pkg/grpcsrv/cert_identity.go (ветка `case !trusted:`): WithoutPrincipal →
// WithPrincipal(SystemPrincipal()). Проверка, которая на этой подмене осталась
// зелёной, инварианта не держит.
//
// Известный предел (документируется, а не скрывается): гейт видит сравнение,
// где вызов SystemPrincipal() стоит В САМОМ сравнении. Вынесенный в переменную
// (`want := …SystemPrincipal(); if got != want`) он не увидит. Это floor, а не
// ceiling: молчание гейта не является доказательством, что проверка различает
// два состояния — это по-прежнему устанавливается чтением.
func TestNoBlindPrincipalAbsenceAssertions(t *testing.T) {
	root := repoRoot(t)

	var hits []string
	goFiles, testFiles, mentioning := 0, 0, 0
	walkGoFiles(t, root, func(rel string, body []byte) {
		goFiles++
		if !strings.HasSuffix(rel, "_test.go") {
			return
		}
		testFiles++
		if containsPath(contractOwners, rel) {
			return
		}
		// Упоминание имени — НЕ находка (оно бывает и в прозе), но это перепись
		// предмета: множество, из которого гейт вообще может что-то выбрать.
		if strings.Contains(string(body), "SystemPrincipal") {
			mentioning++
		}
		for _, line := range systemPrincipalComparisons(t, rel, body) {
			hits = append(hits, rel+":"+strconv.Itoa(line))
		}
	})

	// «Ноль находок» обязано быть отличимо от «ноль прочитанного»: без этой
	// строки молчание гейта нечем отличить от обхода, который никуда не дошёл.
	t.Logf("осмотрено: файлов .go %d, из них тестовых %d (освобождено %d); "+
		"тестовых файлов, упоминающих SystemPrincipal, %d; сравнений найдено %d",
		goFiles, testFiles, len(contractOwners), mentioning, len(hits))

	if goFiles == 0 || testFiles == 0 {
		t.Fatalf("обход не дошёл ни до одного тестового файла (.go %d, тестовых %d) — "+
			"это отказ, а не чистота: гейту нечего было судить", goFiles, testFiles)
	}

	if len(hits) > 0 {
		t.Errorf("найдено %d сравнений с operations.SystemPrincipal() в тестах:\n  %s\n\n"+
			"Это значение — ОБЩЕЕ наблюдаемое двух разных состояний: «личности не было» и "+
			"«системная личность выставлена явно». PrincipalFromContext их не различает "+
			"(так сказано в его godoc), поэтому утверждать им вычищенность носителя нельзя: "+
			"проверка останется зелёной, когда недоверенному отправителю системную личность "+
			"ВЫДАЛИ — а она владелец каждой системно записанной операции.\n\n"+
			"Исходы: утверждение об отсутствии -> PrincipalFromContextOK (признак наличия) / "+
			"утверждение о присутствии -> сравнение с самим идентификатором / предмет проверки "+
			"есть само запасное значение -> в pkg/operations + запись в contractOwners.",
			len(hits), strings.Join(hits, "\n  "))
	}
}

// TestContractOwnerExemptionsStillHaveSubject — исключение обязано умереть
// вместе со своим предметом.
//
// Освобождённый файл, из которого сравнения уже убрали, — тихая дыра в гейте:
// туда можно внести слепую проверку, и никто не возразит. Поэтому пустое
// исключение здесь считается ошибкой, а не «просто больше не нужно».
func TestContractOwnerExemptionsStillHaveSubject(t *testing.T) {
	root := repoRoot(t)

	for _, rel := range contractOwners {
		body, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Errorf("исключение %s: файл не читается (%v) — запись устарела, удали её "+
				"из contractOwners", rel, err)
			continue
		}
		if len(systemPrincipalComparisons(t, rel, body)) == 0 {
			t.Errorf("исключение %s больше не нужно: сравнений с SystemPrincipal() в файле "+
				"нет. Удали запись из contractOwners — иначе файл останется вне гейта и туда "+
				"можно будет внести слепую проверку незамеченной.", rel)
		}
	}
}

// TestPrincipalFallbackPremiseHolds — запрет выше опирается на факт, который
// может перестать быть верным, и тогда сам запрет станет вредным.
//
// Запрет обоснован ТОЛЬКО тем, что PrincipalFromContext действительно не
// различает вычищенный контекст и явно выставленную системную личность. Если
// аксессор когда-нибудь сузят (например, начнёт отдавать нулевую личность на
// контексте без принципала), сравнение с SystemPrincipal() снова станет
// осмысленным утверждением, и запрет придётся пересматривать — иначе он начнёт
// запрещать рабочую проверку.
//
// Без этой проверки запрет пережил бы своё обоснование молча. Гейт обязан
// падать на смене предпосылки, а не продолжать требовать своё.
//
// Вторая половина предпосылки не менее важна: аксессор, на который гейт
// ПЕРЕВОДИТ, обязан различать эти состояния. Если и он перестанет — переводить
// будет некуда, и требование гейта станет невыполнимым.
func TestPrincipalFallbackPremiseHolds(t *testing.T) {
	cleared := operations.WithoutPrincipal(context.Background())
	explicit := operations.WithPrincipal(context.Background(), operations.SystemPrincipal())

	// Половина первая: неразличающий аксессор действительно не различает.
	if operations.PrincipalFromContext(cleared) != operations.PrincipalFromContext(explicit) {
		t.Fatalf("PrincipalFromContext теперь РАЗЛИЧАЕТ вычищенный контекст и явно "+
			"выставленную системную личность (%+v vs %+v). Предпосылка запрета "+
			"(TestNoBlindPrincipalAbsenceAssertions) больше не выполняется: сравнение с "+
			"SystemPrincipal() снова что-то утверждает. Пересмотри запрет.",
			operations.PrincipalFromContext(cleared), operations.PrincipalFromContext(explicit))
	}

	// Половина вторая: аксессор, на который гейт переводит, различает.
	if _, ok := operations.PrincipalFromContextOK(cleared); ok {
		t.Fatalf("PrincipalFromContextOK сообщил наличие личности на ВЫЧИЩЕННОМ контексте — " +
			"гейту некуда переводить проверки, его требование стало невыполнимым")
	}
	if _, ok := operations.PrincipalFromContextOK(explicit); !ok {
		t.Fatalf("PrincipalFromContextOK сообщил отсутствие личности на явно выставленной " +
			"системной — перевод на него сделал бы проверки ложно-красными")
	}
}

// systemPrincipalComparisons возвращает номера строк, где вызов SystemPrincipal()
// участвует в сравнении: в операторе ==/!= либо в аргументе функции сравнения
// (Equal/NotEqual и их варианты).
//
// Разбор идёт по синтаксическому дереву, а не по тексту: упоминание в
// комментарии, в строковом литерале или передача SystemPrincipal() АРГУМЕНТОМ
// (WithPrincipal(ctx, SystemPrincipal()), CreateWithPrincipal(…)) — не сравнения
// и под запрет не попадают. Текстовый поиск здесь давал бы ложные срабатывания
// ровно на них.
func systemPrincipalComparisons(t *testing.T, rel string, body []byte) []int {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, body, 0)
	if err != nil {
		t.Fatalf("разбор %s: %v", rel, err)
	}

	var lines []int
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.BinaryExpr:
			if node.Op != token.EQL && node.Op != token.NEQ {
				return true
			}
			if callsSystemPrincipal(node.X) || callsSystemPrincipal(node.Y) {
				lines = append(lines, fset.Position(node.Pos()).Line)
			}
		case *ast.CallExpr:
			sel, ok := node.Fun.(*ast.SelectorExpr)
			if !ok || !assertionFuncRe.MatchString(sel.Sel.Name) {
				return true
			}
			for _, arg := range node.Args {
				if callsSystemPrincipal(arg) {
					lines = append(lines, fset.Position(node.Pos()).Line)
					break
				}
			}
		}
		return true
	})
	return lines
}

// callsSystemPrincipal — содержит ли выражение вызов SystemPrincipal() (в том
// числе как приёмник селектора: `operations.SystemPrincipal().ID`).
func callsSystemPrincipal(expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fn := call.Fun.(type) {
		case *ast.Ident:
			if fn.Name == "SystemPrincipal" {
				found = true
			}
		case *ast.SelectorExpr:
			if fn.Sel.Name == "SystemPrincipal" {
				found = true
			}
		}
		return !found
	})
	return found
}

func containsPath(list []string, rel string) bool {
	for _, p := range list {
		if p == rel {
			return true
		}
	}
	return false
}
