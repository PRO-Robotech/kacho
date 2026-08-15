// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package quota_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/quota"
	vpcrepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
)

// Материализация строк учёта: по ВСЕМ видам домена разом, на промахе, из уже
// существующего вызова к соседу.
//
// Приёмка `docs/specs/sub-phase-quota-v2-materialised-usage-acceptance.md`
// (APPROVED, раунд 2), V2-1, V2-4 и DoD S2 п.5.
//
// ПОЧЕМУ ПО ВСЕМ ВИДАМ РАЗОМ, А НЕ ПО ТОМУ, КУДА ПРИШЛА ПЕРВАЯ ЗАПИСЬ. Иначе
// новый ВИД ресурса не получает строки вовсе и требует дозаписи беклогом (§0.1б),
// а чтение квот проекта, создавшего одну сеть, показывало бы один вид из восьми
// — то есть отвечало бы про ресурс не то, что о нём верно.
//
// ПОЧЕМУ ПРОМАХ, А НЕ СОЗДАНИЕ ПРОЕКТА. Ребро «владелец величин → владелец типа»
// завести нельзя: iam остаётся листом, обратный вызов замкнул бы цикл (§1.6).
// Значит строку заводит САМ владелец, и единственный момент, когда он о проекте
// узнаёт, — обращение к нему. Отсюда же ответ на «проект, созданный ДО этой
// работы»: он материализуется на первом же обращении, ничем не отличаясь от
// свежего. Обратного заполнения миграцией не существует и существовать не может
// — перечня проектов у владельца типа нет by construction.

const svc = "vpc"

// fakeResolver — владелец величин. Отвечает набором строк по видам домена.
type fakeResolver struct {
	limits    []quota.ResolvedLimit
	err       error
	callCount int
	lastScope string
	lastSvc   string
}

func (f *fakeResolver) Resolve(_ context.Context, scopeID, service string) ([]quota.ResolvedLimit, error) {
	f.callCount++
	f.lastScope, f.lastSvc = scopeID, service
	return f.limits, f.err
}

// fakeAccounts — тот же вызов к соседу, из которого уже берётся существование
// проекта. Считает обращения, чтобы утверждать «нового ребра не заведено».
type fakeAccounts struct {
	account   string
	err       error
	callCount int
}

func (f *fakeAccounts) AccountOf(_ context.Context, _ string) (string, error) {
	f.callCount++
	return f.account, f.err
}

// eightKinds — набор, который отдаёт владелец величин домену vpc.
//
// ВОСЕМЬ, А НЕ ДВЕНАДЦАТЬ — и это свойство настоящего резолва, а не удобство
// дублёра. Каталог держит у домена двенадцать видов; четыре из них считаются в
// РОДИТЕЛЬСКОМ ресурсе (сколько подсетей в сети, сколько интерфейсов в подсети),
// и резолв по корню аренды их не отвечает: на уровне проекта у такого вида нет
// единственного значения.
//
// Пока фильтра не было, дублёр отдавал восемь там, где настоящий отдавал
// двенадцать, — то есть был СНИСХОДИТЕЛЬНЕЕ настоящего и делал невидимым ровно
// тот дефект, ради которого его подставляют: четыре строки уезжали арендатору с
// носителем «проект» и потреблением, которое не наполнится никогда. Свойство
// закреплено там, где оно решается, — `TestResolveEffective_AnswersOnlyTenancyRootKinds`
// в домене iam.
func eightKinds() []quota.ResolvedLimit {
	kinds := []string{
		"vpc.network", "vpc.subnet", "vpc.address", "vpc.networkInterface",
		"vpc.securityGroup", "vpc.routeTable", "vpc.gateway", "vpc.cidrGroup",
	}
	out := make([]quota.ResolvedLimit, 0, len(kinds))
	for _, k := range kinds {
		out = append(out, quota.ResolvedLimit{Kind: k, Value: 4, SourceScope: "DEFAULT"})
	}
	return out
}

// kindsExcept — набор владельца величин без одного названного вида.
func kindsExcept(skip string) []quota.ResolvedLimit {
	all := eightKinds()
	out := make([]quota.ResolvedLimit, 0, len(all))
	for _, l := range all {
		if l.Kind == skip {
			continue
		}
		out = append(out, l)
	}
	return out
}

func newGuard(t testing.TB, r *kachomock.Repository, res *fakeResolver, acc *fakeAccounts) *quota.Guard {
	t.Helper()
	return quota.NewGuard(r, res, acc, svc)
}

