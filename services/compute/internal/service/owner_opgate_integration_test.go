// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// owner-tuple op-gating (P4) — service-level integration tests (testcontainers
// Postgres 16 + REAL default-registry LRO worker + fake FGA confirm/register port).
//
// Guarantee (acceptance owner-tuple-opgate, OTG-03/-04/-05/-05b/-16):
// Instance/Disk Create-Operation reaches done=true,result=response ONLY after the
// owner-tuple read-after-register confirm succeeds; a client that awaited
// success-done can IMMEDIATELY mutate (Update/Delete) its resource without the 403
// "no direct relations granted" window. Fail-closed: confirm not achieved within
// the confirmation deadline → op.error(codes.Unavailable, "owner-tuple registration
// not confirmed") (NEVER a false success-done), resource-ref durable + discoverable
// on the error terminal.
//
// The fake FGA models owner-tuple propagation lag: Register(object) makes the tuple
// effective after propDelay; both the confirm-probe (service OwnerConfirmer) and the
// modelled gateway scope_extractor Check read the SAME effectiveness map — consistent
// by construction (FIX-2). The worker mechanics themselves are locked at the corelib
// P1 layer (pkg/operations/worker_confirm_test.go); here we lock the compute WIRING.
package service_test

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"google.golang.org/grpc/codes"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/compute/internal/migrations"
	"github.com/PRO-Robotech/kacho/services/compute/internal/ports/portmock"
	"github.com/PRO-Robotech/kacho/services/compute/internal/repo"
	"github.com/PRO-Robotech/kacho/services/compute/internal/service"
)

// testConfirmDeadline — confirmation deadline применяемый ко ВСЕМ confirm-gated
// операциям этого тест-бинарника (ConfigureDefault в TestMain). Щедрый относительно
// propDelay (нормальная пропагация ≪ deadline) → happy-path резолвится задолго до
// него; timeout-ветка (OTG-05) триггерится ровно на нём.
const testConfirmDeadline = 1200 * time.Millisecond

// propDelay — модельная задержка пропагации owner-tuple в FGA. ≫ окна «op.done →
// немедленный gateway Check», поэтому БЕЗ gate немедленная мутация детерминированно
// видит tuple ещё не эффективным (403), а С gate — эффективным (no 403).
const propDelay = 250 * time.Millisecond

var (
	sharedDSN  string
	sharedPool *pgxpool.Pool
)

func TestMain(m *testing.M) {
	// Confirmation deadline для default-registry LRO worker'а — до первого Run
	// (ConfigureDefault обязан предшествовать Start). Влияет ТОЛЬКО на confirm-gated
	// операции (confirm!=nil); прочие тесты бинарника (confirm=nil) не затронуты.
	if err := operations.ConfigureDefault(operations.WithConfirmationDeadline(testConfirmDeadline)); err != nil {
		panic("configure default registry confirm deadline: " + err.Error())
	}
	flag.Parse()
	if testing.Short() {
		os.Exit(m.Run())
	}
	ctx := context.Background()
	pgc, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("kacho_compute_test"),
		postgres.WithUsername("compute"),
		postgres.WithPassword("compute"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		panic("start postgres: " + err.Error())
	}
	dsn, err := pgc.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		panic("dsn: " + err.Error())
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		panic("sql.Open: " + err.Error())
	}
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		panic(err)
	}
	if err := goose.Up(db, "."); err != nil {
		panic("migrate: " + err.Error())
	}
	_ = db.Close()
	pool, err := coredb.NewPool(ctx, dsn)
	if err != nil {
		panic("pool: " + err.Error())
	}
	sharedDSN, sharedPool = dsn, pool

	code := m.Run()

	pool.Close()
	_ = pgc.Terminate(ctx)
	os.Exit(code)
}

// ---- fake FGA (shared confirm/register + gateway-Check model) --------------

type fgaCall struct{ subject, relation, object string }

// fakeFGA моделирует owner-tuple propagation в FGA: Register делает object
// эффективным через propDelay; Check(subject,relation,object) читает ту же
// effectiveness-map, что и confirm-проба и модельный gateway scope_extractor
// (consistency by construction, FIX-2). outage=true → Register no-op (moderates
// IAM/FGA outage, OTG-05: tuple никогда не становится эффективным).
type fakeFGA struct {
	mu     sync.Mutex
	effAt  map[string]time.Time
	calls  []fgaCall
	outage bool
	prop   time.Duration
}

