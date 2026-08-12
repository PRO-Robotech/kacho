// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package disktype — use-case ресурса DiskType.
//
// Публичный DiskTypeService.Get/List — read-only (sync). Admin CRUD —
// InternalDiskTypeService на :9091 (Create/Update/Delete/SetLifecycle СИНХРОННЫ,
// возвращают ресурс, не Operation: admin-справочник не нуждается в LRO). id —
// admin-assigned slug; zone_ids — cross-service ссылки на geo.Zone (без FK).
//
// Правка идёт ПО МАСКЕ: поле, которого маска не назвала, не пишется. Состояние
// обращения правкой не меняется вовсе — для него заведён отдельный глагол
// SetLifecycle, иначе полный PATCH при пустой маске возвращал бы выведенный класс в
// обращение правкой описания.
package disktype

import (
	"context"
	"fmt"
	"slices"

	"github.com/PRO-Robotech/kacho/pkg/validate"

	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	storageerr "github.com/PRO-Robotech/kacho/services/storage/internal/errors"
)

// Pagination — вход для List с cursor-пагинацией.
type Pagination struct {
	PageSize  int64
	PageToken string
}

// DiskTypeUpdate — резолвнутый набор ИЗМЕНЯЕМЫХ полей класса: nil-указатель
// означает «не менять», а не «обнулить». Пустая строка и пустой список — законные
// ЗНАЧЕНИЯ, поэтому «снять описание» и «не трогать описание» различимы.
//
// Набор объявлен ЗДЕСЬ, в use-case, а не в адаптере: порт принадлежит тому, кто им
// пользуется. Форма отвечает маске правки один в один — поле, названное маской,
// приезжает указателем, не названное остаётся nil, — поэтому дисциплина маски
// ложится на репозиторий без второй интерпретации «что считать незаданным».
type DiskTypeUpdate struct {
	Name            *string
	Description     *string
	ZoneIDs         *[]string
	PerformanceTier *domain.PerformanceTier
	Lifecycle       *domain.DiskTypeLifecycle
	MinSizeBytes    *int64
	MaxSizeBytes    *int64
	SizeStepBytes   *int64
}

// Repo — порт хранилища disk_types (публичный read + admin write). Update пишет
// ТОЛЬКО названные поля (см. DiskTypeUpdate): полная замена выражается тем же
// набором со всеми названными полями, а не вторым методом — два места, пишущие одну
// строку, расходятся, вопрос лишь когда.
type Repo interface {
	Get(ctx context.Context, id string) (*domain.DiskType, error)
	List(ctx context.Context, p Pagination) ([]*domain.DiskType, string, error)
	Insert(ctx context.Context, d *domain.DiskType) (*domain.DiskType, error)
	Update(ctx context.Context, id string, u DiskTypeUpdate) (*domain.DiskType, error)
	Delete(ctx context.Context, id string) error
}

// knownUpdateFields — ИЗМЕНЯЕМЫЕ поля класса (дисциплина update_mask). Имена — пути
// маски, то есть поля РЕСУРСА DiskType, а не запроса.
//
// Неизменяемых полей здесь нет намеренно: их отвергает switch в UpdateAdmin
// конвенционным текстом ДО этой сверки, иначе они получили бы общее «unknown field».
var knownUpdateFields = map[string]struct{}{
	"name":        {},
	"description": {},
	"zone_ids":    {},
	"tier":        {},
	"limits":      {},
}

// UseCase — бизнес-логика DiskType.
type UseCase struct {
	repo Repo
}

// New собирает UseCase для DiskType.
func New(repo Repo) *UseCase { return &UseCase{repo: repo} }

// Get возвращает DiskType по id (public read).
func (u *UseCase) Get(ctx context.Context, id string) (*domain.DiskType, error) {
	return u.repo.Get(ctx, id)
}

// List возвращает типы дисков (public read, cursor-пагинация; garbage page_size →
// InvalidArgument).
func (u *UseCase) List(ctx context.Context, p Pagination) ([]*domain.DiskType, string, error) {
	size, err := validate.PageSize("page_size", p.PageSize)
	if err != nil {
		return nil, "", err
	}
	p.PageSize = size
	return u.repo.List(ctx, p)
}

// CreateAdmin создаёт DiskType (Internal :9091, sync). Self-validating domain: пустой
// id / слишком длинный name → InvalidArgument ДО repo (иначе ” — валидный PK-slug).
func (u *UseCase) CreateAdmin(ctx context.Context, d *domain.DiskType) (*domain.DiskType, error) {
	// Умолчание состояния обращения проставляется ЗДЕСЬ — в одном названном месте
	// на краю, а не в домене: домен, угадывающий незаполненное поле, принял бы
	// опечатку админа за намерение и завёл класс принимающим новые тома.
	if d.Lifecycle == "" {
		d.Lifecycle = domain.LifecycleActive
	}
	if err := d.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %s", storageerr.ErrInvalidArg, err.Error())
	}
	return u.repo.Insert(ctx, d)
}

