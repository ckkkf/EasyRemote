//go:build integration

package store_test

import (
	"context"
	"crypto/rand"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/easyremote/server/internal/session"
	"github.com/easyremote/server/internal/stack"
	"github.com/easyremote/server/internal/store"
	"github.com/easyremote/server/internal/store/storetest"
	"github.com/jackc/pgx/v5"
)

// 与内存实现跑完全相同的契约用例——这是「PG 实现不能靠肉眼保证正确」的落地。
func TestPGContract(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping PG contract test")
	}
	base, err := url.Parse(dsn)
	if err != nil || (base.Scheme != "postgres" && base.Scheme != "postgresql") {
		t.Fatal("DATABASE_URL must be a PostgreSQL URL")
	}
	ddl, err := stack.LoadMigration()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	admin, err := store.NewPG(ctx, dsn)
	cancel()
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = admin.Close() })

	newStore := func(t *testing.T) store.Store {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		schema := "er_contract_" + strings.ToLower(rand.Text())
		ident := pgx.Identifier{schema}.Sanitize()
		if _, err := admin.ExecRaw(ctx, "CREATE SCHEMA "+ident); err != nil {
			t.Fatalf("isolate contract test: %v", err)
		}
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if _, err := admin.ExecRaw(ctx, "DROP SCHEMA "+ident+" CASCADE"); err != nil {
				t.Errorf("cleanup contract schema: %v", err)
			}
		})
		scoped := *base
		query := scoped.Query()
		query.Set("search_path", schema)
		scoped.RawQuery = query.Encode()
		pg, err := store.NewPG(ctx, scoped.String())
		if err != nil {
			t.Fatalf("connect to contract schema: %v", err)
		}
		t.Cleanup(func() { _ = pg.Close() })
		if err := pg.Migrate(ctx, ddl); err != nil {
			t.Fatalf("migrate contract schema: %v", err)
		}
		return pg
	}
	storetest.Run(t, newStore)
	t.Run("ExpiredRequestReplacement", func(t *testing.T) {
		testPGExpiredRequestReplacement(t, newStore(t))
	})
}

func testPGExpiredRequestReplacement(t *testing.T, st store.Store) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	svc := session.New(st)
	now := time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC)
	svc.SetClock(func() time.Time { return now })
	first, err := svc.Request(ctx, "controller", "target")
	if err != nil {
		t.Fatal(err)
	}
	authorized, err := svc.Consent(ctx, first.ID, true, "")
	if err != nil {
		t.Fatal(err)
	}
	now = authorized.AuthExpireAt.Add(time.Second)
	next, err := svc.Request(ctx, "controller", "target")
	if err != nil || next.State != session.StateRequested || next.ID == first.ID {
		t.Fatalf("request reused expired authorization: %+v %v", next, err)
	}
	old, err := svc.Get(ctx, first.ID)
	if err != nil || old.State != session.StateClosed || old.Reason != "auth_window_exceeded" {
		t.Fatalf("expired authorization was not closed: %+v %v", old, err)
	}
	events, err := st.ListSessionEvents(ctx, false, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.SessionID == old.ID && event.OldState == session.StateAuthorized &&
			event.NewState == old.State && event.Version == old.Version && event.Reason == old.Reason {
			return
		}
	}
	t.Fatal("missing outbox event for expired authorization")
}

func TestPGPing(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	pg, err := store.NewPG(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()
	if err := pg.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
}
