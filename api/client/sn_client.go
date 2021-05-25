package client

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/filecoin-project/go-jsonrpc"

	"github.com/filecoin-project/lotus/api"
	"github.com/gwaylib/errors"
)

func NewWorkerSnRPC(ctx context.Context, addr string, requestHeader http.Header) (api.WorkerSnAPI, jsonrpc.ClientCloser, error) {
	u, err := url.Parse(addr)
	if err != nil {
		return nil, nil, err
	}
	switch u.Scheme {
	case "ws":
		u.Scheme = "http"
	case "wss":
		u.Scheme = "https"
	}
	addr = u.String()

	var res api.WorkerSnAPIStruct
	closer, err := jsonrpc.NewMergeClient(ctx, addr, "Filecoin",
		[]interface{}{
			&res.Internal,
		},
		requestHeader,
		jsonrpc.WithNoReconnect(),
		jsonrpc.WithTimeout(120*time.Second),
	)
	if err != nil {
		return nil, nil, errors.As(err, addr)
	}
	return &res, closer, err

}

func NewSrvCenterRPC(ctx context.Context, addr string, requestHeader http.Header) (api.SrvCenterAPI, jsonrpc.ClientCloser, error) {
	var res api.SrvCenterAPIStruct

	closer, err := jsonrpc.NewMergeClient(ctx, addr, "Filecoin",
		[]interface{}{
			&res.Internal,
		},
		requestHeader,
		jsonrpc.WithNoReconnect(),
		jsonrpc.WithTimeout(120*time.Second),
	)
	if err != nil {
		return nil, nil, err
	}
	return &res, closer, err

}

func NewServerSnRPC(ctx context.Context, addr string, requestHeader http.Header) (api.ServerSnAPI, jsonrpc.ClientCloser, error) {
	var res api.ServerSnAPIStruct

	closer, err := jsonrpc.NewMergeClient(ctx, addr, "Filecoin",
		[]interface{}{
			&res.Internal,
		},
		requestHeader,
		jsonrpc.WithNoReconnect(),
		jsonrpc.WithTimeout(120*time.Second),
	)
	if err != nil {
		return nil, nil, errors.As(err, addr)
	}
	return &res, closer, err
}
