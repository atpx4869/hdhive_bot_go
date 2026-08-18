package app

import (
	"context"
	"testing"
	"time"

	"github.com/atpx4869/hdhive_bot_go/internal/store"
	"github.com/atpx4869/hdhive_bot_go/internal/telegram"
)

func TestTransferAdapterSharesInFlightResult(t *testing.T) {
	adapter := &TransferAdapter{calls: map[string]*transferCall{}}
	call := &transferCall{done: make(chan struct{})}
	adapter.calls["1:r1"] = call

	resultCh := make(chan string, 1)
	go func() {
		result, _ := adapter.Transfer115(context.Background(), 1, store.P115Config{}, telegram.Resource{ID: "r1"})
		resultCh <- result
	}()
	call.result = "已接收"
	close(call.done)
	select {
	case result := <-resultCh:
		if result != "已接收" {
			t.Fatalf("unexpected shared result: %q", result)
		}
	case <-time.After(time.Second):
		t.Fatal("waiting caller did not receive shared result")
	}
}

func TestTransferAdapterDoesNotKeepCompletedEntry(t *testing.T) {
	adapter := &TransferAdapter{calls: map[string]*transferCall{}}
	key := "1:r1"
	call := &transferCall{done: make(chan struct{})}
	adapter.calls[key] = call
	delete(adapter.calls, key)
	if _, exists := adapter.calls[key]; exists {
		t.Fatal("completed transfer entry must not remain cached")
	}
}
