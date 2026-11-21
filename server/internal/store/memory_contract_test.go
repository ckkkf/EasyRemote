package store_test

import (
	"context"
	"testing"

	"github.com/easyremote/server/internal/store"
	"github.com/easyremote/server/internal/store/storetest"
)

func TestMemoryContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T) store.Store {
		m := store.NewMemory()
		t.Cleanup(func() { _ = m.Close() })
		return m
	})
}

func TestMemoryPingHealthy(t *testing.T) {
	m := store.NewMemory()
	if err := m.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
}
