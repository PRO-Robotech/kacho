// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package auditlistfilter states how kacho-vpc is laid out for the public-List
// gate. The analysis itself — and why it parses instead of grepping — lives in
// pkg/listfiltergate.
//
// # This service's shape
//
// vpc colocates transport and use-cases per resource: internal/apps/kacho/api/<res>
// holds a `Handler` whose `List` delegates to a per-RPC use-case, which reads the
// page and then narrows it through `FilterVisibleIDs`. The transport type is called
// `Handler` in every package, so the PACKAGE is what tells one resource from
// another.
//
// The filter call is worth noting: in most packages it does NOT sit in the use-case
// body but in a package-local helper the use-case calls (filterVisibleNetworks and
// its siblings). That is why the gate follows the calls a List makes instead of
// reading one method — and equally why it must not simply search the package, which
// would let any filtered neighbour vouch for an unfiltered List.
//
// # What the previous gate keyed on, and why that was a defect
//
// It collected candidates with `grep -rl 'func .* List(' --include='handler.go'` —
// that is, it recognised a resource by a FILE being called handler.go — and then
// searched that file and its sibling list.go for the filter's name. Two
// consequences:
//
//   - renaming handler.go, or moving the List declaration into any other file of
//     the package, removed the resource from the gate's view, and its List went
//     unjudged;
//   - a text search cannot tell a call from a sentence about a call, nor from an
//     interface method declaration. Deleting the filter and leaving the comment that
//     described it kept the gate green.
//
// On top of both, a missing internal/apps/kacho/api exited 0 with a message on
// stderr, so "the gate could not find the tree" and "the tree is clean" were the
// same verdict.
//
// # Declarations
//
// vpc has by far the widest listing surface of the three services this analyser
// drives: 21 listing methods across 8 resources. The previous gate saw 8 of them —
// the ones named exactly `List` — and was silent about the other 13, which are the
// child collections and the operation histories. Each is now declared.
//
//   - the eight `List` RPCs are the project-scoped collections and stay RowFilter;
//   - eleven child listings (ListSubnets, ListSecurityGroups, ListRouteTables,
//     ListUsedAddresses and the seven ListOperations) are ParentGate: the handler
//     reads the containing resource through get.Execute and returns on its error
//     before the page is read. The gate is named "get.Execute" rather than
//     "Execute" because the page read is ALSO an Execute — matching the bare method
//     name would let the page read vouch for its own gate;
//   - address.ListBySubnet is EdgeGate: nothing in the service reads the subnet, and
//     the handler's own comment says so — the per-RPC authorization at the edge is
//     what settles it. That is a real check, so the gate verifies it in the proto
//     rather than taking the word for it: rpc ListBySubnet must carry a
//     required_relation and a scope_extractor on subnet_id. Had it resolved its
//     scope from "*", the relation would be satisfied by the wildcard tuple the
//     cluster catalog is opened with, and the declaration would be documenting a
//     check that narrows nothing;
//   - addresspool's two listings are ClusterScoped. It used to be excluded with
//     --allow=addresspool, which excluded the RESOURCE — and so also covered
//     ListAddresses, a method the exclusion was never written about. Both now say
//     so separately.
package auditlistfilter

import "github.com/PRO-Robotech/kacho/pkg/listfiltergate"

// parentGate is the shape of every child listing in this service: read the
// containing resource first, return on its error, then read the page.
func parentGate() listfiltergate.Listing {
	return listfiltergate.Listing{Shape: listfiltergate.ParentGate, Gate: "get.Execute"}
}

// adminPool is the shared reason for addresspool's two listings.
//
// Здесь стояло «Internal admin RPC». С ADM-1 S1 поверхность ПУБЛИЧНА, и довод
// от места вызова перестал быть верным — но сам вывод не изменился и стал
// прочнее: сужать по-прежнему нечего, потому что у пула нет владельца, о котором
// можно спросить пообъектно. Объект один и он кластерный, поэтому список
// гейтится одним вопросом `system_admin` @ `cluster`, а арендатор без права
// получает отказ, а не пустую страницу.
const adminPool = "AddressPool is an admin RPC gated on system_admin @ cluster: a cluster-wide " +
	"pool inventory with no per-object grants to narrow to. Published on the public listener " +
	"(ADM-1 S1) — what closes it is the caller's grant, not the listener. The exclusion expires " +
	"with its method — retire the RPC and this entry becomes a finding."

