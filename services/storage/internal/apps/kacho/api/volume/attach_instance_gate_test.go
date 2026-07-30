// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package volume_test

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/volume"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/storage/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/repomock"
)

// Attach/Detach называют ДВА объекта с РАЗНЫМИ владельцами — том и инстанс, — а
// спрошено было ровно про один: том. Инстанс приходил из самоописывающегося
// payload'а и не сверялся ни с проектом, ни с правами; правдивость payload'а
// держалась только на том, что его выводит compute из своей строки, уже проверив
// право на инстанс. Кто попадает на внутренний листенер минуя compute, называл
// СВОЙ том и ЧУЖОЙ инстанс: строка привязки появлялась в наборе чужой машины, а
// загрузочный слот той машины оказывался занят.
//
// Поэтому вопрос теперь задаётся про ОБА объекта: том — per-RPC Check'ом
// интерсептора (запись каталога, `editor` на `storage_volume`), инстанс — здесь,
// на уровне данных, тем же отношением, которым compute гейтит свои
// AttachDisk/DetachDisk (`v_update` на `compute_instance`). Снос машины идёт под
// личностью инициатора и держит `v_delete`, поэтому отцепление принимает
// `v_update` ЛИБО `v_delete` — иначе удаление машины с томом стало бы невозможным
// для роли, у которой есть только право удаления.

// fakeInstanceGate — фейк порта вопроса про один названный объект. Разрешает
// только перечисленные (объект, отношение) пары и запоминает форму вопроса, чтобы
// тест утверждал ПРЕДМЕТ спроса, а не только его исход.
type fakeInstanceGate struct {
	// allow[objectID][relation] = true
	allow map[string]map[string]bool
	err   error

	calls     int
	subject   string
	resType   string
	action    string
	relations []string
	objectID  string
}

func (g *fakeInstanceGate) AllowedOnObject(
	_ context.Context, subject, resourceType, action string, relations []string, id string,
) (bool, error) {
	g.calls++
	g.subject, g.resType, g.action, g.objectID = subject, resourceType, action, id
	g.relations = append([]string(nil), relations...)
	if g.err != nil {
		return false, g.err
	}
	for _, rel := range relations {
		if g.allow[id][rel] {
			return true, nil
		}
	}
	return false, nil
}

func gateAllowing(objectID string, relations ...string) *fakeInstanceGate {
	rel := make(map[string]bool, len(relations))
	for _, r := range relations {
		rel[r] = true
	}
	return &fakeInstanceGate{allow: map[string]map[string]bool{objectID: rel}}
}

const (
	gateVolumeID   = "vol00000000000000001"
	gateOwnInst    = "ins-mine"
	gateForeignIns = "ins-theirs"
)

// countingWriter — Writer, считающий attach/detach. Тест обязан утверждать, что
// строка НЕ БЫЛА записана, а не только что вызывающий увидел отказ: отказ после
// мутации оставляет привязку в чужой машине.
type countingWriter struct {
	repomock.VolumeWriter
	attaches int
	detaches int
}

func newCountingWriter() *countingWriter {
	w := &countingWriter{}
	w.AttachFunc = func(context.Context, *domain.VolumeAttachment) error { w.attaches++; return nil }
	w.DetachFunc = func(context.Context, string, string) error { w.detaches++; return nil }
	return w
}

func attachedVolume() *domain.Volume {
	return &domain.Volume{ID: gateVolumeID, ProjectID: "prj-mine", ZoneID: "region-1-a"}
}

// newAttachUC собирает use-case с фейковым writer'ом, reader'ом и гейтом инстанса.
func newAttachUC(w volume.Writer, g authzfilter.ObjectGate) *volume.UseCase {
	reader := &repomock.VolumeReader{
		GetFunc: func(context.Context, string) (*domain.Volume, error) { return attachedVolume(), nil },
	}
	return volume.New(reader, w, &repomock.PeerClient{}, &repomock.PeerClient{},
		nil, serviceerr.ToStatus).WithInstanceGate(g)
}

func attachSpec(instanceID string) *domain.VolumeAttachment {
	return &domain.VolumeAttachment{
		VolumeID:     gateVolumeID,
		InstanceID:   instanceID,
		InstanceName: "whatever-the-caller-says",
		ProjectID:    "prj-mine",
		ZoneID:       "region-1-a",
		DeviceName:   "sdb",
	}
}

