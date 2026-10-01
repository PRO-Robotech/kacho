// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package anonmail

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"google.golang.org/grpc/codes"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// ProofHeader — заголовок доказательства: `<challenge>:<nonce>` (Р5).
const ProofHeader = "X-Kacho-Proof"

// Значения ответов звена (приёмка NTF-2, Р5) — часть контракта.
const (
	textChallenge   = "proof of work required"
	textRateLimited = "too many requests"
	textUnavailable = "request limiter is unavailable"

	reasonChallenge   = "PROOF_OF_WORK_REQUIRED"
	reasonRateLimited = "RATE_LIMITED"

	// errorDomain — домен причин отказа звена: решение края.
	errorDomain = "api-gateway.kacho.cloud"

	errorInfoType = "type.googleapis.com/google.rpc.ErrorInfo"
)

// GateConfig — провязка звена. Все поля обязательны.
type GateConfig struct {
	Store  Store
	PoW    *PoW
	Limits config.AnonMailLimits
	// ClientIP — оператор клиентского адреса края (`ContextExtractor.ClientIP`):
	// тот же адрес ретрансляция отдаёт службе в `X-Forwarded-For` (CX2-12).
	ClientIP func(*http.Request) string
	// Now — часы звена: решение, срок вызова и окна судят их.
	Now    func() time.Time
	Logger *slog.Logger
}

// Stats — наблюдаемые величины звена.
type Stats struct {
	// StoreUnavailable — ответов 503 «хранилище ограничителя недоступно» за
	// жизнь процесса (`kacho_api_gateway_anon_mail_store_unavailable_total`).
	StoreUnavailable uint64
}

// Gate — звено-ограничитель.
type Gate struct {
	cfg         GateConfig
	unavailable atomic.Uint64
}

// NewGate собирает звено. Неполная провязка — ошибка сборки корня: звено,
// отвечающее «пропустить» за отсутствием части, и есть ветка «не смог
// спросить → пропустить», которой нет.
func NewGate(cfg GateConfig) (*Gate, error) {
	switch {
	case cfg.Store == nil:
		return nil, errors.New("anonmail gate: store is required")
	case cfg.PoW == nil:
		return nil, errors.New("anonmail gate: proof-of-work issuer is required")
	case cfg.ClientIP == nil:
		return nil, errors.New("anonmail gate: client address operator is required")
	case cfg.Now == nil:
		return nil, errors.New("anonmail gate: clock is required")
	case cfg.Logger == nil:
		return nil, errors.New("anonmail gate: logger is required")
	case cfg.Limits.Source.Hard < 1 || cfg.Limits.PoWBits.Base < 1 || cfg.Limits.Global.Burst < 1:
		return nil, errors.New("anonmail gate: limits are not resolved — build them with config.ResolveEdgeLimits")
	}
	return &Gate{cfg: cfg}, nil
}

// Stats — снимок величин звена.
func (g *Gate) Stats() Stats { return Stats{StoreUnavailable: g.unavailable.Load()} }

// Wrap ставит звено перед next (ретрансляцией полосы формы).
func (g *Gate) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.serve(w, r, next)
	})
}

func (g *Gate) serve(w http.ResponseWriter, r *http.Request, next http.Handler) {
	keys, err := KeysFor(g.cfg.ClientIP(r))
	if err != nil {
		// Адрес не выведен — решать нечем; закрытый отказ тем же ответом.
		g.cfg.Logger.Warn("anon mail gate: client address is not keyable; refusing", "path", r.URL.Path)
		g.writeUnavailable(w)
		return
	}
	req := Request{Keys: keys, Now: g.cfg.Now()}
	if h := r.Header.Get(ProofHeader); h != "" {
		if p, err := g.cfg.PoW.Verify(h); err == nil {
			req.Proof = &p
		} else {
			req.ProofRejected = true
		}
	}
	v := g.cfg.Store.Decide(r.Context(), req)
	switch v.Outcome {
	case Pass:
		next.ServeHTTP(w, r)
	case Challenge:
		ch, err := g.cfg.PoW.Mint(v.Bits)
		if err != nil {
			g.cfg.Logger.Error("anon mail gate: challenge mint failed; refusing", "err", err)
			g.writeUnavailable(w)
			return
		}
		writeChallenge(w, ch)
	case Reject:
		writeRateLimited(w, v.RetryAfter)
	default:
		g.writeUnavailable(w)
	}
}

func (g *Gate) writeUnavailable(w http.ResponseWriter) {
	g.unavailable.Add(1)
	w.Header().Set("Cache-Control", "no-store")
	middleware.WriteEdgeStatus(w, http.StatusServiceUnavailable, codes.Unavailable, textUnavailable)
}

// errorInfo — `google.rpc.ErrorInfo` в форме grpc-gateway; порядок полей —
// часть значения.
type errorInfo struct {
	Type     string            `json:"@type"`
	Reason   string            `json:"reason"`
	Domain   string            `json:"domain"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type statusWithInfo struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Details []errorInfo `json:"details"`
}

func writeStatusWithInfo(w http.ResponseWriter, msg string, info errorInfo) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusTooManyRequests)
	_ = json.NewEncoder(w).Encode(statusWithInfo{
		Code: int(codes.ResourceExhausted), Message: msg, Details: []errorInfo{info},
	})
}

// writeChallenge — вызов: 429, code 8, `proof of work required`,
// ErrorInfo{PROOF_OF_WORK_REQUIRED, metadata: challenge, difficultyBits,
// expiresAt}.
func writeChallenge(w http.ResponseWriter, ch IssuedChallenge) {
	writeStatusWithInfo(w, textChallenge, errorInfo{
		Type: errorInfoType, Reason: reasonChallenge, Domain: errorDomain,
		Metadata: map[string]string{
			"challenge":      ch.Token,
			"difficultyBits": strconv.Itoa(ch.Bits),
			"expiresAt":      ch.ExpiresAt.UTC().Format(time.RFC3339),
		},
	})
}

// writeRateLimited — жёсткий отказ: 429, code 8, `too many requests`,
// ErrorInfo{RATE_LIMITED}, `Retry-After` в целых секундах.
func writeRateLimited(w http.ResponseWriter, retryAfter time.Duration) {
	secs := int64(retryAfter / time.Second)
	if secs < 1 {
		secs = 1
	}
	w.Header().Set("Retry-After", strconv.FormatInt(secs, 10))
	writeStatusWithInfo(w, textRateLimited, errorInfo{Type: errorInfoType, Reason: reasonRateLimited, Domain: errorDomain})
}
