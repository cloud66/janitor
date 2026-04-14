package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/cloud66/janitor/core"
	"github.com/cloud66/janitor/executors"
)

// out is the package-level sink for all user-visible output. Tests can swap
// this to a bytes.Buffer via captureOutput to assert on printed skip reasons.
var out io.Writer = os.Stdout

const (
	actionWebServer = "webserver"
	actionDelete    = "delete"

	//Defaults
	defaultSshKeyKeepCount = 10
)

var (
	clouds     map[string]core.ExecutorInterface
	flagAction string

	flagMaxAgeNormal     float64
	flagMaxAgeLong       float64
	flagSshKeysKeepCount int

	flagClouds string
	flagMock   bool
	flagYes    bool

	//credentials
	flagDOPat              string
	flagAWSAccessKeyID     string
	flagAWSSecretAccessKey string
	flagVultrPat           string
	flagHetznerPat         string
)

// requireYesGate returns "" when a delete run may proceed, or the operator-
// facing refusal message when --mock=false was set without --yes. extracted
// from main() so it's directly unit-testable without invoking os.Exit.
func requireYesGate(mock, yes bool) string {
	if mock || yes {
		return ""
	}
	return "Refusing to run with --mock=false without --yes on the command line."
}

func prettyPrint(message string, mock bool) {
	// route through the package-level out sink so tests can capture output.
	// Fprintf errors on stdout / bytes.Buffer are not actionable → ignore.
	if mock {
		_, _ = fmt.Fprintf(out, "[MOCK] %s", message)
	} else {
		_, _ = fmt.Fprintf(out, "%s", message)
	}
}

func handler(w http.ResponseWriter, r *http.Request) {
	// best-effort write to the HTTP response; the connection may already be
	// torn down by the time this returns, so ignore the error.
	_, _ = fmt.Fprint(w, "Its a TRAP!")
}

