// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"

	operationv1 "github.com/PRO-Robotech/corelib/api/corelib/operation"
	"github.com/PRO-Robotech/corelib/authz/authzmetrics"
	"github.com/PRO-Robotech/corelib/grpcclient"
	"github.com/PRO-Robotech/corelib/grpcsrv"
	"github.com/PRO-Robotech/corelib/listnarrow"
	"github.com/PRO-Robotech/corelib/listnarrow/narrowmetrics"
	"github.com/PRO-Robotech/corelib/observability"
	"github.com/PRO-Robotech/corelib/operations"
	"github.com/PRO-Robotech/corelib/operations/operationspb"
	"github.com/PRO-Robotech/corelib/servicehost"
	"github.com/PRO-Robotech/kacho/pkg/authz/authziam"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowiam"

	notifyv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/notify/v1"

	"github.com/PRO-Robotech/kacho/services/notify/cmd/notify-api/internal/authzwiring"
	"github.com/PRO-Robotech/kacho/services/notify/cmd/notify-api/internal/config"
	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/kacho/api/notice"
	noticecancel "github.com/PRO-Robotech/kacho/services/notify/internal/apps/kacho/api/notice/cancel"
	noticecomplete "github.com/PRO-Robotech/kacho/services/notify/internal/apps/kacho/api/notice/complete"
	noticecreate "github.com/PRO-Robotech/kacho/services/notify/internal/apps/kacho/api/notice/create"
	noticeget "github.com/PRO-Robotech/kacho/services/notify/internal/apps/kacho/api/notice/get"
	noticelist "github.com/PRO-Robotech/kacho/services/notify/internal/apps/kacho/api/notice/list"
	noticestart "github.com/PRO-Robotech/kacho/services/notify/internal/apps/kacho/api/notice/start"
	noticeupdate "github.com/PRO-Robotech/kacho/services/notify/internal/apps/kacho/api/notice/update"
	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/kacho/api/publicnotice"
	publicget "github.com/PRO-Robotech/kacho/services/notify/internal/apps/kacho/api/publicnotice/get"
	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/kacho/api/publicnotice/getbyaccount"
	publiclist "github.com/PRO-Robotech/kacho/services/notify/internal/apps/kacho/api/publicnotice/list"
	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/kacho/api/publicnotice/listbyaccount"
	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/kacho/authzcheck"
	"github.com/PRO-Robotech/kacho/services/notify/internal/handler"
	"github.com/PRO-Robotech/kacho/services/notify/internal/repo/noticerepo"
)

// operationsSchema — схема таблицы операций в kacho_notify: миграции notify
// создают свои таблицы в схеме по умолчанию.
const operationsSchema = "public"

// apiRuntime — части корня notify-api, которые не ручки: пул kacho_notify,
// часы notify (они же часы сужателя, З14 п.1), реестр метрик и журнал. Значения
// ручек приходят отдельно — типом конфигурации, тем же, что разбирает загрузчик.
type apiRuntime struct {
	Pool    *pgxpool.Pool
	Now     func() time.Time
	Metrics prometheus.Registerer
	Logger  *slog.Logger
}

