package main

import (
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/cloud66/janitor/core"
	"github.com/cloud66/janitor/executors"
	"golang.org/x/net/context"
)

var (
	clouds       map[string]core.Executor
	flagExcludes string
	flagIncludes string
	flagClouds   string
	flagPrompt   bool
	flagMock     bool
	flagMaxAge   int
	// credentials
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

func main() {
	// credentials
	flag.StringVar(&flagDOPat, "do-pat", os.Getenv("DO_PAT"), "DigitalOcean Personal Access Token")
	flag.StringVar(&flagAWSAccessKeyID, "aws-access-key-id", os.Getenv("AWS_ACCESS_KEY_ID"), "AWS Access Key ID")
	flag.StringVar(&flagAWSSecretAccessKey, "aws-secret-access-key", os.Getenv("AWS_SECRET_ACCESS_KEY"), "AWS Secret Access Key")
	// config
	flag.StringVar(&flagExcludes, "excludes", "^(PERMANENT|DND).*", "Regexp to exclude servers to delete by name")
	flag.StringVar(&flagIncludes, "includes", "", "Regexp to include servers to delete by name")
	flag.StringVar(&flagClouds, "clouds", "", "Clouds to work on (comma separated for multiple)")

	mock := strings.ToLower(os.Getenv("MOCK")) != "false"
	flag.BoolVar(&flagMock, "mock", mock, "Don't actually delete anything, just show what *would* happen")

	var maxAge int
	if os.Getenv("MAX_AGE") != "" {
		maxAge, _ = strconv.Atoi(os.Getenv("MAX_AGE"))
	} else {
		maxAge = 3
	}
	flag.IntVar(&flagMaxAge, "max-age", maxAge, "Maximum allowed server age (days). Anything older will be deleted!")
	flag.Parse()

	if flagClouds == "" {
		fmt.Println("No cloud provider is specified. Use the --cloud option")
		os.Exit(1)
	}

	clouds = make(map[string]core.Executor)
	// Just add new clouds here
	clouds["digitalocean"] = executors.DigitalOcean{}
	clouds["aws"] = executors.Aws{}

	ctx := context.Background()
	ctx = context.WithValue(ctx, "DO_PAT", flagDOPat)
	ctx = context.WithValue(ctx, "AWS_ACCESS_KEY_ID", flagAWSAccessKeyID)
	ctx = context.WithValue(ctx, "AWS_SECRET_ACCESS_KEY", flagAWSSecretAccessKey)
	var includes, excludes *regexp.Regexp
	if flagIncludes != "" {
		includes, _ = regexp.Compile(flagIncludes)
	} else {
		includes, _ = regexp.Compile(".*")
	}

	if flagExcludes != "" {
		excludes, _ = regexp.Compile(flagExcludes)
	} else {
		excludes, _ = regexp.Compile("")
	}
	ctx = context.WithValue(ctx, "excludes", excludes)
	ctx = context.WithValue(ctx, "includes", includes)

	userClouds := strings.Split(flagClouds, ",")
	for _, userCloud := range userClouds {
		// Output the cloud
		prettyPrint(fmt.Sprintf("[[%s CLOUD]]\n", strings.ToUpper(userCloud)), flagMock)

		if _, ok := clouds[userCloud]; !ok {
			fmt.Printf("Unsupported cloud %s\n", flagClouds)
			continue
		}

		executor := clouds[userCloud]
		// List all servers
		servers, err := executor.ListServers(ctx)
		if err != nil {
			fmt.Printf("Cannot list servers due to %s\n", err.Error())
			continue
		}

		// Check them for exclude and includes
		sort.Sort(core.ServerSorter(servers))
		for _, server := range servers {
			name := fmt.Sprintf("[%s] [%s]", server.Region, server.Name)

			if includes.MatchString(server.Name) {
				if !excludes.MatchString(server.Name) {
					if server.Age > float64(flagMaxAge) {
						if flagMock {
							fmt.Printf("[MOCK] [%.2f days old] %s ▶  Would be deleted!\n", server.Age, name)
						} else {
							fmt.Printf("[%.2f days old] %s ▶  ", server.Age, name)
							err := executor.DeleteServer(ctx, server)
							if err != nil {
								fmt.Printf("ERROR: %s\n", err.Error())
							} else {
								fmt.Printf("Deleted!\n")
							}
						}
					} else {
						prettyPrint(fmt.Sprintf("[%.2f days old] %s ▶  Skipped (due to age)\n", server.Age, name), flagMock)
					}
				} else {
					prettyPrint(fmt.Sprintf("[%.2f days old] %s ▶  Skipped (due to excludes)\n", server.Age, name), flagMock)
				}
			} else {
				prettyPrint(fmt.Sprintf("[%.2f days old] %s ▶  Skipped (due to includes)\n", server.Age, name), flagMock)
			}
		}
	}
}
