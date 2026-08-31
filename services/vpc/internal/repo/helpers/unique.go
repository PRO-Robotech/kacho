// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package helpers

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/PRO-Robotech/kacho/pkg/db/pgfault"
	"github.com/PRO-Robotech/kacho/pkg/quota/quotadetail"
)

// IsUniqueViolation — Postgres unique-constraint violation (SQLSTATE 23505).
// Используется в Create/Update для маппинга в gRPC AlreadyExists.
func IsUniqueViolation(err error) bool {
	return pgfault.Classify(err).Is(pgfault.Unique)
}

// NICMacUniqueConstraint — имя UNIQUE-индекса network_interfaces_mac_address_key
// на network_interfaces.mac_address (baseline 0001_initial.sql). См. IsNICMacCollision.
const NICMacUniqueConstraint = "network_interfaces_mac_address_key"

// IsNICMacCollision — true если err — это нарушение UNIQUE на
// network_interfaces.mac_address (а не на (project_id, name) или другом
// constraint таблицы). Используется в networkInterfaceWriter.Insert
// (internal/repo/kacho/pg) чтобы различить retry-able MAC-collision от
// настоящего AlreadyExists по имени.
func IsNICMacCollision(err error) bool {
	f := pgfault.Classify(err)
	return f.Is(pgfault.Unique) && f.Constraint == NICMacUniqueConstraint
}

// NICUsedByIndexUniqueConstraint — имя partial-UNIQUE-индекса ni_used_by_index_uniq
// на (used_by_id, used_by_index) WHERE used_by_id<>” (миграция
// 0014_network_interface_used_by_index). См. IsNICIndexCollision.
const NICUsedByIndexUniqueConstraint = "ni_used_by_index_uniq"

// IsNICIndexCollision — true если err — это нарушение partial-UNIQUE на
// (used_by_id, used_by_index): выбранный слот уже занят другим NIC на том же
// инстансе. Используется networkInterfaceWriter.AttachToInstance, чтобы отличить
// retry-able slot-collision (auto-index пересчитывает свободный слот) от прочих
// нарушений. Аналог IsNICMacCollision.
func IsNICIndexCollision(err error) bool {
	f := pgfault.Classify(err)
	return f.Is(pgfault.Unique) && f.Constraint == NICUsedByIndexUniqueConstraint
}

// GatewaySubnetFKConstraint — имя внешнего ключа `gateways.subnet_id →
// subnets(id)` (миграция 0030). Ссылка own-owned, поэтому её нарушение читается
// полосой direct-read: NOT_FOUND «Subnet <id> not found», а не generic
// FailedPrecondition из WrapPgErr.
const GatewaySubnetFKConstraint = "gateways_subnet_fk"

// AddressSubnetProjectFKConstraint — имя составного внешнего ключа
// `addresses (project_id, internal_subnet_id) → subnets (project_id, id)`
// (миграция 0033). Ссылка own-owned, и её нарушение означает ровно одно из двух:
// подсети нет ИЛИ подсеть принадлежит другому проекту. Оба читаются полосой
// direct-read — `NOT_FOUND "Subnet <id> not found"`, дословно тем же текстом,
// каким на отсутствующую подсеть отвечает синхронная проверка в use-case'е.
//
// Тон здесь не косметика, а анти-oracle: различимый текст позволял бы отличить
// «подсеть есть, но чужая» от «подсети нет», то есть отвечал бы на вопрос о
// чужом проекте (`security.md` §hardening, п.6). Generic-ветка `WrapPgErr` для
// 23503 («<kind> has dependent resources», FailedPrecondition) здесь неверна
// вдвойне: и полосой, и смыслом — зависимых ресурсов у вставляемого адреса нет.
const AddressSubnetProjectFKConstraint = "addresses_subnet_project_fk"

