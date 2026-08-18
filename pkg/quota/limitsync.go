// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package quota держит ОБЩИЕ части учёта потолков: то, что у всех владельцев
// одинаково, и ровно это.
//
// # Что здесь есть и чего здесь нет
//
// Число, из которого вычитают, остаётся в базе владельца ресурса — списание
// обязано идти в транзакции вставки, а распределённой транзакции в стеке нет.
// Поэтому здесь НЕТ ни таблицы учёта, ни списания, ни отказа.
//
// Здесь — синхронизатор величин: то, что тянет дельту у владельца величин и
// правит снимки в проекции. Он одинаков у всех владельцев by construction,
// потому что и таблица проекции, и правило старшинства областей у них одни и те
// же. Владелец подключается, передав имя своей схемы и клиента к соседу, и не
// пишет ни одного своего оператора.
//
// # Почему это не «ещё один слой», а устранение шестой копии
//
// Синхронизатор — предмет, которого не существовало вовсе: столбцы под него
// (`limit_revision`, `synced_at`) были заведены в пяти схемах, индекс под них
// создан, а писателя не было ни одного. Написанный пять раз порознь, он
// разошёлся бы на правиле старшинства — то есть там, где расхождение не видно
// ни одной из сторон: каждая копия по отдельности верна, а администратор
// получает разный эффект от одного и того же действия в разных доменах.
package quota

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// Scope — область, на которой величина назначена. Порядок старшинства —
// PROJECT > ACCOUNT > DEFAULT — выражен методом AtLeast, а не сравнением строк в
// месте применения: правило одно, и жить оно обязано в одном месте.
type Scope string

const (
	ScopeDefault Scope = "DEFAULT"
	ScopeAccount Scope = "ACCOUNT"
	ScopeProject Scope = "PROJECT"
)

// rank — старшинство области. Не экспортируется: наружу торчит только
// отношение, а не число, чтобы у порядка не завелось второго представления.
func (s Scope) rank() int {
	switch s {
	case ScopeProject:
		return 3
	case ScopeAccount:
		return 2
	case ScopeDefault:
		return 1
	default:
		return 0
	}
}

// Valid отвечает, знает ли платформа такую область.
//
// Неизвестная область — НЕ «примем как умолчание»: это ровно тот случай, когда
// сосед сказал то, чего мы не понимаем, и применить это значило бы записать в
// проекцию догадку. Классификация чужого ответа без корзины «прочее».
func (s Scope) Valid() bool { return s.rank() > 0 }

// AtLeast отвечает, не ниже ли эта область, чем other.
func (s Scope) AtLeast(other Scope) bool { return s.rank() >= other.rank() }

// Change — одно изменение величины, каким его видит владелец проекции.
//
// Нейтрально к транспорту НАМЕРЕННО: пакет не импортирует сгенерённые стабы,
// поэтому синхронизатор проверяем без соседа и без сети, а владелец волен
// приносить дельту чем угодно.
type Change struct {
	// Kind — вид ресурса точечным токеном платформы.
	Kind string
	// Scope / ScopeID — где величина назначена. У умолчания ScopeID пуст.
	Scope   Scope
	ScopeID string
	// Value — назначенная величина. На отзыве не применяется (см. Withdrawn).
	Value int64
	// Revision — глобально возрастающий номер изменения у владельца величин. Он
	// же курсор дельты и он же то, с чем сверяется снимок строки.
	Revision int64
	// Withdrawn — величина снята. Строка проекции убирается, и следующее
	// списание разрешит потолок заново, получив следующую по старшинству
	// область.
	Withdrawn bool
}

// Validate отвергает изменение, которое нельзя применить не догадываясь.
//
// Отвергается ИМЕННО ЗДЕСЬ, а не в SQL: оператор, получивший неизвестную
// область, тихо не совпал бы ни с одной строкой и отчитался бы успехом —
// «применено 0», неотличимое от «нечего применять».
func (c Change) Validate() error {
	switch {
	case c.Kind == "":
		return errors.New("kind: required")
	case !c.Scope.Valid():
		return fmt.Errorf("scope: unknown %q", string(c.Scope))
	case c.Scope != ScopeDefault && c.ScopeID == "":
		return fmt.Errorf("scope_id: required for scope %s", string(c.Scope))
	case c.Revision <= 0:
		return fmt.Errorf("revision: must be positive, got %d", c.Revision)
	case !c.Withdrawn && c.Value < 0:
		return fmt.Errorf("value: must not be negative, got %d", c.Value)
	}
	return nil
}

