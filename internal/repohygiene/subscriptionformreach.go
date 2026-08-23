// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// subscriptionformreach.go — анализатор «у объявленной формы есть, кому её взять».
//
// # Предмет
//
// Общая форма подписки объявлена фазой, которая НЕ объявляет глагола: сервер её
// берёт следующей фазой (kacho#1018). До тех пор у ВСЕХ её типов верхнего уровня
// ноль ссылок — и это законное, СРОЧНОЕ состояние, а не мёртвый код.
//
// Отличие от мёртвого кода названо приёмкой прямо, и оно ровно в двух вещах:
// у похороненной поверхности не было НИ СРОКА, НИ ПРЕДМЕТА, который её возьмёт;
// у этой формы есть и то и другое. Значит послабление обязано истекать ОТ
// ЗАКРЫТИЯ ПРЕДМЕТА, а не от чьей-то памяти: закрыли задачу-берущего, а ссылок
// так и ноль — форму надлежит снять, а не оставить «на будущее» (ban #11: три
// исхода, и «полежит» среди них нет).
//
// # Почему это ОТДЕЛЬНЫЙ механизм, а не запись в ведомости транспортных сообщений
//
// Ведомость транспортных сообщений судит по СУФФИКСУ ИМЕНИ (`Request`,
// `Response`, `Metadata`) и потому видит ОДИН тип: `SubscriptionOpened`,
// `SubscriptionEvent` и перечисление `SubscriptionAnchor` вне её предмета by
// construction — не по недосмотру, а по устройству её дискриминатора. Этот
// анализатор судит по ПАКЕТУ и покрывает их ВСЕ, включая перечисления: их он
// собирает наравне с сообщениями (`subReachEnumRe`), поэтому «типов» здесь
// означает типы верхнего уровня любого рода, а не только сообщения.
//
// Числа тут намеренно нет: состав формы меняется вместе с фазой, а перепись
// печатает его на КАЖДОМ прогоне («типов пакета … объявлено N») — на сегодняшнем
// дереве это перечисление `SubscriptionAnchor` и три сообщения. DoD фазы требует
// оба механизма и прямо называет подмену одного другим ошибкой: она сузила бы
// наблюдение со всей формы до одного типа.
//
// # Направление истечения у двух механизмов РАЗНОЕ, и это несущее
//
// Ведомость транспортных сообщений истекает ПО УСПЕХУ: объявит следующая фаза
// глагол — сообщение окажется названо глаголом, и запись уронит прогон как
// истёкшая. Этот гейт срабатывает ПО ОТКАЗУ ОТ ПРЕДМЕТА: задача закрыта, а
// ссылок нет. Ни один из двух не заменяет другого, потому что они ловят
// противоположные исходы одного ожидания.
//
// # Кто считается ссылкой, а кто нет
//
//	ДА   другой контракт дерева, называющий тип полным именем
//	     (`kacho.cloud.subscription.SubscriptionRequest`);
//	ДА   прод-код Go, импортирующий сгенерённый пакет ИМЕНОВАННО и
//	     употребляющий тип.
//	НЕТ  сами файлы общего пакета — тип не ссылается на себя;
//	НЕТ  сгенерённые стабы (`pkg/api/`) — они существуют оттого, что объявление
//	     есть, и потому ссылкой на его нужность не являются;
//	НЕТ  пробы (`_test.go`) — проба живёт ради гейта, а гейт ради этой формы:
//	     засчитать её значило бы замкнуть наблюдение на само себя;
//	НЕТ  ПУСТОЙ импорт (`_ "…"`) в любом файле — он наполняет реестр
//	     дескрипторов и об употреблении типа не говорит ничего.
//
// Последние два исключения — не педантизм: на сегодняшнем дереве пустых
// импортов сгенерённого пакета ровно два, оба в пробах, и без этих двух правил
// гейт был бы зелёным навсегда, ничего при этом не наблюдая.
//
// # Пустой обход — отказ
//
// Ноль прочитанных контрактов, ноль объявленных типов общего пакета — и «ноль
// находок» становится неотличимо от «ноль прочитанного».
package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// SubscriptionGoPackage — импортный путь сгенерённого пакета общей формы.
const SubscriptionGoPackage = "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"

