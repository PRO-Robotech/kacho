// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package serviceerr

import (
	"errors"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// classifyRepoSentinel — единый источник истины для маппинга repo-sentinel'а в
// gRPC-код. Покрывает весь superset sentinel'ов слоя repo; ok=false, если err не
// принадлежит семейству sentinel'ов. Раньше три параллельных mapper'а
// (serviceerr.MapRepoErr, handler.internalMapErr, addresspool.mapPoolErr)
// держали каждый свой switch с subtly различающимся набором ветвей — новый
// sentinel легко было забыть в одном из них (дрейф). Теперь классификация — в
// одном месте, а различие политик (контекстный vs strict leak-safe текст,
// fallback-сообщение) выражено параметрами двух публичных обёрток ниже.
func classifyRepoSentinel(err error) (code codes.Code, sentinel error, ok bool) {
	switch {
	case errors.Is(err, ErrNotFound):
		return codes.NotFound, ErrNotFound, true
	case errors.Is(err, ErrAlreadyExists):
		return codes.AlreadyExists, ErrAlreadyExists, true
	case errors.Is(err, ErrFailedPrecondition):
		return codes.FailedPrecondition, ErrFailedPrecondition, true
	case errors.Is(err, ErrPoolNotResolved):
		// Ни один шаг IPAM cascade не дал pool — FailedPrecondition
		// (конфигурация пулов неполна), а не INTERNAL.
		return codes.FailedPrecondition, ErrPoolNotResolved, true
	case errors.Is(err, ErrInvalidArg):
		return codes.InvalidArgument, ErrInvalidArg, true
	case errors.Is(err, ErrConflict):
		// Retryable concurrency-конфликт (40001/40P01) → Aborted, не INTERNAL:
		// клиент по контракту может безопасно повторить транзакцию.
		return codes.Aborted, ErrConflict, true
	case errors.Is(err, ErrInternal):
		return codes.Internal, ErrInternal, true
	}
	return codes.Unknown, nil, false
}

// MapRepoErr — трансляция repo-sentinel в gRPC status для публичных сервисов
// (контекстная политика сообщений).
//
// Sentinel-prefix (`failed precondition: `, `not found`, ...) удаляется при
// преобразовании, чтобы клиент видел чистый текст без internal-обертки. Для
// ErrInternal — фиксированный "internal database error" (no leak).
//
// Fallthrough: неклассифицированный err (например raw pgx без обертки в repo)
// → codes.Internal "internal database error". Это закрывает info-leak через
// Operation.error.message, если новый repo-метод забыли обернуть.
//
// Resource-use-case'ы в `internal/apps/kacho/api/<x>/` держат собственные
// **локальные** копии `mapRepoErr`; этот общий `MapRepoErr` используется
// не-ресурсными сервисами в `internal/apps/kacho/services/*`
// (AddressPoolService, AddressReferenceService, NetworkInternal).
//
// Порядок веток — сначала pass-through, потом sentinel-классификация (форма
// kacho-nlb). Он несущий, а не косметический: pkg/validate кладёт имя поля
// ТОЛЬКО в google.rpc.BadRequest-details, сообщение остаётся общим «invalid
// argument». Пересборка статуса в sentinel-ветке (`status.Error(code,
// stripSentinel(...))`) детали теряет, поэтому ошибка, обёрнутая через `%w` на
// repo-sentinel, обязана пройти pass-through ПЕРВОЙ. status с codes.Unknown под
// pass-through НЕ попадает (guard `!= Unknown`) — он падает в классификацию и
// дальше в фиксированный INTERNAL, без leak'а.
func MapRepoErr(err error) error {
	if err == nil {
		return nil
	}
	// Если err уже gRPC-status (например из самого service-слоя через
	// status.Errorf) — пробрасываем как есть. status.FromError возвращает
	// (status, true) даже для не-status err (с code=Unknown) — поэтому
	// проверяем code != Unknown.
	if st, ok := status.FromError(err); ok && st.Code() != codes.Unknown {
		return err
	}
	// Отказ учёта разбирается ДО общей классификации: ему мало кода, он обязан
	// нести машинный признак полосы (`quota.go`). Общая ветка признака не даёт,
	// и попав в неё первым, отказ учёта уехал бы к арендатору неотличимым от
	// любого другого предусловия.
	if refusal, ok := quotaRefusal(err); ok {
		return refusal
	}
	if code, sentinel, ok := classifyRepoSentinel(err); ok {
		if sentinel == ErrInternal {
			return status.Error(codes.Internal, "internal database error")
		}
		if sentinel == ErrConflict {
			// Фиксированный текст: ErrConflict оборачивает raw pgx (root-cause для
			// логов), stripSentinel вернул бы pgx-tail → leak. Отдаём чистый sentinel.
			return status.Error(codes.Aborted, ErrConflict.Error())
		}
		return status.Error(code, stripSentinel(err, sentinel))
	}
	// Defensive: raw error из repo без обертки → не leak'аем текст.
	return status.Error(codes.Internal, "internal database error")
}

// MapRepoErrLeakSafe — трансляция repo-sentinel в gRPC status для Internal/admin
// handler'ов (:9091) со **строгой** политикой сообщений: для sentinel-ветвей
// отдаётся голый `sentinel.Error()` (обёрнутый tail, который мог бы содержать
// pgx-detail — hostname/db/query-fragment — НЕ протаскивается наружу). Для
// неклассифицированного err — фиксированный `fallback` (Internal), raw-текст не
// течёт. Уже-сформированный gRPC status (не Unknown) пробрасывается как есть.
//
// Единая замена бывших handler.internalMapErr и addresspool.mapPoolErr, которые
// повторяли этот switch. `fallback` — контекстный tag оператора ("internal
// error" / "address pool admin error" / ...).
//
// Порядок веток — тот же, что в MapRepoErr: pass-through идёт ПЕРВЫМ, чтобы
// details уже-сформированного статуса не терялись при пересборке. Строгая
// текстовая политика касается sentinel-ветвей (голый `sentinel.Error()`), а не
// уже-курированного статуса, который и раньше проходил насквозь — просто ниже.
func MapRepoErrLeakSafe(err error, fallback string) error {
	if err == nil {
		return nil
	}
	if st, ok := status.FromError(err); ok && st.Code() != codes.Unknown {
		return err
	}
	// Тот же разбор, что и у публичного отображения, и намеренно тот же ТЕКСТ.
	// Строгая политика этой обёртки существует против утечки pgx-хвоста; текст
	// отказа учёта хвостом не является — его производит наша же функция базы, он
	// назван контрактом и обязан читаться одинаково на обоих слушателях. Полоса,
	// теряющая код или признак на одном из портов, давала бы один отказ,
	// читаемый по-разному в зависимости от того, куда постучались.
	if refusal, ok := quotaRefusal(err); ok {
		return refusal
	}
	if code, sentinel, ok := classifyRepoSentinel(err); ok {
		if sentinel == ErrInternal {
			return status.Error(codes.Internal, "internal database error")
		}
		return status.Error(code, sentinel.Error())
	}
	if fallback == "" {
		fallback = "internal database error"
	}
	return status.Error(codes.Internal, fallback)
}

// stripSentinel — извлекает «полезную» часть сообщения (после «sentinel: »),
// чтобы выдать клиенту чистый текст без internal-обертки sentinel-ошибки.
// Если err == sentinel или контекст не добавлен, возвращает sentinel.Error().
func stripSentinel(err, sentinel error) string {
	msg := err.Error()
	prefix := sentinel.Error() + ": "
	if rest, ok := strings.CutPrefix(msg, prefix); ok {
		// Пустой остаток — вырожденный случай: обёртка без текста
		// (`fmt.Errorf("%w: %s", sentinel, "")`). Отдать его клиенту значило бы
		// отказать БЕЗ СООБЩЕНИЯ — код без единого слова о том, что делать
		// дальше, неотличимый в журнале от потери сообщения. Замещается текстом
		// самого sentinel'а (задача продукта #1658, полоса ct2-misc).
		if rest == "" {
			return sentinel.Error()
		}
		return rest
	}
	return msg
}