// TestAttach_ForeignInstanceIsRefusedBeforeTheRowIsWritten — несущая регрессия.
//
// Вызывающий держит законный `editor` на СВОЁМ томе (per-RPC Check интерсептора
// его пропускает) и называет ЧУЖОЙ instance_id. Отказ обязан прийти ДО записи:
// строка, попавшая в набор привязок чужой машины, занимает её загрузочный слот и
// делает саму машину неудаляемой.
func TestAttach_ForeignInstanceIsRefusedBeforeTheRowIsWritten(t *testing.T) {
	w := newCountingWriter()
	g := gateAllowing(gateOwnInst, authzfilter.RelationInstanceUpdate)
	uc := newAttachUC(w, g)

	_, err := uc.Attach(aliceCtx(), attachSpec(gateForeignIns))
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("Attach onto a foreign instance: err = %v, want PermissionDenied", err)
	}
	if w.attaches != 0 {
		t.Fatalf("the attachment row was written (%d writer calls) for an instance the caller may not change — "+
			"a refusal after the mutation still squats the victim's boot slot", w.attaches)
	}
}

// TestAttach_OwnInstanceProceeds — обратная сторона: право на СВОЙ инстанс есть,
// привязка пишется. Без этой половины предыдущий тест зеленел бы и на «отказывать
// всем».
func TestAttach_OwnInstanceProceeds(t *testing.T) {
	w := newCountingWriter()
	uc := newAttachUC(w, gateAllowing(gateOwnInst, authzfilter.RelationInstanceUpdate))

	v, err := uc.Attach(aliceCtx(), attachSpec(gateOwnInst))
	if err != nil {
		t.Fatalf("Attach onto the caller's own instance: %v", err)
	}
	if v == nil || v.ID != gateVolumeID {
		t.Fatalf("Attach returned %v, want the updated volume", v)
	}
	if w.attaches != 1 {
		t.Fatalf("writer attach calls = %d, want 1", w.attaches)
	}
}

// TestAttach_AsksAboutTheInstanceTheCallerNamed — форма вопроса, а не только исход:
// спрашивается ИНСТАНС из запроса, тип объекта чужого домена, отношение — то же,
// которым compute гейтит AttachDisk, субъект — вызывающий.
func TestAttach_AsksAboutTheInstanceTheCallerNamed(t *testing.T) {
	g := gateAllowing(gateOwnInst, authzfilter.RelationInstanceUpdate)
	uc := newAttachUC(newCountingWriter(), g)

	if _, err := uc.Attach(aliceCtx(), attachSpec(gateOwnInst)); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if g.calls != 1 {
		t.Fatalf("gate calls = %d, want exactly 1 question per Attach", g.calls)
	}
	if g.subject != "user:usr_alice" {
		t.Fatalf("gate subject = %q, want %q", g.subject, "user:usr_alice")
	}
	if g.resType != authzfilter.ResourceTypeComputeInstance {
		t.Fatalf("gate object type = %q, want %q", g.resType, authzfilter.ResourceTypeComputeInstance)
	}
	if g.objectID != gateOwnInst {
		t.Fatalf("gate object id = %q, want the instance the caller named (%q)", g.objectID, gateOwnInst)
	}
	if g.action != authzfilter.ActionVolumeAttach {
		t.Fatalf("gate action = %q, want %q (the same permission string the catalog carries)",
			g.action, authzfilter.ActionVolumeAttach)
	}
	if len(g.relations) != 1 || g.relations[0] != authzfilter.RelationInstanceUpdate {
		t.Fatalf("gate relations = %v, want exactly [%s] — attaching mutates the instance",
			g.relations, authzfilter.RelationInstanceUpdate)
	}
}

// TestAttach_DeleteVerbAloneDoesNotAllowAttaching — асимметрия закреплена: право
// снести машину не есть право что-то к ней подвесить.
func TestAttach_DeleteVerbAloneDoesNotAllowAttaching(t *testing.T) {
	w := newCountingWriter()
	uc := newAttachUC(w, gateAllowing(gateOwnInst, authzfilter.RelationInstanceDelete))

	_, err := uc.Attach(aliceCtx(), attachSpec(gateOwnInst))
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("Attach with only the delete verb: err = %v, want PermissionDenied", err)
	}
	if w.attaches != 0 {
		t.Fatalf("row written (%d calls) with only the delete verb on the instance", w.attaches)
	}
}

// TestAttach_EmptySubjectIsRefusedUnconditionally — не извлечённая identity значит
// «не знаю, кто ты», а не «доверенный»: ни вопроса модели, ни записи.
func TestAttach_EmptySubjectIsRefusedUnconditionally(t *testing.T) {
	w := newCountingWriter()
	g := gateAllowing(gateOwnInst, authzfilter.RelationInstanceUpdate)
	uc := newAttachUC(w, g)

	_, err := uc.Attach(context.Background(), attachSpec(gateOwnInst))
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("Attach without a caller identity: err = %v, want PermissionDenied", err)
	}
	if w.attaches != 0 || g.calls != 0 {
		t.Fatalf("no identity: writer calls=%d gate calls=%d, want 0/0", w.attaches, g.calls)
	}
}