// SubscriptionGoDefaultAlias — имя пакета Go по умолчанию (из `go_package`).
const SubscriptionGoDefaultAlias = "subscriptionv1"

// SubscriptionFormTaker — задача, чья работа состоит в том, чтобы форму ВЗЯТЬ.
//
// Номер стоит здесь, а не в тексте находки, потому что он же печатается
// переписью на КАЖДОМ прогоне: «сверено 0» никогда не должно выглядеть как
// «сверено и в порядке».
const SubscriptionFormTaker = 1018

// SubscriptionFormType — один объявленный тип общего пакета и его ссылки.
type SubscriptionFormType struct {
	// Name — имя типа верхнего уровня (`SubscriptionRequest`).
	Name string
	// Kind — "message" | "enum".
	Kind string
	// Where — файл контракта, где тип объявлен (путь относительно корня).
	Where string
	// ProtoReferrers — контракты дерева, называющие тип полным именем.
	ProtoReferrers []string
	// GoReferrers — прод-файлы Go, употребляющие тип.
	GoReferrers []string
}

// Referrers — сколько ссылок у типа всего.
func (t SubscriptionFormType) Referrers() int {
	return len(t.ProtoReferrers) + len(t.GoReferrers)
}

// SubscriptionReachOptions — вход анализатора.
type SubscriptionReachOptions struct {
	// Root — корень репозитория.
	Root string
	// ProtoRoot — путь (относительно Root) к дереву исходного контракта.
	ProtoRoot string
	// GoRoots — каталоги (относительно Root), в которых ищутся ссылки из кода.
	// Пустой список означает «весь корень».
	GoRoots []string
}

// SubscriptionReachCensus — то, что анализатор прочитал.
type SubscriptionReachCensus struct {
	ProtoFiles int
	GoFiles    int
	// CommonTypes — типов верхнего уровня объявлено общим пакетом.
	CommonTypes int
	// Unreferenced — из них с нулём ссылок.
	Unreferenced int
	// BlankImports — пустых импортов сгенерённого пакета (в счёт ссылок НЕ идут,
	// но печатаются: иначе их отсутствие в переписи выглядело бы как их отсутствие
	// в дереве).
	BlankImports int
	// TestReferrers — употреблений типа в пробах (в счёт ссылок НЕ идут).
	TestReferrers int
	// GoUnparsed — файлов Go, которые не разобрались. Печатаются, а не
	// проглатываются: непрочитанный файл не есть отсутствие ссылки.
	GoUnparsed int
}

var (
	subReachEnumRe = regexp.MustCompile(`\benum\s+([A-Za-z0-9_]+)`)
)

