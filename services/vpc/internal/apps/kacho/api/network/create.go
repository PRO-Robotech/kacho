// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package network

import (
	"context"
	"fmt"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/fgaregister"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/helpers"
)

// CreateNetworkUseCase инициирует создание Network. Sync-проверки (name unique)
// выполняются ДО создания Operation — клиент получает fast-fail gRPC-status, а не
// «200 + операция, упавшая через секунду». Async-часть (`doCreate`) — атомарный
// backstop через FK/UNIQUE.
//
// Worker открывает ОДНУ Writer-TX и делает в ней Insert(Network) →
// [Insert(SG, default) → SetDefaultSGID] → Insert(RT, default) →
// SetDefaultRouteTableID со всеми outbox-emit'ами. Либо весь композит виден
// (Commit), либо ни один DML (Abort/crash) — orphan-window исключён.
//
// Default-SG creation БЕЗУСЛОВНО: ни поля запроса, ни настройки оператора, которые
// бы её отменяли, больше нет. Условность была непоставленным пунктом утверждённой
// приёмки и давала состояние, в котором сеть жива, а группы у неё нет; по решению
// владельца модель закрыта в обе стороны и интерфейс НАСЛЕДУЕТ группу своей сети,
// поэтому сеть без группы означала бы интерфейс без единого правила — «не разрешено
// ничего». Default-RouteTable (F3) тоже не гейтится: она
// материализует `Network.defaultRouteTableId°`, от которого зависит
// детерминированная auto-assoc RT в Subnet.Create. Обе inline-композиции вынесены
// в отдельные use-case'ы (`default_sg.go` / `default_rt.go`) и вызываются ВНУТРИ
// writer-TX `doCreate` перед `Commit()`, чем и сохраняется atomic-семантика.
type CreateNetworkUseCase struct {
	// quota — совещательная полоса учёта (порт QuotaGuard).
	//
	// nil означает «раннего отказа нет», а НЕ «предела нет»: место по-прежнему
	// занимает триггер в writer-транзакции, и исчерпание приезжает отказом
	// операции. Различие наблюдаемо (429 синхронно против отказа в операции), и
	// потому провязка обязательна на любом поднятом стенде; отсутствие допустимо
	// только там, где нет и соседа, у которого спрашивать величины.
	quota           QuotaGuard
	repo            Repo
	projectClient   ProjectClient
	opsRepo         operations.Repo
	createDefaultSG *CreateDefaultSGUseCase
	// createDefaultRT — inline-провижн системной default-RouteTable (VPC-1 F3).
	// В отличие от default-SG флагом НЕ гейтится: `Network.defaultRouteTableId°`
	// объявлен единственным источником истины «дефолтная RT сети» и от него
	// зависит детерминированная auto-assoc в Subnet.Create.
	createDefaultRT *CreateDefaultRTUseCase

	// registrar — синхронная регистрация owner-tuple'а в kaname после commit
	// (sync-primary; outbox-intent остается at-least-once backstop'ом). nil →
	// sync-путь пропускается (dev/no-iam), регистрация только через drainer.
	registrar fgaregister.Registrar

	// logger — диагностический trail async-worker'а (panic-recover до того, как
	// op-worker замаскирует причину). FGA owner-tuple эмитится как intent в
	// writer-TX, а не пишется напрямую отсюда.
	logger *slog.Logger
}

// NewCreateNetworkUseCase создает CreateNetworkUseCase. Признака условности
// создания группы по умолчанию у конструктора НЕТ намеренно: его появление снова
// сделало бы посадку безопасности зависящей от настройки стенда. Прежний текст
// пояснял, откуда берётся
// создается default SG (через композицию с `CreateDefaultSGUseCase`) и
// `Network.default_security_group_id` заполняется атомарно с Insert(Network).
func NewCreateNetworkUseCase(r Repo, projectClient ProjectClient, opsRepo operations.Repo) *CreateNetworkUseCase {
	return &CreateNetworkUseCase{
		repo:            r,
		projectClient:   projectClient,
		opsRepo:         opsRepo,
		createDefaultSG: NewCreateDefaultSGUseCase(),
		createDefaultRT: NewCreateDefaultRTUseCase(),
	}
}

