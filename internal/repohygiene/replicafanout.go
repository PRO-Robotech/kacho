// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Разбор фоновых петель для гейта «фоновая работа обязана нести исход по
// многорепличности».
//
// Вынесено в не-тестовый файл пакета, чтобы инъекционная проба звала ТОТ ЖЕ
// разбор, а не свою копию: копия разошлась бы с оригиналом молча и доказывала бы
// способность упасть у кода, который не исполняется.

// fanoutMarker — приставка записи об исходе. Русская, как и весь корпус правил, и
// заметная глазом в шапке функции.
const fanoutMarker = "РЕПЛИКИ:"

// fanoutReasonMin — сколько байт обязана нести причина после тире.
//
// Причина, состоящая из одного слова, есть та же форма без содержания, которую
// гейт и ловит: «на-реплику — ок» проходит проверку наличия маркера и не говорит
// читателю ничего. Порог грубый НАМЕРЕННО — он отсекает пустую отписку, а не
// судит качество текста, судить которое машина не умеет.
const fanoutReasonMin = 24

// fanoutKinds — ЗАКРЫТЫЙ словарь исходов. Корзины «прочее» нет: вид, которого
// здесь не назвали, есть незакрытый вопрос, а не пятый способ.
var fanoutKinds = map[string]string{
	// строки берутся клеймом — репликам достаются непересекающиеся партии
	"клейм": "клейм строки либо условная правка-CAS",
	// проход целиком достаётся одной реплике
	"одиночка": "замок прохода",
	// работа разбита по ключу, каждая часть — своей реплике
	"разбиение": "разбиение по ключу",
	// петля работает в каждой реплике ПО ЗАМЫСЛУ (своё состояние процесса)
	"на-реплику": "по замыслу в каждой реплике",
	// петля принадлежит запросу или вызову, а не жизни процесса
	"запрос": "принадлежит запросу",
}

// bgLoop — одна фоновая петля с её исходом.
type bgLoop struct {
	File   string
	Line   int
	Func   string
	Driver string // чем петля движима: тик, ожидание уведомления, пауза
	Kind   string // вид исхода из закрытого словаря; пусто — записи нет
	Reason string // причина после тире
	Bad    string // почему запись негодна (пусто — годна)
}

// bgCensus — перепись обхода. Печатается ВСЕГДА: «ноль находок» обязано быть
// отличимо от «ноль прочитанного».
type bgCensus struct {
	Dirs      []string
	FilesRead int
	Loops     []bgLoop
}

// ByKind — сколько петель каждого исхода.
func (c bgCensus) ByKind() map[string]int {
	out := map[string]int{}
	for _, l := range c.Loops {
		if l.Kind != "" && l.Bad == "" {
			out[l.Kind]++
		}
	}
	return out
}

// Findings — петли без годной записи об исходе.
func (c bgCensus) Findings() []bgLoop {
	var out []bgLoop
	for _, l := range c.Loops {
		if l.Kind == "" || l.Bad != "" {
			out = append(out, l)
		}
	}
	return out
}

// bgScanDirs — каталоги развёрнутого процесса. Провайдер инфраструктуры и
// служебные инструменты сюда НЕ входят: они запускаются оператором в одном
// экземпляре, а предмет гейта — работа, которую поднимает КАЖДАЯ реплика
// развёрнутого сервиса.
var bgScanDirs = []string{"pkg", "gateway", "services"}

// bgSkipParts — куски пути, выведенные из осмотра, каждый со своей причиной.
var bgSkipParts = []string{
	"/repomock/", "/portmock/", "/kachomock/", "/mock/", // дублёры портов, не прод-процесс
	"/pkg/sdk/", // клиентская библиотека: её петли крутит вызывающий
	"pkg/api/",  // сгенерённые стабы, руками не правятся
	"/pgtest/",  // харнесс проб
}

