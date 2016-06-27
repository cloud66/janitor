package main

import (
	"flag"
	"fmt"
	"os"
	"regexp"

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
)

func main() {
	flag.StringVar(&flagDOPat, "do-pat", "", "DigitalOcean Personal Access Token")
	flag.StringVar(&flagExcludes, "excludes", "^(PERMANENT|DND).*", "Regexp to exclude servers to delete by name")
	flag.StringVar(&flagIncludes, "includes", "", "Regexp to include servers to delete by name")
	flag.StringVar(&flagCloud, "cloud", "", "Cloud to work on")

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
				fmt.Printf("Found server %s\n", server.Name)
			}
		}

		// TODO Delete
	}
}
