package executors

import (
	"fmt"
	"strconv"

	"github.com/cloud66/janitor/core"
	"github.com/digitalocean/godo"
	"golang.org/x/net/context"
	"golang.org/x/oauth2"
)

type TokenSource struct {
	AccessToken string
}

type DigitalOcean struct {
}

func (d DigitalOcean) ListServers(ctx context.Context) ([]core.Server, error) {
	droplets, _, err := d.client(ctx).Droplets.List(&godo.ListOptions{})
	if err != nil {
		return nil, err
	}

	result := make([]core.Server, 0, len(droplets))
	for _, droplet := range droplets {
		result = append(result, core.Server{VendorID: strconv.Itoa(droplet.ID), Name: droplet.Name})
	}

	return result, nil
}

func (d DigitalOcean) DeleteServer(ctx context.Context, server core.Server) error {
	fmt.Printf("Deleting %s...\n", server.Name)
	return nil
}

func (t *TokenSource) Token() (*oauth2.Token, error) {
	token := &oauth2.Token{
		AccessToken: t.AccessToken,
	}
	return token, nil
}

func (d *DigitalOcean) client(context context.Context) *godo.Client {
	pat := context.Value("PAT").(string)
	tokenSource := &TokenSource{
		AccessToken: pat,
	}
	oauthClient := oauth2.NewClient(oauth2.NoContext, tokenSource)
	return godo.NewClient(oauthClient)
}
