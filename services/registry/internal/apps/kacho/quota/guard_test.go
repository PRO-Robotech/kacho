// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package quota

import (
	"context"
	"errors"
	"testing"

	regerrors "github.com/PRO-Robotech/kacho/services/registry/internal/errors"
)

// Пробы совещательной полосы: порядок «спросить → материализовать → спросить
// ещё раз», раскладка видов по двум таблицам и fail-closed на отказе соседа.
//
// Приёмка `docs/specs/sub-phase-quota-v2-materialised-usage-acceptance.md`
// (APPROVED, раунд 2), V2-1, V2-3, DoD S4 п.1.
//
// Базы здесь нет намеренно: предмет этих проб — ПОРЯДОК и РАСКЛАДКА, то есть
// поведение самой полосы, а не поведение триггера. Списание и возврат проверяют
// интеграционные пробы репозитория, где есть настоящая схема.

// stubStore — подставное хранилище. Не «разрешающее»: оно отвечает ровно тем,
// что ему задали, и записывает КАЖДЫЙ вызов, чтобы порядок был утверждаем.
type stubStore struct {
	admitErrs  []error // ответы на последовательные вызовы QuotaAdmit
	admitCalls []string
	flat       []Row
	nested     []Row
	materiaErr error
}

func (s *stubStore) QuotaAdmit(_ context.Context, carrierType, carrierID, kind string) error {
	s.admitCalls = append(s.admitCalls, carrierType+"|"+carrierID+"|"+kind)
	if len(s.admitErrs) == 0 {
		return nil
	}
	err := s.admitErrs[0]
	s.admitErrs = s.admitErrs[1:]
	return err
}

func (s *stubStore) MaterializeQuotas(_ context.Context, rows []Row) (int64, error) {
	if s.materiaErr != nil {
		return 0, s.materiaErr
	}
	s.flat = append(s.flat, rows...)
	return int64(len(rows)), nil
}

func (s *stubStore) MaterializeNestedDefaults(_ context.Context, rows []Row) (int64, error) {
	if s.materiaErr != nil {
		return 0, s.materiaErr
	}
	s.nested = append(s.nested, rows...)
	return int64(len(rows)), nil
}

type stubResolver struct {
	limits []ResolvedLimit
	err    error
	calls  int
}

func (r *stubResolver) Resolve(_ context.Context, _, _ string) ([]ResolvedLimit, error) {
	r.calls++
	return r.limits, r.err
}

type stubAccounts struct {
	id  string
	err error
}

func (a *stubAccounts) AccountOf(_ context.Context, _ string) (string, error) {
	return a.id, a.err
}

// TestGuard_MissMaterialisesThenAsksAgain — порядок «спросить → материализовать
// → спросить ещё раз», и второй вопрос ТЕРМИНАЛЕН.
func TestGuard_MissMaterialisesThenAsksAgain(t *testing.T) {
	store := &stubStore{admitErrs: []error{regerrors.ErrQuotaNotProvisioned, nil}}
	res := &stubResolver{limits: []ResolvedLimit{
		{Kind: "registry.registries", Value: 8, SourceScope: "DEFAULT"},
	}}
	g := NewGuard(store, res, &stubAccounts{id: "acc-1"}, "registry")

	if err := g.Admit(context.Background(), "prj-1", "registry.registries"); err != nil {
		t.Fatalf("после материализации место обязано найтись: %v", err)
	}
	if res.calls != 1 {
		t.Fatalf("сосед спрошен %d раз, ожидался ровно один", res.calls)
	}
	if len(store.admitCalls) != 2 {
		t.Fatalf("вопросов местной строке %d, ожидалось два: %v", len(store.admitCalls), store.admitCalls)
	}
}

// TestGuard_SecondMissIsRefusalNotPermission — сосед, не назвавший вид,
// оставляет строку незаведённой, и второй вопрос отвечает ОТКАЗОМ.
//
// Это то самое утверждение, ради которого правило V2-3 и написано: пропуск на
// промахе неотличим от отсутствия квот вовсе.
func TestGuard_SecondMissIsRefusalNotPermission(t *testing.T) {
	store := &stubStore{admitErrs: []error{
		regerrors.ErrQuotaNotProvisioned,
		regerrors.ErrQuotaNotProvisioned,
	}}
	// Сосед называет ДРУГОЙ вид — тот, о котором спрашивают, остаётся без потолка.
	res := &stubResolver{limits: []ResolvedLimit{
		{Kind: "registry.repositories", Value: 256, SourceScope: "DEFAULT"},
	}}
	g := NewGuard(store, res, &stubAccounts{id: "acc-1"}, "registry")

	err := g.Admit(context.Background(), "prj-1", "registry.registries")
	if !errors.Is(err, regerrors.ErrQuotaNotProvisioned) {
		t.Fatalf("ожидался отказ «потолок не назван», получено: %v", err)
	}
}

