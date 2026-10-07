// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package notice — общее use-case'ов внутреннего сервиса извещений
// `InternalNoticeService` (замысел issue-2924 З1, З3, З5; приёмка NTF-5 Р2–Р5):
// сущность извещения, порты хранилища, отказы и проекции контракта.
//
// Use-case на метод — подпакеты `create`, `get`, `list`, `update`, `start`,
// `complete`, `cancel`; здесь — то, что они делят: второй копии отказа или
// проекции ни в одном из них нет.
//
// Слой чист: хранилище (pgx) и транспорт (grpc-сервер) сюда не входят — они
// реализуют порты этого пакета и собираются в корне notify-api.
package notice

import (
	"context"
	"errors"
	"fmt"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	coreerrors "github.com/PRO-Robotech/corelib/errors"
	"github.com/PRO-Robotech/corelib/operations"
	corevalidate "github.com/PRO-Robotech/corelib/validate"

	"github.com/PRO-Robotech/kacho/services/notify/internal/notice/rules"
)

// Service — имя службы в домене деталей отказа (`notify.kacho.cloud`).
const Service = "notify"

// ResourceType — имя типа ресурса в текстах отказов и метаданных деталей.
const ResourceType = "notice"

// Clock — часы notify (порт): момент приёма, моменты переходов и `now`
// проверки «в будущем» берутся только отсюда (З3 п.1, З25).
type Clock func() time.Time

// Scope — область аудитории: аккаунт либо проект.
type Scope struct {
	// Type — "account" либо "project" (написание базы, `notice_audience.scope_type`).
	Type string
	ID   string
}

const (
	// ScopeAccount, ScopeProject — написания вида области.
	ScopeAccount = "account"
	ScopeProject = "project"
)

// Ref — затронутый ресурс `{type, id}` (Р4).
type Ref struct{ Type, ID string }

// Notice — извещение во внутренней проекции (Р18): всё, что знает база.
type Notice struct {
	ID          string
	Kind        rules.Kind
	State       rules.State
	StartsAt    time.Time
	EndsAt      *time.Time
	StartedAt   *time.Time
	CompletedAt *time.Time
	CancelledAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Revision    int64
	CreatedBy   string
	// AllAccounts — аудитория «все аккаунты»; тогда Audience пуст.
	AllAccounts bool
	// Audience — области аудитории в порядке запроса оператора.
	Audience []Scope
	// Affected — затронутые ресурсы в порядке запроса, без сужения.
	Affected []Ref
	// Reminders — несостоявшиеся напоминания по возрастанию момента.
	Reminders []time.Time
}

// CreateRequest — заявка Create, как её записывает приём (Р10 шаг 1, З3 п.1).
type CreateRequest struct {
	NoticeID    string
	OperationID string
	Kind        rules.Kind
	StartsAt    *time.Time
	EndsAt      *time.Time
	AllAccounts bool
	AccountIDs  []string
	ProjectIDs  []string
	Affected    []Ref
	CreatedBy   string
	AcceptedAt  time.Time
}

// Page — страница внутреннего списка: keyset `(created_at, id)` после курсора.
type Page struct {
	// After — курсор: строки строго после пары; нулевое значение — первая страница.
	AfterCreatedAt time.Time
	AfterID        string
	// Limit — сколько строк читать (размер страницы + 1 — признак следующей).
	Limit int
}

// Finisher строит операцию, рождённую завершённой (Ф2), и её ответ из
// извещения ПОСЛЕ записи — в той же транзакции, что и запись (Р5).
type Finisher func(n Notice) (operations.Operation, *anypb.Any, error)

// TransitInput — переход Start/Complete/Cancel (З5 п.1–3).
type TransitInput struct {
	ID         string
	Transition rules.Transition
	Now        time.Time
}

// UpdateInput — перенос моментов (З5 п.4). nil — поле не меняется.
type UpdateInput struct {
	ID       string
	StartsAt *time.Time
	EndsAt   *time.Time
	Now      time.Time
	// Lead — ручка KACHO_NOTIFY_NOTICE_REMINDER_LEAD: напоминания пересчитываются
	// той же функцией правила, что у фиксации Create (З7 п.1).
	Lead time.Duration
}

