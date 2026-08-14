// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package fake — реализация порта блочного хранения [blockbackend.Backend] в памяти.
//
// # Зачем
//
// Отрицательные пути системы — исчерпание ёмкости, недоступность бэкенда, отказ в
// правах, конфликт расходящегося повтора — нельзя проверить на живом хранилище: их там
// не заказать. Дублёр с управляемой инъекцией отказа делает эти пути проверяемыми, а
// значит и существующими: путь, который никто никогда не исполнял, отличается от
// несуществующего только тем, что про него написано.
//
// # Дублёр НЕ снисходительнее настоящего
//
// Ввод, который отвергнет адаптер живого хранилища, здесь отвергается той же полосой
// [blockbackend.Outcome]. Дублёр, молча глотающий такой ввод, прячет ровно тот дефект,
// ради которого его подставляют: проба зеленеет, а на стенде тот же вызов отказывает.
//
// Держится это не обещанием в этом абзаце, а суитой: пакет
// blockbackend/contract прогоняет ОДИН набор случаев против любой реализации порта, и
// здешний прогон (fake_test.go) — тот же самый, что обязан пройти адаптер. Проверено
// инъекцией дефекта: наивная первая версия дублёра, принимавшая расходящийся повтор и
// не проверявшая ввод, дала 26 красных случаев из 47.
//
// # Чего дублёр НЕ моделирует — и почему это не пробел
//
//   - Корзину отложенного удаления ([blockbackend.Capabilities.TrashTTLSeconds]).
//     Порт её не наблюдает ни одним глаголом, поэтому смоделированное поведение
//     нельзя было бы утверждать ни одним случаем суиты — это была бы выдумка о
//     контракте, а не контракт.
//   - Энфорсмент QoS. Числа проверяются на осмысленность и не хранятся: прочитать их
//     обратно порт не даёт, а состояние, которого никто не видит, только притворяется
//     проверенным.
//   - Отображение тома на узле. Его нет и в порту — это работа узлового агента со
//     своей границей доверия.
//   - Способности onlineGrow / multiAttach / encryptionAtRest. Они решаются НАД
//     портом: у бэкенда нет глагола привязки, поэтому «растить только отвязанный» и
//     «второй привязки не бывает» здесь не о чем утверждать. Гейт, заведённый тут,
//     был бы строже настоящего — а дублёр, отвергающий то, что живое хранилище
//     принимает, посылает разбираться с дефектом, которого нет.
//
// # Потокобезопасность
//
// Все глаголы держат один мьютекс. Дублёр живёт в пробах гонок use-case и гоняется под
// -race: небезопасный по потокам, он красил бы их своим дефектом, и разбирать пришлось
// бы не тот код.
package fake

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/PRO-Robotech/kacho/services/storage/internal/blockbackend"
)

// Дублёр обязан оставаться реализацией порта: расхождение ловится компиляцией, а не
// прогоном, который до него мог бы и не дойти.
var _ blockbackend.Backend = (*Backend)(nil)

// defaultListLimit — предел страницы, когда вызывающий его не назвал.
//
// Ноль означает «не назвал» и законен; отрицательное — мусор и отвергается. Разница не
// косметическая: приняв отрицательный предел за умолчание, реализация превратила бы
// ошибку вызывающего в тихо работающий обход.
const defaultListLimit = 100

// objectKind — род объекта у бэкенда.
//
// Родов ДВА, а не три: том и образ с точки зрения хранилища — один и тот же обычный
// объект, и различает их только роль, которую им назначает control plane. Отсюда же
// выбор способности при засеве: источник-снимок спрашивает cloneFromSnapshot,
// источник-обычный объект — cloneFromImage.
type objectKind int

const (
	kindPlain objectKind = iota
	kindSnapshot
)

func (k objectKind) String() string {
	if k == kindSnapshot {
		return "snapshot"
	}
	return "volume"
}

// key — адрес объекта. Локатор входит в ключ целиком: пространство арендатора и есть
// единица изоляции, и объекты двух арендаторов с одинаковым именем — разные объекты.
type key struct{ pool, namespace, name string }

