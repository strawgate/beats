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

package logstash

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/elastic/beats/v7/libbeat/beat"
	"github.com/elastic/beats/v7/libbeat/outputs"
	"github.com/elastic/beats/v7/libbeat/outputs/outest"
	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-libs/transport"
)

type testAsyncDriver struct {
	client  outputs.NetworkClient
	ch      chan testDriverCommand
	returns []testClientReturn
	wg      sync.WaitGroup
}

// mockBatch is a mock implementation of publisher.Batch for testing ACK calls.
type mockBatch struct {
	events []publisher.Event
	acked  int
	ackedMu sync.Mutex
	done chan struct{} // Channel to signal when ACK is called
}

func newMockBatch(events []publisher.Event) *mockBatch {
	return &mockBatch{
		events: events,
		done: make(chan struct{}),
	}
}

func (b *mockBatch) Events() []publisher.Event {
	return b.events
}

func (b *mockBatch) ACK() {
	b.ackedMu.Lock()
	defer b.ackedMu.Unlock()
	b.acked++
	// Signal that ACK was called
	select {
	case b.done <- struct{}{}:
	default:
	}
}

func (b *mockBatch) RetryEvents(events []publisher.Event) {
	// For this test, we assume no retries
}

func (b *mockBatch) Cancelled() {
	// For this test, we assume no cancellations
}

func (b *mockBatch) GetACKCount() int {
	b.ackedMu.Lock()
	defer b.ackedMu.Unlock()
	return b.acked
}

func (b *mockBatch) WaitACK() {
	<-b.done
}


func TestAsyncSendZero(t *testing.T) {
	testSendZero(t, makeAsyncTestClient)
}

func TestAsyncSimpleEvent(t *testing.T) {
	testSimpleEvent(t, makeAsyncTestClient)
}

func TestAsyncStructuredEvent(t *testing.T) {
	testStructuredEvent(t, makeAsyncTestClient)
}

func makeAsyncTestClient(conn *transport.Client) testClientDriver {
	config := defaultConfig()
	config.Timeout = 1 * time.Second
	config.Pipelining = 3
	logger, err := logp.NewDevelopmentLogger("")
	if err != nil {
		panic(err)
	}
	client, err := newAsyncClient(beat.Info{Logger: logger}, conn, outputs.NewNilObserver(), &config)
	if err != nil {
		panic(err)
	}
	return newAsyncTestDriver(client)
}

func newAsyncTestDriver(client outputs.NetworkClient) *testAsyncDriver {
	driver := &testAsyncDriver{
		client:  client,
		ch:      make(chan testDriverCommand, 1),
		returns: nil,
	}

	driver.wg.Add(1)
	go func() {
		defer driver.wg.Done()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		for {
			cmd, ok := <-driver.ch
			if !ok {
				return
			}

			switch cmd.code {
			case driverCmdQuit:
				return
			case driverCmdConnect:
				driver.client.Connect(ctx)
			case driverCmdClose:
				driver.client.Close()
			case driverCmdPublish:
				err := driver.client.Publish(context.Background(), cmd.batch)
				driver.returns = append(driver.returns, testClientReturn{cmd.batch, err})
			}
		}
	}()

	return driver
}

func (t *testAsyncDriver) Close() {
	t.ch <- testDriverCommand{code: driverCmdClose}
}

func (t *testAsyncDriver) Connect() {
	t.ch <- testDriverCommand{code: driverCmdConnect}
}

func (t *testAsyncDriver) Stop() {
	if t.ch != nil {
		t.ch <- testDriverCommand{code: driverCmdQuit}
		t.wg.Wait()
		close(t.ch)
		t.client.Close()
		t.ch = nil
	}
}

func (t *testAsyncDriver) Publish(batch *outest.Batch) {
	t.ch <- testDriverCommand{code: driverCmdPublish, batch: batch}
}

func (t *testAsyncDriver) Returns() []testClientReturn {
	return t.returns
}
