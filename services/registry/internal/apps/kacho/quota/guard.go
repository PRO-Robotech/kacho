// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package quota — совещательная полоса учёта числа ресурсов и материализация
// строк учёта у владельца типа.
//
// Приёмка `docs/specs/sub-phase-quota-v2-materialised-usage-acceptance.md`
// (APPROVED, раунд 2): V2-1, V2-3, V2-4, DoD S4 п.1.
//
// # Где этот пакет стоит в потоке
//
// Он стоит ПЕРЕД writer-транзакцией мутации и после неё не участвует ни в чём.
// Решение о списании принимает триггер базы в той же транзакции, что вставка
// строки ресурса; здесь — ранний и понятный отказ плюс заведение строк, без
// которых триггеру нечего списывать.
//
// # Почему материализация живёт у владельца типа, а не у владельца величин
//
// Ребро «владелец величин → владелец типа» завести нельзя: kaname остаётся
// листом, и обратный вызов замкнул бы цикл, запрещённый `polyrepo.md`. Значит
// строку заводит сам владелец — и единственный момент, когда он о проекте
// узнаёт, это обращение к нему.
package quota

import (
	"context"
	"errors"

	"github.com/PRO-Robotech/kacho/pkg/quota/quotaread"
	regerrors "github.com/PRO-Robotech/kacho/services/registry/internal/errors"
)

// ResolvedLimit — одна разрешённая величина, как её отдаёт владелец величин.
//
// ПСЕВДОНИМ, а не своя структура. Ровно та же тройка полей была объявлена в пяти
// доменах порознь: пять деклараций компилировались одинаково и разошлись бы при
// первом же новом поле — добавленном одному владельцу, а нужном всем. Общий
// экземпляр вдобавок делает клиента этого домена пригодным полосе чтения
// (`quotaread.Band`) БЕЗ переходника, а переходник — это ровно то место, где
// снисходительность к чужому ответу заводится незамеченной.
//
// Ревизии здесь нет, и это НЕ упущение, а факт контракта: резолв её не отдаёт.
// Строка учёта заводится с ревизией 0 — значением, которого у настоящих ревизий
// не бывает (они монотонны от единицы).
type ResolvedLimit = quotaread.ResolvedLimit

// limitRevisionUnknown — ревизия строки, заведённой резолвом, который её не
// назвал.
const limitRevisionUnknown = 0

// LimitResolver — владелец величин. Старшинство областей разрешается У НЕГО и
// только у него: владелец типа не знает аккаунта проекта иначе как зеркалом.
type LimitResolver interface {
	Resolve(ctx context.Context, scopeID, service string) ([]ResolvedLimit, error)
}

// AccountLocator — аккаунт проекта.
//
// Отдельный порт, а не поле в резолве: аккаунт приходит из УЖЕ существующего
// вызова к соседу на пути создания, и новым ребром эта работа не обзаводится.
type AccountLocator interface {
	AccountOf(ctx context.Context, projectID string) (string, error)
}

// Store — то, что нужно полосе от хранилища.
//
// Порт объявлен здесь, у вызывающего, а реализуется адаптером: use-case не
// импортирует pgx и не знает про схему.
type Store interface {
	// QuotaAdmit — совещательный вопрос: есть ли место, НЕ занимая его.
	QuotaAdmit(ctx context.Context, carrierType, carrierID, kind string) error
	// MaterializeQuotas заводит отсутствующие строки учёта.
	MaterializeQuotas(ctx context.Context, rows []Row) (int64, error)
	// MaterializeNestedDefaults заводит проектный резолв вложенных видов.
	MaterializeNestedDefaults(ctx context.Context, rows []Row) (int64, error)
	// ListStates отдаёт строки учёта носителя — то, что арендатор читает как свои
	// квоты.
	//
	// Пустой срез означает «строк учёта ещё нет», а НЕ «квот нет»: различать эти
	// два состояния обязан вызывающий, и он же обязан ответить арендатору полным
	// набором видов с нулевым потреблением.
	ListStates(ctx context.Context, carrierType, carrierID string) ([]quotaread.State, error)
}

// Row — одна строка учёта, какой её ЗАВОДИТ материализация.
//
// Потребления здесь нет НАМЕРЕННО: `used` принадлежит триггеру и никакому
// другому писателю.
type Row struct {
	CarrierType   string
	CarrierID     string
	Kind          string
	Limit         int64
	SourceScope   string
	SourceScopeID string
	LimitRevision int64
	AccountID     string
}

// carrierProject — носитель учёта «проект».
const carrierProject = "project"

// Guard — совещательная полоса с материализацией на промахе.
type Guard struct {
	store    Store
	resolver LimitResolver
	accounts AccountLocator
	// service — домен, чьи виды спрашиваются у владельца величин. Перечень видов
	// принадлежит ПЛАТФОРМЕ и приезжает ответом: владелец типа не держит своей
	// копии каталога.
	service string
}

