// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
	kachopg "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho/pg"
)

// setupTestDB выдаёт вызывающему СВОЮ мигрированную базу на общем Postgres пакета
// и возвращает DSN с search_path=kacho_nlb,public.
//
// Клонируется шаблон, который TestMain мигрировал один раз, вместо подъёма
// контейнера и проигрывания всей цепочки на каждый вызов — почему именно так, см.
// testmain_integration_test.go. База у вызывающего по-прежнему своя: общим между
// тестами остаётся только процесс сервера, поэтому построчная конкуренция внутри
// теста ведёт себя ровно как когда у каждого теста был свой контейнер.
func setupTestDB(t testing.TB) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test (testing.Short)")
	}
	dsn := appendSearchPathOptions(newSharedDatabase(t, true))

	// Учёт числа ресурсов: вставка строки ресурса СПИСЫВАЕТ место, и списать его
	// не с чего, пока у проекта нет строки учёта. На живом пути её заводит
	// материализация ПЕРЕД writer-транзакцией; проба идёт мимо use-case'а, прямо
	// в репозиторий, поэтому базу в то же состояние приводит фикстура. Разбор,
	// перечень идентичностей и что делать новой пробе — `quota_fixture_test.go`.
	seedFixtureQuotas(t, dsn)

	return dsn
}

// appendSearchPathOptions добавляет libpq `options=-c search_path=kacho_nlb,public`
// (mirror config.baseDSN поведения).
func appendSearchPathOptions(dsn string) string {
	const optionsParam = "options=-c%20search_path%3Dkacho_nlb%2Cpublic"
	if strings.Contains(dsn, "options=") || strings.Contains(dsn, "options%3D") {
		return dsn
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	return dsn + sep + optionsParam
}

// testContext — общий test fixture: pool + repo. Возвращает обоих, чтобы
// тесты могли использовать pool для raw-SQL (CHECK-constraint violations,
// которые нельзя триггернуть через типизированный repo-layer).
type testContext struct {
	Pool *pgxpool.Pool
	Repo *kachopg.Repository
}

// newTestCtx создаёт изолированный test-context (свежий Postgres-контейнер).
// Pool/Repo живут до Cleanup(t).
func newTestCtx(t testing.TB) *testContext {
	t.Helper()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(func() { pool.Close() })
	return &testContext{Pool: pool, Repo: kachopg.New(pool, nil)}
}

// newRepo — short helper для тестов, которым не нужен raw-pool доступ.
func newRepo(t testing.TB, dsn string) (*kachopg.Repository, func()) {
	t.Helper()
	pool, err := coredb.NewPool(context.Background(), dsn)
	require.NoError(t, err)
	return kachopg.New(pool, nil), func() { pool.Close() }
}

// newLB строит свежий domain.LoadBalancer для тестов.
func newLB(projectID, name string) *domain.LoadBalancer {
	return &domain.LoadBalancer{
		ID:              domain.ResourceID(ids.NewID(ids.PrefixLoadBalancer)),
		ProjectID:       domain.ProjectID(projectID),
		RegionID:        "ru-central1",
		Name:            domain.LbName(name),
		Description:     "test lb",
		Labels:          domain.LabelsFromMap(map[string]string{"test": "1"}),
		Type:            domain.LBTypeExternal,
		Status:          domain.LBStatusInactive,
		SessionAffinity: domain.SessionAffinity5Tuple,
	}
}

// newListener строит свежий domain.Listener.
func newListener(lbID domain.ResourceID, projectID, name string, port int32) *domain.Listener {
	return &domain.Listener{
		ID:             domain.ResourceID(ids.NewID(ids.PrefixListener)),
		LoadBalancerID: lbID,
		ProjectID:      domain.ProjectID(projectID),
		RegionID:       "ru-central1",
		Name:           domain.LbName(name),
		Description:    "",
		Labels:         domain.LbLabels{},
		Protocol:       domain.ProtoTCP,
		Port:           domain.LbPort(port),
		Status:         domain.ListenerStatusActive,
	}
}

// newTG строит свежий domain.TargetGroup с safe-defaults (без targets).
func newTG(projectID, name string) *domain.TargetGroup {
	return &domain.TargetGroup{
		ID:                  domain.ResourceID(ids.NewID(ids.PrefixTargetGroup)),
		ProjectID:           domain.ProjectID(projectID),
		RegionID:            "ru-central1",
		Name:                domain.LbName(name),
		Description:         "",
		Labels:              domain.LbLabels{},
		DeregistrationDelay: domain.LbDuration(300 * time.Second),
		SlowStart:           domain.LbDuration(0),
		Status:              domain.TargetGroupStatusActive,
		Port:                8080,
	}
}

// commitWriter — helper: открыть writer, выполнить fn, commit.
func commitWriter(t testing.TB, repo kacho.Repository, fn func(w kacho.RepositoryWriter)) {
	t.Helper()
	ctx := context.Background()
	w, err := repo.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	fn(w)
	require.NoError(t, w.Commit())
}
