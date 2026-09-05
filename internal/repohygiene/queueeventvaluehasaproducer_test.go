// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// queueeventvaluehasaproducer_test.go — вид события, которого никто не пишет,
// это находка, а не запас.
//
// # Предмет
//
// Словарь очереди закрыт ограничением базы, и ограничение читается как обещание:
// «такие события бывают». Значение, которое ни одна строка прод-кода не
// производит, обещает подсистему, которой нет. Оно не безобидно: по нему пишут
// ветки читателей и оговорки в комментариях, а снять его потом некому — за
// значением без производителя никто не отвечает.
//
// Замер, из которого гейт заведён (`kacho@04e2e523f`, 419 не-тестовых файлов
// iam): словарь `subject_change_outbox.op` допускал семь значений; производителей
// не имели два — `jit_revoke` и `bg_revoke`, при том что подсистем
// just-in-time-доступа и фонового отзыва в дереве нет вовсе (предикат
// `git grep -rln 'JITAccess\|jit_access\|JustInTime'` → ноль файлов). Третье,
// `group_member_change`, стояло в ограничении с первого дня схемы и производителя
// не имело до #754 — то есть ведомость admitted вид события, которого продукт не
// умел произвести, и это было ненаблюдаемо.
//
// # Что гейт читает
//
// Производителем считается СТРОКОВЫЙ ЛИТЕРАЛ в не-тестовом Go этого сервиса,
// найденный разбором, а не текстом: упоминание значения в комментарии — не
// производитель, и именно комментарии делали мёртвые значения похожими на живые.
//
// Гранулярность честная и здесь называется: гейт утверждает, что слово живёт в
// словаре продукта, а не что оно попадает ИМЕННО в эту колонку (это требовало бы
// анализа потока данных). Поэтому `binding_grant`/`binding_revoke` он считает
// живыми — их пишут как `event_type`, а в `op` их переводит writer. Мёртвое
// слово он при этом ловит: у мёртвого нет литерала нигде.
package repohygiene

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// retiredQueueEventValues — значения словаря, которые ОСТАЮТСЯ в ограничении, но
// производителя не имеют намеренно, с причиной.
//
// Ключ — «<сервис>:<таблица>:<колонка>:<значение>». Запись самоистекает в обе
// стороны: значение, у которого появился производитель, и значение, которого
// больше нет в живом словаре, — обе находки. Список, которому нечего прощать,
// проверку НЕ роняет: пустой список и есть цель, ради которой он заведён.
//
// Прежде здесь было пусто, и это измерялось: два значения, которым тут было бы
// место (`jit_revoke`, `bg_revoke`), сняты с ограничения миграцией 754001 вместе
// со своим объявлением — исход «снять», а не «простить».
var retiredQueueEventValues = map[string]string{
	"iam:provider_compensation_outbox:event_type:provider.trust_grant.delete": "" +
		"вид намерения «снять доверительный грант у поставщика» снят вместе со своим предметом " +
		"(задача #1124): перечень доверенных издателей стал нашей таблицей и пишется в той же " +
		"транзакции, что строка ключа, — веера обращений к поставщику, который требовалось " +
		"компенсировать, больше не существует. Значение остаётся в ограничении потому, что " +
		"применённые миграции не правятся (ban #5), а сужающая миграция отвергла бы строки, " +
		"доставшиеся от прежней посадки. Производителя у значения нет ни одного; строка такого " +
		"вида, если она где-то осталась, применителю неизвестна и получает ПОСТОЯННЫЙ отказ — " +
		"она не применится и партию не заклинит. Снять запись вместе со значением — при " +
		"следующей правке этого словаря миграцией",
}

