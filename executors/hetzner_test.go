package executors

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cloud66/janitor/core"
)

// newHetznerCtx builds a context pointing the Hetzner executor at the test server.
// hcloud mounts routes under /v1, so the test server URL is passed verbatim.
func newHetznerCtx(ts *httptest.Server) context.Context {
	ctx := context.Background()
	ctx = context.WithValue(ctx, core.HetznerPatKey, "test-pat")
	ctx = context.WithValue(ctx, core.HetznerBaseURLKey, ts.URL+"/v1")
	return ctx
}

func TestHetzner_LoadBalancersGet_TargetTypes(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/load_balancers", func(w http.ResponseWriter, r *http.Request) {
		w.Write(readFixture(t, "hetzner/load_balancers_with_targets.json"))
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	lbs, err := Hetzner{}.LoadBalancersGet(newHetznerCtx(ts), false)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(lbs) != 2 {
		t.Fatalf("want 2 LBs, got %d", len(lbs))
	}

	// find by name
	byName := map[string]core.LoadBalancer{}
	for _, lb := range lbs {
		byName[lb.Name] = lb
	}

	// server-target + IP-target LB: 2 servers + 1 IP = 3
	if got := byName["lb-server-targets"].InstanceCount; got != 3 {
		t.Errorf("server+ip LB: want 3 instances, got %d", got)
	}
	// label selector with empty Targets: counts as 1 (pins current +1 behavior)
	if got := byName["lb-label-selector-empty"].InstanceCount; got != 1 {
		t.Errorf("empty label-selector LB: want 1 (pins +1 fallback), got %d", got)
	}
}

func TestHetzner_LoadBalancerDelete_B9_IDParsing(t *testing.T) {
	// hit the mux only if parsing succeeds — for invalid input we expect an
	// early error and no HTTP call.
	var called int
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/load_balancers/", func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// B9: "123abc" with fmt.Sscanf("%d", ...) partially consumed "123"
	// and silently deleted LB 123. parseHetznerID must reject it.
	err1 := Hetzner{}.LoadBalancerDelete(newHetznerCtx(ts), core.LoadBalancer{LoadBalancerArn: "123abc"})
	if err1 == nil {
		t.Errorf("B9: want error on %q, got nil", "123abc")
	}
	err2 := Hetzner{}.LoadBalancerDelete(newHetznerCtx(ts), core.LoadBalancer{LoadBalancerArn: ""})
	if err2 == nil {
		t.Errorf("want error on empty ID, got nil")
	}

	// B9 tightening: reject non-positive IDs and leading '+' sign and
	// surrounding whitespace. strconv.ParseInt by itself would silently
	// accept these and we'd then issue a DELETE for id=-1/0/123, which is
	// either a no-op or dangerously wrong.
	for _, bad := range []string{"-1", "0", "+123", " 123 "} {
		err := Hetzner{}.LoadBalancerDelete(newHetznerCtx(ts), core.LoadBalancer{LoadBalancerArn: bad})
		if err == nil {
			t.Errorf("B9 tighten: want error on %q, got nil", bad)
		}
	}

	if called != 0 {
		t.Errorf("no HTTP call should be made on invalid IDs, got %d", called)
	}

	// success path
	err3 := Hetzner{}.LoadBalancerDelete(newHetznerCtx(ts), core.LoadBalancer{LoadBalancerArn: "123"})
	if err3 != nil {
		t.Errorf("valid ID should succeed, got %v", err3)
	}
	if called != 1 {
		t.Errorf("expected 1 HTTP call for valid ID, got %d", called)
	}
}

func TestHetzner_LabelsToTags_StableOrder(t *testing.T) {
	// hetznerLabelsToTags must return a deterministic slice regardless of
	// Go's random map iteration order.
	labels := map[string]string{"z": "1", "a": "2", "m": "3"}
	got := hetznerLabelsToTags(labels)
	want := []string{"a=2", "m=3", "z=1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("want %v, got %v", want, got)
	}
	// also confirm sorted invariant
	if !sort.StringsAreSorted(got) {
		t.Errorf("not sorted: %v", got)
	}
}

