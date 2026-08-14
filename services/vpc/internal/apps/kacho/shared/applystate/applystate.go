// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package applystate — заполнитель публичного поля состояния применения.
//
// # Предмет
//
// Шов датаплейна принимает от исполнителя подтверждение, а серверная проекция
// сводит его к паре «применено ли намерение ТЕКУЩЕЙ ревизии» и «класс причины
// отказа». Этот пакет — единственное место, где та пара превращается в поле
// контракта: чтения ресурсов зовут его, и больше никто.
//
// # Почему партия, а не вызов на строку
//
// Стоимость страницы принадлежит ЗАПРОСУ, а не популяции проекта: список из
// пятидесяти ресурсов не вправе становиться пятьюдесятью обращениями к базе.
// Поэтому у заполнителя две операции, и обе делают РОВНО ОДНО обращение —
// [Filler.One] для единичного чтения и [Filler.Page] для страницы.
//
// # Почему отсутствие представимо, а не сведено к «не применено»
//
// Объект, о котором проекция ничего не вернула, получает НЕЗАПОЛНЕННОЕ поле, а
// не «не применено». Заполнив здесь правдоподобное, платформа сообщила бы
// арендатору неправду о его ресурсе — и неправду незаметную: «не применено» на
// сломанной проекции читается ровно так же, как честное «исполнитель ещё не
// дошёл». Отсутствие ключа означает ровно одно: платформа не делает утверждения
// об этом объекте.
//
// Законный случай ровно один — удаление в полёте: чтение увидело ресурс,
// параллельное удаление зафиксировалось, намерение стало снятым, а снятые
// строки проекция намеренно исключает. Ронять на этом чтение значило бы
// отдавать `INTERNAL` на штатной гонке. Всё остальное, что сюда попадёт, —
// дефект, и потому у случая есть СЧЁТЧИК: установившийся ненулевой темп таких
// попаданий, не объяснимый удалениями, есть сигнал; «ноль за всю жизнь» обязано
// быть так же заметно, как ноль доставленных строк очереди.
//
// # Чего этот пакет НЕ делает
//
// Не решает, какая поверхность поле заполняет. Это решение принимает вызывающий,
// и оно записано у него: `Operation.response`, поток намерения исполнителя,
// внутренние проекции и ответы привязки интерфейса заполнителя не зовут вовсе —
// у каждой из этих поверхностей своё основание, названное в комментарии поля
// контракта.
package applystate

import (
	"context"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/dataplane"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
)

// Reader — порт чтения публичной проекции подтверждений.
//
// Объявлен здесь, у потребителя, а реализован адаптером
// (`internal/repo/dataplane`). Форма — та же, что у порта use-case-слоя шва:
// партия идентификаторов на входе, карта на выходе, отсутствие ключа = «сказать
// нечего».
type Reader interface {
	PublicApplyStates(ctx context.Context, resourceIDs []string) (map[string]dataplane.PublicApplyState, error)
}

// Filler переводит проекцию подтверждения в поле контракта.
//
// Нулевое значение (`nil`) — законный вход и означает «этой сборке проекция не
// провязана»: методы отдают незаполненное поле и не обращаются к базе. Так
// устроено ради проб, собирающих один use-case без композиционного корня;
// провязку боевой сборки держит гейт по дереву, а не готовность этого типа
// падать (`internal/dto/toproto/dataplane_apply_pairing_test.go` и
// `apply_state_wiring_test.go` рядом с ним).
type Filler struct {
	read Reader
	// onMissing зовётся один раз на КАЖДЫЙ идентификатор, о котором проекция
	// ничего не сказала. Функция, а не счётчик прямо здесь: пакет живёт на
	// use-case-слое и не вправе импортировать адаптер наблюдаемости.
	onMissing func()
}

// NewFiller собирает заполнитель.
//
// onMissing может быть nil — тогда счётчик не ведётся; это законно для проб и
// НЕ законно для боевой сборки, где отсутствие строки намерения обязано быть
// наблюдаемым.
func NewFiller(r Reader, onMissing func()) *Filler {
	return &Filler{read: r, onMissing: onMissing}
}

// One — состояние применения ОДНОГО ресурса.
//
// Возвращает nil, если проекция об объекте ничего не сказала: на публичном крае
// это `"applyState": null`, то есть «утверждения нет», а не «не применено».
func (f *Filler) One(ctx context.Context, id string) (*vpcv1.ApplyState, error) {
	if f == nil || f.read == nil || id == "" {
		return nil, nil
	}
	states, err := f.read.PublicApplyStates(ctx, []string{id})
	if err != nil {
		return nil, mapReadErr(err)
	}
	st, ok := states[id]
	if !ok {
		f.countMissing()
		return nil, nil
	}
	return toProto(st), nil
}

