// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/PRO-Robotech/kacho/pkg/quota/quotadetail"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// Классификация исходов учёта числа ресурсов.
//
// Приёмка `docs/specs/sub-phase-quota-v2-materialised-usage-acceptance.md`
// (APPROVED, раунд 2), V2-3 и DoD S4 п.1.

// SQLSTATE'ы, которыми триггер учёта сообщает СВОЙ исход.
//
// Классы, начинающиеся с буквы за пределами зарезервированных Postgres'ом,
// свободны для приложения — поэтому эти три не могут совпасть ни с одним кодом
// сервера и ни с одним кодом расширения. Значения те же, что у kacho-vpc, и это
// НЕ совпадение: один и тот же исход обязан приезжать одним и тем же кодом у
// каждого владельца, иначе клиент, научившийся читать один домен, на втором
// получит нераспознанный отказ.
const (
	// sqlstateQuotaExceeded — место кончилось: строка учёта есть, used >= limit.
	sqlstateQuotaExceeded = "KQ001"
	// sqlstateQuotaNotProvisioned — потолок не назван ни на одной области.
	sqlstateQuotaNotProvisioned = "KQ002"
	// sqlstateQuotaNoCarrier — строка ресурса не даёт носителя учёта. Дефект
	// схемы, а не арендатора: наружу уходит фиксированным внутренним отказом.
	sqlstateQuotaNoCarrier = "KQ003"
)

// classifyQuotaErr отличает исход учёта от всего остального; nil означает «это
// не отказ учёта» и передаёт разбор общей классификации.
//
// Текст производителя (`kacho_quota_refuse`) выносится наружу ДОСЛОВНО для двух
// первых исходов: он и есть контракт — называет носителя, предел и вид, — а
// пересказать его здесь значило бы завести второе место об одном предмете,
// ровно от чего обе полосы и защищены единственным производителем. Для третьего
// исхода текст НЕ сохраняется: он про нашу схему, и арендатору о ней знать
// нечего (`security.md` §Hardening #1).
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
			fmt.Errorf("%w: %s", kacho.ErrQuotaExceeded, pgErr.Message), pgErr.Detail)
	case sqlstateQuotaNotProvisioned:
		return quotadetail.Attach(
			fmt.Errorf("%w: %s", kacho.ErrQuotaNotProvisioned, pgErr.Message), pgErr.Detail)
	case sqlstateQuotaNoCarrier:
		return fmt.Errorf("%w: quota accounting: %v", kacho.ErrInternal, err)
	}
	return nil
}
