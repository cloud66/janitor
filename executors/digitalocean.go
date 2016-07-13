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
}

//ListServers returns collection of Server objects
func (d DigitalOcean) ListServers(ctx context.Context) ([]core.Server, error) {
	droplets, _, err := d.client(ctx).Droplets.List(&godo.ListOptions{})
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
			result = append(result, core.Server{VendorID: strconv.Itoa(droplet.ID), Name: droplet.Name, Age: age, Region: "Global"})
		}
	}

	return result, nil
}

// DeleteServer remove the specified server
func (d DigitalOcean) DeleteServer(ctx context.Context, server core.Server) error {
	id, _ := strconv.Atoi(server.VendorID)
	_, err := d.client(ctx).Droplets.Delete(id)
	if err != nil {
		return err
	}
	return nil
}

// Token retrieves the oauth token
func (t *TokenSource) Token() (*oauth2.Token, error) {
	token := &oauth2.Token{
		AccessToken: t.AccessToken,
	}
	return token, nil
}

func (d *DigitalOcean) client(context context.Context) *godo.Client {
	pat := context.Value("DO_PAT").(string)
	tokenSource := &TokenSource{
		AccessToken: pat,
	}
	oauthClient := oauth2.NewClient(oauth2.NoContext, tokenSource)
	return godo.NewClient(oauthClient)
}
