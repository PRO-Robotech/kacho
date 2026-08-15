// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/kacho/services/compute/internal/ports"
)

// Отказ учёта числа ресурсов на пути из хранилища наружу.
//
// # Что здесь больше НЕ живёт
//
// Прежде в этом файле лежали `chargeProjectQuota` / `refundProjectQuota` —
// списание и возврат, которые вызывающий обязан был расставить руками. Их нет:
// учёт ведёт триггер, стоящий на самой таблице ресурса (миграция 0036), поэтому
// он верен для КАЖДОГО писателя, а не только для тех двух путей, где вызов не
// забыли. Файл остался, потому что осталась вторая половина работы — перевод
// отказа хранилища в понятный вызывающему; она к триггеру не переезжает, ей
// место здесь.

// ErrQuotaExceeded — место кончилось: потолок назван и выбран.
// Администратору требуется ПОДНЯТЬ предел.
var ErrQuotaExceeded = ports.ErrQuotaExceeded

// ErrQuotaNotProvisioned — потолок не назван ни на одной области.
// Администратору требуется ЗАВЕСТИ предел.
//
// Отдельный признак, а не оттенок предыдущего: сведи их в один, и читающий
// «место кончилось» пойдёт искать, что понизить, там, где ничего не назначено.
// Это же различие запрещает трактовать отсутствие потолка как «без предела» —
// трактовку, которая в этом самом сервисе и жила, и была измерена как механизм,
// не отказавший ни разу за всю свою жизнь.
var ErrQuotaNotProvisioned = ports.ErrQuotaNotProvisioned

// SQLSTATE'ы единственного производителя отказа (`kacho_quota_refuse`, 0036).
//
// Классы намеренно РАЗНЫЕ: один просит поднять предел, другой — завести его.
// Свести их к одному коду значило бы отправить администратора искать, что
// понизить, там, где ничего не назначено.
const (
	sqlStateQuotaExceeded       = "KQ001"
	sqlStateQuotaNotProvisioned = "KQ002"
	sqlStateQuotaNoProject      = "KQ003"
)

// classifyQuotaErr переводит отказ учёта в его sentinel, сохраняя текст
// производителя ДОСЛОВНО.
//
// Текст выносится наружу как есть, потому что он и есть контракт: называет
// носителя, предел и вид. Пересказать его здесь значило бы завести второе место
// об одном предмете — а именно от этого обе полосы отказа и защищены тем, что
// производитель у них один.
//
// Не отказ учёта — возвращается неизменным: классификация чужих отказов не наше
// дело, и корзины «прочее» у неё нет.
func classifyQuotaErr(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch pgErr.Code {
	case sqlStateQuotaExceeded:
		return fmt.Errorf("%w: %s", ErrQuotaExceeded, pgErr.Message)
	case sqlStateQuotaNotProvisioned:
		return fmt.Errorf("%w: %s", ErrQuotaNotProvisioned, pgErr.Message)
	case sqlStateQuotaNoProject:
		// Строка ресурса без проекта — дефект схемы или пути записи, а не ответ
		// арендатору. Наружу он идёт фиксированным внутренним отказом: называть
		// вызывающему имя таблицы значило бы отдать ему устройство хранилища.
		return ports.ErrInternal
	default:
		return err
	}
}

// QuotaCarrierProject / QuotaRow — ре-экспорт из leaf-пакета `ports`: их
// называют три слоя сразу, поэтому объявлены они там, а не здесь.
const QuotaCarrierProject = ports.QuotaCarrierProject

// QuotaRow — строка учёта в форме, пригодной для материализации.
type QuotaRow = ports.QuotaRow

// QuotaRepo — учёт числа ресурсов у владельца.
type QuotaRepo struct {
	pool *pgxpool.Pool
}

// NewQuotaRepo создаёт QuotaRepo.
func NewQuotaRepo(pool *pgxpool.Pool) *QuotaRepo { return &QuotaRepo{pool: pool} }

