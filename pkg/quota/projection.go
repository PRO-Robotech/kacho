// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package quota

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Проекция величин поверх таблицы учёта владельца.
//
// Таблица у всех владельцев одна и та же с точностью до имени схемы, поэтому и
// операторы здесь одни и те же. Владелец передаёт имя схемы — и получает
// работающий синхронизатор, не написав ни одного своего оператора.

// Execer — то единственное, что нужно проекции от базы: два глагола.
// Транзакция, соединение из пула — подходит любое.
//
// Формы драйвера взяты как есть, без своих обёрток: обёртка добавила бы место,
// где адаптер может оказаться СНИСХОДИТЕЛЬНЕЕ настоящего носителя, а проверяемость
// от неё не зависит — `pgconn.CommandTag` есть структура, `pgx.Row` есть
// интерфейс, и подставной носитель выражается обоими без единой строки перехода.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// PgProjection — проекция поверх таблицы `project_resource_quotas` названной
// схемы.
type PgProjection struct {
	db     Execer
	schema string
}

// NewPgProjection собирает проекцию. Имя схемы обязательно и не имеет
// умолчания: умолчание здесь означало бы, что владелец, забывший его передать,
// молча правит чужую таблицу.
func NewPgProjection(db Execer, schema string) (*PgProjection, error) {
	if db == nil {
		return nil, errors.New("quota projection: db is required")
	}
	if !validSchemaName(schema) {
		return nil, fmt.Errorf("quota projection: schema %q is not a plain identifier", schema)
	}
	return &PgProjection{db: db, schema: schema}, nil
}