// TestGuard_NestedKindsGoToTheirOwnTable — вложенный вид кладётся в проектный
// резолв, плоский — в учёт.
//
// Положительный контроль здесь несущий: без него «в учёт не попало» было бы
// неотличимо от «не попало никуда».
func TestGuard_NestedKindsGoToTheirOwnTable(t *testing.T) {
	store := &stubStore{admitErrs: []error{regerrors.ErrQuotaNotProvisioned, nil}}
	res := &stubResolver{limits: []ResolvedLimit{
		{Kind: "registry.registries", Value: 8, SourceScope: "DEFAULT"},
		{Kind: "registry.repositories", Value: 256, SourceScope: "DEFAULT"},
		{Kind: "registry.registries.repositories", Value: 64, SourceScope: "DEFAULT"},
	}}
	g := NewGuard(store, res, &stubAccounts{id: "acc-1"}, "registry")

	if err := g.Admit(context.Background(), "prj-1", "registry.registries"); err != nil {
		t.Fatalf("неожиданный отказ: %v", err)
	}
	if len(store.flat) != 2 {
		t.Fatalf("плоских видов в учёте %d, ожидалось два: %+v", len(store.flat), store.flat)
	}
	if len(store.nested) != 1 {
		t.Fatalf("вложенных видов в резолве %d, ожидался один: %+v", len(store.nested), store.nested)
	}
	if store.nested[0].Kind != "registry.registries.repositories" {
		t.Fatalf("во вложенный резолв попал не тот вид: %s", store.nested[0].Kind)
	}
	// Зеркало аккаунта обязано доехать до КАЖДОЙ строки: без него строка невидима
	// аккаунтной дельте, а снаружи это неотличимо от исправной работы.
	for _, r := range append(append([]Row{}, store.flat...), store.nested...) {
		if r.AccountID != "acc-1" {
			t.Fatalf("строка без зеркала аккаунта: %+v", r)
		}
	}
}

// TestGuard_PeerRefusalIsFailClosed — отказ соседа НЕ пропускает мутацию.
//
// Пропустить, не установив предела, значит снять контроль ровно в тот момент,
// когда это труднее всего заметить.
func TestGuard_PeerRefusalIsFailClosed(t *testing.T) {
	sentinel := errors.New("сосед недоступен")
	store := &stubStore{admitErrs: []error{regerrors.ErrQuotaNotProvisioned}}
	res := &stubResolver{err: sentinel}
	g := NewGuard(store, res, &stubAccounts{id: "acc-1"}, "registry")

	if err := g.Admit(context.Background(), "prj-1", "registry.registries"); !errors.Is(err, sentinel) {
		t.Fatalf("отказ соседа обязан дойти до вызывающего, получено: %v", err)
	}

	// Положительный контроль: отказ ЗЕРКАЛА тоже fail-closed, и это другой путь.
	store2 := &stubStore{admitErrs: []error{regerrors.ErrQuotaNotProvisioned}}
	g2 := NewGuard(store2, &stubResolver{}, &stubAccounts{err: sentinel}, "registry")
	if err := g2.Admit(context.Background(), "prj-1", "registry.registries"); !errors.Is(err, sentinel) {
		t.Fatalf("отказ поиска аккаунта обязан дойти до вызывающего, получено: %v", err)
	}
}

// TestGuard_NilGuardIsNotPermission — несобранная полоса не падает и не решает.
//
// Ловушка типизированного nil: `*Guard`, положенный в интерфейсный порт,
// интерфейсу не равен nil, поэтому проверка у вызывающего истинна и вызов
// доходит сюда. Без этой ветки КАЖДЫЙ Create на стенде без соседа падал бы
// паникой.
func TestGuard_NilGuardIsNotPermission(t *testing.T) {
	var g *Guard
	if err := g.Admit(context.Background(), "prj-1", "registry.registries"); err != nil {
		t.Fatalf("несобранная полоса обязана молчать, получено: %v", err)
	}
	if err := g.AdmitCarrier(context.Background(), "c", "id", "k"); err != nil {
		t.Fatalf("несобранная полоса обязана молчать и на носителе: %v", err)
	}
}

// TestGuard_CarrierAskDoesNotMaterialise — вопрос про носителя-родителя не
// материализует.
//
// Строку учёта родителя заводит та же транзакция, что и самого родителя,
// поэтому её отсутствие означает не «мы ещё не спросили соседа», а «родитель
// заведён без резолва вложенного вида». Ответить на это заведением строки
// значило бы выдумать величину, которой платформа не называла.
func TestGuard_CarrierAskDoesNotMaterialise(t *testing.T) {
	store := &stubStore{admitErrs: []error{regerrors.ErrQuotaNotProvisioned}}
	res := &stubResolver{}
	g := NewGuard(store, res, &stubAccounts{id: "acc-1"}, "registry")

	err := g.AdmitCarrier(context.Background(),
		"registry.registries.repositories", "reg-1", "registry.registries.repositories")
	if !errors.Is(err, regerrors.ErrQuotaNotProvisioned) {
		t.Fatalf("ожидался отказ, получено: %v", err)
	}
	if res.calls != 0 {
		t.Fatalf("сосед спрошен %d раз — вопрос про носителя не должен материализовать", res.calls)
	}
}
