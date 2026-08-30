// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionjournal_test

import (
	"context"
	"testing"
	"time"

	storagev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"
	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowtest"
	"github.com/PRO-Robotech/kacho/services/storage/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/reconciler"
)

// TestSubscribeAnswersOverTheWire — сервер ПРОВЯЗАН И ОТВЕЧАЕТ, и это проверено
// вызовом.
//
// Объявление журнала несёт имена колонок ЗНАЧЕНИЯМИ, а производителя строк —
// триггером базы. Проба, читающая объявление, зеленела бы у сервиса, чей журнал
// разошёлся со своей схемой, и не увидела бы триггера вовсе: он существует
// только на поднятой базе.
func TestSubscribeAnswersOverTheWire(t *testing.T) {
	s := newStand(t)
	v := s.createVolume(t, probeProject, "web-1")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stream := s.subscribe(t, ctx, probeProject, authzfilter.ResourceTypeVolume)

	ev := recv(t, stream)
	// На проводе — ТИП ОБЪЕКТА (`storage_volume`), а не слово, которым триггер
	// записал колонку (`Volume`). Здесь они различаются, и утверждение потому
	// различающее.
	if ev.Kind != authzfilter.ResourceTypeVolume || ev.ResourceId != v.ID {
		t.Fatalf("пришло не то событие: вид %q, предмет %q (ожидался %q)",
			ev.Kind, ev.ResourceId, v.ID)
	}
	if ev.ProjectId != probeProject {
		t.Fatalf("якорь проекта %q, ожидался %q — решение о показе принимается по нему",
			ev.ProjectId, probeProject)
	}
	if ev.Change != subscriptionv1.SubscriptionEvent_CREATED {
		t.Fatalf("род изменения %v, ожидалось создание", ev.Change)
	}
	// Состояние предмета доезжает ДО КЛИЕНТА — через провод, а не только через
	// сборщик.
	//
	// # Что здесь стояло раньше и почему ЗАМЕНЕНО, а не ослаблено
	//
	// Стояло утверждение «состояния нет», охранявшее прежнее решение: публичная
	// проекция выводится через таблицы, и собрать её в триггере значило бы завести
	// вторую реализацию `protoconv` на SQL. Основание снято — триггер кладёт в
	// конверт ВХОДЫ, а не проекцию, — и утверждение об отсутствии стало ложным.
	// Снять его совсем значило бы вывести провод из наблюдения ровно там, где он
	// теперь несёт предмет; поэтому оно перевёрнуто в утверждение о ПОЛНОТЕ.
	//
	// # Почему проверка на проводе, а не только у сборщика
	//
	// Между сборщиком и клиентом лежит упаковка в `Any` и выбор ветви носителя.
	// Проба у сборщика их не проходит, и состояние, собранное верно и не
	// выбранное носителем, осталось бы невидимым: клиент получил бы пустую ветвь,
	// а обе стороны выглядели бы исправными.
	if ev.GetStateUnavailable() != nil {
		t.Fatalf("состояние объявлено недоступным (%v): у создания тома оно есть, и клиент, "+
			"поверивший этой ветви, пошёл бы читать ресурс на каждое событие — то есть "+
			"вернулся бы к опросу, который подписка и снимает", ev.GetStateUnavailable())
	}
	packed := ev.GetState()
	if packed == nil {
		t.Fatal("носитель нагрузки не выбран вовсе — форма требует одну из двух ветвей")
	}
	var got storagev1.Volume
	if err := packed.UnmarshalTo(&got); err != nil {
		t.Fatalf("на проводе не состояние тома (%v): клиент развернёт его по типу и получит "+
			"отказ вместо предмета", err)
	}
	if got.Id != v.ID || got.ProjectId != probeProject || got.Name != "web-1" {
		t.Fatalf("состояние на проводе описывает не тот предмет: %v", &got)
	}
	if got.Status != storagev1.Volume_CREATING {
		t.Errorf("статус на проводе %v, ожидался CREATING — том ещё не подтверждён "+
			"сверщиком, и выведенный статус обязан это отражать", got.Status)
	}
}

