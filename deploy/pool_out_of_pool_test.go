// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

// pool_out_of_pool_test.go — СЛАГАЕМОЕ ВНЕ ПУЛА для арифметики посадки.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЗАЧЕМ ЭТО ЕСТЬ
//
// Соседняя проверка (`pool_fits_database_test.go`) сверяла «реплики × ширина пула
// ≤ предел базы». Слагаемое у неё было ОДНО, а мест, занимающих соединение, в
// дереве несколько, и все они держат его ВНЕ пула:
//
//   - поток подписки — `LISTEN` требует своей сессии, и сессия из пула вернулась
//     бы в него вместе с подпиской; на реплику их до потолка одновременных
//     потоков;
//   - дренаж очереди — соединение ИЗЪЯТО из пула (`Hijack`), то есть пул вправе
//     открыть себе новое: это +1 сверх ширины, а не место внутри неё;
//   - пробуждение реконсиляции — то же изъятие в собственном дереве службы.
//
// Ноль в неучтённом слагаемом обращает сумму в меньшее число и проходит любую
// проверку. Асимметрия исходов, из-за которой это дороже обычного недосчёта:
// упереться в СВОЙ потолок потоков — чистый отказ одному вызывающему
// (`RESOURCE_EXHAUSTED`, повтор осмыслен); упереться в предел БАЗЫ — отказ всему
// процессу и всем арендаторам сразу, включая тех, кто подписок не открывал.
//
// ─────────────────────────────────────────────────────────────────────────────
// АРИФМЕТИКА ПОЛНА BY CONSTRUCTION, А НЕ ПО ПАМЯТИ АВТОРА
//
// «Учли то, что вспомнили» — это честная неполнота, названная полнотой. Поэтому
// здесь два механизма, а не один:
//
//  1. ПЕРЕПИСЬ ЗАХВАТОВ по дереву: всякий захват соединения вне пула опознаётся
//     разбором (`pgx.Connect` и `Hijack` в файле, импортирующем драйвер) и обязан
//     быть КОМУ-ТО приписан. Захват, не приписанный ни службе, ни записи каталога
//     ниже, — НАХОДКА `неучтённый захват вне пула`, а не молчание;
//  2. КАТАЛОГ ДЕРЖАТЕЛЕЙ для мест, лежащих в общем фундаменте: запись называет
//     файл захвата и то, сколько соединений он стоит службе-потребителю. Запись,
//     чьего файла в дереве больше нет, — САМА находка: иначе она пережила бы свой
//     предмет и продолжала бы называться учтённой.
//
// Вместе они дают проверяемое утверждение: неучтённых видов СТОЛЬКО-ТО, и это
// число печатается переписью. Сегодня оно ноль — но ноль ИЗМЕРЕННЫЙ, а не
// обещанный: заведи кто-нибудь восьмой захват, перепись назовёт его файлом.
//
// ─────────────────────────────────────────────────────────────────────────────
// ВЕЛИЧИНА СЛАГАЕМОГО БЕРЁТСЯ ОТТУДА, ГДЕ ОНА РЕШАЕТСЯ (kacho#1384, второй круг)
//
// Мало опознать вид захвата — надо назвать ЧИСЛО, и это число бывает объявлено
// в двух разных местах сразу. Потолок одновременных потоков подписки живёт
// умолчанием в коде службы И объявлением в её чарте
// (`subscription.maxStreams`), а побеждает ВТОРОЕ: раскладчик настроек читает
// то, что ему подставили, а не то, что вкомпилировано.
//
// Первая редакция читала умолчание кода — и была права ровно до того дня, когда
// величину объявили все пять служб с сервером подписки. Заметить это чтением
// суммы было нельзя: объявленное значение байт-в-байт равно вкомпилированному,
// поэтому сумма сходилась, вердикт не менялся, а основание у него было ложным.
// Заметила ПРОБА ПРЕДПОСЫЛКИ, стоявшая рядом, — она для того и стоит.
//
// Отсюда порядок резолва, повторяющий порядок процесса: объявление чарта
// (значения стека: профиль → умбрелла → подчарт) → умолчание кода. Ключ,
// объявленный чартом НЕ из значений, и значения, не несущие названного шаблоном
// пути, дают ОТКАЗ, а не откат к умолчанию: откат вернул бы величину, которой
// процесс не увидит, и сумма сошлась бы по неверному слагаемому.
//
// Предпосылку стережёт `TestOutOfPoolCeilingIsReadWhereItIsDecided`, а
// происхождение каждого слагаемого печатает перепись соседнего гейта — иначе
// обратный съезд («резолвер снова читает исходник») выглядел бы как исправная
// работа: сумма та же, вердикт тот же.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ РОЛЬ, А НЕ СЛОВО
//
// `Hijack` есть и у перехватчика HTTP-соединения на крае — к базе он отношения не
// имеет, и имя метода их не разделяет. Разграничивают РОЛИ:
//
//   - `Connect` засчитывается там, где имя пакета разрешается в ДРАЙВЕР базы.
//     Разрешение идёт по пути импорта, а не по последнему его сегменту: путь
//     версионированного модуля оканчивается номером версии, а пакет зовётся
//     иначе. Первая редакция считала сегмент и опознала НОЛЬ захватов при трёх
//     живых — и печатала при этом «захватов 0», то есть выглядела чистым деревом;
//   - `Hijack` засчитывается там, где файл ВЫДАЁТ `LISTEN`. Это и есть причина, по
//     которой соединение вообще изымают из пула: подписка требует своей сессии, а
//     сессия из пула вернулась бы в него вместе с подпиской. Перехватчик
//     HTTP-соединения `LISTEN` не выдаёт — он законный близнец из настоящего
//     дерева и стоит в самопроверке.
//
// # ГРАНИЦА РАСПОЗНАВАТЕЛЯ — названа, а не умолчана
//
// Изъятое из пула соединение, удерживаемое НЕ РАДИ подписки (долгая выгрузка,
// временный сеансовый параметр), под второй признак не подпадёт. Такого места в
// дереве сегодня нет; появится — оно НЕ будет учтено, и молчание об этом было бы
// неотличимо от полноты. Граница закреплена пробой
// (`TestOutOfPoolRecogniserDeclaresItsBorder`): научит кто-нибудь распознаватель
// — проба покраснеет и заставит поправить эту шапку.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/productnaming"
)

