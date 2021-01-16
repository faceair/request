package request

import "context"

var std = New()

func Get(ctx context.Context, uri string, params ...interface{}) (*Resp, error) {
	return std.Get(ctx, uri, params...)
}

func Post(ctx context.Context, uri string, params ...interface{}) (*Resp, error) {
	return std.Post(ctx, uri, params...)
}

func Patch(ctx context.Context, uri string, params ...interface{}) (*Resp, error) {
	return std.Patch(ctx, uri, params...)
}

func Delete(ctx context.Context, uri string, params ...interface{}) (*Resp, error) {
	return std.Delete(ctx, uri, params...)
}
