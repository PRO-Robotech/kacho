// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionjournal_test

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	registryv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/registry/v1"
	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowtest"
	registryuc "github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/api/registry"
	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
)

// begin — начало «с самого начала журнала».
func begin() *subscriptionv1.SubscriptionRequest_Anchor {
	return &subscriptionv1.SubscriptionRequest_Anchor{Anchor: subscriptionv1.SubscriptionAnchor_BEGINNING}
}

// TestSubscribeAnswersOverTheWire — сервер ПРОВЯЗАН И ОТВЕЧАЕТ, и это проверено
// вызовом.
//
// Объявление журнала несёт имена колонок ЗНАЧЕНИЯМИ, а строку пишет ТРИГГЕР.
// Проба, читающая объявление, зеленела бы и у сервиса, чей журнал разошёлся со
// своей схемой, и у сервиса, где триггера нет вовсе: обе ошибки наступают первым
// запросом в бою, а не сборкой. Поэтому здесь настоящая схема, настоящий
// репозиторий, настоящий сервер и настоящий транспорт.
func TestSubscribeAnswersOverTheWire(t *testing.T) {
	s := newStand(t)
	reg := s.create(t, probeProject, "team-images", map[string]string{"env": "prod"})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stream, err := s.client.Subscribe(ctx, &subscriptionv1.SubscriptionRequest{
		Kinds:     []string{domain.FGAObjectTypeRegistry},
		ProjectId: probeProject,
		Start:     begin(),
	})
	if err != nil {
		t.Fatalf("подписка не открылась: %v", err)
	}

	ev := recv(t, stream)
	if ev.Change != subscriptionv1.SubscriptionEvent_CREATED {
		t.Fatalf("род изменения %v, ожидалось создание", ev.Change)
	}
	if ev.ResourceId != reg.ID {
		t.Fatalf("предмет %q, ожидался %q", ev.ResourceId, reg.ID)
	}
	if ev.Kind != domain.FGAObjectTypeRegistry {
		t.Fatalf("вид %q, ожидался тип объекта модели прав %q", ev.Kind, domain.FGAObjectTypeRegistry)
	}
	if ev.ProjectId != probeProject {
		t.Fatalf("якорь проекта %q, ожидался %q", ev.ProjectId, probeProject)
	}
	if ev.GetState() == nil {
		t.Fatal("создание пришло БЕЗ состояния: подписчику нечего записать, и он вынужден перечитывать предмет")
	}
}

