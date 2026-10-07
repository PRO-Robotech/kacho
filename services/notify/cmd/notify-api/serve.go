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
	"google.golang.org/grpc/credentials"

	operationv1 "github.com/PRO-Robotech/corelib/api/corelib/operation"
	"github.com/PRO-Robotech/corelib/authz/authzmetrics"
	"github.com/PRO-Robotech/corelib/grpcclient"
	"github.com/PRO-Robotech/corelib/grpcsrv"
	"github.com/PRO-Robotech/corelib/listnarrow"
	"github.com/PRO-Robotech/corelib/listnarrow/narrowmetrics"
	"github.com/PRO-Robotech/corelib/operations"
	"github.com/PRO-Robotech/corelib/operations/operationspb"
	"github.com/PRO-Robotech/corelib/servicecontract"
	"github.com/PRO-Robotech/corelib/servicehost"
	"github.com/PRO-Robotech/kacho/pkg/authz/authziam"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowiam"

	notifyv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/notify/v1"

	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/notify/api/notice"
	noticecancel "github.com/PRO-Robotech/kacho/services/notify/internal/apps/notify/api/notice/cancel"
	noticecomplete "github.com/PRO-Robotech/kacho/services/notify/internal/apps/notify/api/notice/complete"
	noticecreate "github.com/PRO-Robotech/kacho/services/notify/internal/apps/notify/api/notice/create"
	noticeget "github.com/PRO-Robotech/kacho/services/notify/internal/apps/notify/api/notice/get"
	noticelist "github.com/PRO-Robotech/kacho/services/notify/internal/apps/notify/api/notice/list"
	noticestart "github.com/PRO-Robotech/kacho/services/notify/internal/apps/notify/api/notice/start"
	noticeupdate "github.com/PRO-Robotech/kacho/services/notify/internal/apps/notify/api/notice/update"
	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/notify/api/publicnotice"
	publicget "github.com/PRO-Robotech/kacho/services/notify/internal/apps/notify/api/publicnotice/get"
	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/notify/api/publicnotice/getbyaccount"
	publiclist "github.com/PRO-Robotech/kacho/services/notify/internal/apps/notify/api/publicnotice/list"
	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/notify/api/publicnotice/listbyaccount"
	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/notify/authzcheck"
	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/notify/authzwiring"
	"github.com/PRO-Robotech/kacho/services/notify/internal/config"
	"github.com/PRO-Robotech/kacho/services/notify/internal/handler"
	"github.com/PRO-Robotech/kacho/services/notify/internal/repo/noticerepo"
)

// operationsSchema — схема таблицы операций в kacho_notify: миграции notify
// создают свои таблицы в схеме по умолчанию.
const operationsSchema = "public"

