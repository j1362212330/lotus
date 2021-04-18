package storage

import (
	"fmt"
	"path/filepath"

	"github.com/filecoin-project/go-state-types/abi"
	"golang.org/x/xerrors"
)

// copy from sector-storage/stores/filetype.go
func ParseSectorID(baseName string) (abi.SectorID, error) {
	var n abi.SectorNumber
	var mid abi.ActorID
	read, err := fmt.Sscanf(baseName, "s-t0%d-%d", &mid, &n)
	if err != nil {
		read, err = fmt.Sscanf(baseName, "s-f0%d-%d", &mid, &n)
		if err != nil {
			return abi.SectorID{}, xerrors.Errorf("sscanf sector name ('%s'): %w", baseName, err)
		}
	}

	if read != 2 {
		return abi.SectorID{}, xerrors.Errorf("parseSectorID expected to scan 2 values, got %d", read)
	}

	return abi.SectorID{
		Miner:  mid,
		Number: n,
	}, nil
}

// copy from sector-storage/stores/filetype.go
func SectorName(sid abi.SectorID) string {
	return fmt.Sprintf("s-t0%d-%d", sid.Miner, sid.Number)
}

type SectorFile struct {
	SectorId    string
	StorageRepo string
}

func (f *SectorFile) SectorID() (abi.SectorID, error) {
	return ParseSectorID(f.SectorId)
}

func (f *SectorFile) UnsealedFile() string {
	return filepath.Join(f.StorageRepo, "unsealed", f.SectorId)
}
func (f *SectorFile) SealedFile() string {
	return filepath.Join(f.StorageRepo, "sealed", f.SectorId)
}
func (f *SectorFile) CachePath() string {
	return filepath.Join(f.StorageRepo, "cache", f.SectorId)
}
