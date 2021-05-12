# README

request is a go http client lib inspired by inspired by [req](https://github.com/imroc/req), 
which encapsualte offical http client, and provide some useful features to simplify development.


## Quick Start
1. import request in your go file, like this
```go
import "github.com/faceair/request"
```

2. call http api with request,
```go
resp, err := request.Get(ctx, url)
if err != nil {
    // handle error here
}
// Unmarshal response to json
var body BodyType
if err := resp.ToJson(&body); err != nil {
    // handle json unmarshal error here
}

// do something with body
```

## Features

1. concise api: query parameters, headers, form field and json payload are all able to be passed in one function call
2. friendly to pooling: common headers, domain name can be set for a set of HTTP apis
3. ready for unit test: just mock the `HTTPClient` and assert you business logic

## TODO
1. [ ] provide request metrics based on prometheus
2. [ ] provide request trace base on jaeger
