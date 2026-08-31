// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package registry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"google.golang.org/protobuf/types/known/anypb"

	registryv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/registry/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
	regerrors "github.com/PRO-Robotech/kacho/services/registry/internal/errors"
)

// RenameRepository — async переименование в пределах ОДНОГО реестра (new_name — голое
// repo-имя; cross-registry rename структурно невыразим, D-5). Sync-часть: format-
// валидация registry_id/repository + new_name (malformed → INVALID_ARGUMENT, no-op
// new_name==repository → "new name must differ from current name", A19, ДО Operation)
// плюс полоса подтверждения (assertRenameConfirmed ниже: доказанные потребители без
// confirm_current_name → FAILED_PRECONDITION с величиной; негодное подтверждение →
// INVALID_ARGUMENT — тоже ДО Operation, поэтому отказ следа не оставляет).
// per-repo v_update@old + v_create@registry Check (deny|absent → NOT_FOUND) — в handler'е.
//
// Async worker: (1) класс источника (GetConfig old — durable vs ephemeral); (2) pre-check
// коллизии целевого имени (overlay ИЛИ проекция занята → ALREADY_EXISTS, A17);
// (3) engine re-home тегов/манифестов/referrers old→new — движок недоступен в середине
// → UNAVAILABLE fail-closed (без частичного rename, старое имя резолвится, A21);
// (4) durable → RekeyConfig (re-key UPDATE, A16) | ephemeral → InsertConfig под new_name
// (auto-promote → durable, A23) — одностейтментная запись под PK-backstop (A18); FGA
// re-register new / unregister old + public-grant governance в той же tx.
func (u *UseCase) RenameRepository(ctx context.Context, registryID, repository, newName, confirmCurrentName string) (*operations.Operation, error) {
	if err := u.assertRepoWired(); err != nil {
		return nil, err
	}
	if err := ValidateRegistryID(registryID); err != nil {
		return nil, err
	}
	if err := domain.ValidateRepositoryName("repository", repository); err != nil {
		return nil, failInvalidArg("%s", err.Error())
	}
	if err := domain.ValidateRepositoryName("new_name", newName); err != nil {
		return nil, failInvalidArg("%s", err.Error())
	}
	if newName == repository {
		return nil, failInvalidArg("new name must differ from current name")
	}
	if err := u.assertRenameConfirmed(ctx, registryID, repository, confirmCurrentName); err != nil {
		return nil, err
	}

	principal := operations.PrincipalFromContext(ctx)
	op, err := operations.NewFromContext(ctx,
		ids.PrefixOperationReg,
		fmt.Sprintf("Rename Repository %s/%s → %s", registryID, repository, newName),
		&registryv1.RenameRepositoryMetadata{RegistryId: registryID, Repository: repository, NewName: newName},
	)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	if err := u.ops.CreateWithPrincipal(ctx, op, principal); err != nil {
		return nil, mapRepoErr(err)
	}

	operations.Run(ctx, u.ops, op.ID, func(workerCtx context.Context) (*anypb.Any, error) {
		wctx := operations.WithPrincipal(workerCtx, principal)
		renamed, rerr := u.doRename(wctx, registryID, repository, newName, principal)
		if rerr != nil {
			return nil, rerr
		}
		return u.repositoryAny(renamed)
	})

	return &op, nil
}

