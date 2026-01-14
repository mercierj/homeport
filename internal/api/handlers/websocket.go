package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
)

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// Allow all origins in development
		// TODO: Configure allowed origins in production
		return true
	},
}

// WSMessageType represents the type of WebSocket message
type WSMessageType string

const (
	WSTypeSyncProgress   WSMessageType = "sync_progress"
	WSTypeSyncComplete   WSMessageType = "sync_complete"
	WSTypeSyncError      WSMessageType = "sync_error"
	WSTypeDeployProgress WSMessageType = "deploy_progress"
	WSTypeDeployComplete WSMessageType = "deploy_complete"
	WSTypeDeployError    WSMessageType = "deploy_error"
	WSTypeExportProgress WSMessageType = "export_progress"
	WSTypeExportComplete WSMessageType = "export_complete"
	WSTypeExportError    WSMessageType = "export_error"
	WSTypeHealthCheck    WSMessageType = "health_check"
	WSTypeLog            WSMessageType = "log"
	WSTypePing           WSMessageType = "ping"
	WSTypePong           WSMessageType = "pong"
)

// WSBroadcastMessage represents a WebSocket broadcast message
type WSBroadcastMessage struct {
	Type      WSMessageType `json:"type"`
	Timestamp string        `json:"timestamp"`
	Data      interface{}   `json:"data"`
}

// SyncProgressData represents sync progress data
type SyncProgressData struct {
	PlanID           string `json:"plan_id"`
	TaskID           string `json:"task_id"`
	TaskType         string `json:"task_type"`
	Name             string `json:"name"`
	Status           string `json:"status"`
	Progress         int    `json:"progress"`
	BytesTransferred int64  `json:"bytes_transferred"`
	BytesTotal       int64  `json:"bytes_total"`
	ItemsCompleted   int    `json:"items_completed"`
	ItemsTotal       int    `json:"items_total"`
	Message          string `json:"message,omitempty"`
}

// SyncCompleteData represents sync completion data
type SyncCompleteData struct {
	PlanID         string `json:"plan_id"`
	Duration       string `json:"duration"`
	TasksCompleted int    `json:"tasks_completed"`
	TasksFailed    int    `json:"tasks_failed"`
	TotalBytes     int64  `json:"total_bytes"`
}

// DeployProgressData represents deploy progress data
type DeployProgressData struct {
	DeploymentID string `json:"deployment_id"`
	Phase        string `json:"phase"`
	PhaseIndex   int    `json:"phase_index"`
	TotalPhases  int    `json:"total_phases"`
	Status       string `json:"status"`
	Message      string `json:"message"`
	Progress     int    `json:"progress"`
}

// DeployCompleteData represents deploy completion data
type DeployCompleteData struct {
	DeploymentID string          `json:"deployment_id"`
	Duration     string          `json:"duration"`
	Services     []ServiceStatus `json:"services"`
}

// ServiceStatus represents a service status
type ServiceStatus struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Ports  []int  `json:"ports,omitempty"`
}

// ExportProgressData represents export progress data
type ExportProgressData struct {
	BundleID string `json:"bundle_id"`
	Step     string `json:"step"`
	Message  string `json:"message"`
	Progress int    `json:"progress"`
}

// LogData represents log data
type LogData struct {
	Level   string                 `json:"level"`
	Source  string                 `json:"source"`
	Message string                 `json:"message"`
	Context map[string]interface{} `json:"context,omitempty"`
}

// WebSocketClient represents a connected WebSocket client
type WebSocketClient struct {
	conn   *websocket.Conn
	send   chan []byte
	done   chan struct{}
	closed bool
	mu     sync.Mutex
}

// WebSocketHub manages WebSocket connections
type WebSocketHub struct {
	// Registered clients by topic (e.g., "sync:plan-123", "deploy:dep-456")
	clients map[string]map[*WebSocketClient]bool
	mu      sync.RWMutex
}

// NewWebSocketHub creates a new WebSocket hub
func NewWebSocketHub() *WebSocketHub {
	return &WebSocketHub{
		clients: make(map[string]map[*WebSocketClient]bool),
	}
}

// Global hub instance
var wsHub = NewWebSocketHub()

// GetWebSocketHub returns the global WebSocket hub
func GetWebSocketHub() *WebSocketHub {
	return wsHub
}

// Register registers a client for a topic
func (h *WebSocketHub) Register(topic string, client *WebSocketClient) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.clients[topic] == nil {
		h.clients[topic] = make(map[*WebSocketClient]bool)
	}
	h.clients[topic][client] = true
}

// Unregister unregisters a client from a topic
func (h *WebSocketHub) Unregister(topic string, client *WebSocketClient) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if clients, ok := h.clients[topic]; ok {
		delete(clients, client)
		if len(clients) == 0 {
			delete(h.clients, topic)
		}
	}
}

