// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzformbench

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver for the volume query
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Pins. Both are overridable by environment so a run can be repeated against the
// version a stand actually carries; the DEFAULTS are what this tree already pulls,
// and the report prints whichever was used.
//
// openfga/openfga:v1.8.4 is the pin services/iam/internal/testsupport/fgatest
// carries, so the shapes are measured against the same engine the tree's other
// real-FGA proofs run against. Note the tree is not of one mind about this:
// services/iam/internal/authzfilter/visibility.go says its BatchCheck ceiling was
// "read off the deployed build (openfga/openfga:v1.14.0)". Two places about one
// subject; this harness does not settle it, it MEASURES the ceiling at runtime
// (probeBatchCap) and prints what the engine under test actually answered.
const (
	defaultOpenFGAImage = "openfga/openfga:v1.8.4"
	defaultCLIImage     = "openfga/cli:v0.7.13"
	defaultPostgres     = "postgres:16-alpine"

	pgUser = "openfga"
	pgPass = "openfga"
	pgDB   = "openfga"
)

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// Stack is one OpenFGA over one Postgres on a private docker network, plus the
// handle on that Postgres the volume measurement needs.
//
// One stack per process, like fgatest's one server per test binary, for the same
// reason: the isolation a container gives is already given by a STORE, and paying
// for a container per shape would dwarf what is being measured.
type Stack struct {
	HTTPBase string  // http://host:port — the OpenFGA HTTP API
	DB       *sql.DB // the OpenFGA datastore, for tuple-bytes accounting
	OpenFGA  string  // image actually used
	Postgres string  // image actually used
	BatchCap int     // measured, not assumed: the engine's BatchCheck ceiling

	// RelDSN — СВОЙ Postgres формы E, отдельным контейнером.
	//
	// Отдельным, а не второй базой в датасторе движка, по двум причинам сразу, и
	// вторая дороже первой: объём снимается по таблицам (смешались бы величины),
	// а буферы, занятые кортежами движка, наказывали бы чтение формы E за чужие
	// данные — и это выглядело бы свойством формы, а не посадки.
	RelDSN      string
	RelPostgres string

	// StmtProducer — состояние производителя `StmtSQL` со стороны движка,
	// снятое контролем в обе стороны при подъёме стека.
	StmtProducer ProducerStatus
	stmts        *pgStmtCounter

	terminate func()
}

var (
	stackOnce sync.Once
	stackVal  *Stack
	stackErr  error
)

// SharedStack boots the stack on first use and returns it to every caller.
//
// Failure to boot is returned to EVERY caller, never only to the one that lost the
// race to run the Once — the rest would otherwise proceed against an empty address
// and report a shape as "slow" when nothing was running.
func SharedStack(ctx context.Context) (*Stack, error) {
	stackOnce.Do(func() { stackVal, stackErr = bootStack(ctx) })
	return stackVal, stackErr
}

// CloseSharedStack terminates the containers. Safe to call when nothing booted.
func CloseSharedStack() {
	if stackVal != nil && stackVal.terminate != nil {
		stackVal.terminate()
		stackVal.terminate = nil
	}
}

