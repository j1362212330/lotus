package api

import (
	"context"
	"fmt"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/specs-actors/actors/runtime/proof"
	"github.com/filecoin-project/specs-storage/storage"
)

type WindowPoStResp struct {
	Proofs []proof.PoStProof
	Ignore []abi.SectorID
}

// 兼容老c2
type SectorRef struct {
	abi.SectorID
	ProofType abi.RegisteredSealProof
}

type WorkerSnAPI interface {
	Version(context.Context) (Version, error)

	SealCommit2(context.Context, SectorRef, storage.Commit1Out) (storage.Proof, error)
	GenerateWinningPoSt(context.Context, abi.ActorID, []storage.ProofSectorInfo, abi.PoStRandomness) ([]proof.PoStProof, error)
	GenerateWindowPoSt(context.Context, abi.ActorID, []storage.ProofSectorInfo, abi.PoStRandomness) (WindowPoStResp, error)
}

//==============================================================================

const (
	C2 = "SealCommit2"
)

type ServiceDescInfo struct {
	Kind string
	Host string
	Port int
}

type StatisticInfo struct {
	UpdatedAt map[string]int64
	IdleNum   map[string]int
}

func (s ServiceDescInfo) Key() string {
	return fmt.Sprintf("%s:%s:%d", s.Kind, s.Host, s.Port)
}

type SrvCenterAPI interface {
	Version(context.Context) (Version, error)

	RegisterService(context.Context, ServiceDescInfo) error
	GetService(context.Context, string) (ServiceDescInfo, error)
	StatisticInfo(context.Context) (StatisticInfo, error)
}

//==============================================================================

type ServerSnAPI interface {
	Version(context.Context) (Version, error)

	SealCommit2(context.Context, storage.SectorRef, storage.Commit1Out) (storage.Proof, error)
}
