package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/security"
)

type actorContextKey struct{}

type requestActor struct {
	Subject string
	Role    security.Role
}

type roleResolver func(*http.Request) security.Role

type auditAction struct {
	Action   string
	Resource string
	Mutates  bool
}

type auditResolver func(*http.Request) auditAction

func securityConfigFromEnv() (secret, issuer, audience string, accessTTL, refreshTTL time.Duration, adminUsername, adminPassword string, adminRole security.Role, rateCapacity int, rateRefill time.Duration) {
	role, err := security.ParseRole(getenv("ECOMMERCE_ADMIN_ROLE", string(security.RoleAdmin)))
	if err != nil {
		role = security.RoleAdmin
	}
	capacity := parseIntEnv("ECOMMERCE_RATE_LIMIT_CAPACITY", 60)
	return strings.TrimSpace(os.Getenv("ECOMMERCE_JWT_SECRET")),
		getenv("ECOMMERCE_JWT_ISSUER", "agentic-ecommerce"),
		getenv("ECOMMERCE_JWT_AUDIENCE", "mc-api"),
		parseDurationEnv("ECOMMERCE_JWT_ACCESS_TTL", 15*time.Minute),
		parseDurationEnv("ECOMMERCE_REFRESH_TTL", 24*time.Hour),
		strings.TrimSpace(os.Getenv("ECOMMERCE_ADMIN_USERNAME")),
		os.Getenv("ECOMMERCE_ADMIN_PASSWORD"),
		role,
		capacity,
		parseDurationEnv("ECOMMERCE_RATE_LIMIT_REFILL", time.Minute)
}

func (s *server) configureSecurity() {
	if s.cfg.jwtSecret != "" {
		manager, err := security.NewTokenManager(security.TokenConfig{
			Secret:    s.cfg.jwtSecret,
			Issuer:    s.cfg.jwtIssuer,
			Audience:  s.cfg.jwtAudience,
			AccessTTL: s.cfg.jwtAccessTTL,
		})
		if err != nil {
			s.log.Warn("jwt auth disabled", "error", err)
		} else {
			s.tokenManager = manager
			s.sessions = security.NewInMemoryRefreshSessionStore(time.Now)
		}
	}
	rateConfig := security.TokenBucketConfig{
		Capacity:       s.cfg.rateLimitCapacity,
		RefillInterval: s.cfg.rateLimitRefill,
	}
	if redisAddr := strings.TrimSpace(os.Getenv("ECOMMERCE_REDIS_ADDR")); redisAddr != "" {
		s.rateLimiter = security.NewRedisTokenBucket(redisAddr, os.Getenv("ECOMMERCE_REDIS_DB"), rateConfig)
		s.rateLimitFallback = security.NewInMemoryTokenBucket(rateConfig)
		return
	}
	s.rateLimiter = security.NewInMemoryTokenBucket(rateConfig)
}

func (s *server) withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		next.ServeHTTP(w, r)
	})
}

func (s *server) withRBAC(resolve roleResolver, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		required := resolve(r)
		if required == "" || r.Method == http.MethodOptions {
			next(w, r)
			return
		}
		actor, ok := s.authenticateRequest(r)
		if !ok {
			if s.tokenManager == nil && s.cfg.apiToken == "" && s.cfg.jwtSecret == "" {
				next(w, r)
				return
			}
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorised"})
			return
		}
		if !actor.Role.Allows(required) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		ctx := context.WithValue(r.Context(), actorContextKey{}, actor)
		next(w, r.WithContext(ctx))
	}
}

func (s *server) authenticateRequest(r *http.Request) (requestActor, bool) {
	value := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(value, "Bearer ") {
		return requestActor{}, false
	}
	token := strings.TrimSpace(strings.TrimPrefix(value, "Bearer "))
	if token == "" {
		return requestActor{}, false
	}
	if s.tokenManager != nil {
		claims, err := s.tokenManager.VerifyAccessToken(token)
		if err == nil {
			return requestActor{Subject: claims.Subject, Role: claims.Role}, true
		}
	}
	if s.cfg.apiToken != "" && subtle.ConstantTimeCompare([]byte(token), []byte(s.cfg.apiToken)) == 1 {
		return requestActor{Subject: "legacy-api-token", Role: security.RoleAdmin}, true
	}
	return requestActor{}, false
}

