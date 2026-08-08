// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// uisourcereadtest_test.go — проба интерфейса не читает собственный исходник как
// текст.
//
// # Предмет
//
// Проба вида «открыть свой .tsx и убедиться, что в нём встречается имя экспорта»
// зелена всегда, пока файл существует: она не рендерит компонент, не зовёт функцию
// и не проверяет ни одного исхода. Такой файл ЗАНИМАЕТ СЛОТ — счётчик проб растёт,
// отчёт зелёный, а поведение не проверено ничем. Это хуже отсутствующей пробы:
// отсутствие видно, а ложная уверенность — нет.
//
// Отслеживается PRO-Robotech/kacho#5. Замер на 2026-08-08: из 240 проб интерфейса
// 135 не делают ничего, кроме чтения своего исходника, ещё 4 совмещают это с
// рендером. Класс РАСТЁТ: с момента заведения issue прибавилось 97 файлов проб, из
// них 36 написаны тем же образцом — то есть он воспроизводится копированием.
//
// # Почему гейт, а не разовая переписка
//
// Заменить 135 файлов — работа, которую нельзя сделать одним заходом. Но пока
// образец не запрещён, каждый новый компонент приносит сто тридцать шестой, и
// переписка не сходится by construction. Гейт останавливает рост: перечень ниже
// фиксирует ДОЛГ, а не разрешение.
//
// # Самоистечение
//
// Перечень истекает сам в обе стороны: файл, который перестал читать свой исходник
// (переписан по существу) или исчез из дерева, роняет гейт как УСТАРЕВШАЯ запись.
// Иначе список пережил бы свой предмет и стал слепой зоной — ровно тем, от чего
// заводился.
//
// # Что НЕ является находкой
//
// Чтение файла в пробе законно, когда предмет пробы — САМ ФАЙЛ: проверка формы
// сгенерированного артефакта, сверка двух копий, разбор конфигурации. Такие пробы
// живут вне `ui-future/**/*.test.ts*` либо вносятся в перечень с разбором.
package repohygiene

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