// serveAPI поднимает единственный внутренний слушатель носителя Х5 с
// InternalNoticeService, NoticeService и OperationService и возвращается по
// отмене ctx (nil) либо с ошибкой носителя. Цепочку звеньев личности и прав
// собирает носитель; корень приносит объявление о себе и службы.
//
// cfg — значения ручек notify-api (замысел issue-2924 З1, З14, З16, З17;
// приёмка NTF-4 Р20 — таблица полей дескриптора носителя Х5). Страж ручек
// (config.Config.Validate) зовёт main до этой функции.
func serveAPI(ctx context.Context, cfg config.Config, rt apiRuntime) error {
	if rt.Pool == nil || rt.Now == nil || rt.Metrics == nil {
		return errors.New("notify-api: пул, часы и реестр метрик обязательны")
	}
	mode, err := cfg.Mode()
	if err != nil {
		return fmt.Errorf("notify-api: %w", err)
	}
	internalCreds, err := grpcsrv.TLSServerTransportCreds(cfg.InternalServerTLS())
	if err != nil {
		return fmt.Errorf("notify-api: транспорт слушателя: %w", err)
	}
	kanameCreds, err := grpcclient.TLSClientTransportCreds(cfg.PeerTLS())
	if err != nil {
		return fmt.Errorf("notify-api→kaname mTLS: %w", err)
	}
	conn, err := grpc.NewClient(cfg.AuthzIAMGRPCAddr, grpc.WithTransportCredentials(kanameCreds),
		grpcclient.KeepaliveDialOption(true))
	if err != nil {
		return fmt.Errorf("notify-api: ребро к службе доступа %s: %w", cfg.AuthzIAMGRPCAddr, err)
	}
	defer func() { _ = conn.Close() }()

	narrower, err := authzwiring.NewListNarrower(narrowiam.New(conn), cfg.ListFilter(), rt.Now)
	if err != nil {
		return fmt.Errorf("notify-api: сужатель затронутых ресурсов: %w", err)
	}
	authzCache := &authzmetrics.Source{}
	if err := registerAuthzCollectors(rt.Metrics, authzCache, narrower); err != nil {
		return fmt.Errorf("notify-api: величины звена прав: %w", err)
	}

	desc, err := describe(cfg, mode, internalCreds, kanameCreds, rt, authzCache.Install)
	if err != nil {
		return err
	}
	// Самоотчёт о посадке — после принятия дескриптора и до подъёма слушателя.
	posture, err := bootPosture(cfg, desc)
	if err != nil {
		return fmt.Errorf("notify-api: самоотчёт о посадке: %w", err)
	}
	observability.LogBootPosture(rt.Logger, posture)

	ops := operations.NewRepo(rt.Pool, operationsSchema)
	// Уборка терминальных строк таблицы операций: порог и расписание объявлены
	// фундаментом один раз; незавершённые (заявки Create до фиксации) она не
	// трогает — предикат судит `done = true`.
	if _, err := operations.StartRetentionSweep(ctx, ops, operations.DefaultRetentionConfig(), rt.Logger); err != nil {
		return fmt.Errorf("notify-api: уборка таблицы операций: %w", err)
	}
	store := noticerepo.New(rt.Pool, ops)
	clock := notice.Clock(rt.Now)
	internalNotice := (&handler.InternalNotice{
		Create:   noticecreate.New(store, clock),
		Get:      noticeget.New(store),
		List:     noticelist.New(store),
		Update:   noticeupdate.New(store, clock, cfg.NoticeReminderLead),
		Start:    noticestart.New(store, clock),
		Complete: noticecomplete.New(store, clock),
		Cancel:   noticecancel.New(store, clock),
	}).Server()
	reads := publicnotice.Deps{
		Reader:   store,
		Checker:  authzcheck.WithBudget(authziam.NewCheckClient(conn), cfg.AuthzCheckTimeout),
		Narrower: narrower,
	}
	publicNotice := (&handler.PublicNotice{
		List:          publiclist.New(reads),
		ListByAccount: listbyaccount.New(reads),
		Get:           publicget.New(reads),
		GetByAccount:  getbyaccount.New(reads),
	}).Server()
	opHandler := operationspb.NewHandler(ops)

	return servicehost.Serve(ctx, desc, nil, func(r grpc.ServiceRegistrar) {
		notifyv1.RegisterInternalNoticeServiceServer(r, internalNotice)
		notifyv1.RegisterNoticeServiceServer(r, publicNotice)
		operationv1.RegisterOperationServiceServer(r, opHandler)
	})
}

// metricsPrefix — сегмент имён величин процесса (`kacho_notifyapi_…`).
const metricsPrefix = "notifyapi"

// registerAuthzCollectors — величины кеша вердиктов звена прав и сужателя:
// доля попаданий — единственное число, которым отвечают на вопрос «сколько
// даёт кеш» (servicecontract.Spec.AuthzObserve).
func registerAuthzCollectors(reg prometheus.Registerer, cache *authzmetrics.Source, narrower *listnarrow.Narrower) error {
	if err := reg.Register(authzmetrics.New(metricsPrefix, map[string]authzmetrics.Reader{
		authzmetrics.LaneRPC:    cache.Cache,
		authzmetrics.LaneNarrow: narrower.CacheStats,
	}, cache.Read)); err != nil {
		return err
	}
	return reg.Register(narrowmetrics.New(metricsPrefix, narrower.Counts))
}
