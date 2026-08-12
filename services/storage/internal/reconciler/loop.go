// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package reconciler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/PRO-Robotech/kacho/services/storage/internal/blockbackend"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
)

// Opener открывает адаптер к названному бэкенду.
//
// Отдельный интерфейс, а не прямая сборка адаптера, ровно по одной причине: сверщик
// обязан проверяться без живого хранилища. Дублёр порта уже есть и гоняется той же
// контрактной суитой, что настоящий адаптер, поэтому подстановка не ослабляет пробу.
type Opener interface {
	Open(ctx context.Context, b Binding) (blockbackend.Backend, error)
}

// Counts — что прошло за один проход.
//
// Числа печатаются КАЖДЫЙ проход, включая нулевые. «Ноль расхождений» обязано быть
// отличимо от «ноль осмотренного»: контроль, который ни разу не отказал, потому что
// ни разу не отработал, выглядит исправным ровно до инцидента.
type Counts struct {
	Scanned   int
	Provision int
	Resize    int
	Remove    int
	Forget    int
	Confirm   int
	Vanished  int
	Waited    int
	Failed    int
	// Leaked — объекты у бэкенда, которым не соответствует ни одна наша строка.
	// Сверщик их НЕ удаляет: снос чужих данных по собственному выводу необратим.
	Leaked int
}

// Reconciler доводит намерение до плоскости данных и возвращает наблюдение обратно.
type Reconciler struct {
	store    *Store
	opener   Opener
	interval time.Duration
	batch    int
	timeout  time.Duration
	logger   *slog.Logger

	mu    sync.Mutex
	cache map[string]blockbackend.Backend
}

// Умолчания на случай неполной конфигурации. Они НЕ заменяют боевого стража старта:
// тот отказывает в старте, если ручки не заданы, — здесь лишь защита от нулевых
// значений, которые превратили бы вызов в мгновенный отказ, а обход в холостой.
const (
	defaultCallTimeout = 30 * time.Second
	defaultBatch       = 100
)

// Config — настройки сверщика.
type Config struct {
	Interval    time.Duration
	Batch       int
	CallTimeout time.Duration
	Logger      *slog.Logger
}

// New собирает сверщика.
func New(store *Store, opener Opener, cfg Config) *Reconciler {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	// Неположительный срок вызова даёт УЖЕ ИСТЁКШИЙ контекст: каждое обращение к
	// бэкенду отказывается мгновенно, сверщик исправно крутится и не двигает ничего.
	// Боевой страж старта такую конфигурацию не пропускает, но конструктор зовут и
	// пробы — а проба, молча получившая истёкший контекст, проверяет не то, что
	// написано в её заголовке.
	if cfg.CallTimeout <= 0 {
		cfg.CallTimeout = defaultCallTimeout
	}
	if cfg.Batch <= 0 {
		cfg.Batch = defaultBatch
	}
	return &Reconciler{
		store: store, opener: opener,
		interval: cfg.Interval, batch: cfg.Batch, timeout: cfg.CallTimeout,
		logger: cfg.Logger,
		cache:  map[string]blockbackend.Backend{},
	}
}

// Run крутит сверку до отмены контекста.
func (r *Reconciler) Run(ctx context.Context) {
	t := time.NewTicker(r.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c := r.Once(ctx)
			// Печатается КАЖДЫЙ проход, включая пустой: молчащий сверщик и
			// сверщик, которому нечего делать, иначе неотличимы.
			r.logger.InfoContext(ctx, "storage reconcile pass",
				"scanned", c.Scanned, "provision", c.Provision, "resize", c.Resize,
				"remove", c.Remove, "forget", c.Forget, "confirm", c.Confirm,
				"vanished", c.Vanished, "waited", c.Waited, "failed", c.Failed)
		}
	}
}

// Once делает один проход по всем видам ресурсов.
func (r *Reconciler) Once(ctx context.Context) Counts {
	var total Counts
	for _, kind := range AllKinds() {
		rows, err := r.store.Drifted(ctx, kind, r.batch)
		if err != nil {
			r.logger.ErrorContext(ctx, "storage reconcile: drift query failed",
				"kind", string(kind), "err", err)
			total.Failed++
			continue
		}
		total.Scanned += len(rows)
		for i := range rows {
			r.step(ctx, rows[i], &total)
		}
	}
	return total
}

