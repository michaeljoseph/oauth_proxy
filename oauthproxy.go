package main

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/bitly/go-simplejson"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

const signInPath = "/oauth2/sign_in"
const oauthStartPath = "/oauth2/start"
const oauthCallbackPath = "/oauth2/callback"

type OauthProxy struct {
	CookieSeed string
	CookieKey  string
	Validator  func(string) bool

	redirectUrl        *url.URL // the url to receive requests at
	oauthRedemptionUrl *url.URL // endpoint to redeem the code
	oauthLoginUrl      *url.URL // to redirect the user to
	oauthScope         string
	clientID           string
	clientSecret       string
	SignInMessage      string
	serveMux           *http.ServeMux
}

func NewOauthProxy(proxyUrls []*url.URL, clientID string, clientSecret string, oauthLoginUrl string, oauthRedemptionUrl string, oauthScope string, validator func(string) bool) *OauthProxy {
	login, _ := url.Parse(oauthLoginUrl)
	redeem, _ := url.Parse(oauthRedemptionUrl)
	serveMux := http.NewServeMux()
	for _, u := range proxyUrls {
		path := u.Path
		u.Path = ""
		log.Printf("mapping %s => %s", path, u)
		serveMux.Handle(path, httputil.NewSingleHostReverseProxy(u))
	}
	return &OauthProxy{
		CookieKey:  "_oauthproxy",
		CookieSeed: *cookieSecret,
		Validator:  validator,

		clientID:           clientID,
		clientSecret:       clientSecret,
		oauthScope:         oauthScope,
		oauthRedemptionUrl: redeem,
		oauthLoginUrl:      login,
		serveMux:           serveMux,
	}
}

func (p *OauthProxy) SetRedirectUrl(redirectUrl *url.URL) {
	redirectUrl.Path = oauthCallbackPath
	p.redirectUrl = redirectUrl
}

func (p *OauthProxy) GetLoginURL() string {
	params := url.Values{}
	params.Add("redirect_uri", p.redirectUrl.String())
	params.Add("scope", p.oauthScope)
	params.Add("client_id", p.clientID)
	params.Add("response_type", "code")
	return fmt.Sprintf("%s?%s", p.oauthLoginUrl, params.Encode())
}