const (
	pgxDriverPkg = "github.com/jackc/pgx/v5"

	subscriptionPkg = "github.com/PRO-Robotech/kacho/pkg/subscription"
	drainerPkg      = "github.com/PRO-Robotech/kacho/pkg/outbox/drainer"
	authzPkg        = "github.com/PRO-Robotech/kacho/pkg/authz"
)

// outOfPoolRoots — корни обхода. Перечисляет ВЫЗЫВАЮЩИЙ, а не обходчик: «здесь
// не смотрели» обязано быть видно в этом файле, а не подразумеваться.
var outOfPoolRoots = []string{"pkg", "services", "gateway", "internal", "cmd", "terraform"}

// ─────────────────────────────────────────────────────────────────────────────
// КАТАЛОГ ДЕРЖАТЕЛЕЙ ОБЩЕГО ФУНДАМЕНТА

// outOfPoolHolder — один вид захвата, лежащий в `pkg/`.
//
// Служба-КАНДИДАТ опознаётся ИМПОРТОМ пакета-держателя, а не перечнем имён:
// перечень разошёлся бы с деревом молча, а импорт есть ребро, которое видно
// разбору.
//
// # Импорт находит КАНДИДАТА, а держателя поднимает КОНСТРУКТОР
//
// Читать импорт как «служба поднимает держателя» было верно ровно до тех пор,
// пока в пакете-держателе не жило ничего, кроме держателя. Это свойство
// ПОПУЛЯЦИИ, на которой гейт писался, а не свойство предмета: пакет вправе
// нести и переиспользуемую часть, и тогда импортёр её берёт, держателя не
// поднимая.
//
// Поэтому величину решает [outOfPoolHolder.PerReplica], и ноль у него бывает
// ЗАКОННЫМ — но только ИЗМЕРЕННЫМ: конструктор держателя в поддереве службы не
// зовётся ни разу. Ноль ПРЕДПОЛОЖЕННЫЙ остаётся находкой, потому что
// неизвестное слагаемое не бывает нулём.
type outOfPoolHolder struct {
	// Kind — как вид называется в разборе и в тексте находки.
	Kind string
	// Site — файл захвата. Его отсутствие в дереве делает запись САМОИСТЁКШЕЙ.
	Site string
	// Pkg — пакет-держатель: службы-КАНДИДАТЫ находятся по его импорту.
	Pkg string
	// RaisedBy — ключ конструктора держателя (`<путь пакета>.<функция>`).
	//
	// Пустой означает «держателя поднимает сам импорт»: в пакете не живёт ничего,
	// кроме держателя, поэтому импортировать его и не поднимать нельзя.
	//
	// Непустой означает, что пакет несёт И держателя, И переиспользуемую часть.
	// Тогда импорт делает службу лишь кандидатом, а решает КОНСТРУКТОР: ни одного
	// его вызова в поддереве службы — держатель не поднят, слагаемое ноль, и ноль
	// этот ИЗМЕРЕН. Заведут конструктор — слагаемое появится само.
	RaisedBy string
	// PerReplica — сколько соединений вид стоит службе на ОДНУ реплику.
	// Возвращает величину, её происхождение и признак «величина не выведена» —
	// последнее есть находка, а не ноль: неизвестное слагаемое не бывает нулём.
	PerReplica func(t *testing.T, c outOfPoolCtx) (int, string, error)
}

// outOfPoolCtx — вход резолвера слагаемого.
//
// Несёт ОБЕ стороны, из которых складывается побеждающая величина: дерево
// исходников (там живут умолчания кода) и посадку этого стека (там живут
// объявления чарта под наложенными профилями). Резолвер, которому дают только
// первую, обречён читать умолчание — даже когда его давно перебивают.
type outOfPoolCtx struct {
	// svcDir — каталог исходников службы: `services/<имя>`.
	svcDir string
	// chartDir — каталог её чарта. ПУСТОЙ означает «чарта нет», и это законный
	// вход: тогда величину решает умолчание кода.
	chartDir string
	// values — значения ПОДЧАРТА после наложения профилей стека, то есть ровно
	// то, что helm подставит в его шаблоны.
	values map[string]any
	tree   *outOfPoolTree
}

