// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// clientaddressreader.go — У АДРЕСА КЛИЕНТА В КРАЕ ОДИН ЧИТАТЕЛЬ (kacho#3028, C2 и C3).
//
// # Предмет
//
// Адрес клиента край выводит из заголовков пересылки и кормит им три места:
// условие `client_ip` модели прав (HTTP и нативный gRPC) и `X-Forwarded-For`
// ретрансляции полосы входа, по которому служба доступа ведёт ограничение
// частоты «на источник». Предикат доверия к пиру — звено ли он — у них обязан
// быть один: второй читатель заголовка, со своим предикатом либо без него,
// разошёлся бы с первым молча, и подделка проходила бы через тот, что слабее.
//
// Поэтому заголовки пересылки (`X-Forwarded-For`, `X-Real-IP`, `Forwarded`)
// в прод-дереве края читаются ТОЛЬКО в функциях-читателях оператора адреса
// (clientAddressReaders), а запись (`Set`/`Add`/`Del` по первому аргументу)
// разрешена где угодно — запись не решает, кому верить.
//
// Метаданные моста `grpcgateway-*` с этими именами не читаются нигде: их
// пишет только наш мост в процессе, а на нативный слушатель мост не ходит —
// там их пишет клиент (C3).
//
// # Что судится
//
// Узел синтаксического дерева: строковый литерал с одним из имён (регистр не
// важен). Комментарий литералом не является и не судится. Литерал в
// объявлении константы или переменной — чтение по построению: имя затем
// уходит в вызов, которого гейт по литералу уже не увидит.
//
// # Объём осмотренного
//
// Перепись печатает число прочитанных файлов, найденных литералов, записей и
// чтений в читателях; пустой обход и читатель без единого чтения — отказ
// предпосылки, а не зелёный.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
)

// clientAddressHeaderNames — имена заголовков пересылки в нижнем регистре.
var clientAddressHeaderNames = []string{"x-forwarded-for", "x-real-ip", "forwarded"}

// clientAddressBridgePrefix — приставка метаданных моста.
const clientAddressBridgePrefix = "grpcgateway-"

// clientAddressReaders — единственное законное место чтения: файл (путь от
// корня репозитория) → имена функций-читателей в нём.
var clientAddressReaders = map[string][]string{
	"gateway/internal/middleware/context_extractor.go": {"httpForwarded", "grpcForwarded"},
}

// clientAddressStrips — СНЯТИЕ адреса источника на выходе края (круг 5): файл
// → имя переменной-перечня ключей, которые край снимает с исходящего вызова к
// службе. Имя в перечне снятия не читает адрес, а убирает его — третий
// законный вид литерала рядом с чтением в читателе и записью. Метаданные
// моста литералом в перечне не пишутся: снятие формы моста выводится из
// голого имени (приставка), и литерал моста — по-прежнему находка C3.
var clientAddressStrips = map[string][]string{
	"gateway/internal/principalmeta/forwarded_address.go": {"forwardedAddressKeys"},
}

// clientAddressCensus — объём осмотренного.
type clientAddressCensus struct {
	Files, Literals, Writes, ReaderReads, Strips int
}

func (c clientAddressCensus) Summary() string {
	return fmt.Sprintf("файлов %d · литералов имён пересылки %d · записей %d · чтений в читателе %d · снятий на выходе %d",
		c.Files, c.Literals, c.Writes, c.ReaderReads, c.Strips)
}

// clientAddressHeaderKind — имя ли это заголовка пересылки и метаданные ли моста.
func clientAddressHeaderKind(lit string) (match, bridge bool) {
	v := strings.ToLower(strings.TrimSpace(lit))
	if rest, ok := strings.CutPrefix(v, clientAddressBridgePrefix); ok {
		v, bridge = rest, true
	}
	for _, n := range clientAddressHeaderNames {
		if v == n {
			return true, bridge
		}
	}
	return false, false
}

// judgeClientAddressReaders судит один файл: rel — путь от корня, src —
// содержимое. Возвращает находки с координатой и пополняет перепись.
func judgeClientAddressReaders(rel string, src []byte, census *clientAddressCensus) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("%s: разбор: %w", rel, err)
	}
	census.Files++
	allowed := map[string]bool{}
	for _, fn := range clientAddressReaders[rel] {
		allowed[fn] = true
	}
	strips := map[string]bool{}
	for _, v := range clientAddressStrips[rel] {
		strips[v] = true
	}
	var findings []string
	var stack []ast.Node
	// inStrip — лежит ли узел внутри объявления переменной-перечня снятия.
	inStrip := func() bool {
		for i := len(stack) - 1; i >= 0; i-- {
			if vs, ok := stack[i].(*ast.ValueSpec); ok {
				for _, n := range vs.Names {
					if strips[n.Name] {
						return true
					}
				}
				return false
			}
		}
		return false
	}
	enclosingFunc := func() string {
		for i := len(stack) - 1; i >= 0; i-- {
			if fd, ok := stack[i].(*ast.FuncDecl); ok {
				return fd.Name.Name
			}
		}
		return ""
	}
	enclosingVar := func() string {
		for i := len(stack) - 1; i >= 0; i-- {
			if vs, ok := stack[i].(*ast.ValueSpec); ok && len(vs.Names) > 0 {
				return ", переменная " + vs.Names[0].Name
			}
		}
		return ""
	}
	isWriteArg := func(lit *ast.BasicLit) bool {
		if len(stack) < 2 {
			return false
		}
		call, ok := stack[len(stack)-2].(*ast.CallExpr)
		if !ok || len(call.Args) == 0 || call.Args[0] != lit {
			return false
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		switch sel.Sel.Name {
		case "Set", "Add", "Del":
			return true
		}
		return false
	}
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		stack = append(stack, n)
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		v, uErr := strconv.Unquote(lit.Value)
		if uErr != nil {
			return true
		}
		match, bridge := clientAddressHeaderKind(v)
		if !match {
			return true
		}
		census.Literals++
		pos := fset.Position(lit.Pos())
		where := fmt.Sprintf("%s:%d", rel, pos.Line)
		switch {
		case bridge && !isWriteArg(lit):
			findings = append(findings, fmt.Sprintf("%s: метаданные моста %q читаются — на нативном gRPC их пишет клиент (C3)", where, v))
		case isWriteArg(lit):
			census.Writes++
		case inStrip():
			census.Strips++
		case allowed[enclosingFunc()]:
			census.ReaderReads++
		default:
			findings = append(findings, fmt.Sprintf("%s: заголовок пересылки %q читается вне оператора адреса (функция %q%s) — второй читатель адреса клиента (C2)", where, v, enclosingFunc(), enclosingVar()))
		}
		return true
	})
	return findings, nil
}