func main() {
	//action
	flag.StringVar(&flagAction, "action", "", "Action to perform: delete|stop|start")
	//credentials
	flag.StringVar(&flagDOPat, "do-pat", os.Getenv("JANITOR_DO_PAT"), "DigitalOcean Personal Access Token")
	flag.StringVar(&flagAWSAccessKeyID, "aws-access-key-id", os.Getenv("JANITOR_AWS_ACCESS_KEY_ID"), "AWS Access Key ID")
	flag.StringVar(&flagAWSSecretAccessKey, "aws-secret-access-key", os.Getenv("JANITOR_AWS_SECRET_ACCESS_KEY"), "AWS Secret Access Key")
	flag.StringVar(&flagVultrPat, "vultr-pat", os.Getenv("JANITOR_VULTR_PAT"), "Vultr Personal Access Token")
	flag.StringVar(&flagHetznerPat, "hetzner-pat", os.Getenv("JANITOR_HETZNER_PAT"), "Hetzner Personal Access Token")
	//config
	flag.BoolVar(&flagMock, "mock", strings.ToLower(os.Getenv("MOCK")) != "false", "Don't actually delete anything, just show what *would* happen")
	// --yes must be passed explicitly on the command line for any live (non-
	// mock) run. env-source is intentionally NOT honoured: a CI step inheriting
	// JANITOR_YES from a parent shell would otherwise enable destructive runs
	// with no flag visible in `ps` / audit logs (panel finding C2).
	flag.BoolVar(&flagYes, "yes", false, "Required CLI flag for non-mock deletions; --mock=false without --yes is rejected.")
	flag.StringVar(&flagClouds, "clouds", "", "Clouds to work on (comma separated for multiple)")

	var maxAgeNormal, maxAgeLong float64
	var sshKeysKeepCount int
	if os.Getenv("MAX_AGE_NORMAL") != "" {
		maxAgeNormal, _ = strconv.ParseFloat(os.Getenv("MAX_AGE_NORMAL"), 64)
	} else {
		maxAgeNormal = 0.38
	}
	if os.Getenv("MAX_AGE_LONG") != "" {
		maxAgeLong, _ = strconv.ParseFloat(os.Getenv("MAX_AGE_LONG"), 64)
	} else {
		maxAgeLong = 5.0
	}
	if os.Getenv("SSH_KEYS_KEEP_COUNT") != "" {
		sshKeysKeepCountParsed, _ := strconv.ParseInt(os.Getenv("SSH_KEYS_KEEP_COUNT"), 10, 0)
		sshKeysKeepCount = int(sshKeysKeepCountParsed)
		if sshKeysKeepCount < 0 {
			sshKeysKeepCount = defaultSshKeyKeepCount
		}
	} else {
		sshKeysKeepCount = defaultSshKeyKeepCount
	}

	flag.Float64Var(&flagMaxAgeNormal, "max-age-regular", maxAgeNormal, "Normal allowed server age (days). Decimal allowed. Anything older will be deleted!")
	flag.Float64Var(&flagMaxAgeLong, "max-age-long", maxAgeLong, "Long allowed server age (days). Decimal allowed. Anything older will be deleted!")
	flag.IntVar(&flagSshKeysKeepCount, "ssh-keys-keep-count", sshKeysKeepCount, "Number of non-user defined SSH keys to keep.")
	flag.Parse()

	if flagAction == actionWebServer {
		http.HandleFunc("/", handler)
		res := http.ListenAndServe(":1234", nil)
		fmt.Println(res)
		os.Exit(0)
	}

	if flagClouds == "" {
		fmt.Println("No cloud provider is specified. Use the --clouds option")
		os.Exit(1)
	}

	clouds = make(map[string]core.ExecutorInterface)
	//Just add new clouds here
	clouds["digitalocean"] = executors.DigitalOcean{}
	clouds["aws"] = executors.Aws{}
	clouds["vultr"] = executors.Vultr{}
	clouds["hetzner"] = executors.Hetzner{}

	ctx := context.Background()
	// typed keys for non-AWS executors (go vet SA1029 clean)
	ctx = context.WithValue(ctx, core.DOPatKey, flagDOPat)
	ctx = context.WithValue(ctx, core.VultrPatKey, flagVultrPat)
	ctx = context.WithValue(ctx, core.HetznerPatKey, flagHetznerPat)
	// AWS executor now reads typed ctx keys only (Phase 5 migration complete).
	ctx = context.WithValue(ctx, core.AWSAccessKeyIDKey, flagAWSAccessKeyID)
	ctx = context.WithValue(ctx, core.AWSSecretAccessKeyKey, flagAWSSecretAccessKey)
	// route warnings to stderr so pipes like `janitor ... | tee report` keep
	// data and diagnostics separate; normal output stays on the `out` sink.
	ctx = context.WithValue(ctx, core.WarnWriterKey, io.Writer(os.Stderr))
	ctx = context.WithValue(ctx, core.OutWriterKey, out)

	if flagAction == actionDelete {
		// guard: --mock=false is destructive; require --yes on the CLI.
		if msg := requireYesGate(flagMock, flagYes); msg != "" {
			fmt.Fprintln(os.Stderr, msg)
			os.Exit(2)
		}
		// loud banner: announce live mode + the cloud(s) being targeted so
		// operators see what's about to happen before any API call fires.
		if !flagMock {
			fmt.Fprintf(os.Stderr, "*** LIVE DELETION MODE — clouds=%s ***\n", flagClouds)
		}
		prettyPrint(fmt.Sprintf("[%s ACTION]\n", strings.ToUpper(flagAction)), flagMock)
		prettyPrint(fmt.Sprintf("NORMAL ALLOWANCE: %.3f days (%.0f hours)\n", flagMaxAgeNormal, flagMaxAgeNormal*24.0), flagMock)
		prettyPrint(fmt.Sprintf("LONG ALLOWANCE: %.3f days (%.0f hours)\n", flagMaxAgeLong, flagMaxAgeLong*24.0), flagMock)

	} else {
		fmt.Printf("Unrecognised action '%s'\n", flagAction)
		os.Exit(1)
	}

	userClouds := strings.Split(flagClouds, ",")
	for _, userCloud := range userClouds {
		//Output the cloud
		fmt.Println()
		prettyPrint(fmt.Sprintf("[%s]\n", strings.ToUpper(userCloud)), flagMock)

		if _, ok := clouds[userCloud]; !ok {
			// in live mode, refuse to silently proceed past a typo'd cloud
			// token (e.g. `--clouds=aws,awz`); a no-op on `awz` with deletes
			// against `aws` is exactly the failure mode --yes guards against.
			if !flagMock {
				fmt.Fprintf(os.Stderr, "Unknown cloud %q in --clouds=%q; refusing to continue in live mode.\n", userCloud, flagClouds)
				os.Exit(2)
			}
			fmt.Printf("Unsupported cloud %q (skipping)\n", userCloud)
			continue
		}

		executor := clouds[userCloud]
		ctx = context.WithValue(ctx, core.ExecutorKey, executor)

		servers, err := executor.ServersGet(ctx, nil, nil)
		if err != nil {
			// match the LB/SSH/Volume callers: a provider that does not
			// implement ServersGet returns ErrUnsupported — treat as silent
			// skip rather than printing an error.
			if !errors.Is(err, core.ErrUnsupported) {
				fmt.Printf("[%s] Cannot get servers due to %s\n", userCloud, err.Error())
			}
		} else {
			prettyPrint(fmt.Sprintf("[%d SERVERS]\n", len(servers)), flagMock)
			sort.Sort(core.ServerSorter(servers))
			if flagAction == actionDelete {
				deleteServers(ctx, userCloud, servers)
			}
		}

		if flagAction == actionDelete {
			loadBalancers, err := executor.LoadBalancersGet(ctx, flagMock)
			if err != nil {
				if !errors.Is(err, core.ErrUnsupported) {
					fmt.Printf("Cannot get load balancers due to %s\n", err.Error())
				}
			} else {
				prettyPrint(fmt.Sprintf("[%d LOAD BALANCERS]\n", len(loadBalancers)), flagMock)
				sort.Sort(core.LoadBalancerSorter(loadBalancers))
				deleteLoadBalancers(ctx, loadBalancers)
			}
		}

		if flagAction == actionDelete {
			sshKeys, err := executor.SshKeysGet(ctx)
			if err != nil {
				if !errors.Is(err, core.ErrUnsupported) {
					fmt.Printf("Cannot get SSH keys due to %s\n", err.Error())
				}
			} else {
				prettyPrint(fmt.Sprintf("[%d SSH KEYS]\n", len(sshKeys)), flagMock)
				sort.Sort(core.SshKeySorter(sshKeys))
				deleteSshKeys(ctx, sshKeys)
			}
		}

		if flagAction == actionDelete {
			volumes, err := executor.VolumesGet(ctx)
			if err != nil {
				if !errors.Is(err, core.ErrUnsupported) {
					fmt.Printf("Cannot get volumes due to %s\n", err.Error())
				}
			} else {
				prettyPrint(fmt.Sprintf("[%d VOLUMES]\n", len(volumes)), flagMock)
				sort.Sort(core.VolumeSorter(volumes))
				deleteVolumes(ctx, volumes)
			}
		}
	}
}

