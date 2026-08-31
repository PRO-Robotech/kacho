// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package serviceerr

import (
	"errors"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	kerrors "github.com/PRO-Robotech/kacho/pkg/errors"
)

// MapRepoErr — единая трансляция repo-sentinel в gRPC status (копия VPC).
//
// Sentinel-prefix (`failed precondition: `, `not found`, ...) удаляется при
// преобразовании в gRPC-сообщение, чтобы клиент видел чистый текст без
// internal-обёртки.
//
// Fallthrough: неклассифицированный err → codes.Internal с фиксированным
// "internal database error" (закрывает info-leak vector через Operation.error.message).
//
// Порядок веток — сначала pass-through, потом sentinel-switch (форма kacho-nlb).
// Он несущий, а не косметический: pkg/validate кладёт имя поля ТОЛЬКО в
// google.rpc.BadRequest-details, сообщение остаётся общим «invalid argument».
// Пересборка статуса в sentinel-ветке (`status.Error(code, stripSentinel(...))`)
// детали теряет, поэтому ошибка, обёрнутая через `%w` на ports.Err*, обязана
// пройти pass-through ПЕРВОЙ. status с codes.Unknown под pass-through НЕ попадает
// (guard `!= Unknown`) — он падает в sentinel-switch и дальше в фиксированный
// INTERNAL, без leak'а.
func MapRepoErr(err error) error {
	if err == nil {
		return nil
	}
	// Если err уже gRPC-status (например из самого service-слоя) — пробрасываем.
	if st, ok := status.FromError(err); ok && st.Code() != codes.Unknown {
		return err
	}
	// Отказ учёта квоты — ПЕРЕД общим switch'ом: ему мало кода, он несёт ещё и
	// машинный признак полосы. Без этой ветки исчерпание доехало бы до
	// фиксированного INTERNAL, и арендатор увидел бы «что-то сломалось» вместо
	// «место кончилось».
	if st, ok := quotaRefusal(err); ok {
		return st
	}
	switch {
	case errors.Is(err, ErrNotFound):
		return status.Error(codes.NotFound, stripSentinel(err, ErrNotFound))
	case errors.Is(err, ErrAlreadyExists):
		return status.Error(codes.AlreadyExists, stripSentinel(err, ErrAlreadyExists))
	case errors.Is(err, ErrFailedPrecondition):
		return status.Error(codes.FailedPrecondition, stripSentinel(err, ErrFailedPrecondition))
	case errors.Is(err, ErrInvalidArg):
		return status.Error(codes.InvalidArgument, stripSentinel(err, ErrInvalidArg))
	case errors.Is(err, ErrInternal):
		return status.Error(codes.Internal, "internal database error")
	}
	// Defensive: raw error из repo без обёртки → generic Internal без leak'а текста.
	return status.Error(codes.Internal, "internal database error")
}

// mapRefErr транслирует ошибку existence-check ссылочного ресурса ИЗ ТОЙ ЖЕ БД
// (Image/Snapshot/Disk/DiskType lookup на request-path). Раньше эти call-site'ы
// слепо маппили ЛЮБУЮ non-nil ошибку в codes.NotFound "<Resource> <id> not found",
// из-за чего транзиентный сбой БД (обрыв соединения, deadline, query-error) во
// время lookup маскировался под перманентный NotFound — клиент не ретраил и вводился
// в заблуждение о несуществовании реально существующего ресурса (CWE-388).
//
// Теперь: настоящий not-found (repo вернул ErrNotFound) → codes.NotFound с
// детерминированным текстом "<Resource> <id> not found"; всё остальное
// (ErrInternal / raw pgx / транспорт) → делегируется MapRepoErr → codes.Internal
// (без leak'а текста) вместо ложного NotFound. Зеркалит дисциплину primary-Get.
func mapRefErr(err error, resource, id string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrNotFound) {
		return status.Errorf(codes.NotFound, "%s %s not found", resource, id)
	}
	return MapRepoErr(err)
}

// MapZoneRefErr транслирует ошибку existence-check zone_id (через ZoneRegistry —
// kacho-geo geo.v1.ZoneService.Get; Geography принадлежит kacho-geo) в
// gRPC-status.
//
// Зона не резолвится у владельца — полоса peer-validate: FailedPrecondition
// "Zone <id> not found" плюс машинный признак. Код здесь берётся у полосы, а не
// выписывается: прежде он был InvalidArgument, и текст в контракт-тоне
// отсутствия ресурса утверждал одно, а код — другое.
//
// Транспортная ошибка к kacho-geo пробрасывается как Unavailable с
// opaque-текстом (без leak'а raw peer-ошибки, зеркалит project-check +
// MapRepoErr-дисциплину).
func MapZoneRefErr(err error, zoneID string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrNotFound) {
		return zoneMissing(zoneID)
	}
	if st, ok := status.FromError(err); ok && st.Code() == codes.NotFound {
		return zoneMissing(zoneID)
	}
	// Opaque message: не эхоим raw peer transport-текст наружу (endpoint / dial
	// error → info-leak, CWE-209). Зеркалит MapRepoErr-дисциплину (фиксированный
	// текст, без leak'а). Детали остаются в server-side логах peer-клиента.
	return kerrors.ReasonPeerUnavailable.Errf(
		kerrors.PeerRef{Service: serviceDomain, ResourceType: "geo.zone", ResourceID: zoneID},
		"zone check: upstream geo service unavailable")
}

// serviceDomain — источник отказа в ErrorInfo.domain. Назван один раз на сервис:
// повторённый по местам вызова литерал разъезжается молча, и разъедется он
// именно там, где деталь читают машиной, а не глазом.
const serviceDomain = "compute"

// zoneMissing — одна форма для обеих веток промаха. Две ветки, собиравшие ответ
// каждая сама, — это то, как один сервис начинает отвечать двумя кодами на один
// текст; здесь у них общий конструктор, поэтому разойтись им нечем.
func zoneMissing(zoneID string) error {
	return kerrors.ReasonPeerResourceMissing.Errf(
		kerrors.PeerRef{Service: serviceDomain, ResourceType: "geo.zone", ResourceID: zoneID},
		"Zone %s not found", zoneID)
}

// stripSentinel — извлекает «полезную» часть сообщения (после «sentinel: »),
// чтобы выдать клиенту чистый текст без internal-обёртки sentinel-ошибки.
func stripSentinel(err, sentinel error) string {
	msg := err.Error()
	prefix := sentinel.Error() + ": "
	if rest, ok := strings.CutPrefix(msg, prefix); ok {
		// Пустой остаток — вырожденный случай: обёртка без текста
		// (`fmt.Errorf("%w: %s", sentinel, "")`). Отдать его клиенту значило бы
		// отказать БЕЗ СООБЩЕНИЯ — код без единого слова о том, что делать
		// дальше, неотличимый в журнале от потери сообщения. Замещается текстом
		// самого sentinel'а.
		if rest == "" {
			return sentinel.Error()
		}
		return rest
	}
	return msg
}
