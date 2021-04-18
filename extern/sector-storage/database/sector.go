package database

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"time"

	"github.com/filecoin-project/specs-storage/storage"
	"github.com/gwaylib/database"
	"github.com/gwaylib/errors"
)

const (
	SECTOR_STATE_INIT = 0

	SECTOR_STATE_MOVE = 100 //开始推送扇区到存储节点
	SECTOR_STATE_PUSH = 101 //已经推送扇区到存储节点

	SECTOR_STATE_DONE   = 200
	SECTOR_STATE_FAILED = 500
)

type SectorInfo struct {
	ID         string    `db:"id"` // s-t0101-1
	MinerId    string    `db:"miner_id"`
	UpdateTime time.Time `db:"updated_at"`
	StorageId  int64     `db:"storage_id"`
	WorkerId   string    `db:"worker_id"`
	State      int       `db:"state,0"`
	StateTime  time.Time `db:"state_time"`
	StateTimes int       `db:"state_times"`
	CreateTime time.Time `db:"created_at"`
}

type SectorStorage struct {
	SectorInfo  SectorInfo
	StorageInfo StorageInfo
	WorkerInfo  WorkerInfo
}

type StorageStatus struct {
	StorageId int64
	MountDir  string
	MountUri  string
	Disable   bool
	Used      time.Duration
	Err       string
}
type StorageStatusSort []StorageStatus

func (g StorageStatusSort) Len() int {
	return len(g)
}
func (g StorageStatusSort) Swap(i, j int) {
	g[i], g[j] = g[j], g[i]
}
func (g StorageStatusSort) Less(i, j int) bool {
	return g[i].Used < g[j].Used
}

func AddSectorInfo(info *SectorInfo) error {
	mdb := GetDB()
	if _, err := database.InsertStruct(mdb, info, "sector_info"); err != nil {
		return errors.As(err)
	}
	return nil
}

// if data not found, it will return a 'default' WorkerId
func GetSectorInfo(id string) (*SectorInfo, error) {
	mdb := GetDB()
	info := &SectorInfo{
		ID: id,
	}
	if err := database.QueryStruct(mdb, info, "SELECT * FROM sector_info WHERE id=?", id); err != nil {
		if !errors.ErrNoData.Equal(err) {
			return nil, errors.As(err)
		}
		info.StorageId = 1
		info.WorkerId = "default"
	}
	return info, nil
}

func GetSectorState(id string) (int, error) {
	mdb := GetDB()
	state := -1
	if err := database.QueryElem(mdb, &state, "SELECT state FROM sector_info WHERE id=?", id); err != nil {
		return state, errors.As(err, id)
	}
	return state, nil
}

func GetSectorFile(id, defaultRepo string) (*storage.SectorFile, error) {
	if !HasDB() {
		return &storage.SectorFile{
			SectorId:    id,
			StorageRepo: defaultRepo,
		}, nil
	}

	mdb := GetDB()
	storageId := uint64(0)
	mountDir := sql.NullString{}
	if err := mdb.QueryRow("SELECT tb1.storage_id,tb2.mount_dir FROM sector_info tb1 LEFT JOIN storage_info tb2 on tb1.storage_id=tb2.id WHERE tb1.id=?", id).Scan(
		&storageId,
		&mountDir,
	); err != nil {
		if err != sql.ErrNoRows {
			return nil, errors.As(err, id)
		}

		// sector not found in db, return default.
		return &storage.SectorFile{
			SectorId:    id,
			StorageRepo: defaultRepo,
		}, nil

	}
	if len(mountDir.String) == 0 {
		return nil, errors.New("storage not found").As(id)
	}
	return &storage.SectorFile{SectorId: id, StorageRepo: filepath.Join(mountDir.String, fmt.Sprintf("%d", storageId))}, nil
}

type SectorList []SectorInfo

func GetSectorByState(storageId int64, state int64) (SectorList, error) {
	mdb := GetDB()
	list := SectorList{}
	if err := database.QueryStructs(mdb, &list, "SELECT * FROM sector_info WHERE storage_id=? AND state=?", storageId, state); err != nil {
		return nil, errors.As(err, storageId)
	}
	return list, nil
}

