// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	corequota "github.com/PRO-Robotech/kacho/pkg/quota"
	"github.com/PRO-Robotech/kacho/pkg/quota/quotadetail"
	"github.com/PRO-Robotech/kacho/pkg/quota/quotaread"
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
	// Величины производителя приклеиваются ЗДЕСЬ — там, где `*pgconn.PgError` ещё
	// не потерян. Дальше по пути его нет, и прочитать `DETAIL` больше негде:
	// текст переживает переход, величины — нет. Разбор
	// общий (`pkg/quota/quotadetail`) по тому же доводу, по которому производитель
	// один: шесть копий разошлись бы молча.
	switch pgErr.Code {
	case sqlStateQuotaExceeded:
		return quotadetail.Attach(
			fmt.Errorf("%w: %s", ErrQuotaExceeded, pgErr.Message), pgErr.Detail)
	case sqlStateQuotaNotProvisioned:
		return quotadetail.Attach(
			fmt.Errorf("%w: %s", ErrQuotaNotProvisioned, pgErr.Message), pgErr.Detail)
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
	// `used` НЕ приходит из Go и не может: единственный, кто знает потребление, —
	// сама база. Прежняя редакция ставила здесь ноль, объясняя это тем, что
	// потребление создаёт триггер; утверждение верно ровно для проекта, у
	// которого на момент заведения строки ресурсов НЕТ, и ложно для всякого
	// другого. Затравка считается тем же отображением «вид → таблица», которым
	// ведёт списание, — оно читается у самих триггеров, поэтому разойтись со
	// списанием не может.
	//
	// Гонки здесь нет by construction: пока строки учёта нет, вставка строки
	// ресурса отвергается, значит множество, которое считают, между счётом и
	// вставкой не меняется. `ON CONFLICT DO NOTHING` при этом сохраняет прежний
	// смысл — потребление уже заведённой строки не переписывается.
	//
	// `COALESCE(…, 0)` покрывает вид, который не списывается НИЧЕМ: у такого вида
	// потребления не существует, и ноль здесь — не догадка, а единственное
	// возможное значение. Само же наличие таких видов — предмет отдельной
	// задачи про виды без производителя списания, а не материализации.
	const stmt = `SELECT kacho_quota_admit($1, $2, $3)`
	if _, err := r.pool.Exec(ctx, stmt, carrierType, carrierID, kind); err != nil {
		return wrapPgErr(err, "Quota", "")
	}
	return nil
}

// QuotaSchema — схема, в которой у этого владельца лежит таблица учёта.
//
// `public`, а не `kacho_compute`: миграции этого сервиса создают таблицы БЕЗ
// квалификации, а DSN идёт без `search_path` (см. `cmd/compute/recovery.go` и
// `operations.NewRepo(pool, "public")`). Имя названо ЯВНО, а не оставлено на
// `search_path`, потому что общий оператор чтения принимает схему параметром: у
// оператора, собираемого из имени, умолчание означало бы молчаливую правку чужой
// таблицы, если соединение однажды придёт с другим путём поиска.
const QuotaSchema = "public"

// ListStates отдаёт строки учёта носителя — то, что арендатор читает как свои
// квоты.
//
// Оператор ОБЩИЙ (`pkg/quota.ListStates`): таблица у всех владельцев одна и та
// же с точностью до имени схемы, и своя копия здесь разошлась бы с соседями на
// составе столбцов или на порядке — то есть там, где расхождение не ломает
// сборку и не видно глазом.
func (r *QuotaRepo) ListStates(
	ctx context.Context, carrierType, carrierID string,
) ([]quotaread.State, error) {
	return corequota.ListStates(ctx, r.pool, QuotaSchema, carrierType, carrierID)
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
		SELECT s.carrier_type, s.carrier_id, s.kind,
		       COALESCE(kacho_quota_used_actual(
		           s.carrier_type, s.carrier_id, s.kind), 0),
		       s.limit_value, s.source_scope, s.source_scope_id,
		       s.limit_revision, s.account_id
		  FROM unnest(
		      $1::text[], $2::text[], $3::text[], $4::bigint[],
		      $5::text[], $6::text[], $7::bigint[], $8::text[])
		      AS s(carrier_type, carrier_id, kind, limit_value,
		           source_scope, source_scope_id, limit_revision, account_id)
		ON CONFLICT (carrier_type, carrier_id, kind) DO NOTHING`

	n := len(rows)
	carrierTypes := make([]string, n)
	carrierIDs := make([]string, n)
	kinds := make([]string, n)
	limits := make([]int64, n)
	scopes := make([]string, n)
	scopeIDs := make([]string, n)
	revisions := make([]int64, n)
	accounts := make([]string, n)
	for i, row := range rows {
		carrierTypes[i] = row.CarrierType
		carrierIDs[i] = row.CarrierID
		kinds[i] = row.Kind
		limits[i] = row.Limit
		scopes[i] = row.SourceScope
		scopeIDs[i] = row.SourceScopeID
		revisions[i] = row.LimitRevision
		accounts[i] = row.AccountID
	}

	tag, err := ex.Exec(ctx, stmt,
		carrierTypes, carrierIDs, kinds, limits, scopes, scopeIDs, revisions, accounts)
	if err != nil {
		return 0, wrapPgErr(err, "Quota", "")
	}
	return tag.RowsAffected(), nil
}