// RouteRefGatewayFKConstraint — имя внешнего ключа
// `route_table_gateway_refs.gateway_id → gateways(id)` (миграция 0030,
// автогенерируемое имя Postgres). Нарушение на пути ЗАПИСИ ссылки означает «шлюза
// с таким id нет» → NOT_FOUND «Gateway <id> not found»; нарушение на пути
// удаления шлюза означает обратное — «на шлюз ссылается живой маршрут», и его
// ловит `IsFKViolation` в `gatewayWriter.Delete`.
const RouteRefGatewayFKConstraint = "route_table_gateway_refs_gateway_id_fkey"

// IsFKViolationOn — нарушение внешнего ключа ИМЕННО названного constraint'а.
//
// Отдельная функция, а не сравнение текста у вызывающего: у одного оператора
// может быть несколько внешних ключей, и «какой-то FK не сошёлся» — это не тот
// же факт, что «не сошёлся вот этот». Fallback по подстроке оставлен для случая,
// когда ошибка пришла уже завёрнутой и типизированный pgconn.PgError из цепочки
// не достаётся, — тот же приём, что у IsNICMacCollision.
func IsFKViolationOn(err error, constraint string) bool {
	f := pgfault.Classify(err)
	return f.Is(pgfault.ForeignKey) && f.Constraint == constraint
}

// IsFKViolation — Postgres foreign_key_violation (SQLSTATE 23503).
// Возникает на Delete parent с зависимыми child-row (RESTRICT FK).
// Маппится в gRPC FailedPrecondition ("Network is not empty").
func IsFKViolation(err error) bool {
	return pgfault.Classify(err).Is(pgfault.ForeignKey)
}

// IsExclusionViolation — PG SQLSTATE 23P01 (exclusion_violation), возникает
// при нарушении EXCLUDE constraint (например `subnets_no_overlap_v4` —
// пересекающиеся v4 CIDR в одной VPC). Маппится на gRPC FailedPrecondition
// ("Subnet CIDRs can not overlap").
func IsExclusionViolation(err error) bool {
	return pgfault.Classify(err).Is(pgfault.Exclusion)
}

// IsCheckViolation — PG SQLSTATE 23514 (check_violation). Возникает при
// нарушении CHECK constraint (например, `network_interfaces_v4_addr_max1` —
// массив v4_address_ids длиннее 1 на одном NIC). Маппится на gRPC
// InvalidArgument через WrapPgErr.
func IsCheckViolation(err error) bool {
	return pgfault.Classify(err).Is(pgfault.Check)
}

// IsSerializationConflict — PG SQLSTATE 40001 (serialization_failure) или 40P01
// (deadlock_detected). Возникает под конкурентной записью на EXCLUDE/gist- или
// UNIQUE-constraint (напр. 3 параллельных Subnet.Create одного CIDR: Postgres
// может выдать deadlock при взаимной проверке gist-диапазонов вместо чистого
// 23P01). Retryable-класс → маппится в gRPC Aborted (не INTERNAL): клиент по
// контракту может безопасно повторить транзакцию.
func IsSerializationConflict(err error) bool {
	return pgfault.Classify(err).Is(pgfault.SerializationConflict)
}

// resourceKindText маппит camelCase Go-имя ресурса в текст для error-message
// "invalid <kind> id 'X'": snake_case для многословных kind-ов (route_table),
// single-word для остального.
func resourceKindText(kind string) string {
	switch kind {
	case "RouteTable":
		return "route_table"
	}
	return strings.ToLower(kind)
}

// IsInvalidUUID — PG SQLSTATE 22P02 (invalid_text_representation),
// возникает когда в WHERE id=$1 передан non-UUID string.
func IsInvalidUUID(err error) bool {
	return pgfault.Classify(err).Is(pgfault.InvalidText)
}

