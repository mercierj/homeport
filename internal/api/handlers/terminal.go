package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/homeport/homeport/internal/app/terminal"
	"github.com/homeport/homeport/internal/pkg/httputil"
	"github.com/homeport/homeport/internal/pkg/logger"
)

const (
	// WebSocket message types
	MsgTypeInput  = "input"
	MsgTypeResize = "resize"
	MsgTypePing   = "ping"

	// Limits
	MaxTerminalContainerIDLength = 128
	MaxMessageSize               = 32768 // 32KB
	PingInterval                 = 30 * time.Second
	WriteTimeout                 = 10 * time.Second
	ReadTimeout                  = 60 * time.Second
)

var (
	terminalContainerIDRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.\-:]*$`)

	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			// In development, allow all origins
			// In production, validate origin against allowed hosts
			return true
		},
	}
)

// WSMessage is the WebSocket message envelope
type WSMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// InputData is the terminal input message data
type InputData struct {
	Data string `json:"data"`
}

// ResizeData is the terminal resize message data
type ResizeData struct {
	Cols uint `json:"cols"`
	Rows uint `json:"rows"`
}

// OutputData is the terminal output message data
type OutputData struct {
	Data string `json:"data"`
}

// ErrorData is the error message data
type ErrorData struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}

// ConnectedData is the connected message data
type ConnectedData struct {
	SessionID   string `json:"session_id"`
	ContainerID string `json:"container_id"`
}

// TerminalHandler handles terminal WebSocket connections
type TerminalHandler struct {
	service *terminal.Service
}

// NewTerminalHandler creates a new terminal handler
func NewTerminalHandler() (*TerminalHandler, error) {
	svc, err := terminal.NewService()
	if err != nil {
		return nil, err
	}
	return &TerminalHandler{service: svc}, nil
}

// Close closes the terminal handler
func (h *TerminalHandler) Close() error {
	if h.service != nil {
		return h.service.Close()
	}
	return nil
}

// validateTerminalContainerID validates container ID format
func validateTerminalContainerID(id string) error {
	if id == "" {
		return &validationError{msg: "container ID is required"}
	}
	if len(id) > MaxTerminalContainerIDLength {
		return &validationError{msg: "container ID too long"}
	}
	if !terminalContainerIDRegex.MatchString(id) {
		return &validationError{msg: "invalid container ID format"}
	}
	return nil
}

type validationError struct {
	msg string
}

func (e *validationError) Error() string {
	return e.msg
}

// HandleExec handles the WebSocket terminal connection
func (h *TerminalHandler) HandleExec(w http.ResponseWriter, r *http.Request) {
	containerID := chi.URLParam(r, "id")
	if err := validateTerminalContainerID(containerID); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	// Parse initial terminal size from query params
	cols, _ := strconv.ParseUint(r.URL.Query().Get("cols"), 10, 32)
	rows, _ := strconv.ParseUint(r.URL.Query().Get("rows"), 10, 32)
	if cols == 0 {
		cols = 80
	}
	if rows == 0 {
		rows = 24
	}

	// Upgrade to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error("WebSocket upgrade failed", "error", err)
		return
	}
	defer conn.Close()

	// Configure WebSocket
	conn.SetReadLimit(MaxMessageSize)

	// Create terminal session
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	session, err := h.service.CreateSession(ctx, containerID, uint(cols), uint(rows))
	if err != nil {
		sendError(conn, "session_error", err.Error())
		return
	}
	defer h.service.CloseSession(session.ID)

	// Send connected message
	_ = sendMessage(conn, "connected", ConnectedData{
		SessionID:   session.ID,
		ContainerID: containerID,
	})

	var wg sync.WaitGroup
	wg.Add(2)

	// Read from container, write to WebSocket
	go func() {
		defer wg.Done()
		defer cancel()
		h.readFromContainer(conn, session)
	}()

	// Read from WebSocket, write to container
	go func() {
		defer wg.Done()
		defer cancel()
		h.readFromWebSocket(ctx, conn, session)
	}()

	wg.Wait()
}

// readFromContainer reads output from container and sends to WebSocket
func (h *TerminalHandler) readFromContainer(conn *websocket.Conn, session *terminal.Session) {
	buf := make([]byte, 4096)
	for {
		n, err := session.HijackedResp.Reader.Read(buf)
		if err != nil {
			if err != io.EOF {
				logger.Debug("Container read error", "error", err)
			}
			return
		}
		if n > 0 {
			session.UpdateActivity()

			_ = conn.SetWriteDeadline(time.Now().Add(WriteTimeout))
			if err := sendMessage(conn, "output", OutputData{Data: string(buf[:n])}); err != nil {
				logger.Debug("WebSocket write error", "error", err)
				return
			}
		}
	}
}

// readFromWebSocket reads input from WebSocket and sends to container
func (h *TerminalHandler) readFromWebSocket(ctx context.Context, conn *websocket.Conn, session *terminal.Session) {
	pingTicker := time.NewTicker(PingInterval)
	defer pingTicker.Stop()

	// Set up pong handler
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(ReadTimeout))
		return nil
	})

	for {
		select {
		case <-ctx.Done():
			return
		case <-pingTicker.C:
			_ = conn.SetWriteDeadline(time.Now().Add(WriteTimeout))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		default:
			_ = conn.SetReadDeadline(time.Now().Add(ReadTimeout))
			_, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
					logger.Debug("WebSocket read error", "error", err)
				}
				return
			}

			var msg WSMessage
			if err := json.Unmarshal(message, &msg); err != nil {
				continue
			}

			session.UpdateActivity()

			switch msg.Type {
			case MsgTypeInput:
				var input InputData
				if err := json.Unmarshal(msg.Data, &input); err != nil {
					continue
				}
				_, err := session.HijackedResp.Conn.Write([]byte(input.Data))
				if err != nil {
					logger.Debug("Container write error", "error", err)
					return
				}

			case MsgTypeResize:
				var resize ResizeData
				if err := json.Unmarshal(msg.Data, &resize); err != nil {
					continue
				}
				_ = h.service.ResizeSession(ctx, session.ID, resize.Cols, resize.Rows)

			case MsgTypePing:
				_ = sendMessage(conn, "pong", nil)
			}
		}
	}
}

// sendMessage sends a typed message over WebSocket
func sendMessage(conn *websocket.Conn, msgType string, data interface{}) error {
	var dataBytes json.RawMessage
	if data != nil {
		var err error
		dataBytes, err = json.Marshal(data)
		if err != nil {
			return err
		}
	}
	msg := WSMessage{Type: msgType, Data: dataBytes}
	return conn.WriteJSON(msg)
}

// sendError sends an error message over WebSocket
func sendError(conn *websocket.Conn, code, message string) error {
	return sendMessage(conn, "error", ErrorData{Error: message, Code: code})
}

// RegisterRoutes registers terminal routes
func (h *TerminalHandler) RegisterRoutes(r chi.Router) {
	r.Route("/terminal", func(r chi.Router) {
		r.Get("/containers/{id}/exec", h.HandleExec)
	})
}