// step разбирает одну строку: наблюдает, решает, действует.
func (r *Reconciler) step(ctx context.Context, row Row, c *Counts) {
	if row.BindingID == "" || row.Ref.Name == "" {
		// Строка без координаты неисполнима, и молчать об этом нельзя: она будет
		// попадать в каждый проход, ничего не двигая. Это НАСТРОЙКА либо дефект
		// записи, а не временный сбой.
		r.logger.ErrorContext(ctx, "storage reconcile: row has no backend coordinate",
			"kind", string(row.Kind), "id", row.ID)
		c.Failed++
		return
	}

	backend, err := r.backendFor(ctx, row.BindingID)
	if err != nil {
		r.logger.ErrorContext(ctx, "storage reconcile: backend unavailable",
			"kind", string(row.Kind), "id", row.ID, "err", err)
		c.Failed++
		return
	}

	callCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	observed, oerr := backend.Observe(callCtx, row.Ref)
	if oerr != nil {
		// Недоступность НЕ является утверждением об отсутствии объекта. Записываем
		// «не установлено» и ждём следующего прохода.
		observed = blockbackend.Observed{State: blockbackend.ObservedUnknown}
		_ = r.store.Observe(ctx, row.Kind, row.ID, observed)
		c.Waited++
		return
	}

	// Источник добирается ТОЛЬКО когда объект предстоит создать: на прочих путях он
	// не нужен, и платить за него на каждом проходе незачем.
	if observed.State == blockbackend.ObservedAbsent && row.Desired == domain.VolumeStatusCreating {
		src, serr := r.store.SourceObject(ctx, row.Kind, row.ID)
		if serr != nil {
			r.logger.ErrorContext(ctx, "storage reconcile: source lookup failed",
				"kind", string(row.Kind), "id", row.ID, "err", serr)
			c.Failed++
			return
		}
		row.SourceObject = src
		if row.Kind == KindSnapshot {
			fromSnapshot, ferr := r.store.SourceIsSnapshot(ctx, row.ID)
			if ferr != nil {
				r.logger.ErrorContext(ctx, "storage reconcile: source kind lookup failed",
					"kind", string(row.Kind), "id", row.ID, "err", ferr)
				c.Failed++
				return
			}
			row.SourceIsSnapshot = fromSnapshot
		}
	}

	item := Item{
		Kind: string(row.Kind), ID: row.ID,
		Desired: row.Desired, DesiredSizeBytes: row.SizeBytes,
		Ref: row.Ref, Observed: observed,
	}
	r.act(ctx, callCtx, backend, row, item, observed, c)
}

// act исполняет решение. Ошибка бэкенда переводится в причину из ЗАКРЫТОГО словаря;
// текст бэкенда наружу не выходит и остаётся в журнале оператора.
func (r *Reconciler) act(ctx, callCtx context.Context, backend blockbackend.Backend,
	row Row, item Item, observed blockbackend.Observed, c *Counts,
) {
	switch Decide(item) {
	case ActionWait:
		_ = r.store.Observe(ctx, row.Kind, row.ID, observed)
		c.Waited++

	case ActionProvision:
		err := r.provision(callCtx, backend, row)
		if err != nil {
			r.fail(ctx, row, err, c)
			return
		}
		if err := r.store.Confirm(ctx, row.Kind, row.ID, observed); err != nil {
			c.Failed++
			return
		}
		c.Provision++

	case ActionResize:
		if err := backend.ResizeVolume(callCtx, row.Ref, row.SizeBytes); err != nil {
			r.fail(ctx, row, err, c)
			return
		}
		c.Resize++

	case ActionRemove:
		if err := r.remove(callCtx, backend, row); err != nil {
			r.fail(ctx, row, err, c)
			return
		}
		c.Remove++

	case ActionForget:
		if err := r.store.Forget(ctx, row.Kind, row.ID); err != nil {
			c.Failed++
			return
		}
		c.Forget++

	case ActionConfirm:
		if err := r.store.Confirm(ctx, row.Kind, row.ID, observed); err != nil {
			c.Failed++
			return
		}
		c.Confirm++

	case ActionReportVanished:
		// Причина отдельная от отказа бэкенда: объект не отказался, он ПРОПАЛ
		// либо нездоров, и предпосылка ресурса перестала выполняться.
		if err := r.store.MarkError(ctx, row.Kind, row.ID, domain.ReasonPreconditionFailed, observed); err != nil {
			c.Failed++
			return
		}
		r.logger.WarnContext(ctx, "storage reconcile: object vanished under a live row",
			"kind", string(row.Kind), "id", row.ID, "object", row.Ref.Name)
		c.Vanished++
	}
}

