package store

import (
	"context"
	"fmt"
	"log"
	"testing"
)

// ExampleMigrations lists every migration embedded in the binary in the
// order Migrate applies them: sorted by version, not by the order the
// directory happened to return them. Name is the descriptive suffix from
// "NNNN_name.sql".
func ExampleMigrations() {
	migrations, err := Migrations()
	if err != nil {
		log.Fatal(err)
	}
	for _, m := range migrations {
		fmt.Printf("%d %s\n", m.Version, m.Name)
	}
	// Output:
	// 1 periods
	// 2 requests
	// 3 commitments
	// 4 channels
	// 5 chain_events
	// 6 settlements
	// 7 anchor_attempts
}

func TestMigrations_LoadsEmbeddedFiles(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatalf("Migrations returned unexpected error: %v", err)
	}
	if len(migrations) < 2 {
		t.Fatalf("got %d migrations, want at least 2 (periods, requests)", len(migrations))
	}
	if migrations[0].Version != 1 || migrations[0].Name != "periods" {
		t.Errorf("migrations[0] = %+v, want version 1 named periods", migrations[0])
	}
	if migrations[1].Version != 2 || migrations[1].Name != "requests" {
		t.Errorf("migrations[1] = %+v, want version 2 named requests", migrations[1])
	}
}

// TestMigrations_ApplyCleanly runs the real embedded migrations against a
// throwaway database and checks the schema they produce actually enforces
// the invariants their CHECK constraints claim to.
func TestMigrations_ApplyCleanly(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	migrations, err := Migrations()
	if err != nil {
		t.Fatalf("Migrations returned unexpected error: %v", err)
	}
	if _, err := Migrate(ctx, pool, migrations); err != nil {
		t.Fatalf("Migrate returned unexpected error: %v", err)
	}

	// A period can be opened and a request filed against it.
	var periodID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO periods (operator, consumer, protocol, price_version, period_start)
		 VALUES ('GOPERATOR', 'GCONSUMER', 'x402', 1, 100) RETURNING id`,
	).Scan(&periodID); err != nil {
		t.Fatalf("insert period: %v", err)
	}

	requestID := make([]byte, 32)
	endpointHash := make([]byte, 32)
	for i := range requestID {
		requestID[i] = byte(i)
		endpointHash[i] = byte(i + 1)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO requests
		 (request_id, operator, consumer, endpoint_hash, method, path_template,
		  unit_count, price_version, charged_amount, protocol, observed_ledger, period_id)
		 VALUES ($1, 'GOPERATOR', 'GCONSUMER', $2, 'GET', '/v1/thing',
		         1, 1, 1000, 'x402', 100, $3)`,
		requestID, endpointHash, periodID,
	); err != nil {
		t.Fatalf("insert request: %v", err)
	}

	// mpp_session without a channel is rejected.
	_, err = pool.Exec(ctx,
		`INSERT INTO periods (operator, consumer, protocol, price_version, period_start)
		 VALUES ('GOPERATOR', 'GCONSUMER2', 'mpp_session', 1, 100)`,
	)
	if err == nil {
		t.Error("insert of mpp_session period without a channel succeeded, want a constraint violation")
	}

	// A second open period for the same (operator, consumer, protocol,
	// channel) scope is rejected by the partial unique index.
	_, err = pool.Exec(ctx,
		`INSERT INTO periods (operator, consumer, protocol, price_version, period_start)
		 VALUES ('GOPERATOR', 'GCONSUMER', 'x402', 2, 200)`,
	)
	if err == nil {
		t.Error("insert of a second open period for the same scope succeeded, want a unique violation")
	}

	// A wrong-length request_id is rejected.
	_, err = pool.Exec(ctx,
		`INSERT INTO requests
		 (request_id, operator, consumer, endpoint_hash, method, path_template,
		  unit_count, price_version, charged_amount, protocol, observed_ledger)
		 VALUES ($1, 'GOPERATOR', 'GCONSUMER', $2, 'GET', '/v1/thing', 1, 1, 1000, 'x402', 100)`,
		[]byte{0x01, 0x02}, endpointHash,
	)
	if err == nil {
		t.Error("insert of a short request_id succeeded, want a constraint violation")
	}
}
