// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package quota

import (
	"context"
	stderrors "errors"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/quota/quotaread"
	storageerr "github.com/PRO-Robotech/kacho/services/storage/internal/errors"
)

// Совещательная полоса: материализация на промахе, отказ — когда потолка нет.
//
// Приёмка `docs/specs/sub-phase-quota-v2-materialised-usage-acceptance.md`
// (APPROVED, раунд 2): V2-3, DoD S4 п.1. Сценарии QV2-15 («потолок не назван» —
// уровень интеграции владельца, сосед отвечает набором БЕЗ вида) и QV2-19
// (сосед недоступен — мутация fail-closed).
//
// # Почему эти два сценария живут здесь, а не в сквозном прогоне
//
// Оба требуют состояния, которого на исправном стенде не бывает. «Потолок не
// назван ни на одной области» запрещён гейтом посева: каждый вид каталога несёт
// строку умолчания. «Сосед недоступен» глобален — он снёс бы каждую параллельную
// суиту вместе с посевом. Это заявление об УРОВНЕ, а не послабление: утверждения
// ниже полные (полоса отказа, её отличие от соседней, положительный контроль) и
// падают на дефекте так же, как упал бы сквозной кейс.

// fakeQuotaRepo — строки учёта в памяти.
//
// Дублёр обязан выполнять контракт настоящего, а не быть снисходительнее его:
// `Admit` здесь отвечает ТЕМИ ЖЕ двумя отказами, что и функция базы, — «строки
// нет» и «строка полна» различимы. Дублёр, отвечающий одним отказом на оба,
// сделал бы невидимым ровно тот дефект, ради которого его подставляют.
type fakeQuotaRepo struct {
	rows map[string]int64 // вид → предел; отсутствие ключа = строки нет
	used map[string]int64
	// materialiseCalls — сколько раз звали материализацию. Считается, чтобы
	// «не звали» было отличимо от «завела ноль строк».
	materialiseCalls int
}

func newFakeRepo() *fakeQuotaRepo {
	return &fakeQuotaRepo{rows: map[string]int64{}, used: map[string]int64{}}
}