// nameTokens splits a resource name on the common identifier delimiters
// (-, _, ., whitespace) and lowercases each token. used for word-boundary
// matching so `alongside-prod` does not register as containing "long" and
// `prolonged-task` does not match either, while `my-long-running-job` still
// does. addresses panel round-2 C7 (B4 substring false positives).
func nameTokens(name string) []string {
	if name == "" {
		return nil
	}
	lower := strings.ToLower(name)
	return strings.FieldsFunc(lower, func(r rune) bool {
		return r == '-' || r == '_' || r == '.' || r == ' ' || r == '\t'
	})
}

// nameMatchesToken returns true when any delimiter-split token of name equals
// marker (already lowercase).
func nameMatchesToken(name, marker string) bool {
	for _, tok := range nameTokens(name) {
		if tok == marker {
			return true
		}
	}
	return false
}

// tagValueMatchesToken returns true when any tag is `key=value` and any
// delimiter-split token of value equals marker. word-boundary semantics on
// the value mirror nameTokens so `lifecycle=long-running` matches "long" but
// `lifecycle=prolonged-window` does not. require explicit `key=value` form:
// bare tags are not allowed to pin resources.
func tagValueMatchesToken(tags []string, marker string) bool {
	for _, tag := range tags {
		i := strings.IndexByte(tag, '=')
		if i < 0 {
			continue
		}
		if nameMatchesToken(tag[i+1:], marker) {
			return true
		}
	}
	return false
}

