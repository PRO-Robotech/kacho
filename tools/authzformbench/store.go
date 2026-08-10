// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzformbench

import (
	"context"
	"errors"
	"fmt"
)

// Counters — две величины обращений, раздельно и НИКОГДА не в сумме.
//
// Сегодняшняя колонка «обращений» у форм A–BCD считала HTTP-вызовы движка, и за
// каждым таким вызовом стоит неизвестное число запросов к его Postgres. У формы E
// «обращение» — это SQL-стейтмент. Это разные величины: «одно обращение против
// пяти» не говорит о стоимости ничего, пока не сказано, обращений ЧЕГО. Поэтому
// колонки две, у каждой своё имя, и складывать их нельзя ни в одной ячейке.
type Counters struct {
	ReqEngine int // круговые обращения к движку по HTTP; у формы E их нет by construction
	StmtSQL   int // SQL-стейтменты, снятые на Postgres
}

// Volume — ёмкостная сторона ячейки.
//
// Смысл колонок закреплён здесь, потому что у двух из них он неочевиден и был
// унаследован от замера пяти форм, а не выбран заново:
//
//   - GrantRows   — строки ВЫДАЧИ: у движка это кортежи store'а за вычетом
//     структурных, у формы E — строки привязки, её субъектов и селектора;
//   - GrantBytes  — логические байты ВСЕХ строк хранилища формы, включая
//     структурные. Так эту величину считает база сравнения (`Stack.TupleBytes`),
//     и форма E считает её ТАК ЖЕ — иначе колонка перестала бы быть
//     сопоставимой. Чтобы асимметрия «строки без структурных, байты со
//     структурными» не читалась молча, структурная часть вынесена в соседнюю
//     пару колонок и печатается рядом;
//   - StructuralRows/Bytes — то, что есть у КАЖДОЙ формы одинаково: указатели
//     родителя у движка, строки зеркала у формы E. Приписать их форме значило бы
//     польстить самой компактной;
//   - TableBytes  — весь размер отношений с индексами. Индекс табличный, а не
//     построковый, поэтому на форму он не делится и печатается отдельно.
type Volume struct {
	GrantRows       int64
	GrantBytes      int64
	StructuralRows  int64
	StructuralBytes int64
	TableBytes      int64
}

// ErrNotApplicable — операция у этой формы не существует ПО ПОСТРОЕНИЮ.
//
// Это не отказ и не «не выполнилось»: у движка отношений нет и не может быть
// общей транзакции с БД предмета выдачи, и ячейка `T-inline-*` для него — самый
// содержательный результат таблицы, а не пропуск замера. Свернуть его в
// «не выполнилось» значило бы напечатать «не-измеренных 2» и заставить читателя
// решить, что замер не доехал.
var ErrNotApplicable = errors.New("операция отсутствует у этой формы by construction")

// ErrModelNotRequired — у формы нет модели авторизации, и это законный ответ.
//
// Конструктор пяти прежних форм требовал DSL для КАЖДОЙ формы и гнал его через
// внешний трансформ; форма E вердикт вычисляет запросом, и модели у неё нет.
// Ответ отделён от ошибки «неизвестная форма» намеренно: иначе «модель не
// требуется» покрывало бы и опечатку в имени формы (XC-10-03).
var ErrModelNotRequired = errors.New("форме не требуется модель авторизации")

// ProducerStatus — состояние ПРОИЗВОДИТЕЛЯ величины `StmtSQL` на одном месте
// снятия.
//
// Величина, у входа которой нет производителя, зеленеет молча: она печатается
// нулём, неотличимым от измеренного. Поэтому производитель называется на каждое
// место снятия и считается заведённым только после контроля в обе стороны —
// счётчик не двигается, когда никто не спрашивает, и двигается ровно на один на
// одном заведомом стейтменте.
type ProducerStatus struct {
	Place    string // где снимается
	Producer string // чем снимается
	OK       bool   // прошёл ли контроль в обе стороны
	Note     string // что именно наблюдалось — печатается и при OK, и при провале
}

func (p ProducerStatus) String() string {
	state := "контроль НЕ пройден"
	if p.OK {
		state = "контроль пройден"
	}
	return fmt.Sprintf("%s — %s — %s (%s)", p.Place, p.Producer, state, p.Note)
}

