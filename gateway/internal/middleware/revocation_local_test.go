// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

// revocation_local_test.go — ОТЗЫВ, ЗАПИСАННЫЙ У НАС, ОБЯЗАН ДЕЙСТВОВАТЬ НА
// ПУТИ ЗАПРОСА.
//
// ПРЕДМЕТ (#797). Выход человека записывает отзыв В НАШУ базу: по
// идентификатору удостоверения и отсечкой по времени. Контроль, действующий на
// ВЫДАЧЕ и не действующий на ПРЕДЪЯВЛЕНИИ, отзывом не является — предъявленное
// проходит до истечения срока, и это состояние не сходится само.
//
// ИСТОЧНИК ОДИН. Их было два, и спрашивались оба: сперва наша запись, потом
// чужой поставщик по административному пути его интроспекции. Второй снят
// вместе с самим поставщиком — его токенов край не принимает, и вопрос ему был
// бы утверждением о предмете, которого у него нет. Композиция выродилась в свою
// первую половину, и половина эта — авторитет.
//
// ПОЧЕМУ FAIL-CLOSED. Недоступность источника отзыва не есть «не отозван».
// Ответ «не знаю» на вопрос о безопасности означает отказ — тот же контракт, что
// у проверки доступа, которую край уже делает рядом. Мягкого прохода для ЭТОГО
// источника не было и нет: авторитет наш, и проход означал бы «отзываем и свой
// же отзыв не исполняем».

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeLocalReader — наш источник отзыва.
type fakeLocalReader struct {
	revoked bool
	err     error
	asked   int
}

func (f *fakeLocalReader) IsSessionRevoked(_ context.Context, _ string) (bool, error) {
	f.asked++
	if f.err != nil {
		return false, f.err
	}
	return f.revoked, nil
}

func TestLocalRevocationIsAskedAndStopsTheRequest(t *testing.T) {
	local := &fakeLocalReader{revoked: true}

	c := NewOwnRevocationSource(local)
	_, err := c.Introspect(context.Background(), "jti-1", "raw")

	if !errors.Is(err, ErrTokenInactive) {
		t.Fatalf("удостоверение, отозванное В НАШЕЙ записи, не остановило запрос: err=%v. "+
			"Пока это так, выход пользователя не прекращает доступ — он лишь "+
			"записывает намерение", err)
	}
	if local.asked != 1 {
		t.Fatalf("наш источник спрошен %d раз, ждали 1", local.asked)
	}
}

func TestLiveTokenPassesAndTheAnswerIsWhole(t *testing.T) {
	// ЗАКОННЫЙ БЛИЗНЕЦ: без него проба выше зеленела бы на проверке, которая
	// отвергает всё подряд.
	//
	// Отличие от предмета РОВНО ОДНО — что ответил источник.
	local := &fakeLocalReader{revoked: false}

	c := NewOwnRevocationSource(local)
	res, err := c.Introspect(context.Background(), "jti-2", "raw")
	if err != nil {
		t.Fatalf("не отозванное удостоверение отвергнуто: %v", err)
	}
	if local.asked != 1 {
		t.Fatalf("наш источник спрошен %d раз, ждали 1", local.asked)
	}
	// «Не отозвано» здесь ПОЛНЫЙ ответ, а не половина прежнего: спрашивать
	// больше некого, и вызывающий обязан получить годность, а не пустоту,
	// которую следующий слой прочитает как «неизвестно».
	if !res.Active {
		t.Fatal("источник ответил «не отозвано», а исход не назван годным — " +
			"вызывающий не отличит его от неотвеченного вопроса")
	}
}

func TestUnavailableSourceIsNotAnAnswer(t *testing.T) {
	// FAIL-CLOSED: «не знаю» — не «не отозван».
	boom := errors.New("сосед не ответил")
	local := &fakeLocalReader{err: boom}

	c := NewOwnRevocationSource(local)
	_, err := c.Introspect(context.Background(), "jti-3", "raw")

	if err == nil {
		t.Fatal("недоступность нашего источника отзыва прошла как «удостоверение живо» — " +
			"это открывает контроль ровно тогда, когда он не работает")
	}
	if errors.Is(err, ErrTokenInactive) {
		t.Fatalf("недоступность подана как отзыв: %v. Это разные исходы, и "+
			"вызывающий обязан их различать", err)
	}
	// Признак молчания ТИПИЗИРОВАН, а не выведен из текста: текст пишет тот, кто
	// ошибку породил, и он меняется от версии соседа. Читает признак слой
	// решения (auth_revocation.go), и без него молчание нашего источника попало
	// бы в мягкий проход, объявленный когда-то для ЧУЖОГО.
	if !errors.Is(err, ErrOwnRevocationSourceSilent) {
		t.Fatalf("молчание не несёт своего признака: %v", err)
	}
	// Причина сохранена цепочкой: журнал обязан назвать диагноз, а не только
	// класс исхода.
	if !errors.Is(err, boom) {
		t.Fatalf("причина молчания потеряна: %v", err)
	}
}

