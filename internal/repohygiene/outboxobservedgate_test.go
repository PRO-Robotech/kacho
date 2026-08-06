// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// outboxobservedgate_test.go — гейт на КЛАСС: у каждой дренируемой очереди есть
// свой сканер состояния.
//
// # Предмет
//
// Дренаж сообщает о СОБЫТИЯХ (строка применилась, строка отравилась) и делает это
// в лог. «В очереди лежит N строк, старейшей M секунд» — СОСТОЯНИЕ, и его не
// производит никто, кроме периодического сканера таблицы. Без такого сканера
// застрявшая очередь неотличима от пустой: обе молчат одинаково.
//
// Класс уже наблюдался на платформе целиком — очередь, не доставившая НИ ОДНОЙ
// строки за всю свою жизнь, выглядела исправной, потому что заметить это было
// нечем; правило `data-integrity.md` («ноль доставленных строк за всю жизнь
// очереди обязано быть заметно») выведено ровно из этого случая. Починили тогда
// одну очередь; братья пережили правку.
//
// # Почему гейт, а не четыре правки
//
// Сервисная проба про свою очередь не умеет заметить собственное отсутствие: у
// сервиса, где сканера нет вовсе, нет и теста, который бы об этом сказал. Общего
// места, где видно «сколько очередей дренится и сколько из них наблюдаемо», в
// дереве не было — а нужно именно оно.
//
// # Что читается
//
// Разбор AST композитных литералов в composition root каждого сервиса, а не
// текст: `drainer.Config{Table: X}` даёт множество дренируемых, а
// `CollectorConfig{Table: X}` — множество наблюдаемых. Имя таблицы резолвится
// через константы того же сервиса, поэтому одноимённые константы разных сервисов
// (`fgaRegisterOutboxTable` живёт в vpc, nlb и storage сразу) не сливаются.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
)

// outboxDirectionSplitExempt — очереди, у которых разложение по направлению не
// имеет предмета, с указанием ПОЧЕМУ. Запись живёт, пока очередь дренится: гейт
// ниже падает на записи, которой больше нечего исключать.
//
// Разложение по направлению отвечает на вопрос, который сводные серии задать не
// могут: «доезжает ли ВТОРАЯ половина». Он осмыслен только там, где половин
// действительно две.
//
// ПРЕДПОСЫЛКА — какие события очередь несёт — здесь НЕ пересказывается: она
// объявлена один раз в `declaredQueueEventDictionary` и сверяется с БД гейтом
// (outboxeventdictionary_test.go). Прежняя редакция этой записи предпосылку
// пересказывала — и пересказ был ложным: «событие ровно одного вида» при
// словаре из двух. Вывод устоял, основание — нет.
var outboxDirectionSplitExempt = map[string]string{
	"kacho_iam.provider_compensation_outbox": "" +
		"видов события два, но НАПРАВЛЕНИЕ у них одно: оба — снятия (клиента у " +
		"провайдера и доверительного гранта), ни одно ничего не устанавливает. " +
		"Обратной половины у потока нет by construction — постановка идёт синхронным " +
		"вызовом на пути запроса и в очередь не попадает, — поэтому разложение по " +
		"направлению разложило бы очередь на неё саму и на пустоту.",
	"kacho_iam.subject_change_outbox": "" +
		"очередь уведомлений об изменении субъекта. Видов события семь, и среди них " +
		"есть выглядящие парными (выдать ↔ отозвать), но направление у всех одно: " +
		"уведомить о том, что решения субъекта устарели. Обратного события — «вернуть " +
		"кэшированное решение» — не существует ни в словаре, ни у принимающей стороны.",
}