// assertRenameConfirmed — полоса подтверждения (#1644).
//
// Переименование — ЕДИНСТВЕННЫЙ глагол платформы, меняющий внешне-адресуемую
// координату: после него `$домен/$registryID/$repository:$тег` отвечает 404 без
// редиректа, алиаса и переходного окна. Обнаруживается это не в момент вызова, а при
// следующей выкатке — и, как правило, у чужой команды. Поэтому у репозитория с
// ДОКАЗАННЫМИ потребителями вызов без подтверждения отвергается синхронно, а отказ
// называет величину и следующий шаг.
//
// Порядок веток выбран так, чтобы поле читалось ВСЕГДА. Сперва — согласие
// подтверждения с предметом (негодное подтверждение отвергается независимо от
// потребителей: иначе на безопасной полосе поле принималось бы и не читалось, что и
// есть запрещённое «принято-и-проигнорировано»). И только затем — вопрос о
// потребителях, который стоит обращения к движку.
//
// Мера потребителей — `download_count`, а НЕ наличие тегов. Репозиторий, заведённый
// опечаткой в `docker push` минуту назад, теги несёт, а потребителей у него нет;
// наказывать частый безобидный случай ради редкого опасного — тот самый размен,
// которым в `known-divergences.md` отвергнуто снятие глагола целиком. Чего эта мера
// НЕ ловит, сказано там же: образ, загруженный сегодня и упомянутый в конвейере,
// который ещё не запускался, скачиваний не имеет и проходит полосу молча.
//
// Ответ движка не получен → потребители НЕИЗВЕСТНЫ, и это не «их нет»: полоса
// закрывается fail-closed. Подтвердивший вызывающий движка не ждёт вовсе — вопрос,
// ради которого его спрашивали, уже снят.
//
// Размен назван прямо: между чтением `download_count` и переименованием возможна
// загрузка, поэтому полоса не является инвариантом данных и не претендует на него
// (ban #10 про инварианты, а это ограждение ВЫЗЫВАЮЩЕГО). Величина, на которой
// держится инвариант, живёт в движке, а не в нашей базе, и одним оператором не
// выражается by construction.
func (u *UseCase) assertRenameConfirmed(ctx context.Context, registryID, repository, confirmCurrentName string) error {
	if confirmCurrentName != "" {
		if confirmCurrentName != repository {
			return failInvalidArg("confirm_current_name must repeat the current repository name")
		}
		return nil
	}

	proj, err := u.zot.RepositoryProjection(ctx, registryID, repository)
	switch {
	case err != nil:
		return failFailedPrecondition("cannot establish whether repository %s has pulls: renaming breaks "+
			"every pull path to it; repeat the current name in confirm_current_name to proceed", repository)
	case proj != nil && proj.DownloadCount > 0:
		return failFailedPrecondition("repository %s has %d recorded pulls: renaming breaks every pull path "+
			"to it; repeat the current name in confirm_current_name to proceed", repository, proj.DownloadCount)
	}
	return nil
}

