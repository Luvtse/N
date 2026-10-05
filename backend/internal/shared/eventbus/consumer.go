package eventbus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/segmentio/kafka-go"
	"go.uber.org/zap"
)

// ============================================================================
// ERRORS
// ============================================================================

var (
	ErrConsumerClosed    = errors.New("consumer is closed")
	ErrHandlerNotFound   = errors.New("handler not found for topic")
	ErrProcessingFailed  = errors.New("message processing failed")
)

// ============================================================================
// TYPES
// ============================================================================

// MessageHandler is a function that processes a Kafka message
type MessageHandler func(ctx context.Context, event Event) error

// ConsumerConfig holds consumer configuration
type ConsumerConfig struct {
	Brokers        []string
	GroupID        string
	Topics         []string
	MinBytes       int
	MaxBytes       int
	MaxWait        time.Duration
	StartOffset    int64
	RetryAttempts  int
	RetryDelay     time.Duration
}

// DefaultConsumerConfig returns sensible defaults
func DefaultConsumerConfig(groupID string, topics ...string) *ConsumerConfig {
	return &ConsumerConfig{
		Brokers:       []string{"localhost:9092"},
		GroupID:       groupID,
		Topics:        topics,
		MinBytes:      10e3, // 10KB
		MaxBytes:      10e6, // 10MB
		MaxWait:       1 * time.Second,
		StartOffset:   kafka.LastOffset,
		RetryAttempts: 3,
		RetryDelay:    1 * time.Second,
	}
}

// Consumer manages Kafka message consumption
type Consumer struct {
	config   *ConsumerConfig
	reader   *kafka.Reader
	handlers map[string]MessageHandler
	logger   *zap.Logger
	mu       sync.RWMutex
	closed   bool
	wg       sync.WaitGroup
}

// NewConsumer creates a new Kafka consumer
func NewConsumer(config *ConsumerConfig, logger *zap.Logger) (*Consumer, error) {
	if logger == nil {
		logger = zap.NewNop()
	}

	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:     config.Brokers,
		GroupID:     config.GroupID,
		Topic:       config.Topics[0], // Single topic for now
		MinBytes:    config.MinBytes,
		MaxBytes:    config.MaxBytes,
		MaxWait:     config.MaxWait,
		StartOffset: config.StartOffset,
	})

	return &Consumer{
		config:   config,
		reader:   reader,
		handlers: make(map[string]MessageHandler),
		logger:   logger,
	}, nil
}

// RegisterHandler registers a handler for a specific event type
func (c *Consumer) RegisterHandler(eventType string, handler MessageHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handlers[eventType] = handler
	c.logger.Info("registered event handler",
		zap.String("event_type", eventType),
	)
}

// Start begins consuming messages
func (c *Consumer) Start(ctx context.Context) error {
	c.logger.Info("starting consumer",
		zap.String("group_id", c.config.GroupID),
		zap.Strings("topics", c.config.Topics),
	)

	c.wg.Add(1)
	go c.consumeLoop(ctx)

	return nil
}

// consumeLoop is the main consumption loop
func (c *Consumer) consumeLoop(ctx context.Context) {
	defer c.wg.Done()

	for {
		select {
		case <-ctx.Done():
			c.logger.Info("consumer context cancelled, shutting down")
			return
		default:
			if err := c.consumeMessage(ctx); err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}
				c.logger.Error("error consuming message", zap.Error(err))
				time.Sleep(1 * time.Second) // Backoff on error
			}
		}
	}
}

// consumeMessage reads and processes a single message
func (c *Consumer) consumeMessage(ctx context.Context) error {
	msg, err := c.reader.ReadMessage(ctx)
	if err != nil {
		return err
	}

	// Deserialize event
	var event Event
	if err := json.Unmarshal(msg.Value, &event); err != nil {
		c.logger.Error("failed to unmarshal event",
			zap.Error(err),
			zap.ByteString("value", msg.Value),
		)
		// Commit offset to avoid poison pill
		return nil
	}

	// Process with retries
	if err := c.processWithRetry(ctx, event); err != nil {
		c.logger.Error("failed to process event after retries",
			zap.String("event_type", event.Type),
			zap.Error(err),
		)
		// Still commit to avoid infinite retry loop
	}

	return nil
}

// processWithRetry attempts to process a message with retries
func (c *Consumer) processWithRetry(ctx context.Context, event Event) error {
	var lastErr error

	for attempt := 0; attempt <= c.config.RetryAttempts; attempt++ {
		if err := c.processEvent(ctx, event); err != nil {
			lastErr = err
			c.logger.Warn("event processing failed, retrying",
				zap.String("event_type", event.Type),
				zap.Int("attempt", attempt+1),
				zap.Error(err),
			)
			time.Sleep(c.config.RetryDelay * time.Duration(attempt+1))
			continue
		}
		return nil
	}

	return fmt.Errorf("%w: %v", ErrProcessingFailed, lastErr)
}

// processEvent routes event to appropriate handler
func (c *Consumer) processEvent(ctx context.Context, event Event) error {
	c.mu.RLock()
	handler, exists := c.handlers[event.Type]
	c.mu.RUnlock()

	if !exists {
		c.logger.Debug("no handler for event type",
			zap.String("event_type", event.Type),
		)
		return nil
	}

	start := time.Now()
	if err := handler(ctx, event); err != nil {
		return err
	}

	c.logger.Debug("event processed successfully",
		zap.String("event_type", event.Type),
		zap.Duration("duration", time.Since(start)),
	)

	return nil
}

// Stop gracefully stops the consumer
func (c *Consumer) Stop() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()

	c.logger.Info("stopping consumer")

	// Wait for processing to complete
	c.wg.Wait()

	// Close reader
	if err := c.reader.Close(); err != nil {
		c.logger.Error("failed to close reader", zap.Error(err))
		return err
	}

	c.logger.Info("consumer stopped successfully")
	return nil
}

// ============================================================================
// CONSUMER GROUP (Multiple Topics)
// ============================================================================

// ConsumerGroup manages multiple consumers for different topics
type ConsumerGroup struct {
	consumers []*Consumer
	logger    *zap.Logger
	mu        sync.Mutex
}

// NewConsumerGroup creates a new consumer group
func NewConsumerGroup(logger *zap.Logger) *ConsumerGroup {
	return &ConsumerGroup{
		consumers: make([]*Consumer, 0),
		logger:    logger,
	}
}

// AddConsumer adds a consumer to the group
func (g *ConsumerGroup) AddConsumer(consumer *Consumer) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.consumers = append(g.consumers, consumer)
}

// StartAll starts all consumers in the group
func (g *ConsumerGroup) StartAll(ctx context.Context) error {
	for _, consumer := range g.consumers {
		if err := consumer.Start(ctx); err != nil {
			return err
		}
	}
	return nil
}

// StopAll stops all consumers in the group
func (g *ConsumerGroup) StopAll() error {
	var lastErr error
	for _, consumer := range g.consumers {
		if err := consumer.Stop(); err != nil {
			lastErr = err
		}
	}
	return lastErr
}