// TestEveryDrainedOutboxIsObserved — каждая очередь, для которой поднят дренаж,
// имеет сканер состояния той же таблицы.
//
// Проверено инъекцией в обе стороны: снятие любого из четырёх сканеров красит
// гейт и печатает имя таблицы и сервис; добавление дренажа со сканером он
// пропускает молча.
func TestEveryDrainedOutboxIsObserved(t *testing.T) {
	root := repoRoot(t)
	inv := outboxWiringInventory(t, root)

	// «Ноль находок» обязано быть отличимо от «ноль прочитанного».
	if inv.filesRead == 0 {
		t.Fatalf("гейт не прочитал ни одного файла composition root — предпосылка обхода "+
			"сломана, молчание ничего не доказывает (корень %s)", root)
	}
	if len(inv.drained) == 0 {
		t.Fatalf("гейт не нашёл НИ ОДНОЙ дренируемой очереди в %d прочитанных файлах — "+
			"распознавание сломано (ищется композитный литерал drainer.Config с полем Table)",
			inv.filesRead)
	}

	// Собственная слепая зона гейта, названная вслух. Координата очереди
	// резолвится разбором исходника, поэтому адрес, вычисляемый в рантайме
	// (поле чужой структуры, вызов функции, конкатенация), для него неотличим от
	// отсутствующего — и такая проводка была бы НЕВИДИМА обоим утверждениям выше.
	// Молчать об этом нельзя: гейт, у которого предмет может уехать из поля
	// зрения незаметно, — сам экземпляр формы без содержания.
	for _, u := range inv.unresolved {
		t.Errorf("%s: имя таблицы задано выражением, которое не резолвится разбором "+
			"исходника — гейт не видит эту очередь и молчал бы о ней «чисто». Назови "+
			"таблицу строковой константой пакета (та же, что стоит в конфигурации "+
			"дренажа), а не полем чужой структуры.", u)
	}

	drained := sortedKeys(inv.drained)
	observed := sortedKeys(inv.observed)
	t.Logf("прочитано файлов composition root: %d; дренируемых очередей: %d; со сканером состояния: %d\n"+
		"  дренаж:  %s\n  сканер:  %s",
		inv.filesRead, len(drained), len(observed),
		strings.Join(drained, "\n           "), strings.Join(observed, "\n           "))

	for _, table := range drained {
		if _, ok := inv.observed[table]; ok {
			continue
		}
		t.Errorf("очередь %s дренится (%s), но её состояние не сканирует никто: "+
			"глубины, возраста головы и числа отравленных строк не производит ни одна серия. "+
			"Застрявшая очередь неотличима от пустой — обе молчат. Подними "+
			"outbox/metrics.Collector на эту таблицу рядом с дренажом.",
			table, strings.Join(inv.drained[table], ", "))
	}
}

// TestEveryDrainedOutboxIsSplitByDirection — очередь, несущая обе половины
// (выдачу и снятие), обязана публиковать их порознь либо иметь запись
// исключения с обоснованием.
//
// Сводные серии на такой очереди здоровы при полностью мёртвом снятии: выдачи
// дренятся непрерывно, поэтому глубина мала и голова молода — что бы ни
// происходило со второй половиной. «Работает» и «ни разу не отозвано» дают
// ОДИНАКОВУЮ картину.
func TestEveryDrainedOutboxIsSplitByDirection(t *testing.T) {
	root := repoRoot(t)
	inv := outboxWiringInventory(t, root)

	if inv.filesRead == 0 {
		t.Fatalf("гейт не прочитал ни одного файла composition root (корень %s)", root)
	}

	for _, table := range sortedKeys(inv.observed) {
		if inv.split[table] {
			continue
		}
		if why, exempt := outboxDirectionSplitExempt[table]; exempt {
			if strings.TrimSpace(why) == "" {
				t.Errorf("исключение %s без обоснования: обязано называть, почему у очереди "+
					"нет второй половины", table)
			}
			continue
		}
		t.Errorf("очередь %s наблюдается только сводно: разложения по направлению нет. "+
			"Сводные серии остаются здоровыми при полностью мёртвом снятии, потому что "+
			"выдачи дренятся непрерывно. Задай CollectorConfig.Directions либо заведи "+
			"запись в outboxDirectionSplitExempt с обоснованием.", table)
	}
}

// TestOutboxDirectionExemptionsHaveSubject — исключение живёт, пока у него есть
// предмет: очередь, исчезнувшая из дерева, унаследует следующую слепую зону.
func TestOutboxDirectionExemptionsHaveSubject(t *testing.T) {
	root := repoRoot(t)
	inv := outboxWiringInventory(t, root)

	for table := range outboxDirectionSplitExempt {
		if _, ok := inv.drained[table]; !ok {
			t.Errorf("исключение %s больше не имеет предмета: такой дренируемой очереди в "+
				"дереве нет. Удали запись.", table)
		}
	}
}

type outboxInventory struct {
	// drained — таблица → координаты (сервис:файл), где поднят дренаж.
	drained map[string][]string
	// observed — таблица → координаты, где поднят сканер состояния.
	observed map[string][]string
	// split — у сканера этой таблицы задано разложение по направлению.
	split map[string]bool
	// partition — таблица → ключ партиции порядка, объявленный проводкой
	// дренажа (drainer.Config.PartitionColumn). Ключа нет в карте = проводка
	// поля не задаёт, то есть заявляет коммутативность потока. Читается гейтом
	// порядка (outboxorderinggate_test.go).
	partition map[string]string
	// partitionUnresolved — координаты проводок, где поле ключа партиции задано,
	// но его значение разбором исходника не восстанавливается. Такая проводка
	// для гейта порядка неотличима от «ключ не задан», поэтому она называется
	// вслух, а не проглатывается.
	partitionUnresolved []string
	// unresolved — координаты проводок, чьё поле Table не резолвится разбором.
	unresolved []string
	filesRead  int
}