// Broadcast sends a message to all clients subscribed to a topic
func (h *WebSocketHub) Broadcast(topic string, message WSBroadcastMessage) {
	h.mu.RLock()
	clients := h.clients[topic]
	h.mu.RUnlock()

	if clients == nil {
		return
	}

	data, err := json.Marshal(message)
	if err != nil {
		log.Printf("Failed to marshal WebSocket message: %v", err)
		return
	}

	for client := range clients {
		select {
		case client.send <- data:
		default:
			// Client buffer full, close connection
			h.Unregister(topic, client)
			client.Close()
		}
	}
}

// BroadcastSyncProgress broadcasts sync progress to a plan's subscribers
func (h *WebSocketHub) BroadcastSyncProgress(planID string, data SyncProgressData) {
	h.Broadcast("sync:"+planID, WSBroadcastMessage{
		Type:      WSTypeSyncProgress,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Data:      data,
	})
}

// BroadcastSyncComplete broadcasts sync completion to a plan's subscribers
func (h *WebSocketHub) BroadcastSyncComplete(planID string, data SyncCompleteData) {
	h.Broadcast("sync:"+planID, WSBroadcastMessage{
		Type:      WSTypeSyncComplete,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Data:      data,
	})
}

// BroadcastDeployProgress broadcasts deploy progress to a deployment's subscribers
func (h *WebSocketHub) BroadcastDeployProgress(deploymentID string, data DeployProgressData) {
	h.Broadcast("deploy:"+deploymentID, WSBroadcastMessage{
		Type:      WSTypeDeployProgress,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Data:      data,
	})
}

// BroadcastDeployComplete broadcasts deploy completion to a deployment's subscribers
func (h *WebSocketHub) BroadcastDeployComplete(deploymentID string, data DeployCompleteData) {
	h.Broadcast("deploy:"+deploymentID, WSBroadcastMessage{
		Type:      WSTypeDeployComplete,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Data:      data,
	})
}

// BroadcastExportProgress broadcasts export progress to a bundle's subscribers
func (h *WebSocketHub) BroadcastExportProgress(bundleID string, data ExportProgressData) {
	h.Broadcast("export:"+bundleID, WSBroadcastMessage{
		Type:      WSTypeExportProgress,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Data:      data,
	})
}

// BroadcastLog broadcasts a log message to a topic's subscribers
func (h *WebSocketHub) BroadcastLog(topic string, data LogData) {
	h.Broadcast(topic, WSBroadcastMessage{
		Type:      WSTypeLog,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Data:      data,
	})
}

// Close closes the client connection
func (c *WebSocketClient) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return
	}

	c.closed = true
	close(c.done)
	c.conn.Close()
}

// writePump pumps messages from the hub to the WebSocket connection
func (c *WebSocketClient) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}

		case <-c.done:
			return
		}
	}
}

// readPump pumps messages from the WebSocket connection to the hub
func (c *WebSocketClient) readPump(topic string) {
	defer func() {
		wsHub.Unregister(topic, c)
		c.Close()
	}()

	c.conn.SetReadLimit(512)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		// Handle ping messages
		var msg struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(message, &msg); err == nil {
			if msg.Type == "ping" {
				pong, _ := json.Marshal(WSBroadcastMessage{
					Type:      WSTypePong,
					Timestamp: time.Now().UTC().Format(time.RFC3339),
					Data:      nil,
				})
				c.send <- pong
			}
		}
	}
}

// HandleSyncWebSocket handles WebSocket connections for sync progress
func HandleSyncWebSocket(w http.ResponseWriter, r *http.Request) {
	planID := chi.URLParam(r, "planId")
	if planID == "" {
		http.Error(w, "Plan ID is required", http.StatusBadRequest)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	client := &WebSocketClient{
		conn: conn,
		send: make(chan []byte, 256),
		done: make(chan struct{}),
	}

	topic := "sync:" + planID
	wsHub.Register(topic, client)

	go client.writePump()
	go client.readPump(topic)
}

// HandleDeployWebSocket handles WebSocket connections for deploy progress
func HandleDeployWebSocket(w http.ResponseWriter, r *http.Request) {
	deploymentID := chi.URLParam(r, "deploymentId")
	if deploymentID == "" {
		http.Error(w, "Deployment ID is required", http.StatusBadRequest)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	client := &WebSocketClient{
		conn: conn,
		send: make(chan []byte, 256),
		done: make(chan struct{}),
	}

	topic := "deploy:" + deploymentID
	wsHub.Register(topic, client)

	go client.writePump()
	go client.readPump(topic)
}

// HandleExportWebSocket handles WebSocket connections for export progress
func HandleExportWebSocket(w http.ResponseWriter, r *http.Request) {
	bundleID := chi.URLParam(r, "bundleId")
	if bundleID == "" {
		http.Error(w, "Bundle ID is required", http.StatusBadRequest)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	client := &WebSocketClient{
		conn: conn,
		send: make(chan []byte, 256),
		done: make(chan struct{}),
	}

	topic := "export:" + bundleID
	wsHub.Register(topic, client)

	go client.writePump()
	go client.readPump(topic)
}
