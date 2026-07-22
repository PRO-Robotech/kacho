// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package targetgroup

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// UpdateTargetGroupUseCase — UpdateMask discipline + async update
// .
//
// Mutable: name / description / labels / health_check / deregistration_delay_seconds /
// slow_start_seconds. Immutable: project_id / region_id (mask → InvalidArgument).
// Targets — отдельная семантика через AddTargets/RemoveTargets; mask=["targets"]
// → InvalidArgument с фиксированным текстом.
type UpdateTargetGroupUseCase struct {
	repo    Repo
	opsRepo OpsRepo
	logger  *slog.Logger
}

// NewUpdateTargetGroupUseCase конструктор.
func NewUpdateTargetGroupUseCase(repo Repo, opsRepo OpsRepo, logger *slog.Logger) *UpdateTargetGroupUseCase {
	if logger == nil {
		logger = slog.Default()
	}
	return &UpdateTargetGroupUseCase{repo: repo, opsRepo: opsRepo, logger: logger}
}

// knownUpdateFieldsTG — whitelist update_mask fields (NLB-1c: durations
// renamed to Duration form; `port` LIVE-mutable; `health_check` supports both a
// bare full-replace and dotted `health_check.<sub>` merge paths).
var knownUpdateFieldsTG = map[string]bool{
	"name":                 true,
	"description":          true,
	"labels":               true,
	"health_check":         true,
	"deregistration_delay": true,
	"slow_start":           true,
	"port":                 true,
}

// knownHCSubFields — allowed dotted `health_check.<sub>` mask paths (scalar
// dotted-mask PATCH + probe-oneof atomic-replace discriminators).
var knownHCSubFields = map[string]bool{
	"interval":            true,
	"timeout":             true,
	"healthy_threshold":   true,
	"unhealthy_threshold": true,
	"tcp":                 true,
	"http":                true,
	"https":               true,
	"grpc":                true,
}

// hcProbeSubFields — probe-oneof discriminators (atomic-replace, sibling-scalar
// preservation).
var hcProbeSubFields = map[string]bool{
	"tcp": true, "http": true, "https": true, "grpc": true,
}

// immutableUpdateFieldsTG — hard-immutable, с фиксированным текстом error text.
var immutableUpdateFieldsTG = map[string]string{
	"project_id": "project_id is immutable after TargetGroup.Create",
	"region_id":  "region_id is immutable after TargetGroup.Create",
}

// Execute — sync mask validation + read existing → apply diff → ops insert + worker.
func (u *UpdateTargetGroupUseCase) Execute(
	ctx context.Context, req *lbv1.UpdateTargetGroupRequest,
) (*operations.Operation, error) {
	id := req.GetTargetGroupId()
	if id == "" {
		return nil, errInvalidArg("target_group_id", "required")
	}
	if err := validateTargetGroupID(id); err != nil {
		return nil, err
	}
	mask := req.GetUpdateMask().GetPaths()
	for _, p := range mask {
		// targets via mask запрещён — отдельный фиксированный текст.
		if p == "targets" {
			return nil, status.Error(codes.InvalidArgument,
				"targets must be modified via AddTargets / RemoveTargets")
		}
		// immutable-check ДО known-set (api-conventions.md: known-set не несёт
		// immutable-полей, иначе они отвергнутся как generic unknown).
		if msg, ok := immutableUpdateFieldsTG[p]; ok {
			return nil, status.Errorf(codes.InvalidArgument, "%s", msg)
		}
		// dotted health_check.<sub> — scalar dotted-mask PATCH / probe replace.
		if sub, ok := strings.CutPrefix(p, "health_check."); ok {
			if !knownHCSubFields[sub] {
				return nil, status.Errorf(codes.InvalidArgument, "unknown update_mask field: %s", p)
			}
			continue
		}
		if !knownUpdateFieldsTG[p] {
			return nil, status.Errorf(codes.InvalidArgument, "unknown update_mask field: %s", p)
		}
	}

	// Read current state.
	rd, err := u.repo.Reader(ctx)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	cur, err := rd.TargetGroups().Get(ctx, id)
	_ = rd.Close()
	if err != nil {
		return nil, mapDomainErr(err)
	}

	updated, err := applyUpdateMaskTG(cur.TargetGroup, req, mask)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	if err := updated.Validate(); err != nil {
		return nil, mapDomainErr(err)
	}

	// Operation row.
	op, err := operations.NewFromContext(ctx,
		ids.PrefixOperationNLB,
		fmt.Sprintf("Update TargetGroup %s", id),
		&lbv1.UpdateTargetGroupMetadata{TargetGroupId: id},
	)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	principal := operations.PrincipalFromContext(ctx)
	if err := u.opsRepo.CreateWithPrincipal(ctx, op, principal); err != nil {
		return nil, mapDomainErr(err)
	}

	// (parity with compute): re-emit the FGA-register intent
	// (carrying the new labels) ONLY when labels change — labels in mask, or empty
	// mask (full PATCH always reapplies labels). A non-labels Update is a mirror
	// no-op (skip the intent to avoid a useless RegisterResource round-trip).
	emitMirror := labelsInMaskTG(mask)
	expectedXmin := cur.Xmin
	operations.Run(ctx, u.opsRepo, op.ID, func(workerCtx context.Context) (*anypb.Any, error) {
		return u.doUpdate(workerCtx, updated, expectedXmin, emitMirror)
	})
	return &op, nil
}