// Source — откуда владелец берёт дельту. Реализуется клиентом к владельцу
// величин.
type Source interface {
	// ListChangedSince отдаёт изменения с ревизией больше курсора и следующий
	// курсор. Следующий курсор возвращается ВСЕГДА, включая пустую страницу.
	ListChangedSince(ctx context.Context, cursor string, pageSize int32) ([]Change, string, error)
}

// Projection — проекция величин у владельца ресурса.
type Projection interface {
	// LoadCursor отдаёт курсор, на котором остановились. Пустая строка означает
	// «с начала времён» — ровно то, что нужно проекции, ни разу не тянувшей
	// дельту.
	LoadCursor(ctx context.Context) (string, error)
	// ApplyChange правит строки снимка, адресуя их по столбцам, и отдаёт число
	// затронутых строк.
	ApplyChange(ctx context.Context, ch Change) (int64, error)
	// SaveCursor двигает курсор и прибавляет число применённых строк к
	// накопительному счётчику. Зовётся ТОЛЬКО когда есть что записать.
	SaveCursor(ctx context.Context, cursor string, appliedRows int64) error
	// Heartbeat отмечает состоявшийся проход, ничего больше не меняя.
	//
	// Отдельный глагол, а не побочный эффект сохранения курсора: иначе «жив» и
	// «работал» слились бы в одно число, и различить остановившийся
	// синхронизатор от живого на неизменной конфигурации стало бы нечем —
	// оба применяют ноль строк.
	Heartbeat(ctx context.Context) error
}

// Config — настройки синхронизатора.
type Config struct {
	// Interval — как часто тянуть дельту. Ноль означает умолчание.
	Interval time.Duration
	// PageSize — размер страницы дельты. Ноль означает умолчание.
	PageSize int32
	// PullTimeout — срок одного прохода. Ноль означает умолчание.
	PullTimeout time.Duration
}

const (
	defaultInterval    = 30 * time.Second
	defaultPageSize    = 200
	defaultPullTimeout = 20 * time.Second
	// maxPagesPerRun ограничивает догоняющий проход, чтобы первый запуск на
	// длинной истории не держал один вызов бесконечно. Остаток догоняется
	// следующим проходом — курсор персистентен, поэтому работа не теряется.
	maxPagesPerRun = 50
)

func (c Config) withDefaults() Config {
	if c.Interval <= 0 {
		c.Interval = defaultInterval
	}
	if c.PageSize <= 0 {
		c.PageSize = defaultPageSize
	}
	if c.PullTimeout <= 0 {
		c.PullTimeout = defaultPullTimeout
	}
	return c
}

// Recorder — сток наблюдаемости тянущего.
//
// Интерфейс, а не Prometheus: фундамент остаётся лёгким, а конкретный реестр
// провязывает владелец в композиционном корне — ровно как у доставки исходящих.
// Дублёр для проб — `MemRecorder`.
//
// Спрашиваются ровно те три величины, которыми «тянущий мёртв» отличается от
// «величины не меняли»: сколько проходов состоялось, сколько строк снимка
// поправлено за всё время и КОГДА проход удался в последний раз. Первых двух по
// отдельности мало: на неизменной конфигурации живой тянущий правит ноль строк,
// поэтому нулевой счётчик строк сам по себе ни о чём не говорит, а вот нулевой
// счётчик ПРОХОДОВ говорит, и возраст последнего успеха говорит громче обоих.
type Recorder interface {
	// IncPulls — состоявшийся проход (в том числе не применивший ни строки).
	IncPulls(schema string)
	// IncPullFailures — проход, окончившийся отказом.
	IncPullFailures(schema string)
	// AddAppliedRows — сколько строк снимка поправлено.
	AddAppliedRows(schema string, rows float64)
	// SetLastSuccessUnix — момент последнего удавшегося прохода.
	SetLastSuccessUnix(schema string, unix float64)
}