func newFakeFGA(prop time.Duration) *fakeFGA {
	return &fakeFGA{effAt: map[string]time.Time{}, prop: prop}
}

func (f *fakeFGA) register(object string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.outage {
		return
	}
	if _, ok := f.effAt[object]; !ok {
		f.effAt[object] = time.Now().Add(f.prop)
	}
}

// effective — эффективен ли tuple ПРЯМО СЕЙЧАС (без записи call'а — для
// gateway-модели/assert'ов, не confirm-пробы).
func (f *fakeFGA) effective(object string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.effAt[object]
	return ok && !time.Now().Before(t)
}

// check — confirm-проба (service OwnerConfirmer.Check). Записывает call.
func (f *fakeFGA) check(subject, relation, object string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fgaCall{subject, relation, object})
	t, ok := f.effAt[object]
	return ok && !time.Now().Before(t)
}

func (f *fakeFGA) snapshotCalls() []fgaCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fgaCall, len(f.calls))
	copy(out, f.calls)
	return out
}

func (f *fakeFGA) resetCalls() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = nil
}

// fakeConfirmer — service.OwnerConfirmer поверх fakeFGA.check.
type fakeConfirmer struct{ fga *fakeFGA }

func (c fakeConfirmer) Check(_ context.Context, subject, relation, object string) (bool, error) {
	return c.fga.check(subject, relation, object), nil
}

// fakeRegistrar — service.OwnerRegistrar поверх fakeFGA.register (моделирует
// sync-registrar, делающий owner-tuple эффективным).
type fakeRegistrar struct{ fga *fakeFGA }

func (r fakeRegistrar) Register(_ context.Context, kind, resourceID, _ string, _ map[string]string) error {
	r.fga.register(objectFor(kind, resourceID))
	return nil
}

func objectFor(kind, id string) string {
	switch kind {
	case "Instance":
		return "compute_instance:" + id
	case "Disk":
		return "compute_disk:" + id
	default:
		return ""
	}
}

// ---- harness ---------------------------------------------------------------

const creatorPrincipalID = "usr_creator"
const creatorSubject = "user:" + creatorPrincipalID

func creatorCtx() context.Context {
	return operations.WithPrincipal(context.Background(),
		operations.Principal{Type: "user", ID: creatorPrincipalID})
}

type opgateEnv struct {
	pool     *pgxpool.Pool
	instRepo *repo.InstanceRepo
	diskRepo *repo.DiskRepo
	imgRepo  *repo.ImageRepo
	snapRepo *repo.SnapshotRepo
	dtRepo   *repo.DiskTypeRepo
	opsRepo  operations.Repo
	fga      *fakeFGA
}

func newEnv(t *testing.T) *opgateEnv {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test (short)")
	}
	require.NotNil(t, sharedPool, "shared pool not initialised (TestMain)")
	return &opgateEnv{
		pool:     sharedPool,
		instRepo: repo.NewInstanceRepo(sharedPool),
		diskRepo: repo.NewDiskRepo(sharedPool),
		imgRepo:  repo.NewImageRepo(sharedPool),
		snapRepo: repo.NewSnapshotRepo(sharedPool),
		dtRepo:   repo.NewDiskTypeRepo(sharedPool),
		opsRepo:  operations.NewRepo(sharedPool, "public"),
		fga:      newFakeFGA(propDelay),
	}
}

// instanceSvc собирает InstanceService поверх real repo/opsRepo; opgate on →
// WithOwnerOpgate(confirmer, registrar), off → без него (baseline).
func (e *opgateEnv) instanceSvc(opgateOn bool) *service.InstanceService {
	svc := service.NewInstanceService(e.instRepo, portmock.NewZoneRegistry(),
		&portmock.ProjectClient{OK: true}, portmock.NewNicClient(), portmock.NewStorageClient(), e.opsRepo)
	if opgateOn {
		svc.WithOwnerOpgate(fakeConfirmer{e.fga}, fakeRegistrar{e.fga})
	}
	return svc
}