// TestTriggerPayloadDecodesIntoTheSameProjectionTheServiceServes — нагрузка
// ТРИГГЕРА разбирается в то же, что сервис отдаёт на чтение.
//
// # Почему без этой пробы работа не принимается
//
// Ключи нагрузки — ИМЕНА КОЛОНОК (`to_jsonb(NEW)`), а не имена полей Go: это
// цена решения эмитировать триггером. Значит разбор на стороне читателя —
// СОБСТВЕННЫЙ, и он есть ВТОРОЕ место об одном предмете рядом со штатным
// отображением реестра в контракт. Два места об одном предмете расходятся молча,
// и здесь расхождение было бы особенно тихим: подписчик получил бы реестр, у
// которого одно поле пусто, и записал бы пустоту как ФАКТ.
//
// Поэтому сверка идёт по ВСЕМУ сообщению целиком (`proto.Equal`), а не по
// выборочным полям: выборка закрепила бы ответ на те поля, о которых автор
// подумал, и промолчала бы ровно о том, о котором он забыл.
//
// Обе стороны получены из НАСТОЯЩЕЙ базы: слева — событие, собранное журналом из
// нагрузки триггера; справа — строка, прочитанная тем же репозиторием и
// пропущенная через то же отображение, каким её отдаёт сервис.
func TestTriggerPayloadDecodesIntoTheSameProjectionTheServiceServes(t *testing.T) {
	s := newStand(t)
	reg := s.create(t, probeProject, "payload-parity", map[string]string{"env": "prod", "team": "core"})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stream, err := s.client.Subscribe(ctx, &subscriptionv1.SubscriptionRequest{
		Kinds:     []string{domain.FGAObjectTypeRegistry},
		ProjectId: probeProject,
		Start:     begin(),
	})
	if err != nil {
		t.Fatalf("подписка не открылась: %v", err)
	}
	ev := recv(t, stream)

	var fromStream registryv1.Registry
	if err := ev.GetState().UnmarshalTo(&fromStream); err != nil {
		t.Fatalf("состояние события не разворачивается в реестр: %v", err)
	}

	stored, err := s.repo.Get(context.Background(), reg.ID)
	if err != nil {
		t.Fatalf("строка не прочиталась: %v", err)
	}
	// Отображение — то же, каким его отдаёт сервис. Use-case собирается с одними
	// лишь величинами: `ProtoRegistry` читает у него только основу адреса, и
	// коллабораторы ему для этой работы не нужны. Подставлять сюда своё
	// отображение значило бы сверять копию с копией.
	uc := registryuc.New(nil, nil, nil, nil, nil, nil, nil, nil, probeEndpointBase)
	fromService := uc.ProtoRegistry(stored)

	if !proto.Equal(&fromStream, fromService) {
		t.Fatalf("событие и штатное чтение разошлись:\n  поток : %v\n  сервис: %v\n"+
			"Ключи нагрузки — имена колонок, и разбор её здесь СВОЙ: расхождение означает, "+
			"что подписчик записал бы у себя не то, что отдаёт сервис.",
			&fromStream, fromService)
	}
	t.Logf("сверено полей сообщения целиком; адрес %q, отметка %v", fromStream.Endpoint, fromStream.CreatedAt.AsTime())
}

// TestMarkDeletingIsSeenAsAnUpdateWithItsAnchor — переход ACTIVE→DELETING виден
// подписчику, и у события ЕСТЬ якорь проекта.
//
// # Почему именно этот путь
//
// `MarkDeleting` — единственный путь мутации реестра, идущий БЕЗ транзакции
// (`pool.QueryRow`), и ровно он делает решение «эмитировать триггером» не
// вкусом, а необходимостью: пристегнуть вызов эмиттера к транзакции, которой
// нет, нельзя. Триггер же исполняется в неявной транзакции одиночного оператора
// — то есть по построению.
//
// Переход наблюдаем арендатором: реестр перестаёт принимать запись и исчезает из
// живых имён проекта. Подписчик, не узнавший о нём, показывал бы реестр
// работающим.
func TestMarkDeletingIsSeenAsAnUpdateWithItsAnchor(t *testing.T) {
	s := newStand(t)
	reg := s.create(t, probeProject, "going-away", nil)

	if _, err := s.repo.MarkDeleting(context.Background(), reg.ID); err != nil {
		t.Fatalf("перевод в DELETING не прошёл: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stream, err := s.client.Subscribe(ctx, &subscriptionv1.SubscriptionRequest{
		Kinds:     []string{domain.FGAObjectTypeRegistry},
		ProjectId: probeProject,
		Start:     begin(),
	})
	if err != nil {
		t.Fatalf("подписка не открылась: %v", err)
	}

	// Первое событие — создание, второе — правка. Читаем оба: пропустить первое
	// значило бы утверждать про «какое-то событие», а не про этот переход.
	if ev := recv(t, stream); ev.Change != subscriptionv1.SubscriptionEvent_CREATED {
		t.Fatalf("первое событие %v, ожидалось создание", ev.Change)
	}
	ev := recv(t, stream)
	if ev.Change != subscriptionv1.SubscriptionEvent_UPDATED {
		t.Fatalf("переход ACTIVE→DELETING отдан родом %v, ожидалась правка", ev.Change)
	}
	if ev.ProjectId == "" {
		t.Fatal("у события правки ПУСТОЙ якорь проекта: пустой якорь по контракту означает " +
			"предмет уровня аккаунта или кластера, и подписка с осью проекта такое событие не пропустит")
	}
	var got registryv1.Registry
	if err := ev.GetState().UnmarshalTo(&got); err != nil {
		t.Fatalf("состояние правки не разворачивается: %v", err)
	}
	if got.Status != registryv1.RegistryStatus_REGISTRY_STATUS_DELETING {
		t.Fatalf("состояние в событии %v, ожидалось DELETING: подписчик показывал бы реестр работающим", got.Status)
	}
}