// doRename исполняет rename в worker'е: класс источника, collision-precheck, engine
// re-home (fail-closed A21), overlay re-key/promote под PK-backstop, projection-merge.
func (u *UseCase) doRename(ctx context.Context, registryID, repository, newName string, principal operations.Principal) (*domain.Repository, error) {
	overlay, oerr := u.cfg.GetConfig(ctx, registryID, repository)
	durable := true
	if oerr != nil {
		if !errors.Is(oerr, regerrors.ErrNotFound) {
			return nil, mapRepoErr(oerr)
		}
		durable = false // ephemeral (проекция без overlay) → auto-promote (A23)
	}

	if cerr := u.assertTargetAvailable(ctx, registryID, repository, newName); cerr != nil {
		return nil, cerr
	}

	// ФАЗА 1 — копирование в движке (НЕразрушающая). Движок недоступен → UNAVAILABLE
	// fail-closed: наложение НЕ трогаем, старое имя резолвится (A21). Под новым именем
	// может остаться частичная копия — она узнаётся как своя и повтор сходится
	// (assertTargetAvailable), а не отвергается «уже существует».
	if merr := u.zot.CopyRepositoryTags(ctx, registryID, repository, newName); merr != nil {
		return nil, mapRepoErr(merr)
	}

	reg, gerr := u.reader.Get(ctx, registryID)
	if gerr != nil {
		return nil, mapRepoErr(gerr)
	}

	visibility := reg.DefaultVisibility
	if durable {
		visibility = overlay.Visibility
	}
	visibility = resolveVisibility(visibility, reg.DefaultVisibility)
	intents := renameIntents(registryID, repository, newName, reg.ProjectID, principal, visibility)

	var (
		written        *domain.RepositoryConfig
		stampedIntents []OutboxIntent
		werr           error
	)
	if durable {
		written, stampedIntents, werr = u.cfg.RekeyConfig(ctx, registryID, repository, newName, intents...)
	} else {
		promoted := &domain.RepositoryConfig{
			RegistryID: registryID,
			Name:       newName,
			Visibility: visibility,
		}
		written, stampedIntents, werr = u.cfg.InsertConfig(ctx, promoted, intents...)
	}
	if werr != nil {
		if errors.Is(werr, regerrors.ErrAlreadyExists) {
			return nil, failAlreadyExists("repository already exists")
		}
		return nil, mapRepoErr(werr)
	}

	// re-register нового repo (parent+owner (+public-grant)) durable в той же tx (outbox);
	// СИНХРОННО регистрируем register-type intents для immediate pull/authz-резолва под
	// новым именем (best-effort non-fatal — drainer at-least-once; unregister old — async).
	u.syncRegisterOwnerTuples(ctx, registerIntents(stampedIntents)...)

	// ФАЗА 2 — снятие тегов под СТАРЫМ именем (разрушающая), и только теперь: имя и
	// права уже закреплены выше, поэтому сбой на этом шаге не делает содержимое
	// недостижимым — оно полностью адресуемо под новым именем. Прежний порядок
	// (снять, потом закрепить) на сбое посередине оставлял часть тегов вне
	// адресуемого имени, а под новым — набор без наложения и регистрации, куда не
	// доходил никто, включая администратора аккаунта и облака; при этом операция
	// докладывала ОТКАЗ, то есть «ничего не произошло».
	//
	// Неудача снятия НЕ отменяет состоявшийся перенос: предмет операции — имя, и оно
	// перенесено. Остаток под старым именем разрегистрирован (unregister-intent выше),
	// поэтому тенанту он недоступен; забрать его можно повторным переименованием или
	// удалением репозитория под старым именем. Это осознанный размен, записанный в
	// docs/architecture/known-divergences.md — а не молчаливое проглатывание: событие
	// именуется и считается.
	if perr := u.zot.PurgeRepositoryTags(ctx, registryID, repository); perr != nil {
		slog.WarnContext(ctx, "rename: stale tags left under the old name (engine purge failed)",
			"event", "registry.rename.purge_failed",
			"registry_id", registryID, "old_name", repository, "new_name", newName,
			"err", perr)
	}

	proj, perr := u.zot.RepositoryProjection(ctx, registryID, newName)
	if perr != nil {
		return nil, mapRepoErr(perr)
	}
	return mergeRepository(registryID, newName, written, proj), nil
}

// assertTargetAvailable — целевое имя доступно под перенос, иначе ALREADY_EXISTS
// (A17). Занято, если под ним есть строка наложения (объявленный ресурс) ЛИБО
// содержимое, которое НЕ является нашей собственной прерванной копией. DB
// PK-backstop (RekeyConfig/InsertConfig) остаётся авторитетным арбитром под
// concurrency (A18); эта проверка — ранний reject до копирования в движке.
//
// Почему «своя прерванная копия» — отдельный случай. Копирование НЕразрушающее и
// идемпотентное, поэтому сбой после первого же скопированного тега оставляет под
// целевым именем часть тегов источника. Прежняя проверка считала это «имя занято»
// и терминально блокировала ЛЮБОЙ повтор при полностью целом источнике — то есть
// операция становилась невыполнимой из-за собственной предыдущей попытки. Признак
// своей копии: под целевым именем нет строки наложения (это не объявленный ресурс)
// И все его теги есть у источника (подмножество). Чужое содержимое — теги, которых
// у источника нет, — по-прежнему отвергается.
func (u *UseCase) assertTargetAvailable(ctx context.Context, registryID, oldName, newName string) error {
	if _, err := u.cfg.GetConfig(ctx, registryID, newName); err == nil {
		return failAlreadyExists("repository already exists")
	} else if !errors.Is(err, regerrors.ErrNotFound) {
		return mapRepoErr(err)
	}
	proj, perr := u.zot.RepositoryProjection(ctx, registryID, newName)
	if perr != nil {
		return mapRepoErr(perr)
	}
	if proj == nil || proj.TagCount == 0 {
		return nil
	}
	ours, oerr := u.targetIsOwnPartialCopy(ctx, registryID, oldName, newName)
	if oerr != nil {
		return oerr
	}
	if !ours {
		return failAlreadyExists("repository already exists")
	}
	return nil
}

