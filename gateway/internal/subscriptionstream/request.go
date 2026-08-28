// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionstream

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"google.golang.org/protobuf/types/known/anypb"

	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
)

// Path — адрес единственной проекции потока. Объявлен здесь, потому что его
// знают трое: композиционный корень (монтаж), полоса прав (резолв имени метода)
// и гейт единственности. Три копии строки разошлись бы молча.
const Path = "/subscription/v1/events"

// MethodFQN — полное имя глагола, который эта ручка исполняет от имени
// вызывающего. Полоса прав спрашивает по нему запись каталога: право одно, и
// второй записи под выдуманным именем не заводится.
const MethodFQN = "kacho.cloud.subscription.InternalSubscriptionService/Subscribe"

// headerLastEventID — единственный носитель позиции. Заголовок стандартный: его
// шлёт САМ браузер при переподключении, и ради этого свойства выбран SSE.
const headerLastEventID = "Last-Event-ID"

// Имена параметров запроса. Регистр — camelCase, как у всякого поля REST-тела
// этого продукта: одна форма имени на обе поверхности.
const (
	paramOwner     = "owner"
	paramKinds     = "kinds"
	paramProjectID = "projectId"
	paramIDs       = "ids"
	paramStart     = "start"
)

// Значения параметра начала. Их РОВНО столько, сколько ветвей якоря в
// контракте, и называются они теми же словами.
const (
	startBeginning  = "beginning"
	startCurrentEnd = "currentEnd"
)

// refusal — отказ, наступивший ДО первого сообщения владельца, то есть до того,
// как ответ стал потоком. Несёт код ответа и текст: после отправки заголовков
// ни того ни другого уже не сказать.
type refusal struct {
	status int
	code   int32
	msg    string
	// details — машинные подробности отказа владельца (`google.rpc.Status.details`).
	// По ним клиент ключуется, а не разбирает прозу: отказ на утраченной позиции
	// называет возобновимую позицию именно здесь.
	details []*anypb.Any
}

func (r refusal) Error() string { return r.msg }

// invalidArgument — отказ на форме запроса. Код 3 (`INVALID_ARGUMENT`) → 400 по
// таблице края; поле и значение называются, потому что «invalid request» не
// говорит вызывающему, что править.
func invalidArgument(format string, args ...any) refusal {
	return refusal{status: http.StatusBadRequest, code: 3, msg: fmt.Sprintf(format, args...)}
}

// parsed — принятый запрос: кому дозваниваться и что спрашивать.
type parsed struct {
	owner string
	req   *subscriptionv1.SubscriptionRequest
}

