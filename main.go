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

	//credentials
	flagDOPat              string
	flagAWSAccessKeyID     string
	flagAWSSecretAccessKey string
	flagVultrPat        string
	flagHetznerPat    string
)

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
	// surface executor warnings and normal output through the same `out` sink
	// so tests and users see "[WARN] ..." lines alongside mock markers.
	ctx = context.WithValue(ctx, core.WarnWriterKey, out)
	ctx = context.WithValue(ctx, core.OutWriterKey, out)

	if flagAction == actionDelete {
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
			fmt.Printf("Unsupported cloud %s\n", flagClouds)
			continue
		}

		executor := clouds[userCloud]
		ctx = context.WithValue(ctx, core.ExecutorKey, executor)

		servers, err := executor.ServersGet(ctx, nil, nil)
		if err != nil {
			fmt.Printf("Cannot get servers due to %s\n", err.Error())
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

// isPermanent checks if a resource name or any of its tags contain the
// permanent marker (case-insensitive). marker string centralised in core.
func isPermanent(name string, tags []string) bool {
	if strings.Contains(strings.ToLower(name), core.TagPermanent) {
		return true
	}
	for _, tag := range tags {
		if strings.Contains(strings.ToLower(tag), core.TagPermanent) {
			return true
		}
	}
	return false
}

// hasLongName checks if a resource name or any of its tags contain the long
// marker (case-insensitive). marker string centralised in core.
func hasLongName(name string, tags []string) bool {
	if strings.Contains(strings.ToLower(name), core.TagLong) {
		return true
	}
	for _, tag := range tags {
		if strings.Contains(strings.ToLower(tag), core.TagLong) {
			return true
		}
	}
	return false
}

// hasSampleTag checks if any tag has key core.TagKeyC66Stack with a value
// containing the sample marker. vultr tags are flat strings in key=value
// format (e.g. "C66-STACK=maestro-sample-prd").
func hasSampleTag(tags []string) bool {
	// lower-cased key we compare against; computed once.
	wantKey := strings.ToLower(core.TagKeyC66Stack)
	for _, tag := range tags {
		// split on the first "=" to isolate key from value
		i := strings.IndexByte(tag, '=')
		if i < 0 {
			// no "=" → not a key=value tag, skip
			continue
		}
		// trim whitespace on the key and lower-case for case-insensitive compare
		key := strings.TrimSpace(strings.ToLower(tag[:i]))
		if key != wantKey {
			continue
		}
		// scan ONLY the value portion for the sample marker (case insensitive)
		val := strings.ToLower(tag[i+1:])
		if strings.Contains(val, core.TagSample) {
			return true
		}
	}
	return false
}

func deleteServers(ctx context.Context, cloud string, servers []core.Server) {
	for _, server := range servers {
		if cloud == "vultr" && hasSampleTag(server.Tags) {
			// skip vultr servers with a C66-STACK tag containing "sample"
			printServer(server, "SMPL")
			_, _ = fmt.Fprintf(out, "skipped (sample tag)\n")
		} else if isPermanent(server.Name, server.Tags) {
			printServer(server, "PERM")
			_, _ = fmt.Fprintf(out, "skipped (permanent)\n")
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