var outOfPoolHolders = []outOfPoolHolder{
	{
		Kind: "поток подписки",
		Site: "pkg/subscription/server.go",
		Pkg:  subscriptionPkg,
		// Пакет несёт, кроме сервера потоков, НАБЛЮДАТЕЛЬ ГРАНИЦЫ УСТОЯВШЕГОСЯ —
		// переиспользуемую часть, которую берут возобновимые чтения, отвечающие на
		// запрос (kacho#1374). Такой импортёр потоков не поднимает и соединений
		// сверх пула не держит вовсе: наблюдение задаёт свой единственный вопрос
		// ТЕМ ЖЕ пулом и возвращает соединение в него.
		//
		// Поэтому здесь решает конструктор, а не импорт. Было наоборот, и было
		// верно — ровно до тех пор, пока в пакете не поселилось ничего, кроме
		// держателя: свойство ПОПУЛЯЦИИ, а не предмета.
		RaisedBy: subscriptionPkg + ".NewServer",
		// Соединение поднимается НА КАЖДЫЙ поток и живёт весь его срок; сверху их
		// держит потолок одновременных потоков реплики.
		PerReplica: subscriptionStreamCeiling,
	},
	{
		Kind: "LISTEN дренажа очереди",
		Site: "pkg/outbox/drainer/internal.go",
		Pkg:  drainerPkg,
		// Дренаж ИЗЫМАЕТ соединение из пула, поэтому пул вправе открыть себе
		// новое: это +1 сверх ширины на каждый заведённый дренаж.
		PerReplica: drainerInstancesPerReplica,
	},
	{
		Kind: "LISTEN сброса кэша решений",
		Site: "pkg/authz/listen_invalidate.go",
		Pkg:  authzPkg,
		// Ноль ИЗМЕРЕННЫЙ, а не предположенный: держателя никто не поднимает —
		// ни один композиционный корень его не упоминает. Заведут — слагаемое
		// появится само, и появится оно у ЧУЖОЙ базы (держатель слушает канал
		// службы личностей), о чём находка и скажет.
		PerReplica: listenInvalidatorPerReplica,
	},
}

// ─────────────────────────────────────────────────────────────────────────────
// ЧТЕНИЕ ДЕРЕВА

// outOfPoolCapture — один захват соединения вне пула.
type outOfPoolCapture struct {
	File string
	Line int
	How  string
}

func (c outOfPoolCapture) String() string { return fmt.Sprintf("%s:%d (%s)", c.File, c.Line, c.How) }

// outOfPoolTree — прочитанное дерево: захваты и импорты прод-файлов.
type outOfPoolTree struct {
	root     string
	captures []outOfPoolCapture
	// imports — файл прод-кода → множество импортированных путей.
	imports map[string]map[string]bool
	// calls — файл прод-кода → сколько раз вызван `<пакет>.<функция>`.
	calls map[string]map[string]int
	files int
}

// jcListen — выдача подписки на канал. Роль, по которой изъятие соединения из
// пула отличается от перехвата HTTP-соединения.
var listenStatement = regexp.MustCompile(`(?i)^\s*(UN)?LISTEN\s`)

// readOutOfPoolTree разбирает прод-дерево ОДИН раз: и захваты, и импорты, и
// вызовы конструкторов держателей.
func readOutOfPoolTree(t *testing.T) *outOfPoolTree { return readOutOfPoolTreeAt(t, "..") }

// readOutOfPoolTreeAt — тот же разбор с явным корнем: самопроверка подаёт ему
// синтетическое дерево, а не подделывает настоящее.
func readOutOfPoolTreeAt(t *testing.T, root string) *outOfPoolTree {
	t.Helper()
	tr := &outOfPoolTree{
		root:    root,
		imports: map[string]map[string]bool{},
		calls:   map[string]map[string]int{},
	}
	for _, r := range outOfPoolRoots {
		dir := filepath.Join(tr.root, r)
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, _ := filepath.Rel(tr.root, path)
			rel = filepath.ToSlash(rel)
			// Сгенерённые стабы соединений не открывают вовсе.
			if strings.HasPrefix(rel, "pkg/api/") {
				return nil
			}
			tr.files++
			return tr.readFile(path, rel)
		})
		if err != nil {
			t.Fatalf("обход %s: %v", r, err)
		}
	}
	sort.Slice(tr.captures, func(i, j int) bool {
		if tr.captures[i].File != tr.captures[j].File {
			return tr.captures[i].File < tr.captures[j].File
		}
		return tr.captures[i].Line < tr.captures[j].Line
	})
	return tr
}

