package bridge

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"zerophone/core"
)

type Session struct {
	CallID       string       `json:"call_id"`
	SIPFrom      string       `json:"sip_from"`
	SIPToExt     string       `json:"sip_to_ext"`
	TargetUser   string       `json:"target_user"`
	Targets      []string     `json:"targets"`
	PickupPolicy string       `json:"pickup_policy"`
	AnsweredBy   string       `json:"answered_by,omitempty"`
	CreatedAt    time.Time    `json:"created_at"`
	Media        MediaProfile `json:"media"`
}

type SIPBridge struct {
	mu sync.RWMutex

	extToUsers map[string][]string
	extPolicy  map[string]string
	sessions   map[string]Session

	router      *core.SignalRouter
	callbackURL string
	httpClient  *http.Client
	media       *MediaWorkerManager
}

type MapRequest struct {
	Extension string `json:"extension"`
	UserID    string `json:"user_id"`
	Policy    string `json:"policy,omitempty"`
}

type RegisterRequest struct {
	Extension string `json:"extension"`
	UserID    string `json:"user_id"`
	Policy    string `json:"policy,omitempty"`
}

type InviteRequest struct {
	CallID    string `json:"call_id"`
	From      string `json:"from"`
	ToExt     string `json:"to_ext"`
	ToUserID  string `json:"to_user_id,omitempty"`
	SDP       string `json:"sdp,omitempty"`
	Direction string `json:"direction,omitempty"`
}

type BYERequest struct {
	CallID string `json:"call_id"`
	From   string `json:"from"`
}

type UnregisterRequest struct {
	Extension string `json:"extension"`
	UserID    string `json:"user_id,omitempty"`
}

func NewSIPBridge(router *core.SignalRouter) *SIPBridge {
	b := &SIPBridge{
		extToUsers:  make(map[string][]string),
		extPolicy:   make(map[string]string),
		sessions:    make(map[string]Session),
		router:      router,
		callbackURL: strings.TrimSpace(os.Getenv("ZEROPHONE_SIP_CALLBACK_URL")),
		httpClient:  &http.Client{Timeout: 4 * time.Second},
		media:       NewMediaWorkerManager(),
	}
	b.attachSignalObservers()
	return b
}

func (b *SIPBridge) HandleMap(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		b.mu.RLock()
		defer b.mu.RUnlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"mappings": b.extToUsers,
			"policy":   b.extPolicy,
		})
		return
	case http.MethodPost:
		var req MapRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		req.Extension = strings.TrimSpace(req.Extension)
		req.UserID = strings.TrimSpace(req.UserID)
		if req.Extension == "" {
			http.Error(w, "extension is required", http.StatusBadRequest)
			return
		}
		b.mu.Lock()
		if req.UserID != "" {
			b.extToUsers[req.Extension] = addUniqueUser(b.extToUsers[req.Extension], req.UserID)
		}
		if p := normalizePolicy(req.Policy); p != "" {
			b.extPolicy[req.Extension] = p
		}
		b.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		return
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (b *SIPBridge) HandleSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]Session, 0, len(b.sessions))
	for _, s := range b.sessions {
		out = append(out, s)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"sessions": out,
		"count":    len(out),
	})
}

func (b *SIPBridge) HandleInvite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req InviteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.CallID == "" || req.From == "" {
		http.Error(w, "call_id and from are required", http.StatusBadRequest)
		return
	}

	target := strings.TrimSpace(req.ToUserID)
	targets := []string{}
	if target != "" {
		targets = []string{target}
	} else {
		targets = b.resolveUsers(req.ToExt)
	}
	if len(targets) == 0 {
		http.Error(w, "no ZeroPhone user mapped for destination", http.StatusNotFound)
		return
	}
	policy := b.resolvePolicy(req.ToExt)
	target = targets[0]

	b.mu.Lock()
	media := BuildMediaProfile(req.SDP)
	b.sessions[req.CallID] = Session{
		CallID:       req.CallID,
		SIPFrom:      req.From,
		SIPToExt:     req.ToExt,
		TargetUser:   target,
		Targets:      append([]string{}, targets...),
		PickupPolicy: policy,
		CreatedAt:    time.Now(),
		Media:        media,
	}
	b.mu.Unlock()

	if b.media != nil {
		if _, err := b.media.EnsureStarted(req.CallID); err != nil {
			log.Printf("[SIP-BRIDGE] media worker start failed for %s: %v", req.CallID, err)
		}
	}

	// Immediate SIP progress feedback.
	b.emitCallbackEvent(map[string]any{
		"call_id":        req.CallID,
		"type":           string(core.SignalTrying),
		"from":           "bridge",
		"target_user":    target,
		"targets":        targets,
		"pickup_policy":  policy,
		"sip_from":       req.From,
		"sip_to_ext":     req.ToExt,
		"timestamp_unix": time.Now().Unix(),
	})

	if b.router == nil {
		b.mu.Lock()
		delete(b.sessions, req.CallID)
		b.mu.Unlock()
		if b.media != nil {
			b.media.Stop(req.CallID)
		}
		http.Error(w, "signal router unavailable", http.StatusServiceUnavailable)
		return
	}

	for _, userID := range targets {
		b.router.Dispatch(core.Signal{
			CallID: req.CallID,
			From:   "sip:" + req.From,
			To:     userID,
			Type:   core.SignalInvite,
			SDP:    req.SDP,
		})
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":        "delivered",
		"target_user":   target,
		"targets":       targets,
		"pickup_policy": policy,
	})
}

