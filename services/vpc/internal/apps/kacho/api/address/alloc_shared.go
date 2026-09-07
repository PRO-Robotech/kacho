// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package address

// alloc_shared.go — единый источник IPAM-selection-логики, общий для двух путей
// аллокации: inline create-time (CreateAddressUseCase.doCreate, `create.go`) и
// internal Allocate RPC (AllocateUseCase, `allocate.go`). Раньше двухфазный
// (random-pick + deterministic sweep) v4-цикл и v6-цикл, а также external
// freelist-pop + error-mapping были скопированы байт-в-байт между обоими
// use-case'ами (~4 семейства). Дрейф-риск: фикс алгоритма в одном месте молча
// расходился со вторым (project-rule #11 / evgeniy cohesion).
//
// Каждая функция принимает УЖЕ открытый writer-TX и УЖЕ прочитанный
// *AddressRecord; вызывающий отвечает за pre-checks (nil-spec / idempotent
// already-allocated / empty subnet_id|pool) и за terminal-wrap результата
// (create → allocResult; allocate → finishAllocate).
//
// Здесь же живут ТЕКСТЫ ОТКАЗОВ этой полосы и их машинные признаки — по той же
// причине, по которой здесь живёт сам алгоритм: два пути (create-time и internal
// Allocate) обязаны отвечать вызывающему одинаково, а разойтись двум копиям
// одного текста нечем, если копия одна. Прежняя редакция этой шапки объявляла
// тексты «сохранёнными дословно» — они с тех пор переписаны осознанно: часть их
// называла вызывающему координаты админского пула, часть утверждала исход,
// которого не было (см. комментарии у конструкторов ниже).

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// ---- причины отказа полосы выделения адреса ----------------------------------
//
// Это ось ЁМКОСТИ, а не полоса резолва идентификатора. Закрытый компилятором
// словарь `pkg/errors.Reason` отвечает на один вопрос — «резолвится ли названный
// id» (формат · своё · чужое · состояние чужого · недоступность владельца), и все
// пять его полос об этом. Здесь вопрос другой: «есть ли что выдать и удалось ли
// занять». Поэтому признаки объявлены у своего производителя, и их ровно три.
//
// Общее у двух осей ровно одно — ИСТОЧНИК отказа (`ErrorInfo.domain`). Второй
// литерал имени сервиса разъехался бы с первым молча и разъехался бы там, где
// деталь читают машиной, поэтому он не оставлен на слово: проба
// `TestCapacityReasonSharesTheResolveLaneDomain` берёт домен у существующего
// производителя полосы резолва и требует совпадения.
const (
	// reasonNoFreeSubnetAddress — пространство подсети осмотрено ЦЕЛИКОМ и все
	// адреса заняты. Проверенное утверждение: повтор не поможет.
	reasonNoFreeSubnetAddress = "SUBNET_NO_FREE_ADDRESS"

	// reasonAllocationContended — кончился ограниченный перебор, пространство
	// осмотрено НЕ полностью. Об исчерпании тут ничего не известно, вызывающему
	// полагается повтор.
	reasonAllocationContended = "ALLOCATION_CONTENDED"

	// reasonExternalUnavailable — платформе нечего выдать под внешний адрес
	// запрошенного семейства. Единственный признак на все причины, потому что
	// различие между ними — свойство конфигурации админского пула.
	reasonExternalUnavailable = "EXTERNAL_ADDRESS_UNAVAILABLE"
)

// vpcReasonDomain — источник отказа в `ErrorInfo.domain`, как его видит клиент.
const vpcReasonDomain = "vpc.kacho.cloud"

