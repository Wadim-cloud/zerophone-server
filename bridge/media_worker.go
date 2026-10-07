package bridge

import (
	"fmt"
	"net"
	"sync"
	"time"
)

type MediaWorkerState string

const (
	MediaWorkerInit    MediaWorkerState = "INIT"
	MediaWorkerRunning MediaWorkerState = "RUNNING"
	MediaWorkerStopped MediaWorkerState = "STOPPED"
	MediaWorkerFailed  MediaWorkerState = "FAILED"
)

type MediaWorker struct {
	CallID string           `json:"call_id"`
	State  MediaWorkerState `json:"state"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	LocalRTPPort int    `json:"local_rtp_port"`
	LocalRTPAddr string `json:"local_rtp_addr"`

	LastError string `json:"last_error,omitempty"`

	conn *net.UDPConn
	mu   sync.RWMutex
}

func NewMediaWorker(callID string) *MediaWorker {
	now := time.Now()
	return &MediaWorker{
		CallID:    callID,
		State:     MediaWorkerInit,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func (w *MediaWorker) Start() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.State == MediaWorkerRunning {
		return nil
	}

	addr, err := net.ResolveUDPAddr("udp", "0.0.0.0:0")
	if err != nil {
		w.State = MediaWorkerFailed
		w.LastError = err.Error()
		w.UpdatedAt = time.Now()
		return err
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		w.State = MediaWorkerFailed
		w.LastError = err.Error()
		w.UpdatedAt = time.Now()
		return err
	}

	w.conn = conn
	w.LocalRTPPort = conn.LocalAddr().(*net.UDPAddr).Port
	w.LocalRTPAddr = fmt.Sprintf("0.0.0.0:%d", w.LocalRTPPort)
	w.State = MediaWorkerRunning
	w.LastError = ""
	w.UpdatedAt = time.Now()
	return nil
}

func (w *MediaWorker) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.conn != nil {
		_ = w.conn.Close()
		w.conn = nil
	}
	if w.State != MediaWorkerStopped {
		w.State = MediaWorkerStopped
		w.UpdatedAt = time.Now()
	}
}

func (w *MediaWorker) Snapshot() MediaWorker {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return MediaWorker{
		CallID:       w.CallID,
		State:        w.State,
		CreatedAt:    w.CreatedAt,
		UpdatedAt:    w.UpdatedAt,
		LocalRTPPort: w.LocalRTPPort,
		LocalRTPAddr: w.LocalRTPAddr,
		LastError:    w.LastError,
	}
}

type MediaWorkerManager struct {
	mu      sync.RWMutex
	workers map[string]*MediaWorker
}

func NewMediaWorkerManager() *MediaWorkerManager {
	return &MediaWorkerManager{
		workers: make(map[string]*MediaWorker),
	}
}

func (m *MediaWorkerManager) EnsureStarted(callID string) (*MediaWorker, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	w, ok := m.workers[callID]
	if !ok {
		w = NewMediaWorker(callID)
		m.workers[callID] = w
	}
	if err := w.Start(); err != nil {
		return w, err
	}
	return w, nil
}

func (m *MediaWorkerManager) Stop(callID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if w, ok := m.workers[callID]; ok {
		w.Stop()
		delete(m.workers, callID)
	}
}

func (m *MediaWorkerManager) List() []MediaWorker {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]MediaWorker, 0, len(m.workers))
	for _, w := range m.workers {
		out = append(out, w.Snapshot())
	}
	return out
}
