// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// wiring.go — cohesive composition-root sub-builders extracted from runServe.
// Each function is a faithful, side-effect-preserving slice of the original
// linear body: building the interceptor chains, registering the gRPC services,
// and assembling the supervised background workers. runServe stays the short
// orchestration sequence; resource-lifetime `defer`s and the errgroup/shutdown
// loop remain in runServe (moving them here would change their lifetime).
package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"google.golang.org/grpc"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/operations/operationspb"
	"github.com/PRO-Robotech/kacho/pkg/outbox/bootgate"
	"github.com/PRO-Robotech/kacho/pkg/outbox/drainer"
	"github.com/PRO-Robotech/kacho/pkg/outbox/metrics"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow"
	"github.com/PRO-Robotech/kacho/pkg/subscription"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/subscriptionjournal"

	announceapi "github.com/PRO-Robotech/kacho/services/nlb/internal/apps/kacho/api/announce"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/apps/kacho/api/listener"
	lbhandler "github.com/PRO-Robotech/kacho/services/nlb/internal/apps/kacho/api/loadbalancer"
	quotaapi "github.com/PRO-Robotech/kacho/services/nlb/internal/apps/kacho/api/quota"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/apps/kacho/api/targetgroup"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/apps/kacho/config"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/apps/kacho/jobs"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/apps/kacho/quota"
	geoclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/geo"
	iamclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/iam"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	kachopg "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho/pg"
)

// bgWorker — фоновый loop под супервизором (errgroup): неожиданный exit флипает
// readiness в shutting-down и триггерит graceful-shutdown (не fire-and-forget).
type bgWorker struct {
	name string
	run  func(context.Context) error
}

// grpcWiring — зависимости регистрации gRPC-сервисов (composition root bundle).
type grpcWiring struct {
	repo    *kachopg.Repository
	opsRepo operations.Repo
	peers   *peerClients
	pool    *pgxpool.Pool
	cfg     *config.Config
	logger  *slog.Logger
	// syncRegistrar — синхронный регистратор владельца ресурса у kacho-iam.
	// Собирается в композиционном корне ДО подъёма слушателей: его сборка умеет
	// отказать, а регистратор носителя возврата ошибки не имеет — и не должен,
	// отказ обязан случиться раньше первого принятого соединения.
	syncRegistrar iamclient.Registrar
	// quotaGuard — совещательная полоса учёта числа ресурсов.
	//
	// Собирается в композиционном корне: ей нужны и репозиторий, и оба соседа —
	// владелец величин (резолв) и владелец проектов (зеркало аккаунта). nil
	// означает «раннего отказа нет», а НЕ «предела нет»: место по-прежнему
	// занимает триггер в writer-транзакции.
	quotaGuard *quota.Guard
	// subscription — ОБЩИЙ сервер потока изменений (`pkg/subscription`), по
	// экземпляру на владельца журнала.
	//
	// Собирается в композиционном корне: ему нужны выделенное соединение вне
	// пула, сужатель по правам и величины посадки, а его сборка умеет отказать —
	// и отказать обязана раньше первого принятого соединения.
	subscription subscriptionv1.InternalSubscriptionServiceServer
}

// buildQuotaGuard собирает совещательную полосу учёта.
//
// Typed-nil здесь важен так же, как у регистратора: конкретный `*quota.Guard`
// строится ТОЛЬКО когда есть у кого спрашивать величины, иначе в интерфейсном
// поле лежал бы non-nil интерфейс с nil внутри — и вызывающий, проверяющий
// `!= nil`, дошёл бы до вызова.
//
// Отсутствие соседа НЕ означает «пределов нет»: списание остаётся за триггером,
// и исчерпание приезжает отказом операции. Теряется ровно ранний синхронный
// отказ.
func buildQuotaGuard(repo *kachopg.Repository, peers *peerClients) *quota.Guard {
	if peers.Limit == nil || peers.Project == nil {
		return nil
	}
	accounts, ok := peers.Project.(quota.AccountLocator)
	if !ok {
		return nil
	}
	// `loadbalancer` — имя ДОМЕНА в каталоге видов, а не имя каталога сервиса:
	// токены каталога начинаются с него (`loadbalancer.listeners`), и резолв
	// спрашивается по нему же.
	return quota.NewGuard(repo, peers.Limit, accounts, "loadbalancer")
}

