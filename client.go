// Package sdwan is a Cisco SDWAN REST client library for Go.
package sdwan

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/tidwall/gjson"
)

const DefaultMaxRetries int = 3
const DefaultBackoffMinDelay int = 2
const DefaultBackoffMaxDelay int = 60
const DefaultBackoffDelayFactor float64 = 3

// Client is an HTTP SDWAN client.
// Use sdwan.NewClient to initiate a client.
// This will ensure proper cookie handling and processing of modifiers.
type Client struct {
	// HttpClient is the *http.Client used for API requests.
	HttpClient *http.Client
	// Url is the SDWAN vManage IP or hostname, e.g. https://10.0.0.1:443 (port is optional).
	Url string
	// Token is the current authentication token
	Token string
	// Usr is the SDWAN username.
	Usr string
	// Pwd is the SDWAN password.
	Pwd string
	// Insecure determines if insecure https connections are allowed.
	Insecure bool
	// Maximum number of retries
	MaxRetries int
	// Minimum delay between two retries
	BackoffMinDelay int
	// Maximum delay between two retries
	BackoffMaxDelay int
	// Backoff delay factor
	BackoffDelayFactor float64
	// Authentication mutex
	AuthenticationMutex *sync.Mutex
}

// NewClient creates a new SDWAN HTTP client.
// Pass modifiers in to modify the behavior of the client, e.g.
//
//	client, _ := NewClient("vmanage1.cisco.com", "user", "password", true, RequestTimeout(120))
func NewClient(url, usr, pwd string, insecure bool, mods ...func(*Client)) (Client, error) {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure},
	}

	cookieJar, _ := cookiejar.New(nil)
	httpClient := http.Client{
		Timeout:   60 * time.Second,
		Transport: tr,
		Jar:       cookieJar,
	}

	client := Client{
		HttpClient:          &httpClient,
		Url:                 url,
		Usr:                 usr,
		Pwd:                 pwd,
		Insecure:            insecure,
		MaxRetries:          DefaultMaxRetries,
		BackoffMinDelay:     DefaultBackoffMinDelay,
		BackoffMaxDelay:     DefaultBackoffMaxDelay,
		BackoffDelayFactor:  DefaultBackoffDelayFactor,
		AuthenticationMutex: &sync.Mutex{},
	}

	for _, mod := range mods {
		mod(&client)
	}
	return client, nil
}

// RequestTimeout modifies the HTTP request timeout from the default of 60 seconds.
func RequestTimeout(x time.Duration) func(*Client) {
	return func(client *Client) {
		client.HttpClient.Timeout = x * time.Second
	}
}

// MaxRetries modifies the maximum number of retries from the default of 3.
func MaxRetries(x int) func(*Client) {
	return func(client *Client) {
		client.MaxRetries = x
	}
}

// BackoffMinDelay modifies the minimum delay between two retries from the default of 2.
func BackoffMinDelay(x int) func(*Client) {
	return func(client *Client) {
		client.BackoffMinDelay = x
	}
}

// BackoffMaxDelay modifies the maximum delay between two retries from the default of 60.
func BackoffMaxDelay(x int) func(*Client) {
	return func(client *Client) {
		client.BackoffMaxDelay = x
	}
}

// BackoffDelayFactor modifies the backoff delay factor from the default of 3.
func BackoffDelayFactor(x float64) func(*Client) {
	return func(client *Client) {
		client.BackoffDelayFactor = x
	}
}

// NewReq creates a new Req request for this client.
func (client Client) NewReq(method, uri string, body io.Reader, mods ...func(*Req)) Req {
	httpReq, _ := http.NewRequest(method, client.Url+uri, body)
	req := Req{
		HttpReq:    httpReq,
		LogPayload: true,
	}
	for _, mod := range mods {
		mod(&req)
	}
	return req
}

