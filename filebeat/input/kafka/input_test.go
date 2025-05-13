// Licensed to Elasticsearch B.V. under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Elasticsearch B.V. licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

//go:build !integration

package kafka

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/elastic/beats/v7/libbeat/tests/resources"
	conf "github.com/elastic/elastic-agent-libs/config"
	"github.com/elastic/elastic-agent-libs/mapstr"
)

func TestNewInputDone(t *testing.T) {
	config := conf.MustNewConfigFrom(mapstr.M{
		"hosts":    "localhost:9092",
		"topics":   "messages",
		"group_id": "filebeat",
	})

	AssertNotStartedInputCanBeDone(t, config)
}

// AssertNotStartedInputCanBeDone checks that the context of an input can be
// done before starting the input, and it doesn't leak goroutines. This is
// important to confirm that leaks don't happen with CheckConfig.
func AssertNotStartedInputCanBeDone(t *testing.T, configMap *conf.C) {
	goroutines := resources.NewGoroutinesChecker()
	defer goroutines.Check(t)

	config, err := conf.NewConfigFrom(configMap)
	require.NoError(t, err)

	_, err = Plugin().Manager.Create(config)
	require.NoError(t, err)
}

// MockSaramaClaim is a mock implementation of sarama.ConsumerGroupClaim
type MockSaramaClaim struct {
	messages chan *sarama.ConsumerMessage
	topic    string
	partition int34
}

func NewMockSaramaClaim(topic string, partition int32) *MockSaramaClaim {
	return &MockSaramaClaim{
		messages: make(chan *sarama.ConsumerMessage, 1), // Buffer of 1
		topic:    topic,
		partition: partition,
	}
}

func (m *MockSaramaClaim) Topic() string { return m.topic }
func (m *MockSaramaClaim) Partition() int32 { return m.partition }
func (m *MockSaramaClaim) InitialOffset() int64 { return 0 }
func (m *MockSaramaClaim) HighWaterMarkOffset() int64 { return 0 }
func (m *MockSaramaClaim) Messages() <-chan *sarama.ConsumerMessage { return m.messages }

func (m *MockSaramaClaim) SendMessage(msg *sarama.ConsumerMessage) {
	m.messages <- msg
}

func (m *MockSaramaClaim) CloseChannel() {
	close(m.messages)
}

// MockLogger is a mock implementation of logp.Logger
type MockLogger struct {
	Errors []string
	Infos  []string
	Debugs []string
}

func NewMockLogger() *MockLogger {
	return &MockLogger{}
}

func (m *MockLogger) Named(name string) *logp.Logger { return logp.NewLogger(name) } // Not strictly needed for this test
func (m *MockLogger) Errorf(format string, args ...interface{}) { m.Errors = append(m.Errors, fmt.Sprintf(format, args...)) }
func (m *MockLogger) Errorw(msg string, keysAndValues ...interface{}) { m.Errors = append(m.Errors, msg) } // Simplified
func (m *MockLogger) Infof(format string, args ...interface{}) { m.Infos = append(m.Infos, fmt.Sprintf(format, args...)) }
func (m *MockLogger) Infow(msg string, keysAndValues ...interface{}) { m.Infos = append(m.Infos, msg) } // Simplified
func (m *MockLogger) Debugf(format string, args ...interface{}) { m.Debugs = append(m.Debugs, fmt.Sprintf(format, args...)) }
func (m *MockLogger) Debugw(msg string, keysAndValues ...interface{}) { m.Debugs = append(m.Debugs, msg) } // Simplified

func TestListFromFieldReaderNonArray(t *testing.T) {
	mockClaim := NewMockSaramaClaim("test-topic", 0)
	mockLogger := NewMockLogger()
	handler := &groupHandler{
		log: mockLogger.Named("test"), // Use a named logger to match input.go
	}

	reader := &listFromFieldReader{
		claim:        mockClaim,
		groupHandler: handler,
		field:        "not_an_array_field",
		log:          mockLogger.Named("test"), // Use a named logger
	}

	// Simulate a Kafka message where the field is not an array
	nonArrayMessage := `{ "not_an_array_field": "this is a string" }`
	kafkaMsg := &sarama.ConsumerMessage{
		Value: []byte(nonArrayMessage),
		Topic: "test-topic",
		Partition: 0,
		Offset: 1,
		Timestamp: time.Now(),
	}

	// Send the message to the mock claim channel
	mockClaim.SendMessage(kafkaMsg)
	mockClaim.CloseChannel() // Close the channel after sending the message

	// Read from the reader
	// We expect Next() to process the message, log an error, and return io.EOF
	// because parseMultipleMessages will return an empty slice and the claim channel is closed.
	_, err := reader.Next()

	// Assert that an error was logged
	require.Len(t, mockLogger.Errors, 1, "Expected exactly one error log")
	require.Contains(t, mockLogger.Errors[0], "Kafka configured field 'not_an_array_field' is not a JSON array", "Expected specific error message")

	// Assert that Next() returned io.EOF
	require.ErrorIs(t, err, io.EOF, "Expected io.EOF after processing non-array message and closing channel")
}
