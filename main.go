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
	flagDOPat    string
	flagExcludes string
	flagIncludes string
	flagCloud    string
	flagPrompt   bool
	flagMock     bool
	flagMaxAge   int
)

func main() {
	flag.StringVar(&flagDOPat, "do-pat", os.Getenv("DO_PAT"), "DigitalOcean Personal Access Token")
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

	if _, ok := clouds[flagCloud]; !ok {
		fmt.Printf("Unsupported cloud %s\n", flagCloud)
		os.Exit(1)
	}

	ctx := context.Background()
	ctx = context.WithValue(ctx, "PAT", flagDOPat)
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
		if includes.MatchString(server.Name) {
			if !excludes.MatchString(server.Name) {
				if server.Age > float64(flagMaxAge) {
					if flagMock {
						fmt.Printf("%s (%f days old) ▶ Deleted! (MOCK)\n", server.Name, server.Age)
					} else {
						fmt.Printf("%s (%f days old) ▶ ", server.Name, server.Age)
						err := executor.DeleteServer(ctx, server)
						if err != nil {
							fmt.Printf("ERROR: %s\n", err.Error())
						} else {
							fmt.Printf("Deleted!\n")
						}
					}
				}
			}
		}
	}
}
