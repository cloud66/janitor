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
	flagCloud    string
)

func main() {
	flag.StringVar(&flagDOPat, "do-pat", "", "DigitalOcean Personal Access Token")
	flag.StringVar(&flagExcludes, "excludes", "^(PERMANENT|DND).*", "Regexp to exclude servers to delete by name")
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
	excludes, _ := regexp.Compile(flagExcludes)
	ctx = context.WithValue(ctx, "excludes", excludes)

	executor := clouds[flagCloud]

	// List all servers
	servers, err := executor.ListServers(ctx)
	if err != nil {
		fmt.Printf("Cannot list servers due to %s\n", err.Error())
		os.Exit(1)
	}

	// Check them for exclude and includes
	for _, server := range servers {
		if !excludes.MatchString(server.Name) {
			fmt.Printf("Found server %s\n", server.Name)
		}

		// TODO Delete
	}
}