// uiSourceReadingTests — пробы интерфейса, читающие собственный исходник как текст.
//
// ЭТО ДОЛГ, А НЕ РАЗРЕШЕНИЕ. Каждая строка — проба, которая ничего не проверяет.
// Правильный исход по каждой: переписать так, чтобы она звала функцию или рендерила
// компонент и утверждала ИСХОД, — и удалить строку отсюда.
var uiSourceReadingTests = []string{
	"ui-future/compute/src/api/client.endpoints.test.ts",
	"ui-future/compute/src/components/atoms/CopyableName/CopyableName.test.tsx",
	"ui-future/compute/src/lib/form-schema.attribution.test.ts",
	"ui-future/compute/src/test/operations-subroute-audit.test.ts",
	"ui-future/dashboard/src/lib/proto-package-names.test.ts",
	"ui-future/dashboard/src/lib/service-modules.coverage.test.ts",
	"ui-future/host/src/components/molecules/HostBreadcrumb/HostBreadcrumb.labels.test.tsx",
	"ui-future/host/src/components/organisms/HostRail/HostRail.spec-icons.test.ts",
	"ui-future/iam/src/access-binding-field-names.test.ts",
	"ui-future/iam/src/registry-iam1.test.ts",
	"ui-future/iam/src/vite-proxy-covers-rule-pickers.test.ts",
	"ui-future/nlb/src/api/types.contract.test.ts",
	"ui-future/nlb/src/api/wire-naming.test.ts",
	"ui-future/nlb/src/components/organisms/form/RefSelect/refOptionLabel.test.ts",
	"ui-future/nlb/src/dev-proxy.test.ts",
	"ui-future/nlb/src/lib/proto-package-names.test.ts",
	"ui-future/nlb/src/lib/spec-columns.bool.test.tsx",
	"ui-future/nlb/src/pages/NlbPage/NlbPage.test.tsx",
	"ui-future/registry/src/api/types.contract.test.ts",
	"ui-future/registry/src/api/wire-naming.test.ts",
	"ui-future/registry/src/components/organisms/form/RefSelect/refOptionLabel.test.ts",
	"ui-future/registry/src/dev-proxy.test.ts",
	"ui-future/registry/src/lib/proto-package-names.test.ts",
	"ui-future/registry/src/pages/RegistryPage/RegistryPage.test.tsx",
	"ui-future/shared/src/components/atoms/ContextBadge/ContextBadge.test.tsx",
	"ui-future/shared/src/components/atoms/CopyableId/CopyableId.test.tsx",
	"ui-future/shared/src/components/atoms/StatusBadge/StatusBadge.test.tsx",
	"ui-future/shared/src/components/atoms/ui/Button/Button.test.tsx",
	"ui-future/shared/src/components/atoms/ui/Dialog/Dialog.test.tsx",
	"ui-future/shared/src/components/atoms/ui/Input/Input.test.tsx",
	"ui-future/shared/src/components/molecules/DeleteDialog/DeleteDialog.test.tsx",
	"ui-future/shared/src/components/molecules/DetailOverviewActions/DetailOverviewActions.test.tsx",
	"ui-future/shared/src/components/molecules/DopplerButton/DopplerButton.test.tsx",
	"ui-future/shared/src/components/molecules/EditableKVTable/EditableKVTable.test.tsx",
	"ui-future/shared/src/components/molecules/ErrorResult/ErrorResult.test.tsx",
	"ui-future/shared/src/components/molecules/IpamUtilizationBar/IpamUtilizationBar.test.tsx",
	"ui-future/shared/src/components/molecules/JsonEditor/JsonEditor.test.tsx",
	"ui-future/shared/src/components/molecules/JsonMonacoView/JsonMonacoView.test.tsx",
	"ui-future/shared/src/components/molecules/MoveStubDialog/MoveStubDialog.test.tsx",
	"ui-future/shared/src/components/molecules/OperationBanner/OperationBanner.test.tsx",
	"ui-future/shared/src/components/molecules/OperationDialog/OperationDialog.test.tsx",
	"ui-future/shared/src/components/molecules/OperationsTable/OperationsTable.test.tsx",
	"ui-future/shared/src/components/molecules/OperationToastWatcher/OperationToastWatcher.test.tsx",
	"ui-future/shared/src/components/molecules/PageHeaderSlot/PageHeaderSlot.test.tsx",
	"ui-future/shared/src/components/molecules/PanelHeader/PanelHeader.test.tsx",
	"ui-future/shared/src/components/molecules/ProjectRequiredEmpty/ProjectRequiredEmpty.test.tsx",
	"ui-future/shared/src/components/molecules/RefNameLink/RefNameLink.test.tsx",
	"ui-future/shared/src/components/molecules/ResourceEmptyState/ResourceEmptyState.test.tsx",
	"ui-future/shared/src/components/molecules/ResourceRefChips/ResourceRefChips.test.tsx",
	"ui-future/shared/src/components/molecules/RowActionsMenu/RowActionsMenu.test.tsx",
	"ui-future/shared/src/components/molecules/SectionHeader/SectionHeader.test.tsx",
	"ui-future/shared/src/components/molecules/SubnetCidrChips/SubnetCidrChips.test.tsx",
	"ui-future/shared/src/components/molecules/TableToolbar/TableToolbar.test.tsx",
	"ui-future/shared/src/components/organisms/AddressPoolCidrManager/AddressPoolCidrManager.test.tsx",
	"ui-future/shared/src/components/organisms/DependencyTreePanel/DependencyTreePanel.test.tsx",
	"ui-future/shared/src/components/organisms/DetailShell/DetailShell.test.tsx",
	"ui-future/shared/src/components/organisms/form/FieldLabel/FieldLabel.test.tsx",
	"ui-future/shared/src/components/organisms/form/FormField/FormField.test.tsx",
	"ui-future/shared/src/components/organisms/form/FormFooter/FormFooter.test.tsx",
	"ui-future/shared/src/components/organisms/form/FormShell/FormShell.test.tsx",
	"ui-future/shared/src/components/organisms/form/ImmutableField/ImmutableField.test.tsx",
	"ui-future/shared/src/components/organisms/form/LabelsEditor/LabelsEditor.test.tsx",
	"ui-future/shared/src/components/organisms/form/NicSpecFields/NicSpecFields.test.tsx",
	"ui-future/shared/src/components/organisms/form/RefSelect/RefSelect.test.tsx",
	"ui-future/shared/src/components/organisms/form/ResourceFormBody/ResourceFormBody.test.tsx",
	"ui-future/shared/src/components/organisms/form/ResourceIcon/ResourceIcon.registry.test.ts",
	"ui-future/shared/src/components/organisms/form/ResourceIcon/ResourceIcon.test.tsx",
	"ui-future/shared/src/components/organisms/form/SgRulesEditor/SgRulesEditor.test.tsx",
	"ui-future/shared/src/components/organisms/GlobalResourceFormModal/GlobalResourceFormModal.test.tsx",
	"ui-future/shared/src/components/organisms/iam/IamCommon/IamCommon.test.tsx",
	"ui-future/shared/src/components/organisms/InlineAddressPoolCreateForm/InlineAddressPoolCreateForm.test.tsx",
	"ui-future/shared/src/components/organisms/InlineAddressPoolEditForm/InlineAddressPoolEditForm.test.tsx",
	"ui-future/shared/src/components/organisms/InlineNetworkInterfaceCreateForm/InlineNetworkInterfaceCreateForm.test.tsx",
	"ui-future/shared/src/components/organisms/InlineNetworkInterfaceEditForm/InlineNetworkInterfaceEditForm.test.tsx",
	"ui-future/shared/src/components/organisms/InlineResourceCreateForm/InlineResourceCreateForm.test.tsx",
	"ui-future/shared/src/components/organisms/InlineResourceEditForm/InlineResourceEditForm.test.tsx",
	"ui-future/shared/src/components/organisms/InlineResourceForm/InlineResourceForm.test.tsx",
	"ui-future/shared/src/components/organisms/InlineSecurityGroupEditForm/InlineSecurityGroupEditForm.test.tsx",
	"ui-future/shared/src/components/organisms/InlineSubnetCreateForm/InlineSubnetCreateForm.test.tsx",
	"ui-future/shared/src/components/organisms/InlineSubnetEditForm/InlineSubnetEditForm.test.tsx",
	"ui-future/shared/src/components/organisms/LabelsEditor/LabelsEditor.test.tsx",
	"ui-future/shared/src/components/organisms/NetworkCidrManager/NetworkCidrManager.test.tsx",
	"ui-future/shared/src/components/organisms/OperationsTab/OperationsTab.test.tsx",
	"ui-future/shared/src/components/organisms/ResourceCreatePage/ResourceCreatePage.test.tsx",
	"ui-future/shared/src/components/organisms/ResourceDetailExtensions/ResourceDetailExtensions.test.tsx",
	"ui-future/shared/src/components/organisms/ResourceDetailPage/ResourceDetailPage.test.tsx",
	"ui-future/shared/src/components/organisms/ResourceEditPage/ResourceEditPage.test.tsx",
	"ui-future/shared/src/components/organisms/ResourceFormModal/ResourceFormModal.test.tsx",
	"ui-future/shared/src/components/organisms/ResourceListPage/ResourceListPage.test.tsx",
	"ui-future/shared/src/components/organisms/ResourceShell/ResourceShell.test.tsx",
	"ui-future/shared/src/components/organisms/ResourceTable/ResourceTable.test.tsx",
	"ui-future/shared/src/components/organisms/RoutesEditor/RoutesEditor.test.tsx",
	"ui-future/shared/src/components/organisms/RoutesPanel/RoutesPanel.test.tsx",
	"ui-future/shared/src/components/organisms/SgRulesPanel/SgRulesPanel.test.tsx",
	"ui-future/shared/src/components/organisms/SubnetCidrManager/SubnetCidrManager.test.tsx",
	"ui-future/shared/src/components/organisms/SubnetCidrPanel/SubnetCidrPanel.test.tsx",
	"ui-future/shared/src/components/organisms/system/GrantAdminModal/GrantAdminModal.test.tsx",
	"ui-future/shared/src/lib/api-path-surface.test.ts",
	"ui-future/shared/src/lib/operations-subroute.test.ts",
	"ui-future/shared/src/pages/InstanceDetailPage.attach.test.ts",
	"ui-future/shared/src/test/antd-icons-stub.test.ts",
	"ui-future/shared/src/test/console-verb-routes-exist.test.ts",
	"ui-future/shared/src/test/iam-pages-authz-single-source.test.ts",
	"ui-future/shared/src/test/request-body-built-not-spread.test.ts",
	"ui-future/shared/src/test/shared-organisms-single-source.test.ts",
	"ui-future/storage/src/api/wire-naming.test.ts",
	"ui-future/storage/src/components/atoms/StatusBadge/StatusBadge.test.tsx",
	"ui-future/storage/src/components/organisms/form/RefSelect/refOptionLabel.test.ts",
	"ui-future/storage/src/dev-proxy.test.ts",
	"ui-future/storage/src/lib/proto-package-names.test.ts",
	"ui-future/system/src/components/molecules/auth/StepUpModal/StepUpModal.test.tsx",
	"ui-future/system/src/pages/SystemPage/SystemPage.host-addresses.test.ts",
	"ui-future/system/src/pages/SystemPage/SystemPage.proxy-coverage.test.ts",
	"ui-future/system/src/pages/SystemPage/SystemPage.search-domains.test.ts",
}