func bootStack(ctx context.Context) (*Stack, error) {
	fgaImage := envOr("AUTHZFORMBENCH_FGA_IMAGE", defaultOpenFGAImage)
	pgImage := envOr("AUTHZFORMBENCH_PG_IMAGE", defaultPostgres)

	var stop []func()
	undo := func() {
		for i := len(stop) - 1; i >= 0; i-- {
			stop[i]()
		}
	}

	net, err := network.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("create network: %w", err)
	}
	stop = append(stop, func() { _ = net.Remove(context.Background()) })

	// ── Postgres ────────────────────────────────────────────────────────────────
	// Plain GenericContainer rather than the postgres module: the module's
	// ConnectionString is host-side, and OpenFGA needs the CONTAINER-side address on
	// the shared network. Both are wanted here, so the addresses are built
	// explicitly instead of half-derived.
	pgReq := testcontainers.ContainerRequest{
		Image:        pgImage,
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     pgUser,
			"POSTGRES_PASSWORD": pgPass,
			"POSTGRES_DB":       pgDB,
		},
		// Предзагрузка библиотеки статистики стейтментов — единственный способ
		// узнать, сколько запросов движок посылает своему Postgres за одно HTTP-
		// обращение. Правится СВОЙ контейнер харнесса, чужой прод-код не трогается.
		Cmd:            []string{"postgres", "-c", "shared_preload_libraries=pg_stat_statements"},
		Networks:       []string{net.Name},
		NetworkAliases: map[string][]string{net.Name: {"benchpg"}},
		WaitingFor: wait.ForListeningPort("5432/tcp").
			WithStartupTimeout(90 * time.Second),
	}
	pgc, err := testcontainers.GenericContainer(ctx,
		testcontainers.GenericContainerRequest{ContainerRequest: pgReq, Started: true})
	if err != nil {
		undo()
		return nil, fmt.Errorf("start postgres: %w", err)
	}
	stop = append(stop, func() { _ = pgc.Terminate(context.Background()) })

	pgHost, err := pgc.Host(ctx)
	if err != nil {
		undo()
		return nil, fmt.Errorf("postgres host: %w", err)
	}
	pgPort, err := pgc.MappedPort(ctx, "5432")
	if err != nil {
		undo()
		return nil, fmt.Errorf("postgres port: %w", err)
	}
	hostDSN := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		pgUser, pgPass, pgHost, pgPort.Port(), pgDB)
	inNetDSN := fmt.Sprintf("postgres://%s:%s@benchpg:5432/%s?sslmode=disable",
		pgUser, pgPass, pgDB)

	db, err := sql.Open("pgx", hostDSN)
	if err != nil {
		undo()
		return nil, fmt.Errorf("open datastore: %w", err)
	}
	stop = append(stop, func() { _ = db.Close() })
	if err := waitDB(ctx, db); err != nil {
		undo()
		return nil, err
	}

	// ── OpenFGA migrate (one-shot) ──────────────────────────────────────────────
	// The deployed chart runs this as an initContainer (values.dev.yaml
	// openfga.datastore.migrationType: initContainer); here it is a one-shot
	// container that must EXIT 0 before the server starts. A server started against
	// an unmigrated datastore fails on first write, which would read as "shape A is
	// slow" instead of "nothing was measured".
	migrate := testcontainers.ContainerRequest{
		Image:      fgaImage,
		Cmd:        []string{"migrate"},
		Env:        map[string]string{"OPENFGA_DATASTORE_ENGINE": "postgres", "OPENFGA_DATASTORE_URI": inNetDSN},
		Networks:   []string{net.Name},
		WaitingFor: wait.ForExit().WithExitTimeout(120 * time.Second),
	}
	mc, err := testcontainers.GenericContainer(ctx,
		testcontainers.GenericContainerRequest{ContainerRequest: migrate, Started: true})
	if err != nil {
		undo()
		return nil, fmt.Errorf("openfga migrate: %w", err)
	}
	mstate, err := mc.State(ctx)
	_ = mc.Terminate(context.Background())
	if err != nil {
		undo()
		return nil, fmt.Errorf("openfga migrate state: %w", err)
	}
	if mstate.ExitCode != 0 {
		undo()
		return nil, fmt.Errorf("openfga migrate exited %d", mstate.ExitCode)
	}

	// ── OpenFGA server ──────────────────────────────────────────────────────────
	fgaReq := testcontainers.ContainerRequest{
		Image:        fgaImage,
		Cmd:          []string{"run"},
		ExposedPorts: []string{"8080/tcp"},
		Env: map[string]string{
			"OPENFGA_DATASTORE_ENGINE": "postgres",
			"OPENFGA_DATASTORE_URI":    inNetDSN,
		},
		Networks: []string{net.Name},
		WaitingFor: wait.ForHTTP("/healthz").WithPort("8080/tcp").
			WithStartupTimeout(120 * time.Second),
	}
	fgac, err := testcontainers.GenericContainer(ctx,
		testcontainers.GenericContainerRequest{ContainerRequest: fgaReq, Started: true})
	if err != nil {
		undo()
		return nil, fmt.Errorf("start openfga: %w", err)
	}
	stop = append(stop, func() { _ = fgac.Terminate(context.Background()) })

	fHost, err := fgac.Host(ctx)
	if err != nil {
		undo()
		return nil, fmt.Errorf("openfga host: %w", err)
	}
	fPort, err := fgac.MappedPort(ctx, "8080")
	if err != nil {
		undo()
		return nil, fmt.Errorf("openfga port: %w", err)
	}

	// ── Postgres формы E ────────────────────────────────────────────────────────
	// Свой контейнер, а не вторая база в датасторе движка: иначе смешались бы
	// объём и нагрузка, и «форма E читает медленно» было бы неотличимо от
	// «буферы заняты чужими кортежами».
	relReq := testcontainers.ContainerRequest{
		Image:        pgImage,
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     pgUser,
			"POSTGRES_PASSWORD": pgPass,
			"POSTGRES_DB":       pgDB,
		},
		WaitingFor: wait.ForListeningPort("5432/tcp").
			WithStartupTimeout(90 * time.Second),
	}
	relc, err := testcontainers.GenericContainer(ctx,
		testcontainers.GenericContainerRequest{ContainerRequest: relReq, Started: true})
	if err != nil {
		undo()
		return nil, fmt.Errorf("start postgres формы E: %w", err)
	}
	stop = append(stop, func() { _ = relc.Terminate(context.Background()) })
	relHost, err := relc.Host(ctx)
	if err != nil {
		undo()
		return nil, fmt.Errorf("postgres формы E, хост: %w", err)
	}
	relPort, err := relc.MappedPort(ctx, "5432")
	if err != nil {
		undo()
		return nil, fmt.Errorf("postgres формы E, порт: %w", err)
	}

	s := &Stack{
		HTTPBase:    fmt.Sprintf("http://%s:%s", fHost, fPort.Port()),
		DB:          db,
		OpenFGA:     fgaImage,
		Postgres:    pgImage,
		RelPostgres: pgImage,
		RelDSN: fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
			pgUser, pgPass, relHost, relPort.Port(), pgDB),
		terminate: undo,
	}

	// Производитель `StmtSQL` со стороны движка заводится ЗДЕСЬ и сразу проходит
	// контроль в обе стороны. Непрошедший контроль стек не роняет: колонка тогда
	// просто не печатается (не ноль и не прочерк), а формулировка «на общем для
	// форм уровне» из отчёта снимается — исход назван заранее и не является
	// выбором исполнителя.
	s.stmts, s.StmtProducer = VerifyEngineStmtProducer(ctx, db)
	return s, nil
}