// capacityRefusal — сборка отказа этой оси: код и текст у вызывающего, машинный
// признак в деталях.
//
// Инвариант метаданных: деталь НЕ говорит больше сообщения. Иначе скрытое из
// текста возвращалось бы через `ErrorInfo.metadata` — то же раскрытие, только
// незаметное глазом при чтении диффа.
func capacityRefusal(code codes.Code, reason, msg string, meta map[string]string) error {
	st := status.New(code, msg)
	withDetails, derr := st.WithDetails(&errdetails.ErrorInfo{
		Reason:   reason,
		Domain:   vpcReasonDomain,
		Metadata: meta,
	})
	if derr != nil {
		// Деталь не прикрепилась — код и текст важнее признака.
		return st.Err()
	}
	return withDetails.Err()
}

// familyLabel — семейство адреса словом контракта (`IPv4` / `IPv6`). Выводится
// из версии, а не пишется по местам: разойдясь, два написания дали бы один
// сервис, отвечающий про одно семейство двумя текстами.
func familyLabel(v domain.IpVersion) string {
	if v == domain.IpVersionIPv6 {
		return "IPv6"
	}
	return "IPv4"
}

// noExternalAddressAvailable — ЕДИНСТВЕННЫЙ ответ вызывающему на любую причину,
// по которой платформа не может выдать внешний адрес: пула для запроса нет, у
// пула нет блоков этого семейства, его учёт пуст, подряд заняты смещения.
//
// Одна причина — не упрощение, а требование: пул адресов живёт в `Internal*` на
// :9091, то есть это ресурс АДМИНИСТРАТОРА. Его идентификатор, ёмкость и число
// занятых смещений на публичной поверхности — инфра-данные (security.md
// §«Инфра-чувствительные данные»), а различие перечисленных состояний выводимо
// именно из них. Оператору это различие адресовано — оно уходит в журнал и
// остаётся наблюдаемым в методе утилизации пула.
func noExternalAddressAvailable(v domain.IpVersion) error {
	return capacityRefusal(codes.FailedPrecondition, reasonExternalUnavailable,
		"no external "+familyLabel(v)+" address available", nil)
}

// noFreeSubnetAddress — в подсети НЕТ свободных адресов, и это проверено:
// перебор осмотрел всё её пространство. Код исчерпания (`RESOURCE_EXHAUSTED`) —
// повтор бессмыслен, вызывающему надо освободить адрес или взять подсеть шире.
func noFreeSubnetAddress(subnetID string, v domain.IpVersion) error {
	return capacityRefusal(codes.ResourceExhausted, reasonNoFreeSubnetAddress,
		fmt.Sprintf("subnet %s has no free %s addresses", subnetID, familyLabel(v)),
		map[string]string{"resource_type": "vpc.subnet", "resource_id": subnetID})
}

// allocationContended — занять адрес не удалось в пределах ограниченного
// перебора, и пространство подсети осмотрено НЕ полностью. Утверждать исчерпание
// здесь нельзя: оно не проверено, а конкурировавший писатель мог уже освободить
// адрес. Код `ABORTED` — та же полоса, что у прочих конкурентных конфликтов
// сервиса: вызывающий по контракту вправе безопасно повторить.
func allocationContended(subnetID string, v domain.IpVersion) error {
	return capacityRefusal(codes.Aborted, reasonAllocationContended,
		fmt.Sprintf("subnet %s: could not claim a free %s address, retry", subnetID, familyLabel(v)),
		map[string]string{"resource_type": "vpc.subnet", "resource_id": subnetID})
}

// poolResolveFailure — единый ответ на отказ каскадного резолва пула, общий для
// create-пути и internal Allocate. Ветви отвечают на РАЗНЫЕ вопросы:
//
//   - пула под этот запрос нет (`ErrPoolNotResolved`) — предусловие внешнего
//     адреса не выполнено. Собственный текст резолвера называет идентификаторы
//     адреса и сети и номер семейства; наружу уходит одна причина без координат;
//   - всё остальное — сбой чтения хранилища. Это НЕ предусловие запроса, поэтому
//     код берётся у своей полосы, а текст драйвера не уезжает (`MapRepoErr`
//     отдаёт фиксированный текст неклассифицированному отказу и пропускает
//     уже-курированный статус, сохраняя retryable-полосу соседа).
func poolResolveFailure(ctx context.Context, addressID string, v domain.IpVersion, err error) error {
	slog.ErrorContext(ctx, "allocator: address pool resolve failed",
		"address_id", addressID, "ip_version", familyLabel(v), "err", err)
	if errors.Is(err, serviceerr.ErrPoolNotResolved) {
		return noExternalAddressAvailable(v)
	}
	return serviceerr.MapRepoErr(err)
}