// outboxInventoryCache — разобранный инвентарь на корень дерева.
//
// Семь проб (три про наблюдаемость, три про порядок, одна про словарь событий)
// спрашивают ОДНО И ТО ЖЕ, а один разбор стоит двух проходов по всем не-тестовым
// файлам каждого сервиса: сперва за строковыми константами, потом за
// композитными литералами.
//
// # Величина выигрыша — и почему она приведена парами, а не одним числом
//
// Замер сумм собственного времени проб, по три прогона на условие, флаги
// `-short -count=1`:
//
//	условие      | без кэша          | с кэшем
//	-------------|-------------------|------------------
//	под -race    | 12.8 / 13.0 / 15.0| 2.00 / 2.11 / 2.18
//	без -race    | 1.93 / 1.96 / 1.97| 0.32 / 0.33 / 0.33
//
// Выигрыш — примерно ШЕСТИКРАТНЫЙ, и он один и тот же в обеих строках. А вот
// абсолютные величины между строками отличаются на порядок, и это ловушка, в
// которую здесь уже попались: сообщение коммита, вводившего кэш, назвало пару
// «12.54 c → 0.36 c», то есть «до» из верхней строки и «после» из НИЖНЕЙ. Флаги
// в том сообщении объявлены с `-race`. Выигрыш оказался завышен впятеро с
// половиной, и заметил это приёмщик, а не автор: он не смог воспроизвести
// «после» ни при одном наборе флагов — потому что при ОБЪЯВЛЕННЫХ флагах такой
// величины не бывает.
//
// Мораль для следующего замера: величина без условия — не величина. Условие
// здесь — флаги, набор проб и машина; менять их между «до» и «после» нельзя даже
// случайно.
//
// # Зачем кэш вообще
//
// Пакет `internal/repohygiene` целиком под `-race` на машине разработчика:
// 160 / 201 / 202 c при потолке `go test` в 300 c на бинарь пакета. Разброс тут
// сам по себе предмет — он больше трети от потолка, и вариативность ранера этот
// запас уже съедала (один и тот же коммит: 192 c успехом и таймаут на 300 c).
// Семь одинаковых разборов — не то, на что стоит тратить запас.
//
// Кэш безопасен по построению: ни одна проба этого пакета в читаемое дерево НЕ
// пишет (те, кому нужно другое дерево, строят его во временном каталоге и зовут
// другой разборщик), поэтому в пределах одного процесса ответ не может
// устареть. Ключ — корень, так что появление второго корня даст свою запись, а
// не чужой ответ. Кладётся только УСПЕШНЫЙ разбор: неудачный завершается
// t.Fatalf внутри, до присвоения.
//
// Объём осмотренного при этом не подделывается: `filesRead` лежит в самом
// инвентаре, поэтому проба, получившая кэш, печатает и проверяет НАСТОЯЩЕЕ число
// прочитанных файлов — то самое, на котором инвентарь построен в этом же
// процессе. Требование «ноль находок отличимо от ноль прочитанного» остаётся
// выполненным: пустой разбор в кэш попадёт так же, как непустой, и первая же
// проба на нём упадёт.
var (
	outboxInventoryMu    sync.Mutex
	outboxInventoryCache = map[string]outboxInventory{}
)

