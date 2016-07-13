package main

import (
	"flag"
	"fmt"
	"os"
	"regexp"
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
	flagCloud    string
	flagPrompt   bool
	flagMock     bool
	flagMaxAge   int
	// credentials
	flagDOPat              string
	flagAWSAccessKeyID     string
	flagAWSSecretAccessKey string
)

func main() {
	// credentials
	flag.StringVar(&flagDOPat, "do-pat", os.Getenv("DO_PAT"), "DigitalOcean Personal Access Token")
	flag.StringVar(&flagAWSAccessKeyID, "aws-access-key-id", os.Getenv("AWS_ACCESS_KEY_ID"), "AWS Access Key ID")
	flag.StringVar(&flagAWSSecretAccessKey, "aws-secret-access-key", os.Getenv("AWS_SECRET_ACCESS_KEY"), "AWS Secret Access Key")
	// config
	flag.StringVar(&flagExcludes, "excludes", "^(PERMANENT|DND).*", "Regexp to exclude servers to delete by name")
	flag.StringVar(&flagIncludes, "includes", "", "Regexp to include servers to delete by name")
	flag.StringVar(&flagCloud, "cloud", "", "Cloud to work on")

	var mock bool
	if strings.ToLower(os.Getenv("MOCK")) == "true" {
		mock = false
	} else {
		mock = true
	}
	flag.BoolVar(&flagMock, "mock", mock, "Don't actually delete anything, just show what *would* happen")

	var maxAge int
	if os.Getenv("MAX_AGE") != "" {
		maxAge, _ = strconv.Atoi(os.Getenv("MAX_AGE"))
	} else {
		maxAge = 3
	}
	flag.IntVar(&flagMaxAge, "max-age", maxAge, "Maximum allowed server age (days). Anything older will be deleted!")
	flag.Parse()

	if flagCloud == "" {
		fmt.Println("No cloud provider is specified. Use the --cloud option")
		os.Exit(1)
	}

	clouds = make(map[string]core.Executor)
	// Just add new clouds here
	clouds["digitalocean"] = executors.DigitalOcean{}
	clouds["aws"] = executors.Aws{}

	if _, ok := clouds[flagCloud]; !ok {
		fmt.Printf("Unsupported cloud %s\n", flagCloud)
		os.Exit(1)
	}

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

	executor := clouds[flagCloud]

	// List all servers
	servers, err := executor.ListServers(ctx)
	if err != nil {
		fmt.Printf("Cannot list servers due to %s\n", err.Error())
		os.Exit(1)
	}

	// Check them for exclude and includes
	for _, server := range servers {
		if server.Name == "VICTEST" {
			fmt.Printf("%s (%f days old) ▶  ", server.Name, server.Age)
			err := executor.DeleteServer(ctx, server)
			if err != nil {
				fmt.Printf("ERROR: %s\n", err.Error())
			} else {
				fmt.Printf("Deleted!\n")
			}
		}

		if includes.MatchString(server.Name) {
			if !excludes.MatchString(server.Name) {
				if server.Age > float64(flagMaxAge) {
					if flagMock {
						fmt.Printf("[MOCK] %s (%f days old) ▶  Deleted!\n", server.Name, server.Age)
					} else {
						fmt.Printf("%s (%f days old) ▶  ", server.Name, server.Age)
						err := executor.DeleteServer(ctx, server)
						if err != nil {
							fmt.Printf("ERROR: %s\n", err.Error())
						} else {
							fmt.Printf("Deleted!\n")
						}
					}
				} else if flagMock {
					fmt.Printf("[MOCK] %s (%f days old) ▶  Skipped due to age\n", server.Name, server.Age)
				}
			} else if flagMock {
				fmt.Printf("[MOCK] %s (%f days old) ▶  Skipped due to excludes\n", server.Name, server.Age)
			}
		} else if flagMock {
			fmt.Printf("[MOCK] %s (%f days old) ▶  Skipped due to includes\n", server.Name, server.Age)
		}
	}
}