// TestGuard_MaterialisesEveryKindOnFirstMiss — первое обращение заводит строки по
// всем восьми видам, а не только по тому, о котором спросили.
func TestGuard_MaterialisesEveryKindOnFirstMiss(t *testing.T) {
	ctx := context.Background()
	repo := kachomock.NewRepository()
	res := &fakeResolver{limits: eightKinds()}
	acc := &fakeAccounts{account: "acc-1"}
	g := newGuard(t, repo, res, acc)

	require.NoError(t, g.Admit(ctx, "prj-1", "vpc.network"),
		"после материализации место есть — обращение обязано пройти")

	rows := repo.QuotaRows()
	assert.Len(t, rows, 8, "заведены строки по ВСЕМ видам домена, а не по одному спрошенному")
	assert.Contains(t, rows, "project/prj-1/vpc.gateway",
		"вид, о котором никто не спрашивал, тоже обязан получить строку")

	assert.Equal(t, 1, res.callCount, "резолв величин — один на материализацию, а не по виду")
	assert.Equal(t, "prj-1", res.lastScope)
	assert.Equal(t, svc, res.lastSvc, "спрашиваются виды СВОЕГО домена: перечень принадлежит платформе")
}

// TestGuard_SecondAdmitDoesNotResolveAgain — материализация идёт на ПРОМАХЕ, а не
// на каждом обращении.
//
// Без этого утверждения «на промахе» неотличимо от «всегда»: на успешном пути обе
// формы дают одно и то же состояние строк, и разница видна только по числу
// обращений к соседу.
func TestGuard_SecondAdmitDoesNotResolveAgain(t *testing.T) {
	ctx := context.Background()
	repo := kachomock.NewRepository()
	res := &fakeResolver{limits: eightKinds()}
	acc := &fakeAccounts{account: "acc-1"}
	g := newGuard(t, repo, res, acc)

	require.NoError(t, g.Admit(ctx, "prj-1", "vpc.network"))
	require.NoError(t, g.Admit(ctx, "prj-1", "vpc.subnet"))

	assert.Equal(t, 1, res.callCount, "второе обращение обязано решаться местной строкой")
	assert.Equal(t, 1, acc.callCount, "и не платить вторым обращением к соседу")
}

// TestGuard_ResolverSilentAboutTheKindIsRefusal — QV2-15.
//
// Сосед ответил набором, НЕ называющим вид. Материализация прошла успешно (по
// другим видам строки заведены), но спрошенный вид потолка не получил — и это
// ОТКАЗ, а не «без предела».
func TestGuard_ResolverSilentAboutTheKindIsRefusal(t *testing.T) {
	ctx := context.Background()
	repo := kachomock.NewRepository()
	// Набор БЕЗ `vpc.gateway` — и вид исключается ПО ИМЕНИ, а не срезом по
	// индексу. Первая редакция резала `[:7]` и оставляла `vpc.gateway` внутри:
	// фикстура, которая обязана отличаться, не отличалась, и проба краснела на
	// собственной арифметике, а не на предмете.
	limits := kindsExcept("vpc.gateway")
	require.Len(t, limits, 7, "фикстура обязана БЫТЬ другой: вид действительно исключён")
	res := &fakeResolver{limits: limits}
	acc := &fakeAccounts{account: "acc-1"}
	g := newGuard(t, repo, res, acc)

	err := g.Admit(ctx, "prj-1", "vpc.gateway")

	require.Error(t, err, "вид без потолка обязан ОТВЕРГАТЬСЯ, а не пропускаться")
	assert.True(t, errors.Is(err, vpcrepo.ErrQuotaNotProvisioned),
		"признак «потолок не назван», а не «место кончилось»: администратору требуется ЗАВЕСТИ предел")
	assert.False(t, errors.Is(err, vpcrepo.ErrQuotaExceeded),
		"и он НЕ равен исчерпанию — иначе читающий пойдёт искать, что понизить")

	assert.Contains(t, repo.QuotaRows(), "project/prj-1/vpc.network",
		"положительный контроль: названные соседом виды строки получили — материализация не сорвалась целиком")
}

// TestGuard_ExhaustedIsRefusedWithoutTouchingThePeer — исчерпание решается
// местной строкой.
func TestGuard_ExhaustedIsRefusedWithoutTouchingThePeer(t *testing.T) {
	ctx := context.Background()
	repo := kachomock.NewRepository()
	repo.SeedQuota(vpcrepo.QuotaCarrierProject, "prj-1", "vpc.network", 2, 2)
	res := &fakeResolver{limits: eightKinds()}
	acc := &fakeAccounts{account: "acc-1"}
	g := newGuard(t, repo, res, acc)

	err := g.Admit(ctx, "prj-1", "vpc.network")

	require.Error(t, err)
	assert.True(t, errors.Is(err, vpcrepo.ErrQuotaExceeded), "исчерпание, а не отсутствие потолка")
	assert.Zero(t, res.callCount,
		"исчерпание — местный факт: платить за него обращением к соседу не за что")
}

