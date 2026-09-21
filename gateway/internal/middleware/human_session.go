// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// human_session.go — НАША сессия человека, прочитанная по носителю (посадка
// `own`; приёмка Ф3, Р7).
//
// # Порт, а не клиент
//
// Полоса личности и маршрут «кто я» спрашивают службу о сессии через ЭТОТ порт;
// адаптер над `InternalHumanSessionService.Resolve` живёт в
// `gateway/internal/clients` и единственный знает коды транспорта. Порт объявлен
// здесь, чтобы полоса проверялась дублёром без поднятого соседа — и чтобы
// дублёр был на ОДИН вопрос и не снисходительнее настоящей службы.
//
// # Что ответ несёт и чего не несёт — решение владельца (Ф3-09, Д12)
//
// Состав — ровно то, на чём край РЕШАЕТ: субъект (наш идентификатор, F4d Р10),
// почта и имя (для «кто я»), момент аутентификации (для отсечки — Р7), срок
// (показывается, не сравнивается — Ф3-11), уровень уверенности (пол ступенчатой
// аутентификации, Ф11) и подтверждённость адреса. Множества предъявленного и
// момента последнего предъявления здесь нет:
// на крае у них нет читателя, а поле без читателя есть значение, которое пишут
// и не читают.
//
// # Кэша НЕТ — ни положительного, ни отрицательного (Р7)
//
// Читатель сессии поставщика держит положительный ответ 30 с. С таким окном
// выход и блокировка не могут вычеркнуть чужой кэш, и Ф1-14/Ф1-16 («негодны
// немедленно») не производятся. Один вызов на предъявление — цена, уже
// уплаченная на этом же пути решением о доступе; второй вызов той же службы
// порядка не меняет.
package middleware

import (
	"context"
	"errors"
	"sync/atomic"
	"time"
)

// HumanSession — сессия, прочитанная службой по носителю (Ф3 Р1, состав Ф3-09).
type HumanSession struct {
	// UserID — НАШ субъект (F4d Р10), не внешний идентификатор.
	UserID      string
	Email       string
	DisplayName string
	// AuthenticatedAt — момент аутентификации, неподвижный на всю жизнь сессии
	// (Ф11 Р6). Приходит В РАЗРЕШЕНИИ ХРАНИЛИЩА (микросекунды), не усечённым:
	// край сравнивает его с отсечкой включающе на суб-секундной оси (Р7, §4.1
	// п.19), и усечённая пара сравнима только на целых секундах.
	AuthenticatedAt time.Time
	// ExpiresAt — абсолютный срок; край его не сравнивает (срок судит служба —
	// Ф3-11), только показывает. Усечён до секунды по конвенции.
	ExpiresAt time.Time
	// AssuranceLevel — уровень уверенности НА ОСИ КАТАЛОГА («1» — пароль): наша
	// сессия объявляет его сама (Ф11), перевода со словаря поставщика здесь нет.
	AssuranceLevel string
	// EmailVerified — подтверждён ли ТЕКУЩИЙ адрес (Ф2 П1).
	EmailVerified bool
}

// ErrHumanSessionUnsupported — авторитет ЖИВ, но такого вопроса не предлагает
// (`UNIMPLEMENTED` на `Resolve`).
//
// В отличие от того же исхода на вопросе об отсечке (ErrSessionCutoffUnsupported),
// здесь это ОТКАЗ, а не проход: без ответа о сессии годность носителя не
// подтверждена ничем, и «проход громко» означал бы личность из ниоткуда.
// F4d-23: тот же код и тот же текст, что у отсечки, носитель цел. Ставит его
// АДАПТЕР — знание о кодах транспорта принадлежит ему.
var ErrHumanSessionUnsupported = errors.New("human session: authority does not offer this question")

// HumanSessionReader — порт вопроса «какая сессия стоит за этим носителем».
//
// Три исхода несёт ПАРА (found, err): «сессии нет» (found=false, err=nil) — один
// ответ на значение неизвестно · снята выходом · истекла · личность
// заблокирована (Ф3-10); «спросить не удалось» (err) — F4d-23. Слитые в одно,
// они дали бы либо отказ с гашением носителя на заминке соседа, либо анонимный
// проход держателя снятой копии.
type HumanSessionReader interface {
	ResolveHumanSession(ctx context.Context, bearer string) (sess HumanSession, found bool, err error)
}

