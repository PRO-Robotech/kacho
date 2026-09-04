// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionjournal_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowtest"
	"github.com/PRO-Robotech/kacho/pkg/pgtest"
	"github.com/PRO-Robotech/kacho/pkg/subscription"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/shared/quota"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/pg"
	"github.com/PRO-Robotech/kacho/services/storage/internal/subscriptionjournal"
)

const (
	probeProject = "prj-1234567890abcdef"
	probeOther   = "prj-abcdef1234567890"
	// probeZone — зона фикстур. Названа один раз: та же строка, выписанная в
	// каждой пробе, разъезжается с перечнем зон посева молча.
	probeZone = "region-1-a"
	// probeDiskType — класс, который заводит САМА проба.
	//
	// Не из посева каталога: каталог классов у storage — данные СТЕНДА, они
	// заводятся посевом подъёма, а не миграцией, и на пустой базе проб их нет.
	// Своим классом проба не зависит от того, чем стенд засеян сегодня.
	probeDiskType = "block-subscription-fixture"
)

// stand — сервер потока НАСТОЯЩИЙ, за настоящим gRPC, над настоящей схемой
// storage.
//
// Предмет этих проб — «провязан и ОТВЕЧАЕТ», а это проверяется вызовом. Проба,
// читающая объявление, осталась бы зелёной у сервиса, чей журнал разошёлся со
// своей схемой: имя колонки живёт значением, и ошибка в нём наступает первым
// запросом в бою, а не сборкой. У storage к этому добавляется второе: сам
// ПРОИЗВОДИТЕЛЬ строк — триггер базы, и вне поднятой схемы его не существует.
type stand struct {
	pool   *pgxpool.Pool
	client subscriptionv1.InternalSubscriptionServiceClient
}

func newStand(t *testing.T) *stand {
	t.Helper()
	return newStandWithNarrower(t, narrowtest.AllowingAll())
}

// newStandWithNarrower — тот же стенд с ЗАДАННЫМ сужателем.
//
// Отдельный вход нужен там, где предмет пробы — не «доезжает ли событие вообще»,
// а «доезжает ли оно тому, чьи права изменились»: разрешающий всё сужатель по
// построению не отличает эти два вопроса.
func newStandWithNarrower(t *testing.T, narrower *listnarrow.Narrower) *stand {
	t.Helper()
	if testing.Short() {
		t.Skip("интеграционная проба: нужна настоящая база")
	}

	// Схема в строке соединения — ТА ЖЕ, что в бою: боевой DSN несёт
	// `search_path=kacho_storage` (`internal/config`, `baseDSN`), и репозитории
	// адресуют свои таблицы без схемы. Стенд без неё падал бы на фикстуре, а не
	// на предмете пробы; объявлена она `pgtest.Config.SearchPath` этого пакета.
	dsn := pgtest.NewDB(t)
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("пул не собрался: %v", err)
	}
	pgtest.ClosePoolAtEnd(t, pool)
	// Условия, БЕЗ которых боевой путь записи отвергает вставку раньше, чем дело
	// дойдёт до журнала: класс диска, обслуживаемый в зоне, и строка учёта
	// предела. Без них проба падала бы на подготовке и называла виновником
	// невиновного — журнал, до которого исполнение не дошло.
	seedFixtureCatalog(t, pool)
	seedFixtureQuotas(t, pool, probeProject, probeOther)

	gate, err := subscriptionjournal.ProjectGate()
	if err != nil {
		t.Fatalf("страж не собрался: %v", err)
	}
	srv, err := subscription.NewServer(subscription.Config{
		Journal:      subscriptionjournal.Journal(),
		DSN:          dsn,
		Narrower:     narrower,
		ProjectGate:  gate,
		MaxStreams:   4,
		IdlePoll:     150 * time.Millisecond,
		StreamBudget: 30 * time.Second,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("сервер потока не поднялся: %v", err)
	}

	lis := bufconn.Listen(1 << 20)
	// Личность вызывающего кладёт звено — там же, где её кладёт боевая цепочка.
	caller := narrowtest.Caller()
	gsrv := grpc.NewServer(grpc.StreamInterceptor(
		func(s any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, h grpc.StreamHandler) error {
			return h(s, principalStream{ServerStream: ss, ctx: mergeCancel(caller, ss.Context())})
		}))
	subscriptionv1.RegisterInternalSubscriptionServiceServer(gsrv, srv)
	go func() { _ = gsrv.Serve(lis) }()
	t.Cleanup(gsrv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("клиент не собрался: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	return &stand{pool: pool, client: subscriptionv1.NewInternalSubscriptionServiceClient(conn)}
}

type principalStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s principalStream) Context() context.Context { return s.ctx }

func mergeCancel(values, cancel context.Context) context.Context {
	ctx, stop := context.WithCancel(values)
	go func() {
		<-cancel.Done()
		stop()
	}()
	return ctx
}

// seedFixtureCatalog делает класс диска обслуживаемым в зоне проб.
//
// Класс без действующей ревизии привязки в зоне не обслуживает её вовсе, и
// вставка тома отвергается ДО журнала.
func seedFixtureCatalog(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		INSERT INTO disk_types (id, name, lifecycle) VALUES ($1, $1, 'ACTIVE')
		ON CONFLICT (id) DO NOTHING`, probeDiskType); err != nil {
		t.Fatalf("класс диска фикстуры не завёлся: %v", err)
	}
	backendID := ids.NewHyphenID("sb")
	if _, err := pool.Exec(ctx, `
		INSERT INTO storage_backends (id, name, kind, zone_ids, endpoint, credentials_ref)
		VALUES ($1, $1, 'CEPH_RBD', '[]'::jsonb, 'cfg://fixture', 'vault://fixture')`,
		backendID); err != nil {
		t.Fatalf("бэкенд фикстуры не завёлся: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO disk_type_bindings
			(id, disk_type_id, zone_id, backend_id, revision, pool, namespace_template,
			 cap_snapshots, cap_clone_from_snapshot, cap_clone_from_image, cap_online_grow, status)
		VALUES ($1, $2, $3, $4, 1, 'kacho-fixture', '{projectId}', true, true, true, true, 'ACTIVE')`,
		ids.NewHyphenID("dtb"), probeDiskType, probeZone, backendID); err != nil {
		t.Fatalf("привязка класса фикстуры не завелась: %v", err)
	}
}

