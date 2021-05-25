package api

import (
	"context"

	"github.com/filecoin-project/specs-storage/storage"
)

type ServerSnAPI interface {
	Version(context.Context) (Version, error) //perm:admin

	SealCommit2(context.Context, storage.SectorRef, storage.Commit1Out) (storage.Proof, error) //perm:admin
}