func TestReaderWithoutASourceRefuses(t *testing.T) {
	// Конструктор участника ТРЕБУЕТ, но не проверяет. Читатель без источника
	// отвечал бы на вопрос о безопасности, ничего не спросив, и отличить это от
	// исправной работы было бы нечем.
	//
	// Признак — ErrIntrospectionMisconfigured, а не «не ответил»: неполная
	// сборка не лечится повтором.
	c := NewOwnRevocationSource(nil)
	_, err := c.Introspect(context.Background(), "jti-4", "raw")
	if !errors.Is(err, ErrIntrospectionMisconfigured) {
		t.Fatalf("читатель без источника обязан отвечать признаком настройки, получено: %v", err)
	}
}

// ─── БЮДЖЕТ ОДНОГО ВОПРОСА (C1) ────────────────────────────────────────────
//
// Вопрос об отзыве стоит НА ПУТИ ЗАПРОСА и уходит к соседу по кластеру. Сырой
// контекст запроса пределом не является: у внешнего слушателя края своего
// предела нет, а у соединения к службе — ни `WithTimeout`, ни
// `WithDefaultCallOptions`. Неотвечающий сосед без названного бюджета держит
// горутину столько, сколько держится клиент.
//
// Прежде предел на этой полосе НЁС второй участник композиции — кеш
// интроспекции чужого поставщика с `Timeout: cfg.IntrospectionTimeoutMs`. Он
// снят вместе со своим предметом, и полоса осталась без бюджета целиком: ручку
// `KACHO_INTROSPECTION_TIMEOUT_MS` читают три места прод-кода, и ни одно из них
// не на этой полосе.
//
// Соседняя полоса того же соседа по тому же соединению бюджет ставит
// (`BasicCredentialCallBudget`), и форма взята у неё.

// deadlineProbe — источник отзыва, ЗАПОМИНАЮЩИЙ предел, с которым его спросили.
type deadlineProbe struct {
	asked       int
	hadDeadline bool
	left        time.Duration
}

func (d *deadlineProbe) IsSessionRevoked(ctx context.Context, _ string) (bool, error) {
	d.asked++
	dl, ok := ctx.Deadline()
	d.hadDeadline = ok
	if ok {
		d.left = time.Until(dl)
	}
	return false, nil
}

// TestOwnRevocationSourceCarriesItsOwnCallBudget — ДЕФЕКТ: вызов соседа уходит
// на сыром контексте запроса.
func TestOwnRevocationSourceCarriesItsOwnCallBudget(t *testing.T) {
	probe := &deadlineProbe{}

	if _, err := NewOwnRevocationSource(probe).Introspect(
		context.Background(), "jti-1", "raw"); err != nil {
		t.Fatalf("живое удостоверение отвергнуто: %v", err)
	}

	if probe.asked != 1 {
		t.Fatalf("источник спрошен %d раз, ждали 1", probe.asked)
	}
	if !probe.hadDeadline {
		t.Fatalf("вопрос об отзыве ушёл к соседу БЕЗ своего предела: вызывающий не " +
			"обязан его ставить, у внешнего слушателя края предела нет, у соединения " +
			"к службе — тоже. Неотвечающий сосед держит горутину пути запроса столько, " +
			"сколько держится клиент. Объяви бюджет рядом с вызовом, как это делает " +
			"полоса базового секрета (BasicCredentialCallBudget)")
	}
	if probe.left > OwnRevocationCallBudget {
		t.Errorf("предел вызова %v ШИРЕ объявленного бюджета %v", probe.left, OwnRevocationCallBudget)
	}
	if probe.left < OwnRevocationCallBudget/2 {
		t.Errorf("предел вызова %v много уже объявленного бюджета %v — бюджет "+
			"объявлен один, а применяется другой", probe.left, OwnRevocationCallBudget)
	}
}

// TestOwnRevocationSourceDoesNotExtendATighterCallerDeadline — ЗАКОННЫЙ
// БЛИЗНЕЦ: бюджет НЕ расширяет предел, уже поставленный вызывающим.
//
// Против дефекта меняется РОВНО ОДИН факт: у контекста вызывающего предел уже
// есть. Без этой половины «бюджет поставлен» было бы неотличимо от «бюджет
// поставлен ВМЕСТО чужого, более строгого».
func TestOwnRevocationSourceDoesNotExtendATighterCallerDeadline(t *testing.T) {
	probe := &deadlineProbe{}
	const tighter = 20 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), tighter)
	defer cancel()

	if _, err := NewOwnRevocationSource(probe).Introspect(ctx, "jti-1", "raw"); err != nil {
		t.Fatalf("живое удостоверение отвергнуто: %v", err)
	}
	if !probe.hadDeadline {
		t.Fatal("предел вызывающего до источника не доехал — измеряется не то")
	}
	if probe.left > tighter {
		t.Errorf("бюджет РАСШИРИЛ предел вызывающего: у источника %v при заданных %v. "+
			"Свой бюджет — потолок, а не замена чужого решения", probe.left, tighter)
	}
}