func (tr *outOfPoolTree) readFile(path, rel string) error {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return fmt.Errorf("разбор %s: %w", rel, err)
	}

	// Локальное имя пакета → путь импорта. Псевдоним учитывается: судить по
	// последнему сегменту пути значило бы промахнуться на первом же псевдониме.
	byName := map[string]string{}
	paths := map[string]bool{}
	issuesListen := false
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		if text, uerr := strconv.Unquote(lit.Value); uerr == nil && listenStatement.MatchString(text) {
			issuesListen = true
		}
		return true
	})
	for _, im := range file.Imports {
		p, uerr := strconv.Unquote(im.Path.Value)
		if uerr != nil {
			continue
		}
		paths[p] = true
		name := packageNameOf(p)
		if im.Name != nil {
			name = im.Name.Name
		}
		byName[name] = p
	}
	tr.imports[rel] = paths

	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		fun := call.Fun
		// `drainer.New[T](…)` — вызов родовой функции: под селектором стоит
		// индексное выражение. Разбор его снимает by construction, а поиск по
		// тексту `drainer.New(` не нашёл бы ни одного места в дереве.
		switch idx := fun.(type) {
		case *ast.IndexExpr:
			fun = idx.X
		case *ast.IndexListExpr:
			fun = idx.X
		}
		sel, ok := fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		line := fset.Position(call.Pos()).Line

		// Захват вида «своё соединение»: `pgx.Connect`. Роль подтверждается
		// импортом драйвера — одноимённый метод чужого пакета захватом не является.
		if ident, isIdent := sel.X.(*ast.Ident); isIdent && sel.Sel.Name == "Connect" {
			if byName[ident.Name] == pgxDriverPkg {
				tr.captures = append(tr.captures, outOfPoolCapture{rel, line, "pgx.Connect"})
			}
		}
		// Захват вида «изъять из пула»: `Hijack`. Приёмник — значение, а не пакет,
		// и типа его здесь нет, поэтому роль решает ПРИЧИНА изъятия: файл выдаёт
		// `LISTEN`. Ровно этим отсекается перехватчик HTTP-соединения на крае.
		if sel.Sel.Name == "Hijack" && issuesListen {
			tr.captures = append(tr.captures, outOfPoolCapture{rel, line, "pgxpool→Hijack"})
		}
		// Вызовы конструкторов держателей — для счёта заведённых экземпляров.
		if ident, isIdent := sel.X.(*ast.Ident); isIdent {
			if p := byName[ident.Name]; p != "" {
				key := p + "." + sel.Sel.Name
				if tr.calls[rel] == nil {
					tr.calls[rel] = map[string]int{}
				}
				tr.calls[rel][key]++
			}
		}
		return true
	})
	return nil
}

// importers — прод-файлы поддерева, импортирующие пакет.
func (tr *outOfPoolTree) importers(prefix, pkg string) []string {
	var out []string
	for rel, paths := range tr.imports {
		if strings.HasPrefix(rel, prefix) && paths[pkg] {
			out = append(out, rel)
		}
	}
	sort.Strings(out)
	return out
}

// callSites — сколько раз в поддереве вызвана функция пакета.
func (tr *outOfPoolTree) callSites(prefix, key string) (int, []string) {
	n := 0
	var where []string
	for rel, byKey := range tr.calls {
		if !strings.HasPrefix(rel, prefix) {
			continue
		}
		if c := byKey[key]; c > 0 {
			n += c
			where = append(where, rel)
		}
	}
	sort.Strings(where)
	return n, where
}

// ─────────────────────────────────────────────────────────────────────────────
// РЕЗОЛВЕРЫ СЛАГАЕМЫХ

func drainerInstancesPerReplica(_ *testing.T, c outOfPoolCtx) (int, string, error) {
	svcDir, tr := c.svcDir, c.tree
	n, where := tr.callSites(svcDir+"/", drainerPkg+".New")
	if n == 0 {
		return 0, "", fmt.Errorf(
			"служба импортирует дренаж очереди, но ни одного его заведения не найдено: "+
				"слагаемое не выведено, а неизвестное слагаемое не бывает нулём (искали вызовы %s.New в %s)",
			drainerPkg, svcDir)
	}
	return n, fmt.Sprintf("заведений дренажа %d (%s)", n, strings.Join(where, ", ")), nil
}

func listenInvalidatorPerReplica(_ *testing.T, c outOfPoolCtx) (int, string, error) {
	svcDir, tr := c.svcDir, c.tree
	n, where := tr.callSites(svcDir+"/", authzPkg+".ListenInvalidator")
	if n == 0 {
		// Импорт пакета есть (в нём живёт и перехватчик решений), а держателя
		// никто не поднимает. Это ЗАКОННЫЙ ноль, и он измерен, а не предположен.
		return 0, "держатель не поднят ни одним композиционным корнем", nil
	}
	return n, fmt.Sprintf("заведений %d (%s)", n, strings.Join(where, ", ")), nil
}

// outOfPoolFromValues — приставка ПРОИСХОЖДЕНИЯ величины, взятой из значений
// стека.
//
// Стоит константой, а не набирается текстом дважды, потому что её ЧИТАЕТ
// перепись соседнего гейта: разойдись написание с чтением — перепись назвала бы
// источником не то, что вернул резолвер, и разошлась бы молча.
const outOfPoolFromValues = "значения стека: "