// AuditSubscriptionFormReach читает дерево и возвращает состав типов общего
// пакета вместе с их ссылками.
//
// Вердикта («находка это или нет») он НЕ выносит: вердикт зависит от состояния
// задачи-берущего, а это измерение сетевое. Решение живёт отдельной чистой
// функцией `SubscriptionReachFindings`, чья способность упасть доказывается без
// сети.
func AuditSubscriptionFormReach(
	opts SubscriptionReachOptions, out io.Writer,
) ([]SubscriptionFormType, SubscriptionReachCensus, error) {
	var c SubscriptionReachCensus

	byName := map[string]*SubscriptionFormType{}
	var order []string
	// commonFiles — файлы САМОГО общего пакета: ссылка типа на себя ссылкой не
	// является, поэтому они исключаются из поиска употреблений.
	commonFiles := map[string]bool{}

	// ── проход 1: что объявлено общим пакетом ────────────────────────────────
	err := rootedWalk(filepath.Join(opts.Root, opts.ProtoRoot), func(rel string) bool {
		return strings.HasSuffix(rel, ".proto")
	}, func(path string, b []byte) error {
		c.ProtoFiles++
		clean := stripProtoComments(string(b))
		m := protoPackageRe.FindStringSubmatch(clean)
		if m == nil || m[1] != SubscriptionCommonPackage {
			return nil
		}
		rel := subRelTo(opts.Root, path)
		commonFiles[rel] = true
		for _, kind := range []struct {
			re   *regexp.Regexp
			name string
		}{{subMessageRe, "message"}, {subReachEnumRe, "enum"}} {
			for _, loc := range kind.re.FindAllStringSubmatchIndex(clean, -1) {
				depth := strings.Count(clean[:loc[0]], "{") - strings.Count(clean[:loc[0]], "}")
				if depth != 0 {
					continue // вложенный тип адресуется через внешний — отдельной ссылки не имеет
				}
				name := clean[loc[2]:loc[3]]
				if _, seen := byName[name]; seen {
					continue
				}
				byName[name] = &SubscriptionFormType{Name: name, Kind: kind.name, Where: rel}
				order = append(order, name)
			}
		}
		return nil
	})
	if err != nil {
		return nil, c, err
	}
	c.CommonTypes = len(byName)

	if c.ProtoFiles == 0 || c.CommonTypes == 0 {
		return nil, c, fmt.Errorf(
			"в дереве контракта %q прочитано файлов %d, типов пакета %s объявлено %d — "+
				"наблюдать нечего, и «ноль находок» неотличимо от «ноль прочитанного»",
			opts.ProtoRoot, c.ProtoFiles, SubscriptionCommonPackage, c.CommonTypes)
	}

	// ── проход 2: кто называет их в контрактах ───────────────────────────────
	err = rootedWalk(filepath.Join(opts.Root, opts.ProtoRoot), func(rel string) bool {
		return strings.HasSuffix(rel, ".proto")
	}, func(path string, b []byte) error {
		rel := subRelTo(opts.Root, path)
		if commonFiles[rel] {
			return nil
		}
		clean := stripProtoComments(string(b))
		for _, name := range order {
			if subReachNames(clean, SubscriptionCommonPackage+"."+name) {
				byName[name].ProtoReferrers = append(byName[name].ProtoReferrers, rel)
			}
		}
		return nil
	})
	if err != nil {
		return nil, c, err
	}

	// ── проход 3: кто употребляет их в коде ──────────────────────────────────
	goRoots := opts.GoRoots
	if len(goRoots) == 0 {
		goRoots = []string{"."}
	}
	fset := token.NewFileSet()
	for _, gr := range goRoots {
		err = rootedWalk(filepath.Join(opts.Root, gr), func(rel string) bool {
			return strings.HasSuffix(rel, ".go")
		}, func(path string, b []byte) error {
			rel := subRelTo(opts.Root, path)
			// Сгенерённые стабы существуют ОТТОГО, что объявление есть, и потому
			// ссылкой на его нужность не являются.
			if strings.HasPrefix(filepath.ToSlash(rel), "pkg/api/") {
				return nil
			}
			file, perr := parser.ParseFile(fset, rel, b, parser.SkipObjectResolution)
			if perr != nil {
				// Файл, который не разбирается, ссылкой не считается — но и
				// молча не пропускается: он попадает в перепись, иначе
				// «ссылок ноль» стало бы неотличимо от «не прочитано».
				c.GoUnparsed++
				return nil
			}
			alias, blank := subReachAlias(file)
			if blank {
				c.BlankImports++
				return nil
			}
			if alias == "" {
				return nil
			}
			c.GoFiles++
			isTest := strings.HasSuffix(rel, "_test.go")
			used := subReachUsedTypes(file, alias)
			for _, name := range order {
				if !used[name] {
					continue
				}
				if isTest {
					// Проба живёт ради гейта, а гейт — ради этой формы. Засчитать
					// её значило бы замкнуть наблюдение на само себя.
					c.TestReferrers++
					continue
				}
				byName[name].GoReferrers = append(byName[name].GoReferrers, rel)
			}
			return nil
		})
		if err != nil {
			return nil, c, err
		}
	}

	sort.Strings(order)
	types := make([]SubscriptionFormType, 0, len(order))
	for _, name := range order {
		t := *byName[name]
		sort.Strings(t.ProtoReferrers)
		sort.Strings(t.GoReferrers)
		if t.Referrers() == 0 {
			c.Unreferenced++
		}
		types = append(types, t)
	}

	if out != nil {
		_, _ = fmt.Fprintf(out,
			"перепись: файлов контракта %d; типов пакета %s объявлено %d, из них без ссылок %d; "+
				"файлов Go с именованным импортом %d, пустых импортов %d, употреблений в пробах %d, "+
				"не разобралось файлов Go %d\n",
			c.ProtoFiles, SubscriptionCommonPackage, c.CommonTypes, c.Unreferenced,
			c.GoFiles, c.BlankImports, c.TestReferrers, c.GoUnparsed)
		for _, t := range types {
			_, _ = fmt.Fprintf(out, "  %s %s (%s): ссылок %d %v%v\n",
				t.Kind, t.Name, t.Where, t.Referrers(), t.ProtoReferrers, t.GoReferrers)
		}
	}
	return types, c, nil
}

