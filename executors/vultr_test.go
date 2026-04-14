package executors

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cloud66/janitor/core"
)

func newVultrCtx(ts *httptest.Server) context.Context {
	ctx := context.Background()
	ctx = context.WithValue(ctx, core.VultrPatKey, "test-pat")
	ctx = context.WithValue(ctx, core.VultrBaseURLKey, ts.URL)
	return ctx
}

// TestVultr_ServersGet_CursorPagination confirms cursor-based pagination via
// meta.Links.Next: two fixtures, two calls, concatenated result.
func TestVultr_ServersGet_CursorPagination(t *testing.T) {
	var calls int32
	// vultr path for instances — use prefix handler since exact path may vary
	handler := func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "instances") {
			http.NotFound(w, r)
			return
		}
		atomic.AddInt32(&calls, 1)
		cursor := r.URL.Query().Get("cursor")
		if cursor == "" {
			w.Write(readFixture(t, "vultr/instances_list_page1.json"))
			return
		}
		w.Write(readFixture(t, "vultr/instances_list_page2.json"))
	}
	ts := httptest.NewServer(http.HandlerFunc(handler))
	defer ts.Close()

	servers, err := Vultr{}.ServersGet(newVultrCtx(ts), nil, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(servers) != 2 {
		t.Fatalf("want 2 servers across 2 pages, got %d", len(servers))
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Errorf("want 2 HTTP calls, got %d", calls)
	}
}

// TestVultr_LoadBalancersGet_NilInstances ensures a LB with instances=null
// resolves to InstanceCount=0 without panicking.
func TestVultr_LoadBalancersGet_NilInstances(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "load-balancers") {
			http.NotFound(w, r)
			return
		}
		w.Write(readFixture(t, "vultr/load_balancers_list.json"))
	}
	ts := httptest.NewServer(http.HandlerFunc(handler))
	defer ts.Close()

	lbs, err := Vultr{}.LoadBalancersGet(newVultrCtx(ts), false)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(lbs) != 2 {
		t.Fatalf("want 2 LBs, got %d", len(lbs))
	}
	byName := map[string]core.LoadBalancer{}
	for _, lb := range lbs {
		byName[lb.Name] = lb
	}
	if byName["vultr-lb-active"].InstanceCount != 2 {
		t.Errorf("active LB: want 2, got %d", byName["vultr-lb-active"].InstanceCount)
	}
	if byName["vultr-lb-empty"].InstanceCount != 0 {
		t.Errorf("nil-instances LB: want 0, got %d", byName["vultr-lb-empty"].InstanceCount)
	}
}

// TestVultr_LoadBalancerDelete_IDPassthrough: the LB ID stored in
// LoadBalancerArn is sent verbatim to the DELETE endpoint.
func TestVultr_LoadBalancerDelete_IDPassthrough(t *testing.T) {
	var gotPath string
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "not allowed", http.StatusMethodNotAllowed)
			return
		}
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}
	ts := httptest.NewServer(http.HandlerFunc(handler))
	defer ts.Close()

	err := Vultr{}.LoadBalancerDelete(newVultrCtx(ts), core.LoadBalancer{LoadBalancerArn: "lb-verbatim-id"})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !strings.Contains(gotPath, "lb-verbatim-id") {
		t.Errorf("expected LB id in path, got %q", gotPath)
	}
}

// TestVultr_VolumesGet_NoTagsGap documents that Vultr block storage has no
// tags: Tags is always nil.
func TestVultr_VolumesGet_NoTagsGap(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "blocks") {
			http.NotFound(w, r)
			return
		}
		w.Write(readFixture(t, "vultr/blocks_list.json"))
	}
	ts := httptest.NewServer(http.HandlerFunc(handler))
	defer ts.Close()

	vols, err := Vultr{}.VolumesGet(newVultrCtx(ts))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(vols) != 2 {
		t.Fatalf("want 2 vols, got %d", len(vols))
	}
	for _, v := range vols {
		if v.Tags != nil {
			t.Errorf("vultr volume %q: want nil Tags (platform has none), got %v", v.VendorID, v.Tags)
		}
	}
	// attachment mapping
	attachedCount := 0
	for _, v := range vols {
		if v.Attached {
			attachedCount++
		}
	}
	if attachedCount != 1 {
		t.Errorf("want 1 attached volume, got %d", attachedCount)
	}
}

// TestVultr_ServersGet_MalformedDate_B10 pins fixed behavior: a bad DateCreated
// should log WARN and include the instance with Age=0 instead of aborting.
func TestVultr_ServersGet_MalformedDate_B10(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "instances") {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"instances":[{"id":"bad","label":"bad-date","date_created":"not-a-date","region":"ewr","tags":[]}],"meta":{"total":1,"links":{"next":"","prev":""}}}`))
	}
	ts := httptest.NewServer(http.HandlerFunc(handler))
	defer ts.Close()

	servers, err := Vultr{}.ServersGet(newVultrCtx(ts), nil, nil)
	if err != nil {
		t.Fatalf("malformed date must not abort list: %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("want instance included with Age=0, got %d", len(servers))
	}
	if servers[0].Age != 0 {
		t.Errorf("want Age=0 on parse failure, got %v", servers[0].Age)
	}
}