// Do makes a request.
// Requests for Do are built ouside of the client, e.g.
//
//	req := client.NewReq("GET", "/admin/resourcegroup", nil)
//	res, _ := client.Do(req)
func (client *Client) Do(req Req) (Res, error) {
	// add token
	req.HttpReq.Header.Add("X-XSRF-TOKEN", client.Token)
	// retain the request body across multiple attempts
	var body []byte
	if req.HttpReq.Body != nil {
		body, _ = io.ReadAll(req.HttpReq.Body)
	}

	var res Res

	for attempts := 0; ; attempts++ {
		req.HttpReq.Body = io.NopCloser(bytes.NewBuffer(body))
		if req.LogPayload {
			log.Printf("[DEBUG] HTTP Request: %s, %s, %s", req.HttpReq.Method, req.HttpReq.URL, req.HttpReq.Body)
		} else {
			log.Printf("[DEBUG] HTTP Request: %s, %s", req.HttpReq.Method, req.HttpReq.URL)
		}

		httpRes, err := client.HttpClient.Do(req.HttpReq)
		if err != nil {
			if ok := client.Backoff(attempts); !ok {
				log.Printf("[ERROR] HTTP Connection error occured: %+v", err)
				log.Printf("[DEBUG] Exit from Do method")
				return Res{}, err
			} else {
				log.Printf("[ERROR] HTTP Connection failed: %s, retries: %v", err, attempts)
				continue
			}
		}

		defer httpRes.Body.Close()
		bodyBytes, err := io.ReadAll(httpRes.Body)
		if err != nil {
			if ok := client.Backoff(attempts); !ok {
				log.Printf("[ERROR] Cannot decode response body: %+v", err)
				log.Printf("[DEBUG] Exit from Do method")
				return Res{}, err
			} else {
				log.Printf("[ERROR] Cannot decode response body: %s, retries: %v", err, attempts)
				continue
			}
		}
		res = Res(gjson.ParseBytes(bodyBytes))
		if req.LogPayload {
			log.Printf("[DEBUG] HTTP Response: %s", res.Raw)
		}

		if httpRes.StatusCode >= 200 && httpRes.StatusCode <= 299 {
			log.Printf("[DEBUG] Exit from Do method")
			break
		} else {
			if ok := client.Backoff(attempts); !ok {
				log.Printf("[ERROR] HTTP Request failed: StatusCode %v", httpRes.StatusCode)
				log.Printf("[DEBUG] Exit from Do method")
				return res, fmt.Errorf("HTTP Request failed: StatusCode %v", httpRes.StatusCode)
			} else if httpRes.StatusCode == 429 {
				retryAfter := httpRes.Header.Get("Retry-After")
				retryAfterDuration := time.Duration(0)
				if retryAfter == "0" {
					retryAfterDuration = time.Second
				} else if retryAfter != "" {
					retryAfterDuration, _ = time.ParseDuration(retryAfter + "s")
				} else {
					retryAfterDuration = 15 * time.Second
				}
				log.Printf("[WARNING] HTTP Request rate limited, waiting %v seconds, Retries: %v", retryAfterDuration.Seconds(), attempts)
				time.Sleep(retryAfterDuration)
				continue
			} else if httpRes.StatusCode == 408 || (httpRes.StatusCode >= 500 && httpRes.StatusCode <= 599) {
				log.Printf("[ERROR] HTTP Request failed: StatusCode %v, Retries: %v", httpRes.StatusCode, attempts)
				continue
			} else {
				log.Printf("[ERROR] HTTP Request failed: StatusCode %v", httpRes.StatusCode)
				log.Printf("[DEBUG] Exit from Do method")
				return res, fmt.Errorf("HTTP Request failed: StatusCode %v", httpRes.StatusCode)
			}
		}
	}

	errCode := res.Get("error.code").Str
	if errCode != "" {
		log.Printf("[ERROR] JSON error: %s", res.Raw)
		return res, fmt.Errorf("JSON error: %s", res.Raw)
	}
	return res, nil
}

// Get makes a GET request and returns a GJSON result.
// Results will be the raw data structure as returned by vManage
func (client *Client) Get(path string, mods ...func(*Req)) (Res, error) {
	req := client.NewReq("GET", "/dataservice"+path, nil, mods...)
	err := client.Authenticate()
	if err != nil {
		return Res{}, err
	}
	return client.Do(req)
}

// Delete makes a DELETE request.
func (client *Client) Delete(path string, mods ...func(*Req)) (Res, error) {
	req := client.NewReq("DELETE", "/dataservice"+path, nil, mods...)
	err := client.Authenticate()
	if err != nil {
		return Res{}, err
	}
	return client.Do(req)
}

// DeleteBody makes a DELETE request with a payload.
// Hint: Use the Body struct to easily create DELETE body data.
func (client *Client) DeleteBody(path, data string, mods ...func(*Req)) (Res, error) {
	req := client.NewReq("DELETE", "/dataservice"+path, strings.NewReader(data), mods...)
	err := client.Authenticate()
	if err != nil {
		return Res{}, err
	}
	return client.Do(req)
}

