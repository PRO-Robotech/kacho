// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package registry

import (
	"fmt"

	"github.com/PRO-Robotech/kacho/pkg/ids"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"

	"github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/shared/namepage"
	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
	regerrors "github.com/PRO-Robotech/kacho/services/registry/internal/errors"
)

// ValidateRegistryID отсекает malformed registry-id синхронно первым стейтментом
// RPC: prefix `reg` (family-agnostic) → InvalidArgument "invalid registry id '<X>'".
// Пустой id пропускается (required-проверка — отдельно у caller'а). Экспортирован —
// единый канонический валидатор для use-case и handler-предчека ScopeFiltered-RPC
// (текст ошибки — часть контракта, не дублируем правило по слоям).
func ValidateRegistryID(id string) error {
	return corevalidate.ResourceID("registry", ids.PrefixRegistry, id)
}

// validatePageSize приводит page_size к контракту List (0→default 50, вне [0..1000]
// → InvalidArgument). Возвращает effective-значение для LIMIT.
func validatePageSize(size int64) (int64, error) {
	return corevalidate.PageSize("page_size", size)
}

// ValidateRepoListPagination — формат страницы ListRepositories, пригодный к вызову
// ИЗ ХЕНДЛЕРА, до гейта доступа.
//
// Зачем экспортировано, а не оставлено внутри use-case'а. ListRepositories и
// ListTags — ScopeFiltered: хендлер спрашивает права первой строкой (namespaceGate /
// checkRepo) и только потом зовёт use-case, где формат и проверялся. Значит один и
// тот же мусорный курсор отвечал по-разному в зависимости от того, что вызывающему
// выдано: у кого грант есть — INVALID_ARGUMENT про page_token, у кого нет — отказ по
// правам. Вопрос «правильно ли составлен запрос» имеет ОДИН ответ для всех
// (api-conventions.md: формат → authz → repo), поэтому предикат обязан быть доступен
// там, где стоит гейт.
//
// Предикатов ДВА, а не один: кодек курсора у перечислений разный — полоса+позиция у
// репозиториев, имя граничного тега у тегов. Общий предикат делал бы вид, что курсоры
// взаимозаменяемы. Use-case и zot-адаптер повторяют обе проверки и остаются
// авторитетными на служимом пути.
func ValidateRepoListPagination(pageSize int64, pageToken string) error {
	if _, err := validatePageSize(pageSize); err != nil {
		return err
	}
	if _, _, err := decodeRepoCursor(pageToken); err != nil {
		return err
	}
	return nil
}

// ValidateTagListPagination — то же для ListTags; курсор тегов именует граничный тег
// (namepage), а не полосу с позицией. См. godoc ValidateRepoListPagination.
func ValidateTagListPagination(pageSize int64, pageToken string) error {
	if _, err := validatePageSize(pageSize); err != nil {
		return err
	}
	if pageToken == "" {
		return nil
	}
	if _, err := namepage.Decode(pageToken); err != nil {
		return fmt.Errorf("%w: invalid page_token", regerrors.ErrInvalidArg)
	}
	return nil
}

// knownUpdateFields — whitelist update_mask Registry. name/project_id/id/region_id/
// placement_type входят как известные, но hard-immutable (см. immutableUpdateFields) —
// их наличие в mask даёт InvalidArgument с каноничным immutable-текстом, а не
// unknown-field. defaultRepositoryVisibility (REG-1 F5 rename) — mutable admin-gated.
var knownUpdateFields = map[string]struct{}{
	"name":                          {},
	"project_id":                    {},
	"projectId":                     {},
	"id":                            {},
	"description":                   {},
	"labels":                        {},
	"default_repository_visibility": {},
	"defaultRepositoryVisibility":   {},
	"region_id":                     {},
	"regionId":                      {},
	"placement_type":                {},
	"placementType":                 {},
}

// immutableUpdateFields → каноничный immutable-текст (update_mask discipline): поле в
// mask, но менять нельзя после Create. name — mutable (смена не трогает endpoint/zot по
// id, F2). id — единственная идентичность/адресация (F1, REG-1-04); project_id — owner;
// region_id/placement_type — placement-якорь immutable (F4, REG-1-14; перенос региона
// сломал бы storage-locality блобов).
var immutableUpdateFields = map[string]string{
	"id":             "id is immutable after Registry.Create",
	"project_id":     "projectId is immutable after Registry.Create",
	"projectId":      "projectId is immutable after Registry.Create",
	"region_id":      "regionId is immutable after Registry.Create",
	"regionId":       "regionId is immutable after Registry.Create",
	"placement_type": "placementType is immutable after Registry.Create",
	"placementType":  "placementType is immutable after Registry.Create",
}

