package httpapi

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tallybook-Org/tallybook/internal/meter"
)

type fakeLedgerSource struct {
	ledger uint32
	err    error
}

func (f fakeLedgerSource) CurrentLedger(context.Context) (uint32, error) {
	if f.err != nil {
		return 0, f.err
	}
	return f.ledger, nil
}

type fakeRecorder struct {
	recorded []RecordedRequest
	err      error
}

func (f *fakeRecorder) RecordRequest(_ context.Context, req RecordedRequest) error {
	if f.err != nil {
		return f.err
	}
	f.recorded = append(f.recorded, req)
	return nil
}

func mustPricer(t *testing.T, unitPrice string) *meter.Pricer {
	t.Helper()
	sched, err := meter.ParseSchedule([]byte(`{"rules":[{"method":"GET","path_template":"/v1/items","unit_price":"` + unitPrice + `"}]}`))
	if err != nil {
		t.Fatalf("ParseSchedule returned unexpected error: %v", err)
	}
	catalog, err := meter.NewCatalog(meter.CatalogVersion{Version: 1, EffectiveLedger: 1000, Schedule: sched})
	if err != nil {
		t.Fatalf("NewCatalog returned unexpected error: %v", err)
	}
	return meter.NewPricer(catalog)
}

func baseConfig(t *testing.T, ledger LedgerSource, recorder Recorder) Config {
	t.Helper()
	return Config{
		Operator:     "GDOLCHAOYP63BEHGAUJJS5IVQNUXLPBWCHO2HZRBTZRU52XBMW2TRJLM",
		Method:       "GET",
		PathTemplate: "/v1/items",
		Pricer:       mustPricer(t, "1000"),
		Ledger:       ledger,
		Recorder:     recorder,
	}
}

func TestMiddleware_RecordsAndCallsNext(t *testing.T) {
	recorder := &fakeRecorder{}
	cfg := baseConfig(t, fakeLedgerSource{ledger: 1500}, recorder)

	mw, err := Middleware(cfg)
	if err != nil {
		t.Fatalf("Middleware returned unexpected error: %v", err)
	}

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/v1/items", nil)
	info := RequestInfo{Consumer: "GAJ46LDZSSYAB4YY6VMPM763P652ZROT7TBG3YYE3BZBAUZDXYDDK6OY", Protocol: ProtocolX402}
	req = req.WithContext(WithRequestInfo(req.Context(), info))
	rec := httptest.NewRecorder()

	mw(next).ServeHTTP(rec, req)

	if !nextCalled {
		t.Fatal("next was not called")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if len(recorder.recorded) != 1 {
		t.Fatalf("recorded %d requests, want 1", len(recorder.recorded))
	}

	got := recorder.recorded[0]
	if got.Operator != cfg.Operator {
		t.Errorf("Operator = %q, want %q", got.Operator, cfg.Operator)
	}
	if got.Consumer != info.Consumer {
		t.Errorf("Consumer = %q, want %q", got.Consumer, info.Consumer)
	}
	if got.Method != "GET" || got.PathTemplate != "/v1/items" {
		t.Errorf("Method/PathTemplate = %q/%q, want GET//v1/items", got.Method, got.PathTemplate)
	}
	if got.UnitCount != 1 {
		t.Errorf("UnitCount = %d, want 1 (default)", got.UnitCount)
	}
	if got.PriceVersion != 1 {
		t.Errorf("PriceVersion = %d, want 1", got.PriceVersion)
	}
	if got.ChargedAmount.Cmp(big.NewInt(1000)) != 0 {
		t.Errorf("ChargedAmount = %s, want 1000", got.ChargedAmount)
	}
	if got.Protocol != ProtocolX402 {
		t.Errorf("Protocol = %q, want %q", got.Protocol, ProtocolX402)
	}
	if got.Channel != "" {
		t.Errorf("Channel = %q, want empty for x402", got.Channel)
	}
	if got.ObservedLedger != 1500 {
		t.Errorf("ObservedLedger = %d, want 1500", got.ObservedLedger)
	}
	if got.RequestID == ([32]byte{}) {
		t.Error("RequestID is all zeros, want a random value")
	}
}

func TestMiddleware_MissingRequestInfoFailsClosed(t *testing.T) {
	recorder := &fakeRecorder{}
	cfg := baseConfig(t, fakeLedgerSource{ledger: 1500}, recorder)
	mw, err := Middleware(cfg)
	if err != nil {
		t.Fatalf("Middleware returned unexpected error: %v", err)
	}

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { nextCalled = true })

	req := httptest.NewRequest("GET", "/v1/items", nil) // no RequestInfo attached
	rec := httptest.NewRecorder()
	mw(next).ServeHTTP(rec, req)

	if nextCalled {
		t.Error("next was called despite missing RequestInfo")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if len(recorder.recorded) != 0 {
		t.Error("a request was recorded despite missing RequestInfo")
	}
}

func TestConfig_Validate(t *testing.T) {
	valid := baseConfig(t, fakeLedgerSource{ledger: 1500}, &fakeRecorder{})

	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{"empty operator", func(c *Config) { c.Operator = "" }},
		{"empty method", func(c *Config) { c.Method = "" }},
		{"empty path template", func(c *Config) { c.PathTemplate = "" }},
		{"nil pricer", func(c *Config) { c.Pricer = nil }},
		{"nil ledger source", func(c *Config) { c.Ledger = nil }},
		{"nil recorder", func(c *Config) { c.Recorder = nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := valid
			tt.mutate(&cfg)
			if _, err := Middleware(cfg); err == nil {
				t.Error("Middleware accepted an invalid config")
			}
		})
	}
}

