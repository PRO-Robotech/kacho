// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package handler — unit-тесты handler-level listauthz для RegistryService.List
// (ScopeFiltered collection): interceptor пропускает per-RPC Check (single-object
// Check на пустом collection-id → «empty object id» → 403), авторизация — row-filter
// В ХЕНДЛЕРЕ по registry_registry v_list. non-member → 200+empty, member → свои,
// iam-error → UNAVAILABLE (fail-closed), breakglass → все. REG-06.
package handler

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	registryv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/registry/v1"
	registry "github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/api/registry"
	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
	regerrors "github.com/PRO-Robotech/kacho/services/registry/internal/errors"
)

// batchRecorder — приёмная сторона, считающая ЗАПРОСЫ и ВОПРОСЫ раздельно.
//
// Раздельно — потому что предмет пробы именно в их отношении: поштучный опрос давал
// столько же запросов, сколько вопросов, и страница каталога стоила до тысячи
// обращений при веере восемь. Пакетный вопрос оставляет вопросы теми же и сокращает
// запросы до ceil(n/партия).
type batchRecorder struct {
	requests  int
	questions int
	maxBatch  int
	allow     map[string]bool
	allowAll  bool
}

func (b *batchRecorder) Check(_ context.Context, _, _, object string) (bool, error) {
	b.requests++
	b.questions++
	return b.allowAll || b.allow[object], nil
}

func (b *batchRecorder) CheckMany(
	_ context.Context, _, _, objectType string, objectIDs []string,
) ([]string, error) {
	b.requests++
	b.questions += len(objectIDs)
	if len(objectIDs) > b.maxBatch {
		b.maxBatch = len(objectIDs)
	}
	out := make([]string, 0, len(objectIDs))
	for _, id := range objectIDs {
		if b.allowAll || b.allow[domain.FGAObjectRef(objectType, id)] {
			out = append(out, id)
		}
	}
	return out, nil
}

// REG-06 стоимость — страница спрашивается ПАРТИЯМИ, а не поштучно.
//
// Прежде здесь стояло утверждение об одновременности поштучных вопросов: реализация
// фанила Check по одному, и проба ловила последовательность зависанием на барьере.
// Одновременность поштучных вопросов больше не является свойством, которое надо
// доказывать, — вопросов-запросов стало на два порядка меньше, и доказывать надо
// именно это. Старая проба не «устарела по форме»: она утверждала механизм, которого
// в коде больше нет, и осталась бы зелёной ровно до тех пор, пока кто-нибудь не
// вернул бы поштучный опрос обратно.
func TestRepoAuthz_REG06_FilterRegistries_AsksInBatchesNotOneByOne(t *testing.T) {
	const rows = 250

	az := &batchRecorder{allowAll: true}
	regs := make([]*domain.Registry, 0, rows)
	for i := 0; i < rows; i++ {
		regs = append(regs, &domain.Registry{
			ID: fmt.Sprintf("reg%017d", i), ProjectID: "prj-P",
			Name: fmt.Sprintf("team-%03d", i), Status: domain.RegistryStatusActive,
		})
	}

	got, err := newRepoAuthz(az).filterRegistries(carolCtx(), regs)
	require.NoError(t, err)
	require.Len(t, got, rows, "все реестры разрешены → все видны")

	require.Equal(t, rows, az.questions, "вопросов ровно по строке страницы")
	require.Equal(t, 1, az.requests,
		"страница обязана уходить одним обращением к порту, а не по одному на строку")
	require.Equal(t, rows, az.maxBatch, "и обращение обязано нести всю страницу разом")
}

// REG-06 order — row-filter сохраняет входной порядок реестров (детерминизм после
// параллельного fan-out): allow всех → выход в том же порядке, что вход.
func TestRepoAuthz_REG06_FilterRegistries_PreservesOrder(t *testing.T) {
	az := &recordingAuthorizer{allow: map[string]bool{
		registryObjectRef(regA): true,
		registryObjectRef(regB): true,
	}}
	regs := []*domain.Registry{
		{ID: regA, ProjectID: "prj-P", Name: "team-a", Status: domain.RegistryStatusActive},
		{ID: regB, ProjectID: "prj-P", Name: "team-b", Status: domain.RegistryStatusActive},
	}
	got, err := newRepoAuthz(az).filterRegistries(carolCtx(), regs)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, regA, got[0].ID)
	require.Equal(t, regB, got[1].ID)
}

// listReader — фейк RegistryReader, возвращающий заранее заданный набор реестров
// (сервер-side курсор эмулируется полем next).
type listReader struct {
	regs []*domain.Registry
	next string
}