func (b *SIPBridge) HandleMediaCapabilities(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"sip_ingress": map[string]any{
			"supports_audio":      true,
			"preferred_codec_mvp": "PCMU",
			"known_codecs":        []string{"PCMU", "PCMA", "OPUS"},
			"notes":               "Current bridge tracks SDP/media negotiation metadata; RTP/WebRTC relay is next milestone.",
		},
	})
}

func (b *SIPBridge) HandleMediaSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]map[string]any, 0, len(b.sessions))
	for _, s := range b.sessions {
		out = append(out, map[string]any{
			"call_id":     s.CallID,
			"sip_from":    s.SIPFrom,
			"sip_to_ext":  s.SIPToExt,
			"target_user": s.TargetUser,
			"created_at":  s.CreatedAt,
			"media":       s.Media,
		})
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"sessions": out,
		"count":    len(out),
	})
}

func (b *SIPBridge) HandleMediaWorkers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	workers := []MediaWorker{}
	if b.media != nil {
		workers = b.media.List()
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"workers": workers,
		"count":   len(workers),
	})
}

func (b *SIPBridge) HandleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	req.Extension = strings.TrimSpace(req.Extension)
	req.UserID = strings.TrimSpace(req.UserID)
	if req.Extension == "" || req.UserID == "" {
		http.Error(w, "extension and user_id are required", http.StatusBadRequest)
		return
	}
	b.mu.Lock()
	b.extToUsers[req.Extension] = addUniqueUser(b.extToUsers[req.Extension], req.UserID)
	if p := normalizePolicy(req.Policy); p != "" {
		b.extPolicy[req.Extension] = p
	}
	b.mu.Unlock()
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":    "registered",
		"extension": req.Extension,
		"user_id":   req.UserID,
		"users":     b.resolveUsers(req.Extension),
		"policy":    b.resolvePolicy(req.Extension),
	})
}

func (b *SIPBridge) HandleUnregister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req UnregisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	req.Extension = strings.TrimSpace(req.Extension)
	if req.Extension == "" {
		http.Error(w, "extension is required", http.StatusBadRequest)
		return
	}
	b.mu.Lock()
	if strings.TrimSpace(req.UserID) == "" {
		delete(b.extToUsers, req.Extension)
		delete(b.extPolicy, req.Extension)
	} else {
		users := b.extToUsers[req.Extension]
		kept := make([]string, 0, len(users))
		for _, u := range users {
			if u != req.UserID {
				kept = append(kept, u)
			}
		}
		if len(kept) == 0 {
			delete(b.extToUsers, req.Extension)
			delete(b.extPolicy, req.Extension)
		} else {
			b.extToUsers[req.Extension] = kept
		}
	}
	b.mu.Unlock()
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":    "unregistered",
		"extension": req.Extension,
		"users":     b.resolveUsers(req.Extension),
		"policy":    b.resolvePolicy(req.Extension),
	})
}

func (b *SIPBridge) HandleBYE(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req BYERequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.CallID == "" {
		http.Error(w, "call_id is required", http.StatusBadRequest)
		return
	}

	sess, ok := b.getSession(req.CallID)
	if !ok {
		http.Error(w, "unknown call_id", http.StatusNotFound)
		return
	}

	if b.router != nil {
		b.router.Dispatch(core.Signal{
			CallID: req.CallID,
			From:   "sip:" + req.From,
			To:     sess.TargetUser,
			Type:   core.SignalBye,
		})
	}
	if b.media != nil {
		b.media.Stop(req.CallID)
	}
	// Clean up session on BYE
	b.mu.Lock()
	delete(b.sessions, req.CallID)
	b.mu.Unlock()
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "delivered"})
}