// TestRemovalReachesTheSubscriberWithItsProjectAnchor — СОБЫТИЕ СНЯТИЯ доезжает
// до подписки, отобранной по проекту.
//
// Это проба того, ради чего эмиссия отдана триггеру. Снятие идёт путём СВЕРЩИКА
// (`reconciler.Store.Forget`), а он выполняет `DELETE … WHERE id = $1 AND state =
// 'DELETING'` БЕЗ `RETURNING project_id`: при вызове-эмиссии якорь снятию было бы
// взять неоткуда, ось `project_id` его бы не пропустила, и потребитель, снявший
// опрос, держал бы удалённый том ВЕЧНО. Отказ этот тихий — ни ошибки, ни пропуска
// в нумерации, — поэтому он проверяется, а не подразумевается.
func TestRemovalReachesTheSubscriberWithItsProjectAnchor(t *testing.T) {
	s := newStand(t)
	v := s.createVolume(t, probeProject, "doomed")

	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `UPDATE volumes SET state = 'DELETING' WHERE id = $1`, v.ID); err != nil {
		t.Fatalf("том не переведён в снятие: %v", err)
	}
	if err := reconciler.NewStore(s.pool).Forget(ctx, reconciler.KindVolume, v.ID); err != nil {
		t.Fatalf("сверщик не снял строку: %v", err)
	}

	sctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	stream := s.subscribe(t, sctx, probeProject, authzfilter.ResourceTypeVolume)

	var removal *subscriptionv1.SubscriptionEvent
	for removal == nil {
		ev := recv(t, stream)
		if ev.Change == subscriptionv1.SubscriptionEvent_DELETED {
			removal = ev
		}
	}
	if removal.ResourceId != v.ID {
		t.Fatalf("предмет снятия %q, ожидался %q", removal.ResourceId, v.ID)
	}
	if removal.ProjectId != probeProject {
		t.Fatalf("у СНЯТИЯ якорь проекта %q, ожидался %q. Пустой якорь означает "+
			"«предмет уровня аккаунта», и подписка с осью проекта событие не "+
			"пропустила бы — потребитель держал бы удалённый том вечно",
			removal.ProjectId, probeProject)
	}
}

// TestTheProjectAxisNarrowsByTheColumn — ось проекта ОТБИРАЕТ, а не украшает.
//
// Отрицание в паре с положительным контролем той же пробы: без положительного
// «чужого не видно» зеленело бы на потоке, который не отдаёт ничего вовсе.
func TestTheProjectAxisNarrowsByTheColumn(t *testing.T) {
	s := newStand(t)
	other := s.createVolume(t, probeOther, "not-mine")
	mine := s.createVolume(t, probeProject, "mine")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stream := s.subscribe(t, ctx, probeProject, authzfilter.ResourceTypeVolume)

	ev := recv(t, stream)
	if ev.ResourceId == other.ID {
		t.Fatal("отдано событие ЧУЖОГО проекта: ось project_id не отбирает, и подписчик " +
			"видит ресурсы, которых не называл")
	}
	if ev.ResourceId != mine.ID {
		t.Fatalf("отдан предмет %q, ожидался свой %q", ev.ResourceId, mine.ID)
	}
}

