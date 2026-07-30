// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package registry

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/protobuf/types/known/anypb"

	registryv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/registry/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
	regerrors "github.com/PRO-Robotech/kacho/services/registry/internal/errors"
)

// DeleteTag — async удаление тега/манифеста в zot (проекция read-only, source of
// truth = zot). Sync-часть: id-формат + непустые repo/tag. Async worker (с
// проброшенным principal) исполняет zot.DeleteTag; полл до done.
//
// Per-repo authz (v_delete, existence-hiding deny→NOT_FOUND) — В ХЕНДЛЕРЕ ДО вызова
// use-case: handler синхронно Check'ает и НЕ создаёт Operation на deny (иначе async-
// Operation с error раскрыл бы факт существования repo/тега).
func (u *UseCase) DeleteTag(ctx context.Context, registryID, repository, tag string) (*operations.Operation, error) {
	if err := u.assertWired(); err != nil {
		return nil, err
	}
	if err := ValidateRegistryID(registryID); err != nil {
		return nil, err
	}
	if repository == "" {
		return nil, failInvalidArg("repository is required")
	}
	if tag == "" {
		return nil, failInvalidArg("tag is required")
	}

	op, err := operations.NewFromContext(ctx,
		ids.PrefixOperationReg,
		fmt.Sprintf("Delete tag %s of %s/%s", tag, registryID, repository),
		&registryv1.DeleteTagMetadata{RegistryId: registryID, Repository: repository, Tag: tag},
	)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	principal := operations.PrincipalFromContext(ctx)
	if err := u.ops.CreateWithPrincipal(ctx, op, principal); err != nil {
		return nil, mapRepoErr(err)
	}

	operations.Run(ctx, u.ops, op.ID, func(workerCtx context.Context) (*anypb.Any, error) {
		// Worker-ctx детачнут — восстанавливаем principal (иначе downstream/peer-
		// вызовы уходят анонимно, authz_no_principal).
		wctx := operations.WithPrincipal(workerCtx, principal)
		if derr := u.zot.DeleteTag(wctx, registryID, repository, tag); derr != nil {
			return nil, mapRepoErr(derr)
		}
		if uerr := u.unregisterRepoIfEmpty(wctx, registryID, repository); uerr != nil {
			return nil, uerr
		}
		return emptyAny()
	})

	return &op, nil
}

// unregisterRepoIfEmpty — снимает authz-объект registry_repository:<reg>/<repo>
// ТОЛЬКО когда ресурс действительно прекратил существование: эфемерный repo не имеет
// СОБСТВЕННОЙ КОНФИГУРАЦИИ — он живёт ровно постольку, поскольку в движке лежат его
// теги, поэтому с последним тегом исчезает и он (висячий authz-объект не оставляем).
//
// Durable-признак существования эфемерного repo (строка регистрации, миграция 0022)
// снимается ТОЙ ЖЕ транзакцией, что и эмиссия unregister-интента (emitFGAIntent), —
// звать что-то отдельно отсюда не нужно и НЕЛЬЗЯ: разъехаться они не могут только пока
// пишутся вместе. После снятия предикат существования снова отвечает «нет», поэтому
// следующий push под этим именем гейтится правом «создавать репозитории в реестре» — тем
// же, что и первый, а не правом на объект, у которого больше нет ни одного tuple.
//
// Долговременный repo (строка наложения есть) пустоту ПЕРЕЖИВАЕТ: он остаётся
// читаемым поштучно и в перечислении, снять его может только DeleteRepository — и
// именно он эмитит unregister в одной транзакции с удалением строки. Снимать права у
// такого ресурса нельзя: владелец теряет доступ к живому repo, а имя, на которое
// потом кто-то запушит, зарегистрируется заново уже на другого субъекта.
//
// Порядок проверок — сначала остаток тегов (дешёвый признак «содержимое кончилось»),
// затем наличие строки наложения. Ошибка любого чтения → проброс: при НЕИЗВЕСТНОМ
// состоянии права не трогаем (worker ретраит идемпотентно — zot.DeleteTag no-op на
// уже-удалённый тег, повторный unregister-intent идемпотентен в iam).
//
// repoReg==nil (breakglass) либо cfg==nil (composition root не подал overlay-порт) →
// no-op: без наложения различить «ресурс исчез» и «ресурс жив и пуст» невозможно, а
// ошибиться в сторону снятия прав у живого ресурса нельзя.
func (u *UseCase) unregisterRepoIfEmpty(ctx context.Context, registryID, repository string) error {
	if u.repoReg == nil || u.cfg == nil {
		return nil
	}
	tags, _, err := u.zot.ListTags(ctx, TagListQuery{RegistryID: registryID, Repository: repository})
	if err != nil {
		return mapRepoErr(err)
	}
	if len(tags) > 0 {
		return nil // repo ещё непуст — authz-объект жив
	}
	switch _, cerr := u.cfg.GetConfig(ctx, registryID, repository); {
	case cerr == nil:
		return nil // durable: строка наложения жива ⇒ ресурс существует ⇒ права остаются
	case errors.Is(cerr, regerrors.ErrNotFound):
		// ephemeral: своей строки нет, содержимое кончилось ⇒ ресурса больше нет.
	default:
		return mapRepoErr(cerr)
	}
	intent := domain.UnregisterIntentForRepo(registryID, repository)
	if uerr := u.repoReg.UnregisterRepository(ctx, intent); uerr != nil {
		return mapRepoErr(uerr)
	}
	return nil
}

// TriggerGC — async garbage collection namespace в zot (Internal admin, :9091).
// Sync-часть: id-формат. Async worker (с principal) форсирует GC; реальная
// рекламация — native-scheduler zot, повторный вызов идемпотентен. Authz (admin-tier)
// энфорсит per-RPC interceptor на internal-листенере (internal НЕ освобождён).
func (u *UseCase) TriggerGC(ctx context.Context, registryID string) (*operations.Operation, error) {
	if err := u.assertWired(); err != nil {
		return nil, err
	}
	if err := ValidateRegistryID(registryID); err != nil {
		return nil, err
	}

	op, err := operations.NewFromContext(ctx,
		ids.PrefixOperationReg,
		fmt.Sprintf("Garbage-collect Registry %s", registryID),
		&registryv1.TriggerGarbageCollectionMetadata{RegistryId: registryID},
	)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	principal := operations.PrincipalFromContext(ctx)
	if err := u.ops.CreateWithPrincipal(ctx, op, principal); err != nil {
		return nil, mapRepoErr(err)
	}

	operations.Run(ctx, u.ops, op.ID, func(workerCtx context.Context) (*anypb.Any, error) {
		wctx := operations.WithPrincipal(workerCtx, principal)
		if gerr := u.zot.TriggerGC(wctx, registryID); gerr != nil {
			return nil, mapRepoErr(gerr)
		}
		return emptyAny()
	})

	return &op, nil
}