// WithLogger подключает диагностический логгер для async Create-worker'а. FGA
// owner-tuple эмитится как outbox-intent в writer-TX, а не пишется напрямую.
// Nil logger → диагностический trail отключен.
func (u *CreateNetworkUseCase) WithLogger(logger *slog.Logger) *CreateNetworkUseCase {
	u.logger = logger
	return u
}

// WithRegistrar подключает синхронный owner-tuple registrar (Decision 2). После
// коммита Network (+ inline default-SG) те же Item'ы, что эмитятся в
// outbox-intent, синхронно регистрируются в kaname — owner-grant доступен
// сразу. Nil registrar → sync-путь пропускается (только async drainer).
func (u *CreateNetworkUseCase) WithRegistrar(r fgaregister.Registrar) *CreateNetworkUseCase {
	u.registrar = r
	return u
}

// Execute — sync-валидация + create Operation + запуск worker'а. Возвращает
// созданный Operation указателем (caller'у нужен он для `OperationService.Get`).
// Принимает `domain.Network` напрямую; поле `n.ID` на входе пустое — назначаем
// внутри use-case'а через `ids.NewID(ids.PrefixNetwork)`.
func (u *CreateNetworkUseCase) Execute(ctx context.Context, n domain.Network) (*operations.Operation, error) {
	if n.ProjectID == "" {
		return nil, status.Error(codes.InvalidArgument, "project_id required")
	}
	if err := serviceerr.FromValidation(n.Validate()); err != nil {
		return nil, err
	}
	// F2: объявленный супернет валидируется по формату (canonical CIDR,
	// host-bits=0, корректное семейство) sync, ДО создания Operation.
	// Cardinality-потолок — первым (до пер-блочного парсинга), чтобы вход-
	// переросток не оплачивался CPU на request-path.
	if err := validateSupernetCardinality(n.IPv4CidrBlocks, n.IPv6CidrBlocks); err != nil {
		return nil, err
	}
	if err := validateNetworkSupernet(n.IPv4CidrBlocks, n.IPv6CidrBlocks); err != nil {
		return nil, err
	}
	// Sync project.Exists precheck не делаем — он race-prone: между sync-проверкой
	// и async-частью project может быть удален peer-сервисом, и second-writer-wins
	// безусловно создавал бы ресурс. NotFound возвращается через `operation.error`
	// из async `doCreate`.
	name := string(n.Name)
	if name != "" {
		rd, err := u.repo.Reader(ctx)
		if err != nil {
			return nil, serviceerr.MapRepoErr(err)
		}
		existing, _, lerr := rd.Networks().List(ctx, NetworkFilter{ProjectID: n.ProjectID, Name: name}, Pagination{})
		_ = rd.Close()
		if lerr != nil {
			return nil, serviceerr.MapRepoErr(lerr)
		}
		if len(existing) > 0 {
			return nil, status.Errorf(codes.AlreadyExists, "Network with name %s already exists", name)
		}
	}

	netID := ids.NewID(ids.PrefixNetwork)
	// Пустое имя не доживает до записи: ресурса без имени не бывает (#715).
	// Подстановка стоит ПОСЛЕ чеканки идентификатора (умолчание производно от
	// него) и ДО сборки строки — и она же снимает нужду в проверке «а не занято
	// ли»: идентификатор глобально уникален by construction, поэтому уникальность
	// имени остаётся за индексом БД, а не за чтением-перед-вставкой (ban #10).
	name = corevalidate.NameOrDefault(name, netID)
	n.Name = domain.RcNameVPC(name)
	// Учёт числа ресурсов: ранний отказ ДО создания операции.
	//
	// Здесь же материализуются строки учёта, если проект их ещё не имеет, —
	// момент, когда владелец типа впервые узнаёт о проекте, и есть обращение к
	// нему. Отказ уходит арендатору синхронно тем же текстом и признаком, каким
	// его произвёл бы триггер: у обеих полос один производитель.
	if u.quota != nil {
		if err := u.quota.Admit(ctx, string(n.ProjectID), "vpc.network"); err != nil {
			return nil, serviceerr.MapRepoErr(err)
		}
	}

	op, err := operations.NewFromContext(
		ctx,
		ids.PrefixOperationVPC,
		fmt.Sprintf("Create network %s", name),
		&vpcv1.CreateNetworkMetadata{NetworkId: netID},
	)
	if err != nil {
		return nil, err
	}
	if err := u.opsRepo.Create(ctx, op); err != nil {
		return nil, err
	}

	// Create — durable commit → op done сразу после worker-fn. Owner-tuple
	// материализуется eventually-consistent (sync-registrar после commit +
	// register-drainer/reconciler backstop), а не гейтит done.
	if err := operations.RunSync(ctx, u.opsRepo, &op, func(ctx context.Context) (res *anypb.Any, derr error) {
		// Поднимаем наружу диагностику падений async-worker'а. operations.Run
		// маскирует любую не-gRPC-status ошибку (и panic) как Operation `INTERNAL
		// "internal worker error"` и НЕ логирует ее — упавший Network.Create
		// молча оставил бы Network без FGA register-intent (writer-TX
		// откатилась), и каждый per-resource Check возвращал бы `no path` без
		// единого следа. Recover + лог реальной причины ДО того, как op-worker
		// ее замаскирует.
		defer func() {
			if r := recover(); r != nil {
				derr = fmt.Errorf("panic in Network.Create doCreate: %v", r)
				if u.logger != nil {
					u.logger.Error("network create operation panicked",
						"op", op.ID, "network_id", netID, "project_id", string(n.ProjectID),
						"panic", fmt.Sprint(r))
				}
			}
		}()
		res, derr = u.doCreate(ctx, netID, n)
		if derr != nil && u.logger != nil {
			u.logger.Error("network create operation failed",
				"op", op.ID, "network_id", netID, "project_id", string(n.ProjectID),
				"err", derr.Error())
		}
		return res, derr
	}); err != nil {
		return nil, err
	}

	return &op, nil
}