// Syncer тянет дельту величин и правит проекцию.
type Syncer struct {
	src  Source
	proj Projection
	cfg  Config
	log  *slog.Logger

	// schema / rec — наблюдаемость. Схема здесь ЯРЛЫК величин: домены поднимают
	// тянущего каждый со своей, и без ярлыка пять рядов слились бы в один.
	schema string
	rec    Recorder

	// pullsTotal / rowsTotal — накопительные, ради того чтобы «ни одна строка не
	// синхронизирована за всё время» было ЗАМЕТНО. Механизм, не тронувший ни
	// одной строки за всю свою жизнь, — находка, а не тишина; без счётчика он
	// неотличим от исправно работающего на неизменной конфигурации.
	pullsTotal int64
	rowsTotal  int64
}

// NewSyncer собирает синхронизатор. Отказывает сразу, если подключать нечего:
// собранный без источника или без проекции, он был бы формой без содержания —
// исполнялся бы по расписанию и не делал ничего.
func NewSyncer(src Source, proj Projection, cfg Config, log *slog.Logger) (*Syncer, error) {
	if src == nil {
		return nil, errors.New("quota sync: source is required")
	}
	if proj == nil {
		return nil, errors.New("quota sync: projection is required")
	}
	if log == nil {
		log = slog.Default()
	}
	return &Syncer{src: src, proj: proj, cfg: cfg.withDefaults(), log: log}, nil
}

// WithObserver подключает сток наблюдаемости. Без него тянущий работает так же,
// но его состояние читается только из журнала и из накопительных столбцов
// курсора — то есть глазами, а не оповещением.
func (s *Syncer) WithObserver(schema string, rec Recorder) *Syncer {
	s.schema, s.rec = schema, rec
	return s
}

// observe отдаёт исход прохода стоку. Нулевой сток — законное состояние: сток
// подключает владелец, и тянущий без него обязан работать, а не падать.
func (s *Syncer) observe(rows int64, err error) {
	if s.rec == nil {
		return
	}
	if err != nil {
		s.rec.IncPullFailures(s.schema)
		return
	}
	s.rec.IncPulls(s.schema)
	s.rec.AddAppliedRows(s.schema, float64(rows))
	s.rec.SetLastSuccessUnix(s.schema, float64(time.Now().Unix()))
}

// RunOnceWithin делает догоняющий проход ПОД СОБСТВЕННЫМ сроком.
//
// Отдельный глагол, а не аргумент: бюджет прохода — свойство тянущего, и он
// объявлен в его настройках. Прежде срок применял только цикл, а догоняющий
// проход подъёма шёл сырым контекстом процесса — то есть неотвечающий сосед
// держал бы подъём столько, сколько живёт процесс.
func (s *Syncer) RunOnceWithin(ctx context.Context) (int64, error) {
	runCtx, cancel := context.WithTimeout(ctx, s.cfg.PullTimeout)
	defer cancel()
	return s.RunOnce(runCtx)
}

// RunOnce делает один догоняющий проход и отдаёт число затронутых строк снимка.
//
// Страницы тянутся, пока курсор двигается: владелец величин отдаёт дельту
// постранично, и остановка на первой странице означала бы, что администратор,
// поменявший больше страницы за раз, ждёт следующего тика на каждую страницу.
func (s *Syncer) RunOnce(ctx context.Context) (int64, error) {
	cursor, err := s.proj.LoadCursor(ctx)
	if err != nil {
		s.observe(0, err)
		return 0, fmt.Errorf("load cursor: %w", err)
	}

	var rows int64
	for page := 0; page < maxPagesPerRun; page++ {
		changes, next, err := s.src.ListChangedSince(ctx, cursor, s.cfg.PageSize)
		if err != nil {
			s.observe(0, err)
			return rows, fmt.Errorf("pull changes: %w", err)
		}

		var pageRows int64
		for _, ch := range changes {
			if err := ch.Validate(); err != nil {
				// Непонятое изменение НЕ пропускается молча и НЕ применяется
				// наугад: проход останавливается, курсор не двигается, и
				// следующий проход встретит то же самое. Иначе дельта уехала бы
				// вперёд, а строка осталась бы со старой величиной навсегда —
				// и снаружи это выглядело бы как исправная синхронизация.
				s.observe(0, err)
				return rows, fmt.Errorf("change %s@%s rev %d: %w",
					ch.Kind, string(ch.Scope), ch.Revision, err)
			}
			n, err := s.proj.ApplyChange(ctx, ch)
			if err != nil {
				s.observe(0, err)
				return rows, fmt.Errorf("apply %s@%s rev %d: %w",
					ch.Kind, string(ch.Scope), ch.Revision, err)
			}
			pageRows += n
		}

		rows += pageRows

		caughtUp := len(changes) == 0 || next == cursor

		// Курсор двигается ПОСЛЕ применения страницы и в том же проходе:
		// сохранённый раньше, он потерял бы изменения при отказе применения.
		// Повтор применения безопасен — оператор идемпотентен по ревизии.
		//
		// Пустая догоняющая страница курсор НЕ переписывает: запись, которой
		// нечего записать, стоила бы оператора на каждом тике и вдобавок
		// двигала бы счётчик работы там, где работы не было.
		if !caughtUp || len(changes) > 0 {
			if err := s.proj.SaveCursor(ctx, next, pageRows); err != nil {
				s.observe(0, err)
				return rows, fmt.Errorf("save cursor: %w", err)
			}
		}

		if caughtUp {
			break
		}
		cursor = next
	}

	// Проход состоялся — и это отмечается ВСЕГДА, включая проход, которому
	// нечего было применять. Иначе остановившийся синхронизатор выглядел бы
	// точно так же, как живой на неизменной конфигурации.
	if err := s.proj.Heartbeat(ctx); err != nil {
		s.observe(0, err)
		return rows, fmt.Errorf("heartbeat: %w", err)
	}

	s.pullsTotal++
	s.rowsTotal += rows
	s.observe(rows, nil)
	return rows, nil
}