// NewGuard собирает полосу.
func NewGuard(s Store, resolver LimitResolver, accounts AccountLocator, service string) *Guard {
	return &Guard{store: s, resolver: resolver, accounts: accounts, service: service}
}

// Admit — ранний отказ по учёту, если места нет.
//
// nil означает «место было в момент вопроса», а не «место будет»: между этим
// ответом и вставкой помещается чужая запись. Решает атомарное списание триггера
// (ban #10), и приёмка прямо запрещает считать эту полосу решением (§7.4).
func (g *Guard) Admit(ctx context.Context, projectID, kind string) error {
	if g == nil {
		// Полоса не собрана. Проверка стоит ЗДЕСЬ из-за ловушки типизированного
		// nil: `*Guard`, положенный в интерфейсный порт, интерфейсу не равен nil.
		//
		// nil означает «раннего отказа нет», а НЕ «предела нет».
		return nil
	}
	err := g.store.QuotaAdmit(ctx, carrierProject, projectID, kind)
	if !errors.Is(err, regerrors.ErrQuotaNotProvisioned) {
		return err
	}
	if merr := g.materialise(ctx, projectID); merr != nil {
		return merr
	}
	return g.store.QuotaAdmit(ctx, carrierProject, projectID, kind)
}

// AdmitCarrier — тот же вопрос, но про носителя-РОДИТЕЛЯ.
//
// Материализации на промахе здесь НЕТ намеренно: строку учёта родителя заводит
// та же транзакция, что и самого родителя (триггер жизненного цикла), поэтому её
// отсутствие означает не «мы ещё не спросили соседа», а «родитель заведён без
// резолва вложенного вида». Ответить на это заведением строки значило бы
// выдумать величину, которой платформа не называла.
func (g *Guard) AdmitCarrier(ctx context.Context, carrierType, carrierID, kind string) error {
	if g == nil {
		return nil
	}
	return g.store.QuotaAdmit(ctx, carrierType, carrierID, kind)
}

// materialise заводит строки учёта по всем видам, которые назвал владелец
// величин.
//
// Fail-closed на каждом отказе соседа: пропустить мутацию, не установив предела,
// значит снять контроль ровно в тот момент, когда это труднее всего заметить
// (`security.md` §Hardening п.8).
func (g *Guard) materialise(ctx context.Context, projectID string) error {
	accountID, err := g.accounts.AccountOf(ctx, projectID)
	if err != nil {
		return err
	}

	limits, err := g.resolver.Resolve(ctx, projectID, g.service)
	if err != nil {
		return err
	}
	if len(limits) == 0 {
		// Владелец величин не назвал ни одного вида. Заводить нечего, и это НЕ
		// разрешение: вызывающий получит отказ «потолок не назван» вторым вопросом.
		return nil
	}

	// Плоские виды идут в учёт, вложенные — в проектный резолв: у вложенного вида
	// есть проектная ВЕЛИЧИНА и нет проектного ПОТРЕБЛЕНИЯ, поэтому строка учёта
	// на него завела бы `used`, которого никто не производит.
	flat := make([]Row, 0, len(limits))
	nested := make([]Row, 0, len(limits))
	for _, l := range limits {
		row := Row{
			CarrierType:   carrierProject,
			CarrierID:     projectID,
			Kind:          l.Kind,
			Limit:         l.Value,
			SourceScope:   l.SourceScope,
			SourceScopeID: l.SourceScopeID,
			LimitRevision: limitRevisionUnknown,
			AccountID:     accountID,
		}
		if countedInAParent(l.Carrier) {
			nested = append(nested, row)
			continue
		}
		flat = append(flat, row)
	}

	if _, err := g.store.MaterializeQuotas(ctx, flat); err != nil {
		return err
	}
	_, err = g.store.MaterializeNestedDefaults(ctx, nested)
	return err
}

// countedInAParent — вид считается в ОДНОМ родителе, а не в корне аренды.
//
// Признак — НОСИТЕЛЬ, названный владельцем величин, а не форма токена. Прежде
// здесь считались точки: три части читались как «вложенный». Совпадение это
// давало верное, но опиралось на имя, а не на факт каталога, — а форма носителя
// не определяет: `iam.project` двухчастен и считается в АККАУНТЕ. Своей копии
// каталога здесь по-прежнему нет и быть не должно; изменилось лишь то, что
// носитель теперь приезжает вместе с величиной, а не восстанавливается по виду.
//
// Пустой носитель означает, что сосед не назвал его вовсе: такой вид не
// раскладывается наугад в проектную полосу, а пропускается — строка под неверным
// носителем не отказывает громко, её потребление просто не наполняется никогда.
func countedInAParent(carrier string) bool {
	return carrier != "" && carrier != carrierProject
}