// scanBackgroundLoops обходит дерево и возвращает перепись фоновых петель.
//
// Чтение идёт `rootedWalk`, то есть В ПРЕДЕЛАХ осматриваемого каталога: вердикт
// обязан быть свойством ЭТОГО дерева, а не постороннего файла, до которого можно
// дотянуться символической ссылкой.
func scanBackgroundLoops(root string) (bgCensus, error) {
	c := bgCensus{}
	fset := token.NewFileSet()

	for _, dir := range bgScanDirs {
		abs := filepath.Join(root, dir)
		if _, err := os.Stat(abs); err != nil {
			continue
		}
		c.Dirs = append(c.Dirs, dir)
		err := rootedWalk(abs,
			func(rel string) bool {
				if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
					return false
				}
				full := dir + "/" + rel
				// Прибор замера запускается оператором в одном экземпляре — это
				// уже сказано у [bgScanDirs]. Признак объявлен ОДИН раз
				// ([isInstrumentPath]), а не повторён здесь куском пути: копия
				// разошлась бы с оригиналом молча.
				if isInstrumentPath(full) {
					return false
				}
				for _, skip := range bgSkipParts {
					if strings.Contains(full, skip) {
						return false
					}
				}
				for _, part := range strings.Split(rel, "/") {
					if part == "node_modules" || part == "testdata" || part == "vendor" {
						return false
					}
				}
				return true
			},
			func(p string, body []byte) error {
				rel, rerr := filepath.Rel(root, p)
				if rerr != nil {
					return rerr
				}
				rel = filepath.ToSlash(rel)
				f, perr := parser.ParseFile(fset, p, body, parser.ParseComments)
				if perr != nil {
					return fmt.Errorf("разбор %s: %w", rel, perr)
				}
				c.FilesRead++
				c.Loops = append(c.Loops, loopsInFile(fset, f, rel)...)
				return nil
			})
		if err != nil {
			return c, err
		}
	}
	sort.Slice(c.Loops, func(i, j int) bool {
		if c.Loops[i].File != c.Loops[j].File {
			return c.Loops[i].File < c.Loops[j].File
		}
		return c.Loops[i].Line < c.Loops[j].Line
	})
	return c, nil
}

// loopsInFile находит фоновые петли одного файла и читает их запись об исходе.
func loopsInFile(fset *token.FileSet, f *ast.File, rel string) []bgLoop {
	var out []bgLoop
	ast.Inspect(f, func(n ast.Node) bool {
		d, ok := n.(*ast.FuncDecl)
		if !ok || d.Body == nil {
			return true
		}
		name := d.Name.Name
		if d.Recv != nil && len(d.Recv.List) > 0 {
			name = receiverName(d.Recv.List[0].Type) + "." + name
		}
		doc := ""
		if d.Doc != nil {
			doc = d.Doc.Text()
		}
		kind, reason, bad := readFanoutMarker(doc)

		// Канал тикера кладут в переменную ДО петли, поэтому собирается он по
		// телу ФУНКЦИИ, а не по телу цикла. Первая редакция этой правки читала
		// тело цикла — и не находила ничего: присваивание стоит выше `for`, и
		// расширение было холостым, о чём сказала перепись (она не изменилась).
		tickVars := tickerChannelVars(d.Body)

		seen := false
		ast.Inspect(d.Body, func(m ast.Node) bool {
			var body *ast.BlockStmt
			switch l := m.(type) {
			case *ast.ForStmt:
				body = l.Body
			case *ast.RangeStmt:
				body = l.Body
			default:
				return true
			}
			driver := loopDriver(body, tickVars)
			if driver == "" {
				return true
			}
			// Одна запись на ФУНКЦИЮ: у функции с двумя петлями исход один, и
			// требовать два маркера значило бы требовать копию, которая разойдётся.
			if seen {
				return true
			}
			seen = true
			pos := fset.Position(m.Pos())
			out = append(out, bgLoop{
				File: rel, Line: pos.Line, Func: name, Driver: driver,
				Kind: kind, Reason: reason, Bad: bad,
			})
			return true
		})
		return true
	})
	return out
}

// readFanoutMarker вытаскивает вид и причину из шапки функции.
//
// Возвращает (вид, причина, чем запись негодна). Отсутствие записи — пустой вид
// без объяснения: это не «негодная запись», а её отсутствие, и сообщение у них
// разное.
func readFanoutMarker(doc string) (kind, reason, bad string) {
	idx := strings.Index(doc, fanoutMarker)
	if idx < 0 {
		return "", "", ""
	}
	rest := doc[idx+len(fanoutMarker):]
	// Запись занимает столько строк, сколько нужно причине; заканчивается пустой
	// строкой либо концом шапки.
	var lines []string
	for _, line := range strings.Split(rest, "\n") {
		if strings.TrimSpace(line) == "" && len(lines) > 0 {
			break
		}
		lines = append(lines, strings.TrimSpace(line))
	}
	rec := strings.TrimSpace(strings.Join(lines, " "))

	// Разделитель — длинное тире: он же отделяет вид от причины во всём корпусе.
	parts := strings.SplitN(rec, "—", 2)
	kind = strings.TrimSpace(parts[0])
	if _, ok := fanoutKinds[kind]; !ok {
		return kind, "", fmt.Sprintf("вид %q не из закрытого словаря (%s)", kind, kindList())
	}
	if len(parts) < 2 {
		return kind, "", "после вида нет тире и причины"
	}
	reason = strings.TrimSpace(parts[1])
	if len(reason) < fanoutReasonMin {
		return kind, reason, fmt.Sprintf("причина короче %d байт — отписка, а не объяснение", fanoutReasonMin)
	}
	return kind, reason, ""
}