// poolCarriesNoBlocks — резолв дал пул, но запрошенного семейства он не выдаёт.
// Вызывающему — та же единственная причина; идентификатор пула уходит в журнал.
func poolCarriesNoBlocks(ctx context.Context, poolID, addressID string, v domain.IpVersion) error {
	slog.WarnContext(ctx, "allocator: resolved pool carries no blocks of the requested family",
		"pool_id", poolID, "address_id", addressID, "ip_version", familyLabel(v))
	return noExternalAddressAvailable(v)
}

// allocateInternalV4IntoTx — двухфазный (random-pick → deterministic sweep)
// подбор свободного IPv4 по всем subnet.V4CidrBlocks с атомарным claim'ом
// каждого кандидата через SetInternalIPv4 в открытой writer-TX. На успех — updated
// record с проставленным internal_ipv4.address; иначе — gRPC status
// (FailedPrecondition при отсутствии v4-CIDR, ResourceExhausted при исчерпании).
//
// Subnet читается через СОБСТВЕННУЮ TX writer'а (`w.Subnets().Get`), а НЕ через
// отдельный SubnetReader-порт: у writer'а уже держится одно соединение пула, и
// открытие второго (Reader на том же пуле) под held-writer'ом — nested-conn
// deadlock под нагрузкой (pool.MaxConns исчерпан writer'ами → каждый ждёт
// reader-conn, которого нет; row-lock GetForUpdate одного address'а копит очередь
// → statement_timeout). Тот же single-conn инвариант, что и на external-пути
// (см. allocate.go AllocateExternalIP: pool резолвится ДО Writer-TX).
//
// Pre-conditions (проверяет caller): addr.InternalIpv4 != nil, .Address == "",
// .SubnetID != "".
func allocateInternalV4IntoTx(ctx context.Context, w Writer, addr *kachorepo.AddressRecord) (*kachorepo.AddressRecord, error) {
	// FOR SHARE: набор диапазонов подсети читается и служит основанием для
	// записи адреса, поэтому чтение обязано быть сериализовано со снятием
	// диапазона (оно берёт FOR UPDATE). Иначе снятие могло пройти между чтением
	// набора и записью адреса, и адрес оказывался бы вне объявленных диапазонов
	// своей подсети — ровно то состояние, которое предусловие снятия и
	// запрещает. Share-lock совместим сам с собой: параллельные аллокации не
	// сериализуются.
	sub, err := w.Subnets().GetForShare(ctx, addr.InternalIpv4.SubnetID)
	if err != nil {
		return nil, err
	}
	if len(sub.V4CidrBlocks) == 0 {
		return nil, status.Errorf(codes.FailedPrecondition,
			"subnet %s has no IPv4 CIDR", sub.ID)
	}

	parsedV4Count := 0
	totalConflicts := 0
	skippedNonV4 := 0
	parseFails := 0
	// blocksFullyExamined — сколько блоков перебор осмотрел ЦЕЛИКОМ. Именно это
	// число отличает «свободных адресов нет» от «кончились попытки»: без него
	// отказ утверждал исчерпание подсети всегда, в том числе про /24, чьи 254
	// адреса тридцатью двумя попытками не осматриваются даже в принципе.
	blocksFullyExamined := 0
	for _, cidrStr := range sub.V4CidrBlocks {
		cidr, err := netip.ParsePrefix(strings.TrimSpace(cidrStr))
		if err != nil {
			parseFails++
			slog.WarnContext(ctx, "allocator: skipping unparseable subnet cidr",
				"subnet_id", sub.ID, "cidr", cidrStr, "err", err)
			continue
		}
		if !cidr.Addr().Is4() {
			skippedNonV4++
			continue
		}
		parsedV4Count++
		tried := make(map[string]struct{}, allocateMaxAttempts)
		// Phase 1: random pick.
		for attempt := 0; attempt < allocateRandomPhase; attempt++ {
			ip, err := domain.PickRandomIPv4(cidr)
			if err != nil {
				break
			}
			if _, dup := tried[ip]; dup {
				continue
			}
			tried[ip] = struct{}{}
			addr.InternalIpv4.Address = ip
			updated, err := w.Addresses().SetInternalIPv4(ctx, addr.ID, addr.InternalIpv4)
			if err != nil {
				if isUniqueViolation(err) {
					totalConflicts++
					addr.InternalIpv4.Address = ""
					continue
				}
				slog.ErrorContext(ctx, "allocator: SetInternalIPv4 returned non-conflict error",
					"subnet_id", sub.ID, "address_id", addr.ID, "ip_attempt", ip, "err", err)
				return nil, err
			}
			return updated, nil
		}
		// Phase 2: deterministic sweep.
		for _, candidate := range domain.UsableIPv4Sweep(cidr, allocateMaxAttempts-allocateRandomPhase) {
			if _, dup := tried[candidate]; dup {
				continue
			}
			tried[candidate] = struct{}{}
			addr.InternalIpv4.Address = candidate
			updated, err := w.Addresses().SetInternalIPv4(ctx, addr.ID, addr.InternalIpv4)
			if err != nil {
				if isUniqueViolation(err) {
					totalConflicts++
					addr.InternalIpv4.Address = ""
					continue
				}
				slog.ErrorContext(ctx, "allocator: SetInternalIPv4 returned non-conflict error in sweep",
					"subnet_id", sub.ID, "address_id", addr.ID, "ip_attempt", candidate, "err", err)
				return nil, err
			}
			return updated, nil
		}
		// Блок осмотрен целиком, если различных опробованных адресов не меньше,
		// чем в нём пригодных. Обе фазы берут кандидатов ТОЛЬКО из пригодного
		// диапазона, а `tried` содержит ровно те, по которым попытка занять была
		// сделана и ответила конфликтом, — поэтому равенство и означает «всё
		// пространство блока занято», а не «мы столько раз пробовали».
		if int64(len(tried)) >= domain.UsableIPv4Count(cidr.String()) {
			blocksFullyExamined++
		}
	}
	// Заголовок журнала намеренно НЕ называет исчерпание: этот же путь исполняется
	// и когда кончились попытки, и прежняя запись утверждала исчерпание в обоих
	// случаях — то есть была неверна ровно в половине своих срабатываний.
	slog.WarnContext(ctx, "allocator: internal IPv4 not claimed",
		"subnet_id", sub.ID,
		"address_id", addr.ID,
		"cidr_blocks", sub.V4CidrBlocks,
		"parsed_ipv4", parsedV4Count,
		"blocks_fully_examined", blocksFullyExamined,
		"skipped_non_v4", skippedNonV4,
		"parse_fails", parseFails,
		"unique_conflicts", totalConflicts)
	if parsedV4Count == 0 {
		return nil, status.Errorf(codes.FailedPrecondition,
			"subnet %s has no IPv4 cidr_blocks (allocator requires IPv4)", sub.ID)
	}
	// Исчерпание объявляется ТОЛЬКО когда осмотрены все блоки: иначе о
	// ненаблюдённом остатке не известно ничего, и верный ответ — «повтори».
	if blocksFullyExamined == parsedV4Count {
		return nil, noFreeSubnetAddress(sub.ID, domain.IpVersionIPv4)
	}
	return nil, allocationContended(sub.ID, domain.IpVersionIPv4)
}

