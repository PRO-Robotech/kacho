// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionjournal_test

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/dto"
	_ "github.com/PRO-Robotech/kacho/services/nlb/internal/dto/type2pb" // регистрация трансферов
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
	pgrepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho/pg"
)

// loadbalancer_state_integration_test.go — У ВИДА `nlb_load_balancer` ДВА
// ПРОИЗВОДИТЕЛЯ, И ОНИ ОБЯЗАНЫ ГОВОРИТЬ ОДНО.
//
// # Предмет
//
// Семь точек эмиссии этого вида — на Go, восьмая — ТРИГГЕР БАЗЫ
// (`lb_status_recompute`), и у неё свой язык: `to_jsonb(строки)` даёт ключи имён
// КОЛОНОК. Контракт единой формы разрешает подписчику читать непустое состояние
// как ПОЛНОЕ, поэтому расхождение двух форм не даёт ни отказа, ни пустоты — оно
// даёт ПОЛНОЕ состояние, в котором части полей нет, и подписчик записывает это
// как факт.
//
// # Почему проба, а не разбор дерева
//
// Разбор пакетов use-case (`TestEveryEmissionOfAStatefulKindBuildsTheSamePayload`)
// судит вызовы Go и триггера не видит вовсе. Гейт по тексту миграций судить его
// тоже не может: живое тело функции — последнее из череды переопределений, а
// прежние лежат в ПРИМЕНЁННЫХ миграциях, править которые нельзя (ban #5), и
// текстовая проверка краснела бы на собственной истории.
//
// Живое тело знает только база. Поэтому проба проигрывает ВСЮ цепочку миграций,
// заставляет триггер сработать настоящим оператором и сверяет его нагрузку с
// нагрузкой строителя Go — на ОДНОЙ И ТОЙ ЖЕ строке.

const (
	probeTG           = "nlb-tg-1234567890abc"
	probeStateRegion  = "ru-central1"
	probeStateProject = probeProject
)

// richLoadBalancer — строка, у которой заполнено ВСЁ, что вообще можно
// заполнить законно.
//
// Бедная строка сделала бы пробу вакуумной: у неё совпали бы и две верные формы,
// и две разные — сравнивать было бы нечего. INTERNAL/REGIONAL выбран не по вкусу,
// а по ограничениям базы: непустой набор зон разрешён только REGIONAL-размещению,
// а непустой набор групп безопасности — только INTERNAL-типу.
func richLoadBalancer() *domain.LoadBalancer {
	return &domain.LoadBalancer{
		ID:                    domain.ResourceID(probeLB),
		ProjectID:             domain.ProjectID(probeStateProject),
		RegionID:              domain.RegionID(probeStateRegion),
		Name:                  domain.LbName("front"),
		Description:           domain.LbDescription("пробный балансировщик"),
		Labels:                domain.LabelsFromMap(map[string]string{"env": "prod", "tier": "edge"}),
		Type:                  domain.LBTypeInternal,
		Status:                domain.LBStatusInactive,
		SessionAffinity:       domain.SessionAffinityClientIPOnly,
		DeletionProtection:    true,
		PlacementType:         domain.PlacementRegional,
		Placement:             domain.PlacementInternalRegional,
		DisabledAnnounceZones: []string{"ru-central1-b"},
		IPFamilies:            []domain.IPVersion{domain.IPVersionV4, domain.IPVersionV6},
		AddressV4:             domain.IPAddress("10.0.0.7"),
		AddressIDV4:           domain.AddressID("addr-1234567890abcde"),
		VipOriginV4:           domain.VipOriginAuto,
		AdminState:            domain.AdminStateDisabled,
		CrossZoneEnabled:      true,
		SecurityGroupIDs:      []string{"sg-1234567890abcdef"},
	}
}

