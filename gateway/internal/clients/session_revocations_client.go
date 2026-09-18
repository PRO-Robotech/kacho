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

	iamv1 "github.com/PRO-Robotech/kaname/pkg/api/kaname/cloud/iam/v1"
)

// sessionRevocationsCallTimeout — предел края на КАЖДЫЙ вызов к внутреннему
// слушателю службы о сессии (#2713). Без него неотвечающий сосед (сборка мусора,
// перегрузка, полуоткрытый TCP) держит горутину края, пока клиент сам не
// разорвёт соединение: сервер края отвечает по чтению запроса (ReadTimeout), но
// не по ожиданию соседа. Одна величина на все sibling-глаголы — per
// architecture.md: «все sibling-методы клиента обязаны применять один и тот же
// configured-timeout (не „часть — да, часть — нет")». Величина та же, что у
// соседних клиентов внутреннего слушателя (iam_subject: 5s; opsproxy: 5s).
const sessionRevocationsCallTimeout = 5 * time.Second

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

	// human — вопрос о НАШЕЙ сессии по носителю (Ф3 Р7): тот же внутренний
	// слушатель, то же соединение, что у вопроса об отсечке. Два вопроса края на
	// одном предъявлении идут одному соседу по одному каналу, и второй адаптер
	// в точке сборки был бы половиной, которую можно провязать — и забыть —
	// отдельно.
	human iamv1.InternalHumanSessionServiceClient

	// callTimeout — единый источник per-call предела, от которого производит
	// дедлайн КАЖДЫЙ глагол адаптера (#2713). Одно поле держит поведение
	// одинаковым под задержкой соседа; ноль означал бы мгновенное истечение,
	// поэтому конструктор всегда выставляет его в sessionRevocationsCallTimeout.
	callTimeout time.Duration
}

// NewSessionRevocationsAdapter wires the adapter onto an existing gRPC
// connection to kaname:9091.
func NewSessionRevocationsAdapter(cc grpc.ClientConnInterface) *SessionRevocationsAdapter {
	return &SessionRevocationsAdapter{
		client:      iamv1.NewInternalSessionRevocationsServiceClient(cc),
		iam:         iamv1.NewInternalIAMServiceClient(cc),
		human:       iamv1.NewInternalHumanSessionServiceClient(cc),
		callTimeout: sessionRevocationsCallTimeout,
	}
}

// ResolveHumanSession спрашивает службу, какая НАША сессия стоит за носителем
// (`InternalHumanSessionService.Resolve`; приёмка Ф3 Р7, Ф3-09).
//
// Три исхода владельца переводятся ЗДЕСЬ, и ни один не сливается:
//   - `OK, found` → сессия составом Ф3-09; момент аутентификации берётся
//     В РАЗРЕШЕНИИ ПРОВОДА (микросекунды) — край сравнивает его с отсечкой
//     включающе, и усечение здесь сделало бы замок Ф3-16 красным by construction;
//   - `OK, !found` → «сессии нет» — один ответ на все причины, БЕЗ ошибки;
//   - `UNIMPLEMENTED` → типизированный признак ErrHumanSessionUnsupported
//     (окно раската): знание о кодах транспорта принадлежит адаптеру, решение —
//     полосе (там это отказ F4d-23, а не проход);
//   - прочее → «спросить не удалось», ошибка без подмены.
func (a *SessionRevocationsAdapter) ResolveHumanSession(
	ctx context.Context, bearer string,
) (middleware.HumanSession, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, a.callTimeout)
	defer cancel()
	resp, err := a.human.Resolve(ctx, &iamv1.ResolveHumanSessionRequest{Bearer: bearer})
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			return middleware.HumanSession{}, false, middleware.ErrHumanSessionUnsupported
		}
		return middleware.HumanSession{}, false, err
	}
	if !resp.GetFound() || resp.GetSession() == nil {
		return middleware.HumanSession{}, false, nil
	}
	s := resp.GetSession()
	return middleware.HumanSession{
		UserID:          s.GetUserId(),
		Email:           s.GetEmail(),
		DisplayName:     s.GetDisplayName(),
		AuthenticatedAt: s.GetAuthenticatedAt().AsTime(),
		ExpiresAt:       s.GetExpiresAt().AsTime(),
		AssuranceLevel:  s.GetAssuranceLevel(),
		EmailVerified:   s.GetEmailVerified(),
	}, true, nil
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
	ctx, cancel := context.WithTimeout(ctx, a.callTimeout)
	defer cancel()
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
	ctx, cancel := context.WithTimeout(ctx, a.callTimeout)
	defer cancel()
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
	ctx, cancel := context.WithTimeout(ctx, a.callTimeout)
	defer cancel()
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
	ctx, cancel := context.WithTimeout(ctx, a.callTimeout)
	defer cancel()
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