// buildSyncRegistrar собирает синхронный регистратор owner-tuple поверх того же
// mTLS-соединения с внутренним листенером kacho-iam, которым идёт дренаж
// регистраций. Пира нет (dev / без iam) → nil-регистратор, синхронный путь
// пропускается; durable-намерение в очереди остаётся at-least-once backstop'ом.
//
// Typed-nil: конкретный `*SyncRegistrar` строится ТОЛЬКО при наличии пира — иначе
// в интерфейсном поле лежал бы non-nil интерфейс с nil внутри.
func buildSyncRegistrar(peers *peerClients) (iamclient.Registrar, error) {
	if peers.Register == nil {
		return nil, nil
	}
	// Отказ, а не пустая операция: несобранный регистратор — это ускоритель,
	// который никогда ничего не ускорит, и заметить это было бы нечем.
	reg, err := iamclient.NewSyncRegistrar(peers.Register)
	if err != nil {
		return nil, fmt.Errorf("собрать синхронный registrar: %w", err)
	}
	return reg, nil
}

// registerPublic регистрирует обработчики ПУБЛИЧНОГО слушателя :9090.
//
// Носитель контура зовёт эту функцию ровно один раз и передаёт ей
// `grpc.ServiceRegistrar` — интерфейс с единственным методом. Сервера здесь нет и
// быть не может, поэтому «зарегистрировать и приделать своё звено» невыразимо.
//
// Owner-tuple ресурса материализуется eventually-consistent (намерение в
// writer-TX → дренаж → kacho-iam RegisterResource → реконсайлер-backstop) и НЕ
// гейтит `Operation.done`. Синхронный регистратор дополнительно регистрирует его
// сразу после коммита, закрывая read-your-writes окно; durable-намерение остаётся
// at-least-once backstop'ом (ban #9).
func registerPublic(reg grpc.ServiceRegistrar, w grpcWiring) {
	// OperationService (exempt: op-id опакен, owner-scoped Get/Cancel).
	operationpb.RegisterOperationServiceServer(reg, operationspb.NewHandler(w.opsRepo))

	// NetworkLoadBalancerService.
	lbHandler := lbhandler.NewHandler(
		w.repo, w.opsRepo,
		w.peers.Project, w.peers.Check, w.peers.Region, w.peers.Zone,
		lbZoneRegion(w.peers.ZoneRegion),
		w.peers.Subnet, w.peers.Address, w.peers.InternalAddress,
		w.peers.ListFilter,
		w.logger,
	).WithRegistrar(w.syncRegistrar).WithSecurityGroupClient(w.peers.SecurityGroup).
		WithQuotaGuard(w.quotaGuard)
	lbv1.RegisterNetworkLoadBalancerServiceServer(reg, lbHandler)

	// ListenerService. InternalAddress нужен только для release legacy-VIP в
	// Delete (nil → Unavailable). Check — пообъектный гейт на caller-supplied
	// `targetGroupId` в Create/Update (per-RPC звено скоупит только родительский
	// балансировщик и сам листенер, целевая группа осталась бы необойдённой —
	// CWE-863).
	listenerHandler := listener.NewHandler(
		w.repo,
		w.opsRepo,
		w.peers.ListFilter,
		w.logger,
	).WithRegistrar(w.syncRegistrar).WithCheckClient(w.peers.Check).
		WithQuotaGuard(w.quotaGuard)
	lbv1.RegisterListenerServiceServer(reg, listenerHandler)

	// TargetGroupService. Фаза B drain — отдельный фоновый runner.
	tgHandler := targetgroup.NewHandler(
		w.repo, w.opsRepo,
		w.peers.Project, w.peers.Check, w.peers.Region,
		w.peers.Instance, w.peers.NetworkInterface, w.peers.Subnet,
		tgZoneRegion(w.peers.ZoneRegion),
		w.peers.ListFilter,
		w.logger,
	).WithRegistrar(w.syncRegistrar).WithQuotaGuard(w.quotaGuard)
	lbv1.RegisterTargetGroupServiceServer(reg, tgHandler)

	// QuotaService — чтение квот арендатором. Выставляется, ТОЛЬКО когда полоса
	// учёта собрана: иначе метод отвечал бы пустым набором на каждый запрос — то
	// есть «квот нет», ровно то утверждение, которое контракт запрещает делать.
	// Незарегистрированный метод отвечает `Unimplemented`, и это честно:
	// возможности здесь действительно нет.
	if w.quotaGuard != nil {
		lbv1.RegisterQuotaServiceServer(reg, quotaapi.NewHandler(w.quotaGuard))
	}
}