// Run гоняет проходы до отмены контекста, ЖДАВ ТИК ПЕРЕД КАЖДЫМ, включая первый.
//
// # Почему ждёт, а не начинает с прохода
//
// Догоняющий проход подъёма принадлежит ВЫЗЫВАЮЩЕМУ (`StartLimitSyncer` делает
// его синхронно и называет исход). Начиная с прохода, цикл повторял бы тот же
// вопрос тому же соседу через десятки миллисекунд после первого — заведомо в том
// же состоянии сети. Наблюдалось на 12 подъёмах из 12: ровно два отказа подряд,
// у одного домена с интервалом 32 мс, и второй не добавлял к первому ничего,
// кроме второй строки в журнале.
//
// # Почему отказ не фатален и не тих
//
// Величина — не путь запроса. Недоступность соседа означает, что снимок какое-то
// время постоит со старой величиной; предел при этом продолжает действовать —
// место занимает триггер в writer-транзакции. Но и «молча продолжаем» не
// годится, поэтому каждый отказ называется, а состояние «ни одного удавшегося
// прохода ЗА ВСЮ ЖИЗНЬ» говорит о себе отдельной строкой: без неё мёртвый
// тянущий неотличим от живого на неизменной конфигурации.
func (s *Syncer) Run(ctx context.Context) {
	t := time.NewTicker(s.cfg.Interval)
	defer t.Stop()

	s.log.Info("quota limit syncer started",
		slog.Duration("interval", s.cfg.Interval),
		slog.Int("page_size", int(s.cfg.PageSize)))

	for {
		select {
		case <-ctx.Done():
			s.log.Info("quota limit syncer stopped",
				slog.Int64("rows_total", s.rowsTotal),
				slog.Int64("pulls_total", s.pullsTotal))
			return
		case <-t.C:
		}

		rows, err := s.RunOnceWithin(ctx)

		switch {
		case err != nil && s.pullsTotal == 0:
			// Отдельная строка, а не оттенок общей: «ни одного удавшегося прохода
			// за всю жизнь» — это НЕ отставший снимок, это неработающий механизм.
			// Пока обе беды писались одинаково, вторая читалась как первая.
			s.log.Error("quota limit sync has NEVER succeeded",
				slog.String("error", err.Error()),
				slog.String("schema", s.schema),
				slog.Int64("pulls_total", s.pullsTotal))
		case err != nil:
			s.log.Error("quota limit sync failed",
				slog.String("error", err.Error()),
				slog.Int64("rows_total", s.rowsTotal),
				slog.Int64("pulls_total", s.pullsTotal))
		case rows > 0:
			s.log.Info("quota limits synchronised",
				slog.Int64("rows", rows),
				slog.Int64("rows_total", s.rowsTotal))
		}
	}
}

// Stats отдаёт накопленное — для наблюдаемости и для проб.
func (s *Syncer) Stats() (pulls, rows int64) { return s.pullsTotal, s.rowsTotal }
