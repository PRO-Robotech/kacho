// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package quota — совещательная полоса учёта числа ресурсов и материализация
// строк учёта у владельца типа.
//
// Приёмка `docs/specs/sub-phase-quota-v2-materialised-usage-acceptance.md`
// (APPROVED, раунд 2): V2-1 (материализуется УЧЁТ, величина наследуется), V2-3
// («не сказано» = отказ), V2-4 (зеркало аккаунта), DoD S2 п.3 и п.5.
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
// листом, и обратный вызов замкнул бы цикл, запрещённый `polyrepo.md`. Вдобавок
// синхронный вызов из iam в семь владельцев на пути создания проекта сделал бы
// создание проекта заложником доступности каждого из них. Значит строку заводит
// сам владелец — и единственный момент, когда он о проекте узнаёт, это обращение
// к нему.
//
// # Что из этого следует для проекта, созданного ДО появления учёта
//
// Ничего особенного: он материализуется на первом же обращении, ничем не
// отличаясь от свежесозданного. Обратного заполнения миграцией не существует и
// существовать не может — перечня проектов у владельца типа нет by construction
// (проекты принадлежат iam). Цена выбора названа честно: самое первое обращение
// в незнакомом проекте делает один дополнительный вызов к соседу; дальше всё
// местное. Предикат пересмотра — доля обращений, попадающих в промах, перестала
// быть пренебрежимой (наблюдаемость V2-8).
package quota

