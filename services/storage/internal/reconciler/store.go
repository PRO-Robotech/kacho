// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package reconciler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/kacho/services/storage/internal/blockbackend"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
)

// Kind — вид ресурса, который сверяется. Строковый тип, а не имя таблицы в
// параметре: имя таблицы, приходящее строкой, рано или поздно приходит из запроса.
type Kind string

// Виды сверяемых ресурсов.
const (
	KindVolume   Kind = "volume"
	KindSnapshot Kind = "snapshot"
	KindImage    Kind = "image"
)

// table возвращает таблицу вида. ЗАКРЫТОЕ отображение: неизвестный вид не
// подставляется в запрос, а роняет вызов — имя таблицы никогда не собирается из
// значения, пришедшего снаружи.
func (k Kind) table() (string, error) {
	switch k {
	case KindVolume:
		return "volumes", nil
	case KindSnapshot:
		return "snapshots", nil
	case KindImage:
		return "images", nil
	default:
		return "", fmt.Errorf("reconciler: unknown resource kind %q", k)
	}
}

// AllKinds — все сверяемые виды. Перечень един для петли и для проб: обход,
// пропустивший вид, был бы неотличим от обхода, у которого по этому виду всё в
// порядке.
func AllKinds() []Kind { return []Kind{KindVolume, KindSnapshot, KindImage} }

// Row — строка, взятая на разбор, вместе с координатой её объекта у бэкенда.
type Row struct {
	Kind      Kind
	ID        string
	ProjectID string
	Desired   domain.VolumeStatus
	SizeBytes int64

	BindingID string
	BackendID string
	Ref       blockbackend.ObjectRef

	// SourceObject — имя объекта-источника, если ресурс засевается из него.
	// Пусто для чистого создания.
	SourceObject string
	// SourceIsSnapshot — источник сам является снимком, то есть ресурс создаётся
	// КОПИЕЙ, а не снятием. Различие определяет глагол: копия снимается с другого
	// снимка, снятие — с тома, и перепутать их значит звать операцию, у которой
	// нет предмета.
	SourceIsSnapshot bool
	// SourceIsCopy — ресурс создаётся КОПИЕЙ с однородного источника в другом
	// локаторе (образ с образа). Клонирование здесь неприменимо: оно предполагает
	// общий локатор с родителем.
	SourceIsCopy bool
}

// Store — доступ сверщика к нашим строкам.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore собирает хранилище сверщика.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// driftedSQL — строки, у которых намерение разошлось с наблюдаемым.
//
// Предикат тот же, что у частичного индекса расхождения (миграция 0017), — иначе
// обход перестал бы попадать в индекс МОЛЧА, и деградация нашлась бы только под
// нагрузкой. Порядок по времени правки: старшее расхождение разбирается раньше.
//
// Привязка и бэкенд добираются тем же запросом: без координаты объект не адресовать,
// а второй запрос на строку — это N+1, заметный ровно тогда, когда чинить дорого.
const driftedSQL = `
SELECT r.id, r.project_id, r.state, %s,
       COALESCE(r.binding_id, ''), COALESCE(b.backend_id, ''),
       COALESCE(b.pool, ''), COALESCE(r.backend_namespace, ''), COALESCE(r.backend_object, '')
  FROM %s r
  LEFT JOIN disk_type_bindings b ON b.id = r.binding_id
 WHERE r.state <> r.observed_state
 ORDER BY %s ASC
 LIMIT $1`