// TestRemovalReachesASubscriberWhoMayNoLongerSeeThePredmet — событие снятия
// доезжает до того, кто вправе видеть ПРОЕКТ, даже когда предмета он уже видеть
// не вправе.
//
// # Почему это не край, а обычный ход событий
//
// Путь удаления коммитит в одной транзакции строку журнала о снятии и намерение
// снять кортеж владения; кортеж снимает дренаж, асинхронно. Значит к моменту,
// когда подписчик читает событие, предмета в модели прав уже нет — и построчный
// вопрос «вправе ли он видеть этот реестр» получает «нет» ЗАКОННО.
//
// Событие при этом не приходит вовсе: ни ошибки, ни пропуска в нумерации, поток
// открыт и молчит. Ровно против этого исхода и стоит якорь КОЛОНКОЙ: решение о
// показе снятия принимается ПО ЯКОРЮ, без обращения к предмету, которого больше
// нет.
//
// Сужатель здесь разрешает ПРОЕКТ и не разрешает реестр — то есть ровно то
// состояние, в котором подписчик оказывается через доли секунды после всякого
// удаления.
func TestRemovalReachesASubscriberWhoMayNoLongerSeeThePredmet(t *testing.T) {
	s := newStandWithNarrower(t, narrowtest.Allowing(probeProject))
	reg := s.create(t, probeProject, "removed-and-forgotten", nil)

	if _, err := s.repo.MarkDeleting(context.Background(), reg.ID); err != nil {
		t.Fatalf("перевод в DELETING не прошёл: %v", err)
	}
	if err := s.repo.Delete(context.Background(), reg.ID,
		domain.UnregisterIntentForDelete(reg.ID, probeProject)); err != nil {
		t.Fatalf("удаление не прошло: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stream, err := s.client.Subscribe(ctx, &subscriptionv1.SubscriptionRequest{
		Kinds:     []string{domain.FGAObjectTypeRegistry},
		ProjectId: probeProject,
		Start:     begin(),
	})
	if err != nil {
		t.Fatalf("подписка не открылась: %v", err)
	}

	var removal *subscriptionv1.SubscriptionEvent
	for i := 0; i < 3 && removal == nil; i++ {
		if ev := recv(t, stream); ev.Change == subscriptionv1.SubscriptionEvent_DELETED {
			removal = ev
		}
	}
	if removal == nil {
		t.Fatal("снятие НЕ доехало: подписчик, потерявший право на предмет, держал бы удалённый " +
			"реестр вечно — ни ошибки, ни пропуска в нумерации при этом не будет")
	}
	if removal.ResourceId != reg.ID {
		t.Fatalf("снят предмет %q, ожидался %q", removal.ResourceId, reg.ID)
	}
	if removal.ProjectId != probeProject {
		t.Fatalf("якорь проекта у снятия %q, ожидался %q", removal.ProjectId, probeProject)
	}
	if removal.GetState() != nil {
		t.Fatal("снятие принесло состояние: подписчик прочитал бы почти пустой реестр как ПОЛНОЕ состояние предмета")
	}
}