// targetIsOwnPartialCopy — все теги под целевым именем есть у источника (значит это
// остаток прерванного копирования этого же переноса). Читает по одной странице с
// каждой стороны: если содержимое не умещается в страницу, ответ консервативный
// (не наша копия → имя занято), потому что доказать подмножество мы не смогли.
func (u *UseCase) targetIsOwnPartialCopy(ctx context.Context, registryID, oldName, newName string) (bool, error) {
	srcTags, truncated, err := u.tagNames(ctx, registryID, oldName)
	if err != nil || truncated {
		return false, err
	}
	dstTags, truncated, err := u.tagNames(ctx, registryID, newName)
	if err != nil || truncated {
		return false, err
	}
	src := make(map[string]struct{}, len(srcTags))
	for _, t := range srcTags {
		src[t] = struct{}{}
	}
	for _, t := range dstTags {
		if _, ok := src[t]; !ok {
			return false, nil
		}
	}
	return len(dstTags) > 0, nil
}

// tagNames читает ОДНУ страницу имён тегов repo. Второй результат — «страница
// оборвана» (есть продолжение): вызывающий обязан трактовать это как «доказать не
// удалось», а не как «тегов больше нет».
func (u *UseCase) tagNames(ctx context.Context, registryID, repository string) ([]string, bool, error) {
	tags, next, err := u.zot.ListTags(ctx, TagListQuery{
		RegistryID: registryID,
		Repository: repository,
		PageSize:   renameTagCompareWindow,
	})
	if err != nil {
		return nil, false, mapRepoErr(err)
	}
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		out = append(out, t.Tag)
	}
	return out, next != "", nil
}

// renameTagCompareWindow — окно сравнения наборов тегов при опознании своей
// прерванной копии. Репозиторий с бОльшим числом тегов даёт консервативный ответ
// (имя занято), а не безразмерное чтение.
const renameTagCompareWindow = 1000

// renameIntents — FGA outbox-intent'ы rename в той же writer-tx: re-register new repo
// (parent+owner создателя), unregister old repo, + public-grant governance по итоговому
// visibility (PUBLIC → register(new)/unregister(old); PRIVATE → unregister(old) на всякий
// случай, no-op в iam если tuple отсутствовал).
func renameIntents(registryID, oldName, newName, projectID string, principal operations.Principal, visibility domain.Visibility) []OutboxIntent {
	subject := domain.FGASubjectFromPrincipal(principal.Type, principal.ID)
	intents := []OutboxIntent{
		{Event: domain.FGAEventRegister, Intent: domain.RegisterIntentForRepoPush(registryID, newName, projectID, subject)},
		{Event: domain.FGAEventUnregister, Intent: domain.UnregisterIntentForRepo(registryID, oldName)},
		{Event: domain.FGAEventUnregister, Intent: domain.UnregisterIntentForRepoPublicGrant(registryID, oldName)},
	}
	if visibility == domain.VisibilityPublic {
		intents = append(intents,
			OutboxIntent{Event: domain.FGAEventRegister, Intent: domain.RegisterIntentForRepoPublicGrant(registryID, newName)})
	}
	return intents
}
