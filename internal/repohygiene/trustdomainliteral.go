// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// trustdomainliteral.go — разбор мест, где домен доверия ОБЪЯВЛЕН КОДОМ
// (приёмка KAN-WIRE-1, сценарий KAN-W4-01, предмет `ПР-4`).
//
// # Предмет
//
// Домен доверия — то, чьи сертификаты установка признаёт своими, а значит и
// круг тех, кто вправе ГОВОРИТЬ ЗА пользователя. Половина посадки настраивается
// честно: домен подставляется величиной профиля, и правка величины даёт
// сертификаты нового домена. Но ЧИТАЛИ эти сертификаты скомпилированные
// константы, ни одна из которых величину не читала. Значит переход только
// значениями оставлял выпускающую сторону новой, а принимающую — прежней, и
// расходились они молча: законный отправитель переставал опознаваться, а отказ
// выглядел как отсутствие личности.
//
// # Что здесь считается ОБЪЯВЛЕНИЕМ ДОМЕНА, а что прозой
//
//	const p = "spiffe://kacho.cloud/ns/"        ← объявление: домен как значение
//	grpcsrv.NewTrustDomain("kacho.cloud")       ← объявление: домен как аргумент
//	// SAN имеет вид spiffe://<домен>/ns/…      ← ПРОЗА: комментарий не исполняется
//	grpcsrv.NewTrustDomain(cfg.TrustDomain)     ← ПОТРЕБИТЕЛЬ: величина приезжает
//
// Разбор судит УЗЛЫ разобранного исходника — строковый литерал и вызов, — а не
// текст файла. Гейт по подстроке краснел бы на собственном объяснении и на
// каждом комментарии, описывающем форму SAN; такие комментарии законны и обязаны
// остаться, иначе форму личности негде прочитать.
//
// # Формы, которые разбор ЗНАЕТ — обе, и обе доказаны инъекцией
//
//  1. **домен в литерале** — строковый литерал, содержащий `spiffe://` с
//     КОНКРЕТНОЙ властью после схемы. Власть опознаётся СТРУКТУРНО, поэтому
//     переименование домена гейт не обманывает: он ловит и `kacho.cloud`, и
//     любое будущее написание, включая то, которого сегодня в дереве нет.
//     ФОРМА личности (`spiffe://`, `spiffe://%s/ns/`, `spiffe://<домен>/ns/…`)
//     домена не несёт и находкой не является нигде: запрети её — и запретишь
//     разбор личности вместе с прозой о нём, включая собственное объяснение
//     этого файла;
//  2. **домен литералом в конструкторе** — `NewTrustDomain("…")` с литеральным
//     аргументом. Без этой формы голая власть `"kacho.cloud"` вернула бы дефект
//     в написании, о котором распознаватель не знает: находки не было бы ни
//     красной, ни зелёной — было бы молчание.
//
// Вызов приводится к ПОЛНОМУ ПУТИ ИМПОРТА, а не к имени пакета: псевдоним
// (`gs "…/pkg/grpcsrv"`) пишется так же коротко, и разбор по имени обойти можно
// было бы одной буквой в объявлении импорта.
//
// # Чего разбор НЕ видит — названо, а не спрятано
//
//  1. **склейку по частям** — `"spiffe" + "://" + d`. Разбор судит литерал, а не
//     поток значений; такой формы в дереве ноль;
//  2. **точечный импорт** владельца (`import . "…/pkg/grpcsrv"`) — вызов пишется
//     голым именем, и привести его к пути импорта нечем. В дереве такого импорта
//     ноль, и форма пропускается сознательно;
//  3. **голую власть вне конструктора** — `const d = "kacho.cloud"`, переданную
//     дальше через переменную. Собрать из неё личность нельзя, не написав схему,
//     а схема — форма 1.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
)

const (
	// TrustDomainOwnerDir — каталог, где живёт тип домена доверия и разбор
	// личности. Читается ПРЕДПОСЫЛКОЙ гейта, а не запретом: объявление формы
	// личности здесь и есть свидетельство того, что предмет ещё существует.
	TrustDomainOwnerDir = "pkg/grpcsrv/"
	// TrustDomainOwnerImport — путь импорта того же владельца. По нему вызов
	// конструктора опознаётся независимо от псевдонима.
	TrustDomainOwnerImport = "github.com/PRO-Robotech/kacho/pkg/grpcsrv"
	// TrustDomainConstructor — имя конструктора домена доверия.
	TrustDomainConstructor = "NewTrustDomain"

	// spiffeScheme — схема, по которой опознаётся личность сертификата. Стоит
	// здесь как ПРЕДМЕТ ЗАПРЕТА, и это одно из мест, выведенных из-под запрета на
	// подробности: проверка, не называющая, что она запрещает, будет снята
	// следующим как непонятная.
	spiffeScheme = "spiffe://"
)

// TrustDomainSite — координата места, где домен доверия объявлен кодом.
type TrustDomainSite struct {
	File string
	Line int
	// Form — форма записи: `literal` (схема в строковом литерале) либо
	// `constructor` (домен литеральным аргументом конструктора).
	Form string
	// Value — то, что прочитано: значение литерала. Находка без него не
	// чинится — читателю нечего искать.
	Value string
	// Authority — власть, стоящая сразу после схемы (для формы `literal`), либо
	// сам домен (для формы `constructor`). Пусто, когда власти нет.
	Authority string
	// AuthorityIsConcrete — власть названа ДОМЕНОМ, а не заполнителем. Форма
	// личности (`spiffe://<домен>/ns/…`, `spiffe://%s/ns/`) домена не несёт и
	// установку ни к чему не обязывает; домен несёт только конкретная власть.
	AuthorityIsConcrete bool
}

