// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package targetgroup

import (
	"fmt"

	"github.com/PRO-Robotech/kacho/pkg/option"
	"google.golang.org/protobuf/types/known/anypb"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// healthCheckFromPb — конвертер proto HealthCheck → domain HealthCheck (NLB-1c
// redesigned: без `name`; 4-way oneof tcp/http/https/grpc; http/https несут
// expected_codes/host/headers). probe-port override опционален (0 → наследует
// TargetGroup.port).
//
// Возвращает zero-value HealthCheck если pb==nil — caller'у тогда лучше отдать
// ошибку «health_check is required» вверх. Threshold/port поля int64 на wire;
// сужение в int32 guard'ится (domain.*FromProto) — иначе overflow-значение
// молча алиасит на валидный остаток и обходит HealthCheck.Validate (gosec G115).
//
// `effective_port` отвергается ЗДЕСЬ, а не у каждого вызывающего: конвертер —
// единственное место, через которое тело проверки живости попадает на путь
// записи (создание и правка), поэтому отказ, поставленный в него, покрывает обе
// формы правки — маску целиком и точечную — и не может разойтись между ними.
// Симметрично соседям той же природы (`targetsFromPbForWrite`), только те
// принимают имя поля параметром: у цели путь индексирован номером в наборе, а у
// проверки живости он один и тот же на обоих глаголах.
func healthCheckFromPb(pb *lbv1.HealthCheck) (domain.HealthCheck, error) {
	if pb == nil {
		return domain.HealthCheck{}, nil
	}
	// Величина выводится сервером (порт пробы, иначе порт группы) и на записи не
	// читается ничем. Принять её значило бы вернуть успех на запрос, который
	// ничего не изменил: ответ пересчитает поле сам. Совпадение с выводимым
	// значением отказа не отменяет — иначе исход зависел бы от того, угадал ли
	// вызывающий, а поле вернулось бы в разряд принимаемых на первой же смене
	// правила вывода.
	if pb.GetEffectivePort() != 0 {
		return domain.HealthCheck{}, errInvalidArg("health_check.effective_port",
			"effective_port is output-only; it is derived from the probe port override, otherwise the group's port")
	}
	unhealthy, err := domain.HealthThresholdFromProto("unhealthy_threshold", pb.GetUnhealthyThreshold())
	if err != nil {
		return domain.HealthCheck{}, err
	}
	healthy, err := domain.HealthThresholdFromProto("healthy_threshold", pb.GetHealthyThreshold())
	if err != nil {
		return domain.HealthCheck{}, err
	}
	hc := domain.HealthCheck{
		Interval:           domain.LbDuration(pb.GetInterval().AsDuration()),
		Timeout:            domain.LbDuration(pb.GetTimeout().AsDuration()),
		UnhealthyThreshold: unhealthy,
		HealthyThreshold:   healthy,
	}
	switch v := pb.GetOptions().(type) {
	case *lbv1.HealthCheck_Tcp:
		port, err := domain.LbPortFromProto(v.Tcp.GetPort())
		if err != nil {
			return domain.HealthCheck{}, err
		}
		hc.TCP = &domain.HealthCheckTCP{Port: port}
	case *lbv1.HealthCheck_Http:
		port, err := domain.LbPortFromProto(v.Http.GetPort())
		if err != nil {
			return domain.HealthCheck{}, err
		}
		hc.HTTP = &domain.HealthCheckHTTP{
			Port:          port,
			Path:          v.Http.GetPath(),
			ExpectedCodes: v.Http.GetExpectedCodes(),
			Host:          v.Http.GetHost(),
			Headers:       v.Http.GetHeaders(),
		}
	case *lbv1.HealthCheck_Https:
		port, err := domain.LbPortFromProto(v.Https.GetPort())
		if err != nil {
			return domain.HealthCheck{}, err
		}
		hc.HTTPS = &domain.HealthCheckHTTPS{
			Port:          port,
			Path:          v.Https.GetPath(),
			ExpectedCodes: v.Https.GetExpectedCodes(),
			Host:          v.Https.GetHost(),
			Headers:       v.Https.GetHeaders(),
		}
	case *lbv1.HealthCheck_Grpc:
		port, err := domain.LbPortFromProto(v.Grpc.GetPort())
		if err != nil {
			return domain.HealthCheck{}, err
		}
		hc.GRPC = &domain.HealthCheckGRPC{
			Port:        port,
			ServiceName: v.Grpc.GetServiceName(),
		}
	}
	return hc, nil
}

// targetFromPb — конвертер proto Target → domain Target. 4-way identity oneof.
// Validate в domain отлавливает «0 либо 2+ identities заданы», так что здесь
// просто mirror'им proto.
func targetFromPb(pb *lbv1.Target) domain.Target {
	if pb == nil {
		return domain.Target{}
	}
	t := domain.Target{Weight: domain.LbWeight(pb.GetWeight())}
	switch id := pb.GetIdentity().(type) {
	case *lbv1.Target_InstanceId:
		t.InstanceID = option.MustNewOption(domain.InstanceID(id.InstanceId))
	case *lbv1.Target_NicId:
		t.NicID = option.MustNewOption(domain.NicID(id.NicId))
	case *lbv1.Target_IpRef:
		t.IPRef = &domain.TargetIPRef{
			SubnetID: domain.SubnetID(id.IpRef.GetSubnetId()),
			Address:  domain.IPAddress(id.IpRef.GetAddress()),
		}
	case *lbv1.Target_ExternalIp:
		ext := &domain.TargetExternalIP{Address: domain.IPAddress(id.ExternalIp.GetAddress())}
		if z := id.ExternalIp.GetZoneId(); z != "" {
			ext.ZoneID = option.MustNewOption(domain.ZoneID(z))
		}
		t.ExternalIP = ext
	}
	return t
}

// targetsFromPb — конвертер repeated proto.Target → domain.Target.
//
// Читает ТОЛЬКО идентичность и вес: `status`/`drain_started_at` объявлены
// output-only и решаются RemoveTargets с drain-runner'ом, а не вызывающим.
// Пути, где цель СОЗДАЁТСЯ, обязаны идти через targetsFromPbForWrite — иначе
// значение принималось бы и выбрасывалось молча.
func targetsFromPb(pbs []*lbv1.Target) []domain.Target {
	if len(pbs) == 0 {
		return nil
	}
	out := make([]domain.Target, 0, len(pbs))
	for _, pb := range pbs {
		out = append(out, targetFromPb(pb))
	}
	return out
}

// targetsFromPbForWrite — тот же конвертер для путей, СОЗДАЮЩИХ цель
// (`CreateTargetGroup.targets`, `AddTargets.targets`): output-only поля там
// отвергаются синхронно, с именем поля. Принять их значило бы пообещать, что
// цель можно добавить уже сливающейся, — возможности, которой нет.
//
// Асимметрия названа намеренно: `RemoveTargets.targets` те же поля ИГНОРИРУЕТ,
// потому что сопоставление там идёт по идентичности, и естественный поток
// «прочитал группу → передал цель на снятие» обязан продолжать работать. Отказ
// на снятии наказывал бы за круговой обмен собственным ответом сервиса.
func targetsFromPbForWrite(field string, pbs []*lbv1.Target) ([]domain.Target, error) {
	for i, pb := range pbs {
		if pb.GetStatus() != lbv1.Target_STATUS_UNSPECIFIED {
			return nil, errInvalidArg(fmt.Sprintf("%s[%d].status", field, i),
				"status is output-only; it is set by RemoveTargets and the drain runner")
		}
		if pb.GetDrainStartedAt() != nil {
			return nil, errInvalidArg(fmt.Sprintf("%s[%d].drain_started_at", field, i),
				"drain_started_at is output-only; it is set by RemoveTargets and the drain runner")
		}
	}
	return targetsFromPb(pbs), nil
}

// Строитель нагрузки вида `nlb_target_group` живёт НЕ ЗДЕСЬ, а в repo-leaf —
// `kachorepo.TargetGroupStatePayload`, рядом со своим читателем.
//
// # Здесь стояли ДВА строителя минимального снимка — их больше нет
//
// `tgOutboxPayload` собирал пять полей строки, `tgMovedPayload` — идентификатор
// и пару проектов; ещё две точки эмиссии (добавление и снятие целей) строили
// нагрузку СВОИМ литералом прямо на месте. Четыре формы у одного вида, и ни
// одна не несла целей.
//
// Событие вида теперь несёт ПОЛНОЕ состояние, а контракт формы разрешает
// подписчику читать непустое состояние как ПОЛНОЕ. Значит форма обязана быть
// ОДНА: одна точка с частичным снимком делает ложным весь вид, и делает тихо.
// Держит это разбор дерева use-case'ов
// (`subscriptionjournal.TestEveryEmissionOfAStatefulKindBuildsTheSamePayload`).
//
// # Что при этом ПОТЕРЯНО, названо вслух
//
// Ключи `old_project_id`/`new_project_id` события переезда в нагрузку больше не
// идут. Читателя у них не было ни одного, а якорь проекта в колонке несёт уже
// целевой — то есть подписчик источника этой строки не получает by construction
// (подписка сужается по проекту). Исходный проект восстанавливается прежним
// событием той же группы, а не полем, за которым никто не следит.

// marshalTargetGroup — anypb.New(TargetGroup) для Operation.Response.
func marshalTargetGroup(rec *kachorepo.TargetGroupRecord) (*anypb.Any, error) {
	pb, err := tgRecordToProto(rec)
	if err != nil {
		return nil, err
	}
	return anypb.New(pb)
}
