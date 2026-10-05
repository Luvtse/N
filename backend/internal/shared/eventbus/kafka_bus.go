package eventbus

import (
	"context"
	"encoding/json"
	"log"

	"github.com/segmentio/kafka-go"
)

type Event struct {
	Type      string                 `json:"type"`
	Payload   map[string]interface{} `json:"payload"`
	Timestamp int64                  `json:"timestamp"`
	TraceID   string                 `json:"trace_id"`
}

type EventBus interface {
	Publish(ctx context.Context, topic string, event Event) error
	Subscribe(ctx context.Context, topic string, handler func(Event)) error
	Close() error
}

type KafkaBus struct {
	writer *kafka.Writer
	reader *kafka.Reader
}

func NewKafkaBus(brokers []string) *KafkaBus {
	writer := &kafka.Writer{
		Addr:     kafka.TCP(brokers...),
		Topic:    "nidaw.events",
		Balancer: &kafka.LeastBytes{},
	}
	
	return &KafkaBus{writer: writer}
}

func (b *KafkaBus) Publish(ctx context.Context, topic string, event Event) error {
	msg, err := json.Marshal(event)
	if err != nil {
		return err
	}
	
	return b.writer.WriteMessages(ctx, kafka.Message{
		Key:   []byte(event.Type),
		Value: msg,
	})
}

func (b *KafkaBus) Subscribe(ctx context.Context, topic string, handler func(Event)) error {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers: []string{"localhost:9092"},
		Topic:   topic,
		GroupID: "nidaw-consumer",
	})
	
	go func() {
		for {
			msg, err := reader.ReadMessage(ctx)
			if err != nil {
				log.Printf("Error reading message: %v", err)
				continue
			}
			
			var event Event
			if err := json.Unmarshal(msg.Value, &event); err != nil {
				log.Printf("Error unmarshaling event: %v", err)
				continue
			}
			
			handler(event)
		}
	}()
	
	return nil
}

func (b *KafkaBus) Close() error {
	return b.writer.Close()
}