// apiInputs — зависимости корня notify-api (замысел issue-2924 З1, З14, З16,
// З17; приёмка NTF-4 Р20 — таблица полей дескриптора носителя Х5). Каждое поле —
// значение ручки либо часть, собранная корнем процесса; умолчаний здесь нет:
// незаданное поле отвергает конструктор дескриптора либо сборщик сужателя.
type apiInputs struct {
	// ListenAddr — адрес единственного (внутреннего) слушателя.
	ListenAddr string
	// ServerTLS — серверное удостоверение слушателя с проверкой клиентов.
	ServerTLS grpcsrv.TLSServer
	// TrustDomain, TrustedForwarderSANs — домен доверия и круг пересылающих
	// принципала (пара звеньев личности носителя).
	TrustDomain          string
	TrustedForwarderSANs []string
	// TrustAnyForwarder — опт-ин «доверять любому пересылающему» вне боевой
	// посадки (KACHO_NOTIFY_AUTHZ_TRUST_ANY_FORWARDER); в боевой не читается.
	TrustAnyForwarder bool

	// Mode — посадка; DBSSLMode — шифрование до kacho_notify.
	Mode      servicecontract.Mode
	DBSSLMode string

	// Pool — пул kacho_notify.
	Pool *pgxpool.Pool
	// Now — часы notify; они же часы сужателя (З14 п.1).
	Now func() time.Time

	// KanameAddr, KanameCreds — ребро к службе доступа: Check звена прав и
	// use-case, пакетная проверка сужателя. Справочник notify-api не зовёт (З1).
	KanameAddr  string
	KanameCreds credentials.TransportCredentials

	// AuthzCacheTTL — окно звена прав (KACHO_NOTIFY_AUTHZ_CACHE_TTL);
	// AuthzCheckTimeout — срок одного вопроса о правах;
	// AuthzDenyBudget — темп непоглощаемых исходов на принципала;
	// HandlingBudget — граница обработки одного вызова.
	AuthzCacheTTL     time.Duration
	AuthzCheckTimeout time.Duration
	AuthzDenyBudget   float64
	HandlingBudget    time.Duration

	// ReminderLead — KACHO_NOTIFY_NOTICE_REMINDER_LEAD (напоминание MAINTENANCE).
	ReminderLead time.Duration
	// ListFilterCacheTTL — KACHO_NOTIFY_LIST_FILTER_CACHE_TTL (окно сужателя).
	ListFilterCacheTTL time.Duration

	// Metrics — реестр диагностической поверхности; Logger — журнал процесса.
	Metrics prometheus.Registerer
	Logger  *slog.Logger
}

// serveAPI поднимает единственный внутренний слушатель носителя Х5 с
// InternalNoticeService, NoticeService и OperationService и возвращается по
// отмене ctx (nil) либо с ошибкой носителя. Цепочку звеньев личности и прав
// собирает носитель; корень приносит только объявление о себе и службы.
func serveAPI(ctx context.Context, in apiInputs) error {
	if in.Pool == nil || in.Now == nil || in.Metrics == nil || in.KanameCreds == nil {
		return errors.New("notify-api: пул, часы, реестр метрик и удостоверение ребра к службе доступа обязательны")
	}
	internalCreds, err := grpcsrv.TLSServerTransportCreds(in.ServerTLS)
	if err != nil {
		return fmt.Errorf("notify-api: транспорт слушателя: %w", err)
	}
	conn, err := grpc.NewClient(in.KanameAddr, grpc.WithTransportCredentials(in.KanameCreds),
		grpcclient.KeepaliveDialOption(true))
	if err != nil {
		return fmt.Errorf("notify-api: ребро к службе доступа %s: %w", in.KanameAddr, err)
	}
	defer func() { _ = conn.Close() }()

	narrower, err := authzwiring.NewListNarrower(narrowiam.New(conn),
		config.ListFilter{CacheTTL: in.ListFilterCacheTTL, CheckTimeout: in.AuthzCheckTimeout}, in.Now)
	if err != nil {
		return fmt.Errorf("notify-api: сужатель затронутых ресурсов: %w", err)
	}
	authzCache := &authzmetrics.Source{}
	if err := registerAuthzCollectors(in.Metrics, authzCache, narrower); err != nil {
		return fmt.Errorf("notify-api: величины звена прав: %w", err)
	}

	desc, err := describe(in, internalCreds, authzCache.Install)
	if err != nil {
		return err
	}

	ops := operations.NewRepo(in.Pool, operationsSchema)
	store := noticerepo.New(in.Pool, ops)
	clock := notice.Clock(in.Now)
	internalNotice := (&handler.InternalNotice{
		Create:   noticecreate.New(store, clock),
		Get:      noticeget.New(store),
		List:     noticelist.New(store),
		Update:   noticeupdate.New(store, clock, in.ReminderLead),
		Start:    noticestart.New(store, clock),
		Complete: noticecomplete.New(store, clock),
		Cancel:   noticecancel.New(store, clock),
	}).Server()
	reads := publicnotice.Deps{
		Reader:   store,
		Checker:  authzcheck.WithBudget(authziam.NewCheckClient(conn), in.AuthzCheckTimeout),
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