// ListStates отдаёт те строки, которые у подставного учёта ЕСТЬ, — и в том же
// порядке, что настоящий (`ORDER BY kind`).
//
// Порядок здесь не косметика: полоса чтения обещает его контракту, и дублёр,
// отдающий что попало, сделал бы невидимым ровно тот дефект, ради которого
// порядок и закреплён.
func (f *fakeQuotaRepo) ListStates(_ context.Context, carrierType, carrierID string) ([]quotaread.State, error) {
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

func (f *fakeQuotaRepo) Admit(_ context.Context, _, _, kind string) error {
	limit, ok := f.rows[kind]
	if !ok {
		return storageerr.ErrQuotaNotProvisioned
	}
	if f.used[kind] >= limit {
		return storageerr.ErrQuotaExceeded
	}
	return nil
}

func (f *fakeQuotaRepo) Materialize(_ context.Context, rows []Row) (int64, error) {
	f.materialiseCalls++
	var n int64
	for _, r := range rows {
		if _, exists := f.rows[r.Kind]; exists {
			continue // ON CONFLICT DO NOTHING — уже заведённое не трогаем
		}
		f.rows[r.Kind] = r.Limit
		n++
	}
	return n, nil
}

// fakeResolver — владелец величин.
type fakeResolver struct {
	limits []ResolvedLimit
	err    error
	calls  int
}

func (f *fakeResolver) Resolve(context.Context, string, string) ([]ResolvedLimit, error) {
	f.calls++
	return f.limits, f.err
}

// fakeAccounts — аккаунт проекта.
type fakeAccounts struct {
	account string
	err     error
}

func (f *fakeAccounts) AccountOf(context.Context, string) (string, error) {
	return f.account, f.err
}

// TestGuard_MaterialisesOnMissThenAdmits — промах заводит строки и пропускает.
func TestGuard_MaterialisesOnMissThenAdmits(t *testing.T) {
	repo := newFakeRepo()
	resolver := &fakeResolver{limits: []ResolvedLimit{
		{Kind: "storage.volumes", Value: 64, SourceScope: "DEFAULT"},
		{Kind: "storage.snapshots", Value: 128, SourceScope: "DEFAULT"},
		{Kind: "storage.images", Value: 32, SourceScope: "DEFAULT"},
	}}
	g := NewGuard(repo, resolver, &fakeAccounts{account: "acc-1"})

	require.NoError(t, g.Admit(context.Background(), "prj-1", "storage.volumes"))
	require.Equal(t, 1, repo.materialiseCalls, "материализация звалась ровно один раз")
	require.Len(t, repo.rows, 3,
		"заведены ВСЕ виды домена разом, а не только спрошенный: иначе следующий вид "+
			"потребовал бы второго обращения к соседу, а новый вид — правки этого места")

	// Второй вопрос идёт мимо соседа: строки уже есть.
	require.NoError(t, g.Admit(context.Background(), "prj-1", "storage.snapshots"))
	require.Equal(t, 1, repo.materialiseCalls, "повторный вопрос соседа не беспокоит")
	require.Equal(t, 1, resolver.calls)
}

// TestGuard_PeerNamingNoSuchKindIsRefusal — QV2-15.
//
// Сосед ответил набором БЕЗ спрошенного вида. Строка не заводится, и второй
// вопрос отвечает тем же отказом — но уже терминально. Это и есть «не сказано =
// отказ»: пропусти здесь, и отсутствие потолка стало бы разрешением.
func TestGuard_PeerNamingNoSuchKindIsRefusal(t *testing.T) {
	repo := newFakeRepo()
	resolver := &fakeResolver{limits: []ResolvedLimit{
		{Kind: "storage.snapshots", Value: 128, SourceScope: "DEFAULT"},
	}}
	g := NewGuard(repo, resolver, &fakeAccounts{account: "acc-1"})

	err := g.Admit(context.Background(), "prj-1", "storage.volumes")
	require.Error(t, err)
	require.True(t, stderrors.Is(err, storageerr.ErrQuotaNotProvisioned),
		"вид, которого сосед не назвал, остаётся БЕЗ потолка — и это отказ: %v", err)
	require.False(t, stderrors.Is(err, storageerr.ErrQuotaExceeded),
		"он НЕ равен исчерпанию: администратору эти два состояния велят разное")

	// Положительный контроль: вид, который сосед НАЗВАЛ, проходит. Без него
	// отрицание зеленело бы и на полностью сломанной полосе.
	require.NoError(t, g.Admit(context.Background(), "prj-1", "storage.snapshots"))
}

// TestGuard_EmptyPeerAnswerIsRefusalToo — сосед не назвал НИ ОДНОГО вида.
//
// Отдельной ветки отказа на этот случай нет намеренно: исход не должен зависеть
// от того, промолчал сосед про все виды или про один. Отвечает второй вопрос —
// тем же «потолок не назван».
func TestGuard_EmptyPeerAnswerIsRefusalToo(t *testing.T) {
	repo := newFakeRepo()
	g := NewGuard(repo, &fakeResolver{limits: nil}, &fakeAccounts{account: "acc-1"})

	err := g.Admit(context.Background(), "prj-1", "storage.volumes")
	require.True(t, stderrors.Is(err, storageerr.ErrQuotaNotProvisioned), "%v", err)
}

// TestGuard_UnavailablePeerFailsClosed — QV2-19.
//
// Пропустить мутацию, не установив предела, значит снять контроль ровно в тот
// момент, когда это труднее всего заметить: «пока сосед лежит, пределов нет».
func TestGuard_UnavailablePeerFailsClosed(t *testing.T) {
	repo := newFakeRepo()
	unavailable := status.Error(codes.Unavailable, "iam is down")
	g := NewGuard(repo, &fakeResolver{err: unavailable}, &fakeAccounts{account: "acc-1"})

	err := g.Admit(context.Background(), "prj-1", "storage.volumes")
	require.Error(t, err, "недоступность соседа НЕ пропускает мутацию")
	require.Equal(t, codes.Unavailable, status.Code(err),
		"полоса отказа — недоступность, а не «потолок не назван»: повтор здесь осмыслен, "+
			"и вызывающий обязан отличать временное от терминального")
	require.NotContains(t, err.Error(), "iam is down",
		"проза соседа наружу не идёт: она может нести имя хоста и текст драйвера")

	// Положительный контроль: тот же проект при живом соседе проходит.
	ok := NewGuard(newFakeRepo(), &fakeResolver{limits: []ResolvedLimit{
		{Kind: "storage.volumes", Value: 1, SourceScope: "DEFAULT"},
	}}, &fakeAccounts{account: "acc-1"})
	require.NoError(t, ok.Admit(context.Background(), "prj-1", "storage.volumes"))
}

// TestGuard_ProjectWithoutAccountIsRefused — зеркало аккаунта обязательно.
//
// Строка без зеркала НЕВИДИМА аккаунтной дельте: изменение аккаунтной области её
// не найдёт, и она проживёт со старой величиной, а снаружи это неотличимо от
// исправной работы. Схема отвергает такую строку; здесь отказ наступает раньше и
// называет предмет.
func TestGuard_ProjectWithoutAccountIsRefused(t *testing.T) {
	repo := newFakeRepo()
	g := NewGuard(repo, &fakeResolver{limits: []ResolvedLimit{
		{Kind: "storage.volumes", Value: 1, SourceScope: "DEFAULT"},
	}}, &fakeAccounts{account: ""})

	err := g.Admit(context.Background(), "prj-1", "storage.volumes")
	require.Error(t, err)
	require.True(t, stderrors.Is(err, storageerr.ErrFailedPrecondition), "%v", err)
	require.Equal(t, 0, repo.materialiseCalls,
		"без зеркала материализация не звалась вовсе — строка с пустым зеркалом не заводится")
}

// TestGuard_ExhaustionDoesNotCallThePeer — исчерпание есть МЕСТНЫЙ факт.
//
// Порядок «сначала спросить своё, потом соседа» несущий: обращение к соседу на
// каждом исчерпании превратило бы предел в источник нагрузки на него ровно
// тогда, когда арендатор упирается чаще всего.
func TestGuard_ExhaustionDoesNotCallThePeer(t *testing.T) {
	repo := newFakeRepo()
	repo.rows["storage.volumes"] = 1
	repo.used["storage.volumes"] = 1
	resolver := &fakeResolver{}
	g := NewGuard(repo, resolver, &fakeAccounts{account: "acc-1"})

	err := g.Admit(context.Background(), "prj-1", "storage.volumes")
	require.True(t, stderrors.Is(err, storageerr.ErrQuotaExceeded), "%v", err)
	require.Equal(t, 0, resolver.calls, "исчерпание решается местной строкой")
	require.Equal(t, 0, repo.materialiseCalls)
}

// TestGuard_NilGuardIsNoEarlyRefusal — не собранная полоса не роняет сервис.
//
// `*Guard`, положенный в интерфейсный порт, интерфейсу не равен nil, поэтому
// проверка у вызывающего истинна и вызов доходит сюда. Без ветки nil-приёмника
// КАЖДЫЙ Create на посадке без внутреннего адреса соседа падал бы паникой — то
// есть «полоса не собрана» означало бы не «нет раннего отказа», а «сервис не
// работает».
func TestGuard_NilGuardIsNoEarlyRefusal(t *testing.T) {
	var g *Guard
	require.NoError(t, g.Admit(context.Background(), "prj-1", "storage.volumes"))
}
