package queues

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// MessageStatus represents the status of a queue message.
type MessageStatus string

const (
	StatusPending   MessageStatus = "pending"
	StatusActive    MessageStatus = "active"
	StatusCompleted MessageStatus = "completed"
	StatusFailed    MessageStatus = "failed"
)

// QueueInfo contains information about a queue.
type QueueInfo struct {
	Name           string    `json:"name"`
	PendingCount   int64     `json:"pending_count"`
	ActiveCount    int64     `json:"active_count"`
	CompletedCount int64     `json:"completed_count"`
	FailedCount    int64     `json:"failed_count"`
	TotalCount     int64     `json:"total_count"`
	CreatedAt      time.Time `json:"created_at,omitempty"`
	UpdatedAt      time.Time `json:"updated_at,omitempty"`
}

// Message represents a queue message.
type Message struct {
	ID          string                 `json:"id"`
	QueueName   string                 `json:"queue_name"`
	Status      MessageStatus          `json:"status"`
	Data        map[string]interface{} `json:"data"`
	Attempts    int                    `json:"attempts"`
	MaxAttempts int                    `json:"max_attempts"`
	Error       string                 `json:"error,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
	ProcessedAt *time.Time             `json:"processed_at,omitempty"`
	CompletedAt *time.Time             `json:"completed_at,omitempty"`
	FailedAt    *time.Time             `json:"failed_at,omitempty"`
}

// Config contains configuration for the queue service.
type Config struct {
	Addr     string
	Password string
	DB       int
}

// Service provides queue inspection and management operations.
type Service struct {
	client *redis.Client
}

// NewService creates a new queue service connected to Redis.
func NewService(cfg Config) (*Service, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &Service{client: client}, nil
}

// Close closes the Redis connection.
func (s *Service) Close() error {
	return s.client.Close()
}

// ListQueues returns all queues for the given stack.
func (s *Service) ListQueues(ctx context.Context, stackID string) ([]QueueInfo, error) {
	pattern := s.getKeyPrefix(stackID) + "*:meta"

	var queues []QueueInfo
	var cursor uint64

	for {
		keys, newCursor, err := s.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return nil, fmt.Errorf("failed to scan queue keys: %w", err)
		}

		for _, key := range keys {
			queueName := s.extractQueueName(key, stackID)
			if queueName == "" {
				continue
			}

			info, err := s.GetQueueInfo(ctx, stackID, queueName)
			if err != nil {
				continue
			}
			queues = append(queues, *info)
		}

		cursor = newCursor
		if cursor == 0 {
			break
		}
	}

	if len(queues) == 0 {
		queues, _ = s.discoverQueuesFromLists(ctx, stackID)
	}

	return queues, nil
}

func (s *Service) discoverQueuesFromLists(ctx context.Context, stackID string) ([]QueueInfo, error) {
	pattern := s.getKeyPrefix(stackID) + "*:wait"
	var queues []QueueInfo
	seen := make(map[string]bool)
	var cursor uint64

	for {
		keys, newCursor, err := s.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return nil, err
		}

		for _, key := range keys {
			queueName := s.extractQueueName(strings.TrimSuffix(key, ":wait"), stackID)
			if queueName == "" || seen[queueName] {
				continue
			}
			seen[queueName] = true

			info, err := s.GetQueueInfo(ctx, stackID, queueName)
			if err != nil {
				continue
			}
			queues = append(queues, *info)
		}

		cursor = newCursor
		if cursor == 0 {
			break
		}
	}

	return queues, nil
}

// GetQueueInfo returns detailed information about a specific queue.
func (s *Service) GetQueueInfo(ctx context.Context, stackID, queueName string) (*QueueInfo, error) {
	prefix := s.getQueueKeyPrefix(stackID, queueName)

	pendingCount, err := s.client.LLen(ctx, prefix+":wait").Result()
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("failed to get pending count: %w", err)
	}

	delayedCount, _ := s.client.ZCard(ctx, prefix+":delayed").Result()
	pendingCount += delayedCount

	activeCount, err := s.client.LLen(ctx, prefix+":active").Result()
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("failed to get active count: %w", err)
	}

	completedCount, err := s.client.ZCard(ctx, prefix+":completed").Result()
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("failed to get completed count: %w", err)
	}

	failedCount, err := s.client.ZCard(ctx, prefix+":failed").Result()
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("failed to get failed count: %w", err)
	}

	return &QueueInfo{
		Name:           queueName,
		PendingCount:   pendingCount,
		ActiveCount:    activeCount,
		CompletedCount: completedCount,
		FailedCount:    failedCount,
		TotalCount:     pendingCount + activeCount + completedCount + failedCount,
	}, nil
}

// ListMessages returns messages from a queue filtered by status.
func (s *Service) ListMessages(ctx context.Context, stackID, queueName string, status MessageStatus, limit, offset int) ([]Message, error) {
	prefix := s.getQueueKeyPrefix(stackID, queueName)

	var messageIDs []string
	var err error

	switch status {
	case StatusPending:
		messageIDs, err = s.client.LRange(ctx, prefix+":wait", int64(offset), int64(offset+limit-1)).Result()
	case StatusActive:
		messageIDs, err = s.client.LRange(ctx, prefix+":active", int64(offset), int64(offset+limit-1)).Result()
	case StatusCompleted:
		messageIDs, err = s.client.ZRevRange(ctx, prefix+":completed", int64(offset), int64(offset+limit-1)).Result()
	case StatusFailed:
		messageIDs, err = s.client.ZRevRange(ctx, prefix+":failed", int64(offset), int64(offset+limit-1)).Result()
	default:
		return nil, fmt.Errorf("invalid status: %s", status)
	}

	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("failed to list messages: %w", err)
	}

	var messages []Message
	for _, id := range messageIDs {
		msg, err := s.GetMessage(ctx, stackID, queueName, id)
		if err != nil {
			continue
		}
		messages = append(messages, *msg)
	}

	return messages, nil
}

// GetMessage returns details of a specific message.
func (s *Service) GetMessage(ctx context.Context, stackID, queueName, messageID string) (*Message, error) {
	prefix := s.getQueueKeyPrefix(stackID, queueName)
	key := prefix + ":" + messageID

	data, err := s.client.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get message: %w", err)
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("message not found")
	}

	msg := &Message{
		ID:        messageID,
		QueueName: queueName,
	}

	if dataStr, ok := data["data"]; ok {
		var msgData map[string]interface{}
		if err := json.Unmarshal([]byte(dataStr), &msgData); err == nil {
			msg.Data = msgData
		}
	}

	if attempts, ok := data["attemptsMade"]; ok {
		msg.Attempts, _ = strconv.Atoi(attempts)
	}

	if opts, ok := data["opts"]; ok {
		var optsData map[string]interface{}
		if err := json.Unmarshal([]byte(opts), &optsData); err == nil {
			if maxAttempts, ok := optsData["attempts"].(float64); ok {
				msg.MaxAttempts = int(maxAttempts)
			}
		}
	}

	if timestamp, ok := data["timestamp"]; ok {
		if ts, err := strconv.ParseInt(timestamp, 10, 64); err == nil {
			t := time.UnixMilli(ts)
			msg.CreatedAt = t
		}
	}

	if processedOn, ok := data["processedOn"]; ok {
		if ts, err := strconv.ParseInt(processedOn, 10, 64); err == nil {
			t := time.UnixMilli(ts)
			msg.ProcessedAt = &t
		}
	}

	if finishedOn, ok := data["finishedOn"]; ok {
		if ts, err := strconv.ParseInt(finishedOn, 10, 64); err == nil {
			t := time.UnixMilli(ts)
			msg.CompletedAt = &t
		}
	}

	if failedReason, ok := data["failedReason"]; ok {
		msg.Error = failedReason
		if msg.FailedAt == nil && msg.CompletedAt != nil {
			msg.FailedAt = msg.CompletedAt
			msg.CompletedAt = nil
		}
	}

	msg.Status = s.determineMessageStatus(ctx, prefix, messageID)

	return msg, nil
}

func (s *Service) determineMessageStatus(ctx context.Context, prefix, messageID string) MessageStatus {
	if s.isInList(ctx, prefix+":active", messageID) {
		return StatusActive
	}
	if s.isInList(ctx, prefix+":wait", messageID) {
		return StatusPending
	}
	if score, err := s.client.ZScore(ctx, prefix+":failed", messageID).Result(); err == nil && score > 0 {
		return StatusFailed
	}
	if score, err := s.client.ZScore(ctx, prefix+":completed", messageID).Result(); err == nil && score > 0 {
		return StatusCompleted
	}
	return StatusPending
}

func (s *Service) isInList(ctx context.Context, key, value string) bool {
	items, err := s.client.LRange(ctx, key, 0, -1).Result()
	if err != nil {
		return false
	}
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

// DeleteMessage removes a message from the queue.
func (s *Service) DeleteMessage(ctx context.Context, stackID, queueName, messageID string) error {
	prefix := s.getQueueKeyPrefix(stackID, queueName)

	pipe := s.client.Pipeline()
	pipe.LRem(ctx, prefix+":wait", 0, messageID)
	pipe.LRem(ctx, prefix+":active", 0, messageID)
	pipe.LRem(ctx, prefix+":delayed", 0, messageID)
	pipe.ZRem(ctx, prefix+":completed", messageID)
	pipe.ZRem(ctx, prefix+":failed", messageID)
	pipe.Del(ctx, prefix+":"+messageID)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete message: %w", err)
	}

	return nil
}

// RetryMessage moves a failed message back to the pending queue.
func (s *Service) RetryMessage(ctx context.Context, stackID, queueName, messageID string) error {
	prefix := s.getQueueKeyPrefix(stackID, queueName)

	if err := s.client.ZRem(ctx, prefix+":failed", messageID).Err(); err != nil {
		return fmt.Errorf("failed to remove from failed set: %w", err)
	}

	if err := s.client.HSet(ctx, prefix+":"+messageID, "attemptsMade", "0").Err(); err != nil {
		return fmt.Errorf("failed to reset attempts: %w", err)
	}

	if err := s.client.LPush(ctx, prefix+":wait", messageID).Err(); err != nil {
		return fmt.Errorf("failed to add to wait list: %w", err)
	}

	return nil
}

// PurgeQueue removes all messages with a specific status from the queue.
func (s *Service) PurgeQueue(ctx context.Context, stackID, queueName string, status MessageStatus) (int64, error) {
	prefix := s.getQueueKeyPrefix(stackID, queueName)

	var count int64
	var err error

	switch status {
	case StatusPending:
		ids, _ := s.client.LRange(ctx, prefix+":wait", 0, -1).Result()
		for _, id := range ids {
			s.client.Del(ctx, prefix+":"+id)
		}
		count, err = s.client.Del(ctx, prefix+":wait").Result()
	case StatusActive:
		ids, _ := s.client.LRange(ctx, prefix+":active", 0, -1).Result()
		for _, id := range ids {
			s.client.Del(ctx, prefix+":"+id)
		}
		count, err = s.client.Del(ctx, prefix+":active").Result()
	case StatusCompleted:
		ids, _ := s.client.ZRange(ctx, prefix+":completed", 0, -1).Result()
		count = int64(len(ids))
		for _, id := range ids {
			s.client.Del(ctx, prefix+":"+id)
		}
		_, err = s.client.Del(ctx, prefix+":completed").Result()
	case StatusFailed:
		ids, _ := s.client.ZRange(ctx, prefix+":failed", 0, -1).Result()
		count = int64(len(ids))
		for _, id := range ids {
			s.client.Del(ctx, prefix+":"+id)
		}
		_, err = s.client.Del(ctx, prefix+":failed").Result()
	default:
		return 0, fmt.Errorf("invalid status: %s", status)
	}

	if err != nil {
		return 0, fmt.Errorf("failed to purge queue: %w", err)
	}

	return count, nil
}

func (s *Service) getKeyPrefix(stackID string) string {
	if stackID == "" || stackID == "default" {
		return "bull:"
	}
	return "bull:" + stackID + ":"
}

func (s *Service) getQueueKeyPrefix(stackID, queueName string) string {
	if stackID == "" || stackID == "default" {
		return "bull:" + queueName
	}
	return "bull:" + stackID + ":" + queueName
}

func (s *Service) extractQueueName(key, stackID string) string {
	prefix := s.getKeyPrefix(stackID)
	if !strings.HasPrefix(key, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(key, prefix)
	parts := strings.Split(rest, ":")
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}
