package executors

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cloud66/janitor/core"
)

// readFixture loads a JSON fixture from testdata.
func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", path))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", path, err)
	}
	return b
}

// newDOCtx builds a context pointing the DO executor at the given test server.
func newDOCtx(ts *httptest.Server) context.Context {
	ctx := context.Background()
	ctx = context.WithValue(ctx, core.DOPatKey, "test-pat")
	ctx = context.WithValue(ctx, core.DOBaseURLKey, ts.URL)
	return ctx
}

// TestDigitalOcean_ServersGet_Pagination covers B11 fix: droplets span two pages.
func TestDigitalOcean_ServersGet_Pagination(t *testing.T) {
	// pre-read fixtures on the test goroutine so handler goroutines never call t.Fatal
	page1 := readFixture(t, "digitalocean/droplets_list_page1.json")
	page2 := readFixture(t, "digitalocean/droplets_list_page2.json")
	var calls int32
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/droplets", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		page := r.URL.Query().Get("page")
		if page == "" || page == "1" {
			// host-agnostic: godo only extracts ?page=N from the URL
			w.Write(page1)
			return
		}
		w.Write(page2)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	d := DigitalOcean{}
	servers, err := d.ServersGet(newDOCtx(ts), nil, nil)
	if err != nil {
		t.Fatalf("ServersGet: %v", err)
	}
	if len(servers) != 3 {
		t.Fatalf("expected 3 droplets across 2 pages, got %d", len(servers))
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("expected exactly 2 HTTP calls, got %d", calls)
	}
}

// TestDigitalOcean_LoadBalancersGet covers tag-based LBs, region nil, and
// happy-path mapping.
func TestDigitalOcean_LoadBalancersGet(t *testing.T) {
	t.Run("explicit droplets", func(t *testing.T) {
		body := readFixture(t, "digitalocean/load_balancers_explicit_droplets.json")
		mux := http.NewServeMux()
		mux.HandleFunc("/v2/load_balancers", func(w http.ResponseWriter, r *http.Request) {
			w.Write(body)
		})
		ts := httptest.NewServer(mux)
		defer ts.Close()

		lbs, err := DigitalOcean{}.LoadBalancersGet(newDOCtx(ts), false)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if len(lbs) != 1 {
			t.Fatalf("want 1, got %d", len(lbs))
		}
		if lbs[0].InstanceCount != 3 {
			t.Errorf("want 3 instances, got %d", lbs[0].InstanceCount)
		}
		if lbs[0].LoadBalancerArn != "lb-uuid-1" {
			t.Errorf("want UUID passthrough, got %q", lbs[0].LoadBalancerArn)
		}
	})

	t.Run("tag-based lb returns zero instance count (documents current behavior)", func(t *testing.T) {
		// PINNED SMELL: DO's /v2/load_balancers response only carries droplet_ids
		// when the LB was created with explicit droplets. Tag-based LBs omit the
		// list, so InstanceCount=0 is what the API gives us — a true count would
		// require a second /v2/droplets?tag=<x> call per LB. Accepted until the
		// resolver path is implemented; update this test if that changes.
		body := readFixture(t, "digitalocean/load_balancers_tag_based.json")
		mux := http.NewServeMux()
		mux.HandleFunc("/v2/load_balancers", func(w http.ResponseWriter, r *http.Request) {
			w.Write(body)
		})
		ts := httptest.NewServer(mux)
		defer ts.Close()

		lbs, err := DigitalOcean{}.LoadBalancersGet(newDOCtx(ts), false)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if lbs[0].InstanceCount != 0 {
			t.Errorf("tag-based LB should currently show InstanceCount=0, got %d", lbs[0].InstanceCount)
		}
	})

	t.Run("malformed Created includes LB with Age=0 and logs WARN (B10 fix)", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/v2/load_balancers", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"load_balancers":[{"id":"lb-bad","name":"bad-date-lb","created_at":"not-a-date","droplet_ids":[],"region":{"slug":"x"}}],"links":{},"meta":{"total":1}}`)
		})
		ts := httptest.NewServer(mux)
		defer ts.Close()

		warnBuf := &bytes.Buffer{}
		ctx := context.WithValue(newDOCtx(ts), core.WarnWriterKey, warnBuf)

		lbs, err := DigitalOcean{}.LoadBalancersGet(ctx, false)
		if err != nil {
			t.Fatalf("malformed Created must NOT abort list: %v", err)
		}
		if len(lbs) != 1 {
			t.Fatalf("want 1 LB included with Age=0, got %d", len(lbs))
		}
		if lbs[0].Age != 0 {
			t.Errorf("want Age=0 on parse failure, got %v", lbs[0].Age)
		}
		if !strings.Contains(warnBuf.String(), "[WARN]") {
			t.Errorf("expected WARN log in sink, got %q", warnBuf.String())
		}
	})

	t.Run("nil region does not panic", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/v2/load_balancers", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"load_balancers":[{"id":"x","name":"n","created_at":"2024-01-01T00:00:00Z","droplet_ids":[]}],"links":{},"meta":{"total":1}}`)
		})
		ts := httptest.NewServer(mux)
		defer ts.Close()

		lbs, err := DigitalOcean{}.LoadBalancersGet(newDOCtx(ts), false)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if lbs[0].Region != "" {
			t.Errorf("want empty region on nil, got %q", lbs[0].Region)
		}
	})

	t.Run("500 propagates error", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/v2/load_balancers", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"id":"server_error","message":"boom"}`, http.StatusInternalServerError)
		})
		ts := httptest.NewServer(mux)
		defer ts.Close()

		_, err := DigitalOcean{}.LoadBalancersGet(newDOCtx(ts), false)
		if err == nil {
			t.Fatalf("expected error on 500, got nil")
		}
	})
}

// TestDigitalOcean_LoadBalancerDelete asserts the UUID is sent verbatim.
func TestDigitalOcean_LoadBalancerDelete(t *testing.T) {
	var gotPath string
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/load_balancers/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("want DELETE, got %s", r.Method)
		}
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	err := DigitalOcean{}.LoadBalancerDelete(newDOCtx(ts), core.LoadBalancer{LoadBalancerArn: "abc-123-def"})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if gotPath != "/v2/load_balancers/abc-123-def" {
		t.Errorf("want UUID passed verbatim, got path %q", gotPath)
	}
}

func TestDigitalOcean_VolumesGet(t *testing.T) {
	body := readFixture(t, "digitalocean/volumes_unattached.json")
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/volumes", func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	vols, err := DigitalOcean{}.VolumesGet(newDOCtx(ts))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(vols) != 2 {
		t.Fatalf("want 2 volumes, got %d", len(vols))
	}
	// Attached flag should map from DropletIDs being non-empty
	var orphan, attached core.Volume
	for _, v := range vols {
		if v.VendorID == "vol-1" {
			orphan = v
		}
		if v.VendorID == "vol-2" {
			attached = v
		}
	}
	if orphan.Attached {
		t.Errorf("vol-1 should be unattached")
	}
	if !attached.Attached {
		t.Errorf("vol-2 should be attached")
	}
}

func TestDigitalOcean_SshKeysGet(t *testing.T) {
	body := readFixture(t, "digitalocean/ssh_keys_list.json")
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/account/keys", func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	keys, err := DigitalOcean{}.SshKeysGet(newDOCtx(ts))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("want 2 keys, got %d", len(keys))
	}
}
