// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package quota — совещательная полоса учёта числа ресурсов и материализация
// строк учёта у владельца типа.
//
// Приёмка `docs/specs/sub-phase-quota-v2-materialised-usage-acceptance.md`
// (APPROVED, раунд 2): V2-1 (материализуется УЧЁТ, величина наследуется), V2-3
// («не сказано» = отказ), V2-4 (зеркало аккаунта), DoD S4 п.1.
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
//
// # Что этот пакет делает СВЕРХ образца vpc
//
// Он материализует ещё и ПРОЕКТНЫЙ РЕЗОЛВ ВЛОЖЕННЫХ видов — величину, из которой
// триггер берёт снимок, заводя строку учёта нового родителя. Без неё родитель
// заводился бы без строки учёта, и первый же ребёнок получал бы «потолок не
// назван» — громко, но неверно: потолок назван, просто не доехал.
package quota

import (
	"context"
	"errors"
	"fmt"

	"github.com/PRO-Robotech/kacho/pkg/quota/quotaread"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
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
// не бывает (они монотонны от единицы), то есть «ревизия не названа резолвом»
// отличимо от любой настоящей.
type ResolvedLimit = quotaread.ResolvedLimit

// limitRevisionUnknown — ревизия строки, заведённой резолвом, который её не
// назвал. Настоящие ревизии монотонны от единицы, поэтому ноль не совпадает ни
// с одной и означает ровно «ещё не синхронизировано».
const limitRevisionUnknown = 0

// LimitResolver — владелец величин. Старшинство областей (PROJECT > ACCOUNT >
// DEFAULT) разрешается У НЕГО и только у него: владелец типа не знает аккаунта
// проекта иначе как зеркалом и потому разрешить старшинство сам не может.
type LimitResolver interface {
	Resolve(ctx context.Context, scopeID, service string) ([]ResolvedLimit, error)
}

// AccountLocator — аккаунт проекта.
//
// Отдельный порт, а не поле в резолве: аккаунт приходит из УЖЕ существующего
// вызова к соседу на пути создания (проверка существования проекта), и новым
// ребром эта работа не обзаводится.
type AccountLocator interface {
	AccountOf(ctx context.Context, projectID string) (string, error)
}

// Repo — то, что нужно полосе от репозитория: чтение для совещательного вопроса
// и запись для материализации.
type Repo interface {
	Reader(ctx context.Context) (kacho.RepositoryReader, error)
	Writer(ctx context.Context) (kacho.RepositoryWriter, error)
}

// Guard — совещательная полоса с материализацией на промахе.
type Guard struct {
	repo     Repo
	resolver LimitResolver
	accounts AccountLocator
	// service — домен, чьи виды спрашиваются у владельца величин. Перечень видов
	// принадлежит ПЛАТФОРМЕ и приезжает ответом: владелец типа не держит своей
	// копии каталога, поэтому новый вид не требует правки этого пакета.
	service string
}

// NewGuard собирает полосу.
func NewGuard(r Repo, resolver LimitResolver, accounts AccountLocator, service string) *Guard {
	return &Guard{repo: r, resolver: resolver, accounts: accounts, service: service}
}

// Admit — ранний отказ по учёту, если места нет.
//
// nil означает «место было в момент вопроса», а не «место будет»: между этим
// ответом и вставкой помещается чужая запись. Решает атомарное списание триггера
// (ban #10), и приёмка прямо запрещает считать эту полосу решением (§7.4).
//
// Порядок ровно такой и он несущий:
//
//  1. спросить местную строку — исчерпание есть местный факт и обращения к
//     соседу не стоит;
//  2. промах («потолок не назван») → материализовать по всем видам домена;
//  3. спросить ещё раз — и если вид потолка так и не получил, это ОТКАЗ
//     `QUOTA_NOT_PROVISIONED`, а не разрешение.
func (g *Guard) Admit(ctx context.Context, projectID, kind string) error {
	if g == nil {
		// Полоса не собрана (нет соседа, у которого спрашивать величины).
		//
		// Приёмник проверяется ЗДЕСЬ, а не у вызывающего, из-за ловушки
		// типизированного nil: `*Guard`, положенный в интерфейсный порт,
		// интерфейсу не равен nil, поэтому проверка у вызывающего истинна и вызов
		// доходит сюда.
		//
		// nil означает «раннего отказа нет», а НЕ «предела нет»: место
		// по-прежнему занимает триггер в writer-транзакции.
		return nil
	}
	err := g.ask(ctx, kacho.QuotaCarrierProject, projectID, kind)
	if !errors.Is(err, kacho.ErrQuotaNotProvisioned) {
		return err
	}
	if merr := g.materialise(ctx, projectID); merr != nil {
		return merr
	}
	return g.ask(ctx, kacho.QuotaCarrierProject, projectID, kind)
}

// AdmitCarrier — тот же вопрос, но про носителя-РОДИТЕЛЯ.
//
// Материализации на промахе здесь НЕТ намеренно: строку учёта родителя заводит
// та же транзакция, что и самого родителя (триггер жизненного цикла), поэтому
// её отсутствие означает не «мы ещё не спросили соседа», а «родитель заведён
// без резолва вложенного вида». Ответить на это заведением строки значило бы
// выдумать величину, которой платформа не называла.
func (g *Guard) AdmitCarrier(ctx context.Context, carrierType, carrierID, kind string) error {
	if g == nil {
		return nil
	}
	return g.ask(ctx, carrierType, carrierID, kind)
}

// ask задаёт совещательный вопрос местной строке.
func (g *Guard) ask(ctx context.Context, carrierType, carrierID, kind string) error {
	rd, err := g.repo.Reader(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = rd.Close() }()
	return rd.Quotas().Admit(ctx, carrierType, carrierID, kind)
}

// materialise заводит строки учёта по всем видам, которые назвал владелец
// величин.
//
// Fail-closed на каждом отказе соседа: пропустить мутацию, не установив предела,
// значит снять контроль ровно в тот момент, когда это труднее всего заметить —
// «пока сосед лежит, пределов нет» (`security.md` §Hardening п.8).
func (g *Guard) materialise(ctx context.Context, projectID string) error {
	accountID, err := g.accounts.AccountOf(ctx, projectID)
	if err != nil {
		return err
	}
	if accountID == "" {
		// Строка без зеркала аккаунта НЕВИДИМА аккаунтной дельте: изменение
		// аккаунтной области её не найдёт, и она проживёт со старой величиной, а
		// снаружи это неотличимо от исправной работы (V2-4). Схема отвергает
		// пустое зеркало ограничением; здесь отказ наступает раньше и называет
		// предмет.
		return fmt.Errorf("%w: project %s carries no account", kacho.ErrFailedPrecondition, projectID)
	}

	limits, err := g.resolver.Resolve(ctx, projectID, g.service)
	if err != nil {
		return err
	}
	if len(limits) == 0 {
		// Владелец величин не назвал ни одного вида. Заводить нечего, и это НЕ
		// разрешение: вызывающий получит отказ «потолок не назван» вторым
		// вопросом — тем же, что и при частичном ответе.
		return nil
	}

	// Плоские виды идут в учёт, вложенные — в проектный резолв: у вложенного
	// вида есть проектная ВЕЛИЧИНА и нет проектного ПОТРЕБЛЕНИЯ, поэтому строка
	// учёта на него завела бы `used`, которого никто не производит.
	flat := make([]kacho.QuotaRow, 0, len(limits))
	nested := make([]kacho.QuotaRow, 0, len(limits))
	for _, l := range limits {
		row := kacho.QuotaRow{
			CarrierType:   kacho.QuotaCarrierProject,
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

	w, err := g.repo.Writer(ctx)
	if err != nil {
		return err
	}
	defer w.Abort()
	if _, err := w.Quotas().Materialize(ctx, flat); err != nil {
		return err
	}
	if _, err := w.Quotas().MaterializeNestedDefaults(ctx, nested); err != nil {
		return err
	}
	return w.Commit()
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
	return carrier != "" && carrier != kacho.QuotaCarrierProject
}