// TestAttach_SystemPrincipalIsRefused — у storage нет «доверенного system-субъекта»
// на этом пути: system не резолвится в FGA-субъекта и попадает в ту же ветку.
func TestAttach_SystemPrincipalIsRefused(t *testing.T) {
	w := newCountingWriter()
	uc := newAttachUC(w, gateAllowing(gateOwnInst, authzfilter.RelationInstanceUpdate))

	ctx := operations.WithPrincipal(context.Background(),
		operations.Principal{Type: "system", ID: "bootstrap"})
	if _, err := uc.Attach(ctx, attachSpec(gateOwnInst)); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("Attach under a system principal: err = %v, want PermissionDenied", err)
	}
	if w.attaches != 0 {
		t.Fatalf("row written (%d calls) under a system principal", w.attaches)
	}
}

// TestAttach_MissingGateIsRefused — «спросить негде» не есть «да». Гейт не
// подключён (посадка без фильтра; production boot-guard её запрещает) → отказ, а
// не тихий проход к записи в чужой набор привязок.
func TestAttach_MissingGateIsRefused(t *testing.T) {
	w := newCountingWriter()
	uc := newAttachUC(w, nil)

	_, err := uc.Attach(aliceCtx(), attachSpec(gateOwnInst))
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("Attach with no instance gate configured: err = %v, want PermissionDenied", err)
	}
	if w.attaches != 0 {
		t.Fatalf("row written (%d calls) with the instance gate absent", w.attaches)
	}
}

// TestAttach_GateErrorFailsClosed — недоступный ответ модели не есть ответ «да»:
// отказ доезжает как есть, строка не пишется.
func TestAttach_GateErrorFailsClosed(t *testing.T) {
	w := newCountingWriter()
	g := &fakeInstanceGate{err: status.Error(codes.Unavailable, "instance gate: iam unreachable")}
	uc := newAttachUC(w, g)

	_, err := uc.Attach(aliceCtx(), attachSpec(gateOwnInst))
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("Attach on a gate error: code = %v, want Unavailable", status.Code(err))
	}
	if w.attaches != 0 {
		t.Fatalf("row written (%d calls) while the rights model was unreachable", w.attaches)
	}
}

// TestAttach_EmptyInstanceIdIsRejectedByName — обязательная по форме запроса ссылка
// несёт СВОЙ required-check: иначе пустая строка уехала бы в вопрос модели про
// объект `compute_instance:` и вернулась бы отказом прав вместо названного
// InvalidArgument.
func TestAttach_EmptyInstanceIdIsRejectedByName(t *testing.T) {
	w := newCountingWriter()
	g := gateAllowing(gateOwnInst, authzfilter.RelationInstanceUpdate)
	uc := newAttachUC(w, g)

	_, err := uc.Attach(aliceCtx(), attachSpec(""))
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Attach with an empty instance_id: code = %v, want InvalidArgument", status.Code(err))
	}
	if g.calls != 0 || w.attaches != 0 {
		t.Fatalf("empty instance_id: gate calls=%d writer calls=%d, want 0/0", g.calls, w.attaches)
	}
}

// TestDetach_ForeignInstanceIsRefusedBeforeTheRowIsRemoved — симметрия: держатель
// `editor` на томе не снимает его привязку с ЛЮБОЙ названной машины.
func TestDetach_ForeignInstanceIsRefusedBeforeTheRowIsRemoved(t *testing.T) {
	w := newCountingWriter()
	uc := newAttachUC(w, gateAllowing(gateOwnInst, authzfilter.RelationInstanceUpdate))

	_, err := uc.Detach(aliceCtx(), gateVolumeID, gateForeignIns)
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("Detach from a foreign instance: err = %v, want PermissionDenied", err)
	}
	if w.detaches != 0 {
		t.Fatalf("the attachment row was removed (%d writer calls) from an instance the caller may not change", w.detaches)
	}
}

// TestDetach_UpdateVerbProceeds — обычное отцепление (compute DetachDisk гейтится
// `v_update`).
func TestDetach_UpdateVerbProceeds(t *testing.T) {
	w := newCountingWriter()
	uc := newAttachUC(w, gateAllowing(gateOwnInst, authzfilter.RelationInstanceUpdate))

	if _, err := uc.Detach(aliceCtx(), gateVolumeID, gateOwnInst); err != nil {
		t.Fatalf("Detach with the update verb: %v", err)
	}
	if w.detaches != 1 {
		t.Fatalf("writer detach calls = %d, want 1", w.detaches)
	}
}

