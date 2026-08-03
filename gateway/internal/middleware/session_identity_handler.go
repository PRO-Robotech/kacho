// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package middleware — the edge's session-identity endpoint for the SPA consoles.
//
// Route:
//
//	GET /iam/v1/auth/me → the caller behind the current identity-provider session,
//	                      or {"user":null} when there is none.
//
// WHY THIS FILE HOLDS ONE ROUTE AND NOT FOUR. It used to register four: a
// sign-in redirect, an authorization-code callback, this one, and a cookie-clear.
// The first two conducted an interactive sign-in ceremony against a DIFFERENT
// identity provider than the one the platform deploys, and they addressed that
// provider's path shapes. They could not be switched on: the config keys that
// enabled them were declared by ZERO deployment profiles, and the client secret
// they needed was to be filled by a provisioning Job that KAC-127 removed. The
// refusal they returned named that Job and its Secret — pointing an operator at
// objects that no longer exist.
//
// So they were a gate whose input had no producer: registered unconditionally,
// looking wired, incapable of doing anything but refuse — on every stand, for
// their whole life. `api-conventions.md` §Принято-и-проигнорировано allows three
// outcomes and not a fourth; "leave it as is" is not one of them. The outcome
// chosen was RETIREMENT, because the ceremony is conducted by the identity
// provider's own sign-in console, which IS deployed and configured, and because
// the acceptance for this sub-phase already placed a Kachō-owned sign-in console
// out of scope. Implementing them would have been NEW work under an approved
// document that says the opposite — not the repair of something broken.
//
// The cookie those two produced (a session bearer minted by the retired
// provider's token endpoint) had exactly one producer — them — and one reader at
// the edge. Producer, carrier, reader and its logout-time cleanup were retired
// together, so no reader is left addressing an input nobody can emit.
//
// WHAT SURVIVED, AND WHY IT IS NOT PART OF THAT CEREMONY. `/iam/v1/auth/me`
// reads the session of the provider the platform ACTUALLY deploys and is called
// by four consoles plus the shared console library. It never took part in the
// retired flow: it resolves a live session cookie to a Kachō principal. The
// consoles' own sign-in already goes to the deployed provider's self-service
// flow, so nothing here is the entry point of a ceremony.
package middleware

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
)

// AdminChecker — port для проверки system-admin.
type AdminChecker interface {
	IsSystemAdmin(ctx context.Context, subject string) (bool, error)
}

// SessionIdentityHandler serves the edge's single session-identity route.
type SessionIdentityHandler struct {
	logger *slog.Logger
	// kratos resolves the deployed identity provider's session cookie. When nil
	// the route answers anonymous — it never falls back to another carrier.
	kratos        *KratosClient
	subjectLookup SubjectLookuper // resolves identity.id → User/SA mirror in kacho-iam
	adminCheck    AdminChecker    // optional admin-tuple lookup
}

func NewSessionIdentityHandler(logger *slog.Logger) *SessionIdentityHandler {
	return &SessionIdentityHandler{logger: logger}
}

// WithKratos — подключает session client + SubjectLookup для /me.
func (h *SessionIdentityHandler) WithKratos(c *KratosClient, lookup SubjectLookuper) *SessionIdentityHandler {
	h.kratos = c
	h.subjectLookup = lookup
	return h
}

// WithAdminChecker — system-admin tuple lookup для /me.
// Возвращает permissions:["*","admin"] если subject имеет соответствующий tuple.
func (h *SessionIdentityHandler) WithAdminChecker(a AdminChecker) *SessionIdentityHandler {
	h.adminCheck = a
	return h
}

// Register крепит handler на http.ServeMux. Должен вызываться ДО общего `/`.
func (h *SessionIdentityHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/iam/v1/auth/me", h.Me)
}

// Me — UI hook /me. Возвращает либо `{"user":null}` если не залогинен,
// либо `{"user":{...}}` с userinfo из сессии провайдера личности.
func (h *SessionIdentityHandler) Me(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if h.kratos != nil {
		cookieHdr := r.Header.Get("Cookie")
		if strings.Contains(cookieHdr, "ory_kratos_session") {
			res := h.kratos.Whoami(r.Context(), cookieHdr)
			if res.Active && res.IdentityID != "" {
				userObj := map[string]any{
					"id":          res.IdentityID,
					"email":       res.Email,
					"displayName": res.DisplayName,
					"subjectType": "user",
					"permissions": []string{},
				}
				// Если есть SubjectLookup — резолвим в Kachō User id (mirror).
				// Если lookuper поддерживает lazy-upsert — используем (new identity → Upsert).
				if h.subjectLookup != nil {
					var subj Subject
					var lerr error
					if kl, ok := h.subjectLookup.(KratosSubjectLookuper); ok {
						subj, lerr = kl.LookupOrUpsertFromKratos(r.Context(), res.IdentityID, res.Email, res.DisplayName)
					} else {
						subj, lerr = h.subjectLookup.LookupByExternalID(r.Context(), res.IdentityID)
					}
					if lerr == nil {
						userObj["id"] = subj.ID
						userObj["subjectType"] = subj.Type
						if subj.DisplayName != "" {
							userObj["displayName"] = subj.DisplayName
						}
						// Проверка system-admin через AdminChecker.
						// Если subject имеет admin-tuple → permissions = ["*","admin"].
						// UI ServiceSidebar показывает "Администрирование" tab по hasPermission("admin").
						if h.adminCheck != nil {
							ok, _ := h.adminCheck.IsSystemAdmin(r.Context(), subj.Type+":"+subj.ID)
							if ok {
								userObj["permissions"] = []string{"*", "admin"}
							}
						}
					}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"user": userObj})
				return
			}
		}
	}

	// Никакой второй ветки нет: единственный носитель личности на этом маршруте —
	// сессия развёрнутого провайдера. Не нашли её — отвечаем анонимом.
	_, _ = w.Write([]byte(`{"user":null}`))
}