// doCreate — async-часть Create (внутри Operation worker'а). Атомарный backstop:
// project-exists + Insert (FK ограничения / UNIQUE-нарушения); inline default-SG
// creation (builder из domain) с link через SetDefaultSGID(Network, sg.ID) и
// безусловный inline default-RouteTable с link через SetDefaultRouteTableID.
//
// ВСЕ идет в одной writer-TX:
//
//	w := u.repo.Writer(ctx)            // открыли единую TX
//	created := w.Networks().Insert     // строки журнала ещё НЕТ
//	u.createDefaultSG.Execute(ctx, w, created.Network)
//	            // → w.SGs().Insert + SG.CREATED outbox
//	            //   + w.Networks().SetDefaultSGID (без строки о сети)
//	u.createDefaultRT.Execute(ctx, w, created.Network)
//	            // → w.RouteTables().Insert + RouteTable.CREATED outbox
//	            //   + w.Networks().SetDefaultRouteTableID (без строки о сети)
//	Emit("Network", finalRec, CREATED) // ОДНА строка, собранная сеть (#1548)
//	w.Commit()                         // либо все, либо ничего (Abort/crash)
//
// Строк журнала на одно создание ТРИ — по одной на каждый заведённый ресурс.
// Прежде их было пять: сеть объявлялась сразу после вставки, пустой, и следом
// шли два `UPDATED` по мере достройки умолчаний. Разбор — у самой эмиссии ниже;
// свойство держит `services/vpc/internal/repo/network_create_journal_rows_integration_test.go`.
//
// Так исключены частичные результаты на crash между шагами (orphan SG, Network
// без default_sg_id или забытый outbox-event). Default-SG composition вынесена в
// `CreateDefaultSGUseCase.Execute`; атомарность сохранена тем, что use-case
// работает в УЖЕ открытой нами `Writer`-TX (`w`), сам ее не открывает и не
// commit'ит.
//
// FK Network.default_security_group_id → security_groups(id) `ON DELETE SET NULL`.
// SG-FK на network_id — RESTRICT, но в одной TX это нормально: Insert(SG)
// ссылается на только что вставленный Network в той же tx (видимость + Postgres
// constraint check на коммите — INSERT(child) после INSERT(parent) в одной TX
// проходит).
func (u *CreateNetworkUseCase) doCreate(ctx context.Context, netID string, n domain.Network) (*anypb.Any, error) {
	exists, err := u.projectClient.Exists(ctx, n.ProjectID)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "project check: %v", err)
	}
	if !exists {
		return nil, status.Errorf(codes.NotFound, "Project %s not found", n.ProjectID)
	}

	n.ID = netID

	w, err := u.repo.Writer(ctx)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	defer w.Abort()

	created, err := w.Networks().Insert(ctx, &n)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}

	// Системная группа правил по умолчанию — в ТОЙ ЖЕ writer-TX, БЕЗУСЛОВНО.
	// Композиция use-case'ов в одной транзакции: он работает в нашей `w`, а
	// Abort/Commit делает вызывающий. Возвращаемую им проекцию сети здесь не
	// удерживаем: следующий шаг (таблица по умолчанию) обновляет ту же строку и его
	// RETURNING отдаёт её целиком, уже с проставленным идентификатором группы.
	if _, sgErr := u.createDefaultSG.Execute(ctx, w, created.Network); sgErr != nil {
		return nil, sgErr
	}

	// Системная default-RouteTable (F3) — в ТОЙ ЖЕ writer-TX, безусловно:
	// `Network.defaultRouteTableId°` обязан быть непустым сразу после Create,
	// иначе Subnet.Create нечем детерминированно ассоциировать подсеть. Её
	// SetDefaultRouteTableID возвращает АКТУАЛЬНУЮ строку сети (RETURNING) —
	// она и есть финальная проекция ресурса для op-response.
	finalRec, rtErr := u.createDefaultRT.Execute(ctx, w, created.Network)
	if rtErr != nil {
		return nil, rtErr
	}

	// Сеть объявляется журналу ОДИН раз — здесь, когда она СОБРАНА (#1548).
	//
	// # Почему не сразу после Insert, как было
	//
	// Умолчания достраиваются той же транзакцией ПОСЛЕ вставки строки, поэтому
	// строка, объявленная сразу, несла пустые `default_security_group_id` и
	// `default_route_table_id`, а следом шли два `UPDATED` — по одному на каждое
	// достроенное умолчание. Подписчик вправе читать непустую нагрузку как ПОЛНОЕ
	// состояние предмета (`proto/kacho/cloud/subscription/subscription.proto`,
	// поле `state`) — и читал: показывал и записывал сеть БЕЗ группы безопасности
	// и БЕЗ таблицы маршрутов, а затем дважды себя поправлял. Состояние это было
	// правдой ровно внутри нашей транзакции и ложью к её концу.
	//
	// # Что этим куплено помимо правды
	//
	// Число событий перестало быть функцией нашей ВНУТРЕННЕЙ композиции. Прежде
	// третье умолчание дало бы подписчику четвёртое событие, хотя арендатор
	// по-прежнему делал одно действие; теперь состав умолчаний — наше частное
	// дело, а наружу едет один факт: сеть создана, вот она целиком.
	//
	// # Чего это НЕ отменяет
	//
	// Группа и таблица — самостоятельные ресурсы, и их появление подписчик узнаёт
	// своими строками (их эмитят те же два use-case'а). Сеть объявляется ПОСЛЕ
	// них: каждое событие называет ресурс, УЖЕ собранный к своей позиции, а
	// межвидового порядка поток не обещает и обещать не может — подписка сужается
	// по видам, и клиент, взявший один вид, соседних строк не увидит вовсе.
	//
	// Атомарность не затронута: эмиссия по-прежнему в той же writer-TX, что и
	// строка ресурса, поэтому при отказе операции события нет ВОВСЕ.
	if err := w.Outbox().Emit(ctx, "Network", finalRec.ID, finalRec.ProjectID, "CREATED",
		helpers.DomainToMap(finalRec)); err != nil {
		return nil, serviceerr.MapRepoErr(fmt.Errorf("%w: outbox emit: %v", repo.ErrInternal, err))
	}

	// Публикуем INTENT на hierarchy-tuple vpc_network→project в ТОЙ ЖЕ writer-TX,
	// что и Insert (один commit, без dual-write). Register-drainer позже применит
	// его через kaname InternalIAMService.RegisterResource (idempotent, retry
	// на Unavailable, tuple durable) — так tuple не теряется при transient
	// FGA-сбое. Inline default-SG tuple — часть ТОГО ЖЕ intent. Network-tuple
	// несет labels сети + parent_project_id для selector-mirror feed; auto
	// default-SG несет пустой feed (он не tenant-labelled selector-target).
	items := []fgaregister.Item{
		fgaregister.ProjectHierarchyItem(string(n.ProjectID), "vpc_network", finalRec.ID,
			domain.LabelsToMap(finalRec.Labels)),
	}
	if finalRec.DefaultSecurityGroupID != "" {
		items = append(items,
			fgaregister.ProjectHierarchyItem(string(n.ProjectID), "vpc_security_group", finalRec.DefaultSecurityGroupID, nil))
	}
	// Системная RT — такой же owner-tuple: без него gateway scope_extractor не
	// резолвит vpc_route_table→project и тенант получает 403 на СВОЕЙ RT.
	if finalRec.DefaultRouteTableID != "" {
		items = append(items,
			fgaregister.ProjectHierarchyItem(string(n.ProjectID), "vpc_route_table", finalRec.DefaultRouteTableID, nil))
	}
	// Версия, которой БД проштамповала intent ВНУТРИ writer-TX: её же понесёт
	// синхронная регистрация ниже, чтобы повторную доставку гасило монотонное
	// сравнение у принимающей стороны независимо от того, кто пришёл первым.
	intentVersion, err := w.FGARegister().EmitRegister(ctx, fgaregister.RegisterItems(items...))
	if err != nil {
		return nil, serviceerr.MapRepoErr(fmt.Errorf("%w: fga register intent: %v", repo.ErrInternal, err))
	}

	if err := w.Commit(); err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}

	// Sync-primary owner-tuple registration идёт ПОСЛЕ durable commit ресурса +
	// outbox-intent: она сокращает окно видимости гранта, но НЕ является условием
	// успеха мутации. Intent на те же tuple'ы лежит в fga_register_outbox той же
	// writer-TX → at-least-once дренаж доведёт грант сам. Провалить операцию здесь
	// значило бы отдать вызывающему код узла прав (status.FromError достаёт
	// вложенный статус и подменяет сообщение всей цепочкой) на уже созданную сеть
	// вместе с её системными SG/RT — фантом. Поэтому предупреждение, а не ошибка.
	fgaregister.DeliverAfterCommit(ctx, u.registrar, items, intentVersion, "Network", finalRec.ID)

	return marshalNetworkRecord(finalRec)
}

// WithQuotaGuard подключает совещательную полосу учёта.
//
// Отдельным глаголом, а не аргументом конструктора: полоса появилась позже
// вызывающих, и обязательный аргумент заставил бы править каждую сборку — в том
// числе те, где соседа с величинами нет вовсе.
func (u *CreateNetworkUseCase) WithQuotaGuard(g QuotaGuard) *CreateNetworkUseCase {
	u.quota = g
	return u
}