// quotaIsAProjectProperty — почему у чтения квот сужать НЕЧЕГО.
//
// Narrowing отвечает на вопрос «какие из этих объектов доступны вызывающему», и
// он осмыслен, пока у строк ответа есть ИНДИВИДУАЛЬНЫЕ владельцы. У квоты их
// нет: это свойство проекта, как его имя или метки. Проект либо читаем этим
// вызывающим, либо нет — ровно один вопрос, и его решает `viewer` на проекте
// через scope_extractor края (`vpc.quotas.list`), а не построчная проверка.
//
// Форма названа `ClusterScoped` потому, что других у гейта две, и вторая
// (`RowFilter`) описывала бы проверку, которая ничего не отсекает. Имя формы
// здесь шире её смысла: ответ project-scoped, cluster-scoped он не становится —
// и это сказано вслух, чтобы следующий читатель не вывел из имени, будто квоты
// видны всему кластеру.
//
// Запись истекает со своим методом: снимите RPC — и она станет находкой.
const quotaIsAProjectProperty = "Quota rows are a property of the project, not objects with " +
	"individual owners: there is nothing to narrow to. The project-scope Check at the edge " +
	"(viewer on project_id) is what settles access, and the proto carries it. Named " +
	"ClusterScoped only because the gate has no third shape — the answer stays project-scoped."

// vpcEnumerationSources names the DECLARED types through which kacho-vpc asks the
// authorization question, so that the enumerate-then-narrow ban is read off their
// method sets instead of being a hand-written list of the forms someone already met.
//
// Neither enumerates today. That is what the declaration is for: the first method
// added to either that answers with a set of identifiers ([]string) is banned inside
// every narrowing listing the day it is written, rather than after the incident that
// revealed it (#651, #684).
var vpcEnumerationSources = []listfiltergate.EnumerationSource{
	// vpc's own client to kaname: "may this subject act on this object"
	// (InternalIAMService.Check). One method along from it is "which objects may this
	// subject act on" — the form the ban must already refuse when it is written.
	{Dir: "internal/check", Type: "IAMCheckClient", Role: listfiltergate.AsksVerdicts},
	// The SHARED narrow port to kaname's AuthorizeService, resolved from the
	// module root because it is foundation rather than service code. It is the
	// shortest path from "narrow this page" to "enumerate the universe": the RPC it
	// fronts is the one that enumerates (AuthorizeService.ListObjects), so a profile
	// watching only its own client would leave the likelier door unwatched.
	{Dir: "pkg/listnarrow", Type: "AuthorizeClient", Role: listfiltergate.AsksVerdicts, Shared: true},
}

