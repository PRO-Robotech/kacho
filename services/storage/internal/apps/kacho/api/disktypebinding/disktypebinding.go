// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package disktypebinding — use-case ревизии привязки класса диска к бэкенду.
//
// # Почему у ресурса нет НИ правки, НИ вывода из обращения отдельным глаголом
//
// Ревизия НЕИЗМЕНЯЕМА, и отсутствие правки — это и есть механизм, ради которого
// ресурс заведён. Ресурс ссылается на ревизию, под которой создан; правка класса
// заводит НОВУЮ ревизию и вытесняет прежнюю, а прежняя живёт, пока на неё
// ссылаются. Поэтому изменение справочника физически не может задним числом
// изменить свойства уже созданного тома — а до этого могло, и заметить было неоткуда:
// правка каталога синхронна, следа не оставляет и операции не порождает.
//
// Добавь сюда Update — и весь механизм исчезнет, оставив вместо себя название.
//
// Вывод ревизии из обращения тоже НЕ отдельная операция. Прежнюю вытесняет сама
// регистрация следующей, а «перестать предлагать класс» выражается там, где это
// свойство и живёт: состоянием обращения КЛАССА либо состоянием БЭКЕНДА. Отдельный
// глагол понадобился бы ровно для одного — снять предложение класса в ОДНОЙ зоне, не
// трогая прочие; такой операции сегодня никто не просил, и заводить ради неё
// мутирующий путь значило бы разменять несущий механизм на возможность впрок.
//
// # Почему список кластерный
//
// Ревизия не принадлежит арендатору: это отображение продуктового класса на чужое
// хранилище. Пообъектного сужения страницы у неё нет, потому что сужать не к чему.
// Ресурс живёт только на внутреннем листенере и гейтится системным админским
// отношением, которое подстановочной выдачей не выполнимо.
package disktypebinding

import (
	"context"
	"fmt"

	"github.com/PRO-Robotech/kacho/pkg/validate"

	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	storageerr "github.com/PRO-Robotech/kacho/services/storage/internal/errors"
)

// Repo — порт хранилища ревизий привязки.
//
// Метода правки здесь нет НАМЕРЕННО (см. шапку пакета). Register вставляет новую
// ревизию и вытесняет прежнюю ОДНИМ стейтментом: «прочитал действующую → пометил →
// вставил» пропустил бы обе конкурентные регистрации.
type Repo interface {
	Get(ctx context.Context, id string) (*domain.DiskTypeBinding, error)
	List(ctx context.Context, pageSize int64, pageToken string) ([]*domain.DiskTypeBinding, string, error)
	Register(ctx context.Context, b *domain.DiskTypeBinding) (*domain.DiskTypeBinding, error)
}

// BackendReader — порт чтения бэкенда, на который ссылается ревизия.
type BackendReader interface {
	Get(ctx context.Context, id string) (*domain.StorageBackend, error)
}

// UseCase — бизнес-логика ревизии привязки.
type UseCase struct {
	repo     Repo
	backends BackendReader
}

// New собирает UseCase.
func New(repo Repo, backends BackendReader) *UseCase {
	return &UseCase{repo: repo, backends: backends}
}

// Get возвращает ревизию по id.
func (u *UseCase) Get(ctx context.Context, id string) (*domain.DiskTypeBinding, error) {
	return u.repo.Get(ctx, id)
}

// List возвращает страницу ревизий. Пообъектного сужения нет: сужать не к чему —
// у ревизии нет владельца-арендатора. Проверка размера страницы стоит первой.
func (u *UseCase) List(ctx context.Context, pageSize int64, pageToken string) ([]*domain.DiskTypeBinding, string, error) {
	size, err := validate.PageSize("page_size", pageSize)
	if err != nil {
		return nil, "", err
	}
	return u.repo.List(ctx, size, pageToken)
}

// Register заводит НОВУЮ ревизию на пару (класс, зона) и вытесняет прежнюю.
//
// Бэкенд обязан принимать новые привязки: выводимый из эксплуатации не должен
// получать новых ресурсов, иначе вывод не заканчивается никогда. Проверка идёт до
// записи, а окончательное решение принимает единственный стейтмент регистрации —
// здесь она нужна ради ВНЯТНОГО отказа, а не как замена ему.
func (u *UseCase) Register(ctx context.Context, b *domain.DiskTypeBinding) (*domain.DiskTypeBinding, error) {
	if err := b.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %s", storageerr.ErrInvalidArg, err.Error())
	}
	backend, err := u.backends.Get(ctx, b.BackendID)
	if err != nil {
		return nil, err
	}
	if !backend.AcceptsNewBindings() {
		return nil, fmt.Errorf("%w: StorageBackend %s is not accepting new bindings",
			storageerr.ErrFailedPrecondition, b.BackendID)
	}
	return u.repo.Register(ctx, b)
}
