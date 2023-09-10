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

	if httpClient, ok := r.http.(*http.Client); ok {
		hosts := make([]string, len(baseURLs))
		for _, baseURL := range baseURLs {
			baseU, err := url.Parse(baseURL)
			if err != nil {
				panic(err)
			}
			hosts = append(hosts, baseU.Host)
		}
		httpClient.Transport = newLoadBalancer(httpClient.Transport, hosts)
	}
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

type loadBalancer struct {
	rand      *safeRnd
	transport http.RoundTripper
	hosts     []string
}

func newLoadBalancer(transport http.RoundTripper, hosts []string) *loadBalancer {
	return &loadBalancer{
		transport: transport,
		hosts:     hosts,
		rand:      newSafeRnd(),
	}
}

func (lb *loadBalancer) RoundTrip(req *http.Request) (*http.Response, error) {
	var lastErr error

	hosts := make([]string, len(lb.hosts))
	copy(hosts, lb.hosts)

	lb.rand.Shuffle(len(hosts), func(i, j int) {
		hosts[i], hosts[j] = hosts[j], hosts[i]
	})

	for _, host := range hosts {
		domain, port, _ := net.SplitHostPort(host)
		ips, err := net.LookupHost(domain)
		if err != nil {
			lastErr = err
			continue
		}

		if len(ips) == 0 {
			lastErr = fmt.Errorf("no such host for %q", domain)
			continue
		}

		lb.rand.Shuffle(len(ips), func(i, j int) {
			ips[i], ips[j] = ips[j], ips[i]
		})

		for _, ip := range ips {
			req.Host = host
			req.URL.Host = net.JoinHostPort(ip, port)
			resp, err := lb.transport.RoundTrip(req)
			if err == nil {
				return resp, nil
			}
			lastErr = err
		}
	}
	return nil, lastErr
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