// TrustDomainCensus — объём осмотренного одним файлом. Печатается вызывающим,
// чтобы «ноль находок» было отличимо от «ноль прочитанного».
type TrustDomainCensus struct {
	Literals int
	Calls    int
	Imports  int
	// DotImports — точечные импорты владельца: форма, которую разбор не видит
	// (см. шапку). Считается, чтобы её отсутствие было утверждением, а не
	// умолчанием.
	DotImports int
}

// ScanTrustDomainLiterals разбирает один исходник и возвращает ВСЕ места, где
// домен доверия объявлен кодом, вместе с объёмом осмотренного.
//
// Разбор возвращает ФАКТЫ, а не вердикт: место в пакете-владельце тоже приходит
// сюда, и законным его делает вызывающий (см. `trustdomainliteral_test.go`).
// Иначе предпосылка гейта — «схему владелец вообще несёт» — проверялась бы
// вторым проходом, то есть вторым разбором об одном предмете.
//
// Обе стороны — и находки, и перепись — возвращаются всегда: вызывающий обязан
// уметь отличить «прочитал и не нашёл» от «не прочитал».
func ScanTrustDomainLiterals(path string, src []byte) (sites []TrustDomainSite, census TrustDomainCensus, err error) {
	fset := token.NewFileSet()
	f, perr := parser.ParseFile(fset, path, src, 0)
	if perr != nil {
		return nil, TrustDomainCensus{}, perr
	}

	ownerAliases := map[string]bool{}
	for _, imp := range f.Imports {
		census.Imports++
		p, uerr := strconv.Unquote(imp.Path.Value)
		if uerr != nil || p != TrustDomainOwnerImport {
			continue
		}
		name := TrustDomainOwnerImport[strings.LastIndex(TrustDomainOwnerImport, "/")+1:]
		if imp.Name != nil {
			switch imp.Name.Name {
			case ".":
				census.DotImports++
				continue
			case "_":
				continue
			default:
				name = imp.Name.Name
			}
		}
		ownerAliases[name] = true
	}
	// Файл САМОГО владельца зовёт конструктор без квалификатора.
	inOwner := strings.HasPrefix(path, TrustDomainOwnerDir)

	ast.Inspect(f, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.BasicLit:
			if node.Kind != token.STRING {
				return true
			}
			census.Literals++
			value, uerr := strconv.Unquote(node.Value)
			if uerr != nil || !strings.Contains(value, spiffeScheme) {
				return true
			}
			auth := authorityAfterScheme(value)
			sites = append(sites, TrustDomainSite{
				File: path, Line: fset.Position(node.Pos()).Line,
				Form: "literal", Value: value, Authority: auth,
				AuthorityIsConcrete: authorityIsConcrete(auth),
			})
		case *ast.CallExpr:
			census.Calls++
			if !callsTrustDomainConstructor(node, ownerAliases, inOwner) {
				return true
			}
			lit, ok := node.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			value, uerr := strconv.Unquote(lit.Value)
			if uerr != nil || strings.TrimSpace(value) == "" {
				// Пустая строка — не объявление домена, а его отсутствие; её
				// исход решает страж старта, а не этот разбор.
				return true
			}
			sites = append(sites, TrustDomainSite{
				File: path, Line: fset.Position(lit.Pos()).Line,
				Form: "constructor", Value: value, Authority: value,
				AuthorityIsConcrete: authorityIsConcrete(value),
			})
		}
		return true
	})
	return sites, census, nil
}

// callsTrustDomainConstructor — вызов ли это конструктора домена доверия.
func callsTrustDomainConstructor(call *ast.CallExpr, ownerAliases map[string]bool, inOwner bool) bool {
	if len(call.Args) != 1 {
		return false
	}
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return inOwner && fn.Name == TrustDomainConstructor
	case *ast.SelectorExpr:
		pkg, ok := fn.X.(*ast.Ident)
		return ok && ownerAliases[pkg.Name] && fn.Sel.Name == TrustDomainConstructor
	}
	return false
}

// authorityAfterScheme — власть, стоящая сразу после схемы: то, что и есть
// домен доверия. Пусто, когда за схемой ничего не стоит.
func authorityAfterScheme(value string) string {
	i := strings.Index(value, spiffeScheme)
	if i < 0 {
		return ""
	}
	rest := value[i+len(spiffeScheme):]
	if j := strings.IndexByte(rest, '/'); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

// authorityIsConcrete — названа ли власть ДОМЕНОМ, а не заполнителем.
//
// Заполнитель — то, что домена не несёт и на месте домена стоять не может:
// пустая власть (`spiffe://`), глагол форматирования (`%s`), угловая скобка
// (`<домен-доверия>`), фигурная (`{{ … }}` шаблона посадки) и знак подстановки
// оболочки (`$TRUST_DOMAIN`). Проза, называющая ФОРМУ личности, законна и
// обязана остаться: из неё оператор читает, что писать в круге отправителей.
func authorityIsConcrete(authority string) bool {
	if authority == "" {
		return false
	}
	switch authority[0] {
	case '%', '<', '{', '$':
		return false
	}
	// Многоточие — та же проза: `spiffe://…/sa/kacho-api-gateway` домена не
	// называет, а показывает, где он стоит.
	return !strings.HasPrefix(authority, "…") && !strings.HasPrefix(authority, "...")
}