func (s *server) withRateLimit(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.rateLimiter == nil || r.Method == http.MethodOptions {
			next(w, r)
			return
		}
		decision, err := s.rateLimiter.Allow(r.Context(), rateLimitKey(r))
		if err != nil {
			if s.rateLimitFallback == nil {
				s.log.Error("rate limit", "error", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
				return
			}
			s.log.Warn("rate limit primary failed; using fallback", "error", err)
			decision, err = s.rateLimitFallback.Allow(r.Context(), rateLimitKey(r))
			if err != nil {
				s.log.Error("rate limit fallback", "error", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
				return
			}
		}
		if !decision.Allowed {
			if decision.RetryAfter > 0 {
				w.Header().Set("Retry-After", strconv.Itoa(int(decision.RetryAfter.Seconds()+0.5)))
			}
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate_limited"})
			return
		}
		next(w, r)
	}
}

func rateLimitKey(r *http.Request) string {
	if auth := strings.TrimSpace(r.Header.Get("Authorization")); auth != "" {
		sum := sha256.Sum256([]byte(auth))
		return "auth:" + base64.RawURLEncoding.EncodeToString(sum[:])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || host == "" {
		host = r.RemoteAddr
	}
	if host == "" {
		host = "unknown"
	}
	return "ip:" + host
}

func (s *server) withAudit(resolve auditResolver, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		action := resolve(r)
		if !action.Mutates {
			next(w, r)
			return
		}
		rec := &responseStatusRecorder{ResponseWriter: w}
		next(rec, r)
		status := rec.status
		if status == 0 {
			status = http.StatusOK
		}
		s.recordAuditEvent(r, action, status)
	}
}

func (s *server) recordAuditEvent(r *http.Request, action auditAction, status int) {
	logger := s.log
	if logger == nil {
		logger = slog.Default()
	}
	actor := requestActor{Subject: "anonymous", Role: security.RoleViewer}
	if fromContext, ok := r.Context().Value(actorContextKey{}).(requestActor); ok {
		actor = fromContext
	}
	attrs := []any{
		"actor", actor.Subject,
		"role", string(actor.Role),
		"action", action.Action,
		"resource", action.Resource,
		"status", status,
		"method", r.Method,
		"path", r.URL.Path,
	}
	if requestID, ok := r.Context().Value(requestIDContextKey{}).(string); ok && requestID != "" {
		attrs = append(attrs, "request_id", requestID)
	}
	logger.InfoContext(r.Context(), "audit.event", attrs...)
}

func publicRole(*http.Request) security.Role {
	return ""
}

func productsRole(r *http.Request) security.Role {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/products"), "/")
	if path == "" && r.Method == http.MethodGet {
		return ""
	}
	if path != "" && r.Method == http.MethodGet && !strings.HasSuffix(path, "/ai-suggestions") {
		return ""
	}
	return security.RoleOperator
}

func ordersRole(r *http.Request) security.Role {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/orders"), "/")
	if path == "" && r.Method == http.MethodPost {
		return ""
	}
	if strings.HasSuffix(path, "/status") {
		return security.RoleOperator
	}
	return security.RoleViewer
}

func syncRole(r *http.Request) security.Role {
	if r.Method == http.MethodGet {
		return security.RoleViewer
	}
	return security.RoleOperator
}

func agentsRole(r *http.Request) security.Role {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/agents"), "/")
	if strings.HasSuffix(path, "/run") && r.Method == http.MethodPost {
		return security.RoleOperator
	}
	return security.RoleViewer
}

func viewerRole(*http.Request) security.Role {
	return security.RoleViewer
}

func operatorRole(*http.Request) security.Role {
	return security.RoleOperator
}

func productsAuditAction(r *http.Request) auditAction {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/products"), "/")
	switch {
	case path == "" && r.Method == http.MethodPost:
		return auditAction{Action: "product.create", Resource: "product", Mutates: true}
	case path != "" && r.Method == http.MethodPut:
		return auditAction{Action: "product.update", Resource: path, Mutates: true}
	case path != "" && r.Method == http.MethodDelete:
		return auditAction{Action: "product.delete", Resource: path, Mutates: true}
	case strings.HasSuffix(path, "/generate-description") && r.Method == http.MethodPost:
		return auditAction{Action: "product.generate_description", Resource: strings.TrimSuffix(path, "/generate-description"), Mutates: true}
	case strings.HasSuffix(path, "/compliance-check") && r.Method == http.MethodPost:
		return auditAction{Action: "product.compliance_check", Resource: strings.TrimSuffix(path, "/compliance-check"), Mutates: true}
	case strings.HasSuffix(path, "/seo-suggestions") && r.Method == http.MethodPost:
		return auditAction{Action: "product.seo_suggestions", Resource: strings.TrimSuffix(path, "/seo-suggestions"), Mutates: true}
	default:
		return auditAction{}
	}
}

func ordersAuditAction(r *http.Request) auditAction {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/orders"), "/")
	if path == "" && r.Method == http.MethodPost {
		return auditAction{Action: "order.create", Resource: "order", Mutates: true}
	}
	if strings.HasSuffix(path, "/status") && r.Method == http.MethodPatch {
		return auditAction{Action: "order.status_update", Resource: strings.TrimSuffix(path, "/status"), Mutates: true}
	}
	return auditAction{}
}

func cartAuditAction(r *http.Request) auditAction {
	if r.Method != http.MethodPut {
		return auditAction{}
	}
	sessionID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/cart/"), "/")
	return auditAction{Action: "cart.replace", Resource: sessionID, Mutates: true}
}

func syncAuditAction(r *http.Request) auditAction {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/sync"), "/")
	switch {
	case strings.HasPrefix(path, "conflicts/") && strings.HasSuffix(path, "/resolve") && r.Method == http.MethodPost:
		return auditAction{Action: "sync.conflict_resolve", Resource: path, Mutates: true}
	case strings.HasPrefix(path, "products/") && strings.HasSuffix(path, "/publish") && r.Method == http.MethodPost:
		return auditAction{Action: "sync.product_publish", Resource: path, Mutates: true}
	default:
		return auditAction{}
	}
}

func agentAuditAction(r *http.Request) auditAction {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/agents"), "/")
	if strings.HasSuffix(path, "/run") && r.Method == http.MethodPost {
		return auditAction{Action: "agent.run", Resource: strings.TrimSuffix(path, "/run"), Mutates: true}
	}
	return auditAction{}
}

func workflowAuditAction(r *http.Request) auditAction {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/workflows"), "/")
	switch {
	case path == "product-publish" && r.Method == http.MethodPost:
		return auditAction{Action: "workflow.product_publish.start", Resource: "product-publish", Mutates: true}
	case path == "media-processing" && r.Method == http.MethodPost:
		return auditAction{Action: "workflow.media_processing.start", Resource: "media-processing", Mutates: true}
	case strings.HasSuffix(path, "/signals/review") && r.Method == http.MethodPost:
		return auditAction{Action: "workflow.product_publish.review_signal", Resource: strings.TrimSuffix(path, "/signals/review"), Mutates: true}
	default:
		return auditAction{}
	}
}

func webhookAuditAction(action string) auditResolver {
	return func(r *http.Request) auditAction {
		if r.Method != http.MethodPost {
			return auditAction{}
		}
		return auditAction{Action: action, Resource: "woocommerce", Mutates: true}
	}
}

func constantTimeStringEqual(a, b string) bool {
	aHash := sha256.Sum256([]byte(a))
	bHash := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(aHash[:], bHash[:]) == 1
}

func parseIntEnv(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