// registerInternal регистрирует обработчики ВНУТРЕННЕГО слушателя :9091.
//
// `Internal.*` живёт ТОЛЬКО здесь — на внешнем endpoint эти службы не публикуются
// (запретом на публикацию внутренних служб наружу). Разделение проверяемо: носитель снимает служимый набор у самих
// серверов (`grpc.Server.GetServiceInfo`), поэтому «зарегистрировали не туда»
// видно наблюдением, а не обзором диффа.
func registerInternal(reg grpc.ServiceRegistrar, w grpcWiring) {
	// InternalLoadBalancerAnnounceService — обратная связь состояния анонса.
	// Инфра-чувствительные данные (BGP/route/VRF/kernel/infra-id) на внешнюю
	// поверхность не выходят.
	announceHandler := announceapi.NewHandler(kachopg.NewAnnounceStore(w.pool), w.logger)
	lbv1.RegisterInternalLoadBalancerAnnounceServiceServer(reg, announceHandler)

	// Поток изменений — ОБЩИЙ сервер (`pkg/subscription`), а не своя обёртка
	// вокруг него: владелец регистрирует его самого. Регистрация безусловна —
	// собирает сервер композиционный корень, и его сборка умеет ОТКАЗАТЬ, поэтому
	// до сюда нулевой указатель не доходит. Условная регистрация означала бы, что
	// подписка тихо отсутствует у процесса, чей дескриптор объявил ей срок жизни.
	subscriptionv1.RegisterInternalSubscriptionServiceServer(reg, w.subscription)

	// Опроса операций здесь НЕТ намеренно: до перевода на носитель он жил только
	// на публичном слушателе, и добавить его «за компанию» значило бы расширить
	// внутреннюю поверхность правкой об оформлении контура. Распределение служб
	// по слушателям остаётся ровно прежним.
}

// tgZoneRegion — typed-nil guard: `*geoclient.ZoneRegionClient(nil)` в
// interface-поле было бы non-nil интерфейсом и обошло бы fail-closed-ветку
// use-case'а. Возвращает истинный nil, когда geo не сконфигурирован.
func tgZoneRegion(c *geoclient.ZoneRegionClient) targetgroup.ZoneRegionClient {
	if c == nil {
		return nil
	}
	return c
}

// lbZoneRegion — тот же typed-nil guard, что tgZoneRegion, для Create-use-case
// NetworkLoadBalancer.
func lbZoneRegion(c *geoclient.ZoneRegionClient) lbhandler.ZoneRegionClient {
	if c == nil {
		return nil
	}
	return c
}

// backgroundDeps — зависимости сборки supervised background-loop'ов.
type backgroundDeps struct {
	pool      *pgxpool.Pool
	repo      *kachopg.Repository
	lroRec    operations.Recorder
	outboxRec metrics.Recorder
	bootGate  *bootgate.Gate
	peers     *peerClients
	cfg       *config.Config
	logger    *slog.Logger
	// freeIPPoisonObs — poison-observer free-ip reconciler'а (nil-safe): каждый
	// раз при изоляции ядовитой строки инкрементит free_ip_poisoned-метрику.
	freeIPPoisonObs func()
}

