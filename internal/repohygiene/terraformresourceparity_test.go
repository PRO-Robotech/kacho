// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Каждый ресурс публичного API обязан быть в Terraform-провайдере.
//
// Правило нельзя удержать перечнем в документации: перечень переживает своё дерево, и
// «покрыто всё» читается верно ровно до следующего сервиса. Поэтому список ресурсов
// ВЫВОДИТСЯ из контрактов, а покрытие сверяется с реестром провайдера.
//
// Гейт трёхсторонний, и все три стороны обязательны:
//
//  1. у каждого создающего сервиса контрактов есть запись в таблице соответствия;
//  2. тип, названный записью, ЗАРЕГИСТРИРОВАН в провайдере (а не просто объявлен файлом);
//  3. запись, чей сервис из контрактов исчез, — НАХОДКА, а не безобидный остаток.
//
// Без (1) новый сервис приезжает непокрытым и молча. Без (2) таблица описывает намерение.
// Без (3) таблица накапливает мёртвые строки, и её «полнота» перестаёт что-либо значить.

// tfCoverage — соответствие сервиса контрактов типу ресурса провайдера.
//
// Пустая строка справа означает ОСОЗНАННОЕ отсутствие ресурса, и такая запись обязана
// нести причину в комментарии рядом. Причина проверяется человеком; гейт проверяет, что
// запись вообще существует и что её сервис ещё жив.
// creatingVerbs — глаголы, ЗАВОДЯЩИЕ ресурс.
//
// Перечень явный, потому что имя глагола угадать нельзя: пользователь заводится не
// `Create`, а `Invite`, и первая редакция этого гейта его не увидела вовсе — то есть
// объявляла «покрыт каждый ресурс API», не прочитав целый вид предмета.
var creatingVerbs = map[string]bool{
	"Create": true, "Issue": true, "Invite": true, "CreateRepository": true,
}

// nonCreatingVerbs — все остальные глаголы публичного API, перечисленные ПОИМЁННО.
//
// Перечень существует ради одного свойства: новый глагол, не попавший ни в один из двух
// списков, роняет гейт. Без него слепое пятно повторится молча — ровно так, как повторилось
// с `Invite`. Список механически выведен из дерева и обязан за ним следовать.
var nonCreatingVerbs = map[string]bool{
	"AddCidrBlocks": true, "AddMember": true, "AddOneToOneNat": true, "AddRoutes": true, "AddTargets": true, "AttachDisk": true,
	"AttachNetworkInterface": true, "BatchCheck": true, "Block": true, "Cancel": true, "Check": true, "Delete": true,
	"DeleteRepository": true, "DeleteTag": true, "DetachDisk": true, "DetachNetworkInterface": true, "Disable": true,
	"Enable": true, "ExpandAccess": true, "ExpandRelations": true, "Get": true, "GetByValue": true, "GetRepository": true,
	"GetSerialPortOutput": true, "GetTargetStates": true, "List": true, "ListAccessBindings": true, "ListAllOperations": true,
	"ListAssignableRoles": true, "ListByAccount": true, "ListByRole": true, "ListByScope": true, "ListBySubject": true,
	"ListBySubnet": true, "ListMembers": true, "ListObjects": true, "ListOperations": true, "ListPermissionCatalog": true,
	"ListReferrers": true, "ListRepositories": true, "ListRouteTables": true, "ListSecurityGroups": true,
	"ListSubjectPrivileges": true, "ListSubjects": true, "ListSubnets": true, "ListTags": true, "ListUsedAddresses": true,
	"Move": true, "Relocate": true, "RemoveCidrBlocks": true, "RemoveMember": true, "RemoveOneToOneNat": true, "RemoveRoutes": true,
	"RemoveTargets": true, "RenameRepository": true, "Restart": true, "Revoke": true, "SetAccessBindings": true,
	"SimulateMaintenanceEvent": true, "Start": true, "Stop": true, "Unblock": true, "Update": true, "UpdateAccessBindings": true,
	"UpdateMetadata": true, "UpdateNetworkInterface": true, "UpdateRepository": true, "UpdateRoute": true, "UpdateRule": true,
	"UpdateRules": true, "WhoAmI": true,
}