func (e *opgateEnv) diskSvc(opgateOn bool) *service.DiskService {
	svc := service.NewDiskService(e.diskRepo, e.imgRepo, e.snapRepo, e.dtRepo,
		portmock.NewZoneRegistry(), &portmock.ProjectClient{OK: true}, e.opsRepo)
	if opgateOn {
		svc.WithOwnerOpgate(fakeConfirmer{e.fga}, fakeRegistrar{e.fga})
	}
	return svc
}

// reqSeq — уникализирует resource-name per Create (UNIQUE(project_id,name) на общем
// pool'е между тестами/итерациями → иначе повторный Create → AlreadyExists).
var reqSeq atomic.Int64

func validInstanceReq() service.CreateInstanceReq {
	return service.CreateInstanceReq{
		ProjectID: "prj-otg", Name: fmt.Sprintf("vm-otg-%d", reqSeq.Add(1)),
		ZoneID: "ru-central1-a", PlatformID: "standard-v3",
		Cores: 2, Memory: 2 << 30, CoreFraction: 100,
	}
}

func validDiskReq() service.CreateDiskReq {
	return service.CreateDiskReq{
		ProjectID: "prj-otg", Name: fmt.Sprintf("disk-otg-%d", reqSeq.Add(1)),
		ZoneID: "ru-central1-a", Size: 4194304,
	}
}