// Store — порт хранилища извещений notify-api. Каждый метод записи — одна
// транзакция, в которой лежит и строка операции (Ф1/Ф2): записи операции вне
// транзакции мутации нет (Р23).
type Store interface {
	// Accept записывает заявку Create и операцию done=false (Ф1) одной
	// транзакцией.
	Accept(ctx context.Context, r CreateRequest, op operations.Operation, p operations.Principal) error
	// Get — извещение во внутренней проекции; ErrNotFound — строки нет.
	Get(ctx context.Context, id string) (Notice, error)
	// List — страница всех извещений во внутренней проекции.
	List(ctx context.Context, p Page) ([]Notice, error)
	// Transit — переход одной записью с условием (З5); отказ — *TransitionRefusal
	// либо ErrNotFound, операция при отказе не пишется.
	Transit(ctx context.Context, in TransitInput, p operations.Principal, finish Finisher) (Notice, error)
	// Update — перенос моментов (З5 п.4); отказы — как у Transit, нарушение
	// ограничения моментов — *ConstraintRefusal.
	Update(ctx context.Context, in UpdateInput, p operations.Principal, finish Finisher) (Notice, error)
}

// ErrNotFound — извещения с этим id нет.
var ErrNotFound = errors.New("notice not found")

// TransitionRefusal — ноль строк записи перехода, классифицированный чтением
// той же транзакции после оператора (З5 п.2): вид не поддерживает глагол либо
// состояние не то.
type TransitionRefusal struct {
	Kind  rules.Kind
	State rules.State
}

func (r *TransitionRefusal) Error() string {
	return fmt.Sprintf("notice transition refused: kind %s state %s", r.Kind, r.State)
}

// ConstraintRefusal — запись отвергнута названным ограничением базы (`23514`).
// Kind — вид извещения: текст отказа «не разрешено для вида» его называет.
type ConstraintRefusal struct {
	Constraint string
	Kind       rules.Kind
}

func (r *ConstraintRefusal) Error() string {
	return "notice write refused by constraint " + r.Constraint
}

// NotFound — отказ «нет такого извещения» (Р3, З13 п.1): один конструктор на
// все чтения и переходы, чтобы невидимое и несуществующее не различались.
func NotFound(id string) error {
	return coreerrors.ReasonResourceNotFound.Errf(
		coreerrors.PeerRef{Service: Service, ResourceType: ResourceType, ResourceID: id},
		"Notice %s not found", id)
}

// ValidateID — форма id извещения первым оператором (Р3, `api-malformed-id-sync`):
// пустой — `notice_id: required`, неверная форма — `invalid notice id '<X>'`.
func ValidateID(id string) error {
	if id == "" {
		return status.Error(codes.InvalidArgument, "notice_id: required")
	}
	if err := corevalidate.ResourceID(ResourceType, "ntc", id); err != nil {
		return coreerrors.ReasonInvalidResourceID.Errf(
			coreerrors.PeerRef{Service: Service, ResourceType: ResourceType, ResourceID: id},
			"invalid notice id '%s'", id)
	}
	return nil
}

// errStorage — фиксированный текст отказа хранилища: текст драйвера наружу не
// выходит (`hard-no-err-echo`).
var errStorage = status.Error(codes.Internal, "notice storage failed")

// Refusal переводит отказ хранилища в статус контракта — единственный дом
// этого перевода для глаголов извещения (Р4, Р5, З5 п.2, п.5).
func Refusal(id string, verb rules.Verb, t rules.Transition, err error) error {
	if errors.Is(err, ErrNotFound) {
		return NotFound(id)
	}
	var tr *TransitionRefusal
	if errors.As(err, &tr) {
		if !t.Supports(tr.Kind) {
			return status.Errorf(codes.FailedPrecondition, "Notice %s of kind %s does not support %s", id, tr.Kind, verb)
		}
		return status.Errorf(codes.FailedPrecondition, "Notice %s is %s, expected %s", id, tr.State, t.From)
	}
	var cr *ConstraintRefusal
	if errors.As(err, &cr) {
		if msg, ok := constraintText(cr); ok {
			return status.Error(codes.InvalidArgument, msg)
		}
	}
	return errStorage
}

// StorageFailure — отказ хранилища без классификации: фиксированный текст.
func StorageFailure() error { return errStorage }

// constraintText — закрытый перевод имени ограничения `notices` в текст Р4
// (З5 п.5). Ограничение вне перечня — не отказ входа, а сбой: ложного текста
// о поле, которого запрос не трогал, нет.
func constraintText(cr *ConstraintRefusal) (string, bool) {
	switch cr.Constraint {
	case "notices_ends_after_starts_chk":
		return "endsAt: must be after startsAt", true
	case "notices_ends_at_kind_chk":
		return "endsAt: not allowed for kind " + string(cr.Kind), true
	case "notices_starts_after_created_chk":
		return "startsAt: must be in the future", true
	}
	return "", false
}

// Seconds — момент, усечённый до секунды (З25): часы notify и моменты входа
// приводятся к секунде один раз, до записи.
func Seconds(t time.Time) time.Time { return t.UTC().Truncate(time.Second) }

// Key — ключ порядка `(created_at, id)` строки списка (Р16).
func Key(n Notice) (time.Time, string) { return n.CreatedAt, n.ID }