// Profile describes kacho-vpc to the analyser.
var Profile = listfiltergate.Profile{
	Service:    "vpc",
	AnchorRoot: "internal/apps/kacho/api",
	// One package per resource, all declaring the same transport type.
	PerPackage:     true,
	ReceiverSuffix: "Handler",
	// PublicHandler — второй транспорт ОДНОГО ресурса, пула адресов: его
	// административная поверхность опубликована на внешнем слушателе (ADM-1 S1),
	// а внутренний транспорт живёт рядом до стадии S3. Оба отдают страницу
	// вызывающему, поэтому оба обязаны судиться; тип, которого профиль не знает,
	// выпал бы из набора ресурсов целиком и остался бы неосуждённым при зелёном
	// гейте. Запись истекает сама: имя, которого не несёт ни одно объявление
	// `List*`, гейт объявляет находкой.
	ExtraReceivers: []string{"PublicHandler"},
	Filters:        []string{"listnarrow.Page", "listnarrow.IDs"},
	Banned:         []string{"ListAllowedIDs", "ListObjects"},
	// Where the ban actually comes FROM (#684). Until this was declared the ban was
	// the two hand-written names and nothing else, and the gate said so on every run:
	// "no enumeration source declared". That line was not a warning about a defect —
	// it was a statement that the service was UNWATCHED for the form that lived in
	// iam for months before #651 found it.
	//
	// Neither surface named here enumerates today, and that is the point: a list of
	// names refuses only the forms someone has already met, so the ban has to arrive
	// BEFORE the first caller. The method set of each is read on every run; the first
	// method added to either that answers with a set of identifiers is banned inside
	// every narrowing listing the day it is written, without anyone editing a list.
	//
	// Copying iam's answer here would have been wrong twice over: iam's sources are
	// its own — the store's port and a paged verdict resolver over its own tables —
	// and kacho-vpc has neither. It holds grants nowhere; every authorization
	// question it asks goes to kaname, through the ports below.
	EnumerationSources: vpcEnumerationSources,
	SubjectScopers:     []string{"ListForCaller"},
	ProtoFiles:         []string{"kacho/cloud/vpc/v1/address_service.proto"},
	FGAModel:           "kacho/cloud/iam/v1/fga_model.fga",

	Listings: map[string]listfiltergate.Listing{
		"address.List":          {Shape: listfiltergate.RowFilter},
		"cidrgroup.List":        {Shape: listfiltergate.RowFilter},
		"gateway.List":          {Shape: listfiltergate.RowFilter},
		"network.List":          {Shape: listfiltergate.RowFilter},
		"networkinterface.List": {Shape: listfiltergate.RowFilter},
		"routetable.List":       {Shape: listfiltergate.RowFilter},
		"securitygroup.List":    {Shape: listfiltergate.RowFilter},
		"subnet.List":           {Shape: listfiltergate.RowFilter},

		"address.ListOperations":          parentGate(),
		"cidrgroup.ListOperations":        parentGate(),
		"gateway.ListOperations":          parentGate(),
		"network.ListOperations":          parentGate(),
		"networkinterface.ListOperations": parentGate(),
		"routetable.ListOperations":       parentGate(),
		"securitygroup.ListOperations":    parentGate(),
		"subnet.ListOperations":           parentGate(),
		"subnet.ListUsedAddresses":        parentGate(),

		// Здесь стояли четыре объявления под-перечислений: три у сети и одно у
		// адреса. Методы СНЯТЫ с контракта (вторые пути к одному ответу с другим
		// объектом проверки прав), поэтому объявлениям больше нечего описывать, и
		// гейт правильно потребовал их снять: иначе следующий метод с тем же именем
		// унаследовал бы утверждение об энфорсменте, которого никто не проверял.
		//
		// Замена у всех четырёх — список ресурса с сужением: по сети (`network_id` в
		// белом списке фильтра) и по подсети (`subnet_id` в списочном запросе).

		"addresspool.List":          {Shape: listfiltergate.ClusterScoped, Reason: adminPool},
		"addresspool.ListAddresses": {Shape: listfiltergate.ClusterScoped, Reason: adminPool},

		"quota.List": {Shape: listfiltergate.ClusterScoped, Reason: quotaIsAProjectProperty},
	},
}

// InternalProfile describes kacho-vpc's OTHER transport package.
//
// vpc is the only service with two: the per-resource packages under
// internal/apps/kacho/api, and a flat internal/handler holding the internal
// listener's handlers. Profile above covers the first. This covers the second, and
// until it existed the gate's census read "8 resources, 21 listing methods" while
// the tree held 22 — the missing one being an RPC that returns NIC attachments for
// instance ids THE CALLER NAMES, with no per-RPC check behind it at all
// (scope_filtered), which makes it among the least suitable methods in the
// repository to be going unjudged.
//
// This is the residual the widened predicate did NOT reach: the method was outside
// the anchor root, not merely outside the name match. Worth stating plainly, because
// "we widened the predicate" is the kind of sentence that gets read as "and therefore
// everything is now seen". Coverage of the tree is a separate property from coverage
// of the names, and it has its own check — census_test.go compares what each analyser
// judged against every transport listing method in the service.
var InternalProfile = listfiltergate.Profile{
	Service:    "vpc-internal",
	AnchorRoot: "internal/handler",
	// A flat package with a handler type per resource — compute's layout.
	PerPackage:     false,
	ReceiverSuffix: "Handler",
	// "svc.ListByInstance" is the delegation into nicinternal.Service, one package
	// over, where the per-NIC narrowing lives; the analyser's walk does not leave the
	// package it is judging. Named the same way as iam's listOp.Execute and safe for
	// the same two reasons: renaming the field turns the gate RED rather than quiet,
	// and the far side is asserted by internal_nic_test.go in this package instead of
	// being assumed.
	Filters: []string{"listnarrow.Page", "listnarrow.IDs", "svc.ListByInstance"},
	Banned:  []string{"ListAllowedIDs", "ListObjects"},
	// The SAME sources as the public profile, and deliberately the same value rather
	// than a second copy: both profiles are audited against the SAME tree in one run
	// (see the command), so two lists here would be two statements about one tree —
	// and the one that stopped being edited would be the one still believed.
	EnumerationSources: vpcEnumerationSources,
	SubjectScopers:     []string{"ListForCaller"},

	Listings: map[string]listfiltergate.Listing{
		"internal_network_interface.ListByInstance": {Shape: listfiltergate.RowFilter},
	},
}