// allocateInternalV6IntoTx — random-pick подбор свободного IPv6 в
// subnet.V6CidrBlocks[0] с атомарным claim'ом через SetInternalIPv6 в открытой
// writer-TX. На успех — updated record с internal_ipv6.address.
//
// Subnet читается через собственную TX writer'а (см. allocateInternalV4IntoTx:
// second-pool-conn под held-writer'ом = nested-conn deadlock под нагрузкой).
//
// Pre-conditions (проверяет caller): addr.InternalIpv6 != nil, .Address == "",
// .SubnetID != "".
func allocateInternalV6IntoTx(ctx context.Context, w Writer, addr *kachorepo.AddressRecord) (*kachorepo.AddressRecord, error) {
	// FOR SHARE — по той же причине, что и в v4-ветке.
	sub, err := w.Subnets().GetForShare(ctx, addr.InternalIpv6.SubnetID)
	if err != nil {
		return nil, err
	}
	if len(sub.V6CidrBlocks) == 0 {
		return nil, status.Errorf(codes.FailedPrecondition, "subnet %s has no v6_cidr_blocks", sub.ID)
	}
	prefix, err := netip.ParsePrefix(strings.TrimSpace(sub.V6CidrBlocks[0]))
	if err != nil || !prefix.Addr().Is6() || prefix.Addr().Is4In6() {
		return nil, status.Errorf(codes.FailedPrecondition, "subnet %s has invalid v6 cidr block %q", sub.ID, sub.V6CidrBlocks[0])
	}
	tried := make(map[string]struct{}, v6AllocateMaxAttempts)
	conflicts := 0
	for attempt := 0; attempt < v6AllocateMaxAttempts; attempt++ {
		ip, perr := domain.PickRandomIPv6(prefix)
		if perr != nil {
			// Подбор кандидата сорвался у источника случайности. Это НАША неудача,
			// а не предусловие подсети вызывающего: прежний ответ утверждал
			// обратное кодом, приглашал «поправить подсеть» и попутно уносил
			// наружу текст источника вместе с идентификатором подсети и её
			// префиксом. Разбор — оператору, вызывающему — тот же непрозрачный
			// текст, что у прочих неклассифицированных отказов этого пути.
			slog.ErrorContext(ctx, "v6 allocator: candidate pick failed",
				"subnet_id", sub.ID, "address_id", addr.ID, "cidr", prefix.String(), "err", perr)
			return nil, serviceerr.MapRepoErr(fmt.Errorf("%w: pick ipv6 candidate", repo.ErrInternal))
		}
		if _, dup := tried[ip]; dup {
			continue
		}
		tried[ip] = struct{}{}
		addr.InternalIpv6.Address = ip
		updated, uerr := w.Addresses().SetInternalIPv6(ctx, addr.ID, addr.InternalIpv6)
		if uerr != nil {
			if isUniqueViolation(uerr) {
				conflicts++
				addr.InternalIpv6.Address = ""
				continue
			}
			slog.ErrorContext(ctx, "v6 allocator: SetInternalIPv6 returned non-conflict error",
				"subnet_id", sub.ID, "address_id", addr.ID, "ip_attempt", ip, "err", uerr)
			return nil, uerr
		}
		return updated, nil
	}
	space := ipv6CandidateSpace(prefix)
	slog.WarnContext(ctx, "v6 allocator: internal IPv6 not claimed",
		"subnet_id", sub.ID, "address_id", addr.ID, "cidr", prefix.String(),
		"candidate_space", space, "distinct_tried", len(tried), "conflicts", conflicts)
	// Тот же разделитель, что и на v4-пути: исчерпание объявляется только когда
	// пространство осмотрено. Для v6 это практически всегда узкий префикс (/128 —
	// один адрес); у любого более широкого шестнадцать попыток пространство не
	// покрывают, поэтому «подсеть исчерпана» там было утверждением, которое
	// проверить нечем.
	if int64(len(tried)) >= space {
		return nil, noFreeSubnetAddress(sub.ID, domain.IpVersionIPv6)
	}
	return nil, allocationContended(sub.ID, domain.IpVersionIPv6)
}