// Post makes a POST request and returns a GJSON result.
// Hint: Use the Body struct to easily create POST body data.
func (client *Client) Post(path, data string, mods ...func(*Req)) (Res, error) {
	req := client.NewReq("POST", "/dataservice"+path, strings.NewReader(data), mods...)
	err := client.Authenticate()
	if err != nil {
		return Res{}, err
	}
	return client.Do(req)
}

// Put makes a PUT request and returns a GJSON result.
// Hint: Use the Body struct to easily create PUT body data.
func (client *Client) Put(path, data string, mods ...func(*Req)) (Res, error) {
	req := client.NewReq("PUT", "/dataservice"+path, strings.NewReader(data), mods...)
	err := client.Authenticate()
	if err != nil {
		return Res{}, err
	}
	return client.Do(req)
}

// Login authenticates to the SDWAN vManage device.
func (client *Client) Login() error {
	data := url.Values{}
	data.Set("j_username", client.Usr)
	data.Set("j_password", client.Pwd)
	for attempts := 0; ; attempts++ {
		req := client.NewReq("POST", "/j_security_check", strings.NewReader(data.Encode()), NoLogPayload)
		req.HttpReq.Header.Add("Content-Type", "application/x-www-form-urlencoded")
		httpRes, err := client.HttpClient.Do(req.HttpReq)
		if err != nil {
			return err
		}
		if httpRes.StatusCode != 200 {
			log.Printf("[ERROR] Authentication failed: StatusCode %v", httpRes.StatusCode)
			return fmt.Errorf("authentication failed, status code: %v", httpRes.StatusCode)
		}
		defer httpRes.Body.Close()
		bodyBytes, _ := io.ReadAll(httpRes.Body)
		if len(bodyBytes) > 0 {
			if ok := client.Backoff(attempts); !ok {
				log.Printf("[ERROR] Authentication failed: Invalid credentials")
				return fmt.Errorf("authentication failed, invalid credentials")
			} else {
				log.Printf("[ERROR] Authentication failed: %s, retries: %v", err, attempts)
				continue
			}
		}
		req = client.NewReq("GET", "/dataservice/client/token", nil)
		httpRes, err = client.HttpClient.Do(req.HttpReq)
		if err != nil {
			return err
		}
		if httpRes.StatusCode != 200 {
			log.Printf("[ERROR] Token retrieval failed: StatusCode %v", httpRes.StatusCode)
			return fmt.Errorf("authentication failed, token retrieval, status code: %v", httpRes.StatusCode)
		}
		defer httpRes.Body.Close()
		token, _ := io.ReadAll(httpRes.Body)
		if string(token) == "" {
			log.Printf("[ERROR] Token retrieval failed: no token in payload")
			return fmt.Errorf("authentication failed, no token in payload")
		}
		client.Token = string(token)
		log.Printf("[DEBUG] Authentication successful")
		return nil
	}
}

// Login if no token available.
func (client *Client) Authenticate() error {
	var err error
	client.AuthenticationMutex.Lock()
	if client.Token == "" {
		err = client.Login()
	}
	client.AuthenticationMutex.Unlock()
	return err
}

// Backoff waits following an exponential backoff algorithm
func (client *Client) Backoff(attempts int) bool {
	log.Printf("[DEBUG] Begining backoff method: attempts %v on %v", attempts, client.MaxRetries)
	if attempts >= client.MaxRetries {
		log.Printf("[DEBUG] Exit from backoff method with return value false")
		return false
	}

	minDelay := time.Duration(client.BackoffMinDelay) * time.Second
	maxDelay := time.Duration(client.BackoffMaxDelay) * time.Second

	min := float64(minDelay)
	backoff := min * math.Pow(client.BackoffDelayFactor, float64(attempts))
	if backoff > float64(maxDelay) {
		backoff = float64(maxDelay)
	}
	backoff = (rand.Float64()/2+0.5)*(backoff-min) + min
	backoffDuration := time.Duration(backoff)
	log.Printf("[TRACE] Starting sleeping for %v", backoffDuration.Round(time.Second))
	time.Sleep(backoffDuration)
	log.Printf("[DEBUG] Exit from backoff method with return value true")
	return true
}