// --- failure-path coverage: every error source must fail closed ---
//
// Each of these fails for a different reason (bad RequestInfo, a ledger
// fetch error, a pricing error, a recorder error) but must produce the same
// externally observable behaviour: a 500, next never called, and nothing
// recorded. A table keeps that invariant checked uniformly rather than
// duplicated per failure kind.

var errLedgerUnavailable = errors.New("rpc: ledger unavailable")
var errRecordFailed = errors.New("store: connection refused")

func TestMiddleware_FailureSourcesAllFailClosed(t *testing.T) {
	tests := []struct {
		name     string
		cfg      func(t *testing.T, recorder *fakeRecorder) Config
		req      func() *http.Request
		wantCode int
	}{
		{
			name: "invalid RequestInfo (bad protocol)",
			cfg:  func(t *testing.T, r *fakeRecorder) Config { return baseConfig(t, fakeLedgerSource{ledger: 1500}, r) },
			req: func() *http.Request {
				req := httptest.NewRequest("GET", "/v1/items", nil)
				info := RequestInfo{Consumer: "GABC", Protocol: "not-a-real-protocol"}
				return req.WithContext(WithRequestInfo(req.Context(), info))
			},
			wantCode: http.StatusInternalServerError,
		},
		{
			name: "ledger source error",
			cfg: func(t *testing.T, r *fakeRecorder) Config {
				return baseConfig(t, fakeLedgerSource{err: errLedgerUnavailable}, r)
			},
			req: func() *http.Request {
				req := httptest.NewRequest("GET", "/v1/items", nil)
				info := RequestInfo{Consumer: "GABC", Protocol: ProtocolX402}
				return req.WithContext(WithRequestInfo(req.Context(), info))
			},
			wantCode: http.StatusInternalServerError,
		},
		{
			name: "pricer error (unpriced endpoint)",
			cfg: func(t *testing.T, r *fakeRecorder) Config {
				cfg := baseConfig(t, fakeLedgerSource{ledger: 1500}, r)
				cfg.PathTemplate = "/v1/unpriced" // no rule for this path in mustPricer's schedule
				return cfg
			},
			req: func() *http.Request {
				req := httptest.NewRequest("GET", "/v1/unpriced", nil)
				info := RequestInfo{Consumer: "GABC", Protocol: ProtocolX402}
				return req.WithContext(WithRequestInfo(req.Context(), info))
			},
			wantCode: http.StatusInternalServerError,
		},
		{
			name: "pricer error (ledger before any published version)",
			cfg: func(t *testing.T, r *fakeRecorder) Config {
				return baseConfig(t, fakeLedgerSource{ledger: 1}, r) // schedule is effective from ledger 1000
			},
			req: func() *http.Request {
				req := httptest.NewRequest("GET", "/v1/items", nil)
				info := RequestInfo{Consumer: "GABC", Protocol: ProtocolX402}
				return req.WithContext(WithRequestInfo(req.Context(), info))
			},
			wantCode: http.StatusInternalServerError,
		},
		{
			name: "recorder error",
			cfg: func(t *testing.T, r *fakeRecorder) Config {
				r.err = errRecordFailed
				return baseConfig(t, fakeLedgerSource{ledger: 1500}, r)
			},
			req: func() *http.Request {
				req := httptest.NewRequest("GET", "/v1/items", nil)
				info := RequestInfo{Consumer: "GABC", Protocol: ProtocolX402}
				return req.WithContext(WithRequestInfo(req.Context(), info))
			},
			wantCode: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := &fakeRecorder{}
			cfg := tt.cfg(t, recorder)
			mw, err := Middleware(cfg)
			if err != nil {
				t.Fatalf("Middleware returned unexpected error: %v", err)
			}

			nextCalled := false
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { nextCalled = true })

			rec := httptest.NewRecorder()
			mw(next).ServeHTTP(rec, tt.req())

			if nextCalled {
				t.Error("next was called on a failure path")
			}
			if rec.Code != tt.wantCode {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantCode)
			}
			if len(recorder.recorded) != 0 {
				t.Error("a request was recorded on a failure path")
			}
			// The response body must never leak the internal error text —
			// only the generic message http.Error writes.
			if body := rec.Body.String(); body != "internal server error\n" {
				t.Errorf("response body = %q, want the generic \"internal server error\" message only", body)
			}
		})
	}
}

