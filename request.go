package request

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// inspired by https://github.com/imroc/req

type Headers map[string]string
type Query map[string]string
type MapJSON map[string]interface{}
type MapForm map[string]string
type GetBody func() (io.ReadCloser, error)

type bodyJSON struct {
	v interface{}
}

func BodyJSON(v interface{}) *bodyJSON {
	return &bodyJSON{v: v}
}

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

func New() *Client {
	jar, _ := cookiejar.New(nil)
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
			DualStack: true,
		}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &Client{
		rnd: newSafeRnd(),
		http: &http.Client{
			Jar:       jar,
			Transport: transport,
			Timeout:   2 * time.Minute,
		},
	}
}

type Client struct {
	rnd      *safeRnd
	http     HTTPClient
	baseURLs []string
	headers  Headers
}

func (r *Client) SetBaseURL(baseURL string) *Client {
	r.SetBaseURLs([]string{baseURL})
	return r
}

func (r *Client) SetBaseURLs(baseURLs []string) *Client {
	r.baseURLs = baseURLs

	httpClient, ok := r.http.(*http.Client)
	if !ok {
		return r
	}
	httpTransport, ok := httpClient.Transport.(*http.Transport)
	if !ok {
		return r
	}

	var dialContext DialContext
	if httpTransport.DialContext != nil {
		dialContext = httpTransport.DialContext
	} else {
		dialContext = (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
			DualStack: true,
		}).DialContext
	}

	balancer := newDNSBalancer(dialContext)
	httpTransport.DialContext = balancer.DialContext
	return r
}

func (r *Client) EnableHTTPBalance(cacheExpire time.Duration) *Client {
	if len(r.baseURLs) == 0 {
		panic("http balancer requires base urls")
	}
	hosts := make([]string, 0, len(r.baseURLs))
	for _, baseURL := range r.baseURLs {
		baseU, err := url.Parse(baseURL)
		if err != nil {
			panic(err)
		}
		hosts = append(hosts, baseU.Host)
	}
	r.http = newHTTPBalancer(r.http, hosts, cacheExpire)
	return r
}

func (r *Client) SetBaseClient(client HTTPClient) *Client {
	r.http = client
	return r
}

func (r *Client) SetTimeout(timeout time.Duration) *Client {
	if client, ok := r.http.(*http.Client); ok {
		client.Timeout = timeout
	}
	return r
}