func refKey(r blockbackend.ObjectRef) key {
	return key{pool: r.Pool, namespace: r.Namespace, name: r.Name}
}

func locKey(loc blockbackend.Locator, name string) key {
	return key{pool: loc.Pool, namespace: loc.Namespace, name: name}
}

// object — то, что бэкенд знает об объекте.
type object struct {
	kind  objectKind
	size  int64
	state blockbackend.ObservedState

	// used хранится отдельно от признака его наличия: отсутствие сведений о
	// потреблении отличается от потребления, равного нулю, и выдавать первое за
	// второе значит врать о пустом томе.
	used    int64
	hasUsed bool

	// parent — адрес источника, от которого объект зависит. Заполняется, только когда
	// бэкенд объявил зависимость клона от родителя и засев не потребовал
	// независимости.
	parent    key
	hasParent bool

	// source — для снимка адрес тома, с которого он снят; для копии снимка адрес
	// снимка-источника. Нужен, чтобы повтор с ДРУГИМ источником был конфликтом, а не
	// молчаливым успехом на совпавшем имени.
	source    key
	hasSource bool

	// children — адреса зависимых клонов. Держатся у родителя, потому что вопрос
	// задаётся именно ему: «можно ли меня удалить».
	children map[key]struct{}
}

// Backend — дублёр порта блочного хранения.
type Backend struct {
	mu      sync.Mutex
	caps    blockbackend.Capabilities
	objects map[key]*object
	faults  map[string]fault
}

// New строит пустой дублёр с объявленными способностями.
//
// Способности задаются при сборке и потом не меняются: у настоящего адаптера они
// константы, и дублёр, позволяющий их переставить на живом экземпляре, разрешал бы
// пробам проверять посадку, которой у адаптера не бывает.
func New(caps blockbackend.Capabilities) *Backend {
	return &Backend{
		caps:    caps,
		objects: make(map[key]*object),
		faults:  make(map[string]fault),
	}
}

// Kind — вид бэкенда для журнала и счётчиков.
func (b *Backend) Kind() string { return "FAKE" }

// Capabilities — что дублёр объявил уметь.
func (b *Backend) Capabilities() blockbackend.Capabilities { return b.caps }

// ---------------------------------------------------------------- глаголы порта

// CreateVolume создаёт обычный объект.
func (b *Backend) CreateVolume(ctx context.Context, spec blockbackend.VolumeSpec) error {
	const verb = verbCreateVolume
	if err := b.begin(ctx, verb, spec.Ref.Name); err != nil {
		return err
	}
	if err := checkRef(verb, spec.Ref); err != nil {
		return err
	}
	if spec.SizeBytes <= 0 {
		return rejected(verb, spec.Ref.Name, "size_bytes must be positive")
	}
	if err := checkQoS(verb, spec.Ref.Name, spec.QoS); err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	k := refKey(spec.Ref)
	if have, ok := b.objects[k]; ok {
		// Повтор попадает в ТОТ ЖЕ объект — на этом стоит вся дисциплина
		// переисполнения. Расхождение аргументов при этом обязано отказать: молча
		// принятое, оно переписало бы размер уже созданного объекта.
		if have.kind != kindPlain {
			return conflict(verb, spec.Ref.Name, "name is taken by a "+have.kind.String())
		}
		if have.size != spec.SizeBytes {
			return conflict(verb, spec.Ref.Name,
				fmt.Sprintf("object exists with size %d, requested %d", have.size, spec.SizeBytes))
		}
		return nil
	}
	b.objects[k] = newObject(kindPlain, spec.SizeBytes)
	return nil
}

// DeleteVolume снимает обычный объект.
func (b *Backend) DeleteVolume(ctx context.Context, ref blockbackend.ObjectRef) error {
	const verb = verbDeleteVolume
	if err := b.begin(ctx, verb, ref.Name); err != nil {
		return err
	}
	if err := checkRef(verb, ref); err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	k := refKey(ref)
	have, ok := b.objects[k]
	if !ok {
		// Удаление утверждает «этого объекта нет» — и оно уже верно.
		return nil
	}
	if have.kind != kindPlain {
		// Перепутанный глагол обязан отказать, а не снести объект другого рода:
		// иначе ошибка вызывающего становится потерей данных, которую нечем заметить.
		return rejected(verb, ref.Name, "object is a snapshot; use DeleteSnapshot")
	}
	if err := b.checkNoDependents(verb, ref.Name, have); err != nil {
		return err
	}
	b.remove(k, have)
	return nil
}