// assembleBackgroundWorkers строит полный набор фоновых loop'ов: lro-reconciler,
// target-drain, free-ip-runner, fga-register-drainer, fga-register-reconciler,
// outbox-metrics-collector. Возвращает workers и ошибку. Сами loop'ы НЕ
// запускаются здесь — их гоняет errgroup в runServe; функция лишь собирает slice
// + строит их ресурсы (drainer.New, backstop, bootGate.SetConnected).
//
// В этом перечне НЕТ и не должно быть двух вещей, которые он раньше называл, —
// обе снялись вместе со своим предметом, и обе успели пережить его в этом же
// комментарии:
//
//   - слушатель инвалидации кэша вердиктов — механизм iam, у nlb нет ни его
//     настроек, ни его кода;
//   - vip-origin-reconcile и возвращаемые им ворота готовности — обратное
//     заполнение дискриминатора источника VIP на ЛИСТЕНЕРЕ. VIP консолидирован
//     на балансировщике, адресные колонки листенера дропнуты миграцией
//     0028_drop_dead_listener_address_columns.sql, и она же прямо фиксирует
//     снятие этой работы вместе с воротами. Имя пережило код и оставалось в
//     дереве только здесь — то есть по грепу выглядело живым.
//
// Перечень в этом комментарии обязан совпадать с именами bgWorker ниже: он
// читается как ответ на вопрос «что вообще крутится в процессе», и разошедшийся
// с кодом ответ хуже отсутствующего.
func assembleBackgroundWorkers(ctx context.Context, d backgroundDeps) ([]bgWorker, error) {
	var background []bgWorker

	// Durable LRO recovery: RecoverAll до трафика (в runServe), периодический Run —
	// backstop под супервизором.
	lroReconciler := startLRORecovery(ctx, d.pool, d.repo, d.lroRec, d.logger)
	background = append(background, bgWorker{"lro-reconciler", func(c context.Context) error {
		lroReconciler.Run(c)
		return nil
	}})

	// target drain-runner (фаза B): tick-loop по cfg.Jobs.TargetDrain.Interval.
	drainRunner := jobs.NewTargetDrainRunner(d.repo, d.logger, d.cfg.Jobs.TargetDrain.Interval)
	background = append(background, bgWorker{"target-drain", drainRunner.Run})

	// free_ip_runner: реконсиляция застрявших балансировщиков (multi-replica-safe). Требует
	// vpc internal-address client (release) — иначе не стартует (иначе утечка VIP).
	if d.peers.InternalAddress != nil {
		var freeIPOpts []jobs.FreeIPOption
		if d.freeIPPoisonObs != nil {
			// Каждая изолированная ядовитая строка бампит free_ip_poisoned_total.
			freeIPOpts = append(freeIPOpts, jobs.WithPoisonObserver(func(string) { d.freeIPPoisonObs() }))
		}
		freeIPRunner := jobs.NewFreeIPRunner(d.pool, d.peers.InternalAddress, d.logger,
			d.cfg.Jobs.FreeIP.Interval, d.cfg.Jobs.FreeIP.AgeThreshold, freeIPOpts...)
		background = append(background, bgWorker{"free-ip-runner", freeIPRunner.Run})
	} else {
		d.logger.Warn("free_ip_runner_disabled — no vpc internal-address client; stuck load-balancer VIP reconcile inactive")
	}

	// FGA register-drainer: corelib outbox/drainer on kacho_nlb.fga_register_outbox
	// (FOR UPDATE SKIP LOCKED → exactly-once). Wired with a real iam peer → opens the
	// boot-gate + starts the reconciler/metrics backstop. Default-on.
	if d.cfg.FGA.RegisterDrainer.Enable && d.peers.Register != nil {
		dr, derr := drainer.New[domain.FGARegisterIntent](
			d.pool,
			drainer.Config{
				Table:        nlbFGAOutboxTable,
				Channel:      nlbFGAOutboxChannel,
				BatchSize:    d.cfg.FGA.RegisterDrainer.BatchSize,
				PollFallback: d.cfg.FGA.RegisterDrainer.PollFallback,
				MaxAttempts:  d.cfg.FGA.RegisterDrainer.MaxAttempts,
				BackoffMin:   d.cfg.FGA.RegisterDrainer.BackoffMin,
				BackoffMax:   d.cfg.FGA.RegisterDrainer.BackoffMax,
				// Order-preserving drain, per resource. This table carries BOTH
				// fga.register AND fga.unregister of the SAME resource (LoadBalancer /
				// Listener / TargetGroup), and iam's materialisation is only PARTIALLY
				// versioned: source_version-LWW (resource_mirror UPSERT guarded by
				// `source_version < EXCLUDED.source_version`) protects the
				// ON-CONFLICT-UPDATE branch ONLY, while unregister is a hard DELETE
				// leaving no tombstone. A reordered STALE register has nothing to
				// compare against, takes the INSERT branch and RESURRECTS the mirror
				// row of a DELETED resource; the level-triggered reconciler then
				// re-materialises its owner-tuple forever (no self-healing).
				//
				// Reorder does not need concurrency: the claim orders by
				// (attempt_count, id), so a transiently-bumped register (attempt>=1
				// after an iam blip) loses to a fresh unregister (attempt=0) even at
				// ApplyConcurrency=1 — nlb's default — and lands in a later batch.
				// PartitionColumn makes the claim partition-head-only: a row is never
				// claimed while a DELIVERABLE same-resource predecessor with a smaller
				// id is unsent, so per-resource FIFO holds cross-batch AND
				// cross-replica; different resources keep draining in parallel.
				//
				// Key is resource_id: every emitter (writer-tx emitter, free_ip_runner
				// unregister, corelib reconciler) writes one row per FGA object stamped
				// with that object's globally-unique id (core rule #15). Requires
				// migration 0024's partial index (resource_id, id) WHERE sent_at IS
				// NULL for the claim's NOT EXISTS. Behaviour pinned by
				// drainer.Test_1_4_45_RegisterOutbox_UnregisterThenStaleRegister.
				PartitionColumn: "resource_id",
			},
			iamclient.DecodeFGARegisterIntent,
			iamclient.NewRegisterApplier(d.peers.Register),
			d.logger,
			// Each poisoned row bumps outbox_poisoned_total{table=…}.
			drainer.WithPoisonObserver[domain.FGARegisterIntent](func() {
				d.outboxRec.IncPoisoned(nlbFGAOutboxTable)
			}),
			// Каждая ДОСТАВЛЕННАЯ строка инкрементит счётчик своего направления
			// (#1714). Прежде эту величину ставил скан как `count(*)` по живым
			// строкам — совпадая с объявленным «за всё время» ровно до тех пор,
			// пока строки не убираются. Наблюдатель считает СОБЫТИЕ доставки,
			// поэтому уборка на величину не влияет by construction.
			drainer.WithDeliveryObserver[domain.FGARegisterIntent](
				metrics.DeliveryObserver(nlbFGAOutboxTable, metrics.RegisterOutboxDirections(), d.outboxRec)),
		)
		if derr != nil {
			return nil, fmt.Errorf("build fga register-drainer: %w", derr)
		}
		background = append(background, bgWorker{"fga-register-drainer", dr.Run})
		// Drainer wired with a real iam peer → IAM-register delivery path is up:
		// open the boot-gate + start the reconciler/metrics backstop.
		d.bootGate.SetConnected(true)
		reconRun, colRun, berr := startBackstop(ctx, d.pool, d.outboxRec, d.logger)
		if berr != nil {
			return nil, fmt.Errorf("start outbox backstop: %w", berr)
		}
		background = append(background,
			bgWorker{"fga-register-reconciler", reconRun},
			bgWorker{"outbox-metrics-collector", colRun},
		)
		d.logger.Info("fga_register_drainer_started", "mtls", d.cfg.MTLS.IAMRegister.Enable)
	} else {
		d.logger.Warn("fga_register_drainer_disabled_or_no_iam_peer — created resources will not get their per-resource FGA owner-tuple",
			"enable", d.cfg.FGA.RegisterDrainer.Enable, "iam_peer", d.peers.Register != nil)
	}

	return background, nil
}