// isPermanent — name token OR any tag-value token matches "permanent".
func isPermanent(name string, tags []string) bool {
	if nameMatchesToken(name, core.TagPermanent) {
		return true
	}
	return tagValueMatchesToken(tags, core.TagPermanent)
}

// hasLongName — name token OR any tag-value token matches "long".
func hasLongName(name string, tags []string) bool {
	if nameMatchesToken(name, core.TagLong) {
		return true
	}
	return tagValueMatchesToken(tags, core.TagLong)
}

// stripInvisibleAndSpace removes ASCII whitespace AND the common zero-width
// Unicode characters that an attacker could insert to evade the sample-tag
// check. U+200B/C/D and BOM are the realistic vectors via tag UIs that
// round-trip Unicode untouched.
func stripInvisibleAndSpace(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '\u200B', '\u200C', '\u200D', '\uFEFF':
			return -1
		case ' ', '\t', '\n', '\r':
			return -1
		}
		return r
	}, s)
}

// hasSampleTag checks if any tag has key core.TagKeyC66Stack with a value
// containing the sample marker. zero-width characters in the key are
// stripped before comparison so they cannot be used to evade the
// sample-skip safety net.
func hasSampleTag(tags []string) bool {
	wantKey := strings.ToLower(core.TagKeyC66Stack)
	for _, tag := range tags {
		i := strings.IndexByte(tag, '=')
		if i < 0 {
			continue
		}
		key := strings.ToLower(stripInvisibleAndSpace(tag[:i]))
		if key != wantKey {
			continue
		}
		val := strings.ToLower(tag[i+1:])
		if strings.Contains(val, core.TagSample) {
			return true
		}
	}
	return false
}

func deleteServers(ctx context.Context, cloud string, servers []core.Server) {
	_ = cloud // retained for callers; sample-tag check now applies to all clouds.
	for _, server := range servers {
		if hasSampleTag(server.Tags) {
			// skip any server with a C66-STACK tag containing "sample" —
			// previously only checked for vultr, leaving AWS/DO/Hetzner sample
			// stacks vulnerable to deletion.
			printServer(server, "SMPL")
			_, _ = fmt.Fprintf(out, "skipped (sample tag)\n")
		} else if isPermanent(server.Name, server.Tags) {
			printServer(server, "PERM")
			_, _ = fmt.Fprintf(out, "skipped (permanent)\n")
		} else if server.Age <= 0 {
			// B10: Age=0 means Created was missing/malformed. do not let a
			// hasLongName/normal predicate decide deletion based on a
			// fabricated age. skip and surface the reason.
			printServer(server, "WARN")
			_, _ = fmt.Fprintf(out, "skipped (unknown age — malformed Created)\n")
		} else if hasLongName(server.Name, server.Tags) {
			printServer(server, "LONG")
			if server.Age > flagMaxAgeLong {
				if flagMock {
					_, _ = fmt.Fprintf(out, "Mock deleted!\n")
				} else {
					deleteServer(ctx, server)
				}
			} else {
				_, _ = fmt.Fprintf(out, "skipped (age)\n")
			}
		} else {
			printServer(server, "NORM")
			if server.Age > flagMaxAgeNormal {
				if flagMock {
					_, _ = fmt.Fprintf(out, "Mock deleted!\n")
				} else {
					deleteServer(ctx, server)
				}
			} else {
				_, _ = fmt.Fprintf(out, "skipped (age)\n")
			}
		}
	}
}

