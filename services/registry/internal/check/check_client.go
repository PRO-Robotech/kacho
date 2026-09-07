// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package check — per-RPC authz-гейт для kacho-registry. Оборачивает
// authz-интерсептор из corelib registry-шной PermissionMap и CheckClient поверх
// IAM (InternalIAMService.Check — вердикт выносит сам iam по своим отношениям).
// registry — CONSUMER iam-authz
// (ребро registry→iam Check); интерсептор навешивается на ОБА листенера
// (public :9090 + internal :9091) — internal НЕ освобождён (security.md).
package check

import (
	"context"
	"time"

	"google.golang.org/grpc"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/auth"
	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowiam"
)

// CheckTimeout — per-call deadline на один Check-вызов к iam. Зеркалит corelib
// authz.InterceptorOptions.CheckTimeout default (2s): registry-interceptor
// (internal/check/factory.go) её не конфигурирует, и, что важнее, эта
// interceptor-side защита покрывает только Check, инициированный САМИМ
// authz.Interceptor.Unary() — а не прямые вызовы IAMCheckClient.Check из
// internal/dataplane (per-request data-plane authz на raw request ctx) и
// internal/handler/listauthz.go (ScopeFiltered per-repo fan-out, до 1000 Check на
// ListRepositories), которые обходят interceptor и раньше форвардили сырой inbound
// ctx без собственного дедлайна. Зависший iam пинил горутину навсегда
// (architecture.md "Per-call deadline на КАЖДОМ внешнем вызове"). Таймаут — здесь,
// ВНУТРИ клиента, чтобы одной правкой покрыть оба call-site'а uniformly.
const CheckTimeout = 2 * time.Second

// IAMCheckClient адаптирует kaname.InternalIAMService.Check под authz.CheckClient.
type IAMCheckClient struct {
	cli iamv1.InternalIAMServiceClient
	// narrower — общий сужатель списков поверх ТОГО ЖЕ соединения: `AuthorizeService`
	// зарегистрирован и на внутреннем листенере kaname, поэтому второго дозвона не
	// заводится.
	narrower *listnarrow.Narrower
}

// NewIAMCheckClient строит адаптер поверх conn к internal-листенеру kaname (:9091).
func NewIAMCheckClient(conn grpc.ClientConnInterface) *IAMCheckClient {
	return &IAMCheckClient{
		cli: iamv1.NewInternalIAMServiceClient(conn),
		narrower: listnarrow.New(narrowiam.New(conn), listnarrow.Config{
			// Предикат страницы registry задаётся ЯВНО на каждом вызове (см.
			// CheckMany), поэтому карта здесь несёт лишь умолчание — но несёт, иначе
			// сборка сужателя отвергла бы посадку без предиката.
			Relations: map[string][]string{"": {"v_list"}},
			Timeout:   CheckTimeout,
		}),
	}
}

// Check вызывает InternalIAMService.Check с собственным per-call deadline
// (CheckTimeout) — НЕ полагается на дедлайн вызывающего ctx. Исходящий ctx
// оборачивается auth.PropagateOutgoing, чтобы на стороне iam principal-extract
// видел реального вызывающего. Timeout/iam-error → non-nil error (caller
// fail-closed'ит: data-plane → 503, listauthz → UNAVAILABLE).
func (c *IAMCheckClient) Check(ctx context.Context, subjectID, relation, object string) (bool, error) {
	cctx, cancel := context.WithTimeout(ctx, CheckTimeout)
	defer cancel()
	resp, err := c.cli.Check(auth.PropagateOutgoing(cctx), &iamv1.CheckRequest{
		SubjectId: subjectID,
		Relation:  relation,
		Object:    object,
	})
	if err != nil {
		return false, err
	}
	return resp.GetAllowed(), nil
}

var _ authz.CheckClient = (*IAMCheckClient)(nil)

// CheckMany — тот же вопрос про МНОГО объектов одного типа, партиями ≤
// listnarrow.MaxBatchSize через `AuthorizeService.BatchCheck`, ограниченным веером.
//
// Дверь другая, чем у Check: `InternalIAMService.Check` спрашивает про ОДИН объект и
// пакетной формы не имеет. Прежде страница каталога опрашивалась ею поштучно — до
// тысячи запросов на страницу при веере восемь, то есть 125 последовательных волн
// против двух у соседних сервисов, которые ту же страницу спрашивают партиями.
//
// Механика — общий сужатель списков (`pkg/listnarrow`): партии, ограниченный веер,
// бюджет от бюджета операции, окно ПОЛОЖИТЕЛЬНЫХ вердиктов и fail-closed. Своей
// копии здесь не заводится — именно четыре таких копии и сводились в один дом.
//
// Личность вызывающего к этому моменту уже установлена гейтом (`callerSubject`), и
// его собственный код отказа остаётся за ним: субъект передаётся сюда ЗНАЧЕНИЕМ.
func (c *IAMCheckClient) CheckMany(
	ctx context.Context, subject, relation, objectType string, objectIDs []string,
) ([]string, error) {
	if len(objectIDs) == 0 {
		return nil, nil
	}
	cctx, cancel := context.WithTimeout(ctx, CheckTimeout)
	defer cancel()
	return c.narrower.Visible(auth.PropagateOutgoing(cctx), subject, objectType,
		catalogPageAction, relation, objectIDs)
}

// catalogPageAction — аудит-строка вопроса о СТРАНИЦЕ каталога. Она едет в каждый
// вопрос для аудита и трассировки; решение принимает явное отношение, а не она.
const catalogPageAction = "registry.repositories.list"

// Narrower отдаёт сужатель, через который этот клиент задаёт ПАКЕТНЫЙ вопрос о
// странице (`CheckMany` → `listnarrow.Visible`).
//
// Существует ради одного читателя — дескриптора процесса
// (`servicecontract.Spec.Narrowers`): носитель сверяет ПРОВОДКУ сужателя с
// каталогом прав в обе стороны (О3/О4), и предъявить он может только тот
// сужатель, который реально исполняется на сужаемых методах. Собрать здесь
// второй значило бы завести две реализации одного предмета — ровно то, ради
// устранения чего сужатель и сведён в один дом.
//
// nil-клиент отдаёт nil: «сужателя нет» обязано быть отличимо от «сужатель есть
// и молчит», и носитель читает это как отсутствие проводки (О3).
func (c *IAMCheckClient) Narrower() *listnarrow.Narrower {
	if c == nil {
		return nil
	}
	return c.narrower
}
