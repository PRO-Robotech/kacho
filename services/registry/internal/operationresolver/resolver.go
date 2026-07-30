// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package operationresolver — доменный разрешитель осиротевших операций kacho-registry.
//
// Движок живёт в corelib (pkg/operations.Reconciler): он сканирует таблицу
// операций по grace-окну и клеймит кандидатов под FOR UPDATE SKIP LOCKED.
// Доменная часть здесь: она знает типы метаданных registry и решает, каким
// терминалом закрыть строку, чей исполнитель ПРОВЕРЕННО мёртв (кандидатом строка
// становится, только пережив grace-окно, которое строго больше предела исполнения
// одной операции).
//
// Зачем. Строка операции коммитится ДО запуска работы, а живой исполнитель
// добирает только то, что диспетчеризовал сам этот процесс. Значит перекат пода,
// OOM, исчерпание бюджета терминальной записи и переполнение очереди исполнителя
// оставляли строку done=false НАВСЕГДА: клиент поллил её до конца своего терпения
// и не узнавал исхода ни разу.
//
// # Две полосы, и обе названы явно
//
//  1. Есть авторитетное чтение в СВОЕЙ БД — реестр (строка registries) и наложение
//     репозитория (строка repository_configs). Тогда исход выводится из
//     закоммиченной реальности: для создания/изменения «есть» → Done(текущий),
//     «нет» → Interrupted; для удаления наоборот.
//
//  2. Авторитетного чтения нет — действия над движком образов: удаление тега,
//     сборка мусора. Их результат живёт в zot, а не в нашей БД. Здесь резолвер НЕ
//     выдумывает успех и не молчит вечно: он закрывает строку Interrupted, чей
//     замороженный текст говорит ровно то, что произошло, — «операция прервана до
//     завершения». Это утверждение о РАБОТЕ, а не о побочном эффекте: тег мог быть
//     удалён, но исполнитель не дожил до записи результата, и клиенту важно, что
//     повторить придётся ему. Все действия этой полосы идемпотентны, повтор
//     безопасен. Молчать вместо этого («пропустить и повторить позже») означало бы
//     ту же вечную неизвестность, ради устранения которой резолвер и появился.
//
// Резолвер НЕ переигрывает работу. Он перестаёт врать о её состоянии.
package operationresolver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"google.golang.org/protobuf/types/known/anypb"

	registryv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/registry/v1"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
	regerrors "github.com/PRO-Robotech/kacho/services/registry/internal/errors"
)

// RegistryReader — узкий read-порт таблицы registries (regerrors.ErrNotFound → нет).
type RegistryReader interface {
	Get(ctx context.Context, id string) (*domain.Registry, error)
}

// RepositoryConfigReader — узкий read-порт наложения репозитория по натуральному
// ключу (registry_id, name).
type RepositoryConfigReader interface {
	GetConfig(ctx context.Context, registryID, name string) (*domain.RepositoryConfig, error)
}

// ProtoRegistry — проекция реестра на wire. Инжектируется композиционным корнем
// (та же функция, которой пользуются worker и handler), чтобы Response
// разрешённой операции был байт-в-байт тем же сообщением, что вернул бы Get.
type ProtoRegistry func(*domain.Registry) *registryv1.Registry

// Readers — набор портов, инжектируемый композиционным корнем.
type Readers struct {
	Registry   RegistryReader
	RepoConfig RepositoryConfigReader
	Proto      ProtoRegistry
}

// Resolver — доменный разрешитель registry.
type Resolver struct {
	r   Readers
	log *slog.Logger
}

// Option — функциональная опция Resolver.
type Option func(*Resolver)

// WithLogger подключает логгер (диагностика разбора).
func WithLogger(l *slog.Logger) Option {
	return func(r *Resolver) {
		if l != nil {
			r.log = l
		}
	}
}

// New конструирует Resolver.
func New(r Readers, opts ...Option) *Resolver {
	rs := &Resolver{r: r, log: slog.Default()}
	for _, o := range opts {
		o(rs)
	}
	return rs
}