// ipv6CandidateSpace — сколько РАЗЛИЧНЫХ адресов несёт префикс, с потолком чуть
// выше бюджета попыток.
//
// Потолок нужен по двум причинам, и обе существенны: ёмкость /64 в int64 не
// помещается вовсе, а различать «больше бюджета» и «несравнимо больше бюджета»
// незачем — перебором не покрыть ни то, ни другое. Сравнение с потолком поэтому
// даёт тот же ответ, что и с точной ёмкостью, но не переполняется.
func ipv6CandidateSpace(prefix netip.Prefix) int64 {
	hostBits := 128 - prefix.Bits()
	if hostBits <= 0 {
		return 1
	}
	if hostBits >= 32 {
		return int64(v6AllocateMaxAttempts) + 1
	}
	return int64(1) << hostBits
}

// allocateExternalV4IntoTx — pop next-free IPv4 из freelist пула
// (address_pool_free_ips) в открытой writer-TX + маппинг repo-ошибок в gRPC
// status. Возвращает выделенный IP.
//
// Pre-conditions (проверяет caller): pool резолвлен, len(pool.V4CIDRBlocks) > 0,
// address ещё не имеет external_ipv4.
func allocateExternalV4IntoTx(ctx context.Context, w Writer, poolID, addressID string) (string, error) {
	ip, err := w.Addresses().AllocateIPFromFreelist(ctx, poolID, addressID)
	if err != nil {
		if errors.Is(err, repo.ErrPoolExhausted) {
			// Идентификатор пула — оператору. Прежний текст называл его
			// вызывающему, а вместе со счётчиком занятых адресов это ёмкость
			// админского ресурса на публичной поверхности.
			slog.WarnContext(ctx, "allocator: external IPv4 pool has nothing to hand out",
				"pool_id", poolID, "address_id", addressID, "cause", "freelist empty")
			return "", noExternalAddressAvailable(domain.IpVersionIPv4)
		}
		slog.ErrorContext(ctx, "allocator: AllocateIPFromFreelist failed",
			"pool_id", poolID, "address_id", addressID, "err", err)
		return "", serviceerr.MapRepoErr(fmt.Errorf("%w: allocate from freelist", repo.ErrInternal))
	}
	return ip, nil
}