// labelsInMaskTG reports whether the Update touches labels: explicit "labels" in
// the mask, or an empty mask (full-object PATCH reapplies all mutable fields).
func labelsInMaskTG(mask []string) bool {
	if len(mask) == 0 {
		return true
	}
	for _, p := range mask {
		if p == "labels" {
			return true
		}
	}
	return false
}

// doUpdate — worker: Writer-TX → Update + outbox UPDATED (+ FGA-register intent
// when labels changed) → Commit. The mirror-feed intent is written in the
// SAME writer-tx as the resource UPDATE (no dual-write); the emitter stamps a
// monotonic source_version so IAM applies the mirror last-source-state-wins.
func (u *UpdateTargetGroupUseCase) doUpdate(ctx context.Context, tg domain.TargetGroup, expectedXmin string, emitMirror bool) (*anypb.Any, error) {
	w, err := u.repo.Writer(ctx)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	defer w.Abort()

	updated, err := w.TargetGroups().Update(ctx, &tg, expectedXmin)
	if err != nil {
		return nil, mapDomainErr(err)
	}
	if err := w.Outbox().Emit(ctx,
		kachorepo.OutboxResourceTargetGroup, string(updated.ID), string(updated.ProjectID),
		kachorepo.OutboxActionUpdated, tgOutboxPayload(updated),
	); err != nil {
		return nil, mapDomainErr(err)
	}
	if emitMirror {
		if err := w.FGARegisterOutbox().Emit(ctx, domain.FGAEventRegister,
			tgMirrorIntent(updated)); err != nil {
			return nil, mapDomainErr(err)
		}
	}
	if err := w.Commit(); err != nil {
		return nil, mapDomainErr(err)
	}
	return marshalTargetGroup(updated)
}

// applyUpdateMaskTG — наложить mask на текущий TG. Empty mask → full PATCH:
// mutable полностью перезаписываются из req; immutable silent-ignored
// (по конвенции Kachō; explicit immutable field в mask уже отлавливается выше).
//
// health_check — oneof-replace дисциплина (NLB-1c): bare `health_check` (или
// empty mask) → full replace; dotted `health_check.<sub>` → merge (scalar-set
// либо probe atomic-replace с сохранением sibling-скаляров).
func applyUpdateMaskTG(
	cur domain.TargetGroup, req *lbv1.UpdateTargetGroupRequest, mask []string,
) (domain.TargetGroup, error) {
	full := len(mask) == 0
	apply := func(field string) bool {
		if full {
			return true
		}
		for _, p := range mask {
			if p == field {
				return true
			}
		}
		return false
	}
	out := cur
	if apply("name") {
		out.Name = domain.LbName(req.GetName())
	}
	if apply("description") {
		out.Description = domain.LbDescription(req.GetDescription())
	}
	if apply("labels") {
		out.Labels = domain.LabelsFromMap(req.GetLabels())
	}
	if apply("deregistration_delay") {
		out.DeregistrationDelay = durationFromPb(req.GetDeregistrationDelay())
	}
	if apply("slow_start") {
		out.SlowStart = durationFromPb(req.GetSlowStart())
	}
	if apply("port") {
		out.Port = domain.LbPort(req.GetPort())
	}
	hc, err := mergeHealthCheckTG(cur.HealthCheck, req.GetHealthCheck(), mask, full)
	if err != nil {
		return domain.TargetGroup{}, err
	}
	out.HealthCheck = hc
	return out, nil
}

