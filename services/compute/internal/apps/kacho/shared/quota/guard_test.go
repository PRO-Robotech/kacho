// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package quota

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/quota/quotaread"
	"github.com/PRO-Robotech/kacho/services/compute/internal/ports"
)

// fakeStore — подставной учёт.
//
// Отвечает ровно тем, чем отвечает настоящий: `Admit` до материализации даёт
// `ErrQuotaNotProvisioned`, после — nil. Дублёр, глотающий вход, на котором
// настоящий отказывает, сделал бы невидимым ровно тот дефект, ради которого его
// подставляют.
type fakeStore struct {
	rows      map[string]int64 // kind → предел
	used      map[string]int64 // kind → занято
	admits    []string
	materials [][]ports.QuotaRow
	admitErr  error
}

func newFakeStore() *fakeStore {
	return &fakeStore{rows: map[string]int64{}, used: map[string]int64{}}
}

func (f *fakeStore) Admit(_ context.Context, _, _, kind string) error {
	f.admits = append(f.admits, kind)
	if f.admitErr != nil {
		return f.admitErr
	}
	limit, ok := f.rows[kind]
	if !ok {
		return fmt.Errorf("%w: project prj-1 has no ceiling stated for %s",
			ports.ErrQuotaNotProvisioned, kind)
	}
	if limit <= 0 {
		return fmt.Errorf("%w: project prj-1 has reached its limit of %d %s",
			ports.ErrQuotaExceeded, limit, kind)
	}
	return nil
}

func (f *fakeStore) Materialize(_ context.Context, rows []ports.QuotaRow) (int64, error) {
	f.materials = append(f.materials, rows)
	for _, r := range rows {
		if _, exists := f.rows[r.Kind]; !exists {
			f.rows[r.Kind] = r.Limit
		}
	}
	return int64(len(rows)), nil
}

