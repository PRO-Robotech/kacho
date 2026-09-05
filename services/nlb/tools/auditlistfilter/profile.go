// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package auditlistfilter states how kacho-nlb is laid out for the public-List
// gate. The analysis itself — and why it parses instead of grepping — lives in
// pkg/listfiltergate.
//
// # This service's shape
//
// nlb colocates transport and use-cases per resource: internal/apps/kacho/api/<res>
// holds a `Handler` whose `List` delegates to a per-RPC use-case, and that use-case
// runs the page it just read through `authzfilter.FilterVisiblePage`. The transport
// type is called `Handler` in every package, so the PACKAGE is what tells one
// resource from another; the proof of filtering is a call reachable from the List
// declaration, wherever in the package it happens to live.
//
// # What the previous gate keyed on, and why that was a defect
//
// It iterated internal/apps/kacho/api/*/list.go — that is, it recognised a resource
// by a FILE being called list.go — and then searched that file's text for
// `authzfilter.Filter` and `authzfilter.FilterVisiblePage(`. Two consequences:
//
//   - moving the List use-case into any other file of the same package (splitting a
//     package by RPC is exactly what this service already does elsewhere) removed
//     the resource from the gate's view, and its List went unjudged;
//   - a text search cannot tell a call from a sentence about a call. Deleting the
//     filter and leaving the comment that described it kept the gate green.
//
// On top of both, a missing internal/apps/kacho/api exited 0 with a message on
// stderr, so "the gate could not find the tree" and "the tree is clean" were the
// same verdict.
//
// # Declarations
//
// Nothing here is excluded, and the census says so out loud: it names every listing
// method it judged, so a passing run reads as "these six were judged" rather than as
// the word OK. `announce`, `operation` and `shared` are not excluded either — they
// declare no listing method at all, so they are outside the gate by construction
// rather than by exception.
//
// That list follows the tree and is not a fixed set: it carried a fourth name until
// that package went out with its contract (kacho#1043). The dead name is not spelled
// here, because a name in backticks reads as a coordinate and would send the next
// reader looking for a package the tree does not have. A package name outlives its
// package silently — nothing here reads this list, so nothing here can go red on it.
//
// The three ListOperations were invisible to the previous gate, which matched the
// method name `List` exactly. They are SubjectScoped: the handler delegates to a
// use-case in the same package, and that use-case narrows by operations.ListForCaller
// — the owner comes from the request context, not from an id the caller supplied. The
// analyser can see this one because the hop stays inside the package; where the
// use-case lives elsewhere (compute) the shape has to be asserted differently.
package auditlistfilter

import "github.com/PRO-Robotech/kacho/pkg/listfiltergate"

// quotaIsAProjectProperty — почему у чтения квот сужать НЕЧЕГО.
//
// Сужение отвечает на вопрос «какие из этих объектов доступны вызывающему», и он
// осмыслен, пока у строк ответа есть ИНДИВИДУАЛЬНЫЕ владельцы. У квоты их нет:
// это свойство проекта, как его имя или метки. Проект либо читаем этим
// вызывающим, либо нет — ровно один вопрос, и его решает `viewer` на проекте
// через извлечение области действия на крае, а не построчная проверка.
//
// Форма названа `ClusterScoped` потому, что других у гейта подходящих нет, а
// вторая (`RowFilter`) описывала бы проверку, которая ничего не отсекает. Имя
// формы здесь шире её смысла: ответ project-scoped, cluster-scoped он не
// становится — и это сказано вслух, чтобы следующий читатель не вывел из имени,
// будто квоты видны всему кластеру.
//
// Запись истекает со своим методом: снимите RPC — и она станет находкой.
const quotaIsAProjectProperty = "Quota rows are a property of the project, not objects with " +
	"individual owners: there is nothing to narrow to. The project-scope Check at the edge " +
	"(viewer on project_id) is what settles access, and the proto carries it. Named " +
	"ClusterScoped only because the gate has no third shape — the answer stays project-scoped."

// Profile describes kacho-nlb to the analyser.
var Profile = listfiltergate.Profile{
	Service:    "nlb",
	AnchorRoot: "internal/apps/kacho/api",
	// One package per resource, all declaring the same transport type.
	PerPackage:     true,
	ReceiverSuffix: "Handler",
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
	// and kacho-nlb has neither. It holds grants nowhere; every authorization
	// question it asks goes to kacho-iam, through the ports below.
	EnumerationSources: []listfiltergate.EnumerationSource{
		// The peer port to kacho-iam — where a new authorization RPC would land —
		// and the adapter service code actually holds. Both are named because a
		// widening could be written at either, and each expires with its own subject.
		{Dir: "internal/clients/iam", Type: "CheckClient", Role: listfiltergate.AsksVerdicts},
		{Dir: "internal/check", Type: "IAMCheckClient", Role: listfiltergate.AsksVerdicts},
		// The SHARED narrow port to kacho-iam's AuthorizeService, resolved from the
		// module root because it is foundation rather than service code. It is the
		// shortest path from "narrow this page" to "enumerate the universe": the RPC
		// it fronts is the one that enumerates (AuthorizeService.ListObjects).
		{Dir: "pkg/listnarrow", Type: "AuthorizeClient", Role: listfiltergate.AsksVerdicts, Shared: true},
	},
	SubjectScopers: []string{"ListForCaller"},

	Listings: map[string]listfiltergate.Listing{
		"quota.List": {Shape: listfiltergate.ClusterScoped, Reason: quotaIsAProjectProperty},

		"listener.List":     {Shape: listfiltergate.RowFilter},
		"loadbalancer.List": {Shape: listfiltergate.RowFilter},
		"targetgroup.List":  {Shape: listfiltergate.RowFilter},

		"listener.ListOperations":     {Shape: listfiltergate.SubjectScoped},
		"loadbalancer.ListOperations": {Shape: listfiltergate.SubjectScoped},
		"targetgroup.ListOperations":  {Shape: listfiltergate.SubjectScoped},
	},
}
