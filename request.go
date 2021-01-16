package request

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// inspired by https://github.com/imroc/req

type Headers map[string]string
type Query map[string]string
type BodyJSON map[string]interface{}
type BodyForm map[string]string

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

func New() *Request {
	return &Request{
		client: &http.Client{Timeout: time.Second * 3},
	}
}

type Request struct {
	client  HTTPClient
	base    string
	headers Headers
}

func (r *Request) SetBaseURL(base string) *Request {
	r.base = base
	return r
}

func (r *Request) SetBaseClient(client HTTPClient) *Request {
	r.client = client
	return r
}

func (r *Request) SetBaseHeaders(headers map[string]string) *Request {
	r.headers = headers
	return r
}

func (r *Request) Get(ctx context.Context, uri string, params ...interface{}) (*Resp, error) {
	return r.Do(ctx, "GET", uri, params...)
}

func (r *Request) Post(ctx context.Context, uri string, params ...interface{}) (*Resp, error) {
	return r.Do(ctx, "POST", uri, params...)
}

func (r *Request) Patch(ctx context.Context, uri string, params ...interface{}) (*Resp, error) {
	return r.Do(ctx, "PATCH", uri, params...)
}

func (r *Request) Delete(ctx context.Context, uri string, params ...interface{}) (*Resp, error) {
	return r.Do(ctx, "DELETE", uri, params...)
}

func (r *Request) Do(ctx context.Context, method, uri string, params ...interface{}) (*Resp, error) {
	var bodyParam io.Reader
	var queryParam Query

	headerParam := make(http.Header)
	for _, param := range params {
		switch v := param.(type) {
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
		case BodyJSON:
			jsonValue, err := json.Marshal(v)
			if err != nil {
				return nil, err
			}
			bodyParam = bytes.NewReader(jsonValue)
			if contentType := headerParam.Get("Content-Type"); contentType == "" {
				headerParam.Set("Content-Type", "application/json; charset=utf-8")
			}
		case BodyForm:
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

	req, err := http.NewRequestWithContext(ctx, method, fmt.Sprintf("%s%s", r.base, uri), bodyParam)
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

	resp, err := r.client.Do(req)
	return &Resp{resp}, err
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

func (r *Resp) ToJSON(v interface{}) error {
	body, err := r.ReadAll()
	if err != nil {
		return err
	}
	return json.Unmarshal(body, v)
}