// TestRemovalIsWithheldFromASubscriberWhoMayNotSeeTheProject — ОТРИЦАНИЕ в паре
// с пробой выше: суждение по якорю не выходит за проект.
//
// Без этой пробы предыдущая зеленела бы и на сервере, который отдаёт снятия
// ВСЕМ: «событие пришло» выполняется и тогда, когда якорь не спрашивают вовсе.
//
// Положительный контроль внутри самой пробы обязателен и по второй причине:
// подписка молчит и тогда, когда снятие законно отсеяно, и тогда, когда поток
// сломан. Различает их видимое событие, пришедшее СЛЕДОМ, — по нему видно, что
// поток жив, дочитал до конца окна и именно ОТСЕЯЛ снятие, а не отстал.
func TestRemovalIsWithheldFromASubscriberWhoMayNotSeeTheProject(t *testing.T) {
	// Разрешены СВОЙ проект и свой реестр; чужой проект — нет.
	mine := newReg(probeProject, "mine", nil)
	s := newStandWithNarrower(t, narrowtest.Allowing(probeProject, mine.ID))

	// Снятие в ЧУЖОМ проекте — его вызывающий видеть не вправе.
	alien := s.create(t, probeOtherProject, "alien", nil)
	if _, err := s.repo.MarkDeleting(context.Background(), alien.ID); err != nil {
		t.Fatalf("перевод чужого реестра в DELETING не прошёл: %v", err)
	}
	if err := s.repo.Delete(context.Background(), alien.ID,
		domain.UnregisterIntentForDelete(alien.ID, probeOtherProject)); err != nil {
		t.Fatalf("удаление чужого реестра не прошло: %v", err)
	}
	// Видимое событие следом — положительный контроль живости потока.
	if _, _, err := s.repo.Insert(context.Background(), mine,
		domain.RegisterIntentForCreate(mine, "user", "usr-alice")); err != nil {
		t.Fatalf("свой реестр не создался: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Подписка БЕЗ оси проекта: с осью страж отверг бы открытие, и предмет пробы
	// (суждение о СТРОКЕ) не наступил бы вовсе.
	stream, err := s.client.Subscribe(ctx, &subscriptionv1.SubscriptionRequest{
		Kinds: []string{domain.FGAObjectTypeRegistry},
		Start: begin(),
	})
	if err != nil {
		t.Fatalf("подписка не открылась: %v", err)
	}

	ev := recv(t, stream)
	if ev.ResourceId == alien.ID {
		t.Fatalf("отдано событие в ЧУЖОМ проекте (%s): суждение по якорю вышло за проект, "+
			"и подписчик узнал о существовании предмета, которого видеть не вправе", ev.ProjectId)
	}
	if ev.ResourceId != mine.ID || ev.Change != subscriptionv1.SubscriptionEvent_CREATED {
		t.Fatalf("пришло не ожидаемое видимое событие: предмет %q, род %v", ev.ResourceId, ev.Change)
	}
}

// TestTheProjectAxisNarrowsByTheColumn — ось проекта ОТБИРАЕТ, а не украшает.
//
// Сужатель разрешает всё, поэтому единственное, чем чужое событие может быть
// отсеяно, — ось. Без отбора первым пришло бы событие чужого проекта, которое
// журнал записал раньше.
func TestTheProjectAxisNarrowsByTheColumn(t *testing.T) {
	s := newStand(t)
	alien := s.create(t, probeOtherProject, "alien-first", nil)
	mine := s.create(t, probeProject, "mine-second", nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stream, err := s.client.Subscribe(ctx, &subscriptionv1.SubscriptionRequest{
		Kinds:     []string{domain.FGAObjectTypeRegistry},
		ProjectId: probeProject,
		Start:     begin(),
	})
	if err != nil {
		t.Fatalf("подписка не открылась: %v", err)
	}

	ev := recv(t, stream)
	if ev.ResourceId == alien.ID {
		t.Fatalf("первым пришло событие ЧУЖОГО проекта (%s): ось не отбирает", ev.ProjectId)
	}
	if ev.ResourceId != mine.ID {
		t.Fatalf("предмет %q, ожидался %q", ev.ResourceId, mine.ID)
	}
}