func awaitOpTerminal(t *testing.T, opsRepo operations.Repo, id string, timeout time.Duration) *operations.Operation {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		op, err := opsRepo.Get(context.Background(), id)
		if err == nil && op.Done {
			return op
		}
		if time.Now().After(deadline) {
			t.Fatalf("operation %s did not reach terminal within %s", id, timeout)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func metaInstanceID(t *testing.T, op *operations.Operation) string {
	t.Helper()
	require.NotNil(t, op.Metadata, "op.Metadata nil — resource-ref must be present on ALL terminals")
	m, err := operations.MetadataFor[*computev1.CreateInstanceMetadata](op)
	require.NoError(t, err)
	return m.GetInstanceId()
}

func metaDiskID(t *testing.T, op *operations.Operation) string {
	t.Helper()
	require.NotNil(t, op.Metadata)
	m, err := operations.MetadataFor[*computev1.CreateDiskMetadata](op)
	require.NoError(t, err)
	return m.GetDiskId()
}

func registerIntentCount(t *testing.T, pool *pgxpool.Pool, resourceID string) int {
	t.Helper()
	var n int
	err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM compute_fga_register_outbox WHERE resource_id=$1 AND event_type='fga.register'`,
		resourceID).Scan(&n)
	require.NoError(t, err)
	return n
}

// ---- OTG-03 — op.done(success) наступает ТОЛЬКО после confirm ---------------

func TestInstance_OTG03_OpDoneOnlyAfterOwnerTupleConfirm(t *testing.T) {
	env := newEnv(t)
	svc := env.instanceSvc(true)

	op, err := svc.Create(creatorCtx(), validInstanceReq())
	require.NoError(t, err)
	instID := metaInstanceID(t, op)
	object := objectFor("Instance", instID)

	// Дожидаемся коммита ресурс-строки (fn/doCreate завершился внутри writer-tx) —
	// с этого момента op находится в confirm-PENDING (owner-tuple ещё пропагируется).
	rowSeen := false
	rowDeadline := time.Now().Add(time.Second)
	for time.Now().Before(rowDeadline) {
		if r, rerr := env.instRepo.Get(context.Background(), instID); rerr == nil {
			require.Equal(t, instID, r.ID)
			rowSeen = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	require.True(t, rowSeen, "resource row must be committed (writer-tx) before the confirm-gate")

	// В момент, когда строка + register-intent durable, op ещё PENDING (done=false),
	// т.к. owner-tuple не эффективен (propDelay не истёк) — success-done НЕ выставлен.
	cur, gerr := env.opsRepo.Get(context.Background(), op.ID)
	require.NoError(t, gerr)
	require.False(t, cur.Done,
		"Create-op must be PENDING (done=false) while owner-tuple confirm has not yet returned ALLOW")
	require.False(t, env.fga.effective(object), "owner-tuple not yet effective during PENDING window")
	require.GreaterOrEqual(t, registerIntentCount(t, env.pool, instID), 1,
		"register-intent durable during PENDING (writer-tx outbox)")

	// Как только tuple становится эффективным → op в течение confirm-окна становится
	// done=true,result=response.
	done := awaitOpTerminal(t, env.opsRepo, op.ID, 2*time.Second)
	require.Nil(t, done.Error, "must be success terminal, got error=%v", done.Error)
	require.NotNil(t, done.Response, "success terminal carries response")

	// Ordering: done(success) НЕ предшествует первому ALLOW confirm-пробы — на момент
	// done owner-tuple эффективен.
	require.True(t, env.fga.effective(object),
		"owner-tuple MUST be effective at success-done (done never precedes confirm ALLOW)")

	// Confirm-проба несла корректный (subject, relation=v_update, object) — реплика
	// gateway scope_extractor немедленной мутации.
	requireConfirmProbe(t, env.fga.snapshotCalls(), creatorSubject, "v_update", object)
}

func requireConfirmProbe(t *testing.T, calls []fgaCall, subject, relation, object string) {
	t.Helper()
	for _, c := range calls {
		if c.subject == subject && c.relation == relation && c.object == object {
			return
		}
	}
	t.Fatalf("no confirm probe with subject=%q relation=%q object=%q; got=%v", subject, relation, object, calls)
}

// ---- OTG-04 (CRIT) — нет окна 403 «no direct relations» между done и мутацией -

func TestInstance_OTG04_NoDirectRelations403Window(t *testing.T) {
	// Baseline (opgate OFF): op.done обгоняет пропагацию owner-tuple → немедленная
	// мутация видит tuple ещё не эффективным (403). Дискриминатор: доказывает, что
	// тест ловит именно окно (при снятом gate он ВОСПРОИЗВОДится).
	t.Run("opgate_off reproduces the 403 window (regression baseline)", func(t *testing.T) {
		env := newEnv(t)
		svc := env.instanceSvc(false)
		op, err := svc.Create(creatorCtx(), validInstanceReq())
		require.NoError(t, err)
		instID := metaInstanceID(t, op)
		object := objectFor("Instance", instID)
		// Модель async drainer: owner-tuple ЗАРЕГИСТРИРОВАН, но станет эффективен
		// только через propDelay.
		env.fga.register(object)

		done := awaitOpTerminal(t, env.opsRepo, op.ID, 2*time.Second)
		require.Nil(t, done.Error, "without gate Create succeeds immediately")
		// Немедленная мутация создателем (gateway scope_extractor Check) — окно 403.
		allowed := env.fga.effective(object)
		require.False(t, allowed,
			"opgate OFF: owner-tuple not yet effective at op.done → immediate mutate is DENIED (the 403 window)")
	})

	// opgate ON: op.done ждёт confirm → немедленная мутация всегда ALLOW, во всех N
	// итерациях. Regression-lock на КОД+ТЕКСТ отсутствия 403 (не «Update прошёл»).
	t.Run("opgate_on closes the 403 window (N iterations)", func(t *testing.T) {
		const N = 3
		for i := 0; i < N; i++ {
			env := newEnv(t)
			svc := env.instanceSvc(true)
			op, err := svc.Create(creatorCtx(), validInstanceReq())
			require.NoError(t, err)
			instID := metaInstanceID(t, op)
			object := objectFor("Instance", instID)

			done := awaitOpTerminal(t, env.opsRepo, op.ID, 2*time.Second)
			require.Nilf(t, done.Error, "iter %d: gated Create must succeed (owner-tuple confirmed)", i)
			require.NotNil(t, done.Response)

			// Немедленная мутация создателем — тот же consistency-read, что confirm
			// (FIX-2): если confirm видит tuple, gateway Check тоже → ALLOW.
			require.Truef(t, env.fga.effective(object),
				"iter %d: after success-done immediate mutate MUST NOT get 403 'no direct relations granted'", i)
		}
	})
}

// ---- OTG-05 (CRIT) — confirm timeout → op.error(Unavailable), no success-done -

func TestInstance_OTG05_ConfirmTimeoutFailClosed(t *testing.T) {
	env := newEnv(t)
	env.fga.outage = true // owner-tuple никогда не станет эффективным (IAM/FGA outage)
	svc := env.instanceSvc(true)

	op, err := svc.Create(creatorCtx(), validInstanceReq())
	require.NoError(t, err)
	instID := metaInstanceID(t, op)

	// До deadline op не должен ложно-succeed.
	early, gerr := env.opsRepo.Get(context.Background(), op.ID)
	require.NoError(t, gerr)
	if early.Done {
		require.NotNil(t, early.Error, "before deadline op must not be a false success-done")
	}

	done := awaitOpTerminal(t, env.opsRepo, op.ID, testConfirmDeadline+3*time.Second)
	require.NotNil(t, done.Error, "confirm timeout must terminate op with error, not success")
	require.Equal(t, codes.Unavailable, codes.Code(done.Error.Code),
		"FIX-1: timeout code is Unavailable")
	require.NotEqual(t, codes.DeadlineExceeded, codes.Code(done.Error.Code),
		"FIX-1: DeadlineExceeded explicitly rejected")
	require.Equal(t, "owner-tuple registration not confirmed", done.Error.Message,
		"stable contract text (part of the contract)")
	require.Nil(t, done.Response, "no success-response on the timeout terminal")

	// Ресурс-строка + register-intent durable (не откачены timeout'ом).
	row, rerr := env.instRepo.Get(context.Background(), instID)
	require.NoError(t, rerr, "resource row durable across timeout")
	require.Equal(t, instID, row.ID)
	require.GreaterOrEqual(t, registerIntentCount(t, env.pool, instID), 1,
		"register-intent durable across timeout")
}

// ---- OTG-05b (CRIT, orphan-guard) — resource-ref discoverable on error terminal

func TestInstance_OTG05b_TimeoutResourceRefDiscoverable(t *testing.T) {
	env := newEnv(t)
	env.fga.outage = true
	svc := env.instanceSvc(true)

	op, err := svc.Create(creatorCtx(), validInstanceReq())
	require.NoError(t, err)

	done := awaitOpTerminal(t, env.opsRepo, op.ID, testConfirmDeadline+3*time.Second)
	require.NotNil(t, done.Error)
	require.Equal(t, codes.Unavailable, codes.Code(done.Error.Code))

	// FIX-3: resource-ref (CreateInstanceMetadata.instance_id) обнаружим НА
	// error-терминале (populated at op-creation, не только на success).
	ref := metaInstanceID(t, done)
	require.NotEmpty(t, ref, "resource-ref must be present on the error terminal")

	// Get(ref) → 200 (ресурс durable, не orphan-без-id).
	row, rerr := env.instRepo.Get(context.Background(), ref)
	require.NoError(t, rerr, "resource durable and discoverable by op.metadata ref")
	require.Equal(t, ref, row.ID)

	// «Recovery»: register-drainer/IAM восстанавливается → owner-tuple эффективен →
	// повторная мутация создателем того же ref → НЕ 403 (клиент НЕ пере-создаёт).
	env.fga.outage = false
	env.fga.prop = 0
	env.fga.register(objectFor("Instance", ref))
	require.True(t, env.fga.effective(objectFor("Instance", ref)),
		"after drainer backstop applies the owner-tuple, the retried mutate is NOT 403")
}

// ---- OTG-16 — Update/Delete существующего НЕ gated --------------------------

func TestInstance_OTG16_UpdateDeleteExistingNotGated(t *testing.T) {
	env := newEnv(t)
	svc := env.instanceSvc(true)

	// Seed через gated Create + дождаться confirmed done (owner-tuple эффективен).
	cop, err := svc.Create(creatorCtx(), validInstanceReq())
	require.NoError(t, err)
	instID := metaInstanceID(t, cop)
	cdone := awaitOpTerminal(t, env.opsRepo, cop.ID, 2*time.Second)
	require.Nil(t, cdone.Error)

	// Confirm-порт НЕ должен вызываться на Update/Delete существующего ресурса.
	env.fga.resetCalls()

	uop, err := svc.Update(creatorCtx(), service.UpdateInstanceReq{
		InstanceID: instID, Description: "upd", UpdateMask: []string{"description"},
	})
	require.NoError(t, err)
	udone := awaitOpTerminal(t, env.opsRepo, uop.ID, 2*time.Second)
	require.Nil(t, udone.Error, "Update of existing must succeed without a confirm-gate")
	require.Empty(t, env.fga.snapshotCalls(),
		"OTG-16: Update of an EXISTING resource must NOT invoke the owner-tuple confirm-probe")

	dop, err := svc.Delete(creatorCtx(), instID)
	require.NoError(t, err)
	ddone := awaitOpTerminal(t, env.opsRepo, dop.ID, 2*time.Second)
	require.Nil(t, ddone.Error, "Delete of existing must succeed without a confirm-gate")
	require.Empty(t, env.fga.snapshotCalls(),
		"OTG-16: Delete of an EXISTING resource must NOT invoke the owner-tuple confirm-probe")
}

// ---- Disk parity (OTG-04 / OTG-05 / OTG-16) --------------------------------

func TestDisk_OTG04_NoDirectRelations403Window(t *testing.T) {
	t.Run("opgate_off reproduces the 403 window", func(t *testing.T) {
		env := newEnv(t)
		svc := env.diskSvc(false)
		op, err := svc.Create(creatorCtx(), validDiskReq())
		require.NoError(t, err)
		diskID := metaDiskID(t, op)
		object := objectFor("Disk", diskID)
		env.fga.register(object)
		done := awaitOpTerminal(t, env.opsRepo, op.ID, 2*time.Second)
		require.Nil(t, done.Error)
		require.False(t, env.fga.effective(object),
			"opgate OFF: compute_disk owner-tuple not effective at op.done → 403 window")
	})
	t.Run("opgate_on closes the 403 window", func(t *testing.T) {
		env := newEnv(t)
		svc := env.diskSvc(true)
		op, err := svc.Create(creatorCtx(), validDiskReq())
		require.NoError(t, err)
		diskID := metaDiskID(t, op)
		object := objectFor("Disk", diskID)
		done := awaitOpTerminal(t, env.opsRepo, op.ID, 2*time.Second)
		require.Nil(t, done.Error)
		require.NotNil(t, done.Response)
		require.True(t, env.fga.effective(object),
			"after success-done immediate mutate of compute_disk MUST NOT get 403")
		requireConfirmProbe(t, env.fga.snapshotCalls(), creatorSubject, "v_update", object)
	})
}

func TestDisk_OTG05_ConfirmTimeoutFailClosed(t *testing.T) {
	env := newEnv(t)
	env.fga.outage = true
	svc := env.diskSvc(true)

	op, err := svc.Create(creatorCtx(), validDiskReq())
	require.NoError(t, err)
	diskID := metaDiskID(t, op)

	done := awaitOpTerminal(t, env.opsRepo, op.ID, testConfirmDeadline+3*time.Second)
	require.NotNil(t, done.Error)
	require.Equal(t, codes.Unavailable, codes.Code(done.Error.Code))
	require.NotEqual(t, codes.DeadlineExceeded, codes.Code(done.Error.Code))
	require.Equal(t, "owner-tuple registration not confirmed", done.Error.Message)
	require.Nil(t, done.Response)

	row, rerr := env.diskRepo.Get(context.Background(), diskID)
	require.NoError(t, rerr, "disk row durable across timeout")
	require.Equal(t, diskID, row.ID)
	require.GreaterOrEqual(t, registerIntentCount(t, env.pool, diskID), 1)
}

func TestDisk_OTG16_UpdateDeleteExistingNotGated(t *testing.T) {
	env := newEnv(t)
	svc := env.diskSvc(true)

	cop, err := svc.Create(creatorCtx(), validDiskReq())
	require.NoError(t, err)
	diskID := metaDiskID(t, cop)
	cdone := awaitOpTerminal(t, env.opsRepo, cop.ID, 2*time.Second)
	require.Nil(t, cdone.Error)

	env.fga.resetCalls()

	uop, err := svc.Update(creatorCtx(), service.UpdateDiskReq{
		DiskID: diskID, Description: "upd", UpdateMask: []string{"description"},
	})
	require.NoError(t, err)
	udone := awaitOpTerminal(t, env.opsRepo, uop.ID, 2*time.Second)
	require.Nil(t, udone.Error)
	assert.Empty(t, env.fga.snapshotCalls(),
		"OTG-16: Disk.Update of an existing resource must NOT invoke the confirm-probe")

	dop, err := svc.Delete(creatorCtx(), diskID)
	require.NoError(t, err)
	ddone := awaitOpTerminal(t, env.opsRepo, dop.ID, 2*time.Second)
	require.Nil(t, ddone.Error)
	assert.Empty(t, env.fga.snapshotCalls(),
		"OTG-16: Disk.Delete of an existing resource must NOT invoke the confirm-probe")
}

var _ = sharedDSN // reserved for future per-test schema isolation
