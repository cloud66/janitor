package executors

import (
	"strconv"
	"time"

	"github.com/cloud66/janitor/core"
	"github.com/digitalocean/godo"
	"golang.org/x/net/context"
	"golang.org/x/oauth2"
)

//TokenSource provides token transport for auth
type TokenSource struct {
	AccessToken string
}

//DigitalOcean encapsulates all DO cloud calls
type DigitalOcean struct {
	*core.Executor
}

//ServersGet returns collection of Server objects
func (d DigitalOcean) ServersGet(ctx context.Context, vendorIDs []string, regions []string) ([]core.Server, error) {
	droplets, _, err := d.client(ctx).Droplets.List(ctx, &godo.ListOptions{})
	if err != nil {
		return nil, err
	}

	result := make([]core.Server, 0, len(droplets))
	for _, droplet := range droplets {
		createdAt := droplet.Created
		if createdAt != "" {
			createdAtDate, err := time.Parse(time.RFC3339, createdAt)
			if err != nil {
				return nil, err
			}
			age := time.Now().Sub(createdAtDate).Hours() / 24.0
			result = append(result, core.Server{VendorID: strconv.Itoa(droplet.ID), Name: droplet.Name, Age: age, Region: "Global", State: "RUNNING"})
		}
	}

	return result, nil
}

//ServerDelete remove the specified server
func (d DigitalOcean) ServerDelete(ctx context.Context, server core.Server) error {
	id, _ := strconv.Atoi(server.VendorID)
	_, err := d.client(ctx).Droplets.Delete(ctx, id)
	if err != nil {
		return err
	}
	return nil
}

//SshKeysGet gets SSH keys
func (d DigitalOcean) SshKeysGet(ctx context.Context) ([]core.SshKey, error) {
	doAllSshKeys := []godo.Key{}
	opt := &godo.ListOptions{}
	for {
		doSshKeys, resp, err := d.client(ctx).Keys.List(ctx, opt)
		if err != nil {
			return nil, err
		}

		for _, doSshKey := range doSshKeys {
			doAllSshKeys = append(doAllSshKeys, doSshKey)
		}

		// If we are at the last page, break out the for loop
		if resp.Links == nil || resp.Links.IsLastPage() {
			break
		}

		page, err := resp.Links.CurrentPage()
		if err != nil {
			return nil, err
		}

		opt.Page = page + 1
	}

	result := make([]core.SshKey, 0, len(doAllSshKeys))
	for _, doSshKey := range doAllSshKeys {
		result = append(result, core.SshKey{VendorID: strconv.Itoa(doSshKey.ID), Name: doSshKey.Name})
	}

	return result, nil
}

//SshKeyDelete deletes an SSH key
func (d DigitalOcean) SshKeyDelete(ctx context.Context, sshKey core.SshKey) error {
	id, _ := strconv.Atoi(sshKey.VendorID)
	_, err := d.client(ctx).Keys.DeleteByID(ctx, id)
	if err != nil {
		return err
	}
	return nil
}

//Token retrieves the oauth token
func (t *TokenSource) Token() (*oauth2.Token, error) {
	token := &oauth2.Token{
		AccessToken: t.AccessToken,
	}
	return token, nil
}

func (d *DigitalOcean) client(context context.Context) *godo.Client {
	pat := context.Value("JANITOR_DO_PAT").(string)
	tokenSource := &TokenSource{
		AccessToken: pat,
	}
	oauthClient := oauth2.NewClient(oauth2.NoContext, tokenSource)
	return godo.NewClient(oauthClient)
}