// subscriptionStreamCeiling — потолок одновременных потоков подписки на реплику.
//
// # Величина берётся ОТТУДА, ГДЕ ОНА РЕШАЕТСЯ
//
// Порядок ровно тот, которым величину получает процесс при старте:
//
//  1. чарт рендерит ключ настроек ИЗ ЗНАЧЕНИЙ → побеждает значение стека
//     (профиль поверх умолчаний умбреллы поверх умолчаний подчарта). Умолчание
//     кода в этом случае не действует НИКОГДА: раскладчик читает то, что ему
//     подставили;
//  2. чарт ключа не рендерит вовсе → побеждает умолчание кода, и читать его
//     разбором исходника — единственный способ его узнать.
//
// # Третьего исхода нет, а «ключ есть, значения нет» — ОТКАЗ
//
// Чарт, рендерящий ключ не из значений (литерал в шаблоне), и значения, не
// несущие названного шаблоном пути, оба означают одно: побеждающее значение
// чтением значений не выводится. Откат к умолчанию кода здесь был бы худшим из
// ответов — он вернул бы величину, которой процесс НЕ УВИДИТ, и сумма посадки
// сошлась бы по неверному слагаемому. Неизвестное слагаемое не бывает нулём и
// не бывает чужим умолчанием.
//
// # Почему ключ настройки, а не слово
//
// Опознаётся не имя поля и не комментарий, а КЛЮЧ НАСТРОЙКИ — то, чем величина
// названа снаружи процесса. Форм записи в дереве по две с каждой стороны, и все
// четыре законны: в коде — тег поля (`envconfig`) и умолчание раскладчика
// (`SetDefault`); в чарте — ключ файла настроек и имя переменной окружения.
// Форма, о которой резолвер не знает, даёт не ноль, а ОТКАЗ.
//
// Предпосылку этого порядка стережёт `TestOutOfPoolCeilingIsReadWhereItIsDecided`:
// снимут объявление из чарта — величина обязана поехать обратно в умолчание
// кода, и проба потребует именно этого.
func subscriptionStreamCeiling(t *testing.T, c outOfPoolCtx) (int, string, error) {
	t.Helper()
	wirings, err := subscriptionCeilingWirings(c.chartDir)
	if err != nil {
		return 0, "", err
	}
	if len(wirings) == 0 {
		return subscriptionCeilingFromSource(c.svcDir, c.tree)
	}

	paths := map[string]bool{}
	var opaque []string
	for _, w := range wirings {
		if w.Path == "" {
			opaque = append(opaque, fmt.Sprintf("%s (%q)", w, w.Text))
			continue
		}
		paths[w.Path] = true
	}
	if len(opaque) > 0 {
		return 0, "", fmt.Errorf(
			"чарт объявляет ключ потолка процессу, но не из значений (%s): побеждающее значение "+
				"не выводится чтением значений, а умолчание кода этим объявлением уже перебито. "+
				"Слагаемое не выведено, и нулём оно не бывает",
			strings.Join(opaque, ", "))
	}
	if len(paths) > 1 {
		names := make([]string, 0, len(paths))
		for p := range paths {
			names = append(names, p)
		}
		sort.Strings(names)
		return 0, "", fmt.Errorf(
			"чарт рендерит ключ потолка из НЕСКОЛЬКИХ путей значений (%s): побеждающее значение "+
				"неопределимо чтением", strings.Join(names, ", "))
	}
	var path string
	for p := range paths {
		path = p
	}

	raw, ok := lookup(c.values, strings.Split(path, ".")...)
	if !ok {
		return 0, "", fmt.Errorf(
			"чарт рендерит ключ потолка из значений %s (%s), а такого ключа в значениях стека "+
				"нет: шаблон подставит пустое, и величиной станет не умолчание кода, а пустота",
			path, wirings[0])
	}
	n, ok := asInt(raw)
	if !ok {
		return 0, "", fmt.Errorf(
			"значение %s = %v (%s) не читается числом: величина посадки обязана быть числом",
			path, raw, wirings[0])
	}
	if n <= 0 {
		return 0, "", fmt.Errorf(
			"потолок потоков объявлен значениями как %d (%s = %d, рендерит %s) — величина "+
				"посадки, а не вкус", n, path, n, wirings[0])
	}
	return n, fmt.Sprintf("%s%s = %d (рендерит %s)", outOfPoolFromValues, path, n, wirings[0]), nil
}

// subscriptionCeilingFromSource — умолчание, вкомпилированное в службу.
//
// Действует ТОЛЬКО когда чарт ключа не объявляет: иначе умолчание перебито
// объявлением и побеждающим значением не является.
func subscriptionCeilingFromSource(svcDir string, tr *outOfPoolTree) (int, string, error) {
	var found []string
	best, where := 0, ""
	err := filepath.WalkDir(filepath.Join(tr.root, svcDir), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return fmt.Errorf("разбор %s: %w", path, perr)
		}
		rel, _ := filepath.Rel(tr.root, path)
		rel = filepath.ToSlash(rel)
		ast.Inspect(file, func(n ast.Node) bool {
			// Форма 1 — тег поля настройки.
			if f, ok := n.(*ast.Field); ok && f.Tag != nil {
				raw, uerr := strconv.Unquote(f.Tag.Value)
				if uerr != nil {
					return true
				}
				tag := reflect.StructTag(raw)
				key := tag.Get("envconfig")
				if key == "" {
					key = tag.Get("mapstructure")
				}
				if subscriptionCeilingKey(key) {
					if v, cerr := strconv.Atoi(tag.Get("default")); cerr == nil {
						best, where = v, rel+" (умолчание тега "+key+")"
						found = append(found, where)
					}
				}
				return true
			}
			// Форма 2 — умолчание раскладчика настроек.
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 2 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "SetDefault" {
				return true
			}
			key, kerr := litString(call.Args[0])
			if kerr != nil || !subscriptionCeilingKey(key) {
				return true
			}
			num, nok := call.Args[1].(*ast.BasicLit)
			if !nok || num.Kind != token.INT {
				return true
			}
			if v, cerr := strconv.Atoi(num.Value); cerr == nil {
				best, where = v, rel+" (умолчание ключа "+key+")"
				found = append(found, where)
			}
			return true
		})
		return nil
	})
	if err != nil {
		return 0, "", err
	}
	switch {
	case len(found) == 0:
		return 0, "", fmt.Errorf(
			"служба поднимает сервер подписки, а потолка одновременных потоков не объявляет ни одной " +
				"известной формой (тег настройки либо умолчание раскладчика, ключ …subscription…max…streams). " +
				"Слагаемое не выведено, а неизвестное слагаемое не бывает нулём")
	case len(found) > 1:
		return 0, "", fmt.Errorf(
			"потолок потоков объявлен НЕСКОЛЬКО раз (%s): побеждающее значение неопределимо чтением",
			strings.Join(found, ", "))
	case best <= 0:
		return 0, "", fmt.Errorf("потолок потоков объявлен как %d (%s) — величина посадки, а не вкус", best, where)
	}
	return best, where, nil
}

