// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// sync_registrar.go — синхронный owner-tuple registrar поверх kacho-iam
// InternalIAMService.RegisterResource (fga-proxy). Мирроринг storage
// SyncRegistrar: Create-flow после durable-commit ресурса СИНХРОННО регистрирует
// те же register-tuple'ы, что эмитятся в registry_outbox — чтобы owner/pull-grant
// был доступен сразу, без гонки с async register-drainer'ом (иначе под burst
// создания repo/registry drainer сериализуется → owner-tuple лагает → repo GET 404
// в окне материализации).
//
// Register-ONLY: применяет register-tuple; unregister идёт исключительно
// async-drainer'ом. Каждый tuple → один RegisterResource с per-call 5s deadline
// (architecture.md: per-call deadline на КАЖДОМ внешнем вызове). Первая ошибка
// прекращает набор и возвращается наверх — вызывающий (use-case) логирует WARN и
// продолжает (durable outbox-intent + drainer остаются at-least-once backstop'ом).
// Field-mapping — 1:1 parity с NewRegisterApplier (register-ветка).
package iam

import (
	"context"
	"errors"
	"fmt"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"

	registry "github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/api/registry"
	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
)

// errSyncRegisterClientNotConfigured — iam-peer не сконфигурирован (nil client). В проде
// serve.go подключает sync-registrar только при непустом iamConn, поэтому это defensive.
var errSyncRegisterClientNotConfigured = errors.New("iam register client not configured")

// SyncRegistrar реализует use-case-порт registry.SyncRegistrar поверх
// RegisterResourceClient. Тот же mTLS-conn к kacho-iam :9091, что и register-drainer.
type SyncRegistrar struct {
	cli     RegisterResourceClient
	timeout time.Duration
}

// NewSyncRegistrar собирает registrar поверх RegisterResourceClient. timeout по умолчанию
// 5s на один RegisterResource (create-path может идти на ctx без жёсткого дедлайна —
// ограничиваем здесь; per-call deadline, architecture.md).
func NewSyncRegistrar(cli RegisterResourceClient) *SyncRegistrar {
	return &SyncRegistrar{cli: cli, timeout: 5 * time.Second}
}

// Register синхронно регистрирует каждый tuple каждого intent через iam RegisterResource.
// Field-mapping (SubjectId/Relation/Object/TraceId=ResourceID/Labels/ParentProjectId/
// SourceVersion) — parity с NewRegisterApplier (register-ветка). Первая ошибка
// прекращает набор и возвращается (idempotent: durable outbox-intent + register-drainer
// повторят at-least-once).
//
// ЭКОНОМИЯ. Каждая регистрация доезжает до iam ДВАЖДЫ: этим путём и повтором из
// очереди. Версия — то, чем iam их различает: этот путь идёт ПОСЛЕ commit'а writer-tx,
// а очередь несёт версию, застампленную БД ВНУТРИ него (clock_timestamp(), миграция
// 0011), поэтому здешняя строго не старше. Зеркало применяется last-source-state-wins,
// значит повтор из очереди не меняет ни строки — iam опознаёт редоставку и не
// материализует её второй раз. Пока registry версию не слал вовсе, доказательства
// редоставки у gate'а не было и он — намеренно — открывался в сторону работы: registry
// платил за обе доставки. Схлопнуть «выдать → отозвать → выдать» это не может: каждая
// регистрация несёт строго более новую версию, а unregister сносит строку зеркала
// целиком (и НИКОГДА не гейтится).
//
// ВОДЯНОЙ ЗНАК СТАВИТСЯ РОВНО ОДИН РАЗ НА ОБЪЕКТ — на ПОСЛЕДНЕМ его tuple'е; все
// предыдущие идут БЕЗ версии (gate их не глотает — при отсутствии версии он открывается
// в сторону работы). Причина в том, что gate ключуется на строке зеркала, а она — ПО
// ОБЪЕКТУ, не по tuple'у, и Create реестра шлёт на ОДИН объект два tuple'а
// (project-hierarchy, затем creator-owner):
//
//   - если версию несёт КАЖДЫЙ tuple, второй вызов зеркало не меняет и неотличим от
//     редоставки — gate проглатывает его вместе с постановкой owner-tuple (проверено на
//     стенде: строка `owner` в fga_outbox исчезала);
//   - если версию несёт каждый, но со сдвигом, второй tuple проходит — но набор,
//     оборванный ПОСЛЕ первого (iam моргнул между вызовами; этот путь fire-once и НЕ
//     ретраит), оставляет зеркало поднятым, и последующая редоставка из очереди
//     гейтится ЦЕЛИКОМ: owner-tuple теряется навсегда и молча.
//
// Поднимая знак только по завершении набора объекта, мы получаем и экономию (полная
// доставка → повтор загейчен целиком), и целость at-least-once (оборванная доставка →
// знак не поднят → повтор из очереди доводит дело). Путь очереди устроен иначе — там
// версия шагает по tuple'ам (stepSourceVersion в NewRegisterApplier): его ретраит
// drainer построчно, поэтому обрыв набора там самовосстанавливается.
func (s *SyncRegistrar) Register(ctx context.Context, intents []domain.RegisterIntent) error {
	if s.cli == nil {
		return errSyncRegisterClientNotConfigured
	}
	watermark := timestamppb.New(time.Now())
	lastOfObject := lastTupleIndexPerObject(intents)
	seq := 0
	for _, intent := range intents {
		for _, t := range intent.Tuples {
			// Версию несёт только вызов, закрывающий набор своего объекта.
			var sv *timestamppb.Timestamp
			if lastOfObject[t.Object] == seq {
				sv = watermark
			}
			seq++
			cctx := ctx
			var cancel context.CancelFunc
			if s.timeout > 0 {
				cctx, cancel = context.WithTimeout(ctx, s.timeout)
			}
			_, err := s.cli.RegisterResource(cctx, &iamv1.RegisterResourceRequest{
				SubjectId:       t.SubjectID,
				Relation:        t.Relation,
				Object:          t.Object,
				TraceId:         intent.ResourceID,
				Labels:          intent.Labels,
				ParentProjectId: intent.ParentProjectID,
				SourceVersion:   sv,
			})
			if cancel != nil {
				cancel()
			}
			if err != nil {
				return fmt.Errorf("sync register owner-tuple %s: %w", t.Object, err)
			}
		}
	}
	return nil
}

// lastTupleIndexPerObject — для каждого FGA-объекта доставки индекс его ПОСЛЕДНЕГО
// tuple'а в общей (сквозной по intent'ам) нумерации. Именно этот вызов поднимает
// водяной знак объекта; см. Register. Ключ — объект, а не intent: одна доставка может
// нести несколько объектов (repo-push + public-grant), и знак у каждого свой.
func lastTupleIndexPerObject(intents []domain.RegisterIntent) map[string]int {
	last := make(map[string]int)
	seq := 0
	for _, intent := range intents {
		for _, t := range intent.Tuples {
			last[t.Object] = seq
			seq++
		}
	}
	return last
}

// Compile-time check: SyncRegistrar удовлетворяет use-case-порту.
var _ registry.SyncRegistrar = (*SyncRegistrar)(nil)