// SubscriptionReachFindings — РЕШЕНИЕ, отделённое от измерения.
//
// takerState — состояние задачи-берущего, как его назвал трекер: "OPEN",
// "CLOSED" либо пустая строка, если состояние выяснить не удалось или сверку не
// запрашивали.
//
// Несостоявшееся измерение находкой НЕ становится: иначе временная
// недоступность трекера роняла бы прогон, и гейт научились бы обходить
// (`security.md` §Hardening-инварианты, п. 8 — отказ считается и печатается, а
// не проглатывается и не превращается в вердикт).
func SubscriptionReachFindings(types []SubscriptionFormType, takerState string) []string {
	if takerState != "CLOSED" {
		return nil
	}
	var out []string
	for _, t := range types {
		if t.Referrers() > 0 {
			continue
		}
		out = append(out, fmt.Sprintf(
			"%s %s (%s): ссылок ноль, а задача #%d, чья работа состояла в том, чтобы форму "+
				"взять, ЗАКРЫТА. Срок послабления истёк вместе со своим предметом: форму "+
				"надлежит СНЯТЬ, а не оставить «на будущее». Другой исход — вернуть задачу "+
				"в работу, и тогда запись оживает вместе с ней",
			t.Kind, t.Name, t.Where, SubscriptionFormTaker))
	}
	sort.Strings(out)
	return out
}

// subReachAlias — под каким именем файл импортирует сгенерённый пакет.
//
// Читается РАЗБОР, а не текст: путь импорта встречается и внутри строковых
// литералов (фикстуры проб, шаблоны генераторов), и поиск по образцу засчитал бы
// такой файл ссылкой — то есть замолчал бы ровно там, где обязан говорить.
// Первая редакция этого анализатора так и делала, и находка нашлась на его же
// собственной переписи.
func subReachAlias(file *ast.File) (alias string, blank bool) {
	want := `"` + SubscriptionGoPackage + `"`
	for _, imp := range file.Imports {
		if imp.Path == nil || imp.Path.Value != want {
			continue
		}
		if imp.Name == nil {
			return SubscriptionGoDefaultAlias, false
		}
		if imp.Name.Name == "_" {
			return "", true
		}
		if imp.Name.Name == "." {
			// Точечный импорт: имена пакета входят в область файла без
			// префикса. Такой формы в этом дереве нет, и притворяться, что
			// анализатор её понимает, он не станет — она попадёт в «не
			// разобралось» через отсутствие псевдонима.
			return "", false
		}
		return imp.Name.Name, false
	}
	return "", false
}

// subReachUsedTypes — какие типы пакета файл действительно употребляет.
func subReachUsedTypes(file *ast.File, alias string) map[string]bool {
	used := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok || id.Name != alias {
			return true
		}
		used[sel.Sel.Name] = true
		return true
	})
	return used
}

// subReachNames — назван ли символ `sym` в тексте как ОТДЕЛЬНОЕ имя.
//
// Проверка границ обязательна: без неё `SubscriptionRequest` находился бы
// внутри `SubscriptionRequestV2`, и переименованный сосед засчитывался бы
// ссылкой на снятый тип.
func subReachNames(body, sym string) bool {
	for i := 0; ; {
		j := strings.Index(body[i:], sym)
		if j < 0 {
			return false
		}
		at := i + j
		end := at + len(sym)
		if end >= len(body) || !isSubIdentRune(body[end]) {
			return true
		}
		i = end
	}
}

func isSubIdentRune(b byte) bool {
	return b == '_' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// stripProtoComments снимает комментарии, чтобы слово в прозе не засчиталось за
// объявление и чтобы скобка в комментарии не сбила глубину вложенности на весь
// остаток файла.
func stripProtoComments(s string) string {
	s = subBlockRe.ReplaceAllString(s, "")
	return subLineRe.ReplaceAllString(s, "")
}

func subRelTo(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}