func (r listReader) Get(context.Context, string) (*domain.Registry, error) {
	return nil, regerrors.ErrNotFound
}
func (r listReader) List(context.Context, registry.ListQuery) ([]*domain.Registry, string, error) {
	return r.regs, r.next, nil
}

func newListHandler(reader registry.RegistryReader, az Authorizer) *RegistryHandler {
	uc := registry.New(reader, stubRepo{}, stubCfg{}, &fakeZotH{}, stubIAM{}, stubGeo{}, stubRepo{}, newMemOpsH(), "registry.kacho.local")
	return NewRegistryHandler(uc, az, 0)
}

const (
	regA = "regA0000000000000000A"
	regB = "regB0000000000000000B"
)

// REG-06 — List row-filter: subject с v_list на registry_registry:regA но НЕ regB →
// ответ содержит ТОЛЬКО regA (namespace-viewer НЕ видит чужие реестры).
func TestHandler_REG06_List_RowFiltered(t *testing.T) {
	reader := listReader{regs: []*domain.Registry{
		{ID: regA, ProjectID: "prj-P", Name: "team-a", Status: domain.RegistryStatusActive},
		{ID: regB, ProjectID: "prj-P", Name: "team-b", Status: domain.RegistryStatusActive},
	}}
	az := &recordingAuthorizer{allow: map[string]bool{
		registryObjectRef(regA): true, // v_list на regA, regB — нет
	}}
	h := newListHandler(reader, az)

	resp, err := h.List(carolCtx(), &registryv1.ListRegistriesRequest{ProjectId: "prj-P"})
	require.NoError(t, err)
	require.Len(t, resp.GetRegistries(), 1)
	require.Equal(t, regA, resp.GetRegistries()[0].GetId())
}

// REG-06 — non-member (нет v_list ни на один реестр) → 200 + пустой список (НЕ 403,
// exempt-parity: List не гейтится per-object Check).
func TestHandler_REG06_List_NonMember_EmptyNot403(t *testing.T) {
	reader := listReader{regs: []*domain.Registry{
		{ID: regA, ProjectID: "prj-P", Name: "team-a", Status: domain.RegistryStatusActive},
	}}
	az := &recordingAuthorizer{allow: map[string]bool{}} // ничего не разрешено
	h := newListHandler(reader, az)

	resp, err := h.List(carolCtx(), &registryv1.ListRegistriesRequest{ProjectId: "prj-P"})
	require.NoError(t, err, "non-member List → 200, не 403")
	require.Empty(t, resp.GetRegistries())
}

// REG-06 — iam.Check недоступен → fail-closed UNAVAILABLE (НЕ отдаём
// нефильтрованный список).
func TestHandler_REG06_List_IAMError_Unavailable(t *testing.T) {
	reader := listReader{regs: []*domain.Registry{
		{ID: regA, ProjectID: "prj-P", Name: "team-a", Status: domain.RegistryStatusActive},
	}}
	az := &recordingAuthorizer{err: regerrors.ErrUnavailable}
	h := newListHandler(reader, az)

	_, err := h.List(carolCtx(), &registryv1.ListRegistriesRequest{ProjectId: "prj-P"})
	require.Equal(t, codes.Unavailable, codeOf(t, err))
}

// REG-06 — breakglass (nil Authorizer) → row-filter пропускается, все реестры видны.
func TestHandler_REG06_List_Breakglass_All(t *testing.T) {
	reader := listReader{regs: []*domain.Registry{
		{ID: regA, ProjectID: "prj-P", Name: "team-a", Status: domain.RegistryStatusActive},
		{ID: regB, ProjectID: "prj-P", Name: "team-b", Status: domain.RegistryStatusActive},
	}}
	h := newListHandler(reader, nil)

	resp, err := h.List(carolCtx(), &registryv1.ListRegistriesRequest{ProjectId: "prj-P"})
	require.NoError(t, err)
	require.Len(t, resp.GetRegistries(), 2)
}

// REG-06 — next-page-token сохраняется после row-filter (курсор сервера не теряется,
// клиент продолжает пагинацию даже если страница «схлопнулась» фильтром).
func TestHandler_REG06_List_PreservesNextToken(t *testing.T) {
	reader := listReader{
		regs: []*domain.Registry{{ID: regA, ProjectID: "prj-P", Name: "team-a", Status: domain.RegistryStatusActive}},
		next: "cursor-token",
	}
	az := &recordingAuthorizer{allow: map[string]bool{registryObjectRef(regA): true}}
	h := newListHandler(reader, az)

	resp, err := h.List(carolCtx(), &registryv1.ListRegistriesRequest{ProjectId: "prj-P"})
	require.NoError(t, err)
	require.Equal(t, "cursor-token", resp.GetNextPageToken())
}
