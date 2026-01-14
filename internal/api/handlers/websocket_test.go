package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestWebSocketHub(t *testing.T) {
	hub := NewWebSocketHub()

	if hub == nil {
		t.Fatal("Expected hub to be created")
	}

	if hub.clients == nil {
		t.Error("Expected clients map to be initialized")
	}
}

func TestWebSocketHubRegisterUnregister(t *testing.T) {
	hub := NewWebSocketHub()
	topic := "test:123"

	// Create a mock client
	client := &WebSocketClient{
		send: make(chan []byte, 256),
		done: make(chan struct{}),
	}

	// Register
	hub.Register(topic, client)

	hub.mu.RLock()
	if _, ok := hub.clients[topic]; !ok {
		t.Error("Expected topic to be registered")
	}
	if _, ok := hub.clients[topic][client]; !ok {
		t.Error("Expected client to be registered under topic")
	}
	hub.mu.RUnlock()

	// Unregister
	hub.Unregister(topic, client)

	hub.mu.RLock()
	if clients, ok := hub.clients[topic]; ok {
		if _, exists := clients[client]; exists {
			t.Error("Expected client to be unregistered")
		}
	}
	hub.mu.RUnlock()
}

func TestWebSocketBroadcast(t *testing.T) {
	hub := NewWebSocketHub()
	topic := "test:456"

	// Create a mock client
	client := &WebSocketClient{
		send: make(chan []byte, 256),
		done: make(chan struct{}),
	}

	hub.Register(topic, client)

	// Broadcast a message
	msg := WSBroadcastMessage{
		Type:      WSTypeSyncProgress,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Data:      map[string]string{"test": "data"},
	}

	hub.Broadcast(topic, msg)

	// Check that message was sent
	select {
	case received := <-client.send:
		var decoded WSBroadcastMessage
		if err := json.Unmarshal(received, &decoded); err != nil {
			t.Fatalf("Failed to decode message: %v", err)
		}
		if decoded.Type != WSTypeSyncProgress {
			t.Errorf("Expected type %s, got %s", WSTypeSyncProgress, decoded.Type)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected to receive message")
	}

	// Cleanup
	hub.Unregister(topic, client)
}

func TestBroadcastSyncProgress(t *testing.T) {
	hub := NewWebSocketHub()
	planID := "plan-123"
	topic := "sync:" + planID

	client := &WebSocketClient{
		send: make(chan []byte, 256),
		done: make(chan struct{}),
	}

	hub.Register(topic, client)

	data := SyncProgressData{
		PlanID:           planID,
		TaskID:           "task-1",
		TaskType:         "database",
		Name:             "PostgreSQL Main",
		Status:           "running",
		Progress:         50,
		BytesTransferred: 1024 * 1024,
		BytesTotal:       2 * 1024 * 1024,
		ItemsCompleted:   500,
		ItemsTotal:       1000,
	}

	hub.BroadcastSyncProgress(planID, data)

	select {
	case received := <-client.send:
		var decoded WSBroadcastMessage
		if err := json.Unmarshal(received, &decoded); err != nil {
			t.Fatalf("Failed to decode message: %v", err)
		}
		if decoded.Type != WSTypeSyncProgress {
			t.Errorf("Expected type %s, got %s", WSTypeSyncProgress, decoded.Type)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected to receive sync progress message")
	}

	hub.Unregister(topic, client)
}

func TestBroadcastDeployProgress(t *testing.T) {
	hub := NewWebSocketHub()
	deploymentID := "deploy-789"
	topic := "deploy:" + deploymentID

	client := &WebSocketClient{
		send: make(chan []byte, 256),
		done: make(chan struct{}),
	}

	hub.Register(topic, client)

	data := DeployProgressData{
		DeploymentID: deploymentID,
		Phase:        "Starting services",
		PhaseIndex:   3,
		TotalPhases:  5,
		Status:       "running",
		Message:      "Deploying PostgreSQL...",
		Progress:     60,
	}

	hub.BroadcastDeployProgress(deploymentID, data)

	select {
	case received := <-client.send:
		var decoded WSBroadcastMessage
		if err := json.Unmarshal(received, &decoded); err != nil {
			t.Fatalf("Failed to decode message: %v", err)
		}
		if decoded.Type != WSTypeDeployProgress {
			t.Errorf("Expected type %s, got %s", WSTypeDeployProgress, decoded.Type)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected to receive deploy progress message")
	}

	hub.Unregister(topic, client)
}

func TestWebSocketMessageTypes(t *testing.T) {
	types := []WSMessageType{
		WSTypeSyncProgress,
		WSTypeSyncComplete,
		WSTypeSyncError,
		WSTypeDeployProgress,
		WSTypeDeployComplete,
		WSTypeDeployError,
		WSTypeExportProgress,
		WSTypeExportComplete,
		WSTypeExportError,
		WSTypeHealthCheck,
		WSTypeLog,
		WSTypePing,
		WSTypePong,
	}

	for _, msgType := range types {
		if msgType == "" {
			t.Error("Message type should not be empty")
		}
	}
}

func TestWebSocketUpgraderConfig(t *testing.T) {
	// Test that upgrader is configured correctly
	if wsUpgrader.ReadBufferSize != 1024 {
		t.Errorf("Expected read buffer size 1024, got %d", wsUpgrader.ReadBufferSize)
	}

	if wsUpgrader.WriteBufferSize != 1024 {
		t.Errorf("Expected write buffer size 1024, got %d", wsUpgrader.WriteBufferSize)
	}

	// Test check origin (should allow all in development)
	req := httptest.NewRequest("GET", "/ws/test", nil)
	if !wsUpgrader.CheckOrigin(req) {
		t.Error("Expected check origin to return true")
	}
}

func TestHandleSyncWebSocketMissingID(t *testing.T) {
	req := httptest.NewRequest("GET", "/ws/sync/", nil)
	rec := httptest.NewRecorder()

	// Create a handler that simulates chi router behavior
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate no planId in URL
		HandleSyncWebSocket(w, r)
	})

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rec.Code)
	}
}

func TestHandleDeployWebSocketMissingID(t *testing.T) {
	req := httptest.NewRequest("GET", "/ws/deploy/", nil)
	rec := httptest.NewRecorder()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		HandleDeployWebSocket(w, r)
	})

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rec.Code)
	}
}

func TestHandleExportWebSocketMissingID(t *testing.T) {
	req := httptest.NewRequest("GET", "/ws/export/", nil)
	rec := httptest.NewRecorder()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		HandleExportWebSocket(w, r)
	})

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rec.Code)
	}
}

// TestWebSocketIntegration is a basic integration test for WebSocket
// This requires a real WebSocket connection, so it's more of an example
func TestWebSocketIntegration(t *testing.T) {
	// Skip if short tests
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Upgrade to WebSocket
		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("Failed to upgrade: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()

		// Send a test message
		msg := WSBroadcastMessage{
			Type:      WSTypePong,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Data:      nil,
		}
		data, _ := json.Marshal(msg)
		_ = conn.WriteMessage(websocket.TextMessage, data)
	}))
	defer server.Close()

	// Connect to the WebSocket
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Read the message
	_, message, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("Failed to read message: %v", err)
	}

	var received WSBroadcastMessage
	if err := json.Unmarshal(message, &received); err != nil {
		t.Fatalf("Failed to unmarshal message: %v", err)
	}

	if received.Type != WSTypePong {
		t.Errorf("Expected type pong, got %s", received.Type)
	}
}