import (
	"context"
	"errors"
	"fmt"

	"github.com/PRO-Robotech/kacho/pkg/peer"
	"github.com/PRO-Robotech/kacho/pkg/quota/quotaread"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
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
// Ревизии здесь нет, и это НЕ упущение, а факт контракта: `EffectiveLimit`
// владельца её не несёт (несёт `Limit`, которого резолв не отдаёт). Строка учёта
// заводится с ревизией 0 — значением, которого у настоящих ревизий не бывает
// (они монотонны от единицы), то есть «ревизия не названа резолвом» отличимо от
// любой настоящей. Синхронизатор (DoD S2 п.6) проставит настоящую; ему же
// принадлежит и решение, расширять ли `EffectiveLimit` полем ревизии — здесь оно
// не принимается за него.
type ResolvedLimit = quotaread.ResolvedLimit

// limitRevisionUnknown — ревизия строки, заведённой резолвом, который её не
// назвал. Настоящие ревизии монотонны от единицы, поэтому ноль не совпадает ни с
// одной и означает ровно «ещё не синхронизировано», а не «синхронизировано
// нулевой».
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
// ребром эта работа не обзаводится. Реализация обязана делить с этой проверкой
// один вызов и один кэш — иначе «нового вызова нет» перестанет быть правдой,
// оставшись правдой на бумаге.
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
//
// Третий шаг и есть то, чем «не сказано = отказ» держится: сосед, ответивший
// набором без этого вида, оставляет строку незаведённой, и второй вопрос
// отвечает тем же отказом — но уже терминально.
func (g *Guard) Admit(ctx context.Context, projectID, kind string) error {
	if g == nil {
		// Полоса не собрана (нет соседа, у которого спрашивать величины).
		//
		// Приёмник проверяется ЗДЕСЬ, а не у вызывающего, из-за ловушки
		// типизированного nil: `*Guard`, положенный в интерфейсный порт,
		// интерфейсу не равен nil, поэтому проверка `u.quota != nil` у
		// вызывающего истинна и вызов доходит сюда. Без этой ветки КАЖДЫЙ Create
		// на стенде без внутреннего адреса соседа падал бы паникой — то есть
		// «полоса не собрана» означало бы не «нет раннего отказа», а «сервис не
		// работает».
		//
		// nil означает «раннего отказа нет», а НЕ «предела нет»: место
		// по-прежнему занимает триггер в writer-транзакции, и исчерпание
		// приезжает отказом операции.
		return nil
	}
	err := g.ask(ctx, projectID, kind)
	if !errors.Is(err, repo.ErrQuotaNotProvisioned) {
		return err
	}
	if merr := g.materialise(ctx, projectID); merr != nil {
		return merr
	}
	return g.ask(ctx, projectID, kind)
}

// ask задаёт совещательный вопрос местной строке.
func (g *Guard) ask(ctx context.Context, projectID, kind string) error {
	rd, err := g.repo.Reader(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = rd.Close() }()
	return rd.Quotas().Admit(ctx, repo.QuotaCarrierProject, projectID, kind)
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
		return g.peerRefusal(err, "iam.project", projectID)
	}
	if accountID == "" {
		// Строка без зеркала аккаунта НЕВИДИМА аккаунтной дельте: изменение
		// аккаунтной области её не найдёт, и она проживёт со старой величиной,
		// а снаружи это неотличимо от исправной работы — дельта отчитается
		// успехом, просто не тронув её (V2-4). Схема отвергает пустое зеркало
		// ограничением; здесь отказ наступает раньше и называет предмет.
		return fmt.Errorf("%w: project %s carries no account", repo.ErrFailedPrecondition, projectID)
	}

	limits, err := g.resolver.Resolve(ctx, projectID, g.service)
	if err != nil {
		return g.peerRefusal(err, "iam.limit", projectID)
	}
	if len(limits) == 0 {
		// Владелец величин не назвал ни одного вида. Заводить нечего, и это НЕ
		// разрешение: вызывающий получит отказ «потолок не назван» вторым
		// вопросом — тем же, что и при частичном ответе. Отдельной ветки отказа
		// здесь нет намеренно, иначе исход зависел бы от того, промолчал сосед
		// про все виды или про один.
		return nil
	}

	// Строка учёта заводится ТОЛЬКО там, где её носитель — проект.
	//
	// Носитель берётся из ответа, а не ставится константой: каталог называет
	// носителем вложенного вида родительский РЕСУРС, и строка такого вида,
	// заведённая на проект, была бы неправдой дважды — носитель назван неверно,
	// и потребление её не наполнится никогда, потому что списание идёт по
	// настоящему носителю.
	//
	// Вложенные виды идут СВОИМ путём, а не в строки учёта проекта: у них есть
	// проектная ВЕЛИЧИНА («по скольку детей на родителя») и нет проектного
	// ПОТРЕБЛЕНИЯ — детей считают в каждом родителе отдельно. Строка учёта с
	// носителем `project` объявляла бы расход, которого никто не производит, и
	// показывала бы арендатору вечный ноль.
	rows := make([]kacho.QuotaRow, 0, len(limits))
	nested := make([]kacho.QuotaRow, 0, len(limits))
	for _, l := range limits {
		if l.Carrier != repo.QuotaCarrierProject {
			// Носитель — родительский тип: это проектный резолв вложенного вида.
			// `CarrierID` здесь ПРОЕКТ, а не родитель: величина разрешается на
			// проект, а раздаётся по родителям — их у проекта много, и назвать
			// одного значило бы выбрать за платформу.
			nested = append(nested, kacho.QuotaRow{
				CarrierType:   l.Carrier,
				CarrierID:     projectID,
				Kind:          l.Kind,
				Limit:         l.Value,
				SourceScope:   l.SourceScope,
				SourceScopeID: l.SourceScopeID,
				LimitRevision: limitRevisionUnknown,
				AccountID:     accountID,
			})
			continue
		}
		rows = append(rows, kacho.QuotaRow{
			CarrierType:   repo.QuotaCarrierProject,
			CarrierID:     projectID,
			Kind:          l.Kind,
			Limit:         l.Value,
			SourceScope:   l.SourceScope,
			SourceScopeID: l.SourceScopeID,
			LimitRevision: limitRevisionUnknown,
			AccountID:     accountID,
		})
	}
	if len(rows) == 0 && len(nested) == 0 {
		// Ответ был, но ни один вид не считается ни в проекте, ни в родителе.
		// Заводить нечего — и это не разрешение: второй вопрос полосы вернёт
		// «потолок не назван».
		return nil
	}

	w, err := g.repo.Writer(ctx)
	if err != nil {
		return err
	}
	defer w.Abort()
	if _, err := w.Quotas().Materialize(ctx, rows); err != nil {
		return err
	}
	// Вложенные — В ТОЙ ЖЕ транзакции. Порознь они разошлись бы ровно там, где
	// расхождение не видно: проект получил бы проектные пределы и остался без
	// вложенных, а снаружи это неотличимо от «вложенных у него и не должно быть».
	if _, err := w.Quotas().MaterializeNestedDefaults(ctx, nested); err != nil {
		return err
	}
	return w.Commit()
}

// peerRefusal переводит отказ соседа в нашу полосу — код и машинный признак
// берутся у носителя (`pkg/peer`), а не выписываются здесь.
//
// Проза соседа наружу не идёт: она может нести имя хоста и текст драйвера
// (`security.md` §Hardening #1).
func (g *Guard) peerRefusal(err error, resourceType, resourceID string) error {
	return peer.Classify(err).Status(
		peer.Ref{Service: "vpc", ResourceType: resourceType, ResourceID: resourceID},
		peer.Prose{
			Missing:     "resource count limits are not stated for project %s",
			State:       "resource count limits for project %s cannot be read",
			Unavailable: "resource count limits are temporarily unavailable",
		},
	)
}