func TestMiddleware_CustomUnitCount(t *testing.T) {
	recorder := &fakeRecorder{}
	cfg := baseConfig(t, fakeLedgerSource{ledger: 1500}, recorder)
	cfg.UnitCount = func(r *http.Request) uint64 { return 7 }

	mw, err := Middleware(cfg)
	if err != nil {
		t.Fatalf("Middleware returned unexpected error: %v", err)
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	req := httptest.NewRequest("GET", "/v1/items", nil)
	info := RequestInfo{Consumer: "GABC", Protocol: ProtocolX402}
	req = req.WithContext(WithRequestInfo(req.Context(), info))
	mw(next).ServeHTTP(httptest.NewRecorder(), req)

	if len(recorder.recorded) != 1 {
		t.Fatalf("recorded %d requests, want 1", len(recorder.recorded))
	}
	got := recorder.recorded[0]
	if got.UnitCount != 7 {
		t.Errorf("UnitCount = %d, want 7", got.UnitCount)
	}
	// unit_price 1000 * 7 units = 7000.
	if got.ChargedAmount.Cmp(big.NewInt(7000)) != 0 {
		t.Errorf("ChargedAmount = %s, want 7000", got.ChargedAmount)
	}
}

func TestMiddleware_RequestIDsAreUniquePerCall(t *testing.T) {
	recorder := &fakeRecorder{}
	cfg := baseConfig(t, fakeLedgerSource{ledger: 1500}, recorder)
	mw, err := Middleware(cfg)
	if err != nil {
		t.Fatalf("Middleware returned unexpected error: %v", err)
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	info := RequestInfo{Consumer: "GABC", Protocol: ProtocolX402}
	const calls = 20
	for i := 0; i < calls; i++ {
		req := httptest.NewRequest("GET", "/v1/items", nil)
		req = req.WithContext(WithRequestInfo(req.Context(), info))
		mw(next).ServeHTTP(httptest.NewRecorder(), req)
	}

	if len(recorder.recorded) != calls {
		t.Fatalf("recorded %d requests, want %d", len(recorder.recorded), calls)
	}
	seen := make(map[[32]byte]bool, calls)
	for _, rec := range recorder.recorded {
		if seen[rec.RequestID] {
			t.Fatalf("duplicate RequestID %x across %d calls", rec.RequestID, calls)
		}
		seen[rec.RequestID] = true
	}
}

func TestMiddleware_MPPSessionChannelIsRecorded(t *testing.T) {
	recorder := &fakeRecorder{}
	cfg := baseConfig(t, fakeLedgerSource{ledger: 1500}, recorder)
	mw, err := Middleware(cfg)
	if err != nil {
		t.Fatalf("Middleware returned unexpected error: %v", err)
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	req := httptest.NewRequest("GET", "/v1/items", nil)
	info := RequestInfo{
		Consumer: "GABC",
		Protocol: ProtocolMPPSession,
		Channel:  "CB2IEP4SQ2GC5747HFHNMXEYWEULC5Z5TTTLET2QA4CAA5SYCWAXFKAW",
	}
	req = req.WithContext(WithRequestInfo(req.Context(), info))
	mw(next).ServeHTTP(httptest.NewRecorder(), req)

	if len(recorder.recorded) != 1 {
		t.Fatalf("recorded %d requests, want 1", len(recorder.recorded))
	}
	if got := recorder.recorded[0].Channel; got != info.Channel {
		t.Errorf("Channel = %q, want %q", got, info.Channel)
	}
	if got := recorder.recorded[0].Protocol; got != ProtocolMPPSession {
		t.Errorf("Protocol = %q, want %q", got, ProtocolMPPSession)
	}
}

func TestRequestInfo_Validate(t *testing.T) {
	tests := []struct {
		name    string
		info    RequestInfo
		wantErr bool
	}{
		{"valid x402", RequestInfo{Consumer: "GABC", Protocol: ProtocolX402}, false},
		{"valid mpp_charge", RequestInfo{Consumer: "GABC", Protocol: ProtocolMPPCharge}, false},
		{"valid mpp_session with channel", RequestInfo{Consumer: "GABC", Protocol: ProtocolMPPSession, Channel: "CDEF"}, false},
		{"empty consumer", RequestInfo{Consumer: "", Protocol: ProtocolX402}, true},
		{"unknown protocol", RequestInfo{Consumer: "GABC", Protocol: "carrier_pigeon"}, true},
		{"mpp_session without channel", RequestInfo{Consumer: "GABC", Protocol: ProtocolMPPSession}, true},
		{"x402 with channel", RequestInfo{Consumer: "GABC", Protocol: ProtocolX402, Channel: "CDEF"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.info.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// ExampleRecorder shows how a caller actually obtains and uses a Recorder.
// Recorder is an interface (metering.go), so "constructing" one means
// implementing RecordRequest — internal/store's request-persistence type
// does this in production — and handing it to Config.Recorder; a config
// left holding the zero value (nil) is rejected at wiring time instead of
// silently recording nothing, which is the difference visible below.
//
// The caller sets only the fields it owns: the Config wiring (Operator,
// Method, PathTemplate, Pricer, Ledger, Recorder) and the request's
// RequestInfo. UnitCount is left unset so the package's default (one unit
// per request) fills it in, and every field of the RecordedRequest —
// request ID, endpoint hash, price version, charged amount, observed
// ledger — is filled in by the middleware, never by the caller.
func ExampleRecorder() {
	// The wiring a caller is required to set, per Config.validate —
	// everything except the Recorder, which comes last so the zero-value
	// case below stays visible.
	sched, err := meter.ParseSchedule([]byte(`{"rules":[{"method":"GET","path_template":"/v1/items","unit_price":"1000"}]}`))
	if err != nil {
		fmt.Println("parse schedule:", err)
		return
	}
	catalog, err := meter.NewCatalog(meter.CatalogVersion{Version: 1, EffectiveLedger: 1000, Schedule: sched})
	if err != nil {
		fmt.Println("new catalog:", err)
		return
	}
	cfg := Config{
		Operator:     "GDOLCHAOYP63BEHGAUJJS5IVQNUXLPBWCHO2HZRBTZRU52XBMW2TRJLM",
		Method:       "GET",
		PathTemplate: "/v1/items",
		Pricer:       meter.NewPricer(catalog),
		Ledger:       fakeLedgerSource{ledger: 1500},
	}

	// The zero value: no implementation to record with, so wiring fails
	// fast and names the problem rather than serving requests unmetered.
	if _, err := Middleware(cfg); err != nil {
		fmt.Println("zero value rejected:", err)
	}

	// A properly-constructed one: an implementation handed to Config.
	// RecordRequest is the only method to satisfy; the in-memory fake
	// stands in for internal/store's type, which needs a database.
	rec := &fakeRecorder{}
	cfg.Recorder = rec

	mw, err := Middleware(cfg)
	if err != nil {
		fmt.Println("middleware:", err)
		return
	}
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	req := httptest.NewRequest("GET", "/v1/items", nil)
	info := RequestInfo{
		Consumer: "GAJ46LDZSSYAB4YY6VMPM763P652ZROT7TBG3YYE3BZBAUZDXYDDK6OY",
		Protocol: ProtocolX402,
	}
	req = req.WithContext(WithRequestInfo(req.Context(), info))
	mw(next).ServeHTTP(httptest.NewRecorder(), req)

	// Everything below was filled in by the package, not the caller.
	got := rec.recorded[0]
	fmt.Println("recorded:", len(rec.recorded))
	fmt.Println("operator:", got.Operator)
	fmt.Println("consumer:", got.Consumer)
	fmt.Println("endpoint:", got.Method, got.PathTemplate)
	fmt.Printf("endpoint hash: %x\n", got.EndpointHash)
	fmt.Println("units:", got.UnitCount, "(package default)")
	fmt.Println("price version:", got.PriceVersion)
	fmt.Println("charged amount:", got.ChargedAmount)
	fmt.Println("protocol:", got.Protocol)
	fmt.Println("observed ledger:", got.ObservedLedger)
	fmt.Println("request id set:", got.RequestID != [32]byte{})

	// Output:
	// zero value rejected: httpapi: metering config: recorder is nil
	// recorded: 1
	// operator: GDOLCHAOYP63BEHGAUJJS5IVQNUXLPBWCHO2HZRBTZRU52XBMW2TRJLM
	// consumer: GAJ46LDZSSYAB4YY6VMPM763P652ZROT7TBG3YYE3BZBAUZDXYDDK6OY
	// endpoint: GET /v1/items
	// endpoint hash: 79a8cbca8c21b745b034907bd0d84cad5dc580f9370b446c9f4030d4e4affb03
	// units: 1 (package default)
	// price version: 1
	// charged amount: 1000
	// protocol: x402
	// observed ledger: 1500
	// request id set: true
}