func TestHetzner_ServersGet(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/servers", func(w http.ResponseWriter, r *http.Request) {
		w.Write(readFixture(t, "hetzner/servers_list.json"))
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	servers, err := Hetzner{}.ServersGet(newHetznerCtx(ts), nil, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("want 1 server, got %d", len(servers))
	}
	if servers[0].VendorID != "42" {
		t.Errorf("want VendorID=42, got %q", servers[0].VendorID)
	}
	// labels → sorted tag strings
	if !strings.Contains(strings.Join(servers[0].Tags, ","), "env=prod") {
		t.Errorf("expected env=prod tag, got %v", servers[0].Tags)
	}
}

// TestHetzner_LoadBalancersGet_MissingCreated_B10 pins the B10 behavior: when
// the API returns a zero-value `created` (null), the LB is still included in
// the result with Age=0 and a WARN line is surfaced via the WarnWriter sink.
func TestHetzner_LoadBalancersGet_MissingCreated_B10(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/load_balancers", func(w http.ResponseWriter, r *http.Request) {
		w.Write(readFixture(t, "hetzner/load_balancers_missing_created.json"))
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// capture warn output through the ctx-scoped writer sink
	warnBuf := &bytes.Buffer{}
	ctx := context.WithValue(newHetznerCtx(ts), core.WarnWriterKey, warnBuf)

	lbs, err := Hetzner{}.LoadBalancersGet(ctx, false)
	if err != nil {
		t.Fatalf("missing Created must NOT abort list: %v", err)
	}
	if len(lbs) != 1 {
		t.Fatalf("want 1 LB included with Age=0, got %d", len(lbs))
	}
	if lbs[0].Age != 0 {
		t.Errorf("want Age=0 on zero-value Created, got %v", lbs[0].Age)
	}
	out := warnBuf.String()
	if !strings.Contains(out, "[WARN]") {
		t.Errorf("expected WARN log in sink, got %q", out)
	}
	// surfaced warning should reference the LB by name so operators can find it
	if !strings.Contains(out, "lb-missing-created") {
		t.Errorf("expected WARN to mention LB name, got %q", out)
	}
}

// TestHetzner_LoadBalancersGet_Pagination ensures hcloud's internal pagination
// across meta.pagination.next_page is exercised end-to-end. Regression guard:
// a future refactor that skips subsequent pages would drop LBs silently.
func TestHetzner_LoadBalancersGet_Pagination(t *testing.T) {
	var calls int32
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/load_balancers", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		// hcloud only parses Meta.Pagination when Content-Type is JSON
		w.Header().Set("Content-Type", "application/json")
		// hcloud advances via ?page=N query param; page defaults to 1 when absent
		page := r.URL.Query().Get("page")
		if page == "" || page == "1" {
			w.Write(readFixture(t, "hetzner/load_balancers_page1.json"))
			return
		}
		w.Write(readFixture(t, "hetzner/load_balancers_page2.json"))
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	lbs, err := Hetzner{}.LoadBalancersGet(newHetznerCtx(ts), false)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(lbs) != 3 {
		t.Fatalf("expected 3 LBs across 2 pages, got %d", len(lbs))
	}
	if atomic.LoadInt32(&calls) < 2 {
		t.Fatalf("expected at least 2 HTTP calls for pagination, got %d", calls)
	}
	// assert names from both pages appear
	names := map[string]bool{}
	for _, lb := range lbs {
		names[lb.Name] = true
	}
	for _, want := range []string{"lb-page1-a", "lb-page1-b", "lb-page2-a"} {
		if !names[want] {
			t.Errorf("expected LB %q in results, missing", want)
		}
	}
}
