package cliutil

import (
	"net/http"
	"net/url"

	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/client"
	"github.com/urfave/cli/v2"
)

func GetSrvCenterAPI(ctx *cli.Context, addr string) (api.SrvCenterAPI, jsonrpc.ClientCloser, error) {
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
	reqHeader := http.Header{}
	return client.NewSrvCenterRPC(ctx.Context, addr, reqHeader)
}

func GetServerSnAPI(ctx *cli.Context, addr string) (api.ServerSnAPI, jsonrpc.ClientCloser, error) {
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
	reqHeader := http.Header{}
	return client.NewServerSnRPC(ctx.Context, addr, reqHeader)
}
