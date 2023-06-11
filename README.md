## request: A versatile HTTP client for Golang

`request` is an intuitive and flexible HTTP client written in Go. Inspired by the simplicity of the project https://github.com/imroc/req, this library aims to provide a higher level of abstraction for handling HTTP requests and responses. With a wide range of customization options, it makes sending HTTP requests and handling responses simpler and more efficient.

## Features

- Support for all basic HTTP methods: GET, POST, PATCH, PUT, DELETE.
- Built-in JSON and form data support for request bodies.
- Cookie jar support for easier cookie management.
- Dynamic and easy setting of query parameters and headers.
- Integrated basic authentication.
- Customizable timeouts and transport settings.
- Context support for cancellable and timeout requests.
- String, byte array, and io.Reader support for request bodies.
- Helper methods for response handling: read as string, read all, convert to JSON, save to file.

## Example Usage

```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/faceair/request"
)

func main() {
	client := request.New()

	client.SetBaseURL("https://api.github.com").
		SetBaseHeaders(request.Headers{
			"Accept": "application/vnd.github.v3+json",
		})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.Get(ctx, "/users/octocat")
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Println("Response:", resp.String())
}
```

This will send a GET request to `https://api.github.com/users/octocat` with the `Accept` header set as `application/vnd.github.v3+json` and a timeout of 5 seconds.

## License

[MIT](LICENSE).
