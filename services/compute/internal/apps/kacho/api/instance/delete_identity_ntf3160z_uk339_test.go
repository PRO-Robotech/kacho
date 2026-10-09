// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// delete_identity_ntf3160z_uk339_test.go — полоса RED S2-B3 (issue-2918, NTF-3):
// какой личностью compute ходит к владельцам привязок при снятии машины.
//
// Предмет — личность, которую видят ВЛАДЕЛЬЦЫ (vpc, storage): именно её
// клиенты compute пересылают (`auth.PropagateOutgoing` берёт принципал
// контекста), и именно она становится инициатором строк журнала владельцев.
//
//   - NTF3-160 (з): публичный `Delete`, начатый `usr-A`, снимает привязки
//     интерфейса и тома личностью `usr-A` на КАЖДОМ обращении к владельцу —
//     перечисление и снятие; личность `releaseAndDelete` — личность исполнителя
//     операции, отдельной установки нет. Близнец — начавший `usr-B`.
//   - УК3-39 (CX3H-02 (а), (д)): проход добивателя (`FinishStuckDeletes`) личность
//     НЕ устанавливает — контекст прохода тот, что дал корень (без принципала).
//     Владелец, которому нечего сказать о «никто», отказывает; проход
//     прерывается: строка машины на месте, привязки у владельцев не сняты,
//     удаления строки не было. Отличие от инъекции `AsComponent` в цикле
//     (суженный пустой перечень → строка удалена, привязки остались) проба
//     держит тремя утверждениями исхода; инъекция прогоняется отдельно и в
//     дерево не входит — она утверждала бы опасный исход как свойство.
//
// Дублёры владельцев выполняют контракт настоящих в той части, которая здесь
// предмет: отказ на контексте без личности (листенер берёт личность пира, а
// владелец ей отказывает аутентификацией; клиент compute сводит его в
// codes.Internal), сужение перечня до видимого личности, у которой нет
// привязок, и полный перечень — для начавшего с правом на машину.
package instance

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/corelib/operations"

	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/ports"
	"github.com/PRO-Robotech/kacho/services/compute/internal/ports/portmock"
)

// ownerView — общая часть дублёров владельцев: кто вправе видеть привязки машины
// и какие личности видел владелец на каждом обращении.
type ownerView struct {
	entitled map[string]bool // initiator-форма личностей с правом на машину
	seen     []string        // личности обращений по порядку; "" — без принципала
}

// admit — решение владельца по контексту обращения: отказ без личности,
// сужение (false) для личности без права, полный ответ (true) для начавшего.
func (o *ownerView) admit(ctx context.Context) (bool, error) {
	p, ok := operations.PrincipalFromContextOK(ctx)
	if !ok || (p.Type == "" && p.ID == "") {
		o.seen = append(o.seen, "")
		// Внешний вид отказа после клиента compute: Unauthenticated → codes.Internal.
		return false, status.Error(codes.Internal, "internal error")
	}
	who := p.Type + ":" + p.ID
	o.seen = append(o.seen, who)
	return o.entitled[who], nil
}

type identityNicPeer struct {
	ownerView
	attached map[string][]string
}

func (f *identityNicPeer) ListByInstance(ctx context.Context, ids []string) ([]ports.NicAttachment, error) {
	full, err := f.admit(ctx)
	if err != nil {
		return nil, err
	}
	if !full {
		return nil, nil // суженный набор: личность без права не видит привязок
	}
	var out []ports.NicAttachment
	for _, id := range ids {
		for _, nic := range f.attached[id] {
			out = append(out, ports.NicAttachment{NICID: nic, InstanceID: id})
		}
	}
	return out, nil
}

func (f *identityNicPeer) Attach(context.Context, ports.NicAttachSpec) (*ports.NicAttachment, error) {
	return nil, errors.New("identityNicPeer.Attach: снятие машины его не зовёт")
}

func (f *identityNicPeer) Detach(ctx context.Context, nicID, instanceID string) error {
	full, err := f.admit(ctx)
	if err != nil {
		return err
	}
	if !full {
		return status.Error(codes.PermissionDenied, "permission denied")
	}
	kept := f.attached[instanceID][:0]
	for _, cur := range f.attached[instanceID] {
		if cur != nicID {
			kept = append(kept, cur)
		}
	}
	f.attached[instanceID] = kept
	return nil
}

type identityVolumePeer struct {
	ownerView
	attached map[string][]string
}

func (f *identityVolumePeer) ListAttachments(ctx context.Context, ids []string) ([]ports.VolumeAttachmentInfo, error) {
	full, err := f.admit(ctx)
	if err != nil {
		return nil, err
	}
	if !full {
		return nil, nil
	}
	var out []ports.VolumeAttachmentInfo
	for _, id := range ids {
		for _, vol := range f.attached[id] {
			out = append(out, ports.VolumeAttachmentInfo{VolumeID: vol, InstanceID: id})
		}
	}
	return out, nil
}

func (f *identityVolumePeer) Attach(context.Context, ports.VolumeAttachSpec) (*ports.VolumeAttachmentInfo, error) {
	return nil, errors.New("identityVolumePeer.Attach: снятие машины его не зовёт")
}

func (f *identityVolumePeer) Detach(ctx context.Context, volumeID, instanceID string) error {
	full, err := f.admit(ctx)
	if err != nil {
		return err
	}
	if !full {
		return status.Error(codes.PermissionDenied, "permission denied")
	}
	kept := f.attached[instanceID][:0]
	for _, cur := range f.attached[instanceID] {
		if cur != volumeID {
			kept = append(kept, cur)
		}
	}
	f.attached[instanceID] = kept
	return nil
}

