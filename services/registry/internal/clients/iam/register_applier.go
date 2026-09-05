// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// register_applier.go — register-drainer applier поверх kaname
// InternalIAMService.RegisterResource / UnregisterResource (fga-proxy). Это
// consumer-half transactional-outbox owner-tuple реле: writer-tx Create/Delete/
// Update пишет domain.RegisterIntent в registry_outbox, а drainer (corelib
// outbox/drainer) читает каждую строку и применяет её tuple-набор через kaname
// по mTLS, мапя gRPC-ответ на three-way классификацию drainer'а:
//
//	nil                       → sent_at (happy path / идемпотентный повтор)
//	drainer.ErrAlreadyApplied → sent_at (target «уже есть»)
//	drainer.ErrPermanent      → poison (attempt_count = Max)
//	прочее                    → transient (retry с exp backoff)
package iam

import (
	"context"
	"errors"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/auth"
	"github.com/PRO-Robotech/kacho/pkg/outbox/drainer"
	"github.com/PRO-Robotech/kacho/pkg/ownerregister"

	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
)

// errRegisterClientNotConfigured — iam-peer не сконфигурирован. Для drainer'а это
// transient (intent остаётся durable, ретраится после wiring'а peer'а).
var errRegisterClientNotConfigured = errors.New("iam register client not configured")

// RegisterResourceClient — узкий порт fga-proxy, нужный applier'у. Реализуется
// сгенерированным InternalIAMServiceClient; fake в тестах пишет вызовы и скриптует
// ответы. Определён здесь (consumer-side), чтобы drainer-код зависел от порта, а не
// от grpc-stub (architecture.md dependency rule).
type RegisterResourceClient interface {
	RegisterResource(ctx context.Context, in *iamv1.RegisterResourceRequest, opts ...grpc.CallOption) (*iamv1.RegisterResourceResponse, error)
	UnregisterResource(ctx context.Context, in *iamv1.UnregisterResourceRequest, opts ...grpc.CallOption) (*iamv1.UnregisterResourceResponse, error)
}

// NewRegisterResourceClient оборачивает grpc-conn к kaname internal-листенеру
// (:9091 — RegisterResource/UnregisterResource Internal-only) в порт. nil → nil.
func NewRegisterResourceClient(conn grpc.ClientConnInterface) RegisterResourceClient {
	if conn == nil {
		return nil
	}
	return iamv1.NewInternalIAMServiceClient(conn)
}

// DecodeRegisterIntent — drainer.Decoder[domain.RegisterIntent] для
// registry_outbox.payload. Malformed JSON / пустой tuple-набор / неполный tuple →
// drainer.ErrPermanent (poison, не бесконечный retry).
func DecodeRegisterIntent(payload []byte) (domain.RegisterIntent, error) {
	i, err := domain.UnmarshalRegisterIntent(payload)
	if err != nil {
		return domain.RegisterIntent{}, fmt.Errorf("%w: registry_outbox: invalid json: %s", drainer.ErrPermanent, err)
	}
	if len(i.Tuples) == 0 {
		return domain.RegisterIntent{}, fmt.Errorf("%w: registry_outbox: empty tuple set", drainer.ErrPermanent)
	}
	for idx, t := range i.Tuples {
		if !t.Valid() {
			return domain.RegisterIntent{}, fmt.Errorf(
				"%w: registry_outbox: incomplete tuple[%d] (subject=%q relation=%q object=%q)",
				drainer.ErrPermanent, idx, t.SubjectID, t.Relation, t.Object)
		}
	}
	return i, nil
}