// validSchemaName — имя схемы попадает в оператор подстановкой, поэтому оно
// обязано быть простым идентификатором. Проверка стоит здесь, а не у
// вызывающего: у вызывающего она была бы пятой копией, а пропустивший её
// владелец собрал бы оператор с чужим содержимым.
func validSchemaName(s string) bool {
	if s == "" || len(s) > 63 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c == '_':
		case c >= '0' && c <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

// binder собирает список аргументов и выдаёт занятые ими номера.
//
// Существует ради того, чтобы номер placeholder'а нельзя было разойтись с
// позицией аргумента: оба берутся из одного действия. Руками расставленные
// номера расходятся ровно тогда, когда в один из двух похожих операторов
// добавляют условие, — и расхождение проявляется не ошибкой разбора, а чужим
// значением в чужом столбце.
type binder struct{ args []any }

func (b *binder) bind(v any) string {
	b.args = append(b.args, v)
	return "$" + strconv.Itoa(len(b.args))
}

// LoadCursor читает курсор дельты.
func (p *PgProjection) LoadCursor(ctx context.Context) (string, error) {
	stmt := fmt.Sprintf(
		`SELECT cursor FROM %s.quota_sync_cursor WHERE id = 'limits'`, p.schema)
	var cursor string
	if err := p.db.QueryRow(ctx, stmt).Scan(&cursor); err != nil {
		return "", fmt.Errorf("read quota sync cursor: %w", err)
	}
	return cursor, nil
}

// SaveCursor двигает курсор и прибавляет применённые строки к накопительному
// счётчику.
//
// Счётчик накопительный НАМЕРЕННО: он и есть то, чем «ни одна строка не
// синхронизирована за всё время» отличается от «сейчас нечего применять».
// Без него мёртвый синхронизатор выглядит точно так же, как живой на
// неизменной конфигурации.
func (p *PgProjection) SaveCursor(ctx context.Context, cursor string, appliedRows int64) error {
	var b binder
	stmt := fmt.Sprintf(`
		UPDATE %s.quota_sync_cursor
		   SET cursor = %s,
		       applied_rows_total = applied_rows_total + %s,
		       updated_at = now()
		 WHERE id = 'limits'`, p.schema, b.bind(cursor), b.bind(appliedRows))
	if _, err := p.db.Exec(ctx, stmt, b.args...); err != nil {
		return fmt.Errorf("save quota sync cursor: %w", err)
	}
	return nil
}

// Heartbeat отмечает состоявшийся проход, не трогая ни курсор, ни счётчик
// работы.
//
// Существует ради различимости двух картин, которые иначе выглядят одинаково:
// «синхронизатор жив, менять нечего» и «синхронизатор не работает». Первая
// двигает отметку времени, вторая — нет.
func (p *PgProjection) Heartbeat(ctx context.Context) error {
	stmt := fmt.Sprintf(`
		UPDATE %s.quota_sync_cursor
		   SET pulls_total = pulls_total + 1,
		       updated_at  = now()
		 WHERE id = 'limits'`, p.schema)
	if _, err := p.db.Exec(ctx, stmt); err != nil {
		return fmt.Errorf("quota sync heartbeat: %w", err)
	}
	return nil
}

// addressPredicate отдаёт условие «кого касается это изменение».
//
// Одно место на обе операции: применение и отзыв обязаны понимать адресацию
// одинаково, а два похожих предиката разошлись бы молча — и разошлись бы на
// аккаунтной области, которую труднее всего проверить глазом.
func addressPredicate(b *binder, scope Scope, scopeID string) string {
	switch scope {
	case ScopeProject:
		return `carrier_type = 'project' AND carrier_id = ` + b.bind(scopeID)
	case ScopeAccount:
		return `account_id = ` + b.bind(scopeID)
	default:
		// Умолчание не адресуется идентификатором: его касаются все строки,
		// живущие из умолчания, в каком бы проекте они ни стояли.
		return `source_scope = 'DEFAULT'`
	}
}

// precedencePredicate — величина применяется, только если её область не ниже
// той, из которой строка живёт сейчас.
//
// Условие стоит в самом `WHERE`, а не после чтения строки: «прочитал → сравнил
// → записал» есть check-then-act через границу оператора, и между чтением и
// записью помещается чужой проход. В предикате строка, чья область изменилась
// параллельно, просто не совпадает — и ни одна не перезаписывается менее
// специфичной величиной (ban #10).
func precedencePredicate(scope Scope) string {
	switch scope {
	case ScopeProject:
		return `TRUE`
	case ScopeAccount:
		return `source_scope IN ('ACCOUNT', 'DEFAULT')`
	default:
		return `source_scope = 'DEFAULT'`
	}
}

// ApplyChange правит строки снимка, адресуя их по столбцам строки учёта.
//
// # Почему отзыв УДАЛЯЕТ строку
//
// Отозванная величина не имеет значения, которым можно заменить снимок: верным
// становится следующий по старшинству потолок, а знает его владелец величин.
// Строка снимается, и ближайшее списание разрешит потолок заново. Потребление
// при этом не теряется — материализация затравляет строку фактом, а не нулём.
func (p *PgProjection) ApplyChange(ctx context.Context, ch Change) (int64, error) {
	if err := ch.Validate(); err != nil {
		return 0, err
	}
	if ch.Withdrawn {
		return p.withdraw(ctx, ch)
	}
	return p.apply(ctx, ch)
}

func (p *PgProjection) apply(ctx context.Context, ch Change) (int64, error) {
	var b binder
	set := strings.Join([]string{
		`limit_value     = ` + b.bind(ch.Value),
		`source_scope    = ` + b.bind(string(ch.Scope)),
		`source_scope_id = ` + b.bind(ch.ScopeID),
	}, ",\n\t\t       ")
	revision := b.bind(ch.Revision)
	kind := b.bind(ch.Kind)
	addr := addressPredicate(&b, ch.Scope, ch.ScopeID)

	stmt := fmt.Sprintf(`
		UPDATE %s.project_resource_quotas
		   SET %s,
		       limit_revision  = %s,
		       synced_at       = now(),
		       updated_at      = now()
		 WHERE kind = %s
		   AND %s
		   AND %s
		   AND limit_revision < %s`,
		p.schema, set, revision, kind, addr, precedencePredicate(ch.Scope), revision)

	tag, err := p.db.Exec(ctx, stmt, b.args...)
	if err != nil {
		return 0, fmt.Errorf("apply limit change: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (p *PgProjection) withdraw(ctx context.Context, ch Change) (int64, error) {
	var b binder
	kind := b.bind(ch.Kind)
	scope := b.bind(string(ch.Scope))
	addr := addressPredicate(&b, ch.Scope, ch.ScopeID)

	// Снимается только строка, которая ЖИВЁТ из отзываемой области. Строка, чей
	// снимок пришёл откуда-то ещё, отзыву не подлежит — иначе отзыв умолчания
	// сносил бы личные перекрытия.
	stmt := fmt.Sprintf(`
		DELETE FROM %s.project_resource_quotas
		 WHERE kind = %s
		   AND source_scope = %s
		   AND %s`, p.schema, kind, scope, addr)

	tag, err := p.db.Exec(ctx, stmt, b.args...)
	if err != nil {
		return 0, fmt.Errorf("withdraw limit: %w", err)
	}
	return tag.RowsAffected(), nil
}

var _ Projection = (*PgProjection)(nil)