// buildSubscriptionServer собирает ОБЩИЙ сервер потока изменений для журнала nlb.
//
// Владелец приносит сюда ЖУРНАЛ и величины ПОСАДКИ — и ничего больше: курсор,
// граница устоявшегося, пределы, сужение по правам и порядок отказов принадлежат
// общему серверу и владельцу не выдаются.
//
// Сужатель — ТОТ ЖЕ объект, что сужает страницы списков: за глаголом подписки нет
// пообъектной проверки на крае (он `scope_filtered`), поэтому откатываться не на
// что, а второй экземпляр означал бы, что поток сужается не тем, чем сужаются
// списки.
//
// Отказ возвращается, а не логируется: величина посадки, о которой никто не
// сказал, не должна обнаруживаться первым запросом в бою.
func buildSubscriptionServer(
	cfg *config.Config,
	listFilter *listnarrow.Narrower,
	logger *slog.Logger,
) (subscriptionv1.InternalSubscriptionServiceServer, error) {
	gate, err := subscriptionjournal.ProjectGate()
	if err != nil {
		return nil, err
	}
	// Страж посадки: параметр ПУЛА в строке одиночного соединения означает отказ
	// на подключении, а не на сборке, — и потому обязан быть пойман здесь, а не
	// первой подпиской в бою. Сегодня строка берётся сырой и пуловых ключей не
	// несёт; страж стоит затем, что это свойство ничем не удержано — ни от ключа
	// в самой строке, ни от перевода этого места на пуловую форму.
	if key := coredb.PoolParamFromDSN(cfg.Repository.Postgres.URL); key != "" {
		return nil, fmt.Errorf("поток изменений: строка подключения несёт параметр пула %q: "+
			"вне пула это неизвестный PG-параметр и FATAL при подключении, "+
			"а отказ наступил бы не на сборке, а у каждой подписки в бою", key)
	}
	srv, err := subscription.NewServer(subscription.Config{
		Journal: subscriptionjournal.Journal(),
		// Выделенное соединение вне пула: `LISTEN` требует своей сессии, а сессия
		// из пула вернулась бы в него вместе с подпиской.
		DSN:          cfg.Repository.Postgres.URL,
		Narrower:     listFilter,
		ProjectGate:  gate,
		MaxStreams:   cfg.APIServer.SubscriptionMaxStreams,
		StreamBudget: cfg.APIServer.SubscriptionStreamBudget,
		IdlePoll:     cfg.APIServer.SubscriptionIdlePoll,
		Logger:       logger,
	})
	if err != nil {
		return nil, fmt.Errorf("subscription server: %w", err)
	}
	return srv, nil
}