type identityKit struct {
	svc  *InstanceService
	repo *portmock.InstanceRepo
	ops  *portmock.OpsRepo
	nics *identityNicPeer
	vols *identityVolumePeer
	id   string
}

// newIdentityKit — машина с интерфейсом и томом; право на неё — у entitled.
func newIdentityKit(t *testing.T, entitled ...string) identityKit {
	t.Helper()
	repo := portmock.NewInstanceRepo()
	id := seedInstance(repo, domain.InstanceStatusStopped).ID
	allow := map[string]bool{}
	for _, e := range entitled {
		allow[e] = true
	}
	nics := &identityNicPeer{ownerView: ownerView{entitled: allow}, attached: map[string][]string{id: {"nic-3"}}}
	vols := &identityVolumePeer{ownerView: ownerView{entitled: allow}, attached: map[string][]string{id: {"vol-13"}}}
	ops := portmock.NewOpsRepo()
	svc := NewInstanceService(repo, nil, nil, nil, nil, nics, vols, ops)
	return identityKit{svc: svc, repo: repo, ops: ops, nics: nics, vols: vols, id: id}
}

func userCtx(id string) (context.Context, string) {
	return operations.WithPrincipal(context.Background(), operations.Principal{Type: "user", ID: id}), "user:" + id
}

func onlyWho(t *testing.T, seen []string, want, msg string) {
	t.Helper()
	require.NotEmpty(t, seen, "%s: к владельцу не обратились ни разу — утверждать нечего", msg)
	for i, who := range seen {
		require.Equal(t, want, who, "%s: обращение #%d к владельцу пришло под личностью %q", msg, i+1, who)
	}
}

// ntf3160zDelete — публичный Delete под личностью начавшего, исход операции.
func ntf3160zDelete(t *testing.T, starterID string, entitled ...string) (identityKit, string) {
	t.Helper()
	ctx, who := userCtx(starterID)
	k := newIdentityKit(t, append(entitled, who)...)
	op, err := k.svc.Delete(ctx, k.id)
	require.NoError(t, err, "NTF3-160 (з): Delete принят")
	done := portmock.AwaitOpDone(t, k.ops, op.ID)
	require.Nil(t, done.Error, "NTF3-160 (з): операция Delete завершилась без ошибки")
	return k, who
}

// TestInstanceDelete_NTF3160z_OwnersSeeTheStarter — NTF3-160 (з).
func TestInstanceDelete_NTF3160z_OwnersSeeTheStarter(t *testing.T) {
	k, who := ntf3160zDelete(t, "usr-A")
	onlyWho(t, k.nics.seen, who, "NTF3-160 (з) vpc")
	onlyWho(t, k.vols.seen, who, "NTF3-160 (з) storage")
	require.Empty(t, k.nics.attached[k.id], "NTF3-160 (з): интерфейс отвязан у владельца")
	require.Empty(t, k.vols.attached[k.id], "NTF3-160 (з): том отвязан у владельца")
	_, err := k.repo.Get(context.Background(), k.id)
	require.ErrorIs(t, err, ports.ErrNotFound, "NTF3-160 (з): строки машины нет")
}

// TestInstanceDelete_NTF3160zTwin_OtherStarter — близнец (з): начавший — usr-B.
func TestInstanceDelete_NTF3160zTwin_OtherStarter(t *testing.T) {
	k, who := ntf3160zDelete(t, "usr-B")
	onlyWho(t, k.nics.seen, who, "NTF3-160 (з) близнец vpc")
	onlyWho(t, k.vols.seen, who, "NTF3-160 (з) близнец storage")
	for _, s := range append(append([]string{}, k.nics.seen...), k.vols.seen...) {
		require.NotEqual(t, "user:usr-A", s, "NTF3-160 (з) близнец: обращений под usr-A — 0")
	}
}

// uk339Pass — машина в DELETING с привязками; один проход добивателя на ctx.
func uk339Pass(t *testing.T, ctx context.Context) (identityKit, error) {
	t.Helper()
	k := newIdentityKit(t) // у личности прохода привязок (права) нет ни у кого
	_, err := k.repo.MarkDeleting(context.Background(), k.id)
	require.NoError(t, err, "ФИКСТУРА: перевод в DELETING")
	_, ran, perr := k.svc.FinishStuckDeletes(ctx, 0)
	require.True(t, ran, "ФИКСТУРА: проход состоялся (замок взят)")
	return k, perr
}

// TestFinishStuckDeletes_UK339_PassIdentityUnchangedRowAndBindingsKept — УК3-39:
// проход с контекстом корня (без принципала) — исход базы: отказ владельца,
// проход прерван, строка в DELETING на месте, привязки не сняты.
func TestFinishStuckDeletes_UK339_PassIdentityUnchangedRowAndBindingsKept(t *testing.T) {
	k, err := uk339Pass(t, context.Background())
	require.Error(t, err, "УК3-39: владелец отказывает личности прохода — проход прерван")
	require.Equal(t, []string{""}, k.nics.seen,
		"УК3-39: к владельцу интерфейса проход обратился без установленной личности, ровно один раз (перечисление)")
	in, gerr := k.repo.Get(context.Background(), k.id)
	require.NoError(t, gerr, "УК3-39: строка машины на месте")
	require.Equal(t, domain.InstanceStatusDeleting, in.Status, "УК3-39: машина осталась в DELETING")
	require.Equal(t, []string{"nic-3"}, k.nics.attached[k.id], "УК3-39: привязка интерфейса не снята")
	require.Equal(t, []string{"vol-13"}, k.vols.attached[k.id], "УК3-39: привязка тома не снята")
}