// TestUITestsDoNotReadTheirOwnSourceAsText — новая проба интерфейса не вправе
// подтверждать себя чтением собственного исходника.
func TestUITestsDoNotReadTheirOwnSourceAsText(t *testing.T) {
	root := repoRoot(t)

	var scanned, reading []string
	for _, rel := range trackedPaths(t, root) {
		if !strings.HasPrefix(rel, "ui-future/") {
			continue
		}
		if !strings.HasSuffix(rel, ".test.ts") && !strings.HasSuffix(rel, ".test.tsx") {
			continue
		}
		scanned = append(scanned, rel)
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("%s: %v — состав проб неизвестен, значит вердикт был бы утверждением ни о чём", rel, err)
		}
		if strings.Contains(string(b), "readFileSync") {
			reading = append(reading, rel)
		}
	}
	if len(scanned) == 0 {
		t.Fatal("обход не нашёл ни одной пробы интерфейса — гейт беспредметен. " +
			"Либо каталог переехал, либо расширение проб изменилось; в обоих случаях " +
			"зелёный вердикт ниже был бы получен даром.")
	}
	sort.Strings(reading)

	known := slices.Clone(uiSourceReadingTests)
	sort.Strings(known)

	for _, rel := range reading {
		if !slices.Contains(known, rel) {
			t.Errorf("проба интерфейса подтверждает себя чтением собственного исходника:\n  %s\n\n"+
				"Такая проба зелена, пока файл существует: компонент не рендерится, функция не "+
				"зовётся, ни один исход не утверждается — слот занят, поведение не проверено.\n"+
				"Исходы: зови функцию и утверждай её ответ / рендери компонент и утверждай "+
				"наблюдаемое / внеси сюда с разбором, если предмет пробы — сам файл.", rel)
		}
	}
	for _, rel := range known {
		if !slices.Contains(reading, rel) {
			t.Errorf("запись пережила свой предмет — эта проба больше не читает свой исходник "+
				"(или её нет в дереве):\n  %s\n\n"+
				"Удали строку из uiSourceReadingTests. Перечень фиксирует ДОЛГ; запись, которой "+
				"нечего исключать, превращает его в слепую зону.", rel)
		}
	}

	t.Logf("перепись: проб интерфейса осмотрено %d, читают свой исходник %d, "+
		"числится долгом %d", len(scanned), len(reading), len(known))
}