// provision создаёт объект. Снимок снимается с объекта-источника, том и образ
// создаются либо клонируются — по наличию источника.
func (r *Reconciler) provision(ctx context.Context, backend blockbackend.Backend, row Row) error {
	if row.Kind == KindSnapshot {
		if row.SourceObject == "" {
			return blockbackend.Errorf(blockbackend.OutcomeRejected, "CreateSnapshot", row.Ref.Name, errNoSource)
		}
		src := row.Ref
		src.Name = row.SourceObject
		// Копия снимается с ДРУГОГО СНИМКА и, как правило, лежит в чужом локаторе:
		// перенос между зонами — весь смысл копии. Снятие же берёт том в том же
		// локаторе. Один глагол на оба случая ошибался бы ровно там, где цена выше.
		if row.SourceIsSnapshot {
			return backend.CopySnapshot(ctx, src, row.Ref)
		}
		return backend.CreateSnapshot(ctx, src, row.Ref)
	}
	if row.SourceObject != "" {
		src := row.Ref
		src.Name = row.SourceObject
		return backend.CloneVolume(ctx, blockbackend.CloneSpec{
			Source: src, Target: row.Ref, SizeBytes: row.SizeBytes,
			Detached: !backend.Capabilities().CloneKeepsParent,
		})
	}
	return backend.CreateVolume(ctx, blockbackend.VolumeSpec{Ref: row.Ref, SizeBytes: row.SizeBytes})
}

// remove снимает объект тем глаголом, который соответствует виду ресурса.
func (r *Reconciler) remove(ctx context.Context, backend blockbackend.Backend, row Row) error {
	if row.Kind == KindSnapshot {
		return backend.DeleteSnapshot(ctx, row.Ref)
	}
	return backend.DeleteVolume(ctx, row.Ref)
}

// fail записывает исход отказа. Повторяемый отказ НЕ метит ресурс ошибкой: он
// пройдёт сам, а ошибка, поставленная на время сетевой неполадки, читалась бы
// арендатором как приговор.
func (r *Reconciler) fail(ctx context.Context, row Row, err error, c *Counts) {
	outcome := blockbackend.OutcomeOf(err)
	c.Failed++
	r.logger.ErrorContext(ctx, "storage reconcile: backend call failed",
		"kind", string(row.Kind), "id", row.ID, "object", row.Ref.Name,
		"outcome", outcome.String(), "err", err)
	if !Terminal(outcome) {
		return
	}
	_ = r.store.MarkError(ctx, row.Kind, row.ID, ReasonFor(outcome),
		blockbackend.Observed{State: blockbackend.ObservedAbsent})
}

// backendFor открывает адаптер по ревизии привязки и запоминает его.
//
// Кэш безопасен потому, что строка ревизии НЕИЗМЕНЯЕМА: правка класса заводит новую
// ревизию, а не переписывает эту. Это прямое следствие append-only и одна из причин,
// по которым он выбран.
func (r *Reconciler) backendFor(ctx context.Context, bindingID string) (blockbackend.Backend, error) {
	r.mu.Lock()
	if b, ok := r.cache[bindingID]; ok {
		r.mu.Unlock()
		return b, nil
	}
	r.mu.Unlock()

	binding, err := r.store.Binding(ctx, bindingID)
	if err != nil {
		return nil, err
	}
	b, err := r.opener.Open(ctx, binding)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.cache[bindingID] = b
	r.mu.Unlock()
	return b, nil
}

// ScanLeaks — ВТОРАЯ ось сверки: объекты у бэкенда, которым не соответствует ни одна
// наша строка.
//
// Она не чинится автоматически НИКОГДА. Удаление чужих данных по собственному выводу
// необратимо, а причин расхождения больше одной: строка могла не доехать, объект мог
// принадлежать соседнему развёртыванию с тем же префиксом, наш собственный проход мог
// идти в момент создания. Сверщик считает и называет, снимает оператор.
func (r *Reconciler) ScanLeaks(ctx context.Context, backend blockbackend.Backend, loc blockbackend.Locator) (Counts, error) {
	var c Counts
	known, err := r.store.KnownObjects(ctx, loc)
	if err != nil {
		return c, err
	}
	cursor := ""
	for {
		callCtx, cancel := context.WithTimeout(ctx, r.timeout)
		names, next, lerr := backend.ListObjects(callCtx, loc, cursor, r.batch)
		cancel()
		if lerr != nil {
			return c, lerr
		}
		c.Scanned += len(names)
		for _, name := range names {
			if _, ok := known[name]; ok {
				continue
			}
			c.Leaked++
			r.logger.WarnContext(ctx, "storage reconcile: backend object without a row",
				"pool", loc.Pool, "namespace", loc.Namespace, "object", name)
		}
		if next == "" {
			break
		}
		cursor = next
	}
	// Печатается всегда, включая нулевой результат: «утечек нет» обязано быть
	// отличимо от «ничего не осмотрено».
	r.logger.InfoContext(ctx, "storage reconcile: leak scan",
		"pool", loc.Pool, "namespace", loc.Namespace, "scanned", c.Scanned, "leaked", c.Leaked)
	return c, nil
}
