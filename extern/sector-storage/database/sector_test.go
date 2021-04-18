package database

import (
	"fmt"
	"testing"
	"time"
)

func TestSectorInfo(t *testing.T) {
	InitDB("./")
	minerId := "t0101"
	sectorId := time.Now().UnixNano()
	id := storage.SectorID(minerId, sectorId)
	info := &SectorInfo{
		ID:         id,
		MinerId:    minerId,
		UpdateTime: time.Now(),
		StorageId:  1,
		WorkerId:   "default",
		CreateTime: time.Now(),
	}
	if err := AddSectorInfo(info); err != nil {
		t.Fatal(err)
	}

	exp, err := GetSectorInfo(id)
	if err != nil {
		t.Fatal(err)
	}
	if exp.WorkerId != info.WorkerId || info.StorageId != exp.StorageId {
		t.Fatal(*info, *exp)
	}
}

func TestCheckWorkingById(t *testing.T) {
	InitDB("/data/sdb/lotus-user-1/.lotusstorage")
	working, err := CheckWorkingById([]string{"s-t01003-4"})
	if err != nil {
		t.Fatal(err)
	}

	fmt.Printf("%+v\n", working)
}
