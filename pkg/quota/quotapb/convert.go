// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

// Package quotapb — перевод строк учёта в контракт `kacho.cloud.quota.v1`.
//
// # Почему это отдельный пакет, а не пять одинаковых функций в пяти обработчиках
//
// Перевод несёт РЕШЕНИЕ, а не механику: неопознанная область отображается в
// `SCOPE_UNSPECIFIED`, а не в `DEFAULT`. Пять копий этого решения выглядели бы
// одинаково ровно до первой правки одной из них, и расхождение проявилось бы
// самым тихим способом: арендатор одного домена читал бы «величина платформенная»
// там, где домен просто не смог разобрать строку.
//
// Тела здесь ровно столько, сколько нужно, чтобы у решения было одно место.
package quotapb

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	quotav1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/quota/v1"

	"github.com/PRO-Robotech/kacho/pkg/quota/quotaread"
)

// StatesFunc — чтение квот проекта у владельца.
//
// Функция, а не интерфейс: у каждого владельца полоса своя (она знает, чем он
// добирается до своих строк), но нужен от неё ровно один глагол, и интерфейс с
// одним методом здесь означал бы объявление ради объявления.
type StatesFunc func(ctx context.Context, projectID string) ([]quotaread.State, error)

// ListQuotas — тело обработчика `QuotaService.List`, одно на всех владельцев.
//
// Владельцу остаётся упаковать результат в СВОЙ тип ответа: сообщения ответа у
// доменов разные (`computev1.ListQuotasResponse` и соседи), а всё, что до них, —
// одно и то же.
//
// ОБЯЗАТЕЛЬНОСТЬ ПРОВЕРЯЕТСЯ ПЕРВЫМ СТЕЙТМЕНТОМ И СВОИМ ОТКАЗОМ. Оставленная
// краю, пустая строка дошла бы до извлечения области действия, не разрешилась бы
// там и вернулась отказом в правах — то есть утверждением о доступе на вопрос,
// который никогда не был корректным (`api-conventions.md`:
// `corevalidate.ResourceID` пустую строку ПРОПУСКАЕТ, required — отдельная
// ответственность вызывающего).
//
// Проверка стоит ЗДЕСЬ, а не в каждом из пяти обработчиков, ровно потому, что
// это решение, а не механика: пять копий отличались бы текстом отказа, и
// арендатор получал бы на один и тот же неверный ввод разные ответы в разных
// доменах.
func ListQuotas(ctx context.Context, projectID string, states StatesFunc) ([]*quotav1.Quota, error) {
	if projectID == "" {
		return nil, status.Error(codes.InvalidArgument, "project_id: required")
	}
	if states == nil {
		// Полоса не провязана. Пустой ответ здесь был бы утверждением «квот нет»,
		// которого контракт не делает; отказ называет предмет.
		return nil, status.Error(codes.Internal, "quota read band is not wired")
	}
	st, err := states(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return Quotas(st), nil
}

// Quotas переводит строки учёта в сообщения контракта, сохраняя порядок.
//
// Возвращается непустой срез там, где непуст вход, и НЕ nil там, где вход пуст:
// разница видна на проводе (`[]` против отсутствия поля), а обещание контракта
// про пустой массив сделано в одном месте — в самом контракте, и нарушать его
// переводом было бы нечестно вдвойне.
func Quotas(states []quotaread.State) []*quotav1.Quota {
	out := make([]*quotav1.Quota, 0, len(states))
	for _, st := range states {
		out = append(out, &quotav1.Quota{
			Kind:          st.Kind,
			Limit:         st.Limit,
			Used:          st.Used,
			SourceScope:   Scope(st.SourceScope),
			SourceScopeId: st.SourceScopeID,
			CarrierType:   st.CarrierType,
			CarrierId:     st.CarrierID,
		})
	}
	return out
}

// Scope переводит область из строки хранения в перечисление контракта.
//
// Неопознанное значение отображается в `SCOPE_UNSPECIFIED`, а НЕ в `DEFAULT`:
// умолчание — это утверждение «величина платформенная», и делать его на строке,
// которую мы не смогли прочитать, значит выдавать незнание за факт. Пустой
// перечислитель виден арендатору как «источник не назван» и отличим от всех трёх
// законных областей.
//
// ПЕРЕЧИСЛЕНИЕ — СВОЁ, а не заимствованное у службы доступа (задача #2117,
// стадия S2). Имена и номера прежние, поэтому ни провод, ни JSON не изменились
// ни на байт; изменилось направление зависимости: платформа больше не собирается
// из контракта продукта, который из неё выносится.
//
// ЭТО ЕДИНСТВЕННОЕ МЕСТО ПЕРЕВОДА, и оно тотально по типу. Отсюда следует то,
// ради чего прежде заимствовали чужое перечисление: область, которой этот
// словарь не знает, не может быть переведена молча — она попадает в
// `SCOPE_UNSPECIFIED` здесь, в одной функции, а не расходится по пяти доменам.
func Scope(s string) quotav1.Quota_Scope {
	switch s {
	case "DEFAULT":
		return quotav1.Quota_DEFAULT
	case "ACCOUNT":
		return quotav1.Quota_ACCOUNT
	case "PROJECT":
		return quotav1.Quota_PROJECT
	default:
		return quotav1.Quota_SCOPE_UNSPECIFIED
	}
}