// NewRegisterApplier — drainer.Applier[domain.RegisterIntent] поверх fga-proxy.
// На каждый tuple вызывает RegisterResource (fga.register) либо UnregisterResource
// (fga.unregister).
//
// КАЖДЫЙ tuple обрабатывается идемпотентно ВСЕГДА, до конца набора: ErrAlreadyApplied
// на отдельном tuple — это per-tuple success (target «уже есть»), НЕ повод обрывать
// остаток набора. Это критично для at-least-once retry-after-partial-apply: если
// attempt-1 применил project-tuple, но упал на owner-tuple (transient) → drainer
// ретраит строку; на attempt-2 iam отвечает AlreadyExists на уже-применённый
// project-tuple, и applier обязан пойти ДАЛЬШЕ и всё-таки применить owner-tuple.
// Ранний return на первом AlreadyExists ронял owner-tuple навсегда (drainer помечал
// строку sent, не дойдя до второго tuple) — нарушение «owner-tuple не теряется».
//
// Только ErrPermanent (poison) и transient short-circuit'ят набор — drainer
// ретраит/травит всю строку, и оставшиеся tuple будут повторно опрошены. Терминальный
// ErrAlreadyApplied всплывает наверх лишь когда КАЖДЫЙ tuple ответил already-applied
// (нулевая реальная работа) — тогда classify-метрика видит already_applied, а не
// success; если хоть один tuple сделал реальную работу — возвращается nil (оба
// класса → sent_at, но метрика точнее).
func NewRegisterApplier(cli RegisterResourceClient) drainer.Applier[domain.RegisterIntent] {
	return func(ctx context.Context, eventType string, intent domain.RegisterIntent) error {
		if cli == nil {
			// iam-peer не сконфигурирован — transient (intent durable до wiring'а).
			return errRegisterClientNotConfigured
		}
		// PropagateOutgoing — iam-side principal-extractor видит контекст; identity
		// least-priv fga_writer приходит из mTLS client-cert.
		ctx = auth.PropagateOutgoing(ctx)

		// Монотонный source_version, застампленный БД внутри writer-tx (миграция
		// 0011). На register-пути он делает применение зеркала
		// last-source-state-wins И даёт kaname положительное доказательство
		// редоставки: синхронный registrar штампует свою версию уже ПОСЛЕ commit'а,
		// поэтому эта — заведомо не новее, зеркало не меняется, и iam пропускает
		// повторную материализацию. На unregister-пути это TOMBSTONE: iam удаляет
		// строку зеркала под `source_version <= $tombstone`, и unregister без версии
		// ('-infinity') не смог бы снять версионированную строку. Zero (legacy-строка
		// до 0011) → nil → '-infinity' → безусловное применение.
		//
		// Версия ШАГАЕТ на tuple внутри одной строки: gate ключуется на строке зеркала,
		// а она — ПО ОБЪЕКТУ, и один intent несёт несколько tuple'ов ОДНОГО объекта
		// (project-hierarchy + creator-owner). С одинаковой версией второй вызов
		// зеркало не меняет и неотличим от редоставки — gate проглотил бы вместе с ним
		// и постановку owner-tuple. Шаг микросекундный, поэтому строка целиком
		// по-прежнему проигрывает синхронной доставке и гейтится как редоставка.
		var apply func(t domain.FGATuple, seq int) error
		switch eventType {
		case domain.FGAEventRegister:
			apply = func(t domain.FGATuple, seq int) error {
				_, err := cli.RegisterResource(ctx, &iamv1.RegisterResourceRequest{
					SubjectId:       t.SubjectID,
					Relation:        t.Relation,
					Object:          t.Object,
					TraceId:         intent.ResourceID,
					Labels:          intent.Labels,
					ParentProjectId: intent.ParentProjectID,
					// Цепь идёт ОБОИМИ путями доставки — синхронным и очередным.
					// Проведи её одним, и повтор из очереди затёр бы цепь пустой:
					// доставки одного намерения обязаны нести одно содержание.
					//
					// Названная владельцем цепь едет как есть: у репозитория
					// иерархия глубже проекта (репозиторий → реестр → проект), и
					// из области её не вывести. Не названная — выводится из
					// области ЭТОЙ ЖЕ доставки: у самого реестра предок один,
					// проект, и он до сих пор ехал только полем области, из-за
					// чего перерегистрация реестра стирала его ребро.
					ParentChain:   ownerregister.ParentChain(intent.ParentChain, intent.ParentProjectID, ""),
					SourceVersion: stepSourceVersion(intent.SourceVersion.Time, seq),
				})
				return err
			}
		case domain.FGAEventUnregister:
			apply = func(t domain.FGATuple, seq int) error {
				_, err := cli.UnregisterResource(ctx, &iamv1.UnregisterResourceRequest{
					SubjectId:       t.SubjectID,
					Relation:        t.Relation,
					Object:          t.Object,
					TraceId:         intent.ResourceID,
					Labels:          intent.Labels,
					ParentProjectId: intent.ParentProjectID,
					SourceVersion:   stepSourceVersion(intent.SourceVersion.Time, seq),
				})
				return err
			}
		default:
			return fmt.Errorf("%w: registry_outbox: unknown event_type %q", drainer.ErrPermanent, eventType)
		}

		// allAlreadyApplied — становится false, как только хоть один tuple сделал
		// реальную работу (nil-ответ). Стартовое true покрывает пустой набор (decoder
		// такой уже отсеял как poison, но keep-honest на случай прямого вызова).
		allAlreadyApplied := true
		for seq, t := range intent.Tuples {
			switch cerr := classifyRegisterErr(apply(t, seq)); {
			case cerr == nil:
				allAlreadyApplied = false
			case errors.Is(cerr, drainer.ErrAlreadyApplied):
				// per-tuple идемпотентный success — идём к следующему tuple, НЕ обрываем.
			default:
				// ErrPermanent или transient — обрываем; drainer ретраит/травит строку,
				// оставшиеся tuple будут опрошены заново на следующем attempt'е.
				return cerr
			}
		}
		if allAlreadyApplied && len(intent.Tuples) > 0 {
			return fmt.Errorf("%w: all tuples already applied", drainer.ErrAlreadyApplied)
		}
		return nil
	}
}