func deleteServer(ctx context.Context, server core.Server) {
	executor := ctx.Value(core.ExecutorKey).(core.ExecutorInterface)
	err := executor.ServerDelete(ctx, server)
	if err != nil {
		_, _ = fmt.Fprintf(out, "ERROR: %s\n", err.Error())
	} else {
		_, _ = fmt.Fprintf(out, "Deleted!\n")
	}
}

func deleteLoadBalancers(ctx context.Context, loadBalancers []core.LoadBalancer) {
	// minimum age threshold: 1 hour (in days)
	minAge := 1.0 / 24.0

	for _, loadBalancer := range loadBalancers {
		if isPermanent(loadBalancer.Name, loadBalancer.Tags) {
			printLoadBalancer(loadBalancer, "PERM")
			_, _ = fmt.Fprintf(out, "skipped (permanent)\n")
		} else if loadBalancer.Age <= 0 {
			// defense-in-depth: zero/negative age means Created was missing or
			// malformed upstream; never let predicates drive deletion off it.
			printLoadBalancer(loadBalancer, "WARN")
			_, _ = fmt.Fprintf(out, "skipped (unknown age — malformed Created)\n")
		} else if loadBalancer.InstanceCount < 0 {
			// instance count unknown (health check failed) — skip to be safe
			printLoadBalancer(loadBalancer, " N/A")
			_, _ = fmt.Fprintf(out, "skipped (instance count unknown)\n")
		} else if loadBalancer.InstanceCount > 0 {
			// skip LBs that still have servers attached
			printLoadBalancer(loadBalancer, "LIVE")
			_, _ = fmt.Fprintf(out, "skipped (has %d instances)\n", loadBalancer.InstanceCount)
		} else if loadBalancer.Age < minAge {
			// skip recently created LBs that may not have instances yet
			printLoadBalancer(loadBalancer, " NEW")
			_, _ = fmt.Fprintf(out, "skipped (less than 1 hour old)\n")
		} else {
			// no instances and older than 1 hour — delete it
			printLoadBalancer(loadBalancer, "DEAD")
			if flagMock {
				_, _ = fmt.Fprintf(out, "Mock deleted!\n")
			} else {
				deleteLoadBalancer(ctx, loadBalancer)
			}
		}
	}
}

func printServer(server core.Server, state string) {
	ageString := fmt.Sprintf("%.2f days old", server.Age)
	prettyPrint(fmt.Sprintf("[%s] [%s] [%s] [%s] ▶ ", ageString, server.Region, state, server.Name), flagMock)
}

func printLoadBalancer(loadBalancer core.LoadBalancer, state string) {
	ageString := fmt.Sprintf("%.2f days old", loadBalancer.Age)
	prettyPrint(fmt.Sprintf("[%s] [%s] [%s] [%s] [%3d instances] [%s] ▶ ", ageString, loadBalancer.Region, state, loadBalancer.Type, loadBalancer.InstanceCount, loadBalancer.Name), flagMock)
}

func deleteLoadBalancer(ctx context.Context, loadBalancer core.LoadBalancer) {
	executor := ctx.Value(core.ExecutorKey).(core.ExecutorInterface)
	err := executor.LoadBalancerDelete(ctx, loadBalancer)
	if err != nil {
		_, _ = fmt.Fprintf(out, "ERROR: %s\n", err.Error())
	} else {
		_, _ = fmt.Fprintf(out, "Deleted!\n")
	}
}

