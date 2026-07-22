// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// rest_router.go — RestRouteResolver implementation for the per-RPC authz
// middleware.
//
// The authz middleware's HTTP path must translate an incoming REST request
// into the canonical gRPC FQN so it can look the method up in the embedded
// permission catalog.
//
// This router matches the incoming `(method, path)` against the generated
// `generatedRestRoutes` table (built from `google.api.http` annotations in
// kacho-proto). Templates carry `{field}` placeholders and an optional
// grpc-gateway `:verb` suffix-action segment (e.g.
// `/iam/v1/users:invite`, `/vpc/v1/addressPools/{pool_id}:check`).
//
// Matching is O(routes) per request; the route table is ~322 entries and
// the decision cache (5s TTL) absorbs repeat lookups, so this is well within
// the per-RPC latency budget.
package middleware

import (
	"sort"
	"strings"
)

// RestRouter resolves REST (method, path) pairs to gRPC FQNs and exposes the
// reverse path-template map used by the ResourceExtractor's HTTP path
// strategy. It implements RestRouteResolver.
type RestRouter struct {
	// byMethod groups routes by HTTP method for a slightly tighter scan.
	byMethod map[string][]restRoute
	// fqnTemplate maps FQN -> path template (for ResourceExtractor).
	fqnTemplate map[string]string
}

// NewRestRouter builds a router from the build-time generated route table.
func NewRestRouter() *RestRouter {
	r := &RestRouter{
		byMethod:    make(map[string][]restRoute, 8),
		fqnTemplate: make(map[string]string, len(generatedRestRoutes)),
	}
	for _, rt := range generatedRestRoutes {
		r.byMethod[rt.Method] = append(r.byMethod[rt.Method], rt)
		// First binding wins for the reverse map — the primary binding is
		// emitted before additional_bindings by the extractor.
		if _, ok := r.fqnTemplate[rt.FQN]; !ok {
			r.fqnTemplate[rt.FQN] = rt.Template
		}
	}
	// Order each method's routes MOST-SPECIFIC-FIRST so first-match Resolve picks
	// the same route the grpc-gateway mux would. Since `{field=**}` deep-wildcards
	// now match multiple segments, a bare catch-all (`…/repositories/{repo=**}`)
	// would otherwise greedily swallow more-specific sub-resource routes
	// (`…/{repo=**}/referrers`, `…/{repo}/tags/{tag}`). Specificity = more literal
	// segments first, then non-deep before deep, then longer template.
	for m := range r.byMethod {
		routes := r.byMethod[m]
		sort.SliceStable(routes, func(i, j int) bool {
			return moreSpecific(routes[i].Template, routes[j].Template)
		})
	}
	return r
}

// moreSpecific reports whether template a should be tried before template b under
// most-specific-first routing.
func moreSpecific(a, b string) bool {
	la, da, sa := templSpecificity(a)
	lb, db, sb := templSpecificity(b)
	if la != lb {
		return la > lb // more literal segments = more specific
	}
	if da != db {
		return !da // a non-deep route beats a deep-wildcard route
	}
	return sa > sb // longer template = more specific
}

// templSpecificity returns the count of literal (non-placeholder) segments, whether
// the template carries a `{field=**}` deep-wildcard, and its total segment count.
func templSpecificity(template string) (literals int, hasDeep bool, segs int) {
	seg, _ := splitVerb(template)
	parts := splitPath(seg)
	segs = len(parts)
	for _, p := range parts {
		switch {
		case isDeepPlaceholder(p):
			hasDeep = true
		case isPlaceholder(p):
		default:
			literals++
		}
	}
	return literals, hasDeep, segs
}

// Resolve maps an HTTP (method, path) to a gRPC FQN. Returns ok=false when
// no route matches (unknown path) — the middleware then applies its public
// allowlist / deny-by-default policy.
func (r *RestRouter) Resolve(httpMethod, httpPath string) (string, bool) {
	httpMethod = strings.ToUpper(httpMethod)
	// Drop any query string.
	if i := strings.IndexByte(httpPath, '?'); i >= 0 {
		httpPath = httpPath[:i]
	}
	for _, rt := range r.byMethod[httpMethod] {
		if matchTemplate(rt.Template, httpPath) {
			return rt.FQN, true
		}
	}
	return "", false
}