func waitDB(ctx context.Context, db *sql.DB) error {
	deadline := time.Now().Add(60 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		c, cancel := context.WithTimeout(ctx, 3*time.Second)
		last = db.PingContext(c)
		cancel()
		if last == nil {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("datastore never became reachable: %w", last)
}

// TupleBytes reports, for ONE store, the exact tuple count and the logical bytes
// Postgres accounts to those rows.
//
// pg_column_size sums the row's own storage and excludes index overhead — which is
// table-wide and therefore cannot be attributed to a store without lying. The
// whole-table figure is returned separately so a reader can see both and neither is
// presented as the other.
//
// Структурная часть выделена ОТДЕЛЬНОЙ парой величин (правка XC-10): счёт строк
// её вычитает, а байты — нет, и эта асимметрия базы сравнения молча переезжала бы
// на шестую форму. Теперь она хотя бы видна: структурные строки и их байты
// печатаются рядом, и читатель вправе вычесть их сам. Правило подсчёта у шестой
// формы то же самое — иначе колонка была бы сопоставима только на бумаге.
func (s *Stack) TupleBytes(ctx context.Context, storeID string) (
	count, rowBytes, structRows, structBytes, tableTotal int64, err error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT count(*),
		       COALESCE(sum(pg_column_size(t.*)), 0),
		       count(*) FILTER (WHERE t.relation IN ('cluster', 'account', 'project')),
		       COALESCE(sum(pg_column_size(t.*)) FILTER (WHERE t.relation IN ('cluster', 'account', 'project')), 0)
		  FROM tuple t WHERE t.store = $1`, storeID)
	if err = row.Scan(&count, &rowBytes, &structRows, &structBytes); err != nil {
		return 0, 0, 0, 0, 0, fmt.Errorf("tuple bytes for store %s: %w", storeID, err)
	}
	if err = s.DB.QueryRowContext(ctx,
		`SELECT pg_total_relation_size('tuple')`).Scan(&tableTotal); err != nil {
		return count, rowBytes, structRows, structBytes, 0, fmt.Errorf("tuple table size: %w", err)
	}
	return count, rowBytes, structRows, structBytes, tableTotal, nil
}
