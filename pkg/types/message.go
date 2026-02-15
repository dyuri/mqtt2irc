package types

import "time"

// Message represents a message flowing from MQTT to IRC
type Message struct {
	Topic     string
	Payload   []byte
	Timestamp time.Time
	QoS       byte
}