// Admit — СОВЕЩАТЕЛЬНАЯ полоса: говорит о наличии места, не занимая его.
//
// Никогда не решение. Решение принимает условный `UPDATE` триггера в writer-TX
// вставки; эта полоса существует затем, чтобы арендатор получил отказ рано и в
// ТЕХ ЖЕ словах — производитель отказа у обеих полос один (`kacho_quota_refuse`),
// поэтому текст и признак разойтись не могут.
//
// Трактовать её ответ как разрешение нельзя: между ней и вставкой помещается
// чужая запись, и два создателя увидели бы одно и то же свободное место.
func (r *QuotaRepo) Admit(ctx context.Context, carrierType, carrierID, kind string) error {
	const stmt = `SELECT kacho_quota_admit($1, $2, $3)`
	if _, err := r.pool.Exec(ctx, stmt, carrierType, carrierID, kind); err != nil {
		return wrapPgErr(err, "Quota", "")
	}
	return nil
}

// Materialize заводит строки учёта по всем видам разом.
//
// `ON CONFLICT DO NOTHING`: материализация идемпотентна и НИКОГДА не трогает
// потребление уже существующей строки. Заводить строку с ненулевым `used` —
// обход предиката потолка с другой стороны, и гейт дерева его ловит.
//
// По ВСЕМ видам домена разом, а не по тому, куда пришла первая запись: иначе
// новый вид не появился бы у уже живущих проектов и потребовал бы беклога.
func (r *QuotaRepo) Materialize(ctx context.Context, rows []ports.QuotaRow) (int64, error) {
	return MaterializeQuotas(ctx, r.pool, rows)
}

// QuotaExecutor — то, чем исполняется заведение строк учёта: пул, транзакция или
// одиночное соединение. Узкий интерфейс нужен, чтобы фикстура проб пользовалась
// ТЕМ ЖЕ оператором, что живой путь, а не своей копией.
type QuotaExecutor interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// MaterializeQuotas — ЕДИНСТВЕННЫЙ оператор заведения строк учёта.
//
// Единственный намеренно: копия этого INSERT'а разошлась бы с настоящим молча —
// и разошлась бы именно там, где расхождение не видно, на составе столбцов.
func MaterializeQuotas(ctx context.Context, ex QuotaExecutor, rows []ports.QuotaRow) (int64, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	const stmt = `
		INSERT INTO project_resource_quotas
		    (carrier_type, carrier_id, kind, used, limit_value,
		     source_scope, source_scope_id, limit_revision, account_id)
		SELECT * FROM unnest(
		    $1::text[], $2::text[], $3::text[], $4::bigint[], $5::bigint[],
		    $6::text[], $7::text[], $8::bigint[], $9::text[])
		ON CONFLICT (carrier_type, carrier_id, kind) DO NOTHING`

	n := len(rows)
	carrierTypes := make([]string, n)
	carrierIDs := make([]string, n)
	kinds := make([]string, n)
	used := make([]int64, n)
	limits := make([]int64, n)
	scopes := make([]string, n)
	scopeIDs := make([]string, n)
	revisions := make([]int64, n)
	accounts := make([]string, n)
	for i, row := range rows {
		carrierTypes[i] = row.CarrierType
		carrierIDs[i] = row.CarrierID
		kinds[i] = row.Kind
		used[i] = 0 // новая строка учёта не несёт потребления: его создаёт триггер
		limits[i] = row.Limit
		scopes[i] = row.SourceScope
		scopeIDs[i] = row.SourceScopeID
		revisions[i] = row.LimitRevision
		accounts[i] = row.AccountID
	}

	tag, err := ex.Exec(ctx, stmt,
		carrierTypes, carrierIDs, kinds, used, limits, scopes, scopeIDs, revisions, accounts)
	if err != nil {
		return 0, wrapPgErr(err, "Quota", "")
	}
	return tag.RowsAffected(), nil
}
