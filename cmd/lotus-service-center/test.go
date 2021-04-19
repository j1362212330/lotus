package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/filecoin-project/lotus/api"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"
)

var testCmd = &cli.Command{
	Name:  "test",
	Usage: "don not use in production environment",
	Subcommands: []*cli.Command{
		registerServiceCmd,
		getServiceCmd,
	},
}

var registerServiceCmd = &cli.Command{
	Name:  "register",
	Usage: "register an service to server center",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "service-center-address",
			Usage: "service center address",
			Value: "0.0.0.0:3452",
		},
		&cli.StringFlag{
			Name:  "kind",
			Usage: "the kind of service",
			Value: "SealCommit2",
		},
		&cli.StringFlag{
			Name:  "host",
			Usage: "the host provided service",
			Value: "127.0.0.1",
		},
		&cli.IntFlag{
			Name:  "port",
			Usage: "the port provided service",
			Value: 1080,
		},
	},
	Action: func(cctx *cli.Context) error {
		nodeCCtx = cctx
		ctx := lcli.ReqContext(cctx)

		if !cctx.IsSet("service-center-address") {
			return errors.New("need service center address")
		}

		addr := cctx.String("service-center-address")

		urlInfo := strings.Split(addr, ":")
		if len(urlInfo) != 2 {
			return errors.New("error address format")
		}

		rpcUrl = fmt.Sprintf("http://%s:%s/rpc/v0", urlInfo[0], urlInfo[1])

		if !cctx.IsSet("kind") {
			return errors.New("need the kind of the service")
		}

		if !cctx.IsSet("host") {
			return errors.New("need a service host")
		}

		if !cctx.IsSet("port") {
			return errors.New("need a service port")
		}

		service := api.ServiceDescInfo{
			Kind: cctx.String("kind"),
			Host: cctx.String("host"),
			Port: cctx.Int("port"),
		}
		napi, err := GetNodeApi()
		if err != nil {
			return err
		}

		return napi.RegisterService(ctx, service)
	},
}

var getServiceCmd = &cli.Command{
	Name:  "get",
	Usage: "get a service from server center",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "service-center-address",
			Usage: "service center address",
			Value: "127.0.0.1:1080",
		},
		&cli.StringFlag{
			Name:  "kind",
			Usage: "the kind of service",
			Value: "",
		},
	},
	Action: func(cctx *cli.Context) error {
		nodeCCtx = cctx
		ctx := lcli.ReqContext(cctx)

		if !cctx.IsSet("service-center-address") {
			return xerrors.New("need server center address")
		}

		addr := cctx.String("service-center-address")
		urlInfo := strings.Split(addr, ":")
		if len(urlInfo) != 2 {
			return errors.New("error url fomat")
		}

		rpcUrl = fmt.Sprintf("http://%s:%s/rpc/v0", urlInfo[0], urlInfo[1])

		if !cctx.IsSet("kind") {
			return errors.New("need the kind of service")
		}

		kind := cctx.String("kind")
		napi, err := GetNodeApi()
		if err != nil {
			return err
		}

		service, err := napi.GetService(ctx, kind)
		if err != nil {
			return err
		}
		fmt.Printf("Get an Service\n: ")
		fmt.Printf("\tKind: %s\n", service.Kind)
		fmt.Printf("\tHost: %s\n", service.Host)
		fmt.Printf("\tPort: %d\n", service.Port)
		return nil
	},
}
