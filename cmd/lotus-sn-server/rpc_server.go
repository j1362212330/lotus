package main

import (
	"context"
	"errors"
	"os"
	"sync/atomic"

	paramfetch "github.com/filecoin-project/go-paramfetch"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/specs-storage/storage"
)

const gateway = "https://proof-parameters.s3.cn-south-1.jdcloud-oss.com/ipfs/"
const paramdir = "/var/tmp/filecoin-proof-parameters"
const dirEnv = "FIL_PROOFS_PARAMETER_CACHE"
const gwEnv = "IPFS_GATEWAY"

type rpcServer struct {
	sb   *ffiwrapper.Sealer
	busy int32
}

func (w *rpcServer) checkParam(ctx context.Context, ssize abi.SectorSize) error {
	if len(os.Getenv(gwEnv)) == 0 {
		os.Setenv(gwEnv, gateway)
	}

	if len(os.Getenv(dirEnv)) == 0 {
		os.Setenv(dirEnv, paramdir)
	}
	return paramfetch.GetParams(ctx, build.ParametersJSON(), uint64(ssize))
}

func (w *rpcServer) Version(context.Context) (api.Version, error) {
	return 0, nil
}

func (w *rpcServer) SealCommit2(ctx context.Context, sector storage.SectorRef, commit1Out storage.Commit1Out) (storage.Proof, error) {
	if !atomic.CompareAndSwapInt32(&w.busy, 0, 1) {
		log.Info("worker sealcommit2 serverice is  busy")
		return storage.Proof{}, errors.New("worker sealcommit service is  busy")
	}
	defer atomic.SwapInt32(&w.busy, 0)
	log.Infof("SealCommit2 RPC in: %+v", sector)
	defer log.Infof("SealCommit2 RPC out: %+v", sector)
	return w.sb.SealCommit2(ctx, sector, commit1Out)
}

var _ api.ServerSnAPI = &rpcServer{}
