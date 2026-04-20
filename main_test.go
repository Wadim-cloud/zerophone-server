package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
)

func TestHandleStatus(t *testing.T) {
	router := mux.NewRouter()
	router.HandleFunc("/status", handleStatus)

	req, _ := http.NewRequest("GET", "/status", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestHandleNodes(t *testing.T) {
	router := mux.NewRouter()
	router.HandleFunc("/nodes", handleNodes)

	req, _ := http.NewRequest("GET", "/nodes", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestGenerateID(t *testing.T) {
	id1 := generateID()
	id2 := generateID()

	if id1 == "" || len(id1) != 16 {
		t.Errorf("Expected 16 hex chars, got %s", id1)
	}
	if id1 == id2 {
		t.Error("Generated IDs should be unique")
	}
}

func TestGetNodeName(t *testing.T) {
	name := getNodeName()
	if name == "" {
		t.Error("Node name should not be empty")
	}
}