// knownRepoUpdateFields — whitelist update_mask config-overlay Repository (RG-1).
// name/registry_id входят как известные, но hard-immutable (см. immutableRepoUpdateFields):
// их наличие → InvalidArgument с каноничным immutable-текстом (смена имени — только
// RenameRepository), а не generic unknown-field.
var knownRepoUpdateFields = map[string]struct{}{
	"description": {},
	"labels":      {},
	"visibility":  {},
	"name":        {},
	"registry_id": {},
	"registryId":  {},
}

// immutableRepoUpdateFields → каноничный immutable-текст. name — hard-immutable для
// Update (смена имени только RenameRepository); registry_id — натуральный ключ.
var immutableRepoUpdateFields = map[string]string{
	"name":        "name is immutable after Repository.Create",
	"registry_id": "registryId is immutable after Repository.Create",
	"registryId":  "registryId is immutable after Repository.Create",
}

// resolveRepoUpdateMask применяет update_mask discipline к RepositoryConfigUpdate:
//   - immutable поле (name/registry_id) → InvalidArgument (каноничный immutable-текст,
//     ДО UpdateMask — иначе known-set отверг бы их как generic unknown, api-conventions.md);
//   - unknown поле → InvalidArgument (corevalidate.UpdateMask с known-set);
//   - пустой mask → full-object PATCH (description/labels/visibility);
//   - mutable поле → соответствующий Apply*-флаг.
func resolveRepoUpdateMask(spec RepositoryConfigUpdate, mask []string) (RepositoryConfigUpdate, error) {
	for _, p := range mask {
		if msg, ok := immutableRepoUpdateFields[p]; ok {
			return spec, failInvalidArg("%s", msg)
		}
		// REG-1-24 (F7): lifecycle — output-only (system-managed auto-promote), не
		// tenant-input. В update_mask → INVALID_ARGUMENT (тот же класс, что tagCount/
		// createdAt). Reject ДО UpdateMask — специфичный текст вместо generic unknown-field.
		if p == "lifecycle" {
			return spec, failInvalidArg("lifecycle is read-only (system-managed)")
		}
	}
	if err := corevalidate.UpdateMask("update_mask", mask, knownRepoUpdateFields); err != nil {
		return spec, err
	}
	if len(mask) == 0 {
		spec.ApplyDescription = true
		spec.ApplyLabels = true
		spec.ApplyVisibility = true
		return spec, nil
	}
	for _, p := range mask {
		switch p {
		case "description":
			spec.ApplyDescription = true
		case "labels":
			spec.ApplyLabels = true
		case "visibility":
			spec.ApplyVisibility = true
		}
	}
	return spec, nil
}

// resolveUpdateMask применяет update_mask discipline к UpdateSpec:
//   - unknown поле → InvalidArgument (corevalidate.UpdateMask);
//   - immutable поле (project) → InvalidArgument (каноничный текст);
//   - пустой mask → full-object PATCH (все mutable-поля; name — только если задан
//     в теле, иначе description/labels-only PATCH не должен «очистить» имя);
//   - mutable поле → соответствующий Apply*-флаг.
//
// Мутирует spec.ApplyName/ApplyDescription/ApplyLabels, возвращает нормализованный spec.
func resolveUpdateMask(spec UpdateSpec) (UpdateSpec, error) {
	if err := corevalidate.UpdateMask("update_mask", spec.Mask, knownUpdateFields); err != nil {
		return spec, err
	}
	for _, p := range spec.Mask {
		if msg, ok := immutableUpdateFields[p]; ok {
			return spec, failInvalidArg("%s", msg)
		}
	}
	if len(spec.Mask) == 0 {
		// full-object PATCH: применяются все mutable-поля. name применяем только
		// если он реально передан (непустой) — иначе PATCH без имени (обновляющий
		// лишь description/labels) не должен пытаться выставить пустое имя.
		spec.ApplyName = spec.Name != ""
		spec.ApplyDescription = true
		spec.ApplyLabels = true
		// default_visibility в full-PATCH применяем ТОЛЬКО если задано конкретное
		// значение (UNSPECIFIED=не передано клиентом → не клобберим сид в 0; parity с
		// ApplyName). Явный PRIVATE/PUBLIC в теле пустого mask → применяется.
		spec.ApplyDefaultVisibility = spec.DefaultVisibility != domain.VisibilityUnspecified
		return spec, nil
	}
	for _, p := range spec.Mask {
		switch p {
		case "name":
			spec.ApplyName = true
		case "description":
			spec.ApplyDescription = true
		case "labels":
			spec.ApplyLabels = true
		case "default_repository_visibility", "defaultRepositoryVisibility":
			spec.ApplyDefaultVisibility = true
		}
	}
	return spec, nil
}