func GetAllSectorByState(state int64) (map[string]int64, error) {
	mdb := GetDB()
	rows, err := mdb.Query("SELECT id,storage_id FROM sector_info WHERE state=?", state)
	if err != nil {
		return nil, errors.As(err)
	}
	defer rows.Close()

	result := map[string]int64{}
	for rows.Next() {
		sid := ""
		storageId := int64(0)
		if err := rows.Scan(&sid, &storageId); err != nil {
			return nil, errors.As(err)
		}
		result[sid] = storageId
	}
	if len(result) == 0 {
		return nil, errors.ErrNoData.As(state)
	}
	return result, nil
}

func GetSectorStorage(id string) (*SectorStorage, error) {
	mdb := GetDB()
	// TODO: make left join
	seInfo := &SectorInfo{
		ID: id,
	}
	if err := database.QueryStruct(mdb, seInfo, "SELECT * FROM sector_info WHERE id=?", id); err != nil {
		if !errors.ErrNoData.Equal(err) {
			return nil, errors.As(err)
		}
		seInfo.WorkerId = "default"
		seInfo.StorageId = 1
	}
	stInfo := &StorageInfo{}
	if err := database.QueryStruct(mdb, stInfo, "SELECT * FROM storage_info WHERE id=?", seInfo.StorageId); err != nil {
		if !errors.ErrNoData.Equal(err) {
			return nil, errors.As(err, id)
		}
	}
	wkInfo := &WorkerInfo{
		ID: seInfo.WorkerId,
	}
	if err := database.QueryStruct(mdb, wkInfo, "SELECT * FROM worker_info WHERE id=?", seInfo.WorkerId); err != nil {
		if !errors.ErrNoData.Equal(err) {
			return nil, errors.As(err, id)
		}

		// upgrade fixed for worker ip
	}
	return &SectorStorage{
		SectorInfo:  *seInfo,
		StorageInfo: *stInfo,
		WorkerInfo:  *wkInfo,
	}, nil
}

func UpdateSectorState(sid, wid, msg string, state int) error {
	mdb := GetDB()
	if _, err := mdb.Exec(`
UPDATE
	sector_info
SET
	worker_id=?,
	state=?,
	state_time=?,
	state_msg=?,
	state_times=state_times+1
WHERE
	id=?
	
`, wid, state, time.Now(), msg, sid); err != nil {
		return errors.As(err)
	}
	return nil
}

type WorkingSectors []SectorInfo

// IsFullWork 当前worker上正在密封的扇区和密封完成后等待传输到存储节点扇区的数量是否大于该worker的最大任务数量。
func (ws WorkingSectors) IsFullWork(maxTaskNum, cacheNum int) bool {
	working := 0 // < 101
	pushing := 0 // 101
	for _, w := range ws {
		if w.State < SECTOR_STATE_PUSH {
			working++
		} else if w.State < SECTOR_STATE_DONE {
			pushing++
		}
	}
	//当前磁盘上缓存的数量。
	cacheCap := pushing - cacheNum
	if cacheCap < 0 {
		cacheCap = 0
	}
	if cacheCap+working >= maxTaskNum {
		return true
	}
	return false
}
func GetWorking(workerId string) (WorkingSectors, error) {
	mdb := GetDB()
	sectors := WorkingSectors{}
	if err := database.QueryStructs(mdb, &sectors, "SELECT * FROM sector_info WHERE worker_id=? AND state<200", workerId); err != nil {
		return nil, errors.As(err, workerId)
	}
	return sectors, nil
}

// Only called in  cache-mode=1
// SPECS: not call in more than 1000 tasks.
func CheckWorkingById(sid []string) (WorkingSectors, error) {
	sectors := WorkingSectors{}
	args := []rune{}
	for _, s := range sid {
		// checking sql injection
		if _, err := storage.ParseSectorID(s); err != nil {
			return sectors, errors.As(err, sid)
		}
		args = append(args, []rune(",'")...)
		args = append(args, []rune(s)...)
		args = append(args, []rune("'")...)
	}
	if len(args) > 0 {
		args = args[1:] // remove ',' in the head.
	}
	mdb := GetDB()
	sqlStr := fmt.Sprintf("SELECT * FROM sector_info WHERE id in (%s) AND state<200", string(args))
	if err := database.QueryStructs(mdb, &sectors, sqlStr); err != nil {
		return nil, errors.As(err)
	}
	return sectors, nil
}

func UpdateSectorWorkerId(oldWorkerId, newWorkerId string) error {
	mdb := GetDB()
	if _, err := mdb.Exec(`
UPDATE
	sector_info
SET
	worker_id=?
WHERE
	worker_id=?
AND 
	state<200

`, newWorkerId, oldWorkerId); err != nil {
		return errors.As(err)
	}
	return nil
}