// UpdateAdmin меняет изменяемые поля DiskType ПО МАСКЕ (Internal :9091, sync).
//
// Дисциплина маски — общая для платформы (api-conventions.md): неизвестное поле →
// InvalidArgument; неизменяемое → конвенционный текст ДО проверки известных; пустая
// маска → полный PATCH изменяемых полей. Заведена она здесь не ради единообразия:
// без маски правка была полной заменой, и запрос, менявший одно имя, обнулял список
// зон — справочник, от которого зависит размещение данных, снимался со всех зон
// молча.
//
// Self-validating domain (парити с CreateAdmin): пустой id / over-long name →
// InvalidArgument ДО repo, а не только через DB-CHECK.
func (u *UseCase) UpdateAdmin(ctx context.Context, id string, mask []string,
	name, description string, zoneIDs []string,
	tier domain.PerformanceTier, limits domain.SizeLimits) (*domain.DiskType, error) {
	// Неизменяемое поле отвергается ДО сверки с множеством известных: известными
	// считаются только ИЗМЕНЯЕМЫЕ, поэтому UpdateMask ответил бы на `lifecycle`
	// общим «unknown field» — вызывающий не узнал бы ни что поле существует, ни
	// почему оно не принимается здесь.
	for _, p := range mask {
		switch p {
		case "id", "lifecycle", "capabilities":
			return nil, fmt.Errorf("%w: %s is immutable after DiskType.Create", storageerr.ErrInvalidArg, p)
		}
	}
	if err := validate.UpdateMask("update_mask", mask, knownUpdateFields); err != nil {
		return nil, err
	}
	upd, cand := resolveUpdate(id, mask, name, description, zoneIDs, tier, limits)
	if err := cand.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %s", storageerr.ErrInvalidArg, err.Error())
	}
	return u.repo.Update(ctx, id, upd)
}

// resolveUpdate раскладывает маску и тело запроса в набор изменений и кандидата для
// доменной проверки.
//
// Кандидат несёт ТОЛЬКО названные поля: судить надо то, что правка запишет, а не то,
// что оказалось в теле мимо маски. Иначе тело с мусором в неназванном поле давало бы
// отказ на правку, которая этого поля не касается.
func resolveUpdate(id string, mask []string, name, description string, zoneIDs []string,
	tier domain.PerformanceTier, limits domain.SizeLimits) (DiskTypeUpdate, domain.DiskType) {
	var upd DiskTypeUpdate
	cand := domain.DiskType{
		ID: id,
		// Состояние обращения этот путь не пишет вовсе (в наборе изменений его нет):
		// его меняет глагол SetLifecycle. Кандидату оно нужно только потому, что домен
		// требует названного состояния; здесь стоит значение, которое правка НЕ
		// записывает, — иначе проверка судила бы отсутствие того, чего запрос не касается.
		Lifecycle: domain.LifecycleActive,
	}
	named := func(field string) bool {
		// Пустая маска — полный PATCH изменяемых полей (единая дисциплина платформы).
		return len(mask) == 0 || slices.Contains(mask, field)
	}
	if named("name") {
		cand.Name = name
		upd.Name = &name
	}
	if named("description") {
		cand.Description = description
		upd.Description = &description
	}
	if named("zone_ids") {
		cand.ZoneIDs = zoneIDs
		upd.ZoneIDs = &zoneIDs
	}
	if named("tier") {
		cand.PerformanceTier = tier
		upd.PerformanceTier = &tier
	}
	if named("limits") {
		// Границы правятся сообщением целиком: у маски один путь `limits`, поэтому
		// «поменять только минимум» этим запросом невыразимо — и не должно быть.
		cand.Limits = limits
		upd.MinSizeBytes = &limits.MinSizeBytes
		upd.MaxSizeBytes = &limits.MaxSizeBytes
		upd.SizeStepBytes = &limits.SizeStepBytes
	}
	return upd, cand
}

// SetLifecycle переводит класс в другое состояние обращения (Internal :9091, sync).
//
// Отдельный глагол, а не поле правки: пустая маска у UpdateAdmin означает полную
// замену изменяемых полей, поэтому состояние в её теле возвращало бы выведенный класс
// в обращение правкой описания — молча и без заявленного намерения. Здесь намерение и
// есть весь запрос.
//
// Существующие тома вывод из обращения НЕ трогает: они продолжают читаться,
// обновляться и удаляться. Отказ получают только новые (domain.AcceptsNewVolumes), а
// удаление самого класса по-прежнему упирается в ссылающиеся тома (FK RESTRICT).
//
// Состояние судит домен, а не сравнение строк здесь: словарь и текст отказа
// принадлежат ему, и второе место, знающее список состояний, разошлось бы с первым.
func (u *UseCase) SetLifecycle(ctx context.Context, id string, lifecycle domain.DiskTypeLifecycle) (*domain.DiskType, error) {
	// Кандидат несёт ровно то, что этот глагол записывает: id адресует строку,
	// состояние — предмет запроса. Имя и границы в нём пусты не по недосмотру — их
	// правка не касается, и судить их значило бы отвечать отказом на чужое поле.
	cand := domain.DiskType{ID: id, Lifecycle: lifecycle}
	if err := cand.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %s", storageerr.ErrInvalidArg, err.Error())
	}
	return u.repo.Update(ctx, id, DiskTypeUpdate{Lifecycle: &lifecycle})
}

// DeleteAdmin удаляет DiskType (Internal :9091, sync).
func (u *UseCase) DeleteAdmin(ctx context.Context, id string) error {
	return u.repo.Delete(ctx, id)
}
