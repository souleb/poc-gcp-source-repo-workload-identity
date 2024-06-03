package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/fluxcd/pkg/git"
	"github.com/fluxcd/pkg/git/gogit"
	"github.com/fluxcd/pkg/git/repository"
	"github.com/fluxcd/pkg/oci"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type gceToken struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type"`
}

// GCP_TOKEN_URL is the default GCP metadata endpoint used for authentication.
const GCP_TOKEN_URL = "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token"

// ValidHost returns if a given host is a valid GCR host.
func ValidHost(host string) bool {
	return host == "gcr.io" || strings.HasSuffix(host, ".gcr.io") || strings.HasSuffix(host, "-docker.pkg.dev")
}

// Client is a GCP GCR client which can log into the registry and return
// authorization information.
type Client struct {
	tokenURL string
}

// NewClient creates a new GCR client with default configurations.
func NewClient() *Client {
	return &Client{tokenURL: GCP_TOKEN_URL}
}

// WithTokenURL sets the token URL used by the GCR client.
func (c *Client) WithTokenURL(url string) *Client {
	c.tokenURL = url
	return c
}

// getLoginAuth obtains authentication by getting a token from the metadata API
// on GCP. This assumes that the pod has right to pull the image which would be
// the case if it is hosted on GCP. It works with both service account and
// workload identity enabled clusters.
func (c *Client) getLoginAuth(ctx context.Context) (authn.AuthConfig, error) {
	var authConfig authn.AuthConfig

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.tokenURL, nil)
	if err != nil {
		return authConfig, err
	}

	request.Header.Add("Metadata-Flavor", "Google")

	client := &http.Client{}
	response, err := client.Do(request)
	if err != nil {
		return authConfig, err
	}
	defer response.Body.Close()
	defer io.Copy(io.Discard, response.Body)

	if response.StatusCode != http.StatusOK {
		return authConfig, fmt.Errorf("unexpected status from metadata service: %s", response.Status)
	}

	var accessToken gceToken
	decoder := json.NewDecoder(response.Body)
	if err := decoder.Decode(&accessToken); err != nil {
		return authConfig, err
	}

	authConfig = authn.AuthConfig{
		Username: "oauth2accesstoken",
		Password: accessToken.AccessToken,
	}
	return authConfig, nil
}

// Login attempts to get the authentication material for GCR. The caller can
// ensure that the passed image is a valid GCR image using ValidHost().
func (c *Client) Login(ctx context.Context, autoLogin bool, image string, ref name.Reference) (authn.Authenticator, error) {
	if autoLogin {
		log.FromContext(ctx).Info("logging in to GCP GCR for " + image)
		authConfig, err := c.getLoginAuth(ctx)
		if err != nil {
			log.FromContext(ctx).Info("error logging into GCP " + err.Error())
			return nil, err
		}

		auth := authn.FromConfig(authConfig)
		return auth, nil
	}
	return nil, fmt.Errorf("GCR authentication failed: %w", oci.ErrUnconfiguredProvider)
}

func getAuthOpts(ctx context.Context, u url.URL) (*git.AuthOptions, error) {
	login := NewClient()
	auth, err := login.Login(ctx, true, u.String(), nil)
	if err != nil {
		return nil, err
	}

	config, err := auth.Authorization()
	if err != nil {
		return nil, err
	}

	authData := map[string][]byte{
		"username": []byte(config.Username),
		"password": []byte(config.Password),
	}

	// Configure authentication strategy to access the source
	authOpts, err := git.NewAuthOptions(u, authData)
	if err != nil {
		return nil, err
	}
	return authOpts, nil
}

func gitCheckout(ctx context.Context, url string, authOpts *git.AuthOptions, dir string) (*git.Commit, error) {
	// Configure checkout strategy.
	cloneOpts := repository.CloneConfig{
		ShallowClone: true,
	}

	gitCtx, cancel := context.WithTimeout(ctx, time.Second*180)
	defer cancel()

	clientOpts := []gogit.ClientOption{gogit.WithDiskStorage()}
	if authOpts.Transport == git.HTTP {
		clientOpts = append(clientOpts, gogit.WithInsecureCredentialsOverHTTP())
	}

	gitReader, err := gogit.NewClient(dir, authOpts, clientOpts...)
	if err != nil {
		return nil, err
	}
	defer gitReader.Close()

	commit, err := gitReader.Clone(gitCtx, url, cloneOpts)
	if err != nil {
		return nil, err
	}

	return commit, nil
}

func main() {
	// ref is the git cloud source repository url
	ref := "https://source.developers.google.com/p/<project-id>/r/<repo-name>"
	tmpDir, err := tempDirForObj("")
	if err != nil {
		fmt.Println("error creating temp dir: ", err)
		return
	}
	ctx := context.Background()

	u, err := url.Parse(ref)
	if err != nil {
		fmt.Println("error parsing url: ", err)
		return
	}

	auth, err := getAuthOpts(ctx, *u)
	if err != nil {
		fmt.Println("error getting auth options: ", err)
		return
	}

	commit, err := gitCheckout(ctx, ref, auth, tmpDir)
	if err != nil {
		fmt.Println("error checking out git: ", err)
		return
	}

	fmt.Println("commit: ", commit)
	fmt.Println("checkout complete")
}

func tempDirForObj(dir string) (string, error) {
	return os.MkdirTemp(dir, "test-gcp-gitrepo-")
}
