package request

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
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
	baseURL  string
	baseURLs []string
	headers  Headers
}

func (r *Client) SetBaseURL(baseURL string) *Client {
	r.baseURL = baseURL
	return r
}

func (r *Client) SetBaseURLs(baseURLs []string) *Client {
	r.baseURLs = baseURLs
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
		default:
			return nil, fmt.Errorf("unknown param %v", param)
		}
	}

	if u, _ := url.Parse(uri); u != nil && u.Scheme == "" {
		if len(r.baseURLs) > 0 {
			uri = r.baseURLs[r.rnd.IntN(len(r.baseURLs))] + uri
		} else {
			uri = r.baseURL + uri
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, uri, bodyParam)
	if err != nil {
		return nil, err
	}

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
	return ioutil.ReadAll(r.Body)
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

type safeRnd struct {
	mux sync.Mutex
	rnd *rand.Rand
}

func newSafeRnd() *safeRnd {
	return &safeRnd{rnd: rand.New(rand.NewSource(time.Now().UnixNano()))}
}

func (r *safeRnd) IntN(n int) int {
	r.mux.Lock()
	defer r.mux.Unlock()
	return r.rnd.Intn(n)
}
