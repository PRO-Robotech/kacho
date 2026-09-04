// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package listener

import (
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/pkg/option"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// У ссылки на группу целей ОДИН вход — `target_group_id` (задача продукта #1596).
//
// ПРЕДМЕТ. Полей под одну величину было два, и приоритет между ними был
// МОЛЧАЛИВЫМ: `target_group_id` побеждал, а записанное в `default_target_group_id`
// отбрасывалось без единого признака. Дороже всего это стоило синхронизатору
// состояния: чтение отдаёт ОБА поля (одно значение в двух ключах), поэтому
// «прочитал → поправил defaultTargetGroupId → записал объект целиком» уходило с
// прежним `targetGroupId` в теле, тот побеждал, и клиент получал `200` и
// `done=true` на изменение, которого не произошло. Реконсайлер, сверяющий по
// `defaultTargetGroupId`, входил в бесконечный круг «вижу расхождение → пишу →
// ничего не изменилось».
//
// ИСХОД ВЫБРАН ИЗ ТРЁХ ЗАКОННЫХ (`api-conventions.md` §«Принято-и-проигнорировано»):
// не «описать приоритет» и не «снять с контракта», а ОТВЕРГАТЬ ЯВНО. Довод —
// идиома ЭТОГО ЖЕ сервиса: `type`/`placement_type` оставлены в запросе
// балансировщика ровно затем, чтобы клиент, который их шлёт, получил внятный
// отказ, а не молчаливое отбрасывание на крае (край работает с
// `DiscardUnknown` — это осознанное продуктовое решение). Различает два исхода
// вопрос «что увидит клиент, если поле пропадёт»: `target_port` и
// `proxy_protocol_v2` сняты с контракта, потому что не работали НИКОГДА;
// `default_target_group_id` работает и именно им справочник до сих пор учил
// привязывать группу — снять его молча значило бы создавать слушатель БЕЗ
// группы по правильному с виду запросу.
//
// ЧТО ОСТАЁТСЯ. `Listener.default_target_group_id` — по-прежнему поле ОТВЕТА,
// зеркало `target_group_id`: читателей у него не отбирают. Приоритета больше нет
// by construction — входное поле одно.

// RED→GREEN: Create с `default_target_group_id` отвергается явно и называет замену.
func TestCreateListener_RetiredDefaultTargetGroupId_RejectedExplicitly(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	ops := newFakeOpsRepo()
	lb := seedParentLB(t, repo)
	tgID := domain.ResourceID("tgr-legacyinput00001")
	seedListenerTG(repo, tgID, lb.ProjectID, lb.RegionID)
	uc := newCreateUC(repo, ops)

	_, err := uc.Run(contextWithSubject("user:test-actor"), &lbv1.CreateListenerRequest{
		LoadBalancerId:       string(lb.ID),
		Name:                 "tcp-443",
		Protocol:             lbv1.Listener_TCP,
		Port:                 443,
		DefaultTargetGroupId: string(tgID),
	})

	require.Equal(t, codes.InvalidArgument, status.Code(err),
		"молчаливое принятие снятого входа — это ban «принято-и-проигнорировано»")
	msg := status.Convert(err).Message()
	require.Contains(t, msg, "default_target_group_id", "отказ обязан назвать поле")
	require.Contains(t, msg, "target_group_id",
		"отказ обязан назвать, ЧЕМ теперь привязывают группу, иначе он не восстанавливает следующий шаг клиента")
}

// Отказ наступает ДО записи: слушатель не создаётся ни в каком виде.
func TestCreateListener_RetiredDefaultTargetGroupId_NothingWritten(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	ops := newFakeOpsRepo()
	lb := seedParentLB(t, repo)
	// Группа ПОСЕЯНА намеренно: иначе отказ пришёл бы от нерезолвящейся ссылки, и
	// проба зеленела бы, ничего не утверждая о снятии входа.
	tgID := domain.ResourceID("tgr-legacyinput00001")
	seedListenerTG(repo, tgID, lb.ProjectID, lb.RegionID)
	uc := newCreateUC(repo, ops)

	before := len(repo.listeners)
	_, err := uc.Run(contextWithSubject("user:test-actor"), &lbv1.CreateListenerRequest{
		LoadBalancerId:       string(lb.ID),
		Name:                 "tcp-443",
		Protocol:             lbv1.Listener_TCP,
		Port:                 443,
		DefaultTargetGroupId: string(tgID),
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Len(t, repo.listeners, before, "отказ обязан наступать до любой записи")
}

// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ. Без него отрицание выше зеленело бы и на Create,
// сломанном целиком: «отвергается» неотличимо от «не работает ничего».
func TestCreateListener_TargetGroupIdStillWires(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	ops := newFakeOpsRepo()
	lb := seedParentLB(t, repo)
	tgID := domain.ResourceID("tgr-authoritative001")
	seedListenerTG(repo, tgID, lb.ProjectID, lb.RegionID)
	uc := newCreateUC(repo, ops)

	_, err := uc.Run(contextWithSubject("user:test-actor"), &lbv1.CreateListenerRequest{
		LoadBalancerId: string(lb.ID),
		Name:           "tcp-443",
		Protocol:       lbv1.Listener_TCP,
		Port:           443,
		TargetGroupId:  string(tgID),
	})
	require.NoError(t, err)
}

// RED→GREEN: `default_target_group_id` в update_mask отвергается явно.
func TestUpdateListener_RetiredDefaultTargetGroupIdInMask_RejectedExplicitly(t *testing.T) {
	t.Parallel()
	suite := newUpdateSuite(t)
	tgID := domain.ResourceID(ids.NewID(ids.PrefixTargetGroup))
	suite.repo.seedTG(&kachorepo.TargetGroupRecord{
		TargetGroup: domain.TargetGroup{
			ID: tgID, ProjectID: suite.listener.ProjectID, RegionID: suite.listener.RegionID,
			Name: domain.LbName("legacy-mask-tg"), Status: domain.TargetGroupStatusActive,
		},
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	})

	_, err := suite.uc.Run(contextWithSubject("user:test-actor"), &lbv1.UpdateListenerRequest{
		ListenerId:           string(suite.listener.ID),
		UpdateMask:           &fieldmaskpb.FieldMask{Paths: []string{"default_target_group_id"}},
		DefaultTargetGroupId: string(tgID),
	})

	require.Equal(t, codes.InvalidArgument, status.Code(err),
		"снятый вход обязан отвергаться, а не применяться молча")
	msg := status.Convert(err).Message()
	require.Contains(t, msg, "default_target_group_id", "отказ обязан назвать поле")
	require.Contains(t, msg, "target_group_id",
		"отказ обязан назвать замену, иначе он не восстанавливает следующий шаг клиента")
}

// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ для Update: замена работает и действительно переставляет
// ссылку. Без него отрицание выше зеленело бы на Update, сломанном целиком.
func TestUpdateListener_TargetGroupIdStillRepoints(t *testing.T) {
	t.Parallel()
	suite := newUpdateSuite(t)
	tgID := domain.ResourceID(ids.NewID(ids.PrefixTargetGroup))
	suite.repo.seedTG(&kachorepo.TargetGroupRecord{
		TargetGroup: domain.TargetGroup{
			ID: tgID, ProjectID: suite.listener.ProjectID, RegionID: suite.listener.RegionID,
			Name: domain.LbName("repoint-tg"), Status: domain.TargetGroupStatusActive,
		},
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	})

	op, err := suite.uc.Run(contextWithSubject("user:test-actor"), &lbv1.UpdateListenerRequest{
		ListenerId:    string(suite.listener.ID),
		UpdateMask:    &fieldmaskpb.FieldMask{Paths: []string{"target_group_id"}},
		TargetGroupId: string(tgID),
	})
	require.NoError(t, err)
	done := awaitOpDone(t, suite.ops, op.ID, time.Second)
	require.Nil(t, done.Error)

	got := suite.getListener(string(suite.listener.ID))
	v, ok := got.DefaultTargetGroupID.Maybe()
	require.True(t, ok)
	require.Equal(t, tgID, v)
}

// ПУСТАЯ МАСКА — отдельный путь, и на нём снятый вход опаснее всего.
//
// Пустая маска означает правку объекта целиком: применяются ВСЕ изменяемые поля.
// Значит `target_group_id` применяется со своим значением из тела, а оно у
// клиента, пишущего по старому справочнику, пустое — и привязка не «не
// изменилась», а СНИМАЕТСЯ. То есть запрос, который прежде привязывал группу,
// стал бы её отвязывать, и молча.
//
// Поэтому снятый вход отвергается по ПРИСУТСТВИЮ В ТЕЛЕ, а не только по пути в
// маске. Молчаливое игнорирование, которое конвенция update_mask предписывает для
// immutable-полей, здесь не годится: у immutable-поля игнорирование ничего не
// меняет, а тут оно меняет ровно то, ради чего запрос слали.
func TestUpdateListener_RetiredDefaultTargetGroupIdWithEmptyMask_RejectedNotSilentlyCleared(t *testing.T) {
	t.Parallel()
	suite := newUpdateSuite(t)
	tgID := domain.ResourceID(ids.NewID(ids.PrefixTargetGroup))
	suite.repo.seedTG(&kachorepo.TargetGroupRecord{
		TargetGroup: domain.TargetGroup{
			ID: tgID, ProjectID: suite.listener.ProjectID, RegionID: suite.listener.RegionID,
			Name: domain.LbName("empty-mask-tg"), Status: domain.TargetGroupStatusActive,
		},
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	})
	// Привязка ЕСТЬ до запроса: иначе «снялась» было бы неотличимо от «и не было».
	suite.listener.DefaultTargetGroupID = option.MustNewOption(tgID)
	suite.repo.seedListener(suite.listener)

	_, err := suite.uc.Run(contextWithSubject("user:test-actor"), &lbv1.UpdateListenerRequest{
		ListenerId:           string(suite.listener.ID),
		DefaultTargetGroupId: string(tgID), // маски нет вовсе
	})

	require.Equal(t, codes.InvalidArgument, status.Code(err),
		"снятый вход в теле обязан отвергаться и без маски")
	require.Contains(t, status.Convert(err).Message(), "target_group_id")

	// И привязка обязана уцелеть: отказ наступает до записи.
	got := suite.getListener(string(suite.listener.ID))
	v, ok := got.DefaultTargetGroupID.Maybe()
	require.True(t, ok, "привязка снята отказавшим запросом")
	require.Equal(t, tgID, v)
}

// Путь МАСКИ остаётся достижимым — и это надо доказать, иначе ветка
// listenerRetiredMaskPaths мертва (запрет vestigial-кода).
//
// Проверка тела ловит непустое значение; сюда попадает ПУСТОЕ — то есть клиент,
// который пытается СНЯТЬ привязку прежним полем. Молчать здесь нельзя по той же
// причине: снятие прошло бы, но не тем полем, которым клиент его назвал, и
// следующий его запрос по `defaultTargetGroupId` снова ничего бы не значил.
func TestUpdateListener_RetiredDefaultTargetGroupIdInMask_EmptyValue_StillRejected(t *testing.T) {
	t.Parallel()
	suite := newUpdateSuite(t)

	_, err := suite.uc.Run(contextWithSubject("user:test-actor"), &lbv1.UpdateListenerRequest{
		ListenerId:           string(suite.listener.ID),
		UpdateMask:           &fieldmaskpb.FieldMask{Paths: []string{"default_target_group_id"}},
		DefaultTargetGroupId: "", // тело пусто ⇒ проверку тела не задевает
	})

	require.Equal(t, codes.InvalidArgument, status.Code(err),
		"путь маски снятого входа обязан отвергаться и при пустом значении")
	msg := status.Convert(err).Message()
	require.Contains(t, msg, "default_target_group_id")
	require.Contains(t, msg, "target_group_id")
}

// ОБА производителя отказа дают ОДИН текст. Два места об одном предмете
// разошлись бы молча, а тон отказа — часть контракта.
func TestUpdateListener_RetiredDefaultTargetGroup_BothLanesSameMessage(t *testing.T) {
	t.Parallel()
	suite := newUpdateSuite(t)

	_, viaBody := suite.uc.Run(contextWithSubject("user:test-actor"), &lbv1.UpdateListenerRequest{
		ListenerId:           string(suite.listener.ID),
		DefaultTargetGroupId: "tgr-someothergroup1",
	})
	_, viaMask := suite.uc.Run(contextWithSubject("user:test-actor"), &lbv1.UpdateListenerRequest{
		ListenerId: string(suite.listener.ID),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"default_target_group_id"}},
	})

	require.Equal(t, status.Code(viaBody), status.Code(viaMask))
	require.Equal(t, status.Convert(viaBody).Message(), status.Convert(viaMask).Message(),
		"тексты двух полос разошлись — у отказа появилось два разных тона")
}