// Drifted читает партию расходящихся строк одного вида.
func (s *Store) Drifted(ctx context.Context, kind Kind, limit int) ([]Row, error) {
	table, err := kind.table()
	if err != nil {
		return nil, err
	}
	// У снимка и образа нет собственного пространства арендатора и размера в том
	// смысле, в каком они есть у тома: снимок наследует пространство источника, а
	// его размер задаётся при снятии. Различие выражено выбором колонок, а не
	// ветвлением в коде разбора.
	sizeCol, orderCol := "r.size_bytes", "r.updated_at"
	if kind == KindSnapshot {
		orderCol = "r.created_at"
	}
	q := fmt.Sprintf(driftedSQL, sizeCol, table, orderCol)

	rows, err := s.pool.Query(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Row
	for rows.Next() {
		var (
			r     Row
			state string
		)
		r.Kind = kind
		if serr := rows.Scan(&r.ID, &r.ProjectID, &state, &r.SizeBytes,
			&r.BindingID, &r.BackendID, &r.Ref.Pool, &r.Ref.Namespace, &r.Ref.Name); serr != nil {
			return nil, serr
		}
		r.Desired = domain.DeriveStatus(state, false)
		out = append(out, r)
	}
	return out, rows.Err()
}

// Confirm объявляет ресурс существующим: намерение исполнено, объект виден.
//
// Обновление условное — по НАБЛЮДАЕМОМУ значению, из которого мы исходили. Между
// чтением партии и записью исхода строку мог тронуть другой путь (арендатор удалил
// том), и безусловная запись вернула бы его к жизни.
func (s *Store) Confirm(ctx context.Context, kind Kind, id string, obs blockbackend.Observed) error {
	table, err := kind.table()
	if err != nil {
		return err
	}
	q := fmt.Sprintf(`
		UPDATE %s
		   SET state = 'READY', observed_state = 'READY', observed_at = now(),
		       status_reason = ''%s
		 WHERE id = $1 AND state IN ('CREATING','ERROR')`, table, observedSizeSet(kind))
	args := []any{id}
	if kind == KindVolume {
		args = append(args, obs.SizeBytes, usedOrNil(obs))
	}
	_, err = s.pool.Exec(ctx, q, args...)
	return err
}

// observedSizeSet — присваивание наблюдённых величин там, где они есть. У тома
// хранится и размер, и потребление; у снимка с образом — только факт наличия.
func observedSizeSet(kind Kind) string {
	if kind == KindVolume {
		return ", observed_size_bytes = $2, used_bytes = $3"
	}
	return ""
}

// usedOrNil отдаёт потребление ТОЛЬКО когда бэкенд его сообщил.
//
// Ноль на этом месте был бы утверждением о пустом томе. «Не сказали» и «пусто» —
// разные факты, и колонка обязана уметь их различать.
func usedOrNil(obs blockbackend.Observed) any {
	if !obs.HasUsedBytes {
		return nil
	}
	return obs.UsedBytes
}

// Observe записывает наблюдение, не меняя намерения. Нужно там, где действие
// откладывается: состояние не установлено либо отказ временный.
func (s *Store) Observe(ctx context.Context, kind Kind, id string, obs blockbackend.Observed) error {
	table, err := kind.table()
	if err != nil {
		return err
	}
	q := fmt.Sprintf(`UPDATE %s SET observed_state = $2, observed_at = now() WHERE id = $1`, table)
	_, err = s.pool.Exec(ctx, q, id, observedName(obs.State))
	return err
}

// MarkError объявляет ресурс ошибочным с НАЗВАННОЙ причиной из закрытого словаря.
func (s *Store) MarkError(ctx context.Context, kind Kind, id string, reason domain.StatusReason, obs blockbackend.Observed) error {
	table, err := kind.table()
	if err != nil {
		return err
	}
	if !reason.Valid() {
		return fmt.Errorf("reconciler: status reason %q is not in the closed vocabulary", reason)
	}
	q := fmt.Sprintf(`
		UPDATE %s
		   SET state = 'ERROR', observed_state = $2, observed_at = now(), status_reason = $3
		 WHERE id = $1 AND state <> 'DELETING'`, table)
	_, err = s.pool.Exec(ctx, q, id, observedName(obs.State), string(reason))
	return err
}

// Forget снимает строку: объект у бэкенда уже снят, держать запись больше не о чем.
//
// Условие по намерению обязательно: строка, которую арендатор успел вернуть к жизни
// между наблюдением и записью, удалена быть не должна.
func (s *Store) Forget(ctx context.Context, kind Kind, id string) error {
	table, err := kind.table()
	if err != nil {
		return err
	}
	q := fmt.Sprintf(`DELETE FROM %s WHERE id = $1 AND state = 'DELETING'`, table)
	_, err = s.pool.Exec(ctx, q, id)
	return err
}

// KnownObjects отдаёт имена объектов, которые НАШИ строки считают своими в
// названном локаторе. Нужно второй оси сверки: всё, что есть у бэкенда и чего нет
// здесь, — утечка ёмкости.
func (s *Store) KnownObjects(ctx context.Context, loc blockbackend.Locator) (map[string]struct{}, error) {
	out := map[string]struct{}{}
	for _, kind := range AllKinds() {
		table, err := kind.table()
		if err != nil {
			return nil, err
		}
		q := fmt.Sprintf(`
			SELECT r.backend_object FROM %s r
			  JOIN disk_type_bindings b ON b.id = r.binding_id
			 WHERE r.backend_object IS NOT NULL AND b.pool = $1 AND r.backend_namespace = $2`, table)
		rows, err := s.pool.Query(ctx, q, loc.Pool, loc.Namespace)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var name string
			if serr := rows.Scan(&name); serr != nil {
				rows.Close()
				return nil, serr
			}
			out[name] = struct{}{}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// observedName переводит наблюдённое состояние в значение колонки. Значения
// совпадают с ограничением схемы намеренно: разъехавшись, они дали бы отказ записи
// там, где логика верна.
func observedName(s blockbackend.ObservedState) string { return s.String() }

// Binding — координата бэкенда, к которому привязан ресурс.
type Binding struct {
	BackendID      string
	Kind           string
	Endpoint       string
	CredentialsRef string
	Pool           string
}

// Binding читает ревизию привязки вместе с её бэкендом.
//
// Строка ревизии НЕИЗМЕНЯЕМА, поэтому её можно кэшировать без риска устареть — это
// прямое следствие append-only и одна из причин, по которым он выбран.
func (s *Store) Binding(ctx context.Context, bindingID string) (Binding, error) {
	var b Binding
	err := s.pool.QueryRow(ctx, `
		SELECT sb.id, sb.kind, sb.endpoint, sb.credentials_ref, dtb.pool
		  FROM disk_type_bindings dtb
		  JOIN storage_backends sb ON sb.id = dtb.backend_id
		 WHERE dtb.id = $1`, bindingID).
		Scan(&b.BackendID, &b.Kind, &b.Endpoint, &b.CredentialsRef, &b.Pool)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Binding{}, fmt.Errorf("reconciler: binding %s not found", bindingID)
		}
		return Binding{}, err
	}
	return b, nil
}

// StaleBefore — граница, за которой наблюдение считается устаревшим и подлежит
// обновлению даже без расхождения. Без неё пропажа объекта под ГОТОВОЙ строкой не
// нашлась бы никогда: расхождения нет, значит частичный индекс её не покажет.
func StaleBefore(now time.Time, ttl time.Duration) time.Time { return now.Add(-ttl) }

// errNoSource — снимок без объекта-источника неисполним. Отдельная ошибка, а не
// общий отказ: это дефект записи, а не состояние бэкенда, и лечится он не повтором.
var errNoSource = errors.New("reconciler: snapshot row has no source object")

// SourceObject читает имя объекта-источника для строки, которую предстоит создать.
//
// Отдельный запрос на строку, а не соединение в основном обходе, — осознанно.
// Источник нужен ТОЛЬКО на пути создания, то есть на малой доле строк, и он
// ограничен размером партии; пятистороннее соединение платилось бы на КАЖДОМ
// проходе, включая те, где создавать нечего.
func (s *Store) SourceObject(ctx context.Context, kind Kind, id string) (string, error) {
	var q string
	switch kind {
	case KindVolume:
		q = `SELECT COALESCE(sn.backend_object, im.backend_object, '')
		       FROM volumes v
		       LEFT JOIN snapshots sn ON sn.id = v.source_snapshot_id
		       LEFT JOIN images    im ON im.id = v.source_image_id
		      WHERE v.id = $1`
	case KindSnapshot:
		// Источником снимка бывает ЛИБО том (снятие), ЛИБО другой снимок (копия).
		// Различие несущее: у копии тома нет вовсе, и снятие с него отказало бы.
		q = `SELECT COALESCE(v.backend_object, sn.backend_object, '')
		       FROM snapshots s
		       LEFT JOIN volumes   v  ON v.id  = s.source_volume_id
		       LEFT JOIN snapshots sn ON sn.id = s.source_snapshot_id
		      WHERE s.id = $1`
	case KindImage:
		// Образ бывает снят с тома либо снимка — и бывает СКОПИРОВАН с другого
		// образа. Копия лежит в чужом локаторе (перенос между регионами и есть её
		// смысл), поэтому глагол у неё другой.
		q = `SELECT COALESCE(im.backend_object, sn.backend_object, v.backend_object, '')
		       FROM images i
		       LEFT JOIN images    im ON im.id = i.source_image_id
		       LEFT JOIN snapshots sn ON sn.id = i.source_snapshot_id
		       LEFT JOIN volumes   v  ON v.id  = i.source_volume_id
		      WHERE i.id = $1`
	default:
		return "", fmt.Errorf("reconciler: unknown resource kind %q", kind)
	}
	var name string
	if err := s.pool.QueryRow(ctx, q, id).Scan(&name); err != nil {
		if err == pgx.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	return name, nil
}

// SourceIsSnapshot отвечает, снимается ли строка с ДРУГОГО СНИМКА (копия) либо с
// тома (снятие). Различие определяет глагол материализации.
func (s *Store) SourceIsSnapshot(ctx context.Context, id string) (bool, error) {
	var fromSnapshot bool
	err := s.pool.QueryRow(ctx,
		`SELECT source_snapshot_id IS NOT NULL FROM snapshots WHERE id = $1`, id).Scan(&fromSnapshot)
	return fromSnapshot, wrapNoRows(err)
}

// SourceIsImageCopy отвечает, скопирован ли образ с другого образа. Копия переносится
// между локаторами, снятие — берёт источник в своём: глагол у них разный.
func (s *Store) SourceIsImageCopy(ctx context.Context, id string) (bool, error) {
	var fromImage bool
	err := s.pool.QueryRow(ctx,
		`SELECT source_image_id IS NOT NULL FROM images WHERE id = $1`, id).Scan(&fromImage)
	return fromImage, wrapNoRows(err)
}

// wrapNoRows: строки нет — значит и источника нет; это не ошибка чтения.
func wrapNoRows(err error) error {
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	return nil
}