func deleteSshKey(ctx context.Context, sshKey core.SshKey) {
	executor := ctx.Value(core.ExecutorKey).(core.ExecutorInterface)
	err := executor.SshKeyDelete(ctx, sshKey)
	if err != nil {
		_, _ = fmt.Fprintf(out, "ERROR: %s\n", err.Error())
	} else {
		_, _ = fmt.Fprintf(out, "Deleted!\n")
	}
}

func deleteVolumes(ctx context.Context, volumes []core.Volume) {
	// minimum age threshold: 1 hour (in days)
	minAge := 1.0 / 24.0

	for _, volume := range volumes {
		printVolume(volume)
		if isPermanent(volume.Name, volume.Tags) {
			_, _ = fmt.Fprintf(out, "skipped (permanent)\n")
		} else if hasSampleTag(volume.Tags) {
			// sample-stack volumes must be spared along with their owning
			// servers (panel finding A#8).
			_, _ = fmt.Fprintf(out, "skipped (sample tag)\n")
		} else if volume.Age <= 0 {
			// defense-in-depth: malformed/missing Created → skip.
			_, _ = fmt.Fprintf(out, "skipped (unknown age — malformed Created)\n")
		} else if volume.Attached {
			// skip volumes that are attached to an instance
			_, _ = fmt.Fprintf(out, "skipped (attached to instance)\n")
		} else if volume.Age < minAge {
			// skip recently created volumes that may not have been attached yet
			_, _ = fmt.Fprintf(out, "skipped (too new)\n")
		} else {
			if flagMock {
				_, _ = fmt.Fprintf(out, "Mock deleted!\n")
			} else {
				deleteVolume(ctx, volume)
			}
		}
	}
}

func deleteVolume(ctx context.Context, volume core.Volume) {
	executor := ctx.Value(core.ExecutorKey).(core.ExecutorInterface)
	err := executor.VolumeDelete(ctx, volume)
	if err != nil {
		_, _ = fmt.Fprintf(out, "ERROR: %s\n", err.Error())
	} else {
		_, _ = fmt.Fprintf(out, "Deleted!\n")
	}
}

func printVolume(volume core.Volume) {
	ageString := fmt.Sprintf("%.2f days old", volume.Age)
	prettyPrint(fmt.Sprintf("[%s] [%s] [%s] ▶ ", ageString, volume.Region, volume.Name), flagMock)
}

func deleteSshKeys(ctx context.Context, sshKeys []core.SshKey) {
	// IMPORTANT: This implementation assumes that sorting by VendorID is equivalent to sorting by the creation date (some clouds don't return `created_at` for SSH keys)
	// Since there is no `created_at` field, keep last `flagSshKeysKeepCount` to avoid deleting an SSH key before it is used

	nonUserDefinedSshKeyCount := 0
	for _, sshKey := range sshKeys {
		if strings.HasPrefix(sshKey.Name, "c66-") {
			nonUserDefinedSshKeyCount += 1
		}
	}

	deletedSshKeys := 0
	for _, sshKey := range sshKeys {
		prettyPrint(fmt.Sprintf("[%s] [%s] ▶ ", sshKey.VendorID, sshKey.Name), flagMock)
		if strings.HasPrefix(sshKey.Name, "c66-") {
			if (nonUserDefinedSshKeyCount - flagSshKeysKeepCount) > deletedSshKeys {
				deletedSshKeys += 1
				if flagMock {
					_, _ = fmt.Fprintf(out, "Mock deleted!\n")
				} else {
					deleteSshKey(ctx, sshKey)
				}
			} else {
				_, _ = fmt.Fprintf(out, "skipped (keep last %d)\n", flagSshKeysKeepCount)
			}
		} else {
			_, _ = fmt.Fprintf(out, "skipped (name)\n")
		}
	}
}