// seedQuotaCeiling — предусловие записи, а не предмет пробы: без объявленного
// потолка вставка отвергается стражем учёта («не создано условие», не дефект).
func seedQuotaCeiling(t *testing.T, s *stand) {
	t.Helper()
	ctx := context.Background()
	for _, kind := range []string{
		"loadbalancer.networkLoadBalancers",
		"loadbalancer.targetGroups",
		"loadbalancer.listeners",
	} {
		if _, err := s.pool.Exec(ctx, `
			INSERT INTO kacho_nlb.project_resource_quotas
				(carrier_type, carrier_id, kind, used, limit_value,
				 source_scope, source_scope_id, limit_revision, account_id)
			VALUES ('project', $1, $2, 0, 64, 'DEFAULT', '', 0, 'acc-probe')
			ON CONFLICT (carrier_type, carrier_id, kind) DO NOTHING`,
			probeStateProject, kind); err != nil {
			t.Fatalf("потолок учёта не заведён (%s): %v", kind, err)
		}
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO kacho_nlb.nested_quota_defaults
			(project_id, kind, limit_value, source_scope, source_scope_id,
			 limit_revision, account_id)
		VALUES ($1, 'loadbalancer.networkLoadBalancers.listeners', 64, 'DEFAULT', '', 0, 'acc-probe')
		ON CONFLICT (project_id, kind) DO NOTHING`, probeStateProject); err != nil {
		t.Fatalf("потолок учёта вложенного вида не заведён: %v", err)
	}
}

// seedRichLB кладёт строку НАСТОЯЩИМ репозиторием и возвращает её запись.
func seedRichLB(t *testing.T, s *stand) *kachorepo.LoadBalancerRecord {
	t.Helper()
	ctx := context.Background()
	seedQuotaCeiling(t, s)
	repo := pgrepo.New(s.pool, nil)

	w, err := repo.Writer(ctx)
	if err != nil {
		t.Fatalf("writer не открылся: %v", err)
	}
	defer w.Abort()
	rec, err := w.LoadBalancers().Insert(ctx, richLoadBalancer())
	if err != nil {
		t.Fatalf("балансировщик не записался: %v", err)
	}
	if err := w.Commit(); err != nil {
		t.Fatalf("транзакция не зафиксировалась: %v", err)
	}
	return rec
}

// fireStatusRecompute заставляет ТРИГГЕР сработать: привязанный слушатель
// переводит балансировщик INACTIVE → ACTIVE, и триггер пишет свою строку журнала.
//
// Целевая группа и слушатель кладутся своим оператором — они здесь ПРЕДУСЛОВИЕ,
// а не предмет: предмет пробы — нагрузка, которую пишут два производителя вида
// `nlb_load_balancer`.
func fireStatusRecompute(t *testing.T, s *stand) {
	t.Helper()
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO kacho_nlb.target_groups (id, project_id, region_id, name, port)
		VALUES ($1, $2, $3, 'tg', 8080)`,
		probeTG, probeStateProject, probeStateRegion); err != nil {
		t.Fatalf("целевая группа не записалась: %v", err)
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO kacho_nlb.listeners
			(id, load_balancer_id, project_id, region_id, name, protocol, port,
			 default_target_group_id, status)
		VALUES ($1, $2, $3, $4, 'front', 'TCP', 443, $5, 'ACTIVE')`,
		probeListener, probeLB, probeStateProject, probeStateRegion, probeTG); err != nil {
		t.Fatalf("слушатель не записался: %v", err)
	}
}

// projectionOf — состояние, как его собирает СЛУЖБА на чтении: тот же трансфер,
// каким отвечает `Get`.
func projectionOf(t *testing.T, rec *kachorepo.LoadBalancerRecord) *lbv1.NetworkLoadBalancer {
	t.Helper()
	var pb *lbv1.NetworkLoadBalancer
	if err := dto.Transfer(dto.FromTo(*rec, &pb)); err != nil {
		t.Fatalf("штатное отображение записи в контракт не собралось: %v", err)
	}
	return pb
}

// stateOfEvent — состояние события, распакованное из конверта контракта.
func stateOfEvent(t *testing.T, ev *subscriptionv1.SubscriptionEvent, who string) *lbv1.NetworkLoadBalancer {
	t.Helper()
	if ev.GetState() == nil {
		t.Fatalf("%s: состояние НЕ доехало (причина %v) — клиентский отбор по меткам для "+
			"балансировщика остался бы без источника", who, ev.GetStateUnavailable().GetReason())
	}
	var got lbv1.NetworkLoadBalancer
	if err := ev.GetState().UnmarshalTo(&got); err != nil {
		t.Fatalf("%s: в конверте состояния не контракт балансировщика: %v", who, err)
	}
	return &got
}

// TestLoadBalancerStateIsTheSameFromTheTriggerAndFromGo — ДВЕ ФОРМЫ ОДНОГО
// КОНВЕРТА СВЕДЕНЫ, и сведение проверено НА ОДНОЙ СТРОКЕ.
//
// Сравниваются ТРИ предмета, а не два, и третий несущий: состояние события
// триггера · состояние события Go · штатное отображение, каким отвечает чтение.
// Пара без третьего зеленела бы на двух одинаково НЕВЕРНЫХ формах.
func TestLoadBalancerStateIsTheSameFromTheTriggerAndFromGo(t *testing.T) {
	s := newStand(t)

	seedRichLB(t, s)
	// Строка триггера пишется ЗДЕСЬ — пересчётом статуса на вставке слушателя.
	fireStatusRecompute(t, s)

	ctx := context.Background()
	repo := pgrepo.New(s.pool, nil)
	r, err := repo.Reader(ctx)
	if err != nil {
		t.Fatalf("reader не открылся: %v", err)
	}
	defer func() { _ = r.Close() }()
	after, err := r.LoadBalancers().Get(ctx, probeLB)
	if err != nil {
		t.Fatalf("балансировщик не прочитался: %v", err)
	}
	if after.Status != domain.LBStatusActive {
		t.Fatalf("триггер не пересчитал статус (%q) — строки триггера в журнале нет, "+
			"и проба судила бы об одном производителе вместо двух", after.Status)
	}

	// Строка Go — тем же строителем, каким её кладут семь точек эмиссии.
	s.emit(t, kachorepo.OutboxResourceLoadBalancer, probeLB, probeStateProject,
		kachorepo.OutboxActionUpdated, kachorepo.LoadBalancerStatePayload(after))

	sctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	stream := s.subscribe(t, sctx, []string{authzfilter.ResourceTypeLoadBalancer})
	fromTrigger := stateOfEvent(t, recv(t, stream), "событие ТРИГГЕРА")
	fromGo := stateOfEvent(t, recv(t, stream), "событие Go")

	want := projectionOf(t, after)

	// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: сравнивать есть что. Равенство двух пустых
	// сообщений истинно by construction и о формах не говорит ничего.
	if want.GetName() == "" || len(want.GetLabels()) == 0 || len(want.GetSecurityGroupIds()) == 0 {
		t.Fatalf("предмет сравнения беден (%v): равенство форм зеленело бы на пустоте", want)
	}

	if !proto.Equal(fromTrigger, want) {
		t.Errorf("состояние из строки ТРИГГЕРА не равно тому, что отдаёт чтение.\n"+
			"  событие: %v\n  чтение:  %v\n"+
			"Ключи триггера — имена КОЛОНОК; расхождение с формой Go не даёт ни отказа, "+
			"ни пустоты — подписчик получает ПОЛНОЕ состояние без части полей и записывает "+
			"это как факт", fromTrigger, want)
	}
	if !proto.Equal(fromGo, want) {
		t.Errorf("состояние из строки Go не равно тому, что отдаёт чтение.\n"+
			"  событие: %v\n  чтение:  %v", fromGo, want)
	}
	if !proto.Equal(fromTrigger, fromGo) {
		t.Errorf("ДВА ПРОИЗВОДИТЕЛЯ ОДНОГО ВИДА ГОВОРЯТ РАЗНОЕ.\n"+
			"  триггер: %v\n  Go:      %v\n"+
			"Вид объявлен несущим полное состояние ВСЁ-ИЛИ-НИЧЕГО: одна форма, отдающая "+
			"частичный снимок, делает ложным весь вид — и делает тихо", fromTrigger, fromGo)
	}
}

// TestRecomputeEmitsOnlyWhenTheStatusActuallyChanged — строка триггера пишется
// РОВНО тогда, когда пересчёт применился.
//
// # Почему это отдельная проба, а не деталь предыдущей
//
// Форма нагрузки сменилась на строку, забираемую `RETURNING` того же оператора,
// а признак «пересчёт применился» — с числа затронутых строк на непустоту
// забранной строки. Признак сменился, и он единственный стоит между промахом
// CAS и событием, у которого состояние заполнено НУЛЯМИ. Это худший из исходов:
// контракт формы разрешает читать непустое состояние как ПОЛНОЕ, и подписчик
// записал бы как факт балансировщик без имени, без проекта и без меток.
//
// Отрицание не вакуумно: положительный контроль — та же вставка слушателя, но
// ПРИВЯЗАННОГО, в пробе выше; здесь слушатель не привязан, статус не меняется, и
// строки быть не должно.
func TestRecomputeEmitsOnlyWhenTheStatusActuallyChanged(t *testing.T) {
	s := newStand(t)
	ctx := context.Background()

	seedRichLB(t, s)

	// Слушатель БЕЗ привязки к группе: `has_wired` остаётся ложью, статус
	// остаётся INACTIVE, пересчёту нечего применять.
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO kacho_nlb.listeners
			(id, load_balancer_id, project_id, region_id, name, protocol, port, status)
		VALUES ($1, $2, $3, $4, 'idle', 'TCP', 8080, 'ACTIVE')`,
		probeListener, probeLB, probeStateProject, probeStateRegion); err != nil {
		t.Fatalf("слушатель без привязки не записался: %v", err)
	}

	var rows int
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM kacho_nlb.nlb_outbox
		 WHERE resource_type = $1 AND resource_id = $2`,
		kachorepo.OutboxResourceLoadBalancer, probeLB).Scan(&rows); err != nil {
		t.Fatalf("перепись строк журнала не собралась: %v", err)
	}
	if rows != 0 {
		t.Fatalf("строк журнала у балансировщика %d, ожидался 0: пересчёт не применялся "+
			"(статус не менялся), а событие с НЕЗАПОЛНЕННЫМ состоянием подписчик прочёл бы "+
			"как полное — балансировщик без имени, проекта и меток", rows)
	}

	// Положительный контроль ТУТ ЖЕ: привязка того же слушателя статус меняет, и
	// строка обязана появиться. Без него «ноль строк» было бы неотличимо от
	// «триггер снят вовсе».
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO kacho_nlb.target_groups (id, project_id, region_id, name, port)
		VALUES ($1, $2, $3, 'tg', 8080)`,
		probeTG, probeStateProject, probeStateRegion); err != nil {
		t.Fatalf("целевая группа не записалась: %v", err)
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE kacho_nlb.listeners SET default_target_group_id = $2 WHERE id = $1`,
		probeListener, probeTG); err != nil {
		t.Fatalf("привязка слушателя не записалась: %v", err)
	}
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM kacho_nlb.nlb_outbox
		 WHERE resource_type = $1 AND resource_id = $2`,
		kachorepo.OutboxResourceLoadBalancer, probeLB).Scan(&rows); err != nil {
		t.Fatalf("перепись строк журнала не собралась: %v", err)
	}
	if rows != 1 {
		t.Fatalf("строк журнала у балансировщика %d, ожидалась 1: пересчёт применился, и "+
			"событие обязано быть ровно одно", rows)
	}
}
