// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"sort"
	"strconv"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// stubgopackageplace.go — путь, ЗАПЕЧЁННЫЙ в дескриптор порождённого стаба,
// обязан совпадать с путём импорта, который следует из места файла.
//
// # Предмет, и почему он появился именно сейчас
//
// Фундамент уезжает в отдельный репозиторий (`release-and-versioning.md` §1.1),
// то есть путь импорта каждого переезжающего пакета МЕНЯЕТСЯ. Импорты правятся
// текстом — их читает компилятор, и ошибка немедленно красная. С порождёнными
// стабами так НЕ выйдет: `go_package` живёт у них не только в объявлении
// импорта, а вторым экземпляром — внутри сериализованного дескриптора, в том же
// файле, строковым литералом.
//
// Оба исхода текстовой правки этого литерала измерены опытом (одно-фактная
// инъекция: менялись только байты `go_package`, всё остальное цело):
//
//	правка МЕНЯЕТ длину строки   → префикс длины разошёлся с телом:
//	                               panic: slice bounds out of range [-1:]
//	                               в filedesc.(*File).unmarshalSeed, при init
//	правка НЕ менялась вовсе     → дескриптор объявляет ПРЕЖНИЙ путь,
//	                               файл лежит по НОВОМУ; всё собирается,
//	                               все пробы зелены, регистрация проходит
//
// Первый исход громкий и ловится любым прогоном. Второй — МОЛЧАЛИВЫЙ, и он же
// вероятный: переезд каталога дешевле, чем регенерация, поэтому соблазн
// перенести стабы «как есть» велик. Цена молчания: очередная генерация пишет
// файл обратно по запечённому пути — то есть возвращает стаб в прежний модуль,
// а всякий, кто выводит путь импорта из дескриптора, получает несуществующую
// координату.
//
// # Почему это НЕ ловится существующими проверками
//
//	сборка и пробы            видят объявление импорта, дескриптор для них данные
//	сверка порождённого       сравнивает ИЗМЕНЁННОЕ; согласованно перенесённый
//	  с деревом                 стаб ничего не меняет
//	покрытие генерации        судит, назван ли корень контракта во входах, а не
//	                            куда порождённое ложится
//
// # Гейт читает ДЕСКРИПТОР, а не текст
//
// Путь `github.com/PRO-Robotech/kacho/pkg/...` встречается в этом дереве
// тысячами — в импортах, в прозе, в шапке самого этого файла, — поэтому проверка
// по подстроке краснела бы на собственном объяснении. Здесь литерал `rawDesc`
// берётся УЗЛОМ разбора, разворачивается `strconv.Unquote` и разбирается как
// `FileDescriptorProto`; судится поле `options.go_package`, а не совпадение
// строк.
//
// # Что держит этот гейт, а что РАНТАЙМ — измерено, а не поделено на глаз
//
// Обе инъекции прогнаны на настоящем дереве, каждая одним фактом:
//
//	громкий исход    гейт не высказывается ВОВСЕ: стаб фундамента лежит в графе
//	                 импортов самого прогона, поэтому двоичное падает при init —
//	                 до первой строки любого теста (0.006 с, panic из
//	                 filedesc.unmarshalSeed). Держит РАНТАЙМ, и держит надёжно
//	молчаливый исход `go build` пакета выходит УСПЕХОМ, все пробы зелены, а гейт
//	                 краснеет и называет файл, оба пути и модуль
//
// То есть предмет гейта — вторая строка, и только она. Ветвь «дескриптор не
// разбирается» оставлена не ради громкого исхода, а ради стаба ВНЕ графа
// прогона: там init не наступает, и падать нечему. Прежняя редакция этой шапки
// обещала, что гейт называет и громкий исход, — инъекция это опровергла.
//
// # Стаб БЕЗ дескриптора — не находка, но и не невидимка
//
// Транспортные стабы (`*_grpc.pb.go`) дескриптора не несут вовсе: у них нечего
// сверять. Они считаются ОТДЕЛЬНОЙ строкой переписи, потому что «ноль
// расхождений» обязано быть отличимо от «ноль прочитанных дескрипторов»: если
// извлечение литерала однажды перестанет работать, все файлы уедут в эту графу,
// и без её числа гейт останется зелёным, не прочитав ни одного дескриптора.

// stubGoPackageSite — одно наблюдение: что запечено в дескрипторе стаба и что
// следует из его места.
type stubGoPackageSite struct {
	File     string // путь файла от корня дерева
	Baked    string // go_package из дескриптора, без суффикса `;alias`
	Expected string // путь импорта, следующий из места файла
	Module   string // путь модуля, которому файл принадлежит
}

// stubGoPackageCensus — объём осмотренного. Печатается ВСЕГДА.
type stubGoPackageCensus struct {
	FilesRead         int // стабов прочитано
	WithDescriptor    int // из них несут дескриптор и он разобран
	WithoutDescriptor int // из них дескриптора не несут (транспортные стабы)
	Unparsable        int // дескриптор есть, но не разбирается
	Modules           int // различных модулей среди наблюдений
}

func (c stubGoPackageCensus) String() string {
	return fmt.Sprintf("перепись: стабов прочитано %d · с дескриптором %d · "+
		"без дескриптора %d · неразбираемых %d · модулей %d",
		c.FilesRead, c.WithDescriptor, c.WithoutDescriptor, c.Unparsable, c.Modules)
}

