package executors

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
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