var tfCoverage = map[string]string{
	// vpc
	"NetworkService":          "kacho_vpc_network",
	"SubnetService":           "kacho_vpc_subnet",
	"AddressService":          "kacho_vpc_address",
	"GatewayService":          "kacho_vpc_gateway",
	"RouteTableService":       "kacho_vpc_route_table",
	"SecurityGroupService":    "kacho_vpc_security_group",
	"NetworkInterfaceService": "kacho_vpc_network_interface",

	// iam
	"ProjectService":        "kacho_iam_project",
	"GroupService":          "kacho_iam_group",
	"ServiceAccountService": "kacho_iam_service_account",
	"SAKeyService":          "kacho_iam_service_account_key",
	"AccountService":        "kacho_iam_account",
	"RoleService":           "kacho_iam_role",
	"AccessBindingService":  "kacho_iam_access_binding",
	"UserService":           "kacho_iam_user_invitation",

	"UserTokenService": "kacho_iam_user_token",

	// nlb
	"TargetGroupService":         "kacho_nlb_target_group",
	"NetworkLoadBalancerService": "kacho_nlb_load_balancer",
	"ListenerService":            "kacho_nlb_listener",

	// compute
	"InstanceService": "kacho_compute_instance",

	// storage
	"VolumeService":   "kacho_storage_volume",
	"SnapshotService": "kacho_storage_snapshot",
	"ImageService":    "kacho_storage_image",

	// registry
	"RegistryService": "kacho_registry_registry",
}

// tfPending — ресурсы, которых в провайдере ЕЩЁ нет, с названным предметом.
//
// Это не послабление, а ДОЛГ С ЧИСЛОМ. Каждая запись обязана называть тикет; гейт печатает
// их количество, поэтому «покрыто всё» никогда не читается там, где покрыто не всё.
//
// Запись САМОИСТЕКАЕТ: как только ресурс появляется в реестре, она становится находкой —
// иначе исключение переживёт свой предмет и будет молча прикрывать следующий пропуск.
//
// У всех семи одна причина: их запрос создания несёт ВЛОЖЕННЫЕ объекты (спецификации
// адреса, шлюза, статические маршруты, правила группы, правила роли, субъекты привязки,
// спецификации машины). Общий каркас плоских ресурсов их не выражает, и втискивать их туда
// значило бы сделать каркас условным — то есть перестать понимать, что он делает.
var tfPending = map[string]string{
	"InstanceService": "PRO-Robotech/kacho#235",
}

// tfExtraResources — ресурсы провайдера, у которых НЕТ своего создающего сервиса.
//
// Так бывает у под-ресурсов: репозиторий реестра заводится глаголом самого реестра
// (`CreateRepository`), а не отдельным сервисом. Перечень существует, чтобы обратная
// сверка не считала такой ресурс лишним.
var tfExtraResources = map[string]string{
	"kacho_registry_repository": "заводится глаголом CreateRepository сервиса RegistryService",
}

// Под-ресурс обязан БЫТЬ, а не числиться: запись без ресурса — исключение, которому нечего
// исключать. Проверка стоит ниже, в самом гейте.