// TestQueueEventValueHasAProducer — каждое значение закрытого словаря очереди
// пишется хоть чем-то в прод-коде своего сервиса.
func TestQueueEventValueHasAProducer(t *testing.T) {
	root := repoRoot(t)
	dicts := enumDictionaryInventory(t, root, trackedMigrationSQL)
	wiring := outboxWiringInventory(t, root)

	if dicts.filesRead == 0 {
		t.Fatalf("гейт не прочитал ни одной миграции (корень %s) — предпосылка сломана, "+
			"молчание ничего не доказывает", root)
	}

	tables := make([]string, 0, len(declaredQueueEventDictionary))
	for table := range declaredQueueEventDictionary {
		tables = append(tables, table)
	}
	sort.Strings(tables)

	seenRetired := map[string]bool{}
	valuesChecked, filesRead := 0, 0

	for _, table := range tables {
		svc := queueService(wiring, table)
		if svc == "" {
			t.Errorf("таблица %q не называет сервис ни проводкой, ни схемой — какой "+
				"прод-код считать её производителем, не определено", table)
			continue
		}
		key := svc + ":" + unqualify(table)
		live, closed := dicts.dict[key]
		if !closed {
			t.Errorf("у очереди %s нет закрытого словаря в миграциях: проверять нечего, "+
				"а объявленная перепись declaredQueueEventDictionary при этом есть — "+
				"два места об одном предмете, из которых верно одно", table)
			continue
		}

		svcFiles, err := producerCorpus(root, svc)
		if err != nil {
			t.Errorf("состав прод-кода сервиса %s не прочитан (%v) — остановись здесь: "+
				"«производителей нет» неотличимо от «не смотрели»", svc, err)
			continue
		}
		producers, n := stringLiteralsIn(svcFiles)
		filesRead += n
		if n == 0 {
			t.Errorf("прод-код сервиса %s не прочитан (ноль файлов) — «производителей нет» "+
				"неотличимо от «не смотрели»", svc)
			continue
		}

		cols := make([]string, 0, len(live))
		for col := range live {
			cols = append(cols, col)
		}
		sort.Strings(cols)

		for _, col := range cols {
			for _, v := range live[col] {
				valuesChecked++
				rk := key + ":" + col + ":" + v
				reason, retired := retiredQueueEventValues[rk]
				if retired {
					seenRetired[rk] = true
				}
				switch {
				case producers[v] && retired:
					t.Errorf("%s: значение %q объявлено снятым с производства (%s), но "+
						"производитель у него ЕСТЬ (%s). Послабление пережило свой предмет: "+
						"либо снимай запись, либо снимай производителя.",
						table, v, reason, producers2coord(v, svcFiles))
				case !producers[v] && !retired:
					t.Errorf("%s: словарь колонки %q допускает %q, а в не-тестовом коде %s "+
						"нет ни одного места, которое это значение пишет (прочитано %d файлов). "+
						"Ведомость обещает вид события, которого продукт произвести не умеет. "+
						"Исходов три: научить производить · снять значение вместе с его "+
						"объявлением (миграция, сужающая CHECK) · записать в "+
						"retiredQueueEventValues с причиной.",
						table, col, v, svc, n)
				}
			}
		}
	}

	for rk, reason := range retiredQueueEventValues {
		if !seenRetired[rk] {
			t.Errorf("запись о снятом значении %q (%s) больше нечего исключать: в живом "+
				"словаре такого значения нет. Исключение, у которого исчез предмет, — "+
				"находка: следующую слепую зону оно унаследует молча.", rk, reason)
		}
	}

	t.Logf("прочитано миграций: %d; прочитано не-тестовых файлов сервисов: %d; "+
		"очередей: %d; значений сверено: %d; записей о снятых: %d",
		dicts.filesRead, filesRead, len(tables), valuesChecked, len(retiredQueueEventValues))
}

// queueService — сервис, чей прод-код считать производителем значений очереди.
//
// # Почему НЕ по схеме
//
// Схема называет сервис только там, где её так назвали: `kaname` → `iam`.
// Журнал службы вычислений живёт в `public`, и вывод по схеме дал бы «сервис
// public», которого в дереве нет, — то есть гейт сообщал бы об отказе ЧТЕНИЯ
// там, где на самом деле не сработала догадка. Координата проводки — факт: она
// говорит, чей композиционный корень эту очередь поднимает.
//
// Схема остаётся запасным путём для очередей, которых не поднимает ни одна
// проводка: она хуже, но лучше, чем ничего, и её отказ виден вызывающему.
func queueService(wiring outboxInventory, table string) string {
	if coords, ok := wiring.drained[table]; ok {
		if svc, err := exemptQueueService(coords); err == nil {
			return svc
		}
	}
	if coords, ok := wiring.observed[table]; ok {
		if svc, err := exemptQueueService(coords); err == nil {
			return svc
		}
	}
	return serviceOfSchema(table)
}

