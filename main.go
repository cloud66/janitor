package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/cloud66/janitor/core"
	"github.com/cloud66/janitor/executors"
	"golang.org/x/net/context"
)

const (
	actionWebServer = "webserver"
	actionDelete    = "delete"
	actionStop      = "stop"
	actionStart     = "start"

	//Defaults
	defaultSshKeyKeepCount = 10
)

var (
	clouds     map[string]core.ExecutorInterface
	flagAction string

	flagShortMatch       string
	flagLongMatch        string
	flagMaxAgeShort      float64
	flagMaxAgeNormal     float64
	flagMaxAgeLong       float64
	flagSshKeysKeepCount int

	flagClouds string
	flagPrompt bool
	flagMock   bool

	//credentials
	flagDOPat              string
	flagAWSAccessKeyID     string
	flagAWSSecretAccessKey string
)

func prettyPrint(message string, mock bool) {
	if mock == true {
		fmt.Printf("[MOCK] %s", message)
	} else {
		fmt.Printf("%s", message)
	}
}

func handler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, "Its a TRAP!")
}

func main() {
	//action
	flag.StringVar(&flagAction, "action", "", "Action to perform: delete|stop|start")
	//credentials
	flag.StringVar(&flagDOPat, "do-pat", os.Getenv("JANITOR_DO_PAT"), "DigitalOcean Personal Access Token")
	flag.StringVar(&flagAWSAccessKeyID, "aws-access-key-id", os.Getenv("JANITOR_AWS_ACCESS_KEY_ID"), "AWS Access Key ID")
	flag.StringVar(&flagAWSSecretAccessKey, "aws-secret-access-key", os.Getenv("JANITOR_AWS_SECRET_ACCESS_KEY"), "AWS Secret Access Key")
	//config
	flag.BoolVar(&flagMock, "mock", strings.ToLower(os.Getenv("MOCK")) != "false", "Don't actually delete anything, just show what *would* happen")
	flag.StringVar(&flagClouds, "clouds", "", "Clouds to work on (comma separated for multiple)")
	flag.StringVar(&flagLongMatch, "match-long", "([-_ ]|^)(LONG|long|PERM|perm|DND|dnd)([-_ ]|$)", "Regexp for long term servers to delete by name")
	flag.StringVar(&flagShortMatch, "match-short", "([-_ ]|^)(SHORT|short|TMP|tmp|TEMP|temp)([-_ ]|$)", "Regexp for short term servers to delete by name")

	var maxAgeShort, maxAgeNormal, maxAgeLong float64
	var sshKeysKeepCount int
	if os.Getenv("MAX_AGE_SHORT") != "" {
		maxAgeShort, _ = strconv.ParseFloat(os.Getenv("MAX_AGE_SHORT"), 64)
	} else {
		maxAgeShort = 0.75
	}
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
	flag.Float64Var(&flagMaxAgeShort, "max-age-short", maxAgeShort, "Short allowed server age (days). Decimal allowed. Anything older will be deleted!")
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

	ctx := context.Background()
	ctx = context.WithValue(ctx, "JANITOR_DO_PAT", flagDOPat)
	ctx = context.WithValue(ctx, "JANITOR_AWS_ACCESS_KEY_ID", flagAWSAccessKeyID)
	ctx = context.WithValue(ctx, "JANITOR_AWS_SECRET_ACCESS_KEY", flagAWSSecretAccessKey)

	var shortRegex, longRegex *regexp.Regexp
	if flagShortMatch != "" {
		shortRegex, _ = regexp.Compile(flagShortMatch)
	} else {
		shortRegex, _ = regexp.Compile("")
	}
	if flagLongMatch != "" {
		longRegex, _ = regexp.Compile(flagLongMatch)
	} else {
		longRegex, _ = regexp.Compile("")
	}

	ctx = context.WithValue(ctx, "shortRegex", shortRegex)
	ctx = context.WithValue(ctx, "longRegex", longRegex)

	if flagAction == actionDelete {
		prettyPrint(fmt.Sprintf("[%s ACTION]\n", strings.ToUpper(flagAction)), flagMock)
		prettyPrint(fmt.Sprintf("NORMAL ALLOWANCE: %.3f days (%.0f hours)\n", flagMaxAgeNormal, flagMaxAgeNormal*24.0), flagMock)
		prettyPrint(fmt.Sprintf("SHORT ALLOWANCE: %.3f days (%.0f hours)\n", flagMaxAgeShort, flagMaxAgeShort*24.0), flagMock)
		prettyPrint(fmt.Sprintf("LONG ALLOWANCE: %.3f days (%.0f hours)\n", flagMaxAgeLong, flagMaxAgeLong*24.0), flagMock)

	} else if flagAction == actionStop {
		prettyPrint(fmt.Sprintf("%s ACTION\n", strings.ToUpper(flagAction)), flagMock)
	} else if flagAction == actionStart {
		prettyPrint(fmt.Sprintf("%s ACTION\n", strings.ToUpper(flagAction)), flagMock)
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
		ctx = context.WithValue(ctx, "executor", executor)

		servers, err := executor.ServersGet(ctx, nil, nil)
		if err != nil {
			fmt.Printf("Cannot get servers due to %s\n", err.Error())
		} else {
			prettyPrint(fmt.Sprintf("[%d SERVERS]\n", len(servers)), flagMock)
			sort.Sort(core.ServerSorter(servers))
			if flagAction == actionDelete {
				deleteServers(ctx, longRegex, shortRegex, servers)
			} else if flagAction == actionStop {
				stopServers(ctx, longRegex, shortRegex, servers)
			} else if flagAction == actionStart {
				startServers(ctx, longRegex, shortRegex, servers)
			}
		}

		if flagAction == actionDelete {
			loadBalancers, err := executor.LoadBalancersGet(ctx)
			if err != nil {
				if err.Error() != "Action not available" {
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
				if err.Error() != "Action not available" {
					fmt.Printf("Cannot get SSH keys due to %s\n", err.Error())
				}
			} else {
				prettyPrint(fmt.Sprintf("[%d SSH KEYS]\n", len(sshKeys)), flagMock)
				sort.Sort(core.SshKeySorter(sshKeys))
				deleteSshKeys(ctx, sshKeys)
			}
		}
	}
}

func deleteServers(ctx context.Context, longRegex *regexp.Regexp, shortRegex *regexp.Regexp, servers []core.Server) {
	for _, server := range servers {
		if longRegex.MatchString(server.Name) {
			prettyPrint(fmt.Sprintf("[%.2f days old] [%s] [%s] [%s] ▶  ", server.Age, server.Region, "LONG", server.Name), flagMock)
			if server.Age > float64(flagMaxAgeLong) {
				if flagMock {
					fmt.Printf("Mock deleted!\n")
				} else {
					deleteServer(ctx, server)
				}
			} else {
				fmt.Printf("skipped (age)\n")
			}
		} else if shortRegex.MatchString(server.Name) {
			prettyPrint(fmt.Sprintf("[%.2f days old] [%s] [%s] [%s] ▶  ", server.Age, server.Region, "SHORT", server.Name), flagMock)
			if server.Age > float64(flagMaxAgeShort) {
				if flagMock {
					fmt.Printf("Mock deleted!\n")
				} else {
					deleteServer(ctx, server)
				}
			} else {
				fmt.Printf("skipped (age)\n")
			}
		} else {
			prettyPrint(fmt.Sprintf("[%.2f days old] [%s] [%s] [%s] ▶  ", server.Age, server.Region, "NORMAL", server.Name), flagMock)
			if server.Age > float64(flagMaxAgeNormal) {
				if flagMock {
					fmt.Printf("Mock deleted!\n")
				} else {
					deleteServer(ctx, server)
				}
			} else {
				fmt.Printf("skipped (age)\n")
			}
		}
	}
}

func stopServers(ctx context.Context, longRegex *regexp.Regexp, shortRegex *regexp.Regexp, servers []core.Server) {
	for _, server := range servers {
		if longRegex.MatchString(server.Name) {
			prettyPrint(fmt.Sprintf("[%.2f days old] [%s] [%s] [%s] ▶  ", server.Age, server.Region, "LONG", server.Name), flagMock)
			if flagMock {
				fmt.Printf("Mock stopped!\n")
			} else {
				stopServer(ctx, server)
			}
		} else if shortRegex.MatchString(server.Name) {
			prettyPrint(fmt.Sprintf("[%.2f days old] [%s] [%s] [%s] ▶  ", server.Age, server.Region, "SHORT", server.Name), flagMock)
			if flagMock {
				fmt.Printf("Mock stopped!\n")
			} else {
				stopServer(ctx, server)
			}
		} else {
			prettyPrint(fmt.Sprintf("[%.2f days old] [%s] [%s] [%s] ▶  ", server.Age, server.Region, "NORMAL", server.Name), flagMock)
			if flagMock {
				fmt.Printf("Mock stopped!\n")
			} else {
				stopServer(ctx, server)
			}
		}
	}
}

func startServers(ctx context.Context, longRegex *regexp.Regexp, shortRegex *regexp.Regexp, servers []core.Server) {
	for _, server := range servers {
		if longRegex.MatchString(server.Name) {
			prettyPrint(fmt.Sprintf("[%.2f days old] [%s] [%s] [%s] ▶  ", server.Age, server.Region, "LONG", server.Name), flagMock)
			if flagMock {
				fmt.Printf("Mock started!\n")
			} else {
				startServer(ctx, server)
			}
		} else if shortRegex.MatchString(server.Name) {
			prettyPrint(fmt.Sprintf("[%.2f days old] [%s] [%s] [%s] ▶  ", server.Age, server.Region, "SHORT", server.Name), flagMock)
			if flagMock {
				fmt.Printf("Mock started!\n")
			} else {
				startServer(ctx, server)
			}
		} else {
			prettyPrint(fmt.Sprintf("[%.2f days old] [%s] [%s] [%s] ▶  ", server.Age, server.Region, "NORMAL", server.Name), flagMock)
			if flagMock {
				fmt.Printf("Mock started!\n")
			} else {
				startServer(ctx, server)
			}
		}
	}
}

func deleteServer(ctx context.Context, server core.Server) {
	executor := ctx.Value("executor").(core.ExecutorInterface)
	err := executor.ServerDelete(ctx, server)
	if err != nil {
		fmt.Printf("ERROR: %s\n", err.Error())
	} else {
		fmt.Printf("Deleted!\n")
	}
}

func stopServer(ctx context.Context, server core.Server) {
	executor := ctx.Value("executor").(core.ExecutorInterface)
	err := executor.ServerStop(ctx, server)
	if err != nil {
		fmt.Printf("ERROR: %s\n", err.Error())
	} else {
		fmt.Printf("Stopped!\n")
	}
}

func startServer(ctx context.Context, server core.Server) {
	executor := ctx.Value("executor").(core.ExecutorInterface)
	err := executor.ServerStart(ctx, server)
	if err != nil {
		fmt.Printf("ERROR: %s\n", err.Error())
	} else {
		fmt.Printf("Started!\n")
	}
}

func deleteLoadBalancers(ctx context.Context, loadBalancers []core.LoadBalancer) {
	for _, loadBalancer := range loadBalancers {
		prettyPrint(fmt.Sprintf("[%.2f days old] [%s] [%d instance attached] [%s] ▶  ", loadBalancer.Age, loadBalancer.Region, loadBalancer.InstanceCount, loadBalancer.Name), flagMock)
		// any loadbalancer older than 30 mins
		if loadBalancer.Age < float64(0.02) {
			fmt.Printf("skipped (age)\n")
		} else if loadBalancer.InstanceCount > 0 {
			fmt.Printf("skipped (instances)\n")
		} else {
			if flagMock {
				fmt.Printf("Mock deleted!\n")
			} else {
				deleteLoadBalancer(ctx, loadBalancer)
			}
		}
	}
}

func deleteLoadBalancer(ctx context.Context, loadBalancer core.LoadBalancer) {
	executor := ctx.Value("executor").(core.ExecutorInterface)
	err := executor.LoadBalancerDelete(ctx, loadBalancer)
	if err != nil {
		fmt.Printf("ERROR: %s\n", err.Error())
	} else {
		fmt.Printf("Deleted!\n")
	}
}

func deleteSshKey(ctx context.Context, sshKey core.SshKey) {
	executor := ctx.Value("executor").(core.ExecutorInterface)
	err := executor.SshKeyDelete(ctx, sshKey)
	if err != nil {
		fmt.Printf("ERROR: %s\n", err.Error())
	} else {
		fmt.Printf("Deleted!\n")
	}
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
		prettyPrint(fmt.Sprintf("[%s] [%s] ▶  ", sshKey.VendorID, sshKey.Name), flagMock)
		if strings.HasPrefix(sshKey.Name, "c66-") {
			if (nonUserDefinedSshKeyCount - flagSshKeysKeepCount) > deletedSshKeys {
				deletedSshKeys += 1
				if flagMock {
					fmt.Printf("Mock deleted!\n")
				} else {
					deleteSshKey(ctx, sshKey)
				}
			} else {
				fmt.Printf(fmt.Sprintf("skipped (keep last %d)\n", flagSshKeysKeepCount))
			}
		} else {
			fmt.Printf("skipped (name)\n")
		}
	}
}
