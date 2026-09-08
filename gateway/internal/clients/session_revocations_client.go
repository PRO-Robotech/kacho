// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package clients — adapter: wraps the generated
// `InternalSessionRevocationsServiceClient` so the handler/logout package can
// depend on a narrow port-interface (`handler.SessionRevocationsClient`)
// instead of the full proto stub. Clean Architecture: adapter is the only
// place that talks gRPC.
package clients

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	"github.com/PRO-Robotech/kacho/gateway/internal/streamrevocation"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"
)

// SessionRevocationsAdapter wraps the generated gRPC client to satisfy
// handler.SessionRevocationsClient. The Operation result is discarded —
// callers care only about success/failure of the synchronous DB write.
type SessionRevocationsAdapter struct {
	client iamv1.InternalSessionRevocationsServiceClient

	// iam — ТОТ ЖЕ внутренний слушатель того же соседа, другой набор глаголов.
	//
	// Вопрос о живости базового удостоверения (kacho#1450) живёт в
	// `InternalIAMService`, а не в службе отзыва сессий, — но задаёт его тот же
	// спрашивающий тому же соседу по тому же соединению. Второй адаптер и второе
	// поле в точке сборки означали бы половину, которую можно провязать
	// отдельно, — то есть половину, которую можно ЗАБЫТЬ отдельно, и забытая
	// выглядела бы исполняемым контролем ровно для той полосы, что осталась.
	iam iamv1.InternalIAMServiceClient
}

// NewSessionRevocationsAdapter wires the adapter onto an existing gRPC
// connection to kaname:9091.
func NewSessionRevocationsAdapter(cc grpc.ClientConnInterface) *SessionRevocationsAdapter {
	return &SessionRevocationsAdapter{
		client: iamv1.NewInternalSessionRevocationsServiceClient(cc),
		iam:    iamv1.NewInternalIAMServiceClient(cc),
	}
}

// IsBasicCredentialLive спрашивает НАШ авторитет, живо ли базовое удостоверение,
// названное идентификатором своей строки (kacho#1450).
//
// ЗАЧЕМ ОТДЕЛЬНЫЙ ВОПРОС, ЕСЛИ РЯДОМ ЕСТЬ РЕЗОЛВ. Тот отвечает ПРЕДЪЯВИТЕЛЮ и
// потому требует предъявленной строки. Спрашивающий с открытого соединения
// предъявителем не является: секрет он видел однажды, при открытии, и держать
// его живым весь срок соединения значило бы завести поверхность хранения ради
// контроля.
//
// ТРИ ИСХОДА ВЛАДЕЛЬЦА ПЕРЕВОДЯТСЯ ЗДЕСЬ, И НИ ОДИН НЕ СЛИВАЕТСЯ:
//   - `OK` → живо;
//   - `UNAUTHENTICATED` → НЕ живо, единым отказом полосы (различимого исхода у
//     владельца нет намеренно — он был бы оракулом);
//   - `UNIMPLEMENTED` → окно раската: служба прав ещё не докатилась до того же
//     дерева. Знание о кодах транспорта принадлежит адаптеру, поэтому перевод в
//     типизированный признак делается ЗДЕСЬ, а решение — на слое, который им
//     пользуется;
//   - прочее → «спросить не удалось».
func (a *SessionRevocationsAdapter) IsBasicCredentialLive(
	ctx context.Context, credentialID string,
) (bool, error) {
	_, err := a.iam.CheckBasicCredentialLive(ctx,
		&iamv1.CheckBasicCredentialLiveRequest{CredentialId: credentialID})
	switch {
	case err == nil:
		return true, nil
	case status.Code(err) == codes.Unauthenticated:
		return false, nil
	case status.Code(err) == codes.Unimplemented:
		return false, streamrevocation.ErrBasicCredentialLivenessUnsupported
	default:
		return false, err
	}
}

// Revoke invokes InternalSessionRevocationsService.Revoke and discards the
// Operation envelope. Returns the underlying gRPC error unchanged — the
// handler caller is responsible for mapping it to a user-visible warning.
func (a *SessionRevocationsAdapter) Revoke(ctx context.Context, in *iamv1.RevokeRequest) error {
	_, err := a.client.Revoke(ctx, in)
	return err
}

// IsSessionRevoked asks kaname whether the credential with this identifier
// has been revoked in OUR record — the one written by sign-out and by the
// administrative force-logout.
//
// ЗАЧЕМ ОТДЕЛЬНЫЙ ВОПРОС, ЕСЛИ КРАЙ УЖЕ СПРАШИВАЕТ ПРОВАЙДЕРА. Провайдер знает
// о своих отзывах и об истечении срока; о записи, которую делаем МЫ, он не знает
// и знать не может. До появления этого вызывающего наш отзыв не участвовал в
// решении на пути запроса вовсе: запись писалась и читалась только
// административными путями (#797).
//
// Ошибка возвращается БЕЗ ПОДМЕНЫ: «спросить не удалось» и «отозван» — разные
// исходы, и вызывающий (middleware.LocalThenProviderRevocation) обязан их
// различать, иначе недоступность соседа читалась бы как отзыв, а отзыв — как
// недоступность.
func (a *SessionRevocationsAdapter) IsSessionRevoked(ctx context.Context, jti string) (bool, error) {
	resp, err := a.client.IsRevoked(ctx, &iamv1.IsRevokedRequest{TokenJti: jti})
	if err != nil {
		return false, err
	}
	return resp.GetRevoked(), nil
}

// SessionCutoffOf спрашивает НАШ авторитет про СУБЪЕКТА: момент, раньше которого
// его сессии недействительны.
//
// ЗАЧЕМ ОТДЕЛЬНЫЙ ВОПРОС, ЕСЛИ РЯДОМ ЕСТЬ `IsSessionRevoked`. Тот спрашивает про
// одно удостоверение по его идентификатору. У браузерной сессии удостоверения
// нет вовсе — ни `jti`, ни подписи, которую край мог бы прочитать; спросить про
// неё можно только по паре (субъект, момент аутентификации). До появления этого
// вызывающего запись, которую делает наш выход, на браузерной полосе не читал
// никто, и административный принудительный выход человека из консоли не выводил.
//
// Три исхода несёт ПАРА возвращаемых значений вместе с ошибкой: «отсечки нет»
// (found=false) и «спросить не удалось» (err) — разные состояния, и слитые в
// одно они дали бы либо мягкий проход на молчащем авторитете, либо отказ
// каждому, кого никто не отзывал.
func (a *SessionRevocationsAdapter) SessionCutoffOf(
	ctx context.Context, userID string,
) (time.Time, bool, error) {
	resp, err := a.client.SessionCutoffOf(ctx, &iamv1.SessionCutoffOfRequest{UserId: userID})
	if err != nil {
		// «Метода нет» — НЕ «не ответил». Раскат не атомарен: реплика края
		// поднимается раньше, чем докатится служба прав, и в этом окне она
		// отвечает именно так. Знание о кодах транспорта принадлежит адаптеру,
		// поэтому перевод в типизированный признак делается здесь, а решение —
		// на слое, который им пользуется.
		if status.Code(err) == codes.Unimplemented {
			return time.Time{}, false, middleware.ErrSessionCutoffUnsupported
		}
		return time.Time{}, false, err
	}
	if !resp.GetFound() {
		return time.Time{}, false, nil
	}
	return resp.GetRevokeBefore().AsTime(), true, nil
}