// TestGuard_PeerUnavailableFailsClosed — QV2-19: сосед не отвечает.
//
// Мутация обязана быть отвергнута, а не пропущена: мягкий проход здесь означал
// бы «пока сосед лежит, пределов нет» — то есть контроль, снимающий себя ровно в
// тот момент, когда его труднее всего заметить.
func TestGuard_PeerUnavailableFailsClosed(t *testing.T) {
	ctx := context.Background()
	repo := kachomock.NewRepository()
	res := &fakeResolver{err: status.Error(codes.Unavailable, "iam is down")}
	acc := &fakeAccounts{account: "acc-1"}
	g := newGuard(t, repo, res, acc)

	err := g.Admit(ctx, "prj-1", "vpc.network")

	require.Error(t, err, "недоступность соседа обязана отвергать мутацию, а не пропускать её")
	st, ok := status.FromError(err)
	require.True(t, ok, "отказ обязан быть gRPC-статусом")
	assert.Equal(t, codes.Unavailable, st.Code(), "повторяемый отказ: край отдаёт по нему 503")
}

// TestGuard_MaterialisedProjectWorksWhilePeerIsDown — второй положительный
// контроль QV2-19: уже материализованный проект переживает недоступность соседа.
//
// Он и есть довод в пользу СНИМКА: решение принимается по местной строке, и
// падение владельца величин не останавливает работу арендаторов, о которых уже
// известно.
func TestGuard_MaterialisedProjectWorksWhilePeerIsDown(t *testing.T) {
	ctx := context.Background()
	repo := kachomock.NewRepository()
	repo.SeedQuota(vpcrepo.QuotaCarrierProject, "prj-1", "vpc.network", 4, 1)
	res := &fakeResolver{err: status.Error(codes.Unavailable, "iam is down")}
	acc := &fakeAccounts{err: status.Error(codes.Unavailable, "iam is down")}
	g := newGuard(t, repo, res, acc)

	assert.NoError(t, g.Admit(ctx, "prj-1", "vpc.network"),
		"местная строка есть — сосед для решения не нужен")
	assert.Zero(t, res.callCount, "и не спрашивается")
}

// TestGuard_EmptyAccountIsRefusedBeforeTheWrite — строка без зеркала аккаунта не
// заводится.
//
// Такая строка НЕВИДИМА аккаунтной дельте: изменение аккаунтной области её не
// найдёт, и она проживёт со старой величиной, а снаружи это неотличимо от
// исправной работы — дельта отчитается успехом, просто не тронув её (V2-4).
// Схема отвергает пустое зеркало ограничением; здесь отказ наступает раньше и
// называет предмет, а не приезжает нарушением ограничения.
func TestGuard_EmptyAccountIsRefusedBeforeTheWrite(t *testing.T) {
	ctx := context.Background()
	repo := kachomock.NewRepository()
	res := &fakeResolver{limits: eightKinds()}
	acc := &fakeAccounts{account: ""}
	g := newGuard(t, repo, res, acc)

	err := g.Admit(ctx, "prj-1", "vpc.network")

	require.Error(t, err, "строка без зеркала аккаунта не заводится")
	assert.Empty(t, repo.QuotaRows(), "и не заводится ЧАСТИЧНО: ни одной строки")
}

// TestGuard_NilGuardAdmitsWithoutPanicking — ловушка типизированного nil.
//
// `*Guard`, положенный в интерфейсный порт вызывающего, интерфейсу НЕ равен nil:
// проверка `u.quota != nil` истинна, и вызов доходит до приёмника. Без ветки
// nil-приёмника каждый Create на стенде без внутреннего адреса соседа падал бы
// паникой — то есть «полоса не собрана» означало бы «сервис не работает», а не
// «раннего отказа нет».
//
// Проба доказала свою способность падать до фикса: сборка композиционного корня
// была написана раньше неё и роняла именно этот путь.
func TestGuard_NilGuardAdmitsWithoutPanicking(t *testing.T) {
	var g *quota.Guard

	// Ровно та форма, в которой полоса живёт у вызывающего: интерфейсный порт.
	var port interface {
		Admit(ctx context.Context, projectID, kind string) error
	} = g

	// Сравнение ПРЯМОЕ, а не через require.NotNil: тот отражением доходит до
	// указателя и считает типизированный nil нулевым — то есть отвечает на другой
	// вопрос, чем задаёт `if u.quota != nil` у вызывающего. Проба обязана
	// повторять предикат вызывающего дословно, иначе она про него не утверждает.
	if port == nil {
		t.Fatal("типизированный nil интерфейсу не равен — на этом ловушка и стоит")
	}
	assert.NoError(t, port.Admit(context.Background(), "prj-1", "vpc.network"),
		"несобранная полоса означает «раннего отказа нет»; место по-прежнему занимает триггер")
}