// durationFromPb — proto Duration → domain.LbDuration (nil → 0s).
func durationFromPb(d *durationpb.Duration) domain.LbDuration {
	if d == nil {
		return 0
	}
	return domain.LbDuration(d.AsDuration())
}

// mergeHealthCheckTG — oneof-replace merge для health_check в Update.
//
//   - bare "health_check" (или empty mask) → full replace из req (probe
//     discriminator обязателен — ловится domain.Validate);
//   - dotted "health_check.<sub>" → merge поверх cur: scalar-set (interval/
//     timeout/*_threshold) либо probe atomic-replace (tcp/http/https/grpc) с
//     сохранением sibling-скаляров;
//   - health_check не тронут маской → cur без изменений.
func mergeHealthCheckTG(
	cur domain.HealthCheck, reqPb *lbv1.HealthCheck, mask []string, full bool,
) (domain.HealthCheck, error) {
	bareHC := full
	var dotted []string
	for _, p := range mask {
		switch {
		case p == "health_check":
			bareHC = true
		case strings.HasPrefix(p, "health_check."):
			dotted = append(dotted, strings.TrimPrefix(p, "health_check."))
		}
	}
	if !bareHC && len(dotted) == 0 {
		return cur, nil // health_check не в маске — не трогаем.
	}
	reqHC, err := healthCheckFromPb(reqPb)
	if err != nil {
		return domain.HealthCheck{}, err
	}
	if bareHC {
		// Full replace: probe-discriminator обязателен — domain.Validate ловит
		// "exactly one of" при пустом probe (NLB-1-38, generic health_check).
		return reqHC, nil
	}
	// Dotted merge поверх cur — sibling-скаляры и probe сохраняются, пока их
	// сабпуть не в маске.
	out := cur
	for _, sub := range dotted {
		if hcProbeSubFields[sub] {
			if err := replaceProbeTG(&out, reqHC, sub); err != nil {
				return domain.HealthCheck{}, err
			}
			continue
		}
		switch sub {
		case "interval":
			out.Interval = reqHC.Interval
		case "timeout":
			out.Timeout = reqHC.Timeout
		case "healthy_threshold":
			out.HealthyThreshold = reqHC.HealthyThreshold
		case "unhealthy_threshold":
			out.UnhealthyThreshold = reqHC.UnhealthyThreshold
		}
	}
	return out, nil
}

// replaceProbeTG — atomic-replace probe-oneof на `sub`, сохраняя sibling-скаляры
// (interval/timeout/thresholds уцелевают — NLB-1-37). Discriminator (тело
// выбранной пробы) ОБЯЗАН присутствовать в req — иначе INVALID_ARGUMENT (не
// silent-clear, NLB-1-38).
func replaceProbeTG(out *domain.HealthCheck, reqHC domain.HealthCheck, sub string) error {
	out.TCP, out.HTTP, out.HTTPS, out.GRPC = nil, nil, nil, nil
	switch sub {
	case "tcp":
		if reqHC.TCP == nil {
			return errMissingProbeBody("tcp")
		}
		out.TCP = reqHC.TCP
	case "http":
		if reqHC.HTTP == nil {
			return errMissingProbeBody("http")
		}
		out.HTTP = reqHC.HTTP
	case "https":
		if reqHC.HTTPS == nil {
			return errMissingProbeBody("https")
		}
		out.HTTPS = reqHC.HTTPS
	case "grpc":
		if reqHC.GRPC == nil {
			return errMissingProbeBody("grpc")
		}
		out.GRPC = reqHC.GRPC
	}
	return nil
}

// errMissingProbeBody — INVALID_ARGUMENT когда маска указывает probe-путь, но
// его тело/дискриминатор отсутствует в теле req (NLB-1-38, не silent-clear).
func errMissingProbeBody(sub string) error {
	return status.Errorf(codes.InvalidArgument,
		"health_check.%s requires the %s probe body when the update_mask replaces the probe", sub, sub)
}