// outboxWiringInventory разбирает composition root'ы всех сервисов и шлюза.
//
// Резолв имени таблицы: значение поля Table — либо строковый литерал, либо
// идентификатор/селектор, чьё значение объявлено константой ГДЕ-ТО В ТОМ ЖЕ
// сервисе. Константы собираются по сервису, а не по дереву: одноимённые
// константы разных сервисов не должны сливаться в одну запись.
func outboxWiringInventory(t *testing.T, root string) outboxInventory {
	t.Helper()
	outboxInventoryMu.Lock()
	cached, ok := outboxInventoryCache[root]
	outboxInventoryMu.Unlock()
	if ok {
		return cached
	}
	inv := outboxInventory{
		drained:   map[string][]string{},
		observed:  map[string][]string{},
		split:     map[string]bool{},
		partition: map[string]string{},
	}

	servicesDir := filepath.Join(root, "services")
	entries, err := os.ReadDir(servicesDir)
	if err != nil {
		t.Fatalf("читаю %s: %v", servicesDir, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		svc := e.Name()
		svcRoot := filepath.Join(servicesDir, svc)
		consts := serviceConstStrings(t, svcRoot)
		files := goFilesUnder(t, svcRoot)
		for _, path := range files {
			inv.filesRead++
			scanOutboxWiring(t, path, svc, root, consts, &inv)
		}
	}
	outboxInventoryMu.Lock()
	outboxInventoryCache[root] = inv
	outboxInventoryMu.Unlock()
	return inv
}

// serviceConstStrings собирает все строковые константы сервиса: имя → значение.
// Имя берётся без квалификатора пакета, поэтому `clients.ProviderCompensationTable`
// резолвится по хвосту селектора.
func serviceConstStrings(t *testing.T, svcRoot string) map[string]string {
	t.Helper()
	out := map[string]string{}
	fset := token.NewFileSet()
	for _, path := range goFilesUnder(t, svcRoot) {
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			continue
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					v, err := strconv.Unquote(lit.Value)
					if err != nil {
						continue
					}
					out[name.Name] = v
				}
			}
		}
	}
	return out
}

// goFilesUnder — не-тестовые .go файлы поддерева, взятые из ИНДЕКСА git, а не
// обходом диска.
//
// Разница не косметическая: под services/ на любой машине, где поднимали стенд
// или гоняли прогоны, лежат игнорируемые каталоги (распаковки чартов, рабочие
// копии, отчёты). Обход диска сделал бы вердикт гейта свойством рабочего
// каталога, а не коммита. Пустой корпус treecorpus считает отказом — «ноль
// находок» на «ноль прочитанного» здесь неотличимо от чистого дерева.
func goFilesUnder(t *testing.T, dir string) []string {
	t.Helper()
	tracked, err := treecorpus.UnderWithSuffix(dir, ".go")
	if err != nil {
		t.Fatalf("состав дерева под %s взять неоткуда: %v", dir, err)
	}
	out := make([]string, 0, len(tracked))
	for _, path := range tracked {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

// scanOutboxWiring находит в одном файле композитные литералы drainer.Config и
// CollectorConfig и записывает таблицу, которую они называют.
func scanOutboxWiring(t *testing.T, path, svc, root string, consts map[string]string, inv *outboxInventory) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return
	}
	rel, _ := filepath.Rel(root, path)
	coord := svc + ":" + rel

	ast.Inspect(f, func(n ast.Node) bool {
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		sel, ok := cl.Type.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		// Проводка распознаётся ДО чтения полей: иначе нерезолвящийся Table
		// молча выпал бы из обеих картин вместе со своим литералом.
		kind := ""
		switch sel.Sel.Name {
		case "Config":
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "drainer" {
				kind = "дренаж"
			}
		case "CollectorConfig":
			kind = "сканер"
		}
		if kind == "" {
			return true
		}

		var table string
		var hasTableKey bool
		var hasDirections bool
		var partition string
		var hasPartitionKey bool
		for _, elt := range cl.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok {
				continue
			}
			switch key.Name {
			case "Table":
				hasTableKey = true
				table = resolveStringExpr(kv.Value, consts)
			case "Directions":
				hasDirections = true
			case "PartitionColumn":
				hasPartitionKey = true
				partition = resolveStringExpr(kv.Value, consts)
			}
		}
		if table == "" {
			if hasTableKey {
				inv.unresolved = append(inv.unresolved,
					coord+" ("+kind+", строка "+strconv.Itoa(fset.Position(cl.Pos()).Line)+")")
			}
			return true
		}
		if kind == "дренаж" {
			inv.drained[table] = append(inv.drained[table], coord)
			switch {
			case hasPartitionKey && partition != "":
				inv.partition[table] = partition
			case hasPartitionKey:
				inv.partitionUnresolved = append(inv.partitionUnresolved,
					coord+" (строка "+strconv.Itoa(fset.Position(cl.Pos()).Line)+")")
			}
			return true
		}
		inv.observed[table] = append(inv.observed[table], coord)
		if hasDirections {
			inv.split[table] = true
		}
		return true
	})
}

// resolveStringExpr — строковый литерал, идентификатор-константа либо селектор
// вида `pkg.Const`; иначе пустая строка.
func resolveStringExpr(e ast.Expr, consts map[string]string) string {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return ""
		}
		s, err := strconv.Unquote(v.Value)
		if err != nil {
			return ""
		}
		return s
	case *ast.Ident:
		return consts[v.Name]
	case *ast.SelectorExpr:
		return consts[v.Sel.Name]
	}
	return ""
}

func sortedKeys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
