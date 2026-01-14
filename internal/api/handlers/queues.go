package handlers

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"

	"github.com/homeport/homeport/internal/app/queues"
	"github.com/homeport/homeport/internal/pkg/httputil"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

const (
	MaxQueueNameLength   = 256
	MaxMessageIDLength   = 256
	DefaultMessageLimit  = 50
	MaxMessageLimit      = 1000
)

var (
	queueNameRegex  = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.\-]*$`)
	messageIDRegex  = regexp.MustCompile(`^[a-zA-Z0-9\-_]+$`)
)

func validateQueueName(name string) error {
	if name == "" {
		return fmt.Errorf("queue name is required")
	}
	if len(name) > MaxQueueNameLength {
		return fmt.Errorf("queue name must be at most %d characters", MaxQueueNameLength)
	}
	if !queueNameRegex.MatchString(name) {
		return fmt.Errorf("queue name must start with alphanumeric and contain only letters, digits, hyphens, underscores, and periods")
	}
	return nil
}

func validateMessageID(id string) error {
	if id == "" {
		return fmt.Errorf("message ID is required")
	}
	if len(id) > MaxMessageIDLength {
		return fmt.Errorf("message ID must be at most %d characters", MaxMessageIDLength)
	}
	if !messageIDRegex.MatchString(id) {
		return fmt.Errorf("message ID must contain only letters, digits, hyphens, and underscores")
	}
	return nil
}

func validateMessageStatus(status string) (queues.MessageStatus, error) {
	switch status {
	case "pending", "":
		return queues.StatusPending, nil
	case "active":
		return queues.StatusActive, nil
	case "completed":
		return queues.StatusCompleted, nil
	case "failed":
		return queues.StatusFailed, nil
	default:
		return "", fmt.Errorf("invalid status: must be one of pending, active, completed, failed")
	}
}

// QueuesHandler handles queue-related HTTP requests.
type QueuesHandler struct {
	service *queues.Service
}

// NewQueuesHandler creates a new queues handler.
func NewQueuesHandler(cfg queues.Config) (*QueuesHandler, error) {
	svc, err := queues.NewService(cfg)
	if err != nil {
		return nil, err
	}
	return &QueuesHandler{service: svc}, nil
}

// Close closes the handler's service connection.
func (h *QueuesHandler) Close() error {
	if h.service != nil {
		return h.service.Close()
	}
	return nil
}

// HandleListQueues handles GET /stacks/{stackID}/queues
func (h *QueuesHandler) HandleListQueues(w http.ResponseWriter, r *http.Request) {
	stackID := chi.URLParam(r, "stackID")
	if err := validateStackID(stackID); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	queueList, err := h.service.ListQueues(r.Context(), stackID)
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	// Ensure we return an empty slice instead of null
	if queueList == nil {
		queueList = make([]queues.QueueInfo, 0)
	}

	render.JSON(w, r, map[string]interface{}{
		"queues": queueList,
		"count":  len(queueList),
	})
}

// HandleGetQueue handles GET /stacks/{stackID}/queues/{queue}
func (h *QueuesHandler) HandleGetQueue(w http.ResponseWriter, r *http.Request) {
	stackID := chi.URLParam(r, "stackID")
	queueName := chi.URLParam(r, "queue")

	if err := validateStackID(stackID); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}
	if err := validateQueueName(queueName); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	queueInfo, err := h.service.GetQueueInfo(r.Context(), stackID, queueName)
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, queueInfo)
}

// HandleListMessages handles GET /stacks/{stackID}/queues/{queue}/messages
func (h *QueuesHandler) HandleListMessages(w http.ResponseWriter, r *http.Request) {
	stackID := chi.URLParam(r, "stackID")
	queueName := chi.URLParam(r, "queue")

	if err := validateStackID(stackID); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}
	if err := validateQueueName(queueName); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	statusStr := r.URL.Query().Get("status")
	status, err := validateMessageStatus(statusStr)
	if err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	limit := DefaultMessageLimit
	if l := r.URL.Query().Get("limit"); l != "" {
		parsed, err := strconv.Atoi(l)
		if err != nil || parsed < 1 {
			httputil.BadRequest(w, r, "invalid limit parameter: must be a positive number")
			return
		}
		if parsed > MaxMessageLimit {
			httputil.BadRequest(w, r, fmt.Sprintf("limit must be at most %d", MaxMessageLimit))
			return
		}
		limit = parsed
	}

	offset := 0
	if o := r.URL.Query().Get("offset"); o != "" {
		parsed, err := strconv.Atoi(o)
		if err != nil || parsed < 0 {
			httputil.BadRequest(w, r, "invalid offset parameter: must be a non-negative number")
			return
		}
		offset = parsed
	}

	messages, err := h.service.ListMessages(r.Context(), stackID, queueName, status, limit, offset)
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	// Ensure we return an empty slice instead of null
	if messages == nil {
		messages = make([]queues.Message, 0)
	}

	render.JSON(w, r, map[string]interface{}{
		"messages": messages,
		"count":    len(messages),
		"limit":    limit,
		"offset":   offset,
		"status":   status,
	})
}

// HandleGetMessage handles GET /stacks/{stackID}/queues/{queue}/messages/{messageID}
func (h *QueuesHandler) HandleGetMessage(w http.ResponseWriter, r *http.Request) {
	stackID := chi.URLParam(r, "stackID")
	queueName := chi.URLParam(r, "queue")
	messageID := chi.URLParam(r, "messageID")

	if err := validateStackID(stackID); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}
	if err := validateQueueName(queueName); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}
	if err := validateMessageID(messageID); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	message, err := h.service.GetMessage(r.Context(), stackID, queueName, messageID)
	if err != nil {
		httputil.NotFound(w, r, err.Error())
		return
	}

	render.JSON(w, r, message)
}

// HandleDeleteMessage handles DELETE /stacks/{stackID}/queues/{queue}/messages/{messageID}
func (h *QueuesHandler) HandleDeleteMessage(w http.ResponseWriter, r *http.Request) {
	stackID := chi.URLParam(r, "stackID")
	queueName := chi.URLParam(r, "queue")
	messageID := chi.URLParam(r, "messageID")

	if err := validateStackID(stackID); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}
	if err := validateQueueName(queueName); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}
	if err := validateMessageID(messageID); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	if err := h.service.DeleteMessage(r.Context(), stackID, queueName, messageID); err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]string{
		"status":    "deleted",
		"messageID": messageID,
	})
}

// HandleRetryMessage handles POST /stacks/{stackID}/queues/{queue}/messages/{messageID}/retry
func (h *QueuesHandler) HandleRetryMessage(w http.ResponseWriter, r *http.Request) {
	stackID := chi.URLParam(r, "stackID")
	queueName := chi.URLParam(r, "queue")
	messageID := chi.URLParam(r, "messageID")

	if err := validateStackID(stackID); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}
	if err := validateQueueName(queueName); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}
	if err := validateMessageID(messageID); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	if err := h.service.RetryMessage(r.Context(), stackID, queueName, messageID); err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]string{
		"status":    "retried",
		"messageID": messageID,
	})
}

// HandlePurgeQueue handles DELETE /stacks/{stackID}/queues/{queue}/messages
func (h *QueuesHandler) HandlePurgeQueue(w http.ResponseWriter, r *http.Request) {
	stackID := chi.URLParam(r, "stackID")
	queueName := chi.URLParam(r, "queue")

	if err := validateStackID(stackID); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}
	if err := validateQueueName(queueName); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	statusStr := r.URL.Query().Get("status")
	if statusStr == "" {
		httputil.BadRequest(w, r, "status query parameter is required for purge operation")
		return
	}

	status, err := validateMessageStatus(statusStr)
	if err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	count, err := h.service.PurgeQueue(r.Context(), stackID, queueName, status)
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]interface{}{
		"status":  "purged",
		"deleted": count,
	})
}