// Page — состояние применения СТРАНИЦЫ, одним обращением к базе.
//
// Карта возвращается всегда непустой ссылкой (возможно, пустой картой), чтобы
// вызывающему не приходилось различать nil и «ничего не нашлось»: и то и другое
// означает одно — по этим идентификаторам утверждения нет.
func (f *Filler) Page(ctx context.Context, ids []string) (map[string]*vpcv1.ApplyState, error) {
	out := make(map[string]*vpcv1.ApplyState, len(ids))
	if f == nil || f.read == nil || len(ids) == 0 {
		return out, nil
	}
	states, err := f.read.PublicApplyStates(ctx, ids)
	if err != nil {
		return nil, mapReadErr(err)
	}
	for _, id := range ids {
		st, ok := states[id]
		if !ok {
			f.countMissing()
			continue
		}
		out[id] = toProto(st)
	}
	return out, nil
}

// FillPage заполняет поле состояния у ЦЕЛОЙ страницы сообщений, одним
// обращением к проекции.
//
// Обобщённая функция, а не семь копий трёх операторов: копии разошлись бы —
// одна забыла бы проверить отказ, другая спросила бы по строке. Тип сообщения
// при этом остаётся у вызывающего, поэтому «взял идентификатор» и «положил
// состояние» он называет сам: сеттеров у сгенерированных типов нет, и
// подставлять вместо них отражение значило бы менять проверяемое компилятором
// на проверяемое в рантайме.
func FillPage[T any](
	ctx context.Context,
	f *Filler,
	items []T,
	id func(T) string,
	set func(T, *vpcv1.ApplyState),
) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]string, 0, len(items))
	for _, it := range items {
		ids = append(ids, id(it))
	}
	states, err := f.Page(ctx, ids)
	if err != nil {
		return err
	}
	for _, it := range items {
		set(it, states[id(it)])
	}
	return nil
}

// mapReadErr переводит отказ проекции в отказ RPC.
//
// Отказ ЦЕЛИКОМ, а не пропуск одной записи и не «не применено»: строка негодной
// формы может появиться ровно одним способом — словарь базы разошёлся с
// контрактом, — и свернув расхождение в «не применено», проекция сообщила бы
// арендатору правдоподобную неправду и пережила бы собственный предмет.
//
// Текст наружу — ФИКСИРОВАННЫЙ и непрозрачный (это делает общий переводчик
// отказов хранилища): ни текста драйвера, ни имени таблицы, ни имени колонки.
// Диагноз остаётся в журнале процесса, а не в теле ответа арендатору.
func mapReadErr(err error) error {
	return serviceerr.MapRepoErr(err)
}

// countMissing отмечает чтение объекта, о котором проекция ничего не сказала.
func (f *Filler) countMissing() {
	if f.onMissing != nil {
		f.onMissing()
	}
}

// toProto переводит проекцию в поле контракта.
//
// Перевод прямой и БЕЗ корзины «прочее»: класс отказа приезжает сюда уже
// проверенным по закрытому словарю (строка негодной формы роняет чтение целиком
// ещё в проекции), поэтому неизвестному значению здесь взяться неоткуда, а
// ветвь «всё остальное → UNSPECIFIED» сделала бы расхождение словарей
// невидимым — молча превратив названный класс в «класса нет».
func toProto(st dataplane.PublicApplyState) *vpcv1.ApplyState {
	return &vpcv1.ApplyState{
		Applied: st.Applied,
		Reason:  reasonToProto(st.Reason),
	}
}

// reasonToProto — класс отказа в значение перечисления контракта.
//
// Перечень ветвей равен закрытому словарю (`dataplane.KnownFailureReasons`), и
// это утверждается пробой: ветвь, потерянная здесь, превращала бы названный
// класс в «класса нет» — то есть арендатор видел бы «не применено» без причины
// там, где причина названа.
func reasonToProto(r dataplane.FailureReason) vpcv1.ApplyFailureReason {
	switch r {
	case dataplane.ReasonCapacity:
		return vpcv1.ApplyFailureReason_APPLY_FAILURE_REASON_CAPACITY
	case dataplane.ReasonConflict:
		return vpcv1.ApplyFailureReason_APPLY_FAILURE_REASON_CONFLICT
	case dataplane.ReasonUnsupported:
		return vpcv1.ApplyFailureReason_APPLY_FAILURE_REASON_UNSUPPORTED
	case dataplane.ReasonDependencyNotReady:
		return vpcv1.ApplyFailureReason_APPLY_FAILURE_REASON_DEPENDENCY_NOT_READY
	case dataplane.ReasonTransient:
		return vpcv1.ApplyFailureReason_APPLY_FAILURE_REASON_TRANSIENT
	case dataplane.ReasonExecutorInternal:
		return vpcv1.ApplyFailureReason_APPLY_FAILURE_REASON_EXECUTOR_INTERNAL
	default:
		return vpcv1.ApplyFailureReason_APPLY_FAILURE_REASON_UNSPECIFIED
	}
}