// WrapPgErr классифицирует pgx-ошибку и возвращает sentinel-ошибку из
// helpers-пакета. mapRepoErr в service-слое потом мапит ее на gRPC-status.
//
// НЕ leak'ает raw PG-сообщение клиенту: для неизвестных классов возвращает
// ErrInternal без exposing.
//
// kind/id — для AlreadyExists/NotFound сообщений (имя ресурса + id).
//
// SQLSTATE 22P02 (invalid_text_representation, malformed UUID-cast) →
// InvalidArgument: `invalid <kind> id '<id>'`.
func WrapPgErr(err error, kind, id string) error {
	if err == nil {
		return nil
	}
	// Отказ учёта классифицируется ПЕРВЫМ, до общих классов.
	//
	// Порядок здесь несущий, а не косметический: собственные SQLSTATE триггера
	// учёта не входят ни в один из классов ниже, поэтому без этой ветки они
	// доехали бы до `ErrInternal` — то есть арендатор, упёршийся в предел, видел
	// бы «что-то сломалось» вместо «место кончилось», и ровно тот отказ, ради
	// которого механизм существует, стал бы неотличим от сбоя хранилища.
	if quotaErr := classifyQuotaErr(err); quotaErr != nil {
		return quotaErr
	}
	if errors.Is(err, pgx.ErrNoRows) {
		if id != "" {
			return fmt.Errorf("%w: %s %s not found", ErrNotFound, kind, id)
		}
		return ErrNotFound
	}
	if IsUniqueViolation(err) {
		return ErrAlreadyExists
	}
	if IsInvalidUUID(err) {
		return fmt.Errorf("%w: invalid %s id '%s'", ErrInvalidArg, resourceKindText(kind), id)
	}
	if IsFKViolation(err) {
		return fmt.Errorf("%w: %s has dependent resources", ErrFailedPrecondition, kind)
	}
	if IsCheckViolation(err) {
		return wrapCheckViolation(err, kind)
	}
	if IsExclusionViolation(err) {
		return fmt.Errorf("%w: value conflicts with existing %s", ErrFailedPrecondition, kind)
	}
	// Retryable concurrency-конфликт (40001 serialization_failure / 40P01 deadlock):
	// под burst-нагрузкой на EXCLUDE/gist-constraint проигравшая транзакция может
	// получить deadlock вместо чистого 23P01 → gRPC Aborted (retryable), не INTERNAL.
	// Root-cause сохраняем в цепочке для server-side логов (как ErrInternal ниже).
	if IsSerializationConflict(err) {
		return fmt.Errorf("%w: %v", ErrConflict, err)
	}
	// Неклассифицированный класс (напр. 57014 statement_timeout, connection reset).
	// Клиент по контракту
	// получит фиксированный INTERNAL (serviceerr сворачивает ErrInternal-ветку в
	// "internal database error", no-leak), но root-cause сохраняем в цепочке для
	// server-side логов оператора — иначе SQLSTATE/constraint/detail теряются
	// безвозвратно на границе repo (CWE-778). Тот же `%w: %v`-паттерн, что и в
	// helpers/jsonb.go.
	return fmt.Errorf("%w: %v", ErrInternal, err)
}

// SQLSTATE'ы, которыми триггер учёта сообщает СВОЙ исход.
//
// Классы, начинающиеся с буквы за пределами зарезервированных Postgres'ом,
// свободны для приложения — поэтому эти три не могут совпасть ни с одним кодом
// сервера и ни с одним кодом расширения. Они объявлены здесь и в шапке
// миграции 0040; оба места называют один предмет, и второе — то, где они
// производятся.
const (
	// sqlstateQuotaExceeded — место кончилось: строка учёта есть, used >= limit.
	sqlstateQuotaExceeded = "KQ001"
	// sqlstateQuotaNotProvisioned — потолок не назван ни на одной области.
	sqlstateQuotaNotProvisioned = "KQ002"
	// sqlstateQuotaNoProjectID — строка ресурса не несёт проекта. Дефект схемы,
	// а не арендатора: наружу уходит фиксированным внутренним отказом.
	sqlstateQuotaNoProjectID = "KQ003"
)

