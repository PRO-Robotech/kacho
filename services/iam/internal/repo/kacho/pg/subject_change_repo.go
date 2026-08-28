// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// subject_change_repo.go — pgxpool adapter implementing service.SubjectChangeReader.
// Drains kacho_iam.subject_change_outbox for the InternalIAMService.PollSubjectChanges
// use-case. Read-only; no mutation.
//
// # Позиция ДОЕЗЖАЕТ по границе устоявшегося, а не по голому номеру (kacho#1374)
//
// Номер строки выдаёт счётчик на ВСТАВКЕ (`nextval` в умолчании колонки), а строка
// становится видимой на ФИКСАЦИИ. Выдачу здесь ничто не сериализует — в отличие от
// `kacho_iam.limits`, где ревизию штампует триггер под блокировкой, держащейся до
// коммита, — поэтому порядок номеров и порядок фиксаций НЕЗАВИСИМЫ.
//
// Потребитель (край, `gateway/internal/watcher`) хранит курсор между проходами и
// двигает его на отданную позицию. Отдай он позицию за номером писателя в полёте —
// та строка не вернётся НИКОГДА: перечитывание идёт строго «больше курсора»,
// пропуска в нумерации потребитель не видит, и сходиться тут нечему. Предмет
// журнала — оповещение о том, что вердикты по субъекту пора считать заново, значит
// цена потери — отзыв доступа, который не доедет до следующего события по тому же
// субъекту, а его может не быть.
//
// Гарантия и её ЦЕНА объявлены один раз —
// `docs/architecture/journal-position-settled-watermark.md`; наблюдатель в дереве
// один и лежит в фундаменте ([subscription.Watermark]), поэтому здесь он берётся,
// а не пишется заново.
package pg

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/kacho/pkg/subscription"

	"github.com/PRO-Robotech/kacho/services/iam/internal/service"
)

// Имена журнала — КОНСТАНТЫ этого пакета, а не строки, пришедшие снаружи:
// наблюдение подставляет их в текст запроса и не экранирует.
const (
	subjectChangeTable    = "kacho_iam.subject_change_outbox"
	subjectChangePosition = "id"
)

// SubjectChangeRepo — pgxpool adapter for service.SubjectChangeReader.
type SubjectChangeRepo struct {
	pool *pgxpool.Pool

	// settled — наблюдатель границы устоявшегося. ОДИН на процесс: граница есть
	// свойство ЖУРНАЛА, а не читателя, поэтому пять параллельных проходов не
	// заводят пяти наблюдений, а складываются в одно — и подтверждают его чаще.
	settled *subscription.Watermark
}

// NewSubjectChangeRepo constructs a SubjectChangeRepo backed by pool.
func NewSubjectChangeRepo(pool *pgxpool.Pool) *SubjectChangeRepo {
	return NewSubjectChangeRepoWithLogger(pool, slog.Default())
}

// NewSubjectChangeRepoWithLogger — та же сборка с названным журналом.
//
// Читатель у журнала ЕСТЬ: удержание границы незавершившимся писателем уходит
// предупреждением, иначе «край не сбрасывает кэш, потому что кто-то не
// коммитится» было бы неотличимо от «событий нет».
func NewSubjectChangeRepoWithLogger(pool *pgxpool.Pool, log *slog.Logger) *SubjectChangeRepo {
	return &SubjectChangeRepo{
		pool:    pool,
		settled: subscription.NewWatermark(subjectChangeTable, subjectChangePosition, log),
	}
}

// Compile-time guard: SubjectChangeRepo must implement service.SubjectChangeReader.
var _ service.SubjectChangeReader = (*SubjectChangeRepo)(nil)

// PollSubjectChanges returns rows of the window `(sinceID, settled]` ordered
// ascending, at most limit rows, plus headID = the settled boundary.
//
// # headID — ГРАНИЦА УСТОЯВШЕГОСЯ, а не MAX(id)
//
// Прежде отдавался наибольший ВИДИМЫЙ номер, и потребитель, принимающий его за
// позицию, перепрыгивал через номер писателя в полёте. Теперь отдаётся то, за что
// прыгать безопасно by construction: каждый номер ≤ headID либо уже видим, либо не
// появится никогда.
//
// Ноль означает «ещё не устоялось» и приходит в двух случаях: журнал пуст либо
// наблюдение холодное и его первый проход лишь ЗАПОМНИЛ писателей. Потребитель,
// который на первом проходе усваивает headID вместо истории, во втором случае
// начнёт с нуля и вычитает хвост журнала за несколько проходов — исход ХУЖЕ
// желаемого, но по-прежнему верный, тогда как выдача MAX(id) была неверна.
func (r *SubjectChangeRepo) PollSubjectChanges(ctx context.Context, sinceID int64, limit int32) ([]service.SubjectChange, int64, error) {
	// Наблюдение идёт ДО чтения: окно обязано быть закрыто сверху уже к моменту
	// запроса строк, иначе в него попадёт номер, за которым появится меньший.
	if err := r.settled.Advance(ctx, r.pool); err != nil {
		return nil, 0, fmt.Errorf("settled watermark of subject_change_outbox: %w", err)
	}
	settled := r.settled.Settled()
	if settled <= sinceID {
		// Нечего отдавать, и позиция НЕ двигается: назвать здесь границу значило
		// бы откатить курсор потребителя назад, когда он ушёл вперёд нас.
		return nil, sinceID, nil
	}

	rows, err := r.pool.Query(ctx,
		`SELECT id, subject_id, op
		   FROM kacho_iam.subject_change_outbox
		  WHERE id > $1 AND id <= $2
		  ORDER BY id ASC
		  LIMIT $3`,
		sinceID, settled, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("poll subject_change_outbox: %w", err)
	}
	defer rows.Close()

	var changes []service.SubjectChange
	for rows.Next() {
		var c service.SubjectChange
		if err := rows.Scan(&c.ID, &c.SubjectID, &c.Op); err != nil {
			return nil, 0, fmt.Errorf("scan subject_change: %w", err)
		}
		changes = append(changes, c)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate subject_change: %w", err)
	}

	// Позиция УРЕЗАЕТСЯ до последней строки ПОЛНОЙ страницы.
	//
	// Неполная страница означает, что окно `(sinceID, settled]` вычитано целиком:
	// отдать границу безопасно. Полная — что чтение упёрлось в предел, и за
	// последней строкой в том же окне остались НЕПРОЧИТАННЫЕ. Назвать тогда
	// границу значило бы перепрыгнуть через них — вторая полоса той же потери,
	// которую закрывает первая: прежде отдавался `MAX(id)`, и страница из 256
	// строк уносила курсор за весь непрочитанный хвост.
	headID := settled
	if limit > 0 && len(changes) >= int(limit) {
		headID = changes[len(changes)-1].ID
	}

	return changes, headID, nil
}