// PathTemplates returns the FQN -> path-template map, suitable for wiring
// into NewResourceExtractor so the HTTP path strategy can pluck `{field}`
// placeholders out of the URL.
func (r *RestRouter) PathTemplates() map[string]string {
	out := make(map[string]string, len(r.fqnTemplate))
	for k, v := range r.fqnTemplate {
		out[k] = v
	}
	return out
}

// matchTemplate reports whether reqPath matches a grpc-gateway path template.
//
// Template grammar handled:
//   - literal segments               `/iam/v1/accounts`
//   - `{field}` placeholders         `/iam/v1/accounts/{account_id}`
//   - `{field=**}` deep placeholders  (rare; treated as single placeholder)
//   - `:verb` suffix action on the last segment `/iam/v1/users:invite`
func matchTemplate(template, reqPath string) bool {
	tSeg, tVerb := splitVerb(template)
	pSeg, pVerb := splitVerb(reqPath)
	if tVerb != pVerb {
		return false
	}
	tparts := splitPath(tSeg)
	pparts := splitPath(pSeg)

	// Locate a `{field=**}` deep-wildcard segment (at most one per template, per
	// grpc-gateway). It matches ONE OR MORE path segments (e.g. repository
	// "backend/api"), so it CANNOT be treated as a single placeholder — doing so
	// made every multi-segment repository RPC fail to resolve → "catalog: no entry
	// for method" → AUTHZ_DENIED (#64 follow-up).
	deep := -1
	for i, t := range tparts {
		if isDeepPlaceholder(t) {
			deep = i
			break
		}
	}

	if deep < 0 {
		// No deep wildcard: exact segment count; each `{field}` = one non-empty segment.
		if len(tparts) != len(pparts) {
			return false
		}
		return matchSegments(tparts, pparts)
	}

	// Deep wildcard: the prefix (before `**`) matches head-to-head, the suffix
	// (after `**`, e.g. `/referrers`) matches tail-to-tail, and `**` consumes the
	// ≥1 middle segments. Total path length must leave room for that middle.
	prefix, suffix := tparts[:deep], tparts[deep+1:]
	if len(pparts) < len(prefix)+1+len(suffix) {
		return false
	}
	if !matchSegments(prefix, pparts[:len(prefix)]) {
		return false
	}
	return matchSegments(suffix, pparts[len(pparts)-len(suffix):])
}

// matchSegments matches equal-length template/path slices where each `{field}`
// template segment matches any single non-empty path segment and every other
// segment must be literally equal.
func matchSegments(tparts, pparts []string) bool {
	for i, t := range tparts {
		if isPlaceholder(t) {
			if pparts[i] == "" {
				return false
			}
			continue
		}
		if t != pparts[i] {
			return false
		}
	}
	return true
}

// isDeepPlaceholder reports whether a template segment is a `{field=**}` deep
// capture (matches 1+ trailing segments), as opposed to a single-segment `{field}`.
func isDeepPlaceholder(seg string) bool {
	return isPlaceholder(seg) && strings.Contains(seg, "=**")
}

// splitVerb separates an optional grpc-gateway `:verb` suffix-action from the
// last path segment. `/iam/v1/users:invite` -> ("/iam/v1/users", "invite").
func splitVerb(p string) (string, string) {
	// The `:verb` action only ever appears on the final segment.
	slash := strings.LastIndexByte(p, '/')
	last := p
	if slash >= 0 {
		last = p[slash+1:]
	}
	if colon := strings.IndexByte(last, ':'); colon >= 0 {
		verb := last[colon+1:]
		base := p[:len(p)-len(last)] + last[:colon]
		return base, verb
	}
	return p, ""
}

// splitPath tokenizes a path into its non-empty slash-separated segments.
func splitPath(p string) []string {
	return strings.Split(strings.Trim(p, "/"), "/")
}

// isPlaceholder reports whether a template segment is a `{field}` capture.
func isPlaceholder(seg string) bool {
	return len(seg) >= 2 && seg[0] == '{' && seg[len(seg)-1] == '}'
}

// Ensure RestRouter satisfies the interface at compile time.
var _ RestRouteResolver = (*RestRouter)(nil)