// Resolve реализует operations.Resolver.
func (rs *Resolver) Resolve(ctx context.Context, op operations.Operation) (operations.ResolverResult, error) {
	if op.Metadata == nil {
		return skip(), nil
	}
	msg, err := op.Metadata.UnmarshalNew()
	if err != nil {
		rs.log.Warn("operation resolver: undecodable metadata, skipping orphan",
			"op", op.ID, "type_url", op.Metadata.TypeUrl, "err", err)
		return skip(), nil
	}

	switch m := msg.(type) {
	// ---- реестр: авторитетное чтение своей строки ----
	case *registryv1.CreateRegistryMetadata:
		return rs.resolveRegistry(ctx, false, m.GetRegistryId())
	case *registryv1.UpdateRegistryMetadata:
		return rs.resolveRegistry(ctx, false, m.GetRegistryId())
	case *registryv1.DeleteRegistryMetadata:
		return rs.resolveRegistry(ctx, true, m.GetRegistryId())

	// ---- наложение репозитория: авторитетное чтение своей строки ----
	case *registryv1.CreateRepositoryMetadata:
		return rs.resolveRepoConfig(ctx, false, m.GetRegistryId(), m.GetRepository())
	case *registryv1.UpdateRepositoryMetadata:
		return rs.resolveRepoConfig(ctx, false, m.GetRegistryId(), m.GetRepository())
	case *registryv1.DeleteRepositoryMetadata:
		return rs.resolveRepoConfig(ctx, true, m.GetRegistryId(), m.GetRepository())
	case *registryv1.RenameRepositoryMetadata:
		// Переименование завершено ⟺ наложение живёт под НОВЫМ именем.
		return rs.resolveRepoConfig(ctx, false, m.GetRegistryId(), m.GetNewName())

	// ---- действия над движком образов: авторитетного чтения нет ----
	case *registryv1.DeleteTagMetadata, *registryv1.TriggerGarbageCollectionMetadata:
		// См. полосу 2 в godoc пакета: утверждение о РАБОТЕ («прервана до
		// завершения»), а не о побочном эффекте. Обе операции идемпотентны, повтор
		// безопасен; вечное «в процессе» безопасным не было.
		return interrupted(), nil

	default:
		return skip(), nil
	}
}

// resolveRegistry — исход по существованию строки реестра.
func (rs *Resolver) resolveRegistry(ctx context.Context, isDelete bool, id string) (operations.ResolverResult, error) {
	if rs.r.Registry == nil {
		return skip(), nil
	}
	reg, err := rs.r.Registry.Get(ctx, id)
	switch {
	case err == nil:
	case errors.Is(err, regerrors.ErrNotFound):
		reg = nil
	default:
		return operations.ResolverResult{}, fmt.Errorf("operationresolver: get registry %q: %w", id, err)
	}
	if isDelete {
		if reg != nil {
			return interrupted(), nil
		}
		return done(nil), nil
	}
	if reg == nil {
		return interrupted(), nil
	}
	if rs.r.Proto == nil {
		// Проекция не провязана — исход известен, а тела ответа нет. Отдаём Done без
		// Response вместо выдуманного сообщения.
		return done(nil), nil
	}
	resp, merr := anypb.New(rs.r.Proto(reg))
	if merr != nil {
		return operations.ResolverResult{}, fmt.Errorf("operationresolver: marshal registry %q: %w", id, merr)
	}
	return done(resp), nil
}

// resolveRepoConfig — исход по существованию строки наложения репозитория.
// Response не заполняется: тело ответа этих операций собирается объединением
// наложения с проекцией движка, а проекция здесь недоступна. Терминал важнее тела —
// клиент узнаёт исход и перечитывает ресурс обычным Get.
func (rs *Resolver) resolveRepoConfig(ctx context.Context, isDelete bool, registryID, name string) (operations.ResolverResult, error) {
	if rs.r.RepoConfig == nil || registryID == "" || name == "" {
		return skip(), nil
	}
	cfg, err := rs.r.RepoConfig.GetConfig(ctx, registryID, name)
	switch {
	case err == nil:
	case errors.Is(err, regerrors.ErrNotFound):
		cfg = nil
	default:
		return operations.ResolverResult{}, fmt.Errorf(
			"operationresolver: get repository overlay %q/%q: %w", registryID, name, err)
	}
	present := cfg != nil
	if isDelete {
		if present {
			return interrupted(), nil
		}
		return done(nil), nil
	}
	if !present {
		return interrupted(), nil
	}
	return done(nil), nil
}

func skip() operations.ResolverResult {
	return operations.ResolverResult{Outcome: operations.OutcomeSkip}
}

func interrupted() operations.ResolverResult {
	return operations.ResolverResult{Outcome: operations.OutcomeInterrupted}
}

func done(resp *anypb.Any) operations.ResolverResult {
	return operations.ResolverResult{Outcome: operations.OutcomeDone, Response: resp}
}