func kindList() string {
	keys := make([]string, 0, len(fanoutKinds))
	for k := range fanoutKinds {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, " · ")
}

// loopDriver отвечает, чем движима петля, и пусто — если ничем из перечисленного.
//
// Перечень закрыт и назван: тик таймера, ожидание уведомления базы, пауза. Это и
// есть механическое определение «фоновой» работы — та, что повторяется по
// ВРЕМЕНИ или по внешнему событию, а не по данным вызова.
//
// # Канал тикера В ПЕРЕМЕННОЙ — вторая законная форма записи того же предмета
//
// Распознаватель видел `<-тикер.C` только как СЕЛЕКТОР. Петля, кладущая канал в
// переменную ради подменяемых часов (`c, stop = t.C, t.Stop`, затем `case <-c:`),
// давала узел-ИДЕНТИФИКАТОР и была гейту НЕВИДИМА — не нарушением, а
// невидимостью: ни красного, ни зелёного, молчание.
//
// Форма эта не край и не редкость: управляемые часы нужны всякой петле, у
// которой есть детерминированная проба. Поэтому распознаётся и она — но не
// «всякий приём из переменной»: узнаётся ровно тот идентификатор, в который
// канал тикера ПОЛОЖИЛИ в этой же функции. Петля, движимая каналом из данных
// вызова, фоновой по-прежнему не считается, и это утверждается инъекцией.
func loopDriver(b *ast.BlockStmt, tickVars map[string]bool) string {
	var found []string
	add := func(s string) {
		for _, x := range found {
			if x == s {
				return
			}
		}
		found = append(found, s)
	}
	ast.Inspect(b, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.UnaryExpr:
			if x.Op != token.ARROW {
				return true
			}
			switch src := x.X.(type) {
			case *ast.Ident:
				// Канал тикера, положенный в переменную (см. шапку).
				if tickVars[src.Name] {
					add("тик")
				}
			case *ast.SelectorExpr:
				if src.Sel.Name == "C" {
					add("тик")
				}
			case *ast.CallExpr:
				if sel, ok := src.Fun.(*ast.SelectorExpr); ok {
					if id, ok := sel.X.(*ast.Ident); ok && id.Name == "time" &&
						(sel.Sel.Name == "After" || sel.Sel.Name == "Tick") {
						add("тик")
					}
				}
			}
		case *ast.CallExpr:
			sel, ok := x.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "time" && sel.Sel.Name == "Sleep" {
				add("пауза")
			}
			if sel.Sel.Name == "WaitForNotification" {
				add("уведомление")
			}
		}
		return true
	})
	sort.Strings(found)
	return strings.Join(found, "+")
}

// tickerChannelVars собирает имена переменных, в которые в ЭТОЙ ЖЕ функции
// положили канал тикера.
//
// Признак — правая часть присваивания вида `<что-то>.C`. Он узкий намеренно:
// широкое «всякий приём из переменной-канала» объявило бы фоновой любую петлю,
// читающую канал из данных вызова, и гейт краснел бы на исправном дереве — то
// есть был бы снят первым же обходом.
func tickerChannelVars(b *ast.BlockStmt) map[string]bool {
	vars := map[string]bool{}
	record := func(lhs, rhs []ast.Expr) {
		for i, r := range rhs {
			if i >= len(lhs) {
				break
			}
			sel, ok := r.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "C" {
				continue
			}
			if id, ok := lhs[i].(*ast.Ident); ok && id.Name != "_" {
				vars[id.Name] = true
			}
		}
	}
	ast.Inspect(b, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			record(x.Lhs, x.Rhs)
		case *ast.ValueSpec:
			lhs := make([]ast.Expr, 0, len(x.Names))
			for _, nm := range x.Names {
				lhs = append(lhs, nm)
			}
			record(lhs, x.Values)
		}
		return true
	})
	return vars
}

// receiverName — имя типа получателя, для координаты в отказе.
func receiverName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return receiverName(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr:
		return receiverName(t.X)
	case *ast.IndexListExpr:
		return receiverName(t.X)
	}
	return "?"
}
