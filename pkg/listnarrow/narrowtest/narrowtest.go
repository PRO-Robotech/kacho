// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package narrowtest — дублёры сужателя списков для проб сервисов.
//
// Дублёр здесь — НАСТОЯЩИЙ `listnarrow.Narrower` поверх подставной приёмной стороны,
// а не собственная реализация порта. Разница не в удобстве: подставной сужатель со
// своим телом молча принимает то, на чём настоящий отказывает, и делает невидимым
// именно тот дефект, ради которого его подставляют. Здесь подменяется только сосед,
// а все ветки — личность, посадка, аварийный режим, партии, окно вердиктов —
// исполняются те же, что в боевом коде.
package narrowtest

import (
	"context"

	"google.golang.org/grpc"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow"
	"github.com/PRO-Robotech/kacho/pkg/operations"
)

// Peer — подставная приёмная сторона: отвечает вердиктом на каждый вопрос, в порядке
// вопросов (контракт `AuthorizeService.BatchCheck`).
type Peer struct {
	// Allow — идентификаторы, которые видимы. nil при AllowAll=false → не видно ничего.
	Allow map[string]bool
	// AllowAll — видно всё, что спросили.
	AllowAll bool
	// Err — если задана, возвращается вместо ответа (отказ соседа).
	Err error

	// Calls / Checks — сколько запросов и сколько вопросов задано (перепись).
	Calls, Checks int

	// Subject / ResourceType / Action / IDs / Relations — С ЧЕМ сосед был спрошен.
	// Записываются, чтобы проба могла утверждать не только исход, но и что вопрос
	// задан о СТРАНИЦЕ и тем отношением, которым гейтится чтение, — а не о
	// вселенной и не ярусом.
	Subject, ResourceType, Action string
	IDs                           []string
	Relations                     []string
}

// BatchCheck — см. listnarrow.AuthorizeClient.
func (p *Peer) BatchCheck(_ context.Context, in *iamv1.BatchAuthorizeCheckRequest,
	_ ...grpc.CallOption) (*iamv1.BatchAuthorizeCheckResponse, error) {
	p.Calls++
	p.Checks += len(in.GetChecks())
	for _, c := range in.GetChecks() {
		p.Subject = c.GetSubject()
		p.ResourceType = c.GetResource().GetType()
		p.Action = c.GetAction()
		p.IDs = append(p.IDs, c.GetResource().GetId())
		if len(p.Relations) == 0 || p.Relations[len(p.Relations)-1] != c.GetRequiredRelation() {
			p.Relations = append(p.Relations, c.GetRequiredRelation())
		}
	}
	if p.Err != nil {
		return nil, p.Err
	}
	out := make([]*iamv1.AuthorizeCheckResponse, 0, len(in.GetChecks()))
	for _, c := range in.GetChecks() {
		out = append(out, &iamv1.AuthorizeCheckResponse{
			Allowed: p.AllowAll || p.Allow[c.GetResource().GetId()],
		})
	}
	return &iamv1.BatchAuthorizeCheckResponse{Responses: out}, nil
}

// relations — предикат дублёра: одно отношение чтения на все типы. Проба, которой
// нужен другой предикат, собирает сужатель сама через listnarrow.New.
func relations() map[string][]string { return map[string][]string{"": {"v_get"}} }

// New — сужатель поверх заданного соседа.
func New(peer listnarrow.AuthorizeClient) *listnarrow.Narrower {
	return listnarrow.New(peer, listnarrow.Config{Relations: relations()})
}

// AllowingAll — сужатель, у которого сосед разрешает всё. Нужен там, где проба
// утверждает НЕ видимость, а что-то другое (порядок, формат, пагинацию), и «пусто»
// зеленело бы по неверной причине.
func AllowingAll() *listnarrow.Narrower { return New(&Peer{AllowAll: true}) }

// Allowing — сужатель, у которого сосед разрешает названные идентификаторы.
func Allowing(ids ...string) *listnarrow.Narrower {
	allow := make(map[string]bool, len(ids))
	for _, id := range ids {
		allow[id] = true
	}
	return New(&Peer{Allow: allow})
}

// DenyingAll — сужатель, у которого сосед не разрешает ничего. Это ОТВЕТ МОДЕЛИ
// «нет», а не отсутствие модели: страница возвращается пустой, без ошибки.
func DenyingAll() *listnarrow.Narrower { return New(&Peer{}) }

// Failing — сужатель, у которого сосед отказывает. Fail-closed: ошибка доходит до
// вызывающего, страница не отдаётся.
func Failing(err error) *listnarrow.Narrower { return New(&Peer{Err: err}) }

// Unwired — сужатель, которому НЕ С КЕМ говорить: модель на этой посадке не
// провязана. Отказывает, а не пропускает.
func Unwired() *listnarrow.Narrower {
	return listnarrow.New(nil, listnarrow.Config{Relations: relations()})
}

// Breakglass — сужатель в аварийном режиме: пропускает страницу целиком и СЧИТАЕТ
// каждое срабатывание (listnarrow.Narrower.Counts).
func Breakglass() *listnarrow.Narrower {
	return listnarrow.New(nil, listnarrow.Config{Relations: relations(), Breakglass: true})
}

// Caller — контекст с НАЗВАННЫМ тенантным вызывающим. Нужен всюду, где проба
// утверждает не личность, а что-то другое: без него любой список отвечает отказом
// по личности, и утверждение зеленело бы по неверной причине.
func Caller() context.Context { return CallerAs("user", "usr_alice") }

// CallerAs — контекст с названным вызывающим заданного типа и идентификатора.
func CallerAs(principalType, id string) context.Context {
	return operations.WithPrincipal(context.Background(),
		operations.Principal{Type: principalType, ID: id})
}

// Recording — сужатель ВМЕСТЕ с его соседом, чтобы проба могла утверждать не только
// исход, но и С ЧЕМ был задан вопрос: о субъекте, о типе, о строках СТРАНИЦЫ и тем
// отношением, которым гейтится чтение. Без этого «страница сузилась» неотличимо от
// «сузилась по другому вопросу».
func Recording(allow ...string) (*listnarrow.Narrower, *Peer) {
	set := make(map[string]bool, len(allow))
	for _, id := range allow {
		set[id] = true
	}
	peer := &Peer{Allow: set}
	return New(peer), peer
}
