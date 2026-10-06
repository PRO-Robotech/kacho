// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// address_refusal.go — отказ адреса почты на крае (приёмка F6b, S1:
// kacho#2900; решение владельца 2026-09-27 «вход дальше экрана регистрации или
// логина доступен только после подтверждения почты»).
//
// # Одно значение на обе поверхности и на оба входа
//
// Отказ произносится в двух местах края: рубежом полосы сессии (Р4) и
// решением по каталогу прав, когда служба ответила «нет» с причиной
// `email_not_verified` (Р3а). Значение у обоих ОДНО — и это значение службы, а не
// своё (Р3): причина принадлежит службе, она произносит тот же отказ на своих
// глаголах и слушателях, а консоль решает по `reason`. Второе написание одной
// причины стало бы вторым решением.
//
// Домен — домен отказов службы (`<служба>.<суффикс продукта>` у её
// `refusaldomain`), а не домен отказа края по каталогу прав
// (`kaname.cloud.iam.v1`, permission_denied_response.go): отказ по каталогу —
// решение края, отказ адреса — решение службы, которое край произносит раньше
// неё.
//
// # Что отказ не делает
//
// Не зовёт на аутентификацию (`WWW-Authenticate` нет: человек аутентифицирован,
// ему не хватает подтверждения) и носитель не трогает (`Set-Cookie` нет: сессия
// нужна экрану подтверждения). Не зависит ни от пути, ни от объекта, ни от
// формата идентификатора: стоит раньше решения по каталогу и раньше скрытия
// существования, поэтому оракулом того, чего не читал, не становится.
package middleware

import (
	"net/http"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Значение отказа адреса (приёмка F6b, Р3) — одно у службы и края. Текст
// называет следующий шаг — подтверждение кодом из письма (решение R36 п. 2;
// тикет текста — kaname#526, `api-conventions.md` §«Error-format»): прежний
// текст называл состояние, и клиент API искал шаг по документации. Сторона края
// введена первой (R36: «край без ожидания службы»); служба переходит на тот же
// текст дословно своей стороной kaname#526 — до её посадки служба на своих
// глаголах пишет прежний текст, а `reason` (по нему решает консоль) у обеих один.
const (
	addressRefusalText   = "email address is not verified: confirm it with the code from the letter (POST /iam/v1/auth/verify-email/confirm)"
	addressRefusalReason = "EMAIL_NOT_VERIFIED"
	addressRefusalDomain = "iam.kaname.cloud"
	// addressRefusalDenyReason — причина в ответе решения службы
	// (`deny_reasons`, Р4а службы). Узнаётся ТОЧНЫМ значением (Р3а).
	addressRefusalDenyReason = "email_not_verified"
)

// addressRefusalBody — тело отказа на HTTP в форме `google.rpc.Status`, как его
// печатает служба: порядок ключей и отсутствие `metadata` — часть значения.
// Собрано из тех же констант, что форма gRPC, а не выписано вторым литералом.
const addressRefusalBody = `{"code":7,"message":"` + addressRefusalText +
	`","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"` + addressRefusalReason +
	`","domain":"` + addressRefusalDomain + `"}]}`

// writeHTTPAddressRefusal — отказ адреса на REST-поверхности: 403, тело
// `addressRefusalBody`, заголовки как у службы. Единственный производитель
// ответа на обоих входах HTTP (рубеж полосы сессии и отказ решения).
func writeHTTPAddressRefusal(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(addressRefusalBody))
}

// addressRefusalStatus — отказ адреса на нативной поверхности:
// PERMISSION_DENIED, тот же текст, `ErrorInfo` с той же причиной и доменом, без
// `metadata`.
func addressRefusalStatus() *status.Status {
	st := status.New(codes.PermissionDenied, addressRefusalText)
	withDetails, err := st.WithDetails(&errdetails.ErrorInfo{Reason: addressRefusalReason, Domain: addressRefusalDomain})
	if err != nil {
		// Сборка деталей не удалась — отказ остаётся отказом тем же кодом и
		// текстом: пропуск на неудаче сборки был бы мягким проходом.
		return st
	}
	return withDetails
}

// denyReasonsNameUnverifiedAddress — назвала ли служба в ответе решения причину
// адреса (Р3а). Совпадение точное: любая иная причина, в том числе похожая
// написанием, — прежний отказ по каталогу, побайтово как до этой под-фазы.
func denyReasonsNameUnverifiedAddress(reasons []string) bool {
	for _, r := range reasons {
		if r == addressRefusalDenyReason {
			return true
		}
	}
	return false
}

// openBeforeAddressConfirmation — доходит ли сессия с неподтверждённым адресом
// почты до этого запроса (приёмка F6b, Р5). Перечень прохода — ОДНО
// объявление в двух частях, и второго здесь не заводится:
//
//   - пути без записи каталога, не являющиеся записями объявления, — тот же
//     перечень `isPublicHTTPPath` («кто я», выход, пробы здоровья);
//   - записи объявления (глаголы формы и координаты церемонии), объявившие
//     «доступна до подтверждения» (`login_lane_paths.go`); нулевое значение —
//     отказ.
//
// Совпадение ТОЧНОЕ и по пути, и по его записи на проводе: путь, записанный
// иначе (экранированная косая внутри сегмента и подобное), разрешённым не
// является, даже если после разбора совпал с разрешённым, — маршрутизатор за
// краем судит запись, а не разобранный путь.
func openBeforeAddressConfirmation(r *http.Request) bool {
	path := r.URL.Path
	if r.URL.EscapedPath() != path {
		return false
	}
	if IsLoginLanePath(path) {
		return loginLaneOpenBeforeAddressConfirmation(path)
	}
	return isPublicHTTPPath(path)
}