// SessionLaneSnapshot — ПРОЧИТАННЫЕ клетки полосы сессии (Ф3-48). Каждая
// существует с нулём до первого события; читает их коллектор диагностической
// поверхности через композиционный корень.
type SessionLaneSnapshot struct {
	// CutoffDenied — отказов F4d-22 по отсечке (носитель погашен).
	CutoffDenied uint64
	// NoSession — отказов F4d-22 на «сессии нет» при существующем носителе
	// (пути платформы и «кто я»; на глаголах формы исход ретранслируется и
	// здесь не считается).
	NoSession uint64
	// Unavailable — отказов F4d-23: служба не ответила ни на один из двух
	// вопросов либо не предлагает вопроса о сессии (носитель цел).
	Unavailable uint64
	// RolloutWindow — проходов «громко»: авторитет не предлагает вопроса об
	// отсечке при подтверждённой сессии (F4d-29).
	RolloutWindow uint64
	// AssuranceOffAxis — ответов службы о живой сессии с уровнем вне оси сессии
	// (нет, «0», словарь поставщика — Ф11-19). Не исход отказа: на глаголе без
	// пола такой ответ проходит, а состояние докладывается само по себе.
	AssuranceOffAxis uint64

	// TransitionalFloorWithheld — сколько раз чужая сессия предъявила уровень,
	// а край его не пропустил, потому что живы ОБА читателя носителя. Величина
	// показывает, скольким людям переходное окно мешает выполнить действие с
	// полом второго фактора, — то есть пора ли его закрывать.
	TransitionalFloorWithheld uint64
}

// SessionLaneCounts — накопитель клеток полосы сессии на горячем пути.
type SessionLaneCounts struct {
	cutoffDenied     atomic.Uint64
	noSession        atomic.Uint64
	unavailable      atomic.Uint64
	rolloutWindow    atomic.Uint64
	assuranceOffAxis atomic.Uint64

	// transitionalFloorWithheld — см. recordTransitionalFloorWithheld.
	transitionalFloorWithheld atomic.Uint64
}

// Snapshot — слепок клеток для коллектора.
func (c *SessionLaneCounts) Snapshot() SessionLaneSnapshot {
	if c == nil {
		return SessionLaneSnapshot{}
	}
	return SessionLaneSnapshot{
		CutoffDenied:     c.cutoffDenied.Load(),
		NoSession:        c.noSession.Load(),
		Unavailable:      c.unavailable.Load(),
		RolloutWindow:    c.rolloutWindow.Load(),
		AssuranceOffAxis: c.assuranceOffAxis.Load(),

		TransitionalFloorWithheld: c.transitionalFloorWithheld.Load(),
	}
}

// record* — nil-безопасны: полоса, собранная без накопителя, считает в никуда,
// но не падает; отсутствие читателя ловит гейт дерева, а не паника на запросе.
func (c *SessionLaneCounts) recordCutoffDenied() {
	if c != nil {
		c.cutoffDenied.Add(1)
	}
}

func (c *SessionLaneCounts) recordNoSession() {
	if c != nil {
		c.noSession.Add(1)
	}
}

func (c *SessionLaneCounts) recordUnavailable() {
	if c != nil {
		c.unavailable.Add(1)
	}
}

func (c *SessionLaneCounts) recordRolloutWindow() {
	if c != nil {
		c.rolloutWindow.Add(1)
	}
}

func (c *SessionLaneCounts) recordAssuranceOffAxis() {
	if c != nil {
		c.assuranceOffAxis.Add(1)
	}
}

// recordTransitionalFloorWithheld — чужая сессия предъявила уровень, а край его
// не пропустил, потому что живы оба читателя.
//
// Своя клетка обязательна: без неё «в этом окне положительный пол на чужой
// полосе не удовлетворяется» осталось бы невидимым до первой жалобы человека,
// у которого действие с полом перестало выполняться. Клетка же называет
// величину, по которой видно, ПОРА ЛИ закрывать окно.
func (c *SessionLaneCounts) recordTransitionalFloorWithheld() {
	if c != nil {
		c.transitionalFloorWithheld.Add(1)
	}
}