// allocateExternalV6IntoTx — sparse-counter external-IPv6 allocate в открытой
// writer-TX + маппинг repo-ошибок. Возвращает выделенный IP.
//
// Pre-conditions (проверяет caller): pool резолвлен, len(pool.V6CIDRBlocks) > 0,
// address ещё не имеет external_ipv6.
func allocateExternalV6IntoTx(ctx context.Context, w Writer, poolID, addressID, zoneID string) (string, error) {
	ip, err := w.Addresses().AllocateExternalIPv6(ctx, poolID, addressID, zoneID)
	if err != nil {
		if errors.Is(err, repo.ErrPoolExhausted) || errors.Is(err, repo.ErrFailedPrecondition) {
			// Оба исхода означают одно: пул не может выдать внешний IPv6. Пуст его
			// учёт, не заведён счётчик, нет блоков нужного семейства, подряд заняты
			// смещения — различие адресовано ОПЕРАТОРУ и уходит в журнал.
			//
			// Здесь стоял пересказ текста хранилища целиком (с обрезанным
			// префиксом сигнальной ошибки). Он называл вызывающему идентификатор
			// пула, число подряд занятых смещений и внутреннее имя процедуры
			// заведения счётчика — то есть ёмкость и устройство админского
			// ресурса, собранные в одну строку.
			slog.WarnContext(ctx, "allocator: external IPv6 pool has nothing to hand out",
				"pool_id", poolID, "address_id", addressID, "err", err)
			return "", noExternalAddressAvailable(domain.IpVersionIPv6)
		}
		slog.ErrorContext(ctx, "allocator: AllocateExternalIPv6 failed",
			"pool_id", poolID, "address_id", addressID, "err", err)
		return "", serviceerr.MapRepoErr(fmt.Errorf("%w: allocate external ipv6", repo.ErrInternal))
	}
	return ip, nil
}