// RightsStore — граница «хранилище прав»: то единственное, о чём знает измеряющий
// код.
//
// Без этой границы шестая форма к матрице физически не подключается: конструктор
// требует модель для каждой формы, засев возвращает конкретный тип HTTP-клиента
// движка, а все семь операций зовут его методы напрямую. Граница вводится не ради
// красоты — до неё форма E не может быть даже проверена на то, отвечает ли она
// правильно.
//
// `[]Tuple` здесь объявлен ОБЩИМ СЛОВАРЁМ намерения, а не wire-формой движка:
// формы A–BCD пишут его как есть, форма E переводит его в строки на своей
// стороне. Общий словарь выбран вместо формо-нейтрального намерения потому, что
// он оставляет производителей намерения (`Grant`, `RelabelOne`, `RevokeSubject`)
// нетронутыми — а значит база сравнения остаётся базой.
type RightsStore interface {
	// Seed кладёт структурные строки и (когда grant непуст) строки выдачи.
	// Засев не входит ни в одну измеряемую величину.
	Seed(ctx context.Context, structural, grant []Tuple) (Counters, error)

	// Write и Remove — измеряемые мутации: выдача, переразметка, отзыв.
	Write(ctx context.Context, tuples []Tuple) (Counters, error)
	Remove(ctx context.Context, tuples []Tuple) (Counters, error)

	// InlineGrant пишет ПРЕДМЕТ выдачи и саму выдачу в ОДНОЙ транзакции.
	// У движка отношений возвращает ErrNotApplicable — общей транзакции с чужим
	// хранилищем не существует, и это ровно то, что операция измеряет.
	InlineGrant(ctx context.Context, data, grant []Tuple) (Counters, error)
	// InlineRevoke — то же для отзыва: отзыв, не зависящий от доставки.
	InlineRevoke(ctx context.Context, data, revoke []Tuple) (Counters, error)

	Check(ctx context.Context, subject, verb, object string) (bool, Counters, error)
	BatchCheck(ctx context.Context, subject, verb string, objects []string) ([]bool, Counters, error)
	// CheckPage разрешает целую страницу и возвращает, на сколько частей она была
	// разложена: у формы E разложение своё, и оно объявляется, а не усредняется.
	CheckPage(ctx context.Context, subject, verb string, objects []string,
		partition, parallel int) (verdicts []bool, parts int, c Counters, err error)

	Volume(ctx context.Context) (Volume, error)

	// StmtProducer называет, чем и где снимается `StmtSQL` у этого хранилища и
	// прошёл ли производитель контроль в обе стороны.
	StmtProducer() ProducerStatus

	// Place — из какого места снята каждая ячейка этого хранилища. Отчёт,
	// собранный из двух мест без этого признака, выдаёт за один прогон два.
	Place() string

	Teardown(ctx context.Context) error
}

// ── адаптер движка ────────────────────────────────────────────────────────────

// engineStore — форма A–BCD за границей. Поведение движка не меняется ни в одной
// точке: адаптер только раскладывает счётчики по двум колонкам и переводит
// отсутствующие операции в ErrNotApplicable.
type engineStore struct {
	st    *Store
	stmts *pgStmtCounter // производитель `StmtSQL` со стороны движка; nil ⇒ не заведён
	prod  ProducerStatus
}

func (e *engineStore) count(ctx context.Context, reqs int, err error) (Counters, error) {
	c := Counters{ReqEngine: reqs}
	if e.stmts != nil && e.prod.OK {
		if d, derr := e.stmts.Stop(ctx); derr == nil {
			c.StmtSQL = d
		}
	}
	return c, err
}

func (e *engineStore) start(ctx context.Context) {
	if e.stmts != nil && e.prod.OK {
		e.stmts.Start(ctx)
	}
}

func (e *engineStore) Seed(ctx context.Context, structural, grant []Tuple) (Counters, error) {
	n, err := e.st.WriteTuples(ctx, structural)
	if err != nil {
		return Counters{ReqEngine: n}, fmt.Errorf("засев структурных: %w", err)
	}
	if len(grant) == 0 {
		return Counters{ReqEngine: n}, nil
	}
	m, err := e.st.WriteTuples(ctx, grant)
	if err != nil {
		return Counters{ReqEngine: n + m}, fmt.Errorf("засев выдачи: %w", err)
	}
	return Counters{ReqEngine: n + m}, nil
}

func (e *engineStore) Write(ctx context.Context, tuples []Tuple) (Counters, error) {
	e.start(ctx)
	n, err := e.st.WriteTuples(ctx, tuples)
	return e.count(ctx, n, err)
}

func (e *engineStore) Remove(ctx context.Context, tuples []Tuple) (Counters, error) {
	e.start(ctx)
	n, err := e.st.DeleteTuples(ctx, tuples)
	return e.count(ctx, n, err)
}

// InlineGrant/InlineRevoke — предмет выдачи живёт в БД сервиса, выдача в движке;
// одной транзакции над ними не бывает. Это неприменимость по построению, а не
// неудача замера.
func (e *engineStore) InlineGrant(context.Context, []Tuple, []Tuple) (Counters, error) {
	return Counters{}, ErrNotApplicable
}

func (e *engineStore) InlineRevoke(context.Context, []Tuple, []Tuple) (Counters, error) {
	return Counters{}, ErrNotApplicable
}

func (e *engineStore) Check(ctx context.Context, subject, verb, object string) (bool, Counters, error) {
	e.start(ctx)
	ok, err := e.st.Check(ctx, subject, verb, object)
	c, err := e.count(ctx, 1, err)
	return ok, c, err
}

func (e *engineStore) BatchCheck(ctx context.Context, subject, verb string, objects []string) ([]bool, Counters, error) {
	e.start(ctx)
	res, err := e.st.BatchCheck(ctx, subject, verb, objects)
	c, err := e.count(ctx, 1, err)
	return res, c, err
}

func (e *engineStore) CheckPage(ctx context.Context, subject, verb string, objects []string,
	partition, parallel int) ([]bool, int, Counters, error) {
	e.start(ctx)
	res, parts, err := e.st.CheckPage(ctx, subject, verb, objects, partition, parallel)
	c, err := e.count(ctx, parts, err)
	return res, parts, c, err
}

func (e *engineStore) Volume(ctx context.Context) (Volume, error) {
	cnt, rowBytes, structRows, structBytes, tableBytes, err := e.st.stack.TupleBytes(ctx, e.st.ID)
	if err != nil {
		return Volume{}, err
	}
	return Volume{
		GrantRows:       cnt - structRows,
		GrantBytes:      rowBytes,
		StructuralRows:  structRows,
		StructuralBytes: structBytes,
		TableBytes:      tableBytes,
	}, nil
}

func (e *engineStore) StmtProducer() ProducerStatus { return e.prod }

func (e *engineStore) Place() string {
	return "tools/authzformbench · store движка (схема OpenFGA)"
}

func (e *engineStore) Teardown(ctx context.Context) error { return e.st.Delete(ctx) }