// judgeStubGoPackagePlace — вердикт: у каждого наблюдения запечённый путь равен
// пути, который следует из места файла.
//
// Пустой обход — НАХОДКА, а не зелёное: гейт, не прочитавший ни одного
// дескриптора, о дереве не высказался.
func judgeStubGoPackagePlace(sites []stubGoPackageSite, unparsable []string, filesRead, withoutDescriptor int) ([]string, stubGoPackageCensus) {
	census := stubGoPackageCensus{
		FilesRead:         filesRead,
		WithDescriptor:    len(sites),
		WithoutDescriptor: withoutDescriptor,
		Unparsable:        len(unparsable),
	}

	mods := map[string]struct{}{}
	for _, s := range sites {
		mods[s.Module] = struct{}{}
	}
	census.Modules = len(mods)

	var faults []string

	if filesRead == 0 {
		faults = append(faults, "обход пуст: ни одного порождённого стаба не прочитано — "+
			"вердикт беспредметен, «ноль расхождений» здесь означает «ноль прочитанного»")
		return faults, census
	}
	if len(sites) == 0 {
		faults = append(faults, fmt.Sprintf("прочитано стабов %d, а дескриптор не разобран НИ У ОДНОГО — "+
			"извлечение литерала `rawDesc` перестало работать; зелёное здесь было бы вакуумным",
			filesRead))
		return faults, census
	}

	for _, f := range unparsable {
		faults = append(faults, fmt.Sprintf("%s: дескриптор не разбирается — так выглядит ТЕКСТОВАЯ правка "+
			"`go_package` внутри байтов: префикс длины разошёлся с телом. Путь стаба меняется "+
			"РЕГЕНЕРАЦИЕЙ из контракта, а не заменой в файле", f))
	}

	ordered := append([]stubGoPackageSite(nil), sites...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].File < ordered[j].File })
	for _, s := range ordered {
		if s.Baked == s.Expected {
			continue
		}
		faults = append(faults, fmt.Sprintf("%s: дескриптор объявляет путь %q, а место файла даёт %q "+
			"(модуль %q). Стаб перенесён без регенерации: следующая генерация запишет его "+
			"обратно по запечённому пути, а путь импорта, выведенный из дескриптора, не резолвится",
			s.File, s.Baked, s.Expected, s.Module))
	}

	return faults, census
}

// expectedImportPath — путь импорта, следующий из места файла: путь модуля плюс
// путь каталога файла относительно корня модуля.
//
// Единица — КАТАЛОГ, а не файл: пакет Go адресуется каталогом.
func expectedImportPath(modulePath, moduleDir, file string) string {
	rel := strings.TrimPrefix(strings.TrimPrefix(file, moduleDir), "/")
	dir := path.Dir(rel)
	if dir == "." || dir == "" {
		return modulePath
	}
	return modulePath + "/" + dir
}

// rawDescriptorLiteral — литерал `rawDesc` стаба, взятый УЗЛОМ разбора.
//
// Второе значение false означает «дескриптора в этом файле нет» — законное
// состояние транспортного стаба, а не отказ.
func rawDescriptorLiteral(src []byte, name string) ([]byte, bool, error) {
	fset := token.NewFileSet()
	af, err := parser.ParseFile(fset, name, src, 0)
	if err != nil {
		return nil, false, fmt.Errorf("разбор %s: %w", name, err)
	}

	var raw string
	found := false
	ast.Inspect(af, func(n ast.Node) bool {
		if found {
			return false
		}
		vs, ok := n.(*ast.ValueSpec)
		if !ok || len(vs.Names) != 1 || len(vs.Values) != 1 {
			return true
		}
		// Имя объявления порождается protoc-gen-go и всегда оканчивается
		// `_rawDesc`; соседние `_rawDescOnce`/`_rawDescData`/`_rawDescGZIP`
		// суффиксу не удовлетворяют и потому не перехватывают отбор.
		if !strings.HasSuffix(vs.Names[0].Name, "_rawDesc") {
			return true
		}
		var b strings.Builder
		collectStringLiterals(vs.Values[0], &b)
		raw = b.String()
		found = true
		return false
	})
	if !found || raw == "" {
		return nil, false, nil
	}
	return []byte(raw), true, nil
}

// collectStringLiterals — конкатенация строковых литералов выражения.
//
// protoc-gen-go эмитит `rawDesc` как `"" + "…" + "…"`, то есть дерево
// BinaryExpr, а не один литерал: брать первый узел значило бы прочитать пустую
// строку и объявить стаб бездескрипторным.
func collectStringLiterals(e ast.Expr, out *strings.Builder) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind == token.STRING {
			if s, err := strconv.Unquote(v.Value); err == nil {
				out.WriteString(s)
			}
		}
	case *ast.BinaryExpr:
		collectStringLiterals(v.X, out)
		collectStringLiterals(v.Y, out)
	case *ast.ParenExpr:
		collectStringLiterals(v.X, out)
	}
}

// goPackageOfDescriptor — `options.go_package` дескриптора, без суффикса
// `;alias`.
func goPackageOfDescriptor(raw []byte) (string, error) {
	var fd descriptorpb.FileDescriptorProto
	if err := proto.Unmarshal(raw, &fd); err != nil {
		return "", err
	}
	gp := fd.GetOptions().GetGoPackage()
	if i := strings.IndexByte(gp, ';'); i >= 0 {
		gp = gp[:i]
	}
	return gp, nil
}