func apiRequest(req *http.Request) (*simplejson.Json, error) {
	httpclient := &http.Client{}
	resp, err := httpclient.Do(req)
	if err != nil {
		return nil, err
	}
	body, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		log.Printf("got response code %d - %s", resp.StatusCode, body)
		return nil, errors.New("api request returned error code")
	}
	log.Printf("got body %s", string(body))
	data, err := simplejson.NewJson(body)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (p *OauthProxy) redeemCode(code string) (string, error) {
	params := url.Values{}
	params.Add("redirect_uri", p.redirectUrl.String())
	params.Add("client_id", p.clientID)
	params.Add("client_secret", p.clientSecret)
	params.Add("code", code)
	params.Add("grant_type", "authorization_code")
	req, err := http.NewRequest("POST", p.oauthRedemptionUrl.String(), bytes.NewBufferString(params.Encode()))
	req.Header.Set("Accept", "application/json")
	if err != nil {
		log.Printf("failed building request %s", err.Error())
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	json, err := apiRequest(req)
	if err != nil {
		log.Printf("failed making request %s", err.Error())
		return "", err
	}
	access_token, err := json.Get("access_token").String()
	if err != nil {
		return "", err
	}
	return access_token, nil
}

func (p *OauthProxy) ClearCookie(rw http.ResponseWriter, req *http.Request) {
	domain := strings.Split(req.Host, ":")[0]
	if *cookieDomain != "" && strings.HasSuffix(domain, *cookieDomain) {
		domain = *cookieDomain
	}
	cookie := &http.Cookie{
		Name:     p.CookieKey,
		Value:    "",
		Path:     "/",
		Domain:   domain,
		Expires:  time.Now().Add(time.Duration(1) * time.Hour * -1),
		HttpOnly: true,
	}
	http.SetCookie(rw, cookie)
}

func (p *OauthProxy) SetCookie(rw http.ResponseWriter, req *http.Request, val string) {

	domain := strings.Split(req.Host, ":")[0] // strip the port (if any)
	if *cookieDomain != "" && strings.HasSuffix(domain, *cookieDomain) {
		domain = *cookieDomain
	}
	cookie := &http.Cookie{
		Name:     p.CookieKey,
		Value:    signedCookieValue(p.CookieSeed, p.CookieKey, val),
		Path:     "/",
		Domain:   domain,
		Expires:  time.Now().Add(time.Duration(168) * time.Hour), // 7 days
		HttpOnly: true,
		// Secure: req. ... ? set if X-Scheme: https ?
	}
	fmt.Printf("calling http setcookie with %#v", cookie)
	http.SetCookie(rw, cookie)
}

func (p *OauthProxy) ErrorPage(rw http.ResponseWriter, code int, title string, message string) {
	log.Printf("ErrorPage %d %s %s", code, title, message)
	rw.WriteHeader(code)
	templates := getTemplates()
	t := struct {
		Title   string
		Message string
	}{
		Title:   fmt.Sprintf("%d %s", code, title),
		Message: message,
	}
	templates.ExecuteTemplate(rw, "error.html", t)
}

func (p *OauthProxy) SignInPage(rw http.ResponseWriter, req *http.Request, code int) {
	// TODO: capture state for which url to redirect to at the end
	p.ClearCookie(rw, req)
	rw.WriteHeader(code)
	templates := getTemplates()

	t := struct {
		SignInMessage string
	}{
		SignInMessage: p.SignInMessage,
	}
	templates.ExecuteTemplate(rw, "sign_in.html", t)
}

func (p *OauthProxy) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	// check if this is a redirect back at the end of oauth
	remoteIP := req.Header.Get("X-Real-IP")
	if remoteIP == "" {
		remoteIP = req.RemoteAddr
	}
	log.Printf("%s %s %s", remoteIP, req.Method, req.URL.Path)

	var ok bool
	if req.URL.Path == signInPath {
		http.Redirect(rw, req, p.GetLoginURL(), 302)
		return
	}
	if req.URL.Path == oauthStartPath {
		http.Redirect(rw, req, p.GetLoginURL(), 302)
		return
	}
	if req.URL.Path == oauthCallbackPath {
		// finish the oauth cycle
		reqParams, err := url.ParseQuery(req.URL.RawQuery)
		if err != nil {
			p.ErrorPage(rw, 500, "Internal Error", err.Error())
			return
		}
		errorString, ok := reqParams["error"]
		if ok && len(errorString) == 1 {
			p.ErrorPage(rw, 403, "Permission Denied", errorString[0])
			return
		}
		code, ok := reqParams["code"]
		if !ok || len(code) != 1 {
			p.ErrorPage(rw, 500, "Internal Error", "Invalid API response")
			return
		}

		token, err := p.redeemCode(code[0])
		if err != nil {
			log.Printf("error redeeming code %s", err.Error())
			p.ErrorPage(rw, 500, "Internal Error", err.Error())
			return
		}

		if !p.Validator(token) {
			p.ErrorPage(rw, 403, "Permission Denied", "Invalid Account")
			return
		}
		p.SetCookie(rw, req, "ok")
		http.Redirect(rw, req, "/", 302)
	}

	if !ok {
		cookie, err := req.Cookie(p.CookieKey)
		if err == nil {
			_, ok = validateCookie(cookie, p.CookieSeed)
		}
	}

	if !ok {
		log.Printf("invalid cookie")
		p.SignInPage(rw, req, 403)
		return
	}

	p.serveMux.ServeHTTP(rw, req)
}