func TestEveryPublicAPIResourceHasATerraformResource(t *testing.T) {
	root := repoRoot(t)

	services := publicCreatingServices(t, root)
	if len(services) == 0 {
		t.Fatal("создающих сервисов в контрактах не найдено — предикат переписи устарел " +
			"или каталог proto переехал; ноль здесь означает сломанный обходчик, а не чистое дерево")
	}

	registered := registeredTerraformResources(t, root)
	if len(registered) == 0 {
		t.Fatal("зарегистрированных ресурсов провайдера не найдено — предикат переписи устарел")
	}

	var missingEntry, missingResource, staleEntry []string

	for _, svc := range services {
		tf, ok := tfCoverage[svc]
		if !ok {
			missingEntry = append(missingEntry, svc)
			continue
		}
		if tf == "" {
			continue // осознанное отсутствие; причина — в комментарии таблицы
		}
		if !registered[tf] {
			if issue := tfPending[svc]; issue != "" {
				continue // долг назван предметом; сверяется ниже
			}
			missingResource = append(missingResource, svc+" → "+tf)
		}
	}

	inTree := map[string]bool{}
	for _, s := range services {
		inTree[s] = true
	}
	for svc := range tfCoverage {
		if !inTree[svc] {
			staleEntry = append(staleEntry, svc)
		}
	}

	sort.Strings(missingEntry)
	sort.Strings(missingResource)
	sort.Strings(staleEntry)

	for _, s := range missingEntry {
		t.Errorf("сервис %s создаёт ресурс, но записи в таблице покрытия нет: заведите ресурс "+
			"провайдера либо объявите отсутствие пустой строкой с причиной", s)
	}
	for _, s := range missingResource {
		t.Errorf("таблица обещает ресурс, которого в реестре провайдера нет: %s", s)
	}
	for _, s := range staleEntry {
		t.Errorf("запись таблицы пережила свой сервис: %s больше не создаёт ресурс — снимите "+
			"строку, иначе полнота таблицы перестаёт что-либо значить", s)
	}

	// Обратная сверка: ресурс провайдера без создающего сервиса — тоже находка, кроме
	// объявленных под-ресурсов.
	promised := map[string]bool{}
	for _, tf := range tfCoverage {
		if tf != "" {
			promised[tf] = true
		}
	}
	for tf := range registered {
		if promised[tf] || tfExtraResources[tf] != "" {
			continue
		}
		t.Errorf("ресурс провайдера %s не соответствует ни одному создающему сервису — "+
			"либо сервис переименован, либо ресурс лишний", tf)
	}
	// Долг самоистекает: появившийся ресурс делает свою запись находкой.
	pending := 0
	for svc, issue := range tfPending {
		tf := tfCoverage[svc]
		if tf == "" {
			t.Errorf("долг назван для %s, но в таблице покрытия у него нет обещанного типа", svc)
			continue
		}
		if registered[tf] {
			t.Errorf("ресурс %s появился в провайдере — снимите запись долга (%s), иначе она "+
				"молча прикроет следующий пропуск", tf, issue)
			continue
		}
		if !inTree[svc] {
			t.Errorf("долг назван для %s, которого в контрактах больше нет", svc)
			continue
		}
		pending++
	}
	if pending > 0 {
		t.Logf("НЕ ПОКРЫТО: %d ресурсов, предмет назван (см. tfPending)", pending)
	}

	for tf, why := range tfExtraResources {
		if !registered[tf] {
			t.Errorf("под-ресурс %s объявлен исключением (%s), но в провайдере его нет — "+
				"исключению нечего исключать", tf, why)
		}
	}

	t.Logf("осмотрено: создающих сервисов %d, ресурсов провайдера %d, долгов %d",
		len(services), len(registered), len(tfPending))
}

var (
	reService = regexp.MustCompile(`(?m)^service\s+(\w+)\s*\{`)
	reAnyRPC  = regexp.MustCompile(`\brpc\s+(\w+)\s*\(`)
	// Две формы объявления имени, и обе настоящие: обычный ресурс склеивает имя с
	// префиксом провайдера, а виды iam берут его из своей таблицы целиком. Предикат,
	// знающий одну форму, объявил бы три существующих ресурса отсутствующими — что он и
	// сделал при первом прогоне.
	reTypeName = regexp.MustCompile(`ProviderTypeName\s*\+\s*"(_[a-z0-9_]+)"`)
	reTypeLit  = regexp.MustCompile(`tfName:\s*"(kacho_[a-z0-9_]+)"`)
)

// publicCreatingServices — сервисы контрактов, заводящие ресурс.
//
// Internal-сервисы исключены НАМЕРЕННО: их поверхность — админский каталог на
// cluster-internal слушателе, арендатору она не адресована, и описывать её декларативно
// нечем. Read-only справочники (тип диска, регион, зона, тип машины) создающих глаголов не
// имеют и в перепись не попадают by construction — их место в источниках данных.
func publicCreatingServices(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	for _, rel := range trackedFilesUnder(t, root, "proto/kacho/cloud", ".proto") {
		src, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь из индекса репозитория
		if err != nil {
			t.Fatalf("чтение %s: %v", rel, err)
		}
		s := string(src)
		for _, m := range reService.FindAllStringSubmatchIndex(s, -1) {
			name := s[m[2]:m[3]]
			if strings.HasPrefix(name, "Internal") {
				continue
			}
			creating := false
			for _, v := range reAnyRPC.FindAllStringSubmatch(serviceBody(s, m[1]), -1) {
				switch {
				case creatingVerbs[v[1]]:
					creating = true
				case nonCreatingVerbs[v[1]]:
				default:
					t.Errorf("сервис %s несёт глагол %s, не отнесённый ни к заводящим, ни к "+
						"остальным — классифицируйте его, иначе новый вид ресурса проедет молча",
						name, v[1])
				}
			}
			if creating {
				out = append(out, name)
			}
		}
	}
	sort.Strings(out)
	return out
}

