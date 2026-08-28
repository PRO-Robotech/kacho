// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/PRO-Robotech/kacho/pkg/db/pgfault"
	coreerrors "github.com/PRO-Robotech/kacho/pkg/errors"
	"github.com/PRO-Robotech/kacho/pkg/pagetoken"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// Ensure pgconn import не считается unused — pgErr typed-assertion использует
// его type identity напрямую в mapPgErr.
var _ *pgconn.PgError = nil

// mapPgErr классифицирует pgx-ошибку и возвращает sentinel из пакета `kacho`.
// service-слой потом мапит её на gRPC-status (см. domain/errors.go).
//
// Не leak'ает raw PG-сообщение клиенту: для неизвестных классов возвращает
// ErrInternal без exposing.
//
// kind/id — для AlreadyExists/NotFound сообщений. Skill workspace CLAUDE.md
// «Within-service refs» — все DB-violations сводятся к одному из 5 sentinel'ов.
//
// SQLSTATE table (Postgres):
//
//	23505 unique_violation             → ErrAlreadyExists
//	23503 foreign_key_violation        → ErrFailedPrecondition
//	23514 check_violation              → ErrInternal (форма имени) | ErrInvalidArg
//	23P01 exclusion_violation          → ErrFailedPrecondition
//	22P02 invalid_text_representation  → ErrInvalidArg (malformed cast)
//
// pgx.ErrNoRows → ErrNotFound. Все остальное → ErrInternal.
func mapPgErr(err error, kind, id string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		if id != "" {
			return fmt.Errorf("%w: %s %s not found", kacho.ErrNotFound, kind, id)
		}
		return kacho.ErrNotFound
	}
	// Учёт числа ресурсов классифицируется ПЕРВЫМ и отдельно от общей таблицы
	// SQLSTATE'ов: клиенту мало кода — он обязан различать полосы машинно, по
	// признаку, а не разбором прозы. Подробности — в classifyQuotaErr.
	if qerr := classifyQuotaErr(err); qerr != nil {
		return qerr
	}
	f := pgfault.Classify(err)
	switch f.Class {
	case pgfault.Unique:
		switch f.Constraint {
		case "listeners_lb_port_proto_uniq":
			return fmt.Errorf("%w: listener with this port and protocol already exists on the load balancer", kacho.ErrAlreadyExists)
		case "targets_instance_id_uniq", "targets_nic_id_uniq",
			"targets_ip_ref_uniq", "targets_external_ip_uniq":
			return fmt.Errorf("%w: target with this identity already exists in the target group", kacho.ErrAlreadyExists)
		}
		return fmt.Errorf("%w: %s with name already exists", kacho.ErrAlreadyExists, kind)
	case pgfault.ForeignKey:
		switch f.Constraint {
		case "listeners_target_group_fk":
			if kind == "TargetGroup" {
				return fmt.Errorf("%w: target group is referenced by listeners", kacho.ErrFailedPrecondition)
			}
			return fmt.Errorf("%w: listener requires an existing target group", kacho.ErrFailedPrecondition)
		}
		return fmt.Errorf("%w: %s has dependent resources", kacho.ErrFailedPrecondition, kind)
	case pgfault.Check:
		return wrapCheckViolation(f, err, kind)
	case pgfault.Exclusion:
		return fmt.Errorf("%w: %s value conflicts", kacho.ErrFailedPrecondition, kind)
	case pgfault.InvalidText:
		return fmt.Errorf("%w: invalid %s id '%s'", kacho.ErrInvalidArg, strings.ToLower(kind), id)
	}
	return fmt.Errorf("%w: %v", kacho.ErrInternal, err)
}

// invalidArg формирует kacho.ErrInvalidArg с user-friendly текстом —
// используется для page-token decode errors и т.п.
func invalidArg(field, msg string) error {
	return fmt.Errorf("%w: %s: %s", kacho.ErrInvalidArg, field, msg)
}

// pageCursor — opaque payload для PageToken: (created_at, id) snapshot.
type pageCursor struct {
	CreatedAt time.Time
	ID        string
}