// sourceVersionStep — шаг версии между tuple'ами ОДНОЙ доставки. Ровно микросекунда:
// это разрешение timestamptz, в котором kaname хранит маркер, — меньший шаг
// схлопнулся бы при записи, больший без нужды съедал бы зазор до версии следующей
// мутации.
const sourceVersionStep = time.Microsecond

// stepSourceVersion — версия seq-го tuple доставки, стартующей с base. Нулевой base
// (версии нет — строка, поставленная в очередь до миграции 0011, где лежал BIGSERIAL)
// остаётся nil на ВСЕХ tuple'ах: kaname трактует nil как '-infinity', а gate
// редоставки при отсутствии версии обязан открыться в сторону работы для ВСЕГО набора,
// а не для его части.
func stepSourceVersion(base time.Time, seq int) *timestamppb.Timestamp {
	if base.IsZero() {
		return nil
	}
	return timestamppb.New(base.Add(time.Duration(seq) * sourceVersionStep))
}

// classifyRegisterErr мапит gRPC-ответ RegisterResource/UnregisterResource на
// three-way классификацию drainer'а:
//
//	nil                    → nil (применено / идемпотентный OK)
//	AlreadyExists          → ErrAlreadyApplied (target «уже есть» — success)
//	InvalidArgument        → ErrPermanent (malformed tuple — retry бессмыслен)
//	PermissionDenied       → ErrPermanent (идентичный повтор не меняет решения об
//	                         авторизации; см. ниже, почему «transient» здесь
//	                         заклинивает партицию, а не лечит)
//	прочее                 → raw (transient — drainer ретраит; intent durable)
func classifyRegisterErr(err error) error {
	if err == nil {
		return nil
	}
	st, ok := status.FromError(err)
	if !ok {
		return err
	}
	// Отказ по правам терминален, а не временен. Решение об авторизации зависит от
	// (вызывающий, отношение, объект), и повтор не меняет ни одного из трёх, поэтому
	// идентичный повтор пройти не может. «Transient» здесь не покупает будущий успех:
	// дренаж намеренно держит временную строку на единицу НИЖЕ порога отравления,
	// поэтому она никогда не покидает блокирующий набор claim-запроса, и ни одна
	// последующая строка её партиции не клеймится. Партиция — это ресурс, а снятие
	// регистрации стоит в очереди ЗА регистрацией: заклиненная голова означает грант,
	// переживший удаление ресурса. Отравление, наоборот, отказывает закрыто:
	// отвергнутая запись не состоялась, партиция разблокирована. Само по себе оно
	// НЕ самоисцеляется — недоставленная регистрация оставляет объект без
	// mirror-строки в kaname, — поэтому идёт в паре с периодическим
	// redrive-бэкстопом (cmd/kacho-registry/redrive_backstop.go).
	switch st.Code() {
	case codes.AlreadyExists:
		return fmt.Errorf("%w: iam register reports duplicate: %s", drainer.ErrAlreadyApplied, st.Message())
	case codes.InvalidArgument, codes.PermissionDenied:
		return fmt.Errorf("%w: iam register rejected (no retry): %s", drainer.ErrPermanent, st.Message())
	default:
		return err
	}
}

// Compile-time guards — возвращаемые Applier/Decoder совпадают с generic-сигнатурами
// drainer'а (рассинхрон сигнатур падает здесь, а не на месте wiring'а в main).
var _ drainer.Applier[domain.RegisterIntent] = NewRegisterApplier(nil)
var _ drainer.Decoder[domain.RegisterIntent] = DecodeRegisterIntent