// ResizeVolume увеличивает объект.
func (b *Backend) ResizeVolume(ctx context.Context, ref blockbackend.ObjectRef, sizeBytes int64) error {
	const verb = verbResizeVolume
	if err := b.begin(ctx, verb, ref.Name); err != nil {
		return err
	}
	if err := checkRef(verb, ref); err != nil {
		return err
	}
	if sizeBytes <= 0 {
		return rejected(verb, ref.Name, "size_bytes must be positive")
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	have, ok := b.objects[refKey(ref)]
	if !ok {
		return notFound(verb, ref.Name)
	}
	if have.kind != kindPlain {
		return rejected(verb, ref.Name, "object is a snapshot and cannot be resized")
	}
	if sizeBytes < have.size {
		// Уменьшение не запрашивается вызывающим и не поддерживается бэкендом.
		// Принять его молча значило бы отдать данные за пределами нового размера.
		return rejected(verb, ref.Name,
			fmt.Sprintf("size can only be increased: object is %d, requested %d", have.size, sizeBytes))
	}
	have.size = sizeBytes
	return nil
}

// CreateSnapshot снимает снимок с обычного объекта.
func (b *Backend) CreateSnapshot(ctx context.Context, volume, snapshot blockbackend.ObjectRef) error {
	const verb = verbCreateSnapshot
	if err := b.begin(ctx, verb, snapshot.Name); err != nil {
		return err
	}
	if err := checkRef(verb, volume); err != nil {
		return err
	}
	if err := checkRef(verb, snapshot); err != nil {
		return err
	}
	if !b.caps.Snapshots {
		// Способность, которой бэкенд не объявил, обязана отказывать и на самом
		// бэкенде. Проверка в use-case — первый рубеж, а не единственный.
		return rejected(verb, snapshot.Name, "backend does not support snapshots")
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	src, ok := b.objects[refKey(volume)]
	if !ok {
		return notFound(verb, volume.Name)
	}
	if src.kind != kindPlain {
		return rejected(verb, volume.Name, "source is a snapshot")
	}

	k := refKey(snapshot)
	if have, ok := b.objects[k]; ok {
		if have.kind != kindSnapshot {
			return conflict(verb, snapshot.Name, "name is taken by a volume")
		}
		if !have.hasSource || have.source != refKey(volume) {
			return conflict(verb, snapshot.Name, "snapshot exists for a different volume")
		}
		return nil
	}
	snap := newObject(kindSnapshot, src.size)
	snap.source, snap.hasSource = refKey(volume), true
	b.objects[k] = snap
	return nil
}

// DeleteSnapshot снимает снимок.
//
// Способностью снимков глагол НЕ гейтится намеренно: на бэкенде без снимков ни одного
// снимка не существует, и отказ превратил бы идемпотентный «его уже нет» в ошибку.
func (b *Backend) DeleteSnapshot(ctx context.Context, ref blockbackend.ObjectRef) error {
	const verb = verbDeleteSnapshot
	if err := b.begin(ctx, verb, ref.Name); err != nil {
		return err
	}
	if err := checkRef(verb, ref); err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	k := refKey(ref)
	have, ok := b.objects[k]
	if !ok {
		return nil
	}
	if have.kind != kindSnapshot {
		return rejected(verb, ref.Name, "object is a volume; use DeleteVolume")
	}
	if err := b.checkNoDependents(verb, ref.Name, have); err != nil {
		return err
	}
	b.remove(k, have)
	return nil
}

// CloneVolume засевает цель из источника.
func (b *Backend) CloneVolume(ctx context.Context, spec blockbackend.CloneSpec) error {
	const verb = verbCloneVolume
	if err := b.begin(ctx, verb, spec.Target.Name); err != nil {
		return err
	}
	if err := checkRef(verb, spec.Source); err != nil {
		return err
	}
	if err := checkRef(verb, spec.Target); err != nil {
		return err
	}
	if spec.SizeBytes <= 0 {
		return rejected(verb, spec.Target.Name, "size_bytes must be positive")
	}
	if err := checkQoS(verb, spec.Target.Name, spec.QoS); err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	srcKey := refKey(spec.Source)
	src, ok := b.objects[srcKey]
	if !ok {
		return notFound(verb, spec.Source.Name)
	}
	// Род источника выбирает способность: снимок спрашивает одну, обычный объект
	// (в роли образа) — другую.
	if src.kind == kindSnapshot {
		if !b.caps.CloneFromSnapshot {
			return rejected(verb, spec.Target.Name, "backend does not support cloning from a snapshot")
		}
	} else if !b.caps.CloneFromImage {
		return rejected(verb, spec.Target.Name, "backend does not support cloning from an image")
	}
	if spec.SizeBytes < src.size {
		return rejected(verb, spec.Target.Name,
			fmt.Sprintf("target size %d is smaller than source size %d", spec.SizeBytes, src.size))
	}

	// Зависимость от родителя объявляет бэкенд, а её снятие требует вызывающий:
	// оба условия обязаны сойтись, чтобы связь появилась.
	wantParent := b.caps.CloneKeepsParent && !spec.Detached

	tgtKey := refKey(spec.Target)
	if have, ok := b.objects[tgtKey]; ok {
		if have.kind != kindPlain {
			return conflict(verb, spec.Target.Name, "name is taken by a "+have.kind.String())
		}
		if have.size != spec.SizeBytes {
			return conflict(verb, spec.Target.Name,
				fmt.Sprintf("object exists with size %d, requested %d", have.size, spec.SizeBytes))
		}
		if have.hasParent != wantParent {
			return conflict(verb, spec.Target.Name, "object exists with a different parent dependency")
		}
		return nil
	}

	child := newObject(kindPlain, spec.SizeBytes)
	if wantParent {
		child.parent, child.hasParent = srcKey, true
		src.children[tgtKey] = struct{}{}
	}
	b.objects[tgtKey] = child
	return nil
}

// CopySnapshot переносит снимок в другой локатор.
func (b *Backend) CopySnapshot(ctx context.Context, source, target blockbackend.ObjectRef) error {
	const verb = verbCopySnapshot
	if err := b.begin(ctx, verb, target.Name); err != nil {
		return err
	}
	if err := checkRef(verb, source); err != nil {
		return err
	}
	if err := checkRef(verb, target); err != nil {
		return err
	}
	if !b.caps.Snapshots {
		return rejected(verb, target.Name, "backend does not support snapshots")
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	srcKey := refKey(source)
	src, ok := b.objects[srcKey]
	if !ok {
		return notFound(verb, source.Name)
	}
	if src.kind != kindSnapshot {
		return rejected(verb, source.Name, "source is not a snapshot")
	}

	k := refKey(target)
	if have, ok := b.objects[k]; ok {
		if have.kind != kindSnapshot {
			return conflict(verb, target.Name, "name is taken by a volume")
		}
		if have.hasSource && have.source != srcKey {
			return conflict(verb, target.Name, "target exists as a copy of another snapshot")
		}
		if have.size != src.size {
			return conflict(verb, target.Name,
				fmt.Sprintf("target exists with size %d, source is %d", have.size, src.size))
		}
		return nil
	}
	copied := newObject(kindSnapshot, src.size)
	copied.source, copied.hasSource = srcKey, true
	b.objects[k] = copied
	return nil
}

// MigrateVolume переносит объект в другой локатор, сохраняя данные.
func (b *Backend) MigrateVolume(ctx context.Context, ref blockbackend.ObjectRef, target blockbackend.Locator) error {
	const verb = verbMigrateVolume
	if err := b.begin(ctx, verb, ref.Name); err != nil {
		return err
	}
	if err := checkRef(verb, ref); err != nil {
		return err
	}
	if err := checkLocator(verb, ref.Name, target); err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	from, to := refKey(ref), locKey(target, ref.Name)
	have, ok := b.objects[from]
	if !ok {
		// Повтор после успешного переноса идёт по ПРЕЖНЕМУ адресу: исполнитель
		// операций переисполняет функцию, не зная, доехал ли перенос, и обязан
		// получить успех, а не «не найдено».
		if _, moved := b.objects[to]; moved {
			return nil
		}
		return notFound(verb, ref.Name)
	}
	if from == to {
		return nil
	}
	if _, taken := b.objects[to]; taken {
		return conflict(verb, ref.Name, "target locator already holds an object with this name")
	}

	delete(b.objects, from)
	b.objects[to] = have
	b.rekey(from, to, have)
	return nil
}

// Observe рассказывает об объекте.
func (b *Backend) Observe(ctx context.Context, ref blockbackend.ObjectRef) (blockbackend.Observed, error) {
	const verb = verbObserve

	// Неответ — это ObservedUnknown, и НИКОГДА ObservedAbsent: молчание бэкенда не
	// является утверждением об отсутствии объекта, а сверщик по такому ответу снёс бы
	// живую строку как утечку.
	if err := b.begin(ctx, verb, ref.Name); err != nil {
		return blockbackend.Observed{State: blockbackend.ObservedUnknown}, err
	}
	if err := checkRef(verb, ref); err != nil {
		return blockbackend.Observed{State: blockbackend.ObservedUnknown}, err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	have, ok := b.objects[refKey(ref)]
	if !ok {
		return blockbackend.Observed{State: blockbackend.ObservedAbsent}, nil
	}
	out := blockbackend.Observed{
		State:        have.state,
		SizeBytes:    have.size,
		UsedBytes:    have.used,
		HasUsedBytes: have.hasUsed,
	}
	if have.hasParent {
		out.Parent = have.parent.name
	}
	return out, nil
}

// ListObjects перечисляет имена объектов локатора для сверки.
func (b *Backend) ListObjects(ctx context.Context, loc blockbackend.Locator, cursor string, limit int) ([]string, string, error) {
	const verb = verbListObjects
	if err := b.begin(ctx, verb, ""); err != nil {
		// Отказ обязан быть пустым по данным: обрывок страницы рядом с ошибкой
		// вызывающий примет за полную картину, если проглядит вторую половину ответа.
		return nil, "", err
	}
	if err := checkLocator(verb, "", loc); err != nil {
		return nil, "", err
	}
	if limit < 0 {
		return nil, "", rejected(verb, "", fmt.Sprintf("limit %d is negative", limit))
	}
	if limit == 0 {
		limit = defaultListLimit
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	names := make([]string, 0, len(b.objects))
	for k := range b.objects {
		if k.pool == loc.Pool && k.namespace == loc.Namespace && k.name > cursor {
			names = append(names, k.name)
		}
	}
	slices.Sort(names)
	if len(names) > limit {
		page := names[:limit]
		return page, page[limit-1], nil
	}
	return names, "", nil
}

// ------------------------------------------------- управление состоянием (не порт)

// SetUsedBytes проставляет объекту потребление.
//
// Не глагол порта: наружу бэкенд потребление только РАССКАЗЫВАЕТ, а взяться ему в
// дублёре неоткуда. Без этой ручки сверщик дрейфа нечем проверить — расхождение
// «в БД одно, у бэкенда другое» надо чем-то создать.
func (b *Backend) SetUsedBytes(ref blockbackend.ObjectRef, used int64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	have, ok := b.objects[refKey(ref)]
	if !ok {
		return fmt.Errorf("fake: object %q is absent in %s/%s", ref.Name, ref.Pool, ref.Namespace)
	}
	have.used, have.hasUsed = used, true
	return nil
}

// SetObservedState проставляет объекту наблюдаемое состояние.
//
// Тем же порядком и по той же причине: неисправный объект у бэкенда заказать нельзя, а
// путь «бэкенд считает объект неисправным» обязан быть исполнен хотя бы раз.
func (b *Backend) SetObservedState(ref blockbackend.ObjectRef, state blockbackend.ObservedState) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	have, ok := b.objects[refKey(ref)]
	if !ok {
		return fmt.Errorf("fake: object %q is absent in %s/%s", ref.Name, ref.Pool, ref.Namespace)
	}
	have.state = state
	return nil
}

// ------------------------------------------------------------------ внутреннее

func newObject(kind objectKind, size int64) *object {
	return &object{
		kind:     kind,
		size:     size,
		state:    blockbackend.ObservedReady,
		children: make(map[key]struct{}),
	}
}

// checkNoDependents отказывает в снятии объекта, у которого есть живые зависимые клоны.
//
// Вопрос задаётся, только когда бэкенд объявил зависимость: не объявив её, он утверждает,
// что клон родителя не держит, — и конфликт означал бы, что реализация отслеживает
// зависимость, о которой не сказала. Вызывается под уже взятым мьютексом.
func (b *Backend) checkNoDependents(verb, name string, have *object) error {
	if !b.caps.CloneKeepsParent || len(have.children) == 0 {
		return nil
	}
	return conflict(verb, name, fmt.Sprintf("object has %d dependent clone(s)", len(have.children)))
}

// remove снимает объект и разрывает его связи. Под уже взятым мьютексом.
func (b *Backend) remove(k key, have *object) {
	if have.hasParent {
		if parent, ok := b.objects[have.parent]; ok {
			delete(parent.children, k)
		}
	}
	delete(b.objects, k)
}

// rekey чинит связи после переезда объекта: адреса — часть связи, и оставленные
// прежними, они указывали бы в пустоту, из-за чего родитель считался бы занятым
// клоном, которого по этому адресу больше нет. Под уже взятым мьютексом.
func (b *Backend) rekey(from, to key, have *object) {
	if have.hasParent {
		if parent, ok := b.objects[have.parent]; ok {
			delete(parent.children, from)
			parent.children[to] = struct{}{}
		}
	}
	for childKey := range have.children {
		if child, ok := b.objects[childKey]; ok {
			child.parent = to
		}
	}
}

// checkRef судит адрес объекта.
func checkRef(verb string, ref blockbackend.ObjectRef) error {
	if err := checkLocator(verb, ref.Name, ref.Locator); err != nil {
		return err
	}
	if ref.Name == "" {
		return rejected(verb, "", "object name is required")
	}
	return nil
}

// checkLocator судит локатор.
//
// Пустое пространство арендатора отвергается наравне с пустым пулом, и это не
// педантизм: без него все арендаторы класса делят одно пространство имён у бэкенда, а
// любая ошибка в правах на его стороне становится межарендной.
func checkLocator(verb, object string, loc blockbackend.Locator) error {
	if loc.Pool == "" {
		return rejected(verb, object, "locator pool is required")
	}
	if loc.Namespace == "" {
		return rejected(verb, object, "locator namespace is required: it is the tenant isolation unit")
	}
	return nil
}

// checkQoS судит числа QoS. Ключи обходятся в устойчивом порядке: иначе на одном и том
// же дурном вводе отказ называл бы разное поле от прогона к прогону.
func checkQoS(verb, object string, qos map[string]int64) error {
	for _, name := range slices.Sorted(maps.Keys(qos)) {
		if name == "" {
			return rejected(verb, object, "qos key must not be empty")
		}
		if qos[name] < 0 {
			return rejected(verb, object, fmt.Sprintf("qos %q is negative (%d)", name, qos[name]))
		}
	}
	return nil
}

func rejected(verb, object, why string) error {
	return blockbackend.Errorf(blockbackend.OutcomeRejected, verb, object, errors.New(why))
}

func notFound(verb, object string) error {
	return blockbackend.Errorf(blockbackend.OutcomeNotFound, verb, object, errors.New("object does not exist"))
}

func conflict(verb, object, why string) error {
	return blockbackend.Errorf(blockbackend.OutcomeConflict, verb, object, errors.New(why))
}