// classifyQuotaErr отличает исход учёта от всего остального; nil означает «это
// не отказ учёта».
//
// Текст триггера сохраняется ДОСЛОВНО для двух первых исходов: он и есть
// контракт («project <P> has reached its limit of <N> <kind>»), а не диагностика
// хранилища, поэтому пересказывать его здесь значило бы завести второе место об
// одном предмете. Для третьего исхода текст НЕ сохраняется — он про нашу схему,
// и арендатору о ней знать нечего.
func classifyQuotaErr(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return nil
	}
	// Величины производителя приклеиваются ЗДЕСЬ — там, где `*pgconn.PgError` ещё
	// не потерян. Дальше по пути его нет, и прочитать `DETAIL` больше негде:
	// текст переживает переход, величины — нет (задача продукта #1605). Разбор
	// общий (`pkg/quota/quotadetail`) по тому же доводу, по которому производитель
	// один: шесть копий разошлись бы молча.
	switch pgErr.Code {
	case sqlstateQuotaExceeded:
		return quotadetail.Attach(
			fmt.Errorf("%w: %s", ErrQuotaExceeded, pgErr.Message), pgErr.Detail)
	case sqlstateQuotaNotProvisioned:
		return quotadetail.Attach(
			fmt.Errorf("%w: %s", ErrQuotaNotProvisioned, pgErr.Message), pgErr.Detail)
	case sqlstateQuotaNoProjectID:
		return fmt.Errorf("%w: quota accounting: %v", ErrInternal, err)
	}
	return nil
}

// wrapCheckViolation разбирает 23514 на ДВЕ полосы по одному вопросу: чьё это
// значение — вызывающего или наше (задача #718).
//
// # Почему один код ответа на два смысла — ложь
//
// Ограничение таблицы бывает двух видов, и они противоположны по тому, кого
// обвиняет отказ:
//
//   - форму значения проверяет САМ СЕРВИС до вставки (имя ресурса: доменный
//     newtype `domain.RcNameVPC` → `nameform.OK`, на обоих путях записи).
//     Ограничение таблицы здесь — защита последнего рубежа, и её срабатывание
//     означает, что негодное значение прошло МИМО проверки. Это наш дефект;
//     `INVALID_ARGUMENT` сказал бы вызывающему «виноват ваш ввод», а он не
//     виноват — и, что хуже, ему нечего исправлять;
//   - форму проверяет ТОЛЬКО база. Тогда отказ по вводу уместен.
//
// Разделяет их `nameform.IsConstraint` — по конструкции имени ограничения,
// которую задаёт миграция 715001, а не по догадке.
//
// # Почему текст не пересказывает СУБД
//
// «violates check constraint» — формулировка Postgres. Вызывающему она не
// сообщает ни поля, ни того, что исправить, зато выносит наружу словарь
// хранилища. Прежняя редакция отдавала её дословно (`"<Kind> violates check
// constraint"`), и именно этот текст арендатор видел при создании адреса.
// Тон отказа по вводу приведён к контрактному (`api-conventions.md`
// §Error-format); исходная ошибка остаётся в цепочке для журнала оператора.
//
// # Наблюдаемость
//
// Полоса внутреннего дефекта пишет ERROR с именем ограничения: иначе «сервис
// пропустил негодное значение» невидимо ниоткуда — на проводе фиксированный
// текст, а в цепочке причина доживает только до отображения в статус. Полоса
// ввода пишет WARN: ограничение, которое ловит ввод регулярно, — кандидат в
// СИНХРОННУЮ проверку, и его частота обязана быть счётной.
func wrapCheckViolation(err error, kind string) error {
	f := pgfault.Classify(err)
	if pgfault.CheckLaneOf(f) == pgfault.LaneServiceDefect {
		slog.Error("name form backstop fired: service admitted a name it validates itself",
			append([]any{"kind", kind}, f.LogAttrs()...)...)
		// Причина сохраняется в цепочке (как в неклассифицированной ветке ниже):
		// serviceerr сворачивает ErrInternal в фиксированный текст, поэтому
		// наружу она не уходит, а в журнале оператора остаётся.
		return fmt.Errorf("%w: %v", ErrInternal, err)
	}
	if f.FromDatabase() {
		slog.Warn("check constraint rejected caller input",
			append([]any{"kind", kind}, f.LogAttrs()...)...)
	}
	return fmt.Errorf("%w: Illegal argument", ErrInvalidArg)
}