// TestRemovalReachesASubscriberWhoMayNoLongerSeeThePredmet — событие снятия
// доезжает до того, кто вправе видеть ПРОЕКТ, даже когда предмета он уже видеть
// не вправе.
//
// # Почему это не край, а обычный ход событий
//
// Путь удаления коммитит строку журнала о снятии и намерение снять кортеж
// владения; кортеж снимает дренаж, асинхронно. Значит к моменту, когда подписчик
// читает событие, предмета в модели прав уже нет — и построчный вопрос «вправе ли
// он видеть этот том» получает «нет» ЗАКОННО.
//
// Событие при этом не приходит вовсе: ни ошибки, ни пропуска в нумерации, поток
// открыт и молчит. Это ровно тот исход, против которого стоит якорь проекта, —
// «потребитель держал бы удалённый том вечно», — только наступающий на шаг позже:
// якорь спас событие от отбора ОСЬЮ, а построчное сужение отсеяло его потом.
//
// Сужатель здесь разрешает ПРОЕКТ и не разрешает том — то есть ровно то
// состояние, в котором подписчик оказывается через доли секунды после всякого
// удаления.
func TestRemovalReachesASubscriberWhoMayNoLongerSeeThePredmet(t *testing.T) {
	s := newStandWithNarrower(t, narrowtest.Allowing(probeProject))
	v := s.createVolume(t, probeProject, "revoked")

	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `UPDATE volumes SET state = 'DELETING' WHERE id = $1`, v.ID); err != nil {
		t.Fatalf("том не переведён в снятие: %v", err)
	}
	if err := reconciler.NewStore(s.pool).Forget(ctx, reconciler.KindVolume, v.ID); err != nil {
		t.Fatalf("сверщик не снял строку: %v", err)
	}

	sctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	stream := s.subscribe(t, sctx, probeProject, authzfilter.ResourceTypeVolume)

	// Событий создания и правки по этому тому подписчик не увидит: том ему уже
	// не разрешён, и построчное сужение их отсеет ЗАКОННО. Доехать обязано
	// именно снятие — оно судится ЯКОРЕМ.
	ev := recv(t, stream)
	if ev.Change != subscriptionv1.SubscriptionEvent_DELETED {
		t.Fatalf("первым доехало событие рода %v по предмету %q; снятие судится якорем "+
			"проекта и обязано пройти там, где предмет уже не разрешён", ev.Change, ev.ResourceId)
	}
	if ev.ResourceId != v.ID {
		t.Fatalf("предмет %q, ожидался %q", ev.ResourceId, v.ID)
	}
	if ev.ProjectId != probeProject {
		t.Fatalf("якорь проекта %q, ожидался %q", ev.ProjectId, probeProject)
	}
}

// TestRemovalIsWithheldFromASubscriberWhoMayNotSeeTheProject — ОТРИЦАНИЕ в паре с
// пробой выше: суждение по якорю не выходит за проект.
//
// Без этой пробы предыдущая зеленела бы и на сервере, который отдаёт снятия
// ВСЕМ: «событие пришло» выполняется и тогда, когда якорь не спрашивают вовсе.
//
// Положительный контроль внутри самой пробы обязателен и по второй причине:
// подписка молчит и тогда, когда снятие законно отсеяно, и тогда, когда поток
// сломан. Различает их видимое событие, пришедшее СЛЕДОМ, — по нему видно, что
// поток жив, дочитал до конца окна и именно ОТСЕЯЛ снятие, а не отстал.
func TestRemovalIsWithheldFromASubscriberWhoMayNotSeeTheProject(t *testing.T) {
	// Идентификатор положительного контроля назван ДО подъёма стенда: сужатель
	// строится вместе с сервером, и разрешить том задним числом уже нечему.
	mineID := ids.NewID(domain.PrefixVolume)
	// Разрешены СВОЙ проект и свой том; чужой проект — нет.
	s := newStandWithNarrower(t, narrowtest.Allowing(probeProject, mineID))
	doomed := s.createVolume(t, probeOther, "foreign-doomed")

	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `UPDATE volumes SET state = 'DELETING' WHERE id = $1`, doomed.ID); err != nil {
		t.Fatalf("чужой том не переведён в снятие: %v", err)
	}
	if err := reconciler.NewStore(s.pool).Forget(ctx, reconciler.KindVolume, doomed.ID); err != nil {
		t.Fatalf("сверщик не снял чужую строку: %v", err)
	}
	// Видимое событие следом — положительный контроль живости потока.
	mine := s.createVolumeWithID(t, mineID, probeProject, "mine-alive")

	sctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// Подписка БЕЗ оси проекта: с осью страж отверг бы открытие, и предмет пробы
	// (суждение о СТРОКЕ) не наступил бы вовсе.
	stream := s.subscribe(t, sctx, "", authzfilter.ResourceTypeVolume)

	ev := recv(t, stream)
	if ev.ResourceId == doomed.ID {
		t.Fatalf("отдано снятие в ЧУЖОМ проекте (%s): суждение по якорю вышло за проект, "+
			"и подписчик узнал о существовании предмета, которого видеть не вправе",
			ev.ProjectId)
	}
	if ev.ResourceId != mine.ID || ev.Change != subscriptionv1.SubscriptionEvent_CREATED {
		t.Fatalf("пришло не ожидаемое видимое событие: предмет %q, род %v", ev.ResourceId, ev.Change)
	}
}