// encodePageToken — base64-encoded "RFC3339Nano\x00id". Skill workspace CLAUDE.md
// opaque cursor: не показываем внутренности клиенту.
func encodePageToken(t time.Time, id string) string {
	if t.IsZero() && id == "" {
		return ""
	}
	// Форма объявлена ОДИН раз — в `pkg/pagetoken` (#652). Она отличается от
	// канонической (алфавит URL без дополнения, разделитель — нулевой байт), и
	// это различие ВИДНО там же: пока каждая форма жила у себя, несовместимость
	// токенов двух служб обнаруживалась только на чужом курсоре.
	return pagetoken.NULSeparatedRawURL.Encode(pagetoken.Cursor{CreatedAt: t, ID: id})
}

// decodePageToken — обратное преобразование. Malformed token →
// invalidArg("page_token",...) (ErrInvalidArg → gRPC InvalidArgument).
func decodePageToken(token string) (pageCursor, error) {
	if token == "" {
		return pageCursor{}, nil
	}
	// Тексты отказа СВЕДЕНЫ к одному: общий разбор не сообщает, на каком именно
	// шаге форма не сошлась, и это осознанно. Три разных текста говорили
	// предъявителю, ЧЕМ именно негоден его непрозрачный курсор, — то есть
	// описывали внутренности, которые он не вправе знать.
	c, ok := pagetoken.NULSeparatedRawURL.Decode(token)
	if !ok || c == nil {
		return pageCursor{}, invalidArg("page_token", "malformed page_token")
	}
	return pageCursor{CreatedAt: c.CreatedAt, ID: c.ID}, nil
}

// pageSizeOrDefault — clamp page_size в [1, MaxPageSize]; 0 → DefaultPageSize=50.
func pageSizeOrDefault(p int64) (int64, error) {
	const (
		defaultPageSize = 50
		maxPageSize     = 1000
	)
	if p == 0 {
		return defaultPageSize, nil
	}
	if p < 0 || p > maxPageSize {
		return 0, coreerrors.InvalidArgument().
			AddFieldViolation("page_size",
				fmt.Sprintf("page_size must be in range [1, %d]", maxPageSize)).
			Err()
	}
	return p, nil
}

// wrapCheckViolation разбирает 23514 на две полосы по вопросу «чьё это
// значение» (задача #718; тот же разбор, что в vpc — класс, а не экземпляр).
//
// Имя ресурса nlb судит сам: `domain.LbName.Validate` зовёт `corevalidate.Name`,
// то есть единую форму дерева, и делает это на обоих путях записи. Ограничение
// таблицы, поставленное миграцией 715001, — защита последнего рубежа; её
// срабатывание означает, что негодное имя прошло МИМО проверки, то есть дефект
// сервиса. Отвечать на него `INVALID_ARGUMENT` значит обвинять вызывающего в
// нашей ошибке и не давать ему ничего, что можно исправить.
//
// Прочие ограничения остаются отказом по вводу, но говорят тоном контракта:
// «violates check constraint» — формулировка Postgres, а не Kachō. Исходная
// ошибка сохраняется в цепочке для журнала оператора; наружу её сворачивает
// отображение в статус.
func wrapCheckViolation(f pgfault.Fault, err error, kind string) error {
	if pgfault.CheckLaneOf(f) == pgfault.LaneServiceDefect {
		slog.Error("name form backstop fired: service admitted a name it validates itself",
			append([]any{"kind", kind}, f.LogAttrs()...)...)
		// Причина остаётся в цепочке для журнала оператора; наружу её не
		// выпускает отображение, сворачивающее ErrInternal в фиксированный текст.
		return fmt.Errorf("%w: %v", kacho.ErrInternal, err)
	}
	slog.Warn("check constraint rejected caller input",
		append([]any{"kind", kind}, f.LogAttrs()...)...)
	return fmt.Errorf("%w: Illegal argument", kacho.ErrInvalidArg)
}