func (r *Client) SetBasicAuth(username, password string) *Client {
	if r.headers == nil {
		r.headers = make(Headers)
	}
	r.headers["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
	return r
}

func (r *Client) SetBaseHeaders(headers map[string]string) *Client {
	if r.headers == nil {
		r.headers = headers
	} else {
		for k, v := range headers {
			r.headers[k] = v
		}
	}
	return r
}

func (r *Client) Get(ctx context.Context, uri string, params ...interface{}) (*Resp, error) {
	return r.Do(ctx, "GET", uri, params...)
}

func (r *Client) Post(ctx context.Context, uri string, params ...interface{}) (*Resp, error) {
	return r.Do(ctx, "POST", uri, params...)
}

func (r *Client) Patch(ctx context.Context, uri string, params ...interface{}) (*Resp, error) {
	return r.Do(ctx, "PATCH", uri, params...)
}

func (r *Client) Put(ctx context.Context, uri string, params ...interface{}) (*Resp, error) {
	return r.Do(ctx, "PUT", uri, params...)
}

func (r *Client) Delete(ctx context.Context, uri string, params ...interface{}) (*Resp, error) {
	return r.Do(ctx, "DELETE", uri, params...)
}

func (r *Client) Do(ctx context.Context, method, uri string, params ...interface{}) (*Resp, error) {
	var bodyParam io.Reader
	var queryParam Query
	var getBody GetBody

	headerParam := make(http.Header)
	for _, param := range params {
		switch v := param.(type) {
		case string:
			bodyParam = strings.NewReader(v)
		case []byte:
			bodyParam = bytes.NewReader(v)
		case io.Reader:
			bodyParam = v
		case http.Header:
			headerParam = v
		case Headers:
			for key, value := range v {
				headerParam.Set(key, value)
			}
		case Query:
			queryParam = v
		case *bodyJSON, MapJSON:
			if vv, ok := param.(*bodyJSON); ok {
				v = vv.v
			}
			jsonValue, err := json.Marshal(v)
			if err != nil {
				return nil, err
			}
			bodyParam = bytes.NewReader(jsonValue)
			if contentType := headerParam.Get("Content-Type"); contentType == "" {
				headerParam.Set("Content-Type", "application/json; charset=utf-8")
			}
		case MapForm:
			form := url.Values{}
			for key, value := range v {
				form.Add(key, value)
			}
			bodyParam = strings.NewReader(form.Encode())
			if contentType := headerParam.Get("Content-Type"); contentType == "" {
				headerParam.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
			}
		case GetBody:
			getBody = v
		default:
			return nil, fmt.Errorf("unknown param %v", param)
		}
	}

	if u, _ := url.Parse(uri); u != nil && u.Scheme == "" {
		if len(r.baseURLs) == 1 {
			uri = r.baseURLs[0] + uri
		} else if len(r.baseURLs) > 1 {
			uri = r.baseURLs[r.rnd.IntN(len(r.baseURLs))] + uri
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, uri, bodyParam)
	if err != nil {
		return nil, err
	}
	req.GetBody = getBody

	query := req.URL.Query()
	for key, value := range queryParam {
		query.Set(key, value)
	}
	req.URL.RawQuery = query.Encode()

	for key, value := range r.headers {
		req.Header.Add(key, value)
	}
	for key, values := range headerParam {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	if host := req.Header.Get("Host"); host != "" {
		req.Host = host
	}

	resp, err := r.http.Do(req)
	if err != nil {
		return nil, err
	}
	return &Resp{resp}, nil
}

type Resp struct {
	*http.Response
}

func (r *Resp) String() string {
	body, _ := r.ReadAll()
	return string(body)
}

func (r *Resp) ReadAll() ([]byte, error) {
	defer func() { _ = r.Body.Close() }()
	return io.ReadAll(r.Body)
}

func (r *Resp) ToFile(filename string) error {
	defer func() { _ = r.Body.Close() }()

	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	_, err = io.Copy(file, r.Body)
	return err
}

func (r *Resp) ToJSON(v interface{}) error {
	body, err := r.ReadAll()
	if err != nil {
		return err
	}
	return json.Unmarshal(body, v)
}

type DialContext func(ctx context.Context, network, addr string) (net.Conn, error)

type DNSBalancer struct {
	rnd         *safeRnd
	dialContext DialContext
}

func newDNSBalancer(dialContext DialContext) *DNSBalancer {
	return &DNSBalancer{
		rnd:         newSafeRnd(),
		dialContext: dialContext,
	}
}

func (lb *DNSBalancer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}

	ips, err := net.LookupHost(host)
	if err != nil {
		return nil, err
	}

	if len(ips) == 0 {
		return nil, fmt.Errorf("no such host for %q", host)
	}

	lb.rnd.Shuffle(len(ips), func(i, j int) {
		ips[i], ips[j] = ips[j], ips[i]
	})

	var lastErr error
	for _, ip := range ips {
		conn, err := lb.dialContext(ctx, network, net.JoinHostPort(ip, port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

type HTTPBalancer struct {
	mu           sync.Mutex
	rnd          *safeRnd
	httpClient   HTTPClient
	hosts        []string
	cacheTTL     time.Duration
	cachedIPs    map[string][]string
	cachedExpiry map[string]time.Time
}

func newHTTPBalancer(http HTTPClient, targetHosts []string, cacheTTL time.Duration) *HTTPBalancer {
	return &HTTPBalancer{
		rnd:          newSafeRnd(),
		httpClient:   http,
		hosts:        targetHosts,
		cacheTTL:     cacheTTL,
		cachedIPs:    make(map[string][]string),
		cachedExpiry: make(map[string]time.Time),
	}
}

func (lb *HTTPBalancer) Do(req *http.Request) (*http.Response, error) {
	var hosts []string

	lb.mu.Lock()
	hosts = lb.hosts
	if len(hosts) > 1 {
		hosts = append([]string(nil), hosts...)
		lb.rnd.Shuffle(len(hosts), func(i, j int) {
			hosts[i], hosts[j] = hosts[j], hosts[i]
		})
	}
	lb.mu.Unlock()

	var finalErr error

	for _, host := range hosts {
		var ips []string

		domain, port, _ := net.SplitHostPort(host)
		if domain == "" {
			domain = host
		}

		lb.mu.Lock()
		if exp, ok := lb.cachedExpiry[host]; ok && time.Now().Before(exp) {
			ips = lb.cachedIPs[host]
			if len(ips) > 1 {
				ips = append([]string(nil), ips...)
			}
		}
		lb.mu.Unlock()

		if ips == nil {
			var err error
			ips, err = net.LookupHost(domain)
			if err != nil {
				finalErr = err
				continue
			}

			lb.mu.Lock()
			lb.cachedIPs[host] = ips
			lb.cachedExpiry[host] = time.Now().Add(lb.cacheTTL)
			lb.mu.Unlock()
		}

		if len(ips) == 0 {
			finalErr = fmt.Errorf("no such host for %q", host)
			continue
		}

		lb.rnd.Shuffle(len(ips), func(i, j int) {
			ips[i], ips[j] = ips[j], ips[i]
		})

		for _, ip := range ips {
			hostname := ip
			if port != "" {
				hostname = net.JoinHostPort(ip, port)
			}
			req.Host = host
			req.URL.Host = hostname
			resp, err := lb.httpClient.Do(req)
			if err == nil {
				return resp, nil
			}
			finalErr = err
		}
	}
	return nil, finalErr
}

type safeRnd struct {
	mux sync.Mutex
	rnd *rand.Rand
}

func newSafeRnd() *safeRnd {
	return &safeRnd{rnd: rand.New(rand.NewSource(time.Now().UnixNano()))}
}

func (r *safeRnd) Shuffle(n int, f func(i, j int)) {
	if n <= 1 {
		return
	}
	r.mux.Lock()
	r.rnd.Shuffle(n, f)
	r.mux.Unlock()
}

func (r *safeRnd) IntN(n int) int {
	r.mux.Lock()
	n = r.rnd.Intn(n)
	r.mux.Unlock()
	return n
}