// fixtureQuotaKinds — три тенантных вида домена, ровно те, на которых висят
// триггеры учёта (0023). Вставка ресурса списывает место у ЛЮБОГО проекта, и без
// строки учёта отвергается «потолок не назван».
var fixtureQuotaKinds = []string{"storage.volumes", "storage.snapshots", "storage.images"}

// seedFixtureQuotas заводит строки учёта названным проектам.
//
// Предел заведомо больше, чем нужно любой пробе пакета: он здесь не предмет
// утверждения, а условие, при котором предмет вообще достижим.
func seedFixtureQuotas(t *testing.T, pool *pgxpool.Pool, projects ...string) {
	t.Helper()
	rows := make([]quota.Row, 0, len(projects)*len(fixtureQuotaKinds))
	for _, p := range projects {
		for _, k := range fixtureQuotaKinds {
			rows = append(rows, quota.Row{
				CarrierType: quota.CarrierProject,
				CarrierID:   p,
				Kind:        k,
				Limit:       1_000_000,
				SourceScope: "DEFAULT",
				AccountID:   "acc-fixture",
			})
		}
	}
	n, err := pg.MaterializeQuotas(context.Background(), pool, rows)
	if err != nil {
		t.Fatalf("фикстура учёта не завелась: %v", err)
	}
	if n != int64(len(rows)) {
		t.Fatalf("перепись фикстуры учёта: заведено %d строк из %d объявленных — часть "+
			"идентичностей уже существовала, то есть проба работает не на свежей базе",
			n, len(rows))
	}
}

// createVolume заводит том ТЕМ ЖЕ репозиторием, каким его заводит сервис.
//
// Не своим `INSERT`: предмет проб — что журнал наполняется БОЕВЫМ путём записи, а
// свой оператор разошёлся бы с боевым молча — и разошёлся бы ровно там, где
// расхождение невидимо.
func (s *stand) createVolume(t *testing.T, projectID, name string) *domain.Volume {
	t.Helper()
	return s.createVolumeWithID(t, ids.NewID(domain.PrefixVolume), projectID, name)
}

// createVolumeWithID — то же с НАЗВАННЫМ идентификатором.
//
// Нужен там, где идентификатор обязан быть известен ДО подъёма стенда: сужатель
// строится вместе с сервером, а решение о показе принимается по идентификатору
// предмета. Проба, разрешающая только проект, не увидела бы и собственный
// положительный контроль.
func (s *stand) createVolumeWithID(t *testing.T, id, projectID, name string) *domain.Volume {
	t.Helper()
	v, _, err := pg.NewVolumeRepo(s.pool).Insert(context.Background(), &domain.Volume{
		ID:         id,
		ProjectID:  projectID,
		Name:       name,
		ZoneID:     probeZone,
		DiskTypeID: probeDiskType,
		SizeBytes:  1 << 30,
	}, "")
	if err != nil {
		t.Fatalf("том не завёлся боевым путём записи: %v", err)
	}
	return v
}

// subscribe открывает поток с НАЗВАННЫМ началом.
//
// Начало названо словом: незаданное по контракту означает «с текущего конца», и
// строки, записанные ДО открытия, не пришли бы вовсе — проба на умолчании
// зеленела бы только по случайности расписания.
func (s *stand) subscribe(
	t *testing.T, ctx context.Context, projectID string, kinds ...string,
) subscriptionv1.InternalSubscriptionService_SubscribeClient {
	t.Helper()
	stream, err := s.client.Subscribe(ctx, &subscriptionv1.SubscriptionRequest{
		Kinds:     kinds,
		ProjectId: projectID,
		Start: &subscriptionv1.SubscriptionRequest_Anchor{
			Anchor: subscriptionv1.SubscriptionAnchor_BEGINNING,
		},
	})
	if err != nil {
		t.Fatalf("подписка не открылась: %v", err)
	}
	return stream
}

// recv читает поток до первого СОБЫТИЯ, пропуская служебное сообщение открытия.
func recv(t *testing.T, stream subscriptionv1.InternalSubscriptionService_SubscribeClient) *subscriptionv1.SubscriptionEvent {
	t.Helper()
	for {
		msg, err := stream.Recv()
		if err != nil {
			t.Fatalf("поток оборвался до события: %v", err)
		}
		if ev := msg.GetEvent(); ev != nil {
			return ev
		}
	}
}