// parseRequest судит вход и собирает запрос подписки.
//
// # Почему отказ, а не пустой поток
//
// То же основание, что у владельца: пустой поток есть УТВЕРЖДЕНИЕ «событий
// нет», и делать его про вход, которого край не понял, он не вправе. Каждый
// негодный вход отвергается ДО открытия потока.
//
// # Что край НЕ проверяет
//
// Виды и идентификаторы. Словарь видов принадлежит ВЛАДЕЛЬЦУ и им закрыт; форма
// идентификатора — тоже его предмет. Проверь их край — он завёл бы вторую копию
// чужого словаря, и она разошлась бы с оригиналом в день, когда владелец добавит
// вид. Отказ по ним приходит от владельца и доезжает сюда кодом.
func parseRequest(r *http.Request, owners Owners) (parsed, error) {
	q := r.URL.Query()

	if unknown := unknownParams(q); len(unknown) > 0 {
		// НЕИЗВЕСТНЫЙ ПАРАМЕТР ОТВЕРГАЕТСЯ, а не принимается молча.
		//
		// Молча принятый параметр — обещание возможности, которой нет: клиент
		// получает успех и уверен, что его отбор применён. Самый вероятный
		// случай здесь даже не выдуманное имя, а ОПЕЧАТКА РЕГИСТРА (`projectid`
		// вместо `projectId` — camelCase есть конвенция этого продукта): она
		// давала бы поток, НЕ СУЖЕННЫЙ ПО ПРОЕКТУ, вместо отказа. То есть ось,
		// по которой принимается решение о показе, снималась бы опечаткой.
		return parsed{}, invalidArgument("unknown query parameter(s): %s", strings.Join(unknown, ", "))
	}

	owner := q.Get(paramOwner)
	if owner == "" {
		return parsed{}, invalidArgument(
			"%s: required — поток открывается к ОДНОМУ владельцу журнала, и назвать его обязан вызывающий: "+
				"у владельцев независимые счётчики, поэтому одной позицией их не адресовать", paramOwner)
	}
	if !owners.Has(owner) {
		// Значение называется, а перечень известных — нет: он есть свойство
		// посадки, и оглашать его отказом значит отвечать на вопрос, которого
		// вызывающий не задавал.
		return parsed{}, invalidArgument("%s: unknown owner '%s'", paramOwner, owner)
	}

	req := &subscriptionv1.SubscriptionRequest{
		Kinds:     nonEmptyValues(q[paramKinds]),
		ProjectId: q.Get(paramProjectID),
		Ids:       nonEmptyValues(q[paramIDs]),
	}

	// Позиция сильнее якоря, и это ОБЪЯВЛЕНО, а не сделано молча: браузер шлёт
	// заголовок на тот же адрес, в котором стоит исходный якорь, и обратный
	// порядок начинал бы поток заново при каждом переподключении — то есть
	// возобновление не работало бы никогда.
	//
	// Молчаливым это не является и с той стороны, что видит клиент: край
	// отдаёт служебное сообщение открытия владельца как есть, а оно несёт и
	// позицию, и перечень честно отобранных осей.
	if position := strings.TrimSpace(r.Header.Get(headerLastEventID)); position != "" {
		req.Start = &subscriptionv1.SubscriptionRequest_Position{Position: position}
		return parsed{owner: owner, req: req}, nil
	}

	switch start := q.Get(paramStart); start {
	case "":
		// Ветвь не выбрана — умолчание контракта: «с текущего конца». Оно живёт
		// у НЕЗАДАННОЙ ветви, а не у незаданного значения внутри выбранной.
	case startBeginning:
		req.Start = &subscriptionv1.SubscriptionRequest_Anchor{
			Anchor: subscriptionv1.SubscriptionAnchor_BEGINNING,
		}
	case startCurrentEnd:
		req.Start = &subscriptionv1.SubscriptionRequest_Anchor{
			Anchor: subscriptionv1.SubscriptionAnchor_CURRENT_END,
		}
	default:
		return parsed{}, invalidArgument("%s: unknown value '%s', expected '%s' or '%s'",
			paramStart, start, startBeginning, startCurrentEnd)
	}

	return parsed{owner: owner, req: req}, nil
}

// knownParams — ЗАКРЫТЫЙ набор имён параметров этой ручки.
//
// Набор, а не проверка на месте: перечень обязан быть один, и читатель отказа
// обязан видеть его целиком рядом с разбором.
var knownParams = map[string]bool{
	paramOwner:     true,
	paramKinds:     true,
	paramProjectID: true,
	paramIDs:       true,
	paramStart:     true,
}

// unknownParams возвращает имена параметров, которых ручка не знает, в
// устойчивом порядке.
//
// Порядок закреплён не ради красоты: отказ есть часть контракта, и текст,
// меняющийся от обхода карты, нельзя ни утверждать пробой, ни сверять глазами.
func unknownParams(q url.Values) []string {
	unknown := make([]string, 0, 2)
	for name := range q {
		if !knownParams[name] {
			unknown = append(unknown, name)
		}
	}
	sort.Strings(unknown)
	return unknown
}

// nonEmptyValues отбрасывает пустые повторы параметра.
//
// Пустая строка как ось означала бы сужение, которому не отвечает ни один
// предмет, — то есть молча замолкший поток. Владелец её и так отвергает; здесь
// она снимается потому, что `?kinds=` без значения — обычная форма записи
// «ничем не сужаю», а не заявление о пустом виде.
func nonEmptyValues(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Owners — ЗАКРЫТЫЙ словарь владельцев журналов.
//
// Закрытость несущая: открытый параметр `owner` отдал бы выбор адреса дозвона
// вызывающему, то есть превратил бы ручку в способ заставить край постучаться
// куда угодно.
type Owners map[string]OwnerConn

// Has отвечает, объявлен ли владелец.
func (o Owners) Has(name string) bool {
	_, ok := o[name]
	return ok
}

// Names возвращает объявленных владельцев в устойчивом порядке — для
// самоотчёта процесса при старте.
func (o Owners) Names() []string {
	names := make([]string, 0, len(o))
	for name := range o {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// asRefusal приводит ошибку разбора к отказу.
//
// Разбор возвращает `refusal` значением, поэтому распознавание идёт по типу, а
// не по тексту: сравнение сообщений сделало бы форму отказа частью его
// классификации, и первая же правка текста молча сменила бы код ответа.
func asRefusal(err error) refusal {
	var r refusal
	if errors.As(err, &r) {
		return r
	}
	return refusal{status: http.StatusBadRequest, code: 3, msg: err.Error()}
}
