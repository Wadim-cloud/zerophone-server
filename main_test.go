package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
)

func TestRootRoute(t *testing.T) {
	router := mux.NewRouter()
	router.HandleFunc("/", handleIndex)

	req, _ := http.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestStatusRoute(t *testing.T) {
	router := mux.NewRouter()
	router.HandleFunc("/status", handleStatus)

	req, _ := http.NewRequest("GET", "/status", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestICEServersRoute(t *testing.T) {
	router := mux.NewRouter()
	router.HandleFunc("/ice/servers", handleICEServers)

	req, _ := http.NewRequest("GET", "/ice/servers", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}
