package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
)

const VERSION = "0.0.2"

var (
	showVersion             = flag.Bool("version", false, "print version string")
	httpAddr                = flag.String("http-address", "127.0.0.1:4180", "<addr>:<port> to listen on for HTTP clients")
	redirectUrl             = flag.String("redirect-url", "", "the OAuth Redirect URL. ie: \"https://internalapp.yourcompany.com/oauth2/callback\"")
	clientID                = flag.String("client-id", "", "the Oauth Client ID: ie: \"123456.apps.googleusercontent.com\"")
	clientSecret            = flag.String("client-secret", "", "the OAuth Client Secret")
	loginUrl                = flag.String("login-url", "", "the OAuth Login URL")
	redemptionUrl           = flag.String("redemption-url", "", "the OAuth code redemption URL")
	cookieSecret            = flag.String("cookie-secret", "", "the seed string for secure cookies")
	cookieDomain            = flag.String("cookie-domain", "", "an optional cookie domain to force cookies to")
	userVerificationCommand = flag.String("user-verification-command", "", "external command, takes the auth token as AUTH_TOKEN env variable, returns 0 if user should be logged in")
	oauthScope              = flag.String("oauth-scope", "", "the scope (or scopes) you are requesting from the oauth provider, separated by commas")
	upstreams               = StringArray{}
)

func init() {
	flag.Var(&upstreams, "upstream", "the http url(s) of the upstream endpoint. If multiple, routing is based on path")
}

func main() {

	flag.Parse()

	// Try to use env for secrets if no flag is set
	if *clientID == "" {
		*clientID = os.Getenv("CLIENT_ID")
	}
	if *clientSecret == "" {
		*clientSecret = os.Getenv("CLIENT_SECRET")
	}
	if *cookieSecret == "" {
		*cookieSecret = os.Getenv("COOKIE_SECRET")
	}

	if *showVersion {
		fmt.Printf("google_auth_proxy v%s\n", VERSION)
		return
	}

	if len(upstreams) < 1 {
		log.Fatal("missing --upstream")
	}
	if *cookieSecret == "" {
		log.Fatal("missing --cookie-secret")
	}
	if *clientID == "" {
		log.Fatal("missing --client-id")
	}
	if *clientSecret == "" {
		log.Fatal("missing --client-secret")
	}

	var upstreamUrls []*url.URL
	for _, u := range upstreams {
		upstreamUrl, err := url.Parse(u)
		if err != nil {
			log.Fatalf("error parsing --upstream %s", err.Error())
		}
		upstreamUrls = append(upstreamUrls, upstreamUrl)
	}
	redirectUrl, err := url.Parse(*redirectUrl)
	if err != nil {
		log.Fatalf("error parsing --redirect-url %s", err.Error())
	}

	validator := NewCommandValidator(*userVerificationCommand)
	oauthproxy := NewOauthProxy(upstreamUrls, *clientID, *clientSecret, *loginUrl, *redemptionUrl, *oauthScope, validator)
	oauthproxy.SetRedirectUrl(redirectUrl)

	listener, err := net.Listen("tcp", *httpAddr)
	if err != nil {
		log.Fatalf("FATAL: listen (%s) failed - %s", *httpAddr, err.Error())
	}
	log.Printf("listening on %s", *httpAddr)

	server := &http.Server{Handler: oauthproxy}
	err = server.Serve(listener)
	if err != nil && !strings.Contains(err.Error(), "use of closed network connection") {
		log.Printf("ERROR: http.Serve() - %s", err.Error())
	}

	log.Printf("HTTP: closing %s", listener.Addr().String())
}