// subscriptionCeilingKey — ключ настройки потолка потоков РЕПЛИКИ в любом
// написании. Разделители у двух раскладчиков разные (`_` и `-`), регистр тоже,
// поэтому написание нормализуется.
//
// # Соседняя величина отсекается ЯВНО, и она в дереве ЖИВАЯ
//
// Рядом с потолком реплики живёт потолок НА ВЫЗЫВАЮЩЕГО
// (`…_MAX_STREAMS_PER_SUBJECT` — объявлен чартом края и его значениями, строкой
// ниже потолка реплики). Он тоже «subscription» и тоже «max streams», но это
// ДРУГАЯ величина: она ограничивает одного вызывающего, а соединений реплика
// держит по ПЕРВОЙ. Не отсеки соседа — распознаватель взял бы его за предмет
// там, где предмет объявлен строкой выше, и подменил бы слагаемое молча.
func subscriptionCeilingKey(key string) bool {
	k := strings.NewReplacer("-", "_", ".", "_").Replace(strings.ToLower(key))
	if !strings.Contains(k, "subscription") || !strings.Contains(k, "max_streams") {
		return false
	}
	return !strings.Contains(k, "per_subject")
}

// coarseCeilingText — ГРУБОЕ зеркало: текст несёт слова ключа потолка где
// угодно, включая комментарии, и соседняя величина здесь НЕ отсекается.
//
// Грубость — предмет этого предиката, а не его недостаток. Он отвечает на
// вопрос «есть ли в этом файле вообще что-нибудь про потолок потоков», и любое
// сужение приблизило бы его к точному, то есть лишило бы роли зеркала: два
// предиката, ослепшие одинаково, зеркалом друг другу не являются.
func coarseCeilingText(text string) bool {
	k := strings.NewReplacer("-", "_", ".", "_").Replace(strings.ToLower(text))
	return strings.Contains(k, "subscription") && strings.Contains(k, "max_streams")
}

// ─────────────────────────────────────────────────────────────────────────────
// ГДЕ ВЕЛИЧИНА РЕШАЕТСЯ НА САМОМ ДЕЛЕ: ОБЪЯВЛЕНИЕ ЧАРТА ПОБЕЖДАЕТ УМОЛЧАНИЕ КОДА

// ceilingWiring — одно место чарта, где потолок потоков объявляется ПРОЦЕССУ.
//
// «Объявляется процессу» — это ключ файла настроек либо имя переменной
// окружения, то есть ровно то написание, которое читает раскладчик настроек
// при старте. Путь в значениях (`subscription.maxStreams`) под это определение
// НЕ подпадает и подпасть не может: предикат ключа требует разделителя между
// `max` и `streams`, а в написании значений его нет. Совпадение не случайное —
// оно и разделяет «чем величина названа СНАРУЖИ процесса» от «чем она названа
// в значениях чарта».
type ceilingWiring struct {
	File string
	Line int
	// Path — путь в значениях, откуда шаблон берёт величину. ПУСТОЙ путь
	// означает «ключ объявлен, но не из значений»: побеждающее значение тогда
	// чтением значений не выводится, и это ОТКАЗ, а не откат к умолчанию кода.
	// Откат был бы худшим из ответов — он вернул бы величину, которой процесс
	// не увидит.
	Path string
	Text string
}

func (w ceilingWiring) String() string { return fmt.Sprintf("%s:%d", w.File, w.Line) }