// ListStates отдаёт те строки, которые у подставного учёта ЕСТЬ, — и в том же
// порядке, что настоящий (`ORDER BY kind`).
//
// Порядок здесь не косметика: полоса чтения обещает его контракту, и дублёр,
// отдающий что попало, сделал бы невидимым ровно тот дефект, ради которого
// порядок и закреплён.
func (f *fakeStore) ListStates(_ context.Context, carrierType, carrierID string) ([]quotaread.State, error) {
	kinds := make([]string, 0, len(f.rows))
	for k := range f.rows {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	out := make([]quotaread.State, 0, len(kinds))
	for _, k := range kinds {
		out = append(out, quotaread.State{
			Kind: k, Limit: f.rows[k], Used: f.used[k],
			CarrierType: carrierType, CarrierID: carrierID,
		})
	}
	return out, nil
}

type fakeResolver struct {
	limits []ports.ResolvedLimit
	err    error
	calls  int
}

func (f *fakeResolver) Resolve(_ context.Context, _, _ string) ([]ports.ResolvedLimit, error) {
	f.calls++
	return f.limits, f.err
}

type fakeAccounts struct {
	account string
	err     error
}

func (f *fakeAccounts) AccountOf(_ context.Context, _ string) (string, error) {
	return f.account, f.err
}

func computeLimits() []ports.ResolvedLimit {
	return []ports.ResolvedLimit{
		{Kind: "compute.instance", Value: 5, SourceScope: "DEFAULT"},
		{Kind: "compute.guestAccessKey", Value: 10, SourceScope: "DEFAULT"},
		{Kind: "compute.placementGroup", Value: 3, SourceScope: "DEFAULT"},
	}
}

// TestGuard_MissMaterialisesAllKindsAndRetriesOnce — промах заводит строки по
// ВСЕМ видам домена и спрашивает СНОВА.
//
// «По всем» — несущая половина: материализация по тому виду, куда пришла первая
// запись, оставила бы остальные без строк, и каждый следующий вид платил бы
// собственным промахом и собственным вызовом к соседу.
func TestGuard_MissMaterialisesAllKindsAndRetriesOnce(t *testing.T) {
	store := newFakeStore()
	resolver := &fakeResolver{limits: computeLimits()}
	g := NewGuard(store, resolver, &fakeAccounts{account: "acc-1"}, "compute")

	if err := g.Admit(context.Background(), "prj-1", "compute.instance"); err != nil {
		t.Fatalf("после материализации место обязано найтись: %v", err)
	}

	if len(store.admits) != 2 {
		t.Errorf("вопрос задан %d раз(а), ожидались два: до материализации и после",
			len(store.admits))
	}
	if len(store.materials) != 1 {
		t.Fatalf("материализация случилась %d раз(а), ожидался один", len(store.materials))
	}
	if got := len(store.materials[0]); got != 3 {
		t.Errorf("заведено строк: %d, ожидались все три вида домена", got)
	}
	// Зеркало аккаунта обязано доехать в КАЖДУЮ строку: строка без него невидима
	// аккаунтной дельте, и снаружи это неотличимо от исправной работы.
	for _, r := range store.materials[0] {
		if r.AccountID != "acc-1" {
			t.Errorf("строка %q заведена без зеркала аккаунта", r.Kind)
		}
		if r.CarrierType != ports.QuotaCarrierProject || r.CarrierID != "prj-1" {
			t.Errorf("строка %q заведена не на том носителе: %s/%s", r.Kind, r.CarrierType, r.CarrierID)
		}
	}
}

// TestGuard_HitDoesNotCallThePeer — положительный контроль к предыдущей пробе.
//
// Без него «материализует по промаху» зеленело бы и на страже, который ходит к
// соседу на КАЖДУЮ мутацию: приёмка обещает ровно один дополнительный запрос —
// на первое создание в проекте, дальше всё локально.
func TestGuard_HitDoesNotCallThePeer(t *testing.T) {
	store := newFakeStore()
	store.rows["compute.instance"] = 5
	resolver := &fakeResolver{limits: computeLimits()}
	g := NewGuard(store, resolver, &fakeAccounts{account: "acc-1"}, "compute")

	if err := g.Admit(context.Background(), "prj-1", "compute.instance"); err != nil {
		t.Fatalf("место есть, отказа быть не должно: %v", err)
	}
	if resolver.calls != 0 {
		t.Errorf("сосед опрошен %d раз(а) при наличии строки учёта — лишний запрос на горячем пути",
			resolver.calls)
	}
	if len(store.materials) != 0 {
		t.Error("материализация случилась при наличии строки учёта")
	}
}

// TestGuard_ExceededIsNotAMissAndNeverMaterialises — исчерпание НЕ приводит к
// материализации и не превращается в промах.
//
// Свести их значило бы ходить к соседу на каждый отказ по исчерпанию — то есть
// сделать самый частый отказ самым дорогим.
func TestGuard_ExceededIsNotAMissAndNeverMaterialises(t *testing.T) {
	store := newFakeStore()
	store.rows["compute.instance"] = 0 // строка есть, места нет
	resolver := &fakeResolver{limits: computeLimits()}
	g := NewGuard(store, resolver, &fakeAccounts{account: "acc-1"}, "compute")

	err := g.Admit(context.Background(), "prj-1", "compute.instance")
	if !errors.Is(err, ports.ErrQuotaExceeded) {
		t.Fatalf("ожидалось исчерпание, получили: %v", err)
	}
	if errors.Is(err, ports.ErrQuotaNotProvisioned) {
		t.Error("исчерпание неотличимо от неназначенного потолка")
	}
	if resolver.calls != 0 || len(store.materials) != 0 {
		t.Error("исчерпание вызвало материализацию: сосед опрашивается на каждый отказ")
	}
}

// TestGuard_PeerUnavailableFailsClosed — недоступный владелец величин НЕ
// означает «разрешено».
//
// Мягкий проход здесь означал бы, что потолок перестаёт действовать ровно тогда,
// когда сосед недоступен, — то есть под нагрузкой, ради которой он и написан.
func TestGuard_PeerUnavailableFailsClosed(t *testing.T) {
	store := newFakeStore()
	resolver := &fakeResolver{err: status.Error(codes.Unavailable, "iam is down")}
	g := NewGuard(store, resolver, &fakeAccounts{account: "acc-1"}, "compute")

	err := g.Admit(context.Background(), "prj-1", "compute.instance")
	if err == nil {
		t.Fatal("недоступность соседа пропустила мутацию: потолок открыт ровно тогда, когда нужен")
	}
	if got := status.Code(err); got != codes.Unavailable {
		t.Errorf("код: получили %v, ожидали Unavailable — иначе вызывающий не поймёт, что можно повторить", got)
	}
	if len(store.materials) != 0 {
		t.Error("строки заведены при недоступном владельце величин")
	}
}

// TestGuard_EmptyAccountIsRefusedBeforeAnyWrite — строка без зеркала аккаунта не
// заводится вовсе.
//
// Такая строка невидима аккаунтной дельте и жила бы со старой величиной, а
// снаружи это неотличимо от исправной работы: дельта отчитается успехом, просто
// не тронув её.
func TestGuard_EmptyAccountIsRefusedBeforeAnyWrite(t *testing.T) {
	store := newFakeStore()
	resolver := &fakeResolver{limits: computeLimits()}
	g := NewGuard(store, resolver, &fakeAccounts{account: ""}, "compute")

	err := g.Admit(context.Background(), "prj-1", "compute.instance")
	if !errors.Is(err, ports.ErrFailedPrecondition) {
		t.Fatalf("ожидалось предусловие, получили: %v", err)
	}
	if len(store.materials) != 0 {
		t.Error("строка заведена без зеркала аккаунта")
	}
	if resolver.calls != 0 {
		t.Error("резолв величин случился раньше, чем выяснилось, что зеркала нет")
	}
}

// TestGuard_PeerNamingNoKindsStaysNotProvisioned — сосед, не назвавший ни одного
// вида, оставляет отказ терминальным.
//
// Подставить свою величину нельзя: числа живут в посеве владельца, а не в коде
// потребителя. И крутиться нельзя — иначе «не назначено» стало бы зависанием.
func TestGuard_PeerNamingNoKindsStaysNotProvisioned(t *testing.T) {
	store := newFakeStore()
	resolver := &fakeResolver{limits: nil}
	g := NewGuard(store, resolver, &fakeAccounts{account: "acc-1"}, "compute")

	err := g.Admit(context.Background(), "prj-1", "compute.instance")
	if !errors.Is(err, ports.ErrQuotaNotProvisioned) {
		t.Fatalf("ожидался неназначенный потолок, получили: %v", err)
	}
	if len(store.admits) != 2 {
		t.Errorf("вопрос задан %d раз(а): повтор обязан быть ровно один", len(store.admits))
	}
}
