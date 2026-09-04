// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// TestEveryTokenVerifierTakesItsCompositionFromThePolicy — реализация, проверяющая
// подпись токена, обязана брать состав проверок из единого объявления (#902).
//
// # Предмет
//
// Один и тот же вопрос — «годен ли этот токен» — получал у нас разные ответы в
// зависимости от того, куда токен предъявлен: три независимые реализации, три
// состава проверок, и сверить их можно было только чтением.
//
// Расхождение устойчиво по трём причинам, и все три — про УМОЛЧАНИЯ:
// обязательность срока, ожидание адресата и издателя в известных библиотеках по
// умолчанию ВЫКЛЮЧЕНЫ; перечень допустимых алгоритмов действует, только если
// объявлен; параметр, помеченный отправителем обязательным, не обрабатывался
// ни одной из трёх.
//
// # Признак, по которому ищется реализация
//
// Разбор подписи: вызов `ParseWithClaims`/`jwt.Parse` либо собственный разбор
// компактного JWS. Такая функция обязана лежать в файле, чей пакет объявляет
// состав методом `DeclaredChecks` — то есть быть сверяемой с
// `tokenpolicy.MandatoryChecks` пробой своего пакета.
//
// # Чего гейт НЕ утверждает
//
// Он не проверяет, что объявленное исполняется: это свойство поведения, и его
// держат пробы самих проверяющих. Гейт держит ДРУГОЕ — что состав вообще
// объявлен и потому сверяем. Четвёртая реализация, написанная завтра, без
// объявления не пройдёт.
func TestEveryTokenVerifierTakesItsCompositionFromThePolicy(t *testing.T) {
	roots := []string{"../../gateway", "../../services", "../../pkg"}

	// Пакеты, где объявлен состав: путь каталога → true.
	declaring := map[string]bool{}
	// Пакеты, где разбирается подпись: путь каталога → файл-свидетель.
	verifying := map[string]string{}

	var scanned int
	for _, root := range roots {
		files, err := treecorpus.UnderWithSuffix(root, ".go")
		if err != nil {
			t.Fatalf("перечислить %s: %v", root, err)
		}
		for _, path := range files {
			if strings.HasSuffix(path, "_test.go") || strings.Contains(path, "/pkg/api/") {
				continue
			}
			scanned++
			fset := token.NewFileSet()
			f, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				continue
			}
			dir := path[:strings.LastIndex(path, "/")]
			if declaresChecks(f) {
				declaring[dir] = true
			}
			if parsesSignature(f) {
				verifying[dir] = path
			}
		}
	}

	var dirs []string
	for d := range verifying {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)

	t.Logf("осмотрено прод-файлов %d; пакетов, разбирающих подпись — %d; "+
		"пакетов, объявляющих состав — %d", scanned, len(verifying), len(declaring))

	if scanned == 0 {
		t.Fatal("корпус пуст — «ноль находок» здесь означало бы «ноль прочитанного»")
	}
	if len(verifying) == 0 {
		t.Fatal("ни одной реализации разбора подписи не распознано: признак разошёлся " +
			"с деревом, и гейт стал тождественно-зелёным")
	}

	for _, dir := range dirs {
		if !declaring[dir] {
			t.Errorf("%s: пакет разбирает подпись токена и НЕ объявляет состав "+
				"проверок (метод DeclaredChecks). Состав, заданный отдельно, "+
				"сверяется с соседним только чтением — а расходится молча "+
				"(свидетель: %s)", short(dir), short(verifying[dir]))
		}
	}
}

// declaresChecks — есть ли в файле метод DeclaredChecks.
func declaresChecks(f *ast.File) bool {
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if ok && fn.Name.Name == "DeclaredChecks" {
			return true
		}
	}
	return false
}

// parsesSignature — разбирает ли файл подпись токена.
//
// Два признака, и оба нужны: библиотечный разбор и собственный. Реализация,
// написанная на стандартной библиотеке, общей библиотеки JWT не использует
// вовсе — именно такой и была третья.
func parsesSignature(f *ast.File) bool {
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		// Собственный разбор зовёт примитив стандартной библиотеки НАПРЯМУЮ и
		// общей библиотеки JWT не использует вовсе — именно такой и была третья
		// реализация. Ловить только библиотечный разбор значило бы не видеть
		// целый вид предмета: «ноль находок» вместо «ноль прочитанного».
		if id, ok := call.Fun.(*ast.Ident); ok {
			if id.Name == "verifySignature" {
				found = true
			}
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg := ""
		if id, ok := sel.X.(*ast.Ident); ok {
			pkg = id.Name
		}
		switch sel.Sel.Name {
		case "ParseWithClaims", "VerifySignature":
			found = true
		case "Parse":
			if pkg == "jwt" {
				found = true
			}
		case "VerifyPKCS1v15", "VerifyPSS":
			if pkg == "rsa" {
				found = true
			}
		case "Verify":
			if pkg == "ecdsa" || pkg == "ed25519" {
				found = true
			}
		}
		return true
	})
	return found
}