// trackedFilesUnder — ОТСЛЕЖИВАЕМЫЕ файлы поддерева, а не содержимое каталога на диске.
//
// Единица счёта здесь — элемент индекса git, и это не формальность: на диске лежат
// установленные зависимости, сборки сайтов и рабочие каталоги инструментов. Обходчик,
// читающий диск, судит о дереве, которого в репозитории нет, — и однажды спотыкается о
// символьную ссылку в чужую рабочую копию. Ровно это и ловит гейт обходчиков.
func trackedFilesUnder(t *testing.T, root, prefix, suffix string) []string {
	t.Helper()
	cmd := exec.Command("git", "ls-files", "-z", "--", prefix)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("чтение индекса репозитория: %v", err)
	}
	var res []string
	for _, rel := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if rel != "" && strings.HasSuffix(rel, suffix) {
			res = append(res, rel)
		}
	}
	if len(res) == 0 {
		t.Fatalf("под %s не найдено ни одного файла %s — предикат устарел или каталог переехал",
			prefix, suffix)
	}
	return res
}

// serviceBody — тело сервиса от открывающей скобки до парной закрывающей.
func serviceBody(s string, open int) string {
	depth := 1
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[open:i]
			}
		}
	}
	return s[open:]
}

// registeredTerraformResources — типы, ДОСТИЖИМЫЕ из реестра провайдера.
//
// Направление обхода принципиально: идём ОТ реестра к ресурсам, а не наоборот. Файл,
// объявляющий ресурс, но не провязанный в реестр, для пользователя не существует — и
// обратный обход счёл бы его существующим.
//
// Реестр знает две формы записи, и обе настоящие: конструктор ресурса со своей механикой
// (`NewXxxResource`) и сборка ресурса из описания (`newFlatResource(xxxSpec)`). Предикат,
// знающий одну, объявил бы половину дерева отсутствующей.
func registeredTerraformResources(t *testing.T, root string) map[string]bool {
	t.Helper()
	sources := map[string]string{}
	var registryBody string
	for _, rel := range trackedFilesUnder(t, root, "terraform/internal/provider", ".go") {
		if strings.HasSuffix(rel, "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь из индекса репозитория
		if err != nil {
			t.Fatalf("чтение %s: %v", rel, err)
		}
		s := string(src)
		name := filepath.Base(rel)
		sources[name] = s
		if name == "provider.go" {
			if i := strings.Index(s, "func (p *kachoProvider) Resources("); i >= 0 {
				registryBody = serviceBody(s, strings.Index(s[i:], "{")+i+1)
			}
		}
	}
	if registryBody == "" {
		t.Fatal("реестр ресурсов провайдера не найден — предикат переписи устарел")
	}

	out := map[string]bool{}

	// Форма 1: собственный конструктор. Имя типа берётся из Metadata его файла.
	for _, m := range reRegCtor.FindAllStringSubmatch(registryBody, -1) {
		ctor := m[1]
		for _, s := range sources {
			if !regexp.MustCompile(`func\s+` + ctor + `\s*\(`).MatchString(s) {
				continue
			}
			for _, t := range reTypeName.FindAllStringSubmatch(s, -1) {
				out["kacho"+t[1]] = true
			}
			for _, t := range reTypeLit.FindAllStringSubmatch(s, -1) {
				out[t[1]] = true
			}
		}
	}

	// Форма 2: сборка из описания. Имя типа берётся из самого описания.
	for _, m := range reRegFlat.FindAllStringSubmatch(registryBody, -1) {
		spec := m[1]
		found := false
		for _, s := range sources {
			d := regexp.MustCompile(`var\s+` + spec + `\s*=\s*flatSpec\{[^}]*?tfName:\s*"(kacho_[a-z0-9_]+)"`)
			if g := d.FindStringSubmatch(s); g != nil {
				out[g[1]] = true
				found = true
			}
		}
		if !found {
			t.Errorf("реестр собирает ресурс из описания %s, но описания с таким именем нет", spec)
		}
	}
	return out
}

var (
	reRegCtor = regexp.MustCompile(`\b(New\w+Resource)\b`)
	reRegFlat = regexp.MustCompile(`newFlatResource\((\w+)\)`)
)