// TestDetach_DeleteVerbProceeds — снос машины идёт под личностью инициатора, а его
// право на удаление это `v_delete`. Требуй здесь только `v_update`, и машина с
// томом стала бы НЕУДАЛЯЕМОЙ для роли, у которой есть право удаления и нет права
// изменения: шаг отцепления отказал бы, а строка машины удаляется последней.
func TestDetach_DeleteVerbProceeds(t *testing.T) {
	w := newCountingWriter()
	uc := newAttachUC(w, gateAllowing(gateOwnInst, authzfilter.RelationInstanceDelete))

	if _, err := uc.Detach(aliceCtx(), gateVolumeID, gateOwnInst); err != nil {
		t.Fatalf("Detach under the delete verb (teardown path): %v", err)
	}
	if w.detaches != 1 {
		t.Fatalf("writer detach calls = %d, want 1 — teardown must be able to release the volume", w.detaches)
	}
}

// TestDetach_AsksBothVerbsAboutTheInstance — форма вопроса отцепления: тот же
// объект, оба принимаемых отношения.
func TestDetach_AsksBothVerbsAboutTheInstance(t *testing.T) {
	g := gateAllowing(gateOwnInst, authzfilter.RelationInstanceUpdate)
	uc := newAttachUC(newCountingWriter(), g)

	if _, err := uc.Detach(aliceCtx(), gateVolumeID, gateOwnInst); err != nil {
		t.Fatalf("Detach: %v", err)
	}
	if g.resType != authzfilter.ResourceTypeComputeInstance || g.objectID != gateOwnInst {
		t.Fatalf("gate asked about (%q,%q), want (%q,%q)",
			g.resType, g.objectID, authzfilter.ResourceTypeComputeInstance, gateOwnInst)
	}
	if g.action != authzfilter.ActionVolumeDetach {
		t.Fatalf("gate action = %q, want %q", g.action, authzfilter.ActionVolumeDetach)
	}
	want := map[string]bool{
		authzfilter.RelationInstanceUpdate: true,
		authzfilter.RelationInstanceDelete: true,
	}
	if len(g.relations) != len(want) {
		t.Fatalf("gate relations = %v, want both %v", g.relations, want)
	}
	for _, r := range g.relations {
		if !want[r] {
			t.Fatalf("gate relations = %v, contains unexpected %q", g.relations, r)
		}
	}
}

// TestDetach_MissingGateIsRefused / EmptySubject / GateError — те же три ветки,
// что у Attach: «спросить негде», «не знаю, кто ты», «модель не ответила».
func TestDetach_MissingGateIsRefused(t *testing.T) {
	w := newCountingWriter()
	uc := newAttachUC(w, nil)
	if _, err := uc.Detach(aliceCtx(), gateVolumeID, gateOwnInst); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("Detach with no instance gate: err = %v, want PermissionDenied", err)
	}
	if w.detaches != 0 {
		t.Fatalf("row removed (%d calls) with the instance gate absent", w.detaches)
	}
}

func TestDetach_EmptySubjectIsRefusedUnconditionally(t *testing.T) {
	w := newCountingWriter()
	g := gateAllowing(gateOwnInst, authzfilter.RelationInstanceUpdate)
	uc := newAttachUC(w, g)
	if _, err := uc.Detach(context.Background(), gateVolumeID, gateOwnInst); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("Detach without a caller identity: err = %v, want PermissionDenied", err)
	}
	if w.detaches != 0 || g.calls != 0 {
		t.Fatalf("no identity: writer calls=%d gate calls=%d, want 0/0", w.detaches, g.calls)
	}
}

func TestDetach_GateErrorFailsClosed(t *testing.T) {
	w := newCountingWriter()
	g := &fakeInstanceGate{err: status.Error(codes.Unavailable, "instance gate: iam unreachable")}
	uc := newAttachUC(w, g)
	if _, err := uc.Detach(aliceCtx(), gateVolumeID, gateOwnInst); status.Code(err) != codes.Unavailable {
		t.Fatalf("Detach on a gate error: code = %v, want Unavailable", status.Code(err))
	}
	if w.detaches != 0 {
		t.Fatalf("row removed (%d calls) while the rights model was unreachable", w.detaches)
	}
}

func TestDetach_EmptyInstanceIdIsRejectedByName(t *testing.T) {
	w := newCountingWriter()
	g := gateAllowing(gateOwnInst, authzfilter.RelationInstanceUpdate)
	uc := newAttachUC(w, g)
	if _, err := uc.Detach(aliceCtx(), gateVolumeID, ""); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Detach with an empty instance_id: err = %v, want InvalidArgument", err)
	}
	if g.calls != 0 || w.detaches != 0 {
		t.Fatalf("empty instance_id: gate calls=%d writer calls=%d, want 0/0", g.calls, w.detaches)
	}
}