// producerCorpus — где искать производителя значения.
//
// Корпус ШИРЕ каталога сервиса на общую библиотеку, и это не послабление, а
// исправление предпосылки. Значение, которое пишет ОБЩИЙ механизм доставки
// (`pkg/audit` помечает строку доставленной), производится продуктом ровно так
// же, как значение, написанное сервисом; корпус, ограниченный каталогом
// сервиса, объявлял бы такое значение мёртвым и толкал бы дублировать общий
// оператор в каждую службу.
//
// Цена названа: значение, которое пишет общий пакет, НЕ поднятый этой службой,
// тоже сойдёт за живое. Она ограничена тем, что запись в переписи вообще
// появляется только у очереди, которую служба поднимает своей проводкой, — то
// есть общий механизм у неё провязан by construction. Способность гейта упасть
// сохраняется: у МЁРТВОГО значения нет литерала нигде в дереве.
func producerCorpus(root, svc string) ([]string, error) {
	files, err := goFilesOfService(root, svc)
	if err != nil {
		return nil, err
	}
	shared, err := goFilesOfService(root, "")
	if err != nil {
		return nil, err
	}
	return append(files, shared...), nil
}

// serviceOfSchema — «kacho_<сервис>.<таблица>» → «<сервис>».
func serviceOfSchema(qualified string) string {
	schema, _, ok := strings.Cut(qualified, ".")
	if !ok {
		return ""
	}
	return strings.TrimPrefix(schema, "kacho_")
}

// goFilesOfService — не-тестовые `.go` сервиса, взятые из ИНДЕКСА git.
//
// Не обход диска: правила игнорирования действуют на любой глубине, и под
// `services/` на машине, где поднимали стенд, лежат распаковки чартов и отчёты
// прогонов. Обойдя их, гейт нашёл бы «производителя» в чужом файле — то есть
// признал бы живым значение, которого продукт не пишет, и вердикт стал бы
// свойством рабочего каталога, а не коммита.
//
// Пустой состав [treecorpus.Under] отдаёт ОТКАЗОМ, а не пустым успехом, — здесь
// это то, что нужно: вызывающий обязан остановиться, а не печатать «ноль
// находок» на «ноль прочитанного».
func goFilesOfService(root, svc string) ([]string, error) {
	dir := filepath.Join(root, "services", svc)
	if svc == "" {
		dir = filepath.Join(root, "pkg")
	}
	all, err := treecorpus.UnderWithSuffix(dir, ".go")
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(all))
	for _, p := range all {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

// stringLiteralsIn — множество строковых литералов перечисленных файлов и число
// прочитанных. Разбор, а не текст: комментарий производителем не является.
func stringLiteralsIn(paths []string) (map[string]bool, int) {
	lits := map[string]bool{}
	files := 0
	for _, p := range paths {
		f, e := parser.ParseFile(token.NewFileSet(), p, nil, 0)
		if e != nil {
			continue
		}
		files++
		ast.Inspect(f, func(n ast.Node) bool {
			if bl, ok := n.(*ast.BasicLit); ok && bl.Kind == token.STRING {
				if v, err := strconv.Unquote(bl.Value); err == nil {
					lits[v] = true
				}
			}
			return true
		})
	}
	return lits, files
}

// producers2coord — координата первого производителя, чтобы отказ называл место,
// а не только значение. Состав файлов — тот же, что у [stringLiteralsIn]: два
// разных состава дали бы отказ, называющий координату из другого дерева.
func producers2coord(value string, paths []string) string {
	coord := "<не найдено>"
	for _, p := range paths {
		fset := token.NewFileSet()
		f, e := parser.ParseFile(fset, p, nil, 0)
		if e != nil {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			bl, ok := n.(*ast.BasicLit)
			if !ok || bl.Kind != token.STRING || coord != "<не найдено>" {
				return true
			}
			if v, err := strconv.Unquote(bl.Value); err == nil && v == value {
				coord = fset.Position(bl.Pos()).String()
			}
			return true
		})
		if coord != "<не найдено>" {
			return coord
		}
	}
	return coord
}