var (
	// chartValuesRef — ссылка на значения в выражении шаблона.
	chartValuesRef = regexp.MustCompile(`\.Values\.([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*)`)
	// chartKeyToken — отдельное слово строки шаблона. Ключ опознаётся по
	// СЛОВУ, а не вхождением в строку целиком: вхождение в строку целиком
	// засчитывало бы «subscription» из одного места и «max_streams» из
	// другого, и находка называла бы файл вместо места.
	chartKeyToken = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_.-]*`)
)

// ceilingWiringWindow — сколько строк НИЖЕ ключа осматривается в поисках
// выражения.
//
// Форм записи в дереве две, и обе законны: ключ файла настроек несёт выражение
// на СВОЕЙ строке (`subscription-max-streams: {{ … }}`), переменная окружения —
// на СЛЕДУЮЩЕЙ (`- name: …` / `value: "{{ … }}"`). Окно ровно на эти две формы
// и рассчитано: шире оно начало бы приписывать ключу чужое выражение соседней
// переменной.
const ceilingWiringWindow = 1

// subscriptionCeilingWirings — места чарта, объявляющие потолок процессу.
//
// Пустой `chartDir` означает «чарта у этой службы нет», и это законный вход:
// тогда величину решает умолчание кода. Нечитаемый непустой — ОТКАЗ: «чарт не
// прочитан» обязано быть отличимо от «чарт ключа не объявляет».
func subscriptionCeilingWirings(chartDir string) ([]ceilingWiring, error) {
	if chartDir == "" {
		return nil, nil
	}
	if st, err := os.Stat(chartDir); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("каталог чарта %s не читается (%v) — «ключ не объявлен» стало бы "+
			"неотличимо от «чарт не прочитан»", chartDir, err)
	}
	var out []ceilingWiring
	err := filepath.WalkDir(chartDir, func(path string, d os.DirEntry, werr error) error {
		if werr != nil || d.IsDir() {
			return werr
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".yaml", ".yml", ".tpl":
		default:
			return nil
		}
		raw, rerr := os.ReadFile(path) // #nosec G304 -- обход собственного дерева
		if rerr != nil {
			return rerr
		}
		lines := strings.Split(string(raw), "\n")
		for i, line := range lines {
			// Судится ИСПОЛНЯЕМАЯ часть: комментарий, называющий ту же
			// переменную (а такие комментарии стоят прямо над каждым из пяти
			// объявлений), объявлением не является.
			code := strings.TrimSpace(line)
			if code == "" || strings.HasPrefix(code, "#") {
				continue
			}
			if !lineDeclaresCeilingKey(code) {
				continue
			}
			w := ceilingWiring{File: filepath.ToSlash(path), Line: i + 1, Text: code}
			for j := i; j < len(lines) && j <= i+ceilingWiringWindow; j++ {
				if m := chartValuesRef.FindStringSubmatch(lines[j]); m != nil {
					w.Path = m[1]
					break
				}
			}
			out = append(out, w)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out, nil
}

// lineDeclaresCeilingKey — строка объявляет ключ потолка процессу.
func lineDeclaresCeilingKey(line string) bool {
	for _, tok := range chartKeyToken.FindAllString(line, -1) {
		if subscriptionCeilingKey(tok) {
			return true
		}
	}
	return false
}

// chartCarriesCeilingKeyCoarsely — ГРУБЫЙ предикат по тексту файла целиком.
//
// Это ровно тот предикат, которым прежняя проба стерегла снятую теперь
// предпосылку («ключ не объявляем чартом»). Он оставлен НЕ как второе изложение
// одного предмета, а как ЗЕРКАЛО построчного распознавателя: они меряют одно
// разной зернистостью, поэтому построчный, переставший что-либо узнавать, не
// сможет молчать вместе с грубым. Без зеркала слепота распознавателя выглядела
// бы как «чарт ключа не объявляет» — то есть как законный вход.
func chartCarriesCeilingKeyCoarsely(chartDir string) ([]string, error) {
	if chartDir == "" {
		return nil, nil
	}
	var out []string
	err := filepath.WalkDir(chartDir, func(path string, d os.DirEntry, werr error) error {
		if werr != nil || d.IsDir() {
			return werr
		}
		raw, rerr := os.ReadFile(path) // #nosec G304 -- обход собственного дерева
		if rerr != nil {
			return rerr
		}
		if coarseCeilingText(string(raw)) {
			out = append(out, filepath.ToSlash(path))
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

// packageNameOf — имя, под которым пакет виден в коде, по пути импорта.
//
// Последним сегментом это НЕ вычисляется: путь версионированного модуля
// оканчивается номером старшей версии (`github.com/jackc/pgx/v5`), а пакет
// зовётся `pgx`. Первая редакция считала сегмент и потому не опознала НИ ОДНОГО
// захвата «своего соединения» — при трёх живых в дереве. Молчание при этом
// выглядело как чистое дерево: перепись печатала «захватов 0».
func packageNameOf(path string) string {
	seg := strings.Split(path, "/")
	last := seg[len(seg)-1]
	if len(seg) > 1 && len(last) > 1 && last[0] == 'v' && isAllDigits(last[1:]) {
		return seg[len(seg)-2]
	}
	return last
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) > 0
}

func litString(e ast.Expr) (string, error) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", fmt.Errorf("не строковый литерал")
	}
	return strconv.Unquote(lit.Value)
}

// ─────────────────────────────────────────────────────────────────────────────
// СЛАГАЕМОЕ СЛУЖБЫ И ПОЛНОТА АРИФМЕТИКИ

const kindOutOfPoolUnattributed = "неучтённый захват вне пула"

// serviceSourceDir — каталог исходников службы по ключу её значений в умбрелле.
//
// Соответствие спрашивается у ЕДИНСТВЕННОГО его владельца
// (`internal/productnaming`), а не выводится здесь заново приставкой имени
// платформы: с тех пор как часть продукта получила собственное имя (`kaname`),
// приставка их больше не связывает, и вывод по ней молча не находил каталога.
// Ненайденный каталог — НАХОДКА у вызывающего, а не молчаливый ноль.
func serviceSourceDir(root, alias string) (string, bool) {
	candidates := []string{alias}
	if dir, ok := productnaming.ServiceDir(alias); ok {
		candidates = append(candidates, dir)
	}
	for _, name := range candidates {
		dir := filepath.Join("services", name)
		if st, err := os.Stat(filepath.Join(root, dir)); err == nil && st.IsDir() {
			return filepath.ToSlash(dir), true
		}
	}
	return "", false
}

// chartServiceDirHint — вторая координата в тексте находки: каталог, который
// владелец соответствия называет для этого ключа. Пусто — если ключ не
// распознан как часть продукта вовсе.
func chartServiceDirHint(alias string) string {
	if dir, ok := productnaming.ServiceDir(alias); ok {
		return dir
	}
	return "(ключ не распознан как часть продукта)"
}

// outOfPoolPerReplica — сколько соединений реплика службы держит ВНЕ пула.
//
// Складывается из двух источников, и оба выведены из дерева:
//
//   - захваты в СОБСТВЕННОМ дереве службы — каждый стоит одного соединения
//     (пробуждение реконсиляции у службы личностей — ровно такой случай);
//   - записи каталога держателей общего фундамента — по импорту пакета.
//
// Третий возврат — виды, чьё слагаемое вывести НЕ УДАЛОСЬ. Он отделён от нуля
// намеренно: сумма с неизвестным слагаемым проходит любую проверку.
func outOfPoolPerReplica(
	t *testing.T, alias, chartDir string, values map[string]any, tree *outOfPoolTree,
) (total int, why, unknown []string) {
	t.Helper()
	svcDir, ok := serviceSourceDir(tree.root, alias)
	if !ok {
		return 0, nil, []string{fmt.Sprintf(
			"каталога исходников не найдено ни по %q, ни по %q — захваты вне пула этой службы "+
				"не осмотрены вовсе", alias, chartServiceDirHint(alias))}
	}

	own := 0
	var ownWhere []string
	for _, c := range tree.captures {
		if strings.HasPrefix(c.File, svcDir+"/") {
			own++
			ownWhere = append(ownWhere, c.String())
		}
	}
	if own > 0 {
		total += own
		why = append(why, fmt.Sprintf("захваты в своём дереве: %d (%s)", own, strings.Join(ownWhere, ", ")))
	}

	for _, h := range outOfPoolHolders {
		if len(tree.importers(svcDir+"/", h.Pkg)) == 0 {
			continue // пакета-держателя эта служба не импортирует вовсе
		}
		// Импорт сделал службу кандидатом; поднят ли держатель — решает его
		// конструктор. Ноль вызовов означает, что взята переиспользуемая часть
		// пакета, а держателя нет: слагаемое ноль, и ноль ИЗМЕРЕН.
		if h.RaisedBy != "" {
			if n, _ := tree.callSites(svcDir+"/", h.RaisedBy); n == 0 {
				continue
			}
		}
		n, where, err := h.PerReplica(t, outOfPoolCtx{
			svcDir: svcDir, chartDir: chartDir, values: values, tree: tree,
		})
		if err != nil {
			unknown = append(unknown, fmt.Sprintf("%s — %v", h.Kind, err))
			continue
		}
		if n == 0 {
			continue
		}
		total += n
		why = append(why, fmt.Sprintf("%s: %d (%s)", h.Kind, n, where))
	}
	sort.Strings(unknown)
	return total, why, unknown
}

// unattributedCaptures — ЗАМЕР ПОЛНОТЫ арифметики.
//
// Всякий захват соединения вне пула обязан быть кому-то приписан: службе (если
// лежит в её дереве) либо записи каталога держателей. Захват, не приписанный
// никому, — находка, а не молчание: именно так «учли то, что вспомнили»
// перестаёт называться полнотой.
//
// Зеркальная половина: запись каталога, чьего файла захвата в дереве больше нет,
// — ТОЖЕ находка. Иначе она пережила бы свой предмет и продолжала бы числиться
// учтённой, а следующий читатель принял бы перечень за действующий.
func unattributedCaptures(t *testing.T, tree *outOfPoolTree) []poolFinding {
	t.Helper()
	known := map[string]string{}
	for _, h := range outOfPoolHolders {
		known[h.Site] = h.Kind
	}
	seen := map[string]bool{}

	var out []poolFinding
	for _, c := range tree.captures {
		if strings.HasPrefix(c.File, "services/") {
			continue // приписан своей службе by construction
		}
		if kind, ok := known[c.File]; ok {
			seen[c.File] = true
			_ = kind
			continue
		}
		out = append(out, poolFinding{
			stack: "дерево", subject: c.File, kind: kindOutOfPoolUnattributed,
			why: fmt.Sprintf("%s: захват соединения ВНЕ пула не приписан ни службе, ни записи "+
				"каталога держателей — значит он не входит в сумму «реплики × (пул + вне пула)», "+
				"и та проходит по отсутствию слагаемого, а не по сходимости. Заведите запись "+
				"в outOfPoolHolders с числом соединений на реплику", c.String()),
		})
	}
	for _, h := range outOfPoolHolders {
		if seen[h.Site] {
			continue
		}
		out = append(out, poolFinding{
			stack: "дерево", subject: h.Site, kind: kindOutOfPoolUnattributed,
			why: fmt.Sprintf("записи каталога держателей %q больше нечего учитывать: захвата "+
				"соединения в %s нет. Снимите запись — перечень, переживший свой предмет, "+
				"читается как действующий", h.Kind, h.Site),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key() < out[j].key() })
	return out
}