func (b *SIPBridge) resolveUsers(ext string) []string {
	ext = strings.TrimSpace(ext)
	b.mu.RLock()
	defer b.mu.RUnlock()
	users := b.extToUsers[ext]
	out := make([]string, 0, len(users))
	for _, u := range users {
		u = strings.TrimSpace(u)
		if u != "" {
			out = append(out, u)
		}
	}
	return out
}

func (b *SIPBridge) resolvePolicy(ext string) string {
	ext = strings.TrimSpace(ext)
	b.mu.RLock()
	defer b.mu.RUnlock()
	p := normalizePolicy(b.extPolicy[ext])
	if p == "" {
		return "single"
	}
	return p
}

func (b *SIPBridge) getSession(callID string) (Session, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	s, ok := b.sessions[callID]
	return s, ok
}

func (b *SIPBridge) attachSignalObservers() {
	if b.router == nil {
		return
	}
	for _, t := range []core.SignalType{
		core.SignalTrying,
		core.SignalRinging,
		core.SignalOK,
		core.SignalReject,
		core.SignalBye,
	} {
		sigType := t
		b.router.On(sigType, func(s core.Signal) {
			b.forwardEvent(s)
			if sigType == core.SignalReject || sigType == core.SignalBye {
				b.mu.Lock()
				delete(b.sessions, s.CallID)
				b.mu.Unlock()
				if b.media != nil {
					b.media.Stop(s.CallID)
				}
			}
		})
	}
}

func (b *SIPBridge) forwardEvent(s core.Signal) {
	sess, ok := b.getSession(s.CallID)
	if !ok {
		return
	}
	// Only forward events from ZeroPhone side for SIP sessions.
	if !strings.HasPrefix(s.From, "user-") && s.From != sess.TargetUser {
		return
	}
	// Single-answer policy: first OK wins.
	if s.Type == core.SignalOK && sess.PickupPolicy == "single" {
		if sess.AnsweredBy == "" {
			sess.AnsweredBy = s.From
			b.mu.Lock()
			b.sessions[s.CallID] = sess
			b.mu.Unlock()
		} else if sess.AnsweredBy != s.From {
			return
		}
	}
	// Ignore BYE/REJECT from non-owner in single mode once answered.
	if sess.PickupPolicy == "single" && sess.AnsweredBy != "" &&
		(s.Type == core.SignalBye || s.Type == core.SignalReject) && s.From != sess.AnsweredBy {
		return
	}

	event := map[string]any{
		"call_id":        s.CallID,
		"type":           string(s.Type),
		"from":           s.From,
		"target_user":    sess.TargetUser,
		"targets":        sess.Targets,
		"pickup_policy":  sess.PickupPolicy,
		"answered_by":    sess.AnsweredBy,
		"sip_from":       sess.SIPFrom,
		"sip_to_ext":     sess.SIPToExt,
		"sdp":            s.SDP,
		"candidate":      s.Candidate,
		"timestamp_unix": time.Now().Unix(),
	}
	b.emitCallbackEvent(event)
}

func normalizePolicy(in string) string {
	p := strings.ToLower(strings.TrimSpace(in))
	switch p {
	case "single", "shared":
		return p
	default:
		return ""
	}
}

func addUniqueUser(users []string, userID string) []string {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return users
	}
	if slices.Contains(users, userID) {
		return users
	}
	return append(users, userID)
}

func (b *SIPBridge) emitCallbackEvent(event map[string]any) {
	if b.callbackURL == "" {
		log.Printf("[SIP-BRIDGE] event: %+v", event)
		return
	}

	payload, err := json.Marshal(event)
	if err != nil {
		log.Printf("[SIP-BRIDGE] failed to marshal callback payload: %v", err)
		return
	}
	req, err := http.NewRequest(http.MethodPost, b.callbackURL, bytes.NewReader(payload))
	if err != nil {
		log.Printf("[SIP-BRIDGE] failed to create callback request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.httpClient.Do(req)
	if err != nil {
		log.Printf("[SIP-BRIDGE] callback HTTP error for call %s type %s: %v", event["call_id"], event["type"], err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("[SIP-BRIDGE] callback failed for call %s type %s: HTTP %d", event["call_id"], event["type"], resp.StatusCode)
	} else {
		log.Printf("[SIP-BRIDGE] callback sent for call %s type %s", event["call_id"], event["type"])
	}
